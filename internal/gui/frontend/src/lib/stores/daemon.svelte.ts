// Daemon connection + stats store. The Go health loop pushes a stats snapshot
// at ~1Hz (and a health event when offline); this store reflects connection
// status and exposes the latest DaemonStats. start() returns a teardown fn —
// call it from the App root's onMount return.
import { on, ev } from "$lib/events";
import { Bridge } from "$lib/bridge";
import type { DaemonStats } from "$lib/types";

type Status = "connecting" | "online" | "offline";

function createDaemon() {
  let status = $state<Status>("connecting");
  let stats = $state<DaemonStats | null>(null);
  const reconnectCbs = new Set<() => void>();

  function start(): () => void {
    const offStats = on<DaemonStats>(ev.daemonStats, (data) => {
      stats = data;
      if (status !== "online") {
        status = "online";
        reconnectCbs.forEach((f) => f());
      }
    });
    const offHealth = on<{ ok: boolean }>(ev.daemonHealth, (data) => {
      if (!data.ok && status !== "offline") status = "offline";
    });
    Bridge.Ping()
      .then(() => {
        if (status === "connecting") status = "online";
      })
      .catch(() => {
        status = "offline";
      });
    return () => {
      offStats();
      offHealth();
    };
  }

  return {
    get status() {
      return status;
    },
    get stats() {
      return stats;
    },
    start,
    // Register a callback fired whenever the daemon transitions back to online.
    // MUST be called inside an $effect with the returned remover in its cleanup.
    onReconnect(f: () => void): () => void {
      reconnectCbs.add(f);
      if (reconnectCbs.size > 32) {
        console.warn("[daemon] reconnect callback set unexpectedly large:", reconnectCbs.size);
      }
      return () => reconnectCbs.delete(f);
    },
  };
}

export const daemon = createDaemon();
