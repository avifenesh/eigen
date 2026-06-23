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
