<script lang="ts">
  // A raised surface with a hairline edge — the app's primary container.
  // Interactive cards lift on hover, depress on press, and expose a focus ring;
  // static cards are inert (no role/tabindex/keydown). Selected cards gain a
  // brand-faint edge and a soft brand wash so a chosen item reads at a glance.
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
    // Space scrolls by default; Enter/Space both activate, like a native button.
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
    aria-pressed={selected || undefined}
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
    position: relative;
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    box-shadow: var(--shadow-1);
  }

  /* A hair of luminance along the top edge — the "fine edge" detail. */
  .card::before {
    content: "";
    position: absolute;
    inset: 0 0 auto 0;
    height: 1px;
    border-radius: var(--r-md) var(--r-md) 0 0;
    background: linear-gradient(
      90deg,
      transparent,
      color-mix(in srgb, var(--text-primary) 6%, transparent),
      transparent
    );
    pointer-events: none;
  }

  .card--interactive {
    cursor: pointer;
    /* Animate transform/opacity/shadow/colour only — never layout props. */
    transition:
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out),
      transform var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out);
    will-change: transform;
  }
  .card--interactive:hover {
    background: var(--bg-raised-2);
    border-color: var(--border-subtle);
    transform: translateY(-1px);
    box-shadow: var(--shadow-2);
  }
  .card--interactive:hover::before {
    background: linear-gradient(
      90deg,
      transparent,
      color-mix(in srgb, var(--text-primary) 10%, transparent),
      transparent
    );
  }
  .card--interactive:active {
    transform: translateY(0);
    background: var(--bg-raised);
    box-shadow: var(--shadow-1);
    transition-duration: var(--dur-instant);
  }
  .card--interactive:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }

  /* SELECTED — brand-faint edge + quiet brand wash, with a left brand rail. */
  .card--selected {
    border-color: var(--border-brand-faint);
    background: var(--state-selected);
  }
  .card--selected::after {
    content: "";
    position: absolute;
    inset: var(--sp-3) auto var(--sp-3) 0;
    width: 2px;
    border-radius: var(--r-full);
    background: var(--brand);
    opacity: 0.85;
    pointer-events: none;
  }
  .card--interactive.card--selected:hover {
    border-color: var(--border-brand);
    background: var(--state-selected);
  }

  @media (prefers-reduced-motion: reduce) {
    .card--interactive {
      transition: none;
    }
    .card--interactive:hover,
    .card--interactive:active {
      transform: none;
    }
  }
</style>
