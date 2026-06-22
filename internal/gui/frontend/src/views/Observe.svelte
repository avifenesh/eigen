<script lang="ts">
  // Observe — live daemon telemetry. Reads the 1Hz DaemonStats stream the
  // bridge pushes. Doubles as the leak HUD: watch Views + Goroutines stay flat
  // as you navigate. (Routes / Tools / Errors sub-views arrive with their own
  // bridge ops — shown as honest coming-soon, never dead tabs.)
  import { daemon } from "$lib/stores/daemon.svelte";
  import Card from "$lib/components/Card.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  const s = $derived(daemon.stats);

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
</script>

{#if !s}
  <EmptyState glyph="◉" title="Waiting for telemetry" line="The daemon pushes a stats snapshot every second." />
{:else}
  <div class="obs selectable">
    <section class="obs__kpis">
      <Card><div class="kpi"><div class="kpi__v tnum">{s.sessions}</div><div class="kpi__l">sessions</div></div></Card>
      <Card><div class="kpi"><div class="kpi__v tnum">{s.running_turns}</div><div class="kpi__l">running turns</div></div></Card>
      <Card><div class="kpi"><div class="kpi__v tnum">{s.bg_tasks}</div><div class="kpi__l">background tasks</div></div></Card>
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
            {#if s.go_version}<dt>go</dt><dd>{s.go_version}</dd>{/if}
          </dl>
        </div>
      </Card>

      <Card>
        <div class="panel">
          <div class="panel__title">tokens</div>
          <div class="cache">
            <div class="cache__bar"><span style="width:{cacheHit}%"></span></div>
            <div class="cache__label tnum">{cacheHit}% cache hit</div>
          </div>
          <dl class="kv">
            <dt>input</dt><dd class="tnum">{(s.input_tokens ?? 0).toLocaleString()}</dd>
            <dt>output</dt><dd class="tnum">{(s.output_tokens ?? 0).toLocaleString()}</dd>
            <dt>cache read</dt><dd class="tnum">{(s.cache_read_tokens ?? 0).toLocaleString()}</dd>
            <dt>cache write</dt><dd class="tnum">{(s.cache_write_tokens ?? 0).toLocaleString()}</dd>
          </dl>
        </div>
      </Card>
    </div>
  </div>
{/if}

<style>
  .obs {
    height: 100%;
    overflow-y: auto;
    padding: var(--sp-9) var(--sp-10);
    display: flex;
    flex-direction: column;
    gap: var(--sp-6);
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
    color: var(--brand);
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
  .cache {
    margin-bottom: var(--sp-6);
  }
  .cache__bar {
    height: 8px;
    background: var(--bg-inset);
    border-radius: var(--r-full);
    overflow: hidden;
  }
  .cache__bar span {
    display: block;
    height: 100%;
    background: var(--spectrum);
    border-radius: var(--r-full);
    transition: width var(--dur-slow) var(--ease-out);
  }
  .cache__label {
    margin-top: var(--sp-3);
    font-size: var(--fs-label);
    color: var(--text-secondary);
  }
</style>
