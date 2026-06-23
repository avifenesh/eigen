<script lang="ts">
  // Command palette (⌘K / Ctrl+K). A single fuzzy-filtered list of REAL actions
  // only — navigate to any view, or jump to any session. No dead/no-op rows.
  // Opens on the shortcut, closes on Escape / selection / scrim click. Keyboard
  // driven: ↑/↓ move, Enter runs. The open listener lives in an $effect with
  // teardown so it never leaks across mounts.
  import { router, routes, type Route } from "$lib/router.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";

  type Item =
    | { kind: "nav"; label: string; hint: string; route: Route }
    | { kind: "session"; label: string; hint: string; id: string };

  let open = $state(false);
  let query = $state("");
  let active = $state(0);
  let input = $state<HTMLInputElement | undefined>(undefined);

  const navItems: Item[] = routes.map((r) => ({
    kind: "nav" as const,
    label: r.charAt(0).toUpperCase() + r.slice(1),
    hint: "view",
    route: r,
  }));

  const items = $derived.by<Item[]>(() => {
    const sess: Item[] = sessions.list.map((s) => ({
      kind: "session" as const,
      label: s.title || "untitled session",
      hint: s.model || "session",
      id: s.id,
    }));
    const all = [...navItems, ...sess];
    const q = query.trim().toLowerCase();
    if (!q) return all;
    return all.filter((i) => i.label.toLowerCase().includes(q) || i.hint.toLowerCase().includes(q));
  });

  // Keep the active index in range as the filtered set changes.
  $effect(() => {
    if (active >= items.length) active = Math.max(0, items.length - 1);
  });

  function show() {
    open = true;
    query = "";
    active = 0;
    queueMicrotask(() => input?.focus());
  }
  function hide() {
    open = false;
  }
  function run(i: Item) {
    if (i.kind === "nav") router.go(i.route);
    else router.go("chat", i.id);
    hide();
  }

  function onWinKey(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
      e.preventDefault();
      open ? hide() : show();
    } else if (e.key === "Escape" && open) {
      hide();
    }
  }
  $effect(() => {
    addEventListener("keydown", onWinKey);
    return () => removeEventListener("keydown", onWinKey);
  });

  function onListKey(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      active = Math.min(active + 1, items.length - 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      active = Math.max(active - 1, 0);
    } else if (e.key === "Enter") {
      e.preventDefault();
      const i = items[active];
      if (i) run(i);
    }
  }
</script>

{#if open}
  <div
    class="pal__scrim"
    role="button"
    tabindex="-1"
    aria-label="Close palette"
    onclick={hide}
    onkeydown={(e) => e.key === "Enter" && hide()}
  ></div>
  <div class="pal" role="dialog" aria-modal="true" aria-label="Command palette">
    <input
      bind:this={input}
      bind:value={query}
      class="pal__input"
      placeholder="Jump to a view or session…"
      aria-label="Command palette filter"
      onkeydown={onListKey}
    />
    <div class="pal__list" role="listbox" aria-label="Commands">
      {#if items.length === 0}
        <div class="pal__empty">No matches.</div>
      {:else}
        {#each items as i, idx (i.kind + ":" + (i.kind === "nav" ? i.route : i.id))}
          <button
            class="pal__row"
            class:pal__row--active={idx === active}
            role="option"
            aria-selected={idx === active}
            onmouseenter={() => (active = idx)}
            onclick={() => run(i)}
          >
            <span class="pal__glyph" aria-hidden="true">{i.kind === "nav" ? "→" : "▶"}</span>
            <span class="pal__label">{i.label}</span>
            <span class="pal__hint">{i.hint}</span>
          </button>
        {/each}
      {/if}
    </div>
    <div class="pal__foot">
      <span><kbd>↑</kbd><kbd>↓</kbd> move</span>
      <span><kbd>↵</kbd> open</span>
      <span><kbd>esc</kbd> close</span>
    </div>
  </div>
{/if}

<style>
  .pal__scrim {
    position: fixed;
    inset: 0;
    background: var(--bg-scrim);
    z-index: 80;
    animation: pal-scrim var(--dur-fast) var(--ease-out);
  }
  .pal {
    position: fixed;
    top: 14vh;
    left: 50%;
    transform: translateX(-50%);
    width: min(560px, 90vw);
    max-height: 64vh;
    background: var(--bg-overlay);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-lg);
    box-shadow: var(--shadow-3);
    z-index: 81;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    animation: pal-in var(--dur-base) var(--ease-out);
  }
  .pal__input {
    flex: none;
    height: 48px;
    padding: 0 var(--sp-6);
    border: none;
    border-bottom: 1px solid var(--border-hairline);
    background: transparent;
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-h3) / 1 var(--font-sans);
    outline: none;
  }
  .pal__input::placeholder {
    color: var(--text-ghost);
  }
  .pal__list {
    flex: 1;
    overflow-y: auto;
    padding: var(--sp-3);
    min-height: 0;
  }
  .pal__empty {
    padding: var(--sp-6);
    text-align: center;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  .pal__row {
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    height: 38px;
    padding: 0 var(--sp-4);
    border: none;
    background: transparent;
    border-radius: var(--r-sm);
    cursor: pointer;
    text-align: left;
    color: var(--text-secondary);
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-sans);
  }
  .pal__row--active {
    background: var(--state-selected);
    color: var(--text-primary);
  }
  .pal__glyph {
    width: 14px;
    text-align: center;
    color: var(--text-ghost);
  }
  .pal__row--active .pal__glyph {
    color: var(--brand);
  }
  .pal__label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pal__hint {
    font-size: var(--fs-micro);
    color: var(--text-faint);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }
  .pal__foot {
    flex: none;
    display: flex;
    gap: var(--sp-6);
    padding: var(--sp-4) var(--sp-6);
    border-top: 1px solid var(--border-hairline);
    font-size: var(--fs-micro);
    color: var(--text-faint);
  }
  .pal__foot kbd {
    font-family: var(--font-mono);
    background: var(--bg-raised-2);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-xs);
    padding: 1px var(--sp-2);
    margin-right: var(--sp-1);
    color: var(--text-muted);
  }
  @keyframes pal-scrim {
    from {
      opacity: 0;
    }
  }
  @keyframes pal-in {
    from {
      transform: translateX(-50%) translateY(-8px);
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pal__scrim,
    .pal {
      animation: none;
    }
  }
</style>
