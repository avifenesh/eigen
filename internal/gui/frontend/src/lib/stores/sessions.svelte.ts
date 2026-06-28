// Sessions store — the live session list backing the Home board and rail badge.
// Refreshed on demand and whenever the daemon reconnects. Cheap: one List RPC.
import { Bridge } from "$lib/bridge";
import { errText } from "$lib/errors";
import { onSessionsListUpdated } from "$lib/stores/sessionReplyWatch.svelte";
import type { SessionInfoDTO } from "$lib/types";

function createSessions() {
  let list = $state<SessionInfoDTO[]>([]);
  let loading = $state(false);
  let error = $state<string | null>(null);
  // True once a refresh has successfully completed at least once — lets views
  // distinguish "no sessions" from "not loaded yet" (e.g. Chat deciding whether
  // a routed session id is genuinely gone vs. simply not fetched).
  let loaded = $state(false);

  // Monotonic guard: many callers refresh concurrently (Home $effect, Live's
  // 2s poll, onReconnect, per-action refreshes). Without it an older snapshot
  // can resolve last and clobber a newer one. Only the latest call commits.
  let loadSeq = 0;
  async function refresh() {
    const seq = ++loadSeq;
    loading = true;
    try {
      const next = await Bridge.Sessions();
      if (seq !== loadSeq) return; // a newer refresh superseded this one
      list = next;
      onSessionsListUpdated(next);
      error = null;
      loaded = true;
    } catch (e) {
      if (seq !== loadSeq) return;
      error = errText(e);
    } finally {
      if (seq === loadSeq) loading = false;
    }
  }

  return {
    get list() {
      return list;
    },
    get loading() {
      return loading;
    },
    get error() {
      return error;
    },
    get loaded() {
      return loaded;
    },
    get count() {
      return list.length;
    },
    refresh,
  };
}

export const sessions = createSessions();
