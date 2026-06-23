<script lang="ts">
  // Skills — the capability gallery. Discovered SKILL.md skills as cards,
  // grouped into source shelves (user / project / extra), each shelf tinted
  // by origin so the page reads as organized capability racks rather than a
  // flat card dump. Dream-proposed drafts ride a distinct PINNED review strip
  // at the top — a warm, alive band that says "review these" and never blends
  // into the active shelves. Clicking a skill opens a slide-over with its
  // rendered body. Skills are local files; the bridge reads them directly.
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { SkillsDTO, SkillDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Markdown from "$lib/components/Markdown.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import { trapFocus } from "$lib/actions";

  let data = $state<SkillsDTO | null>(null);
  let loading = $state(true);
  let query = $state("");

  // Slide-over preview state.
  let openSkill = $state<SkillDTO | null>(null);
  let body = $state("");
  let bodyLoading = $state(false);

  // Per-proposal in-flight guard so accept/reject buttons disable while acting.
  let acting = $state<Record<string, boolean>>({});

  // Page size for the (potentially large) proposed + active grids — the
  // proposals list can run into the hundreds, so we reveal in batches rather
  // than mounting every card at once.
  const PAGE = 24;
  let proposalsShown = $state(PAGE);
  let activeShown = $state(PAGE);

  // alive guard: a late Bridge.Skills() resolution must not write after the
  // view unmounts or a newer load() started.
  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    try {
      const d = await Bridge.Skills();
      if (seq === loadSeq) data = d;
    } catch (e) {
      if (seq === loadSeq) toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      if (seq === loadSeq) loading = false;
    }
  }
  $effect(() => {
    load();
    return () => {
      loadSeq++; // invalidate any in-flight load on unmount
    };
  });

  const filtered = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const all = data?.skills ?? [];
    if (!q) return all;
    return all.filter((s) => s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q));
  });
  // Reset the active page when the filter changes so the visible window always
  // starts from the top of the new result set.
  $effect(() => {
    query;
    activeShown = PAGE;
  });

  const proposals = $derived(data?.proposals ?? []);
  const visibleProposals = $derived(proposals.slice(0, proposalsShown));
  const visibleActive = $derived(filtered.slice(0, activeShown));

  // Source shelves — group the visible (paged) active skills by origin and
  // order them user → project → extra so the most-yours capabilities lead.
  // Each shelf carries its own tint; a shelf only renders when it has skills.
  const SHELF_ORDER = ["user", "project", "extra"] as const;
  type Shelf = { source: string; label: string; skills: SkillDTO[] };
  const shelves = $derived.by<Shelf[]>(() => {
    const by: Record<string, SkillDTO[]> = {};
    for (const s of visibleActive) (by[s.source] ??= []).push(s);
    const known = SHELF_ORDER.filter((src) => by[src]?.length).map((src) => ({
      source: src,
      label: src === "user" ? "Yours" : src === "project" ? "This project" : "Bundled",
      skills: by[src],
    }));
    // Any unexpected source still gets a (neutral) shelf rather than vanishing.
    const extras = Object.keys(by)
      .filter((src) => !SHELF_ORDER.includes(src as (typeof SHELF_ORDER)[number]))
      .sort()
      .map((src) => ({ source: src, label: src, skills: by[src] }));
    return [...known, ...extras];
  });

  async function preview(s: SkillDTO) {
    openSkill = s;
    body = "";
    bodyLoading = true;
    try {
      body = await Bridge.SkillBody(s.name);
    } catch (e) {
      body = "";
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      bodyLoading = false;
    }
  }
  function closePreview() {
    openSkill = null;
    body = "";
  }

  async function accept(name: string) {
    acting[name] = true;
    try {
      await Bridge.AcceptSkill(name);
      toasts.success(`accepted “${name}”`);
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      delete acting[name];
    }
  }
  async function reject(name: string) {
    acting[name] = true;
    try {
      await Bridge.RejectSkill(name);
      toasts.info(`rejected “${name}”`);
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      delete acting[name];
    }
  }

  function sourceTone(src: string): "brand" | "info" | "neutral" {
    return src === "user" ? "brand" : src === "project" ? "info" : "neutral";
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && openSkill) closePreview();
  }
</script>

<svelte:window {onkeydown} />

<div class="skills">
  <header class="skills__head">
    <div class="skills__search">
      <input
        class="skills__input"
        type="text"
        placeholder="Filter skills…"
        bind:value={query}
        aria-label="Filter skills"
      />
      {#if data}<span class="skills__count tnum">{filtered.length}</span>{/if}
    </div>
  </header>

  {#if loading && !data}
    <div class="skills__grid skills__grid--pad">
      {#each Array(6) as _, i (i)}<div class="skills__skel"></div>{/each}
    </div>
  {:else if !data || (data.skills.length === 0 && data.proposals.length === 0)}
    <EmptyState glyph="✦" title="No skills yet" line="Skills are SKILL.md capabilities in ~/.eigen/skills or the project. Add one with `eigen skill add`." />
  {:else}
    <div class="skills__scroll">
      {#if proposals.length > 0}
        <!-- PINNED review strip — a warm, alive band, visually unlike the
             quiet active shelves below: this asks for a decision. -->
        <section class="strip" aria-label="Proposed skills awaiting review">
          <div class="strip__rail" aria-hidden="true"></div>
          <div class="strip__inner">
            <div class="strip__head">
              <span class="strip__eyebrow">
                <span class="strip__pulse" aria-hidden="true"></span>
                Awaiting review
              </span>
              <Badge tone="warn">{proposals.length}</Badge>
            </div>
            <div class="strip__row">
              {#each visibleProposals as p (p.name)}
                <div class="prop">
                  <div class="prop__top">
                    <span class="prop__name">{p.name}</span>
                  </div>
                  <p class="prop__desc">{p.description}</p>
                  <div class="prop__actions">
                    <Button variant="primary" size="sm" loading={acting[p.name]} onclick={() => accept(p.name)}>
                      Accept
                    </Button>
                    <Button variant="ghost" size="sm" disabled={acting[p.name]} onclick={() => reject(p.name)}>
                      Reject
                    </Button>
                  </div>
                </div>
              {/each}
              {#if proposalsShown < proposals.length}
                <button class="prop prop--more" onclick={() => (proposalsShown += PAGE)}>
                  <span class="prop__more-n tnum">+{proposals.length - proposalsShown}</span>
                  <span class="prop__more-label">more to review</span>
                </button>
              {/if}
            </div>
          </div>
        </section>
      {/if}

      {#if filtered.length === 0}
        <p class="skills__empty-note">No skills match “{query}”.</p>
      {:else}
        {#each shelves as shelf (shelf.source)}
          <section class="shelf shelf--{sourceTone(shelf.source)}">
            <div class="shelf__head">
              <span class="shelf__dot" aria-hidden="true"></span>
              <h2 class="shelf__title">{shelf.label}</h2>
              <span class="shelf__n tnum">{shelf.skills.length}</span>
            </div>
            <div class="skills__grid">
              {#each shelf.skills as s (s.name)}
                <Card interactive onclick={() => preview(s)} title={s.path}>
                  <div class="sk sk--{sourceTone(s.source)}">
                    <span class="sk__rail" aria-hidden="true"></span>
                    <div class="sk__top">
                      <span class="sk__name">{s.name}</span>
                      <Badge tone={sourceTone(s.source)}>{s.source}</Badge>
                    </div>
                    <p class="sk__desc">{s.description}</p>
                  </div>
                </Card>
              {/each}
            </div>
          </section>
        {/each}
        {#if activeShown < filtered.length}
          <div class="skills__more">
            <Button variant="ghost" size="sm" onclick={() => (activeShown += PAGE)}>
              Show {Math.min(PAGE, filtered.length - activeShown)} more · {filtered.length - activeShown} remaining
            </Button>
          </div>
        {/if}
      {/if}
    </div>
  {/if}
</div>

{#if openSkill}
  <!-- Slide-over preview -->
  <div
    class="sheet__scrim"
    role="button"
    tabindex="0"
    aria-label="Close preview"
    onclick={closePreview}
    onkeydown={(e) => (e.key === "Enter" || e.key === " ") && closePreview()}
  ></div>
  <div class="sheet" role="dialog" aria-modal="true" tabindex="-1" use:trapFocus aria-label="{openSkill.name} preview">
    <header class="sheet__head">
      <div class="sheet__title-wrap">
        <h2 class="sheet__title">{openSkill.name}</h2>
        <Badge tone={sourceTone(openSkill.source)}>{openSkill.source}</Badge>
      </div>
      <Button variant="icon" size="md" title="Close" onclick={closePreview}>✕</Button>
    </header>
    <p class="sheet__desc">{openSkill.description}</p>
    <div class="sheet__path selectable">{openSkill.path}</div>
    <div class="sheet__body selectable">
      {#if bodyLoading}
        <div class="sheet__loading">Loading…</div>
      {:else if body}
        <Markdown source={body} />
      {:else}
        <p class="skills__empty-note">No body content.</p>
      {/if}
    </div>
  </div>
{/if}

<style>
  .skills {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .skills__head {
    flex: none;
    padding: var(--sp-6) var(--sp-9);
    border-bottom: 1px solid var(--border-hairline);
  }
  .skills__search {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    max-width: 420px;
  }
  .skills__input {
    flex: 1;
    height: 32px;
    padding: 0 var(--sp-5);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body-sm) / 1 var(--font-sans);
    outline: none;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .skills__input:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .skills__input::placeholder {
    color: var(--text-ghost);
  }
  .skills__count {
    font-size: var(--fs-label);
    color: var(--text-faint);
  }
  .skills__scroll {
    flex: 1;
    overflow-y: auto;
    padding: var(--sp-7) var(--sp-9);
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-8);
  }
  .skills__grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: var(--sp-5);
  }
  .skills__grid--pad {
    padding: var(--sp-7) var(--sp-9);
  }
  .skills__skel {
    height: 104px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: sk-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes sk-shimmer {
    to {
      background-position: -200% 0;
    }
  }

  /* PINNED REVIEW STRIP — proposals. A warm-edged band with a live pulse that
     reads as "decide on these," set apart from the cool active shelves. The
     left rail in --working tints the whole strip toward attention. */
  .strip {
    position: relative;
    display: flex;
    gap: 0;
    border-radius: var(--r-lg);
    background:
      linear-gradient(180deg, var(--working-bg), transparent 64%),
      var(--bg-raised);
    border: 1px solid color-mix(in srgb, var(--working) 24%, transparent);
    box-shadow: var(--shadow-1);
    overflow: hidden;
  }
  .strip__rail {
    flex: none;
    width: 3px;
    background: linear-gradient(180deg, var(--working), color-mix(in srgb, var(--working) 30%, transparent));
  }
  .strip__inner {
    flex: 1;
    min-width: 0;
    padding: var(--sp-6) var(--sp-6) var(--sp-6) var(--sp-7);
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .strip__head {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .strip__eyebrow {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-3);
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--working);
  }
  .strip__pulse {
    width: 7px;
    height: 7px;
    border-radius: var(--r-full);
    background: var(--working);
    box-shadow: var(--glow-working);
    animation: strip-breathe var(--breath) var(--ease-inout) infinite;
  }
  @keyframes strip-breathe {
    0%,
    100% {
      opacity: 1;
      transform: scale(1);
    }
    50% {
      opacity: 0.45;
      transform: scale(0.82);
    }
  }
  /* Horizontal review row — proposals queue left-to-right and scroll-snap,
     reinforcing "a stack to work through" vs. the static shelf grids. */
  .strip__row {
    display: flex;
    gap: var(--sp-5);
    overflow-x: auto;
    padding-bottom: var(--sp-2);
    scroll-snap-type: x proximity;
  }
  .prop {
    flex: none;
    width: 268px;
    scroll-snap-align: start;
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
    padding: var(--sp-5);
    border-radius: var(--r-md);
    background: var(--bg-base);
    border: 1px solid color-mix(in srgb, var(--working) 16%, var(--border-hairline));
  }
  .prop__top {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .prop__name {
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .prop__desc {
    margin: 0;
    flex: 1;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .prop__actions {
    display: flex;
    gap: var(--sp-3);
    margin-top: auto;
  }
  /* The "+N more to review" tile lives inline at the end of the queue. */
  .prop--more {
    align-items: center;
    justify-content: center;
    gap: var(--sp-2);
    width: 132px;
    border-style: dashed;
    border-color: color-mix(in srgb, var(--working) 30%, transparent);
    background: transparent;
    color: var(--working);
    cursor: pointer;
    font: inherit;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .prop--more:hover {
    background: var(--working-bg);
  }
  .prop--more:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .prop__more-n {
    font-weight: var(--fw-bold);
    font-size: var(--fs-h2);
    color: var(--working);
    line-height: 1;
  }
  .prop__more-label {
    font-size: var(--fs-label);
    color: var(--text-muted);
  }

  /* SOURCE SHELVES — each origin is its own tinted rack. The shelf head dot
     and the per-card left rail carry the source hue (brand / info / neutral)
     so the grid reads as grouped capability racks even without titles. */
  .shelf {
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .shelf__head {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .shelf__dot {
    width: 8px;
    height: 8px;
    border-radius: var(--r-full);
    flex: none;
    background: var(--shelf-tint);
    box-shadow: 0 0 0 4px var(--shelf-tint-faint);
  }
  .shelf__title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-secondary);
  }
  .shelf__n {
    font-size: var(--fs-label);
    color: var(--text-faint);
  }
  .shelf--brand {
    --shelf-tint: var(--brand);
    --shelf-tint-faint: var(--state-selected);
  }
  .shelf--info {
    --shelf-tint: var(--info);
    --shelf-tint-faint: var(--info-bg);
  }
  .shelf--neutral {
    --shelf-tint: var(--text-ghost);
    --shelf-tint-faint: var(--state-hover);
  }

  .sk {
    position: relative;
    padding: var(--sp-5) var(--sp-5) var(--sp-5) var(--sp-6);
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
    min-height: 92px;
  }
  /* Per-card source rail — the quiet origin signature on the left edge. */
  .sk__rail {
    position: absolute;
    inset: var(--sp-5) auto var(--sp-5) 0;
    width: 2px;
    border-radius: var(--r-full);
    background: var(--card-tint);
    opacity: 0.55;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .sk--brand {
    --card-tint: var(--brand);
  }
  .sk--info {
    --card-tint: var(--info);
  }
  .sk--neutral {
    --card-tint: var(--text-ghost);
  }
  :global(.card--interactive:hover) .sk__rail {
    opacity: 1;
  }
  .sk__top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-4);
  }
  .sk__name {
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sk__desc {
    margin: 0;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .skills__empty-note {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    margin: 0;
  }
  .skills__more {
    display: flex;
    justify-content: center;
    margin-top: var(--sp-5);
  }

  /* SLIDE-OVER PREVIEW */
  .sheet__scrim {
    position: fixed;
    inset: 0;
    background: var(--bg-scrim);
    z-index: 50;
    animation: scrim-in var(--dur-fast) var(--ease-out);
  }
  .sheet__scrim:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .sheet {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(560px, 80vw);
    background: var(--bg-raised);
    border-left: 1px solid var(--border-subtle);
    box-shadow: var(--shadow-3);
    z-index: 51;
    display: flex;
    flex-direction: column;
    padding: var(--sp-7);
    gap: var(--sp-5);
    animation: sheet-in var(--dur-base) var(--ease-out);
  }
  .sheet__head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--sp-5);
  }
  .sheet__title-wrap {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    min-width: 0;
  }
  .sheet__title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-h2) / 1.2 var(--font-display);
    color: var(--text-primary);
    letter-spacing: var(--ls-heading);
  }
  .sheet__desc {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
  }
  .sheet__path {
    font: var(--fw-regular) var(--fs-micro) / 1.4 var(--font-mono);
    color: var(--text-faint);
    word-break: break-all;
  }
  .sheet__body {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    border-top: 1px solid var(--divider);
    padding-top: var(--sp-5);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-prose);
  }
  .sheet__loading {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  @keyframes scrim-in {
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
    .skills__skel,
    .strip__pulse {
      animation: none;
    }
  }
</style>
