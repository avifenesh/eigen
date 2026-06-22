<script lang="ts" generics="T">
  // A windowed list: renders only the rows in (and just around) the viewport,
  // so a 10k-item list keeps ~visible+overscan nodes in the DOM. Estimated row
  // height drives the scroll geometry; actual heights are measured and fed back
  // so variable-height rows settle correctly. Pin-to-bottom optional (chat).
  import type { Snippet } from "svelte";

  let {
    items,
    estimateHeight = 64,
    overscan = 6,
    gap = 0,
    pin = false,
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
    row: Snippet<[T, number]>;
    key?: (item: T, index: number) => string | number;
  } = $props();

  let viewport = $state<HTMLDivElement | undefined>(undefined);
  let scrollTop = $state(0);
  let viewportH = $state(0);
  let pinned = $state(true);
  let pinRaf = 0;

  function keyOf(item: T, index: number): string | number {
    return key ? key(item, index) : index;
  }

  // Measured heights keyed by item identity (NOT array index), so a splice or
  // reorder of `items` (e.g. the transcript CAP eviction) keeps each row's real
  // height instead of mis-attributing a neighbour's. Falls back to the estimate
  // until a row reports. Pruned to live keys so evicted rows don't accumulate.
  let measured = $state<Map<string | number, number>>(new Map());

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

  function onScroll() {
    if (!viewport) return;
    scrollTop = viewport.scrollTop;
    if (pin) {
      const gap = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight;
      pinned = gap < 48;
    }
  }

  function scrollToBottom() {
    if (viewport) viewport.scrollTop = viewport.scrollHeight;
  }

  // ResizeObserver keeps the viewport height current without layout polling.
  $effect(() => {
    if (!viewport) return;
    viewportH = viewport.clientHeight;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) viewportH = e.contentRect.height;
    });
    ro.observe(viewport);
    return () => ro.disconnect();
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
    if (pin && pinned) {
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
      if (h > 0 && measured.get(curKey) !== h) measured.set(curKey, h);
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

<div class="vlist" bind:this={viewport} onscroll={onScroll}>
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
