<script lang="ts" generics="T extends string | number">
  // A custom single-select dropdown. We do NOT use a native <select>: under
  // webkit2gtk (Wails on Linux) the popped-up <option> list is OS-drawn and
  // ignores our CSS, so a dark UI gets a black-on-black unreadable list. This
  // builds the open list ourselves over the shared Popover, so options are
  // always legible and on-theme. Keyboard: Enter/Space opens, ↑/↓ moves the
  // active option, Enter selects, Escape closes (Popover owns Escape + outside
  // click). The closed control shows the selected option's label + a chevron.
  import Popover from "./Popover.svelte";

  type Option = { value: T; label: string; sub?: string; disabled?: boolean };

  let {
    value = $bindable(),
    options,
    placeholder = "Select…",
    label = "Select",
    width,
    disabled = false,
    onchange,
  }: {
    value: T | undefined;
    options: Option[];
    placeholder?: string;
    // Accessible name for the control + popover.
    label?: string;
    // Optional fixed panel width; otherwise sizes to the trigger.
    width?: number;
    disabled?: boolean;
    onchange?: (value: T) => void;
  } = $props();

  let open = $state(false);
  // The keyboard-active row while open; seeded to the selected option on open.
  let activeIdx = $state(-1);

  const selected = $derived(options.find((o) => o.value === value) ?? null);

  function choose(o: Option, close: () => void) {
    if (o.disabled) return;
    value = o.value;
    onchange?.(o.value);
    close();
  }

  // Seed the active row to the current selection each time the list opens.
  $effect(() => {
    if (open) {
      const i = options.findIndex((o) => o.value === value);
      activeIdx = i >= 0 ? i : 0;
    }
  });

  // Move the active row, skipping disabled options; wraps at the ends.
  function move(delta: number) {
    const n = options.length;
    if (n === 0) return;
    let i = activeIdx;
    for (let step = 0; step < n; step++) {
      i = (i + delta + n) % n;
      if (!options[i]?.disabled) {
        activeIdx = i;
        return;
      }
    }
  }

  function onListKey(e: KeyboardEvent, close: () => void) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      move(1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      move(-1);
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      const o = options[activeIdx];
      if (o) choose(o, close);
    }
  }
</script>

<Popover {label} {width} align="start" bind:open>
  {#snippet trigger(toggle)}
    <button
      type="button"
      class="dd__control"
      {disabled}
      aria-haspopup="listbox"
      aria-expanded={open}
      aria-label="{label}: {selected ? selected.label : placeholder}"
      onclick={toggle}
    >
      <span class="dd__value" class:dd__value--placeholder={!selected} aria-hidden="true">
        {selected ? selected.label : placeholder}
      </span>
      <span class="dd__chev" aria-hidden="true"></span>
    </button>
  {/snippet}

  {#snippet children()}
    <!-- The Popover already focus-traps the panel; this wrapper takes the
         keyboard so ↑/↓/Enter drive the list. -->
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <ul
      class="dd__list"
      role="listbox"
      aria-label={label}
      tabindex="-1"
      onkeydown={(e) => onListKey(e, () => (open = false))}
    >
      {#each options as o, i (o.value)}
        <li role="presentation">
          <button
            type="button"
            class="dd__opt"
            class:dd__opt--active={i === activeIdx}
            class:dd__opt--selected={o.value === value}
            role="option"
            aria-selected={o.value === value}
            disabled={o.disabled}
            onmouseenter={() => (activeIdx = i)}
            onclick={() => choose(o, () => (open = false))}
          >
            <span class="dd__opt-label">{o.label}</span>
            {#if o.sub}<span class="dd__opt-sub">{o.sub}</span>{/if}
            {#if o.value === value}<span class="dd__opt-check" aria-hidden="true">✓</span>{/if}
          </button>
        </li>
      {/each}
    </ul>
  {/snippet}
</Popover>

<style>
  /* Closed control — matches the app's input/select geometry but is a plain
     button (no native widget), so it renders identically on every platform. */
  .dd__control {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-4);
    width: 100%;
    height: 30px;
    padding: 0 var(--sp-4);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    background: var(--bg-well);
    color: var(--text-primary);
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-sans);
    cursor: pointer;
    text-align: left;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .dd__control:hover:not(:disabled) {
    border-color: var(--border-strong);
  }
  .dd__control:focus-visible {
    outline: none;
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .dd__control:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .dd__value {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dd__value--placeholder {
    color: var(--text-faint);
  }
  /* Chevron drawn from borders so there's no glyph-font dependency. */
  .dd__chev {
    flex: none;
    width: 7px;
    height: 7px;
    border-right: 1.5px solid var(--text-muted);
    border-bottom: 1.5px solid var(--text-muted);
    transform: translateY(-2px) rotate(45deg);
  }

  /* Open list — our own markup, so it's always on-theme + legible. */
  .dd__list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 180px;
  }
  .dd__opt {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    width: 100%;
    padding: var(--sp-3) var(--sp-4);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    border-radius: var(--r-sm);
    cursor: pointer;
    text-align: left;
    font: var(--fw-medium) var(--fs-body-sm) / 1.2 var(--font-sans);
  }
  /* Hover + keyboard share one "active" wash so mouse and keys agree. */
  .dd__opt--active:not(:disabled) {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .dd__opt--selected {
    color: var(--brand-bright);
  }
  .dd__opt:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .dd__opt-label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* A secondary line (e.g. a project's dir) — quiet, truncates from the left so
     the meaningful tail of a path stays visible. */
  .dd__opt-sub {
    flex: none;
    max-width: 45%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl;
    color: var(--text-faint);
    font-size: var(--fs-micro);
    font-variant-numeric: tabular-nums;
  }
  .dd__opt-check {
    flex: none;
    color: var(--brand);
    font-size: var(--fs-micro);
  }
</style>
