<script lang="ts">
  // Underline tabs: switch between sibling views/sections of one page. The
  // active tab carries a brand underline; the rest are muted. One component for
  // the two underline-tab idioms that had diverged (Observe's telemetry views,
  // Config's category tabs), selected by `divider`:
  //   divider=false — a bare row of tabs (Observe sits in its own header).
  //   divider=true  — a hairline rule spans the full row and the active tab's
  //                   underline overlaps it (Config, which separates the tab
  //                   strip from the fields below).
  // role="tablist" + aria-selected per tab. Brand focus ring is the global
  // :focus-visible baseline.
  type Tab = { value: string; label: string };
  let {
    tabs,
    value,
    onChange,
    divider = false,
    ariaLabel,
  }: {
    tabs: Tab[];
    value: string;
    onChange: (value: string) => void;
    divider?: boolean;
    ariaLabel?: string;
  } = $props();
</script>

<div class="tabs" class:tabs--divider={divider} role="tablist" aria-label={ariaLabel}>
  {#each tabs as t (t.value)}
    <button
      class="tabs__tab"
      class:tabs__tab--on={value === t.value}
      role="tab"
      aria-selected={value === t.value}
      onclick={() => onChange(t.value)}
    >{t.label}</button>
  {/each}
</div>

<style>
  .tabs {
    display: flex;
    gap: var(--sp-2);
  }
  .tabs--divider {
    border-bottom: 1px solid var(--divider);
  }
  .tabs__tab {
    height: 34px;
    padding: 0 var(--sp-5);
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-sans);
    border-bottom: 2px solid transparent;
    transition:
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  /* With a divider, tabs sit tighter and overlap the rule so the active
     underline reads as continuous with it (Config's compact strip). */
  .tabs--divider .tabs__tab {
    height: auto;
    padding: var(--sp-2) var(--sp-3);
    margin-bottom: -1px;
  }
  .tabs__tab:hover {
    color: var(--text-primary);
  }
  .tabs__tab--on {
    color: var(--brand-bright);
    border-bottom-color: var(--brand);
  }

  @media (prefers-reduced-motion: reduce) {
    .tabs__tab {
      transition: none;
    }
  }
</style>
