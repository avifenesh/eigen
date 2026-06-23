<script lang="ts">
  // A small anchored floating panel. A trigger Snippet hosts the control that
  // toggles it; the panel floats below-and-aligned-to that trigger over a
  // transparent click-catching scrim, focus-trapped, dismissing on Escape or a
  // scrim click. The one shared lightweight overlay so every in-place panel in
  // the app (settings clusters, menus) is consistent and keyboard-accessible
  // instead of hand-rolled per view. Motion is a brief fade+rise; reduced-motion
  // collapses it to a plain fade.
  import type { Snippet } from "svelte";
  import { trapFocus } from "$lib/actions";

  let {
    label,
    align = "end",
    width,
    open = $bindable(false),
    trigger,
    children,
  }: {
    label: string;
    // Which edge of the trigger the panel aligns its same edge to.
    align?: "start" | "end";
    // Optional fixed panel width; otherwise it sizes to content (capped in CSS).
    width?: number;
    open?: boolean;
    // Receives a toggle fn so the caller's button can open/close the panel.
    trigger: Snippet<[() => void]>;
    children: Snippet;
  } = $props();

  // The trigger anchor we measure to place the panel beneath it.
  let anchor: HTMLDivElement | undefined = $state(undefined);
  let pos = $state<{ top: number; left: number; right: number }>({ top: 0, left: 0, right: 0 });

  function place() {
    if (!anchor) return;
    const r = anchor.getBoundingClientRect();
    pos = {
      top: r.bottom + 6,
      left: r.left,
      right: window.innerWidth - r.right,
    };
  }

  function toggle() {
    if (!open) place();
    open = !open;
  }
  function close() {
    open = false;
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && open) {
      e.stopPropagation();
      close();
    }
  }
</script>

<svelte:window {onkeydown} onresize={() => open && place()} />

<div class="pop__anchor" bind:this={anchor}>
  {@render trigger(toggle)}
</div>

{#if open}
  <div
    class="pop__scrim"
    role="button"
    tabindex="-1"
    aria-label="Close {label}"
    onclick={close}
    onkeydown={(e) => e.key === "Enter" && close()}
  ></div>
  <div
    class="pop"
    class:pop--fixedw={width != null}
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    aria-label={label}
    style="top:{pos.top}px; {align === 'end'
      ? `right:${pos.right}px`
      : `left:${pos.left}px`}; {width != null ? `--pop-w:${width}px` : ''}"
    use:trapFocus
  >
    {@render children()}
  </div>
{/if}

<style>
  .pop__anchor {
    display: contents;
  }
  /* Transparent catcher: closes on an outside click without dimming the page —
     a settings panel is a light touch, not a modal blackout. */
  .pop__scrim {
    position: fixed;
    inset: 0;
    background: transparent;
    z-index: 70;
  }
  .pop {
    position: fixed;
    z-index: 71;
    max-width: min(360px, 92vw);
    max-height: 70vh;
    overflow-y: auto;
    background: var(--bg-overlay);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    box-shadow: var(--shadow-3);
    padding: var(--sp-6);
    animation: pop-in var(--dur-base) var(--ease-out);
  }
  .pop--fixedw {
    width: var(--pop-w);
  }
  @keyframes pop-in {
    from {
      transform: translateY(-6px);
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pop {
      animation: none;
    }
  }
</style>
