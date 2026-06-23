// Proactive-feed store. The daemon-side feedLoop pushes fresh scans via the
// eigen:feed event; this store reflects them and also seeds from the instant
// cache on start. Items split into "act on" (state-derived: git/github/memory)
// and "ideas" (LLM suggestions, kind=suggest) so Home can lane them separately.
import { on, ev } from "$lib/events";
import { Bridge } from "$lib/bridge";
import type { FeedDTO, FeedItemDTO } from "$lib/types";

function createFeed() {
  let items = $state<FeedItemDTO[]>([]);
  let fresh = $state(false);
  let scannedMs = $state(0);

  function apply(f: FeedDTO | null) {
    if (!f) return;
    items = f.items ?? [];
    fresh = f.fresh;
    scannedMs = f.scannedMs;
  }

  // start: seed from the cache, then ride the eigen:feed push stream. Returns a
  // teardown that removes the listener.
  function start(): () => void {
    Bridge.Feed().then(apply).catch(() => {});
    return on<FeedDTO>(ev.feed, apply);
  }

  async function dismiss(key: string) {
    // optimistic: drop locally; the daemon re-emits the freshened feed too.
    items = items.filter((i) => i.key !== key);
    try {
      await Bridge.DismissFeed(key);
    } catch {
      // a failed dismiss will reappear on the next scan — acceptable.
    }
  }

  return {
    get items() {
      return items;
    },
    get fresh() {
      return fresh;
    },
    get scannedMs() {
      return scannedMs;
    },
    // state-derived loose ends (act on now)
    get actOn() {
      return items.filter((i) => i.kind !== "suggest");
    },
    // forward-looking LLM ideas
    get ideas() {
      return items.filter((i) => i.kind === "suggest");
    },
    get count() {
      return items.length;
    },
    start,
    dismiss,
  };
}

export const feed = createFeed();
