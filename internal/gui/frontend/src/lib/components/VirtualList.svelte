<script lang="ts" generics="T">
  // A windowed list: renders only the rows in (and just around) the viewport,
  // so a 10k-item list keeps ~visible+overscan nodes in the DOM. Estimated row
  // height drives the scroll geometry; actual heights are measured and fed back
  // so variable-height rows settle correctly. Pin-to-bottom optional (chat).
  import type { Snippet } from "svelte";
  import { SvelteMap } from "svelte/reactivity";

  let {
    items,
    estimateHeight = 64,
    overscan = 6,
    gap = 0,
    pin = false,
    /** Bump when opening a session (or similar) to jump to the latest message. */
    scrollToken = "",
    listRef = $bindable<HTMLDivElement | undefined>(undefined),
    row,
    key,
  }: {
    items: T[];
    estimateHeight?: number;
    overscan?: number;
    gap?: number;
    // pin: keep the viewport stuck to the bottom as items grow, UNLESS the user
    // has scrolled up (chat transcript behavior). Re-pins when scrolled back down.
    pin?: boolean;
    scrollToken?: string | number;
    listRef?: HTMLDivElement | undefined;
    row: Snippet<[T, number]>;
    key?: (item: T, index: number) => string | number;
  } = $props();

  let viewport = $state<HTMLDivElement | undefined>(undefined);
  let scrollTop = $state(0);
  let viewportH = $state(0);
  let pinned = $state(true);
  let pinRaf = 0;
  // rAF coalescing for scroll reads, and accumulated scroll-anchor compensation
  // (see measure()). Both are cancelled on teardown.
  let scrollRaf = 0;
  let anchorDelta = 0;
  let anchorRaf = 0;
  // True while the user is actively scrolling — skip pin-to-bottom and defer
  // scroll-anchor compensation until idle so wheel/trackpad stays on the compositor.
  let userScrolling = false;
  let scrollEndTimer = 0;
  // Row height updates deferred while the user scrolls (GUI-001): applying them
  // immediately rebuilds offsets + window on every ResizeObserver tick.
  const pendingMeasures = new SvelteMap<string | number, number>();
  let measureFlushRaf = 0;

  function keyOf(item: T, index: number): string | number {
    return key ? key(item, index) : index;
  }

  // key → index, so a row that remeasures can tell whether it sits above the
  // current scroll position (and therefore needs anchor compensation).
  const indexByKey = $derived.by(() => {
    const m = new Map<string | number, number>();
    for (let i = 0; i < items.length; i++) m.set(keyOf(items[i], i), i);
    return m;
  });

  // Measured heights keyed by item identity (NOT array index), so a splice or
  // reorder of `items` (e.g. the transcript CAP eviction) keeps each row's real
  // height instead of mis-attributing a neighbour's. Falls back to the estimate
  // until a row reports. Pruned to live keys so evicted rows don't accumulate.
  // SvelteMap (not a plain Map): a plain Map's .set()/.delete() do NOT notify
  // Svelte 5 reactivity, so `offsets` would never recompute when a row reports
  // its real height — leaving rows at their estimate positions and making an
  // expanding tool card overlap the next block (the "opened behind / covered"
  // bug) and collapsed rows leave stale gaps. SvelteMap makes mutations reactive
  // so the offset geometry follows actual measured heights.
  const measured = new SvelteMap<string | number, number>();

  function heightAt(i: number): number {
    const h = measured.get(keyOf(items[i], i));
    return (h || estimateHeight) + gap;
  }

  // Cumulative offsets — recomputed when items or measurements change. For the
  // list sizes this app shows (hundreds–low thousands) a linear scan is cheap.
  const offsets = $derived.by(() => {
    const n = items.length;
    const arr = new Array<number>(n + 1);
    arr[0] = 0;
    for (let i = 0; i < n; i++) arr[i + 1] = arr[i] + heightAt(i);
    return arr;
  });
  const totalH = $derived(offsets[items.length] ?? 0);

  // Binary-search the first visible index for the current scrollTop.
  function firstVisible(top: number): number {
    let lo = 0;
    let hi = items.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (offsets[mid + 1] <= top) lo = mid + 1;
      else hi = mid;
    }
    return lo;
  }

  const start = $derived(Math.max(0, firstVisible(scrollTop) - overscan));
  const end = $derived.by(() => {
    let i = firstVisible(scrollTop + viewportH);
    return Math.min(items.length, i + overscan + 1);
  });

  const windowItems = $derived.by(() => {
    const out: { item: T; index: number; top: number }[] = [];
    for (let i = start; i < end; i++) out.push({ item: items[i], index: i, top: offsets[i] });
    return out;
  });

  // Coalesce scroll events to one read per frame. A fast wheel/trackpad fires
  // scroll far more than 60Hz; doing the offset binary-search + window rebuild
  // on every event is what made scrolling (especially up, where rows also
  // remeasure) feel slow. One rAF-batched read per frame keeps it smooth.
  function onScroll() {
    if (!viewport || scrollRaf) return;
    scrollRaf = requestAnimationFrame(() => {
      scrollRaf = 0;
      if (!viewport) return;
      scrollTop = viewport.scrollTop;
      userScrolling = true;
      clearTimeout(scrollEndTimer);
      scrollEndTimer = window.setTimeout(() => {
        userScrolling = false;
        flushPendingMeasures();
        if (anchorDelta !== 0 && viewport) {
          viewport.scrollTop += anchorDelta;
          scrollTop = viewport.scrollTop;
          anchorDelta = 0;
        }
      }, 120);
      if (pin) {
        const gap = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight;
        pinned = gap < 48;
      }
    });
  }

  function scrollToBottom() {
    if (viewport) viewport.scrollTop = viewport.scrollHeight;
  }

  function flushPendingMeasures() {
    if (pendingMeasures.size === 0) return;
    for (const [k, h] of pendingMeasures) {
      if (h > 0) measured.set(k, h);
    }
    pendingMeasures.clear();
    cancelAnimationFrame(measureFlushRaf);
    measureFlushRaf = 0;
  }

  function scheduleMeasureFlush() {
    if (measureFlushRaf) return;
    measureFlushRaf = requestAnimationFrame(() => {
      measureFlushRaf = 0;
      if (!userScrolling) flushPendingMeasures();
    });
  }

  // ResizeObserver keeps the viewport height current without layout polling.
  $effect(() => {
    listRef = viewport;
  });

  // Opening a session (or history landing after load): start at the bottom and
  // follow new messages until the user scrolls up.
  $effect(() => {
    void scrollToken;
    pinned = true;
    cancelAnimationFrame(pinRaf);
    requestAnimationFrame(() => {
      requestAnimationFrame(scrollToBottom);
    });
  });

  $effect(() => {
    if (!viewport) return;
    viewportH = viewport.clientHeight;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) viewportH = e.contentRect.height;
    });
    ro.observe(viewport);
    return () => {
      ro.disconnect();
      cancelAnimationFrame(scrollRaf);
      cancelAnimationFrame(anchorRaf);
      cancelAnimationFrame(measureFlushRaf);
      clearTimeout(scrollEndTimer);
      scrollRaf = 0;
      anchorRaf = 0;
      measureFlushRaf = 0;
      scrollEndTimer = 0;
      pendingMeasures.clear();
    };
  });

  // Prune measured heights for keys no longer present, so an evicted/replaced
  // list doesn't grow the map unbounded.
  $effect(() => {
    if (measured.size <= items.length) return;
    const live = new Set(items.map((it, i) => keyOf(it, i)));
    for (const k of measured.keys()) {
      if (!live.has(k)) measured.delete(k);
    }
  });

  // Pin-to-bottom: when content grows (or a row remeasures taller) and the user
  // hasn't scrolled up, stay glued to the latest. transform/opacity-free — a
  // direct scrollTop write on the next frame, so no layout thrash mid-stream.
  // The rAF handle is cancelled on cleanup so a frame queued just before unmount
  // never fires against a torn-down viewport.
  $effect(() => {
    void totalH; // re-run as content grows
    if (pin && pinned && !userScrolling) {
      cancelAnimationFrame(pinRaf);
      pinRaf = requestAnimationFrame(scrollToBottom);
    }
    return () => cancelAnimationFrame(pinRaf);
  });

  // Measure a rendered row and store its real height keyed by item identity
  // (drives the next layout pass). A reactive param keeps the key current if a
  // row's identity changes in place.
  function measure(node: HTMLElement, k: string | number) {
    let curKey = k;
    const apply = () => {
      const h = node.offsetHeight;
      const prev = measured.get(curKey);
      if (h > 0 && prev !== h) {
        // Scroll-anchor compensation: if this row sits ABOVE the current scroll
        // position, its height change shifts every row below it — including the
        // viewport content — making the list jump (the "scroll up is slow/janky"
        // symptom, where off-screen rows above first get their real height). Add
        // the delta to scrollTop so what the user is looking at stays put.
        //
        // Baseline the delta against estimateHeight on FIRST measure (prev
        // undefined): an unmeasured above-viewport row was occupying
        // estimateHeight in the offset geometry, so resolving it to its real
        // height shifts the rows below by h - estimateHeight. The old guard
        // (prev !== undefined) skipped exactly this case, so scrolling UP into
        // never-measured rows jumped uncompensated — the "up is worse" feel.
        const idx = indexByKey.get(curKey);
        const base = prev ?? estimateHeight;
        if (idx !== undefined && offsets[idx] < scrollTop && h !== base) {
          anchorDelta += h - base;
          if (!userScrolling && !anchorRaf) {
            anchorRaf = requestAnimationFrame(() => {
              anchorRaf = 0;
              if (viewport && anchorDelta !== 0) {
                viewport.scrollTop += anchorDelta;
                scrollTop = viewport.scrollTop;
              }
              anchorDelta = 0;
            });
          }
        }
        if (userScrolling) {
          pendingMeasures.set(curKey, h);
          scheduleMeasureFlush();
        } else {
          measured.set(curKey, h);
        }
      }
    };
    apply();
    const ro = new ResizeObserver(apply);
    ro.observe(node);
    return {
      update(nk: string | number) {
        curKey = nk;
        apply();
      },
      destroy: () => ro.disconnect(),
    };
  }
</script>

<div class="vlist" data-native-scroll="1" bind:this={viewport} onscroll={onScroll}>
  <div class="vlist__sizer" style="height:{totalH}px">
    {#each windowItems as w (keyOf(w.item, w.index))}
      <div class="vlist__row" style="transform:translateY({w.top}px)" use:measure={keyOf(w.item, w.index)}>
        {@render row(w.item, w.index)}
      </div>
    {/each}
  </div>
</div>

<style>
  .vlist {
    height: 100%;
    overflow-y: auto;
    position: relative;
    /* Hint WebKitGTK to keep scrolling on the fast path (no custom ::-webkit-scrollbar). */
    overflow-anchor: auto;
  }
  .vlist__sizer {
    position: relative;
    width: 100%;
  }
  .vlist__row {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    will-change: transform;
  }
</style>
