// Stale-while-revalidate cache for view-level bridge fetches. The router's
// {#if} chain (App.svelte) fully unmounts/remounts a view on every nav switch
// — by design, see App.svelte's comment — so without this, revisiting Crons/
// Skills/Memory/Connectors/Plugins re-runs their full fetch from zero every
// single click, even if nothing changed since the last visit.
//
// fetch(key, fn) always calls fn() to get the true current state (this is a
// refresh cache, not a TTL cache that can go stale-forever) — the savings are
// twofold: (1) get(key) lets a view paint the previous result INSTANTLY while
// the refresh is in flight, instead of blanking to a skeleton every visit, and
// (2) concurrent calls for the same key (e.g. a Rail hover-prefetch racing the
// click-triggered load) join the same in-flight promise instead of double-
// firing the underlying work — material for Crons, whose fetch fans out into
// 2N+1 systemctl subprocess spawns.
type Entry = { data: unknown; ts: number; promise?: Promise<unknown> };

function createViewCache() {
  const entries = new Map<string, Entry>();

  function get<T>(key: string): T | undefined {
    return entries.get(key)?.data as T | undefined;
  }

  // minAge: skip the refetch (serve the cached value as-is) if the entry is
  // younger than this. Default 0 — always refetch — so an explicit view-mount
  // load always reflects true current state; only the hover-prefetch path
  // (Rail) passes a minAge, to avoid re-spawning work on every hover flicker.
  function fetchKey<T>(key: string, fn: () => Promise<T>, opts?: { minAge?: number }): Promise<T> {
    const e = entries.get(key);
    if (e?.promise) return e.promise as Promise<T>;
    if (e && opts?.minAge && Date.now() - e.ts < opts.minAge) {
      return Promise.resolve(e.data as T);
    }
    const p = fn()
      .then((data) => {
        entries.set(key, { data, ts: Date.now() });
        return data;
      })
      .catch((err) => {
        // Drop the in-flight marker but keep any prior stale data — a failed
        // refresh shouldn't strand the cache pointing at a dead promise.
        const cur = entries.get(key);
        if (cur) cur.promise = undefined;
        throw err;
      });
    entries.set(key, { data: e?.data, ts: e?.ts ?? 0, promise: p });
    return p;
  }

  function invalidate(key: string) {
    entries.delete(key);
  }

  return { get, fetch: fetchKey, invalidate };
}

export const viewCache = createViewCache();
