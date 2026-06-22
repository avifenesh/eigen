<script lang="ts">
  // Routing & Models — the catalog of every route candidate and which providers
  // are credentialed in this environment. A provider rail (credential status +
  // model count) filters the model grid; each model card surfaces its
  // capabilities (context window, prompt cache, 1M context, reasoning/effort,
  // live search) and whether its provider is currently usable. Read-only,
  // local + fast (no network probe).
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { RoutingDTO, ModelDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let data = $state<RoutingDTO | null>(null);
  let loading = $state(true);
  let query = $state("");
  let provFilter = $state<string | null>(null);
  let onlyAvailable = $state(false);

  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    try {
      const d = await Bridge.Routing();
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
      loadSeq++;
    };
  });

  const models = $derived.by(() => {
    const all = data?.models ?? [];
    const q = query.trim().toLowerCase();
    return all.filter((m) => {
      if (provFilter && m.provider !== provFilter) return false;
      if (onlyAvailable && !m.available) return false;
      if (q && !m.id.toLowerCase().includes(q) && !m.provider.toLowerCase().includes(q)) return false;
      return true;
    });
  });

  function win(n: number): string {
    if (n <= 0) return "—";
    if (n >= 1000) return `${Math.round(n / 1000)}k`;
    return String(n);
  }
  function caps(m: ModelDTO): string[] {
    const c: string[] = [];
    if (m.cache) c.push("cache");
    if (m.context1m) c.push("1M");
    if (m.reasoning) c.push(m.effort ? `effort:${m.effort}` : "reasoning");
    if (m.search) c.push("search");
    return c;
  }
</script>

<div class="route">
  <aside class="route__rail">
    <div class="route__rail-label">Providers</div>
    <button class="prov" class:prov--on={provFilter === null} onclick={() => (provFilter = null)}>
      <span class="prov__name">All</span>
      {#if data}<span class="prov__n tnum">{data.models.length}</span>{/if}
    </button>
    {#each data?.providers ?? [] as p (p.name)}
      <button class="prov" class:prov--on={provFilter === p.name} onclick={() => (provFilter = provFilter === p.name ? null : p.name)}>
        <StatusDot state={p.credentialed ? "ok" : "idle"} size={7} />
        <span class="prov__name">{p.name}</span>
        <span class="prov__n tnum">{p.modelCount}</span>
      </button>
    {/each}
    <label class="route__toggle">
      <input type="checkbox" bind:checked={onlyAvailable} />
      <span>Credentialed only</span>
    </label>
  </aside>

  <div class="route__main">
    <header class="route__head">
      <input class="route__search" type="text" placeholder="Filter models…" bind:value={query} aria-label="Filter models" />
      {#if data}<span class="route__count tnum">{models.length}</span>{/if}
    </header>

    {#if loading && !data}
      <div class="route__grid route__grid--pad">
        {#each Array(6) as _, i (i)}<div class="route__skel"></div>{/each}
      </div>
    {:else if !data || data.models.length === 0}
      <EmptyState glyph="⇄" title="No models in the catalog" line="The curated model catalog is empty." />
    {:else if models.length === 0}
      <div class="route__grid route__grid--pad"><p class="route__empty-note">No models match the current filters.</p></div>
    {:else}
      <div class="route__grid">
        {#each models as m (m.provider + ":" + m.id)}
          <Card>
            <div class="model" class:model--off={!m.available}>
              <div class="model__top">
                <StatusDot state={m.available ? "ok" : "idle"} size={7} />
                <span class="model__id" title={m.id}>{m.id}</span>
              </div>
              <div class="model__meta">
                <Badge tone="neutral" truncate>{m.provider}</Badge>
                <span class="model__win tnum">{win(m.contextWindow)} ctx</span>
                {#if !m.available}<span class="model__off">no credentials</span>{/if}
              </div>
              {#if caps(m).length > 0}
                <div class="model__caps">
                  {#each caps(m) as cap (cap)}
                    <span class="cap">{cap}</span>
                  {/each}
                </div>
              {/if}
            </div>
          </Card>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .route {
    height: 100%;
    display: flex;
    min-height: 0;
  }
  .route__rail {
    width: 220px;
    flex: none;
    border-right: 1px solid var(--border-hairline);
    background: var(--bg-well);
    padding: var(--sp-6) var(--sp-5);
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    overflow-y: auto;
  }
  .route__rail-label {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    padding: 0 var(--sp-4);
    margin-bottom: var(--sp-3);
  }
  .prov {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    height: 32px;
    padding: 0 var(--sp-4);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    border-radius: var(--r-sm);
    cursor: pointer;
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-sans);
    text-align: left;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .prov:hover {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .prov:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .prov--on {
    background: var(--state-selected);
    color: var(--brand-bright);
  }
  .prov__name {
    flex: 1;
    text-transform: capitalize;
  }
  .prov__n {
    font-size: var(--fs-micro);
    color: var(--text-faint);
  }
  .prov--on .prov__n {
    color: var(--brand);
  }
  .route__toggle {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    margin-top: var(--sp-5);
    padding: 0 var(--sp-4);
    font-size: var(--fs-body-sm);
    color: var(--text-muted);
    cursor: pointer;
  }
  .route__toggle input {
    accent-color: var(--brand);
  }
  .route__main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .route__head {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    padding: var(--sp-6) var(--sp-9);
    border-bottom: 1px solid var(--border-hairline);
  }
  .route__search {
    flex: 1;
    max-width: 420px;
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
  .route__search:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .route__search::placeholder {
    color: var(--text-ghost);
  }
  .route__count {
    font-size: var(--fs-label);
    color: var(--text-faint);
  }
  .route__grid {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-7) var(--sp-9);
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: var(--sp-5);
    align-content: start;
  }
  .route__grid--pad {
    display: block;
  }
  .route__skel {
    height: 104px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: route-shimmer 1.4s ease-in-out infinite;
    margin-bottom: var(--sp-5);
  }
  @keyframes route-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .route__empty-note {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    margin: 0;
  }
  .model {
    padding: var(--sp-5);
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .model--off {
    opacity: 0.62;
  }
  .model__top {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .model__id {
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .model__meta {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .model__win {
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .model__off {
    font-size: var(--fs-micro);
    color: var(--text-faint);
  }
  .model__caps {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-2);
  }
  .cap {
    display: inline-flex;
    align-items: center;
    height: 18px;
    padding: 0 var(--sp-3);
    border-radius: var(--r-xs);
    background: var(--bg-overlay);
    border: 1px solid var(--border-hairline);
    font: var(--fw-medium) var(--fs-micro) / 1 var(--font-sans);
    color: var(--text-secondary);
  }
  @media (prefers-reduced-motion: reduce) {
    .route__skel {
      animation: none;
    }
  }
</style>
