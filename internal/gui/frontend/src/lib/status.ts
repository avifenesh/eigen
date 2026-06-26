// Shared status→StatusDot mapping. Daemon SESSION statuses are
// idle | working | approval | error (internal/daemon/session.go); BgTask
// statuses are running | done | error | canceled | lost. Centralized here so a
// view never invents its own (mis)mapping — the source of the Home "running"
// bug the whole-app validation caught.
export type DotState = "working" | "idle" | "ok" | "warn" | "error";

// Session status (daemon SessionInfo.Status) → dot.
export function sessionDot(status: string): DotState {
  switch (status) {
    case "working":
      return "working";
    case "approval":
      return "warn";
    case "error":
      return "error";
    default: // idle (or unknown)
      return "idle";
  }
}

// Background-task status (agent.BgTask.Status) → dot.
export function taskDot(status: string): DotState {
  switch (status) {
    case "running":
      return "working";
    case "done":
      return "ok";
    case "error":
    case "lost":
      return "error";
    case "canceled":
      return "warn";
    default:
      return "idle";
  }
}

// Relative "x ago" label from a unix-nano timestamp (SessionInfo.updated is
// unix nano). Centralized so consumers stop hand-rolling the /1e6 conversion —
// a single missed division is a 10^6x drift. Views that want a live-ticking
// label still read their shared clock (e.g. `void now.ms`) before calling.
export function relTime(nano: number): string {
  const ms = Date.now() - nano / 1e6;
  const m = Math.floor(ms / 60000);
  if (m < 1) return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}
