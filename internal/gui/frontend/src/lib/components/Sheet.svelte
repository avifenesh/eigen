<script lang="ts">
  // A right-anchored slide-over dialog: dimmed scrim, focus-trapped panel,
  // Escape / scrim-click close, header (title snippet + close button), body
  // snippet. The one shared modal so every slide-over in the app is consistent
  // (and consistently accessible) instead of hand-rolled per view.
  import type { Snippet } from "svelte";
  import { trapFocus } from "$lib/actions";
  import Button from "./Button.svelte";

  let {
    open,
    label,
    width = 600,
    onclose,
    title,
    children,
  }: {
    open: boolean;
    label: string;
    width?: number;
    onclose: () => void;
    title: Snippet;
    children: Snippet;
  } = $props();

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && open) onclose();
  }
</script>

<svelte:window {onkeydown} />

{#if open}
  <div
    class="sheet__scrim"
    role="button"
    tabindex="-1"
    aria-label="Close"
    onclick={onclose}
    onkeydown={(e) => e.key === "Enter" && onclose()}
  ></div>
  <div
    class="sheet"
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    aria-label={label}
    style="--sheet-w:{width}px"
    use:trapFocus
  >
    <header class="sheet__head">
      <div class="sheet__title">{@render title()}</div>
      <Button variant="icon" size="md" title="Close" onclick={onclose}>✕</Button>
    </header>
    <div class="sheet__body selectable">{@render children()}</div>
  </div>
{/if}

<style>
  .sheet__scrim {
    position: fixed;
    inset: 0;
    background: var(--bg-scrim);
    z-index: 60;
    animation: sheet-scrim var(--dur-fast) var(--ease-out);
  }
  .sheet {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(var(--sheet-w), 88vw);
    background: var(--bg-raised);
    border-left: 1px solid var(--border-subtle);
    box-shadow: var(--shadow-3);
    z-index: 61;
    display: flex;
    flex-direction: column;
    padding: var(--sp-7);
    gap: var(--sp-5);
    animation: sheet-in var(--dur-base) var(--ease-out);
  }
  .sheet__head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-5);
  }
  .sheet__title {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    min-width: 0;
  }
  .sheet__body {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }
  @keyframes sheet-scrim {
    from {
      opacity: 0;
    }
  }
  @keyframes sheet-in {
    from {
      transform: translateX(16px);
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .sheet__scrim,
    .sheet {
      animation: none;
    }
  }
</style>
