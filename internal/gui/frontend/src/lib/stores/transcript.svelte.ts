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

export type TextBlock = { kind: "text" | "reasoning"; step: number; text: string };
export type ToolBlock = {
  kind: "tool";
  id: string;
  name: string;
  args: string;
  result?: string;
  isError?: boolean;
  done: boolean;
};
export type NoteBlock = { kind: "note"; text: string };
export type Block = TextBlock | ToolBlock | NoteBlock;

const CAP = 2000;

export type Transcript = ReturnType<typeof createTranscript>;

export function createTranscript(sessionId: string) {
  let history = $state<Block[]>([]);
  let live = $state<TextBlock | null>(null);
  let running = $state(false);
  let truncated = $state(false);

  let pendingText = "";
  let pendingKind: "text" | "reasoning" = "text";
  let pendingStep = -1;
  let rafId = 0;
  let disposed = false;
  let off: (() => void) | null = null;

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
      live = { kind: pendingKind, step: pendingStep, text: pendingText };
    } else {
      live = { ...live, text: live.text + pendingText };
    }
    pendingText = "";
  }

  function onEvent(e: WireEventDTO) {
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
        pushHistory({ kind: "tool", id: e.toolId ?? "", name: e.tool ?? "", args: e.toolArgs ?? "", done: false });
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
        break;
      case "note":
        flush();
        commitLive();
        resetPending();
        pushHistory({ kind: "note", text: e.text ?? "" });
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
    // seed history from a Bridge.State snapshot (newest CAP messages).
    seed(messages: MessageDTO[], isRunning: boolean) {
      history = mapMessages(messages).slice(-CAP);
      truncated = messages.length > CAP;
      running = isRunning;
    },
    // start the live event listener; lifetime == owning $effect.
    start() {
      off = on<StreamEventDTO>(ev.sessionEvent(sessionId), (m) => {
        if (disposed) return;
        running = true;
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
// blocks: assistant tool calls + their results collapse into ToolBlocks.
function mapMessages(messages: MessageDTO[]): Block[] {
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
        out.push({ kind: "tool", id, name: m.toolName ?? "tool", args: "", result: m.text, isError: m.toolError, done: true });
      }
      continue;
    }
    if (m.reasoning) out.push({ kind: "reasoning", step, text: m.reasoning });
    if (m.text) out.push({ kind: "text", step, text: m.text });
    for (const tc of m.toolCalls ?? []) {
      out.push({ kind: "tool", id: tc.id, name: tc.name, args: tc.args, done: false });
    }
    step++;
  }
  return out;
}
