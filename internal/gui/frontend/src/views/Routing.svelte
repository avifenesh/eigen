<script lang="ts">
  // Routing & Models — the catalog of every route candidate and which providers
  // are credentialed in this environment. A provider rail (credential status +
  // model count) filters the model grid; each model card surfaces its
  // capabilities (context window, prompt cache, 1M context, reasoning/effort,
  // live search) and whether its provider is currently usable. Read-only,
  // local + fast (no network probe).
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { RoutingDTO, ModelDTO, RouteStatsDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Button from "$lib/components/Button.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let data = $state<RoutingDTO | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let query = $state("");
  let provFilter = $state<string | null>(null);
  let onlyAvailable = $state(false);

  // Live routing-health strip: how routing actually behaved (from the
  // observability log), shown above the static catalog so the view leads with
  // behaviour. Fetched once with the same disposed-guard Observe uses.
  let routes = $state<RouteStatsDTO | null>(null);
  let disposed = false;

  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    error = null;
    try {
      const d = await Bridge.Routing();
      if (seq === loadSeq) data = d;
    } catch (e) {
      if (seq === loadSeq) error = e instanceof Error ? e.message : String(e);
    } finally {
      if (seq === loadSeq) loading = false;
    }
  }
  async function loadRoutes() {
    try {
      const sum = await Bridge.ObserveSummary(5000);
      if (!disposed && sum && sum.available && sum.records > 0) routes = sum.routes;
    } catch {
      // Health strip is supplementary; a missing observability log is not an error here.
    }
  }
  $effect(() => {
    load();
    loadRoutes();
    return () => {
      loadSeq++;
      disposed = true;
    };
  });

  // The routing-health strip only earns its place once routing has happened.
  const routeTotal = $derived(
    routes ? routes.routed + routes.skipped + routes.assessed + routes.orchestrator : 0,
  );
  const routeStages = $derived(
    routes
      ? ([
          { key: "routed", label: "routed", n: routes.routed, tone: "brand" },
          { key: "assessed", label: "assessed", n: routes.assessed, tone: "accent" },
          { key: "skipped", label: "skipped", n: routes.skipped, tone: "muted" },
          { key: "orchestrator", label: "orchestrator", n: routes.orchestrator, tone: "warn" },
        ] as const)
      : [],
  );
  const routeModelMax = $derived(
    routes ? routes.byModel.reduce((mx, m) => Math.max(mx, m.count), 1) : 1,
  );

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
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n % 1_000_000 ? 1 : 0)}M`;
    if (n >= 1000) return `${Math.round(n / 1000)}k`;
    return String(n);
  }
  function caps(m: ModelDTO): string[] {
    const c: string[] = [];
    if (m.cache) c.push("cache");
    if (m.context1m) c.push("1M");
    if (m.reasoning) c.push(m.effort ? `effort:${m.effort}` : "reasoning");
    if (m.search) c.push("search");
    if (m.vision) c.push("vision");
    if (m.social) c.push("social");
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

    {#if routes && routeTotal > 0}
      <section class="health" aria-label="Routing health">
        <div class="health__lead">
          <span class="health__eyebrow">How routing behaved</span>
          <span class="health__total tnum">{routeTotal.toLocaleString()}<span class="health__total-u"> decisions</span></span>
        </div>
        <div class="health__flow" role="img" aria-label="Routing decision breakdown">
          {#each routeStages as st (st.key)}
            {#if st.n > 0}
              <div class="flow flow--{st.tone}" style="flex:{st.n}" title="{st.label}: {st.n}">
                <span class="flow__n tnum">{st.n.toLocaleString()}</span>
                <span class="flow__l">{st.label}</span>
              </div>
            {/if}
          {/each}
        </div>
        {#if routes.byModel.length > 0}
          <div class="health__models">
            <span class="health__models-label">routed to</span>
            <div class="health__bars">
              {#each routes.byModel as m (m.name)}
                <div class="hbar" title="{m.name}: {m.count}">
                  <span class="hbar__name" title={m.name}>{m.name}</span>
                  <div class="hbar__track"><span style="width:{(m.count / routeModelMax) * 100}%"></span></div>
                  <span class="hbar__n tnum">{m.count.toLocaleString()}</span>
                </div>
              {/each}
            </div>
          </div>
        {/if}
        <div class="health__rule"><span>catalog</span></div>
      </section>
    {/if}

    {#if loading && !data}
      <div class="route__grid route__grid--pad">
        {#each Array(6) as _, i (i)}<div class="route__skel"></div>{/each}
      </div>
    {:else if error && !data}
      <EmptyState glyph="⇄" title="Couldn't load routing" line={error}>
        {#snippet action()}
          <Button variant="secondary" onclick={() => load()}>Retry</Button>
        {/snippet}
      </EmptyState>
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
  /* Routing-health strip — leads with how routing behaves before the catalog. */
  .health {
    flex: none;
    padding: var(--sp-6) var(--sp-9) 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .health__lead {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--sp-5);
  }
  .health__eyebrow {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .health__total {
    font: var(--fw-semibold) var(--fs-h3) / 1 var(--font-sans);
    color: var(--text-primary);
  }
  .health__total-u {
    font-size: var(--fs-label);
    font-weight: var(--fw-regular);
    color: var(--text-muted);
  }
  .health__flow {
    display: flex;
    gap: var(--sp-2);
    height: 46px;
  }
  .flow {
    min-width: 0;
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 1px;
    padding: 0 var(--sp-4);
    border-radius: var(--r-sm);
    overflow: hidden;
    position: relative;
    border: 1px solid var(--border-hairline);
  }
  .flow__n {
    font: var(--fw-semibold) var(--fs-body-sm) / 1 var(--font-sans);
    color: var(--text-primary);
    white-space: nowrap;
  }
  .flow__l {
    font-size: var(--fs-micro);
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .flow--brand {
    background: linear-gradient(180deg, rgba(105, 194, 184, 0.16), rgba(105, 194, 184, 0.06));
    border-color: var(--border-brand-faint);
  }
  .flow--brand .flow__n {
    color: var(--brand-bright);
  }
  .flow--accent {
    background: linear-gradient(180deg, rgba(111, 155, 208, 0.14), rgba(111, 155, 208, 0.05));
    border-color: rgba(111, 155, 208, 0.22);
  }
  .flow--accent .flow__n {
    color: var(--accent-bright);
  }
  .flow--muted {
    background: var(--bg-raised);
  }
  .flow--warn {
    background: linear-gradient(180deg, rgba(224, 179, 106, 0.14), rgba(224, 179, 106, 0.05));
    border-color: rgba(224, 179, 106, 0.22);
  }
  .flow--warn .flow__n {
    color: var(--warn);
  }
  .health__models {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .health__models-label {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .health__bars {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: var(--sp-2) var(--sp-6);
  }
  .hbar {
    display: grid;
    grid-template-columns: minmax(0, 130px) 1fr auto;
    align-items: center;
    gap: var(--sp-4);
  }
  .hbar__name {
    font-size: var(--fs-body-sm);
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hbar__track {
    height: 5px;
    background: var(--bg-inset);
    border-radius: var(--r-full);
    overflow: hidden;
  }
  .hbar__track span {
    display: block;
    height: 100%;
    background: var(--spectrum);
    border-radius: var(--r-full);
    transition: width var(--dur-slow) var(--ease-out);
  }
  .hbar__n {
    font-size: var(--fs-label);
    color: var(--text-muted);
    min-width: 32px;
    text-align: right;
  }
  .health__rule {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    margin-top: var(--sp-1);
  }
  .health__rule::before,
  .health__rule::after {
    content: "";
    flex: 1;
    height: 1px;
    background: var(--border-hairline);
  }
  .health__rule span {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-ghost);
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
    .hbar__track span {
      transition: none;
    }
  }
</style>
