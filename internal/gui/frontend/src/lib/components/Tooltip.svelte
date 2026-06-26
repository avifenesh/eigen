<script lang="ts">
  // A delayed hover/focus tooltip primitive. Wraps its trigger and floats a
  // small label beside it (above by default) after a brief intent delay
  // (~400ms) — long enough to ignore an idle cursor sweep, short enough to feel
  // responsive on a settle. Shows on pointer hover or keyboard focus-visible;
  // dismisses instantly on leave, blur, or Escape. The bubble is
  // pointer-events:none so it can never steal a click or trap the cursor, and
  // it carries a hairline tail that points back at the trigger. Motion is a
  // brief fade+rise (transform/opacity only); reduced-motion collapses it to a
  // plain fade.
  //
  // USAGE — replaces a native `title=` so the label is delayed, consistently
  // styled, and reachable by keyboard focus (native title is touch- and
  // keyboard-hostile). Wrap the control; pick a placement that won't clip at
  // the trigger's edge of the viewport (e.g. `right` for a left rail item,
  // `bottom` for a top-bar button):
  //
  //   <Tooltip text="Refresh" placement="bottom">
  //     <button onclick={refresh} aria-label="Refresh">…</button>
  //   </Tooltip>
  //
  // A falsy `text` is a clean no-op (renders only the trigger), so a
  // conditional label needs no surrounding {#if}.
  import type { Snippet } from "svelte";

  type Placement = "top" | "bottom" | "left" | "right";

  let {
    text,
    placement = "top",
    children,
  }: {
    text?: string;
    // Which side of the trigger the bubble floats on. Default `top`.
    placement?: Placement;
    children: Snippet;
  } = $props();

  const OPEN_DELAY = 400;

  let open = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;
  // SSR-stable, per-instance id for the aria-describedby ↔ tooltip link.
  const tipId = $props.id();

  function schedule() {
    if (!text) return;
    clearTimer();
    timer = setTimeout(() => {
      open = true;
    }, OPEN_DELAY);
  }

  function clearTimer() {
    if (timer !== undefined) {
      clearTimeout(timer);
      timer = undefined;
    }
  }

  function dismiss() {
    clearTimer();
    open = false;
  }

  function onpointerenter() {
    schedule();
  }

  function onpointerleave() {
    dismiss();
  }

  function onfocusin(e: FocusEvent) {
    // Only surface on keyboard focus, not on a pointer-driven focus — those
    // already get the hover path and a pointer focus should stay quiet. The
    // :focus-visible match is the browser's own keyboard-vs-pointer heuristic.
    const el = e.target as Element | null;
    if (el && el.matches(":focus-visible")) {
      schedule();
    }
  }

  function onfocusout() {
    dismiss();
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && open) {
      dismiss();
    }
  }

  $effect(() => {
    // Sole teardown: clear any pending open-delay timer if the trigger unmounts
    // mid-countdown, so no setTimeout fires into a dead component.
    return () => clearTimer();
  });
</script>

<!-- The wrapper is a passive relay, not a control: the real interactive
     element lives inside {children}. role="presentation" tells a11y tooling
     the span itself carries no semantics — it only hosts the hover/focus
     handlers and the absolutely-positioned bubble. -->
<span
  class="tt"
  role="presentation"
  {onpointerenter}
  {onpointerleave}
  {onfocusin}
  {onfocusout}
  {onkeydown}
  aria-describedby={open ? tipId : undefined}
>
  {@render children()}
  {#if text}
    <span
      id={tipId}
      class="tt__bubble tt__bubble--{placement}"
      class:tt__bubble--open={open}
      role="tooltip"
      aria-hidden={!open}
    >
      {text}
      <span class="tt__tail" aria-hidden="true"></span>
    </span>
  {/if}
</span>

<style>
  .tt {
    /* One-off geometry: bubble cap + tail size, kept local to this component. */
    --tt-max-width: 240px;
    --tt-tail: 7px;
    position: relative;
    display: inline-flex;
    /* The wrapper is a passive seam — it never alters the trigger's own box. */
  }

  .tt__bubble {
    position: absolute;
    z-index: 50;
    /* `--tt-rest` is the placement's hidden offset (a hair toward the trigger);
       `--tt-center` re-centers on the cross axis; `--tt-scale` is the rest
       scale. Each top/bottom variant sets center + rest; open state collapses
       rest to 0 and scale to 1. (left/right use plain transforms — they don't
       need the var plumbing since their cross axis is fixed at -50%.) */
    --tt-rest: 0px;
    --tt-center: 0px;
    --tt-scale: 0.97;
    transform: translate(var(--tt-center), var(--tt-rest)) scale(var(--tt-scale));
    max-width: var(--tt-max-width);
    width: max-content;
    padding: var(--sp-2) var(--sp-4);
    font: var(--fw-medium) var(--fs-micro) / var(--lh-snug) var(--font-sans);
    letter-spacing: var(--ls-normal);
    color: var(--text-secondary);
    text-align: center;
    white-space: normal;
    overflow-wrap: break-word;
    background: var(--bg-overlay-2);
    border-radius: var(--r-sm);
    box-shadow: var(--shadow-2);
    opacity: 0;
    pointer-events: none;
    transition:
      opacity var(--dur-fast) var(--ease-out),
      transform var(--dur-fast) var(--ease-out);
  }

  .tt__bubble--open {
    opacity: 1;
    --tt-rest: 0px;
    --tt-scale: 1;
  }

  /* ABOVE the trigger: anchored to its top, centered, easing up from below. */
  .tt__bubble--top {
    bottom: calc(100% + var(--sp-3));
    left: 50%;
    --tt-center: -50%;
    --tt-rest: var(--sp-1);
    transform-origin: bottom center;
  }
  /* BELOW the trigger: anchored to its bottom, easing down from above. */
  .tt__bubble--bottom {
    top: calc(100% + var(--sp-3));
    left: 50%;
    --tt-center: -50%;
    --tt-rest: calc(-1 * var(--sp-1));
    transform-origin: top center;
  }
  /* LEFT of the trigger: anchored to its left edge, centered vertically. */
  .tt__bubble--left {
    right: calc(100% + var(--sp-3));
    top: 50%;
    transform: translate(var(--sp-1), -50%) scale(0.97);
    transform-origin: center right;
  }
  .tt__bubble--left.tt__bubble--open {
    transform: translate(0, -50%) scale(1);
  }
  /* RIGHT of the trigger: anchored to its right edge, centered vertically. */
  .tt__bubble--right {
    left: calc(100% + var(--sp-3));
    top: 50%;
    transform: translate(calc(-1 * var(--sp-1)), -50%) scale(0.97);
    transform-origin: center left;
  }
  .tt__bubble--right.tt__bubble--open {
    transform: translate(0, -50%) scale(1);
  }

  /* A small diamond tail that bridges bubble and trigger, color-matched to the
     overlay surface with a hairline edge so it reads as one continuous chip.
     Only the two edges facing the trigger carry the hairline. */
  .tt__tail {
    position: absolute;
    width: var(--tt-tail);
    height: var(--tt-tail);
    background: var(--bg-overlay-2);
    border-bottom-right-radius: var(--r-xs);
  }
  .tt__bubble--top .tt__tail {
    top: 100%;
    left: 50%;
    transform: translate(-50%, -55%) rotate(45deg);
    border-right: 1px solid var(--border-subtle);
    border-bottom: 1px solid var(--border-subtle);
  }
  .tt__bubble--bottom .tt__tail {
    bottom: 100%;
    left: 50%;
    transform: translate(-50%, 55%) rotate(45deg);
    border-left: 1px solid var(--border-subtle);
    border-top: 1px solid var(--border-subtle);
  }
  .tt__bubble--left .tt__tail {
    left: 100%;
    top: 50%;
    transform: translate(-55%, -50%) rotate(45deg);
    border-top: 1px solid var(--border-subtle);
    border-right: 1px solid var(--border-subtle);
  }
  .tt__bubble--right .tt__tail {
    right: 100%;
    top: 50%;
    transform: translate(55%, -50%) rotate(45deg);
    border-bottom: 1px solid var(--border-subtle);
    border-left: 1px solid var(--border-subtle);
  }

  @media (prefers-reduced-motion: reduce) {
    /* Collapse the rise/slide to a plain fade; no transform travel. */
    .tt__bubble {
      --tt-rest: 0px;
      transition: opacity var(--dur-fast) var(--ease-out);
    }
    .tt__bubble--left,
    .tt__bubble--right {
      transform: translate(0, -50%) scale(0.97);
    }
    .tt__bubble--left.tt__bubble--open,
    .tt__bubble--right.tt__bubble--open {
      transform: translate(0, -50%) scale(1);
    }
  }
</style>
