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
  // The version of THIS gui binary, fetched once on start. Compared against
  // stats.version (the daemon's) so the chrome can flag a daemon/gui mismatch
  // (daemon started from an older build than the running GUI).
  let guiVersion = $state("");
  const reconnectCbs = new Set<() => void>();

  function start(): () => void {
    Bridge.GUIVersion()
      .then((v) => (guiVersion = v))
      .catch(() => {});
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
    // Bootstrap ping only resolves the INITIAL connecting state. A stats event
    // can arrive (→ online) before this settles; guard both arms so the
    // live-driven status is never clobbered by the one-shot bootstrap.
    Bridge.Ping()
      .then(() => {
        if (status === "connecting") status = "online";
      })
      .catch(() => {
        if (status === "connecting") status = "offline";
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
    // This gui binary's own version.
    get guiVersion() {
      return guiVersion;
    },
    // The daemon's reported version (empty until first stats event).
    get daemonVersion() {
      return stats?.version ?? "";
    },
    // True when both versions are known and differ — surfaced as a warning in
    // the chrome so a stale daemon vs fresh GUI (or vice-versa) is visible.
    get versionMismatch() {
      return !!guiVersion && !!stats?.version && guiVersion !== stats.version;
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
