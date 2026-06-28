// Sessions with a finished turn the user has not opened since. Persisted in
// localStorage so unread survives reload. Cleared when the user opens that chat.

const KEY = "eigen.sessionUnread";

function load(): Set<string> {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return new Set();
    const arr = JSON.parse(raw) as unknown;
    if (!Array.isArray(arr)) return new Set();
    return new Set(arr.filter((x): x is string => typeof x === "string" && x.length > 0));
  } catch {
    return new Set();
  }
}

function save(ids: Set<string>) {
  try {
    localStorage.setItem(KEY, JSON.stringify([...ids]));
  } catch {
    /* ignore */
  }
}

function createSessionUnread() {
  let ids = $state<Set<string>>(load());

  function sync(next: Set<string>) {
    ids = next;
    save(next);
  }

  return {
    get ids() {
      return ids;
    },
    get count() {
      return ids.size;
    },
    isUnread(id: string): boolean {
      return ids.has(id);
    },
    markUnread(id: string) {
      if (!id || ids.has(id)) return;
      const next = new Set(ids);
      next.add(id);
      sync(next);
    },
    markRead(id: string) {
      if (!id || !ids.has(id)) return;
      const next = new Set(ids);
      next.delete(id);
      sync(next);
    },
    clear() {
      sync(new Set());
    },
    /** Drop unread markers for sessions that no longer exist. */
    reconcileIds(liveIds: string[]) {
      const live = new Set(liveIds);
      let changed = false;
      const next = new Set(ids);
      for (const id of next) {
        if (!live.has(id)) {
          next.delete(id);
          changed = true;
        }
      }
      if (changed) sync(next);
    },
  };
}

export const sessionUnread = createSessionUnread();