// Per-session streaming transcript. Built for high-frequency token deltas
// WITHOUT re-render storms or unbounded growth:
//   - text/reasoning deltas are coalesced into a single "live" block and
//     flushed once per animation frame (not per delta);
//   - completed blocks live in an append-only $state array, mutated in place
//     (push/splice), never spread-copied;
//   - the live window is hard-capped (CAP); older blocks are paged from
//     Bridge.State on demand (`truncated` flags the boundary);
//   - the Wails listener is registered in start() and removed in dispose(), so
//     listener lifetime == the owning $effect's lifetime (no leaks on nav).
import { on, ev } from "$lib/events";
import type { StreamEventDTO, WireEventDTO, MessageDTO } from "$lib/types";

// uid is a monotonic per-block identity used for stable keying in the view, so
// the CAP splice (which shifts array indices) never forces a full re-render or
// remount of the heavy tool/markdown rows.
export type TextBlock = { uid: number; kind: "text" | "reasoning"; step: number; text: string };
export type ToolBlock = {
  uid: number;
  kind: "tool";
  id: string;
  name: string;
  args: string;
  result?: string;
  isError?: boolean;
  done: boolean;
};
export type NoteBlock = { uid: number; kind: "note"; text: string };
export type Block = TextBlock | ToolBlock | NoteBlock;

// One task from the `todo` tool's plan, surfaced live in the chat view.
export type TodoEntry = { content: string; status: string; priority?: string };

// parseTodos pulls the task list from a `todo` tool block's raw JSON args. The
// tool passes the COMPLETE list every call, so the latest todo block IS the
// current plan. Returns [] on anything unparseable.
function parseTodos(args: string): TodoEntry[] {
  if (!args) return [];
  try {
    const v = JSON.parse(args);
    const list = Array.isArray(v?.todos) ? v.todos : [];
    return list
      .filter((t: unknown): t is TodoEntry => !!t && typeof (t as TodoEntry).content === "string")
      .map((t: TodoEntry) => ({ content: t.content, status: t.status || "pending", priority: t.priority }));
  } catch {
    return [];
  }
}

const CAP = 2000;

// Event kinds that imply an in-flight TURN — receiving one live (non-replay)
// is sufficient to flip an IDLE session to "running". Everything else the
// daemon emits (`note`, `bg_done`, `done`, and the `unknown` fallback) is a
// display/wake/lifecycle signal and must NOT resurrect an idle session.
const TURN_KINDS = new Set<string>(["text", "reasoning", "tool_start", "tool_result", "approval"]);

export type Transcript = ReturnType<typeof createTranscript>;

export function createTranscript(sessionId: string) {
  let history = $state<Block[]>([]);
  let live = $state<TextBlock | null>(null);
  let running = $state(false);
  let truncated = $state(false);
  // Optimistic "request sent, awaiting the first event" flag. running only flips
  // true when the FIRST stream event of a turn arrives — but a slow streaming
  // model (glm/gpt thinking before the first token) can take seconds, leaving a
  // dead gap where the view shows nothing and the turn looks frozen. The view
  // sets this the instant it sends; the first real turn event (or done) clears
  // it, so `working = running || pending` shows the indicator immediately.
  let pending = $state(false);
  // Bumped whenever an `approval` event arrives, so the view can refetch State
  // (which carries the pending approvals) even though the turn stays "running"
  // while the daemon blocks waiting for the user's decision.
  let approvalSeq = $state(0);
  // Latest token count streamed mid-turn. sess.tokens only refreshes on
  // refreshState() (turn end / approval), so the dock context ring freezes
  // then jumps during a long turn; this carries the live count through the
  // seam so the view can drive pct/nearLimit without waiting for the turn to
  // end. Prefer outTokens (grows as the model streams), fall back to inTokens.
  // Cleared at turn end (`done`) so a stale figure never bleeds into the next
  // idle window.
  let liveTokens = $state(0);

  let pendingText = "";
  let pendingKind: "text" | "reasoning" = "text";
  let pendingStep = -1;
  let rafId = 0;
  let disposed = false;
  let off: (() => void) | null = null;
  let uidSeq = 0;
  const nextUid = () => ++uidSeq;
  // Did this turn stream any text/reasoning delta? A non-Streamer provider
  // (Converse/opus path) emits the final answer as a single `done`{text} with
  // NO preceding `text`/`reasoning` deltas, so commitLive() has nothing to
  // flush and the reply would be dropped (GUI-092). When this stays false at
  // `done`, we push e.text as a final text block. Set on the first delta of a
  // turn, reset when a turn starts.
  let streamedThisTurn = false;

  // Seq reassembly: each StreamEventDTO carries a monotonic per-session ordinal
  // (pump.go stamps it in emit order), but Wails dispatches each event on its
  // own goroutine, so arrival at the webview can be reordered. We apply events
  // strictly in seq order: an early arrival is parked until its predecessors
  // land.
  //
  // `expectedSeq` is the next seq to apply. It is LATCHED from the first event
  // actually seen (sentinel 0 = not yet latched) rather than hardcoded to 1 —
  // because the pump's per-session seq counter does NOT reset when a view
  // re-attaches (Bridge.Subscribe is a no-op if the pump is already live), so a
  // re-entered chat can start receiving at seq 50, not 1. Hardcoding 1 parked
  // every event in reorderBuf until the 256 overflow valve fired, so short
  // turns (and the final message of a turn) never rendered. Latching to the
  // first seen seq makes reassembly correct from whatever base the live pump is
  // at, while still ordering within the window.
  let expectedSeq = 0;
  const reorderBuf = new Map<number, StreamEventDTO>();

  // Apply one (now in-order) stream event: update the running flag, then fold it
  // into the transcript. Extracted so the seq-reassembly loop and the no-seq
  // fast path share identical semantics.
  function applyEvent(m: StreamEventDTO) {
    // Only kinds that imply an in-flight TURN may flip an idle session to
    // "running". Wake/lifecycle kinds (bg_done, note, done, unknown) must NOT:
    // the daemon's bg_done wake would otherwise resurrect an IDLE session.
    // Replayed buffer events never flip it (running is owned by the State seed).
    // done always clears it (in onEvent).
    if (!m.replay && TURN_KINDS.has(m.event.kind)) {
      if (!running) streamedThisTurn = false;
      running = true;
      pending = false; // a real turn event took over the optimistic flag
    }
    onEvent(m.event, m.replay);
  }

  // Flush the reorder buffer in seq order and resync the window past it. Used by
  // the overflow valve (a permanently-missing seq, or a live event that raced
  // ahead of a replay that never came) so a stuck buffer can't strand events.
  function drainBufInOrder() {
    const keys = [...reorderBuf.keys()].sort((a, b) => a - b);
    for (const k of keys) {
      applyEvent(reorderBuf.get(k)!);
      reorderBuf.delete(k);
    }
    if (keys.length) expectedSeq = keys[keys.length - 1] + 1;
  }

  function pushHistory(b: Block) {
    // Reassign (new array reference), don't mutate in place. The Chat view binds
    // VirtualList's `items` to a $derived of this array; an in-place .push keeps
    // the SAME reference, so the keyed #each / windowing didn't reliably re-run
    // for the final block at turn end (the last message only appeared after
    // leaving + re-entering the chat, which re-seeds a fresh array). A new
    // reference makes the derived fire every push. This is NOT per-token — live
    // tokens accumulate in `live`; pushHistory only fires at block boundaries
    // (tool start/result, note, and the turn's final commit), so the array copy
    // is cheap and bounded by CAP.
    let next = history.length >= CAP ? history.slice(history.length - CAP + 1) : history.slice();
    if (history.length >= CAP) truncated = true;
    next.push(b);
    history = next;
  }

  function resetPending() {
    pendingText = "";
    pendingKind = "text";
    pendingStep = -1;
  }

  function commitLive() {
    if (live) {
      pushHistory(live);
      live = null;
    }
  }

  function scheduleFlush() {
    if (!disposed && !rafId) rafId = requestAnimationFrame(flush);
  }

  function flush() {
    rafId = 0;
    if (disposed || !pendingText) return;
    if (!live || live.kind !== pendingKind || live.step !== pendingStep) {
      commitLive();
      live = { uid: nextUid(), kind: pendingKind, step: pendingStep, text: pendingText };
    } else {
      live = { ...live, text: live.text + pendingText };
    }
    pendingText = "";
  }

  function onEvent(e: WireEventDTO, replay: boolean) {
    // Stream-token signal (GUI-061): any event MAY carry inTokens/outTokens.
    // Keep the latest live count so the dock context ring updates mid-turn
    // instead of jumping at turn end. outTokens is the figure that grows while
    // the model streams; fall back to inTokens when only that is present.
    if (e.outTokens != null) liveTokens = e.outTokens;
    else if (e.inTokens != null) liveTokens = e.inTokens;
    switch (e.kind) {
      case "text":
      case "reasoning": {
        const k = e.kind;
        const step = e.step ?? 0;
        if (k !== pendingKind || step !== pendingStep) {
          flush(); // close out the current bucket before switching
          pendingKind = k;
          pendingStep = step;
        }
        pendingText += e.text ?? "";
        streamedThisTurn = true;
        scheduleFlush();
        break;
      }
      case "tool_start":
        flush();
        commitLive();
        resetPending();
        pushHistory({ uid: nextUid(), kind: "tool", id: e.toolId ?? "", name: e.tool ?? "", args: e.toolArgs ?? "", done: false });
        break;
      case "tool_result": {
        let matched = false;
        for (let i = history.length - 1; i >= 0; i--) {
          const b = history[i];
          if (b.kind === "tool" && b.id === e.toolId) {
            history[i] = { ...b, result: e.result, isError: e.isError, done: true };
            matched = true;
            break;
          }
        }
        // No matching tool_start in history (a foreign-transcript/empty-id case,
        // or a result that somehow outran its start): surface it as a standalone
        // done tool block rather than silently dropping the result.
        if (!matched) {
          pushHistory({ uid: nextUid(), kind: "tool", id: e.toolId ?? "", name: e.tool ?? "tool", args: "", result: e.result, isError: e.isError, done: true });
        }
        break;
      }
      case "done":
        flush();
        commitLive();
        resetPending();
        // Non-Streamer providers (Converse/opus path) deliver the whole answer
        // in `done`{text} with no preceding deltas, so there is no `live` block
        // to flush — append the final text directly or it is dropped from the
        // transcript (GUI-092). Streaming turns already committed their text via
        // the deltas, so skip to avoid a duplicate tail block.
        if (!streamedThisTurn && e.text) {
          pushHistory({ uid: nextUid(), kind: "text", step: e.step ?? 0, text: e.text });
        }
        streamedThisTurn = false;
        running = false;
        pending = false; // turn over — drop any optimistic indicator
        // Turn over: the view refetches sess.tokens via refreshState(), so the
        // live override is no longer authoritative — clear it.
        liveTokens = 0;
        break;
      case "note":
        flush();
        commitLive();
        resetPending();
        pushHistory({ uid: nextUid(), kind: "note", text: e.text ?? "" });
        // Abnormal turn end (provider error / interrupt / overflow-no-compact /
        // reasoning-only spin-out) emits ONLY a terminal note, never a `done`,
        // so the composer would stay stuck in the working state forever. Clear
        // running AND the optimistic pending flag here (a send that errors before
        // any turn event ends on a note, not a done). In-turn informational notes
        // (e.g. compaction) are followed by a `done` that also clears these.
        running = false;
        pending = false;
        // Clear the streamed-this-turn guard too (GUI-092): a streamed turn that
        // ends on a terminal note (provider error / interrupt) would otherwise
        // leave the flag set, and a following non-Streamer single-`done` answer
        // would be wrongly suppressed and dropped. An in-turn note (compaction)
        // is followed by re-streamed deltas that set the flag again before its
        // own `done`, so clearing here is safe for streaming turns.
        streamedThisTurn = false;
        break;
      case "approval":
        // A gated tool is waiting for the user. The turn stays running, so the
        // view can't rely on running→false to refetch pending approvals; bump a
        // signal it watches instead. Only for LIVE approvals (GUI-065): on
        // reconnect the daemon replays its buffer, so a resolved approval would
        // re-bump approvalSeq → refreshState and the gate UI could briefly
        // re-show an approval the user already decided. Replayed history is
        // already reflected in the State() seed, so ignore it here.
        if (!replay) approvalSeq++;
        break;
    }
  }

  return {
    get history() {
      return history;
    },
    get live() {
      return live;
    },
    get running() {
      return running;
    },
    // working = a real in-flight turn (running) OR an optimistic just-sent state
    // (pending) before the first event lands. The view's activity indicator
    // binds to this so something shows the instant the user sends, even while a
    // slow streaming model is still thinking before its first token.
    get working() {
      return running || pending;
    },
    // markPending is called by the view the moment it dispatches a send, so the
    // indicator appears immediately. Cleared by the first turn event or `done`.
    markPending() {
      pending = true;
    },
    clearPending() {
      pending = false;
    },
    get truncated() {
      return truncated;
    },
    // Read by the view to trigger a State() refetch when a gated approval lands.
    get approvalSeq() {
      return approvalSeq;
    },
    // Latest token count streamed during the in-flight turn (0 when idle / no
    // streamed count yet). Lets the dock context ring update live instead of
    // freezing until refreshState(). 0 means "fall back to sess.tokens".
    get liveTokens() {
      return liveTokens;
    },
    // The current task plan: the latest `todo` tool block's task list (the tool
    // replaces the whole list each call, so newest wins). [] when the model
    // hasn't recorded a plan this session. Drives the chat view's live plan
    // panel. All-done/empty is left to the view to hide.
    get todos(): TodoEntry[] {
      for (let i = history.length - 1; i >= 0; i--) {
        const b = history[i];
        if (b.kind === "tool" && b.name === "todo" && b.args) {
          return parseTodos(b.args);
        }
      }
      return [];
    },
    // seed history from a Bridge.State snapshot (newest CAP messages).
    // Called on first mount AND on every daemon reconnect (the view re-attaches
    // and the daemon replays its event buffer through the listener). The replay
    // rebuilds the live block from deltas, so the in-flight turn must start from
    // a clean slate: drop any half-streamed `live` block, the pending coalescer,
    // and a queued flush — otherwise the orphaned live text + the seeded history
    // (which already contains it) + the replay all show the same reply twice.
    seed(messages: MessageDTO[], isRunning: boolean) {
      live = null;
      resetPending();
      // A reconnect/attach starts the in-flight turn from a clean slate (the
      // replay rebuilds it), so the streamed-this-turn guard (GUI-092) must not
      // carry over from before — otherwise a replayed single-`done` answer could
      // be wrongly suppressed by a stale flag.
      streamedThisTurn = false;
      if (rafId) {
        cancelAnimationFrame(rafId);
        rafId = 0;
      }
      history = mapMessages(messages, nextUid).slice(-CAP);
      truncated = messages.length > CAP;
      running = isRunning;
      pending = false; // seed is authoritative — drop any optimistic flag
      // Reset the reassembly window to "unlatched" (0): the next event seen —
      // whatever seq the live pump is currently at — re-latches the base. (A
      // reconnect MAY restart the pump's seq, but a re-attach to a still-live
      // pump does NOT, so we must not assume 1.)
      expectedSeq = 0;
      reorderBuf.clear();
    },
    // start the live event listener; lifetime == owning $effect.
    start() {
      off = on<StreamEventDTO>(ev.sessionEvent(sessionId), (m) => {
        if (disposed) return;
        // Reassemble by seq: Wails can reorder events across its per-event
        // dispatch goroutines. Park anything ahead of expectedSeq and drain in
        // order. (A 0/absent seq — shouldn't happen from the pump — applies
        // immediately so a contract gap degrades to today's arrival-order behavior
        // rather than stalling.)
        if (!m.seq) {
          applyEvent(m);
          return;
        }
        // Latch the window base to the first seq we actually see (the live pump
        // may already be mid-stream after a re-attach), so we never wait forever
        // for a seq=1 that won't come again. On attach the pump emits the REPLAY
        // buffer first, in seq order, so prefer a replay event as the base: if a
        // live (non-replay) event races ahead while a replay is still expected
        // (running), buffer it instead of latching to its higher seq — otherwise
        // one early live straggler would push the base past the replay and make
        // every replayed event apply out of order via the behind-window path.
        // The buffered event drains in order once the base latches (or via the
        // overflow valve if no replay ever comes).
        if (expectedSeq === 0) {
          if (running && !m.replay) {
            reorderBuf.set(m.seq, m);
            if (reorderBuf.size > 256) drainBufInOrder();
            return;
          }
          expectedSeq = m.seq;
        }
        // A late event whose seq is already behind the window (e.g. a straggler
        // from before a re-latch) applies immediately rather than being dropped.
        if (m.seq < expectedSeq) {
          applyEvent(m);
          return;
        }
        reorderBuf.set(m.seq, m);
        while (reorderBuf.has(expectedSeq)) {
          const next = reorderBuf.get(expectedSeq)!;
          reorderBuf.delete(expectedSeq);
          expectedSeq++;
          applyEvent(next);
        }
        // Guard against an unbounded buffer if a seq is ever permanently missing
        // (a dropped event, or no replay arrived to latch the base): give up
        // waiting and flush what we have in seq order, resyncing past the gap.
        if (reorderBuf.size > 256) drainBufInOrder();
      });
    },
    dispose() {
      disposed = true;
      if (off) {
        off();
        off = null;
      }
      if (rafId) cancelAnimationFrame(rafId);
      rafId = 0;
      resetPending();
      history = [];
      live = null;
    },
  };
}

// mapMessages turns the daemon's full llm.Message history into renderable
// blocks: assistant tool calls + their results collapse into ToolBlocks. uid
// is assigned from the supplied generator so seeded blocks share the store's
// monotonic identity space with streamed ones.
//
// Two perf guards (GUI-018) for large sessions:
//   - matching a `tool`-role result back to its call uses a Map<toolId,block>
//     built during the pass, not an out.find() linear scan (O(N) not O(N^2));
//   - the daemon returns the FULL uncapped transcript but the view only keeps
//     the last CAP blocks, so we map at most the last CAP*2 messages — enough
//     to comfortably fill the window even when every message is a single block
//     (the seed slices to CAP afterward; truncated stays driven by raw length).
//
// Correctness (GUI-062): the Map mirrors the live `tool_result` path — it
// resolves a result to the MOST RECENT undone tool call with the id (last-wins:
// every new call with the same id overwrites the Map entry), and it NEVER keys
// on an empty id. Foreign/importer histories (Codex/Claude) and providers that
// blank ids would otherwise collapse `toolCallId ?? ''` → '' and attach every
// id-less result to the first id-less tool block. An empty-id result instead
// becomes a standalone result block, the same as the live path treats an
// unmatched id.
function mapMessages(messages: MessageDTO[], uid: () => number): Block[] {
  const window = messages.length > CAP * 2 ? messages.slice(-CAP * 2) : messages;
  const out: Block[] = [];
  const byToolId = new Map<string, ToolBlock>();
  let step = 0;
  for (const m of window) {
    if (m.role === "tool") {
      const id = m.toolCallId ?? "";
      // Empty id can't be matched (it would collide with every other id-less
      // call), and an unmatched id has no call to fold into — both surface as a
      // standalone, already-done result block.
      const existing = id ? byToolId.get(id) : undefined;
      if (existing) {
        existing.result = m.text;
        existing.isError = m.toolError;
        existing.done = true;
        // The call has its result; a later same-id call should match on its own
        // block, not steal this one's result.
        byToolId.delete(id);
      } else {
        out.push({ uid: uid(), kind: "tool", id, name: m.toolName ?? "tool", args: "", result: m.text, isError: m.toolError, done: true });
      }
      continue;
    }
    if (m.reasoning) out.push({ uid: uid(), kind: "reasoning", step, text: m.reasoning });
    if (m.text) out.push({ uid: uid(), kind: "text", step, text: m.text });
    for (const tc of m.toolCalls ?? []) {
      const block: ToolBlock = { uid: uid(), kind: "tool", id: tc.id, name: tc.name, args: tc.args, done: false };
      // Last-wins: an id-less call is never matchable (no result can find it),
      // so it stays a standalone pending block and is kept out of the Map.
      if (tc.id) byToolId.set(tc.id, block);
      out.push(block);
    }
    step++;
  }
  return out;
}
