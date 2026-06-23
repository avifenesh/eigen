<script lang="ts">
  // Command palette (⌘K / Ctrl+K). A fuzzy-filtered, grouped list of REAL
  // actions only — run a verb, navigate to any view, or jump to any session.
  // No dead/no-op rows. Opens on the shortcut, closes on Escape / selection /
  // scrim click. Keyboard: ↑/↓ move, Enter runs. The window keydown listener
  // lives in an $effect with teardown so it never leaks across mounts.
  import { router, routes, type Route } from "$lib/router.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";
  import { feed } from "$lib/stores/feed.svelte";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { Bridge } from "$lib/bridge";
  import { trapFocus } from "$lib/actions";

  type Group = "Actions" | "Views" | "Sessions";
  type Item = {
    group: Group;
    label: string;
    hint: string;
    glyph: string;
    run: () => void | Promise<void>;
    keywords?: string; // extra fuzzy-match text
    id?: string; // stable key when label can collide (e.g. untitled sessions)
  };

  let open = $state(false);
  let query = $state("");
  let active = $state(0);
  let input = $state<HTMLInputElement | undefined>(undefined);
  // Row elements keyed by their `items` index so we can scroll the active one
  // into view when arrowing past the height-capped (max-height 64vh) window.
  let rows = $state<Record<number, HTMLElement>>({});

  function go(route: Route) {
    router.go(route);
    hide();
  }
  async function newSession() {
    hide();
    try {
      const id = await Bridge.NewSession("", "", "");
      await sessions.refresh();
      router.go("chat", id);
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    }
  }
  async function pruneEmpty() {
    hide();
    try {
      const removed = await Bridge.PruneSessions();
      toasts.info(removed.length ? `pruned ${removed.length} empty session${removed.length === 1 ? "" : "s"}` : "no empty sessions");
      await sessions.refresh();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    }
  }
  // Trigger a real proactive-feed rescan, land on Home where the fresh results
  // surface (they arrive via the eigen:feed push the feed store rides).
  function refreshFeed() {
    hide();
    router.go("home");
    feed.refresh();
    toasts.info("rescanning projects…");
  }

  // Global actions (verbs the rail+views expose, runnable without the mouse).
  const actions: Item[] = [
    { group: "Actions", label: "Start a session", hint: "new", glyph: "✦", run: newSession, keywords: "new chat create" },
    { group: "Actions", label: "Prune empty sessions", hint: "cleanup", glyph: "⌫", run: pruneEmpty, keywords: "delete remove clean" },
    { group: "Actions", label: "Refresh feed", hint: "scan", glyph: "↻", run: refreshFeed, keywords: "act on proactive ideas rescan" },
  ];
  const navItems: Item[] = routes
    .filter((r) => r !== "chat") // chat is reached via a session, not a bare view
    .map((r) => ({
      group: "Views" as const,
      label: r.charAt(0).toUpperCase() + r.slice(1),
      hint: "view",
      glyph: "→",
      run: () => go(r),
    }));

  // Cheap subsequence fuzzy score: all query chars must appear in order; reward
  // contiguous runs + word-start hits. Returns -1 for no match.
  function fuzzy(q: string, text: string): number {
    if (!q) return 0;
    const t = text.toLowerCase();
    let ti = 0;
    let score = 0;
    let run = 0;
    let prev = -2;
    for (const ch of q) {
      const at = t.indexOf(ch, ti);
      if (at < 0) return -1;
      run = at === prev + 1 ? run + 1 : 0;
      score += 1 + run * 2 + (at === 0 || t[at - 1] === " " || t[at - 1] === "-" ? 3 : 0);
      prev = at;
      ti = at + 1;
    }
    return score;
  }

  const items = $derived.by<Item[]>(() => {
    const sess: Item[] = sessions.list.map((s) => ({
      group: "Sessions" as const,
      label: s.title || "untitled session",
      hint: s.model || "session",
      glyph: "▶",
      run: () => {
        router.go("chat", s.id);
        hide();
      },
      keywords: s.dir,
      id: s.id, // sessions share the "untitled session" label; key on id
    }));
    const all = [...actions, ...navItems, ...sess];
    const q = query.trim().toLowerCase();
    if (!q) return all;
    // Score against label + hint + keywords; keep matches, best first, but
    // preserve group order as a stable tiebreak so sections stay coherent.
    const order: Record<Group, number> = { Actions: 0, Views: 1, Sessions: 2 };
    return all
      .map((i) => ({ i, s: Math.max(fuzzy(q, i.label), fuzzy(q, i.hint), i.keywords ? fuzzy(q, i.keywords) : -1) }))
      .filter((x) => x.s >= 0)
      .sort((a, b) => b.s - a.s || order[a.i.group] - order[b.i.group])
      .map((x) => x.i);
  });

  // Rows with a section header injected when the group changes (visual only;
  // headers aren't selectable — `active` indexes `items`, not the rendered rows).
  const grouped = $derived.by(() => {
    const out: { item: Item; index: number; header?: Group }[] = [];
    let last: Group | null = null;
    items.forEach((item, index) => {
      const header = item.group !== last ? item.group : undefined;
      last = item.group;
      out.push({ item, index, header });
    });
    return out;
  });

  // Keep the active index in range as the filtered set changes.
  $effect(() => {
    if (active >= items.length) active = Math.max(0, items.length - 1);
  });

  // The list is height-capped, so arrowing past the visible window has to drag
  // the viewport along. Track `active` and pull its row into view; "nearest"
  // keeps already-visible rows put and respects the OS reduced-motion setting.
  $effect(() => {
    rows[active]?.scrollIntoView({ block: "nearest" });
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
    void i.run();
  }
  // Register a row element under its `items` index and drop the entry when the
  // row unmounts (filter shrinks the set) so `rows` never holds stale nodes.
  function track(el: HTMLElement, index: number) {
    rows[index] = el;
    return {
      update(next: number) {
        if (next !== index) delete rows[index];
        rows[next] = el;
        index = next;
      },
      destroy() {
        if (rows[index] === el) delete rows[index];
      },
    };
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
  <div class="pal" role="dialog" aria-modal="true" aria-label="Command palette" use:trapFocus>
    <input
      bind:this={input}
      bind:value={query}
      class="pal__input"
      placeholder="Run an action, jump to a view or session…"
      aria-label="Command palette filter"
      role="combobox"
      aria-expanded="true"
      aria-autocomplete="list"
      aria-controls="pal-listbox"
      aria-activedescendant={items.length ? `pal-opt-${active}` : undefined}
      onkeydown={onListKey}
    />
    <div id="pal-listbox" class="pal__list" role="listbox" aria-label="Commands">
      {#if grouped.length === 0}
        <div class="pal__empty">No matches.</div>
      {:else}
        {#each grouped as g (g.item.id ?? g.item.group + ":" + g.item.label)}
          {#if g.header}<div class="pal__section">{g.header}</div>{/if}
          <!-- Non-focusable option: focus stays on the combobox input, which
               points here via aria-activedescendant. onmousedown (not click)
               keeps the input from blurring before the action fires. -->
          <div
            use:track={g.index}
            id={`pal-opt-${g.index}`}
            class="pal__row"
            class:pal__row--active={g.index === active}
            role="option"
            tabindex={-1}
            aria-selected={g.index === active}
            onmouseenter={() => (active = g.index)}
            onmousedown={(e) => {
              e.preventDefault();
              run(g.item);
            }}
          >
            <span class="pal__glyph" aria-hidden="true">{g.item.glyph}</span>
            <span class="pal__label">{g.item.label}</span>
            <span class="pal__hint">{g.item.hint}</span>
          </div>
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
  .pal__section {
    padding: var(--sp-4) var(--sp-4) var(--sp-2);
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .pal__section:first-child {
    padding-top: var(--sp-2);
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
