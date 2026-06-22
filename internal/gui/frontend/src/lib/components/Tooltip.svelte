<script lang="ts">
  // A delayed hover/focus tooltip primitive. Wraps its trigger and floats a
  // small label above it after a brief intent delay (~400ms) — long enough to
  // ignore an idle cursor sweep, short enough to feel responsive on a settle.
  // Shows on pointer hover or keyboard focus-visible; dismisses instantly on
  // leave, blur, or Escape. The bubble is pointer-events:none so it can never
  // steal a click or trap the cursor, and it carries a hairline tail that
  // points back at the trigger. Motion is a brief fade+rise (transform/opacity
  // only); reduced-motion collapses it to a plain fade.
  import type { Snippet } from "svelte";
  import { onDestroy } from "svelte";

  let {
    text,
    children,
  }: {
    text: string;
    children: Snippet;
  } = $props();

  const OPEN_DELAY = 400;

  let open = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;
  // Cancel any pending open-delay timer on unmount.
  onDestroy(() => clearTimeout(timer));
  const tipId = `tt-${Math.random().toString(36).slice(2, 9)}`;

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
    // Guarantee the timer is cleared if the trigger unmounts mid-countdown.
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
      class="tt__bubble"
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
    bottom: calc(100% + var(--sp-3));
    left: 50%;
    z-index: 50;
    /* Center horizontally, then ease up into place from a hair below. */
    transform: translate(-50%, var(--sp-1)) scale(0.97);
    transform-origin: bottom center;
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
    transform: translate(-50%, 0) scale(1);
  }

  /* A small diamond tail that bridges bubble and trigger, color-matched to the
     overlay surface with a hairline edge so it reads as one continuous chip. */
  .tt__tail {
    position: absolute;
    top: 100%;
    left: 50%;
    width: var(--tt-tail);
    height: var(--tt-tail);
    background: var(--bg-overlay-2);
    border-right: 1px solid var(--border-subtle);
    border-bottom: 1px solid var(--border-subtle);
    transform: translate(-50%, -55%) rotate(45deg);
    border-bottom-right-radius: var(--r-xs);
  }

  @media (prefers-reduced-motion: reduce) {
    .tt__bubble {
      transform: translate(-50%, 0) scale(1);
      transition: opacity var(--dur-fast) var(--ease-out);
    }
    .tt__bubble--open {
      transform: translate(-50%, 0) scale(1);
    }
  }
</style>
