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

  function pushHistory(b: Block) {
    history.push(b);
    if (history.length > CAP) {
      history.splice(0, history.length - CAP);
      truncated = true;
    }
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

  function onEvent(e: WireEventDTO) {
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
        for (let i = history.length - 1; i >= 0; i--) {
          const b = history[i];
          if (b.kind === "tool" && b.id === e.toolId) {
            history[i] = { ...b, result: e.result, isError: e.isError, done: true };
            break;
          }
        }
        break;
      }
      case "done":
        flush();
        commitLive();
        resetPending();
        running = false;
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
        // running here. In-turn informational notes (e.g. compaction) are always
        // followed by a `done` that also clears running, so this is safe.
        running = false;
        break;
      case "approval":
        // A gated tool is waiting for the user. The turn stays running, so the
        // view can't rely on running→false to refetch pending approvals; bump a
        // signal it watches instead.
        approvalSeq++;
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
      if (rafId) {
        cancelAnimationFrame(rafId);
        rafId = 0;
      }
      history = mapMessages(messages, nextUid).slice(-CAP);
      truncated = messages.length > CAP;
      running = isRunning;
    },
    // start the live event listener; lifetime == owning $effect.
    start() {
      off = on<StreamEventDTO>(ev.sessionEvent(sessionId), (m) => {
        if (disposed) return;
        // Only kinds that imply an in-flight TURN may flip an idle session to
        // "running". Wake/lifecycle kinds (`bg_done`, `note`, `done`, unknown)
        // must NOT: e.g. the daemon's `bg_done` background-task wake would
        // otherwise resurrect an IDLE session. Replayed buffer events never flip
        // it either (`running` is owned by the State() seed). `done` always
        // clears it (handled in onEvent).
        if (!m.replay && TURN_KINDS.has(m.event.kind)) running = true;
        onEvent(m.event);
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
function mapMessages(messages: MessageDTO[], uid: () => number): Block[] {
  const out: Block[] = [];
  let step = 0;
  for (const m of messages) {
    if (m.role === "tool") {
      const id = m.toolCallId ?? "";
      const existing = out.find((b) => b.kind === "tool" && b.id === id) as ToolBlock | undefined;
      if (existing) {
        existing.result = m.text;
        existing.isError = m.toolError;
        existing.done = true;
      } else {
        out.push({ uid: uid(), kind: "tool", id, name: m.toolName ?? "tool", args: "", result: m.text, isError: m.toolError, done: true });
      }
      continue;
    }
    if (m.reasoning) out.push({ uid: uid(), kind: "reasoning", step, text: m.reasoning });
    if (m.text) out.push({ uid: uid(), kind: "text", step, text: m.text });
    for (const tc of m.toolCalls ?? []) {
      out.push({ uid: uid(), kind: "tool", id: tc.id, name: tc.name, args: tc.args, done: false });
    }
    step++;
  }
  return out;
}
