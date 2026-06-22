<script lang="ts">
  // A raised surface with a hairline edge. Interactive cards lift on hover and
  // expose a focus ring; static cards are inert containers.
  import type { Snippet } from "svelte";
  let {
    interactive = false,
    selected = false,
    title,
    onclick,
    children,
  }: {
    interactive?: boolean;
    selected?: boolean;
    title?: string;
    onclick?: (e: MouseEvent | KeyboardEvent) => void;
    children: Snippet;
  } = $props();

  function onkeydown(e: KeyboardEvent) {
    if (!interactive || !onclick) return;
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      onclick(e);
    }
  }
</script>

{#if interactive}
  <div
    class="card card--interactive"
    class:card--selected={selected}
    {title}
    role="button"
    tabindex="0"
    {onclick}
    {onkeydown}
  >
    {@render children()}
  </div>
{:else}
  <div class="card" class:card--selected={selected} {title}>
    {@render children()}
  </div>
{/if}

<style>
  .card {
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    box-shadow: var(--shadow-1);
  }
  .card--interactive {
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out),
      transform var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out);
  }
  .card--interactive:hover {
    background: var(--bg-raised-2);
    border-color: var(--border-subtle);
    transform: translateY(-1px);
    box-shadow: var(--shadow-2);
  }
  .card--interactive:active {
    transform: translateY(0);
  }
  .card--interactive:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .card--selected {
    border-color: var(--border-brand-faint);
    background: var(--state-selected);
  }
</style>
