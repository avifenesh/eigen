<script lang="ts">
  // Skills — the capability gallery. Discovered SKILL.md skills as cards
  // (grouped by source), plus dream-proposed drafts awaiting accept/reject.
  // Clicking a skill opens a slide-over with its rendered body. Skills are
  // local files; the bridge reads them directly.
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
        <section class="skills__section">
          <div class="skills__section-head">
            <h2 class="skills__section-title">Proposed</h2>
            <Badge tone="warn">{proposals.length} awaiting review</Badge>
          </div>
          <div class="skills__grid">
            {#each visibleProposals as p (p.name)}
              <Card>
                <div class="sk sk--proposal">
                  <div class="sk__top">
                    <span class="sk__name">{p.name}</span>
                    <Badge tone="warn">proposed</Badge>
                  </div>
                  <p class="sk__desc">{p.description}</p>
                  <div class="sk__actions">
                    <Button variant="primary" size="sm" loading={acting[p.name]} onclick={() => accept(p.name)}>
                      Accept
                    </Button>
                    <Button variant="ghost" size="sm" disabled={acting[p.name]} onclick={() => reject(p.name)}>
                      Reject
                    </Button>
                  </div>
                </div>
              </Card>
            {/each}
          </div>
          {#if proposalsShown < proposals.length}
            <div class="skills__more">
              <Button variant="ghost" size="sm" onclick={() => (proposalsShown += PAGE)}>
                Show {Math.min(PAGE, proposals.length - proposalsShown)} more · {proposals.length - proposalsShown} remaining
              </Button>
            </div>
          {/if}
        </section>
      {/if}

      <section class="skills__section">
        <div class="skills__section-head">
          <h2 class="skills__section-title">Active</h2>
          <span class="skills__count tnum">{filtered.length}</span>
        </div>
        {#if filtered.length === 0}
          <p class="skills__empty-note">No skills match “{query}”.</p>
        {:else}
          <div class="skills__grid">
            {#each visibleActive as s (s.name)}
              <Card interactive onclick={() => preview(s)} title={s.path}>
                <div class="sk">
                  <div class="sk__top">
                    <span class="sk__name">{s.name}</span>
                    <Badge tone={sourceTone(s.source)}>{s.source}</Badge>
                  </div>
                  <p class="sk__desc">{s.description}</p>
                </div>
              </Card>
            {/each}
          </div>
          {#if activeShown < filtered.length}
            <div class="skills__more">
              <Button variant="ghost" size="sm" onclick={() => (activeShown += PAGE)}>
                Show {Math.min(PAGE, filtered.length - activeShown)} more · {filtered.length - activeShown} remaining
              </Button>
            </div>
          {/if}
        {/if}
      </section>
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
  .skills__section {
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .skills__section-head {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .skills__section-title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
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
  .sk {
    padding: var(--sp-5);
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
    min-height: 92px;
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
  .sk__actions {
    display: flex;
    gap: var(--sp-3);
    margin-top: auto;
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
    .skills__skel {
      animation: none;
    }
  }
</style>
