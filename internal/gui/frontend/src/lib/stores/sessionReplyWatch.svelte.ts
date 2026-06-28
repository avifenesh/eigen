// Detects sessions that finished a turn (working/approval → idle) while the user
// is not on that chat, marks them unread, and fires desktop + in-app notify.

import { Bridge } from "$lib/bridge";
import { router } from "$lib/router.svelte";
import { sessionUnread } from "$lib/stores/sessionUnread.svelte";
import { toasts } from "$lib/stores/toasts.svelte";
import type { SessionInfoDTO } from "$lib/types";

const ACTIVE = new Set(["working", "approval"]);

function shortTitle(s: SessionInfoDTO): string {
  const t = (s.title ?? "").trim();
  if (t) return t;
  const d = (s.dir ?? "").replace(/\/+$/, "");
  return d.slice(d.lastIndexOf("/") + 1) || "Chat";
}

function viewingChatId(): string | undefined {
  return router.route === "chat" ? router.param : undefined;
}

const prevStatus = new Map<string, string>();

/** Call after each successful Sessions() list refresh. */
export function onSessionsListUpdated(list: SessionInfoDTO[]) {
  sessionUnread.reconcileIds(list.map((s) => s.id));

  const viewId = viewingChatId();
  for (const s of list) {
    const prev = prevStatus.get(s.id);
    prevStatus.set(s.id, s.status);
    if (!prev || !ACTIVE.has(prev) || s.status !== "idle") continue;
    if (viewId === s.id) continue;

    sessionUnread.markUnread(s.id);
    const title = shortTitle(s);
    void Bridge.NotifyChatReply(s.id, title).catch(() => {});
    toasts.info(`New reply · ${title}`);
  }
}