// Sessions store — the live session list backing the Home board and rail badge.
// Refreshed on demand and whenever the daemon reconnects. Cheap: one List RPC.
import { Bridge } from "$lib/bridge";
import type { SessionInfoDTO } from "$lib/types";

function createSessions() {
  let list = $state<SessionInfoDTO[]>([]);
  let loading = $state(false);
  let error = $state<string | null>(null);

  async function refresh() {
    loading = true;
    try {
      list = await Bridge.Sessions();
      error = null;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
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
    get count() {
      return list.length;
    },
    refresh,
  };
}

export const sessions = createSessions();
