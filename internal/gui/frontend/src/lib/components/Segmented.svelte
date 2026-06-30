<script lang="ts">
  // A single-select control: pick exactly one option from a small set. One
  // component for the three segmented idioms that had grown up ad-hoc across
  // the app, selected by `variant`:
  //   solid   — an enclosed track, the active segment filled brand (Skills'
  //             install-source switch).
  //   surface — an enclosed track, the active segment a raised surface, with
  //             an optional per-option count (Dreaming's timeline strands).
  //   chip    — separate full-radius pills, the active one tinted (Board's
  //             owner/state filters). Pills wrap; the others stay on one row.
  // Keyboard: each option is a real <button> with aria-pressed; the group
  // carries role="group" + the caller's label. The brand focus ring comes from
  // the global :focus-visible baseline.
  type Option = { value: string; label: string; count?: number };
  let {
    options,
    value,
    onChange,
    variant = "solid",
    ariaLabel,
  }: {
    options: Option[];
    value: string;
    onChange: (value: string) => void;
    variant?: "solid" | "surface" | "chip";
    ariaLabel: string;
  } = $props();
</script>

<div class="seg seg--{variant}" role="group" aria-label={ariaLabel}>
  {#each options as opt (opt.value)}
    <button
      type="button"
      class="seg__btn"
      class:seg__btn--on={value === opt.value}
      aria-pressed={value === opt.value}
      onclick={() => onChange(opt.value)}
    >
      {opt.label}
      {#if opt.count !== undefined}<span class="seg__count tnum">{opt.count}</span>{/if}
    </button>
  {/each}
</div>

<style>
  /* SOLID + SURFACE share an enclosed track; CHIP is a free row of pills. */
  .seg {
    display: inline-flex;
  }
  .seg--solid {
    padding: 2px;
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--bg-raised);
  }
  .seg--surface {
    padding: var(--sp-1);
    gap: var(--sp-1);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    background: var(--bg-well);
  }
  .seg--chip {
    flex-wrap: wrap;
    gap: var(--sp-3);
  }

  .seg__btn {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-3);
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
    transition:
      color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }

  /* SOLID — compact track, brand-filled active segment. */
  .seg--solid .seg__btn {
    height: 28px;
    padding: 0 var(--sp-4);
    border-radius: var(--r-sm);
  }
  .seg--solid .seg__btn:hover:not(.seg__btn--on) {
    color: var(--text-secondary);
  }
  .seg--solid .seg__btn--on {
    background: var(--brand);
    color: var(--text-on-brand);
  }

  /* SURFACE — taller track, raised active segment, muted count that brightens. */
  .seg--surface .seg__btn {
    height: 28px;
    padding: 0 var(--sp-5);
    border-radius: var(--r-sm);
    font-size: var(--fs-body-sm);
  }
  .seg--surface .seg__btn:hover:not(.seg__btn--on) {
    color: var(--text-primary);
  }
  .seg--surface .seg__btn--on {
    background: var(--bg-raised-2);
    color: var(--text-primary);
  }
  .seg--surface .seg__count {
    font-size: var(--fs-micro);
    color: var(--text-faint);
  }
  .seg--surface .seg__btn--on .seg__count {
    color: var(--brand);
  }

  /* CHIP — separate bordered pills; active is a brand-faint tint. */
  .seg--chip .seg__btn {
    height: 26px;
    padding: 0 var(--sp-4);
    border-radius: var(--r-full);
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised-2);
  }
  .seg--chip .seg__btn:hover:not(.seg__btn--on) {
    color: var(--text-primary);
  }
  .seg--chip .seg__btn--on {
    background: var(--state-selected);
    border-color: var(--border-brand-faint);
    color: var(--brand-bright);
  }

  @media (prefers-reduced-motion: reduce) {
    .seg__btn {
      transition: none;
    }
  }
</style>
