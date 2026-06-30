<script lang="ts">
  // Loading placeholder rows. One component for what had grown into 18 identical
  // per-view skeletons (cfg__skel, sx__skel, mx__skel, …) — each a copy of the
  // same gradient + a same-named-but-distinct @keyframes shimmer.
  //
  // The motion is a compositable OPACITY pulse, NOT the animated
  // `background-position` the old copies used. WebKitGTK (the desktop engine)
  // cannot composite a background-position animation: it repaints the element's
  // box every frame on the main thread — exactly the antipattern tokens.css
  // warns about — and 18 of them could run at once on a slow view. Opacity
  // composites, so this is both less code and less main-thread paint.
  //
  // `count` rows of `height`, separated by `gap`. `radius` and `margin` cover
  // the few views that inset or tighten their rows.
  let {
    height = "96px",
    radius = "var(--r-md)",
    count = 1,
    gap = "var(--sp-4)",
    margin = "0",
  }: {
    height?: string;
    radius?: string;
    count?: number;
    gap?: string;
    margin?: string;
  } = $props();
</script>

<div class="skel-stack" style="--skel-gap:{gap}" aria-hidden="true">
  {#each Array(count) as _, i (i)}
    <div
      class="skel"
      style="--skel-h:{height}; --skel-r:{radius}; --skel-m:{margin}"
    ></div>
  {/each}
</div>

<style>
  .skel-stack {
    display: flex;
    flex-direction: column;
    gap: var(--skel-gap);
  }
  .skel {
    height: var(--skel-h);
    border-radius: var(--skel-r);
    margin: var(--skel-m);
    background: var(--bg-raised-2);
    /* Opacity pulse — composites on WebKitGTK (background-position does not). */
    animation: skel-pulse 1.4s var(--ease-inout) infinite;
    will-change: opacity;
  }
  @keyframes skel-pulse {
    0%,
    100% {
      opacity: 0.5;
    }
    50% {
      opacity: 0.85;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .skel {
      animation: none;
      opacity: 0.6;
    }
  }
</style>
