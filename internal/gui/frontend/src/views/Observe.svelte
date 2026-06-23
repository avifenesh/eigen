<script lang="ts">
  // Observe — telemetry. Two layers: a live OVERVIEW from the 1Hz DaemonStats
  // stream (also the leak HUD — watch Views/Goroutines stay flat), and
  // historical sub-views (Routes / Tools / Models / Hooks / Errors) read from
  // the local metadata-only observability log. Tabs switch between them; the
  // historical summary is fetched lazily the first time a non-overview tab opens.
  import { daemon } from "$lib/stores/daemon.svelte";
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { ObserveSummaryDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  type Tab = "overview" | "routes" | "tools" | "models" | "hooks" | "subagents" | "errors";
  let tab = $state<Tab>("overview");

  const s = $derived(daemon.stats);

  // Historical summary, fetched once on first non-overview tab and cached.
  // `disposed` guards the single fetch from writing after unmount; the
  // summary||summaryLoading guard prevents duplicate fetches across tab
  // switches, so no per-call sequence token is needed here.
  let summary = $state<ObserveSummaryDTO | null>(null);
  let summaryLoading = $state(false);
  let disposed = false;
  async function loadSummary() {
    if (summary || summaryLoading) return;
    summaryLoading = true;
    try {
      const d = await Bridge.ObserveSummary(5000);
      if (!disposed) summary = d;
    } catch (e) {
      if (!disposed) toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      if (!disposed) summaryLoading = false;
    }
  }
  $effect(() => {
    if (tab !== "overview") loadSummary();
  });
  $effect(() => {
    return () => {
      disposed = true;
    };
  });

  function mb(bytes?: number): string {
    if (!bytes) return "0";
    return (bytes / (1024 * 1024)).toFixed(0);
  }
  function dur(sec?: number): string {
    if (!sec) return "—";
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }
  const cacheHit = $derived(
    s && (s.input_tokens ?? 0) > 0
      ? Math.round(((s.cache_read_tokens ?? 0) / (s.input_tokens ?? 1)) * 100)
      : 0,
  );
  // Cache-hit gauge geometry: a 270° arc (gap at the bottom). The fill circle is
  // dashed to (cacheHit/100)·arcLen so the spectrum sweeps with the hit rate.
  const arcR = 52;
  const arcCirc = 2 * Math.PI * arcR;
  const arcLen = arcCirc * 0.75;
  const arcGap = arcCirc - arcLen;
  function ms(d?: number): string {
    if (!d) return "—";
    if (d < 1000) return `${d}ms`;
    return `${(d / 1000).toFixed(1)}s`;
  }
  function k(n: number): string {
    return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);
  }
  // Short git SHA for the runtime panel; daemon embeds the full revision.
  function shortRev(rev?: string): string {
    return rev ? rev.slice(0, 7) : "";
  }

  const tabs: { key: Tab; label: string }[] = [
    { key: "overview", label: "Overview" },
    { key: "routes", label: "Routes" },
    { key: "tools", label: "Tools" },
    { key: "models", label: "Models" },
    { key: "hooks", label: "Hooks" },
    { key: "subagents", label: "Subagents" },
    { key: "errors", label: "Errors" },
  ];

  // Max for proportional bars in a count list.
  function maxCount(items: { count: number }[]): number {
    return items.reduce((mx, i) => Math.max(mx, i.count), 1);
  }
</script>

<div class="obs">
  <header class="obs__head">
    <div class="obs__tabs" role="tablist" aria-label="Telemetry view">
      {#each tabs as t (t.key)}
        <button class="obs__tab" class:obs__tab--on={tab === t.key} role="tab" aria-selected={tab === t.key} onclick={() => (tab = t.key)}>
          {t.label}
        </button>
      {/each}
    </div>
  </header>

  <div class="obs__scroll selectable">
    {#if tab === "overview"}
      {#if !s}
        <EmptyState glyph="◉" title="Waiting for telemetry" line="The daemon pushes a stats snapshot every second." />
      {:else}
        <section class="obs__kpis">
          <Card><div class="kpi"><div class="kpi__v tnum">{s.sessions}</div><div class="kpi__l">sessions</div></div></Card>
          <Card live={s.running_turns > 0}><div class="kpi"><div class="kpi__v tnum" class:kpi__v--live={s.running_turns > 0}>{s.running_turns}</div><div class="kpi__l">running turns</div></div></Card>
          <Card live={s.bg_tasks > 0}><div class="kpi"><div class="kpi__v tnum" class:kpi__v--live={s.bg_tasks > 0}>{s.bg_tasks}</div><div class="kpi__l">background tasks</div></div></Card>
          <Card><div class="kpi"><div class="kpi__v tnum">{s.views}</div><div class="kpi__l">attached views</div></div></Card>
        </section>
        <div class="obs__cols">
          <Card>
            <div class="panel">
              <div class="panel__title">runtime</div>
              <dl class="kv">
                <dt>uptime</dt><dd class="tnum">{dur(s.uptime_sec)}</dd>
                <dt>goroutines</dt><dd class="tnum">{s.goroutines}</dd>
                <dt>heap alloc</dt><dd class="tnum">{mb(s.heap_alloc_b)} MB</dd>
                <dt>rss</dt><dd class="tnum">{mb(s.rss_b)} MB</dd>
                <dt>gc cycles</dt><dd class="tnum">{s.num_gc}</dd>
                {#if s.version}
                  <dt>eigen</dt>
                  <dd>
                    {s.version}{#if s.vcs_revision}
                      <span class="rev" title={s.vcs_revision}>@{shortRev(s.vcs_revision)}{#if s.vcs_modified}<span class="rev__dirty" title="built with uncommitted changes">*</span>{/if}</span>
                    {/if}
                  </dd>
                {/if}
                {#if s.go_version}<dt>go</dt><dd>{s.go_version}</dd>{/if}
              </dl>
            </div>
          </Card>
          <Card>
            <div class="panel">
              <div class="panel__title">tokens (live)</div>
              <div class="tokens">
                <div class="gauge" role="img" aria-label="{cacheHit}% cache hit rate">
                  <svg class="gauge__svg" viewBox="0 0 120 120" aria-hidden="true">
                    <defs>
                      <!-- SVG strokes cannot consume the --spectrum CSS gradient token;
                           these stops replicate its documented stops (brand-strong → brand → mid → accent). -->
                      <linearGradient id="cacheArc" x1="0%" y1="0%" x2="100%" y2="100%">
                        <stop offset="0%" stop-color="#3e9e96" />
                        <stop offset="34%" stop-color="#69c2b8" />
                        <stop offset="64%" stop-color="#5bb6c9" />
                        <stop offset="100%" stop-color="#6f9bd0" />
                      </linearGradient>
                    </defs>
                    <circle class="gauge__track" cx="60" cy="60" r={arcR} stroke-dasharray="{arcLen} {arcGap}" />
                    <circle
                      class="gauge__fill"
                      cx="60"
                      cy="60"
                      r={arcR}
                      stroke="url(#cacheArc)"
                      stroke-dasharray="{(cacheHit / 100) * arcLen} {arcCirc}"
                    />
                  </svg>
                  <div class="gauge__center">
                    <span class="gauge__pct tnum">{cacheHit}<span class="gauge__pct-u">%</span></span>
                    <span class="gauge__cap">cache hit</span>
                  </div>
                </div>
                <dl class="kv tokens__kv">
                  <dt>input</dt><dd class="tnum">{(s.input_tokens ?? 0).toLocaleString()}</dd>
                  <dt>output</dt><dd class="tnum">{(s.output_tokens ?? 0).toLocaleString()}</dd>
                  <dt>cache read</dt><dd class="tnum">{(s.cache_read_tokens ?? 0).toLocaleString()}</dd>
                  <dt>cache write</dt><dd class="tnum">{(s.cache_write_tokens ?? 0).toLocaleString()}</dd>
                </dl>
              </div>
            </div>
          </Card>
        </div>
      {/if}
    {:else if summaryLoading && !summary}
      <div class="obs__loading">Loading telemetry log…</div>
    {:else if !summary || !summary.available || summary.records === 0}
      <EmptyState glyph="◉" title="No telemetry log yet" line="As sessions run, the metadata-only observability log records tool/model/route/hook activity here." />
    {:else if tab === "routes"}
      <div class="obs__single">
        <section class="obs__kpis">
          <Card><div class="kpi"><div class="kpi__v tnum">{summary.routes.routed}</div><div class="kpi__l">routed</div></div></Card>
          <Card><div class="kpi"><div class="kpi__v tnum">{summary.routes.assessed}</div><div class="kpi__l">assessed</div></div></Card>
          <Card><div class="kpi"><div class="kpi__v tnum">{summary.routes.skipped}</div><div class="kpi__l">skipped</div></div></Card>
          <Card><div class="kpi"><div class="kpi__v tnum">{summary.routes.orchestrator}</div><div class="kpi__l">orchestrator</div></div></Card>
        </section>
        <div class="obs__cols">
          {#each [["By model", summary.routes.byModel], ["By kind", summary.routes.byKind], ["By difficulty", summary.routes.byDifficulty], ["Skip reasons", summary.routes.skipReasons]] as [title, items] (title)}
            {@const list = items as { name: string; count: number }[]}
            {#if list.length > 0}
              {@const mx = maxCount(list)}
              <Card>
                <div class="panel">
                  <div class="panel__title">{title}</div>
                  <div class="bars">
                    {#each list as r (r.name)}
                      <div class="bar">
                        <span class="bar__label" title={r.name}>{r.name}</span>
                        <div class="bar__track"><span style="width:{(r.count / mx) * 100}%"></span></div>
                        <span class="bar__n tnum">{r.count}</span>
                      </div>
                    {/each}
                  </div>
                </div>
              </Card>
            {/if}
          {/each}
        </div>
      </div>
    {:else if tab === "tools"}
      {@const toolMax = summary.tools.reduce((mx, t) => Math.max(mx, t.calls), 1)}
      <Card>
        <table class="tbl">
          <thead><tr><th>tool</th><th class="tbl__bar-h">calls</th><th class="tbl__num">errors</th><th class="tbl__num">avg</th></tr></thead>
          <tbody>
            {#each summary.tools as t (t.name)}
              {@const errRate = t.calls > 0 ? t.errors / t.calls : 0}
              <tr>
                <td class="tbl__name">{t.name}</td>
                <td class="tbl__bar">
                  <div class="mbar"><span style="width:{(t.calls / toolMax) * 100}%"></span></div>
                  <span class="mbar__n tnum">{k(t.calls)}</span>
                </td>
                <td
                  class="tbl__num tnum"
                  class:tbl__err={t.errors > 0}
                  class:tbl__err--hot={errRate >= 0.25}
                  title={t.errors > 0 ? `${Math.round(errRate * 100)}% of calls` : undefined}
                >{t.errors}</td>
                <td class="tbl__num tnum">{ms(t.calls > 0 ? Math.round(t.durationMs / t.calls) : 0)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </Card>
    {:else if tab === "models"}
      {@const turnMax = summary.models.reduce((mx, m) => Math.max(mx, m.turns), 1)}
      <Card>
        <table class="tbl">
          <thead><tr><th>model</th><th class="tbl__bar-h">turns</th><th class="tbl__num">in</th><th class="tbl__num">out</th><th class="tbl__num">cache rd</th></tr></thead>
          <tbody>
            {#each summary.models as m (m.name)}
              <tr>
                <td class="tbl__name">{m.name}</td>
                <td class="tbl__bar">
                  <div class="mbar"><span style="width:{(m.turns / turnMax) * 100}%"></span></div>
                  <span class="mbar__n tnum">{m.turns}</span>
                </td>
                <td class="tbl__num tnum">{k(m.inTokens)}</td>
                <td class="tbl__num tnum">{k(m.outTokens)}</td>
                <td class="tbl__num tnum">{k(m.cacheReadTokens)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </Card>
    {:else if tab === "hooks"}
      {#if summary.hooks.length === 0}
        <p class="obs__empty-note">No hook activity recorded.</p>
      {:else}
        {@const hookMax = summary.hooks.reduce((mx, h) => Math.max(mx, h.starts), 1)}
        <Card>
          <table class="tbl">
            <thead><tr><th>hook</th><th class="tbl__bar-h">starts</th><th class="tbl__num">done</th><th class="tbl__num">errors</th></tr></thead>
            <tbody>
              {#each summary.hooks as h (h.name)}
                {@const errRate = h.starts > 0 ? h.errors / h.starts : 0}
                <tr>
                  <td class="tbl__name">{h.name}</td>
                  <td class="tbl__bar">
                    <div class="mbar"><span style="width:{(h.starts / hookMax) * 100}%"></span></div>
                    <span class="mbar__n tnum">{h.starts}</span>
                  </td>
                  <td class="tbl__num tnum">{h.done}</td>
                  <td
                    class="tbl__num tnum"
                    class:tbl__err={h.errors > 0}
                    class:tbl__err--hot={errRate >= 0.25}
                  >{h.errors}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </Card>
      {/if}
    {:else if tab === "subagents"}
      {@const sa = summary.subagents}
      <div class="obs__single">
        <section class="obs__kpis">
          <Card live={sa.taskCalls > 0}>
            <div class="kpi">
              <div class="kpi__v tnum" class:kpi__v--live={sa.taskCalls > 0}>{k(sa.taskCalls)}</div>
              <div class="kpi__l">task calls</div>
            </div>
          </Card>
          <Card live={sa.groupCalls > 0}>
            <div class="kpi">
              <div class="kpi__v tnum" class:kpi__v--live={sa.groupCalls > 0}>{k(sa.groupCalls)}</div>
              <div class="kpi__l">group calls</div>
            </div>
          </Card>
          <Card live={sa.mutatingCalls > 0}>
            <div class="kpi">
              <div class="kpi__v tnum" class:kpi__v--live={sa.mutatingCalls > 0}>{k(sa.mutatingCalls)}</div>
              <div class="kpi__l">mutating calls</div>
            </div>
          </Card>
          <Card live={sa.backgroundDone > 0}>
            <div class="kpi">
              <div class="kpi__v tnum" class:kpi__v--live={sa.backgroundDone > 0}>{k(sa.backgroundDone)}</div>
              <div class="kpi__l">background done</div>
            </div>
          </Card>
        </section>
        <Card>
          <div class="panel">
            <div class="panel__title">delegation</div>
            <dl class="kv">
              <dt>task calls</dt>
              <dd class="tnum">{(sa.taskCalls).toLocaleString()}</dd>
              <dt>task errors</dt>
              <dd class="tnum" class:tbl__err={sa.taskErrors > 0}>{sa.taskErrors}</dd>
              <dt>group calls</dt>
              <dd class="tnum">{(sa.groupCalls).toLocaleString()}</dd>
              <dt>group errors</dt>
              <dd class="tnum" class:tbl__err={sa.groupErrors > 0}>{sa.groupErrors}</dd>
              <dt>mutating calls</dt>
              <dd class="tnum">{(sa.mutatingCalls).toLocaleString()}</dd>
              <dt>mutating errors</dt>
              <dd class="tnum" class:tbl__err={sa.mutatingErrors > 0}>{sa.mutatingErrors}</dd>
              <dt>background dispatched</dt>
              <dd class="tnum">{(sa.backgroundDone).toLocaleString()}</dd>
            </dl>
          </div>
        </Card>
      </div>
    {:else if tab === "errors"}
      {#if summary.errors.length === 0}
        <EmptyState glyph="✓" title="No errors recorded" line="The observability log shows a clean run." />
      {:else}
        {@const mx = maxCount(summary.errors)}
        <Card>
          <div class="panel">
            <div class="panel__title">errors by kind</div>
            <div class="bars">
              {#each summary.errors as e (e.name)}
                <div class="bar">
                  <span class="bar__label" title={e.name}>{e.name}</span>
                  <div class="bar__track"><span class="bar__track--err" style="width:{(e.count / mx) * 100}%"></span></div>
                  <span class="bar__n tnum">{e.count}</span>
                </div>
              {/each}
            </div>
          </div>
        </Card>
      {/if}
    {/if}
  </div>
</div>

<style>
  .obs {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .obs__head {
    flex: none;
    padding: var(--sp-6) var(--sp-9) 0;
    border-bottom: 1px solid var(--border-hairline);
  }
  .obs__tabs {
    display: flex;
    gap: var(--sp-2);
  }
  .obs__tab {
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
  .obs__tab:hover {
    color: var(--text-primary);
  }
  .obs__tab:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .obs__tab--on {
    color: var(--brand-bright);
    border-bottom-color: var(--brand);
  }
  .obs__scroll {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-7) var(--sp-9);
    display: flex;
    flex-direction: column;
    gap: var(--sp-6);
  }
  .obs__single {
    display: flex;
    flex-direction: column;
    gap: var(--sp-6);
  }
  .obs__loading,
  .obs__empty-note {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  .obs__kpis {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
    gap: var(--sp-5);
  }
  .kpi {
    padding: var(--sp-6);
  }
  .kpi__v {
    font: var(--fw-bold) var(--fs-display) / 1 var(--font-display);
    color: var(--text-primary);
  }
  /* Teal is reserved for counts that mean active work right now; a settled or
     zero metric stays neutral so the brand color always means "alive". */
  .kpi__v--live {
    color: var(--brand-bright);
  }
  .kpi__l {
    margin-top: var(--sp-3);
    font-size: var(--fs-label);
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }
  .obs__cols {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
    gap: var(--sp-5);
  }
  .panel {
    padding: var(--sp-6);
  }
  .panel__title {
    font-size: var(--fs-label);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    margin-bottom: var(--sp-5);
  }
  .kv {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: var(--sp-4) var(--sp-6);
    margin: 0;
  }
  .kv dt {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  .kv dd {
    margin: 0;
    color: var(--text-primary);
    font-size: var(--fs-body-sm);
    font-weight: var(--fw-medium);
    text-align: right;
  }
  .rev {
    margin-left: var(--sp-2);
    color: var(--text-faint);
    font-weight: var(--fw-regular);
  }
  .rev__dirty {
    color: var(--brand-bright);
  }
  /* Tokens panel — cache-hit arc gauge beside the token breakdown. */
  .tokens {
    display: flex;
    align-items: center;
    gap: var(--sp-8);
  }
  .tokens__kv {
    flex: 1;
    min-width: 0;
    align-self: center;
  }
  .gauge {
    position: relative;
    width: 116px;
    height: 116px;
    flex: none;
  }
  .gauge__svg {
    width: 100%;
    height: 100%;
    /* Rotate so the 270° gap sits at the bottom and the fill sweeps from lower-left. */
    transform: rotate(135deg);
  }
  .gauge__track,
  .gauge__fill {
    fill: none;
    stroke-width: 9;
    stroke-linecap: round;
  }
  .gauge__track {
    stroke: var(--bg-inset);
  }
  .gauge__fill {
    transition: stroke-dasharray var(--dur-slow) var(--ease-out);
  }
  .gauge__center {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-2);
  }
  .gauge__pct {
    font: var(--fw-bold) var(--fs-h1) / 1 var(--font-display);
    color: var(--brand-bright);
    letter-spacing: var(--ls-heading);
  }
  .gauge__pct-u {
    font-size: var(--fs-h3);
    font-weight: var(--fw-medium);
    color: var(--text-muted);
  }
  .gauge__cap {
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }

  /* Proportional bar lists (routes / errors). */
  .bars {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .bar {
    display: grid;
    grid-template-columns: minmax(0, 140px) 1fr auto;
    align-items: center;
    gap: var(--sp-4);
  }
  .bar__label {
    font-size: var(--fs-body-sm);
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bar__track {
    height: 6px;
    background: var(--bg-inset);
    border-radius: var(--r-full);
    overflow: hidden;
  }
  .bar__track span {
    display: block;
    height: 100%;
    background: var(--brand);
    border-radius: var(--r-full);
  }
  .bar__track--err {
    background: var(--error) !important;
  }
  .bar__n {
    font-size: var(--fs-label);
    color: var(--text-muted);
    min-width: 40px;
    text-align: right;
  }

  /* Tables (tools / models / hooks). */
  .tbl {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-body-sm);
  }
  .tbl thead th {
    text-align: right;
    padding: var(--sp-4) var(--sp-5);
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    border-bottom: 1px solid var(--border-hairline);
  }
  .tbl thead th:first-child {
    text-align: left;
  }
  .tbl__num {
    text-align: right;
  }
  .tbl tbody td {
    padding: var(--sp-4) var(--sp-5);
    border-bottom: 1px solid var(--divider);
    color: var(--text-primary);
  }
  .tbl tbody tr:last-child td {
    border-bottom: none;
  }
  .tbl__name {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }
  .tbl__err {
    color: var(--error);
    font-weight: var(--fw-semibold);
  }
  /* Severity-tinted error cell: a hot rate gets a filled chip so a struggling
     tool/hook reads at a glance, not just a red number. */
  .tbl__err--hot {
    background: var(--error-bg);
    box-shadow: inset 2px 0 0 var(--error);
  }
  /* Inline mini-bar in the primary count column. */
  .tbl__bar-h {
    text-align: left !important;
    padding-left: var(--sp-5);
  }
  .tbl__bar {
    width: 38%;
  }
  .mbar {
    display: inline-block;
    width: calc(100% - 44px);
    height: 5px;
    vertical-align: middle;
    background: var(--bg-inset);
    border-radius: var(--r-full);
    overflow: hidden;
  }
  .mbar span {
    display: block;
    height: 100%;
    background: var(--spectrum);
    border-radius: var(--r-full);
    transition: width var(--dur-slow) var(--ease-out);
  }
  .mbar__n {
    display: inline-block;
    width: 40px;
    margin-left: var(--sp-2);
    text-align: right;
    vertical-align: middle;
    font-size: var(--fs-label);
    color: var(--text-secondary);
  }
  @media (prefers-reduced-motion: reduce) {
    .gauge__fill,
    .mbar span,
    .obs__tab {
      transition: none;
    }
  }
</style>
