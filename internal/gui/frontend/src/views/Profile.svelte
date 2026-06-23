<script lang="ts">
  // Profile — identity + usage. Top: usage KPIs. Lifetime figures (turns,
  // tokens in/out, cache-hit %, errors) come from the DURABLE ObserveSummary
  // log so they survive daemon restarts; the only live daemon.stats counter is
  // the session count, labeled "since daemon start". Then a "top models" table
  // from the summary. Below: the GLOBAL user profile editor (USER.md) — the durable
  // personalization prompt — read from Bridge.Memory().global.profile and saved
  // via Bridge.WriteUserProfile. Same backing file as Memory's global-scope
  // editor; living here keeps identity in one place.
  import { daemon } from "$lib/stores/daemon.svelte";
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { ObserveSummaryDTO, MemoryDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Markdown from "$lib/components/Markdown.svelte";

  const stats = $derived(daemon.stats);

  let summary = $state<ObserveSummaryDTO | null>(null);
  let summaryLoading = $state(true);
  let summaryError = $state<string | null>(null);
  let memory = $state<MemoryDTO | null>(null);
  let memoryLoading = $state(true);

  // Profile editor state. Reads .global.profile; writes the same backing file.
  let editing = $state(false);
  let draft = $state("");
  let saving = $state(false);

  const profile = $derived(memory?.global?.profile ?? "");

  // alive guard: both loads share one sequence token so a late resolution after
  // unmount or a newer load() is dropped.
  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    summaryLoading = true;
    summaryError = null;
    memoryLoading = true;
    // Independent fetches — resolve each on its own so a slow ObserveSummary
    // doesn't block the (local, fast) memory read.
    Bridge.ObserveSummary(5000)
      .then((d) => {
        if (seq === loadSeq) summary = d;
      })
      .catch((e) => {
        // Surface the failure in the Usage section (not just a toast): without a
        // summary the KPIs would read turns=0/errors=0 and the table "no activity"
        // — indistinguishable from a clean log. Mirrors the load-failure-vs-empty
        // pattern used across Routing/Crons/Config.
        if (seq === loadSeq) summaryError = e instanceof Error ? e.message : String(e);
      })
      .finally(() => {
        if (seq === loadSeq) summaryLoading = false;
      });
    Bridge.Memory()
      .then((d) => {
        if (seq === loadSeq) memory = d;
      })
      .catch((e) => {
        if (seq === loadSeq) toasts.error(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (seq === loadSeq) memoryLoading = false;
      });
  }
  $effect(() => {
    load();
    return () => {
      loadSeq++;
    };
  });

  // Usage rollups. Token totals + cache-hit are TRUE LIFETIME figures summed
  // from the durable observability log (summary.models[*]) — the daemon.stats
  // stream is volatile and resets on every daemon restart, so it must not drive
  // lifetime KPIs. Turns and errors likewise come from the historical log
  // (records ≈ turns).
  const modelTotals = $derived(
    (summary?.models ?? []).reduce(
      (acc, m) => {
        acc.in += m.inTokens;
        acc.out += m.outTokens;
        acc.cacheRead += m.cacheReadTokens;
        return acc;
      },
      { in: 0, out: 0, cacheRead: 0 },
    ),
  );
  const inTokens = $derived(modelTotals.in);
  const outTokens = $derived(modelTotals.out);
  const cacheHit = $derived(
    inTokens > 0 ? Math.round((modelTotals.cacheRead / inTokens) * 100) : 0,
  );
  const turns = $derived(summary?.records ?? 0);
  const errorCount = $derived((summary?.errors ?? []).reduce((n, e) => n + e.count, 0));
  // Live, volatile counter — explicitly labeled "since daemon start" in the UI
  // so it's never mistaken for a lifetime tally.
  const sessionCount = $derived(stats?.sessions ?? 0);

  function k(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
    return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);
  }

  function startEdit() {
    draft = profile;
    editing = true;
  }
  function cancelEdit() {
    editing = false;
    draft = "";
  }
  async function save() {
    saving = true;
    try {
      await Bridge.WriteUserProfile(draft);
      toasts.success("profile saved");
      editing = false;
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      saving = false;
    }
  }
</script>

<div class="pf selectable">
  <div class="pf__scroll">
    <!-- USAGE -->
    <section class="pf__section">
      <div class="pf__section-head">
        <h2 class="pf__section-title">Usage</h2>
        {#if summaryLoading}<span class="pf__note">loading log…</span>{/if}
      </div>
      {#if summaryError && !summary}
        <!-- Log read failed — never show turns=0/errors=0 here, that would read
             as a clean log. Distinct failure surface with a Retry, matching the
             load-failure-vs-empty pattern elsewhere. -->
        <Card>
          <div class="pf__err">
            <p class="pf__err-line">Couldn't read the usage log.</p>
            <p class="pf__err-detail">{summaryError}</p>
            <Button variant="secondary" size="sm" onclick={() => load()}>Retry</Button>
          </div>
        </Card>
      {:else}
        <div class="pf__kpis">
          <Card><div class="kpi"><div class="kpi__v tnum">{sessionCount}</div><div class="kpi__l">sessions <span class="kpi__qual">since daemon start</span></div></div></Card>
          <Card><div class="kpi"><div class="kpi__v tnum">{turns}</div><div class="kpi__l">turns logged</div></div></Card>
          <Card><div class="kpi"><div class="kpi__v tnum">{k(inTokens)}</div><div class="kpi__l">tokens in</div></div></Card>
          <Card><div class="kpi"><div class="kpi__v tnum">{k(outTokens)}</div><div class="kpi__l">tokens out</div></div></Card>
          <Card><div class="kpi"><div class="kpi__v tnum">{cacheHit}%</div><div class="kpi__l">cache hit</div></div></Card>
          <Card><div class="kpi"><div class="kpi__v tnum" class:kpi__v--err={errorCount > 0}>{errorCount}</div><div class="kpi__l">errors</div></div></Card>
        </div>
      {/if}
    </section>

    <!-- TOP MODELS -->
    <section class="pf__section">
      <div class="pf__section-head">
        <h2 class="pf__section-title">Top models</h2>
      </div>
      {#if summaryLoading && !summary}
        <div class="pf__skel"></div>
      {:else if summaryError && !summary}
        <p class="pf__note">Usage log unavailable — see above.</p>
      {:else if !summary || !summary.available || summary.models.length === 0}
        <p class="pf__note">No model activity recorded yet. As sessions run, the observability log tallies usage per model here.</p>
      {:else}
        <Card>
          <table class="tbl">
            <thead><tr><th>model</th><th class="tbl__num">turns</th><th class="tbl__num">in</th><th class="tbl__num">out</th><th class="tbl__num">cache rd</th></tr></thead>
            <tbody>
              {#each summary.models as m (m.name)}
                <tr>
                  <td class="tbl__name">{m.name}</td>
                  <td class="tbl__num tnum">{m.turns}</td>
                  <td class="tbl__num tnum">{k(m.inTokens)}</td>
                  <td class="tbl__num tnum">{k(m.outTokens)}</td>
                  <td class="tbl__num tnum">{k(m.cacheReadTokens)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </Card>
      {/if}
    </section>

    <!-- USER PROFILE -->
    <section class="pf__section">
      <div class="pf__section-head">
        <h2 class="pf__section-title">User profile</h2>
        <Badge tone="brand">global</Badge>
        {#if !editing && !memoryLoading}
          <Button variant="link" size="sm" onclick={startEdit}>{profile ? "Edit" : "Add"}</Button>
        {/if}
      </div>
      <p class="pf__desc">The durable personalization prompt (USER.md) injected into every session — who you are, how you like to work.</p>
      {#if memoryLoading && !memory}
        <div class="pf__skel"></div>
      {:else if editing}
        <textarea
          bind:value={draft}
          class="pf__textarea selectable"
          rows="10"
          placeholder="Durable personalization prompt — your role, preferences, working style…"
          aria-label="User profile content"
        ></textarea>
        <div class="pf__edit-actions">
          <Button variant="ghost" size="sm" disabled={saving} onclick={cancelEdit}>Cancel</Button>
          <Button variant="primary" size="sm" loading={saving} onclick={save}>Save</Button>
        </div>
      {:else if profile}
        <Card><div class="pf__profile selectable"><Markdown source={profile} /></div></Card>
      {:else}
        <p class="pf__note">No profile set yet. <button class="pf__inline-link" onclick={startEdit}>Add one.</button></p>
      {/if}
    </section>
  </div>
</div>

<style>
  .pf {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .pf__scroll {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-8) var(--sp-9) var(--sp-10);
    display: flex;
    flex-direction: column;
    gap: var(--sp-9);
    max-width: 1000px;
  }
  .pf__section {
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .pf__section-head {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .pf__section-title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .pf__desc {
    margin: 0;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
  }
  .pf__note {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    margin: 0;
  }
  /* Usage-log read failure — a quiet, honest surface, not an alarm. The leading
     line states the failure; the detail is dimmer; a Retry resolves it. */
  .pf__err {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--sp-4);
    padding: var(--sp-6);
  }
  .pf__err-line {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--fs-body-sm);
    font-weight: var(--fw-medium);
  }
  .pf__err-detail {
    margin: 0;
    color: var(--text-faint);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
    word-break: break-word;
  }
  .pf__inline-link {
    border: none;
    background: none;
    color: var(--accent);
    cursor: pointer;
    font: inherit;
    padding: 0;
  }
  .pf__inline-link:hover {
    color: var(--accent-bright);
    text-decoration: underline;
  }
  .pf__inline-link:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
    border-radius: var(--r-xs);
  }
  .pf__kpis {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: var(--sp-5);
  }
  .kpi {
    padding: var(--sp-6);
  }
  /* Profile KPIs are lifetime tallies, not live signals — they stay neutral so
     brand teal keeps meaning "alive" across the app. Only errors carry color. */
  .kpi__v {
    font: var(--fw-bold) var(--fs-h1) / 1 var(--font-display);
    color: var(--text-primary);
  }
  .kpi__v--err {
    color: var(--error);
  }
  .kpi__l {
    margin-top: var(--sp-3);
    font-size: var(--fs-label);
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }
  /* Qualifier on volatile counters (e.g. "since daemon start") — quieter than
     the label so the durable lifetime KPIs read as the primary figures. */
  .kpi__qual {
    display: block;
    margin-top: var(--sp-2);
    color: var(--text-faint);
    text-transform: none;
    letter-spacing: normal;
    font-size: var(--fs-micro);
  }
  .pf__skel {
    height: 96px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: pf-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes pf-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .pf__textarea {
    width: 100%;
    resize: vertical;
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body-sm) / var(--lh-snug) var(--font-sans);
    padding: var(--sp-5);
    outline: none;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .pf__textarea:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .pf__edit-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-3);
  }
  .pf__profile {
    padding: var(--sp-5);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-prose);
  }

  /* Top-models table — matches Observe's table styling. */
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
  @media (prefers-reduced-motion: reduce) {
    .pf__skel {
      animation: none;
    }
    .pf__textarea {
      transition: none;
    }
  }
</style>
