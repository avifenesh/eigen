<script lang="ts">
  // Machines — remote targets. Lists hosts the daemon knows about (saved eigen
  // remotes + detected ssh-config entries) as cards. Clicking a card opens a
  // slide-over and dials that host over ssh for its live session list — a SLOW
  // call, so it shows a spinner and degrades to a "couldn't reach" note on
  // failure. Reads once on mount via the alive-guard load pattern. Installing
  // eigen on a fresh host is done from the CLI (`eigen remote install`), not
  // here — surfaced as a hint.
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { sessionDot } from "$lib/status";
  import type { MachinesDTO, MachineDTO, SessionInfoDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Button from "$lib/components/Button.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import Sheet from "$lib/components/Sheet.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let data = $state<MachinesDTO | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);

  // Drill-in (slide-over) state.
  let openMachine = $state<MachineDTO | null>(null);
  let remote = $state<SessionInfoDTO[]>([]);
  let remoteLoading = $state(false);
  let remoteError = $state<string | null>(null);

  // alive guard: a late Bridge.Machines() resolution must not write after the
  // view unmounts or a newer load() started.
  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    error = null;
    try {
      const d = await Bridge.Machines();
      if (seq === loadSeq) data = d;
    } catch (e) {
      if (seq === loadSeq) error = e instanceof Error ? e.message : String(e);
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

  // Drill-in dials ssh — slow and may fail. Its own sequence token so a stale
  // resolution (after closing or switching machines) is dropped.
  let remoteSeq = 0;
  async function drill(m: MachineDTO) {
    const seq = ++remoteSeq;
    openMachine = m;
    remote = [];
    remoteError = null;
    remoteLoading = true;
    try {
      const list = await Bridge.RemoteSessions(m.ssh);
      if (seq === remoteSeq) remote = list;
    } catch (e) {
      if (seq === remoteSeq) remoteError = e instanceof Error ? e.message : String(e);
    } finally {
      if (seq === remoteSeq) remoteLoading = false;
    }
  }
  function closeDrill() {
    remoteSeq++; // invalidate any in-flight dial
    openMachine = null;
    remote = [];
    remoteError = null;
    remoteLoading = false;
  }

  function base(dir: string): string {
    const p = (dir ?? "").replace(/\/$/, "").split("/");
    return p[p.length - 1] || dir || "—";
  }

  const machines = $derived(data?.machines ?? []);
</script>

<div class="mx">
  <header class="mx__head">
    <div class="mx__lead">
      <span class="mx__eyebrow">Remote hosts</span>
      {#if data}<span class="mx__n tnum">{machines.length}</span>{/if}
    </div>
    <p class="mx__hint">
      Install eigen on a new host from the CLI: <code class="mx__code">eigen remote install</code>
    </p>
  </header>

  {#if loading && !data}
    <div class="mx__grid mx__grid--pad">
      {#each Array(4) as _, i (i)}<div class="mx__skel"></div>{/each}
    </div>
  {:else if error && !data}
    <EmptyState glyph="⊟" title="Couldn't load machines" line={error}>
      {#snippet action()}
        <Button variant="secondary" onclick={() => load()}>Retry</Button>
      {/snippet}
    </EmptyState>
  {:else if machines.length === 0}
    <EmptyState
      glyph="⊟"
      title="No machines yet"
      line="Add a remote with `eigen remote add`, or define it as a Host in your ~/.ssh/config — detected entries show up here automatically."
    />
  {:else}
    <div class="mx__scroll">
      <div class="mx__grid">
        {#each machines as m (m.name)}
          <Card interactive onclick={() => drill(m)} title={`Dial ${m.ssh} for live sessions`}>
            <div class="mc">
              <div class="mc__top">
                <span class="mc__name">{m.name}</span>
                <div class="mc__tags">
                  {#if m.saved}<Badge tone="brand">saved</Badge>{/if}
                  {#if m.detected}<Badge tone="info">detected</Badge>{/if}
                </div>
              </div>
              <div class="mc__ssh selectable">{m.ssh}</div>
              <dl class="mc__meta">
                {#if m.addr}<dt>addr</dt><dd class="mc__mono">{m.addr}</dd>{/if}
                {#if m.dir}<dt>dir</dt><dd class="mc__mono" title={m.dir}>{m.dir}</dd>{/if}
              </dl>
              {#if m.model || m.perm}
                <div class="mc__badges">
                  {#if m.model}<Badge tone="neutral" truncate>{m.model}</Badge>{/if}
                  {#if m.perm}<Badge tone="neutral">{m.perm}</Badge>{/if}
                </div>
              {/if}
            </div>
          </Card>
        {/each}
      </div>
    </div>
  {/if}
</div>

<Sheet open={openMachine !== null} label={openMachine ? `${openMachine.name} sessions` : "Remote sessions"} width={520} onclose={closeDrill}>
  {#snippet title()}
    <h2 class="mx__sheet-title">{openMachine?.name ?? ""}</h2>
    {#if openMachine}<Badge tone="info">remote</Badge>{/if}
  {/snippet}
  {#if openMachine}
    <div class="mx__sheet-ssh">{openMachine.ssh}</div>
  {/if}
  {#if remoteLoading}
    <div class="mx__dialing">
      <span class="mx__spinner" aria-hidden="true"></span>
      <span>Dialing over ssh…</span>
    </div>
  {:else if remoteError}
    <div class="mx__remote-err">
      <p class="mx__remote-err-title">Couldn't reach this host</p>
      <p class="mx__remote-err-line">{remoteError}</p>
      <p class="mx__remote-err-hint">The host may be offline or have no eigen daemon running. Install with <code class="mx__code">eigen remote install</code>.</p>
    </div>
  {:else if remote.length === 0}
    <p class="mx__sheet-empty">No active sessions on this host.</p>
  {:else}
    <div class="mx__remote-list">
      {#each remote as s (s.id)}
        <div class="rrow">
          <StatusDot state={sessionDot(s.status)} size={7} pulse={s.status === "working" || s.status === "approval"} />
          <div class="rrow__main">
            <span class="rrow__title">{s.title || "untitled session"}</span>
            <span class="rrow__dir" title={s.dir}>{base(s.dir)}</span>
          </div>
          {#if s.model}<Badge tone="neutral" truncate>{s.model}</Badge>{/if}
          <span class="rrow__turns tnum">{s.turns} turn{s.turns === 1 ? "" : "s"}</span>
        </div>
      {/each}
    </div>
  {/if}
</Sheet>

<style>
  .mx {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .mx__head {
    flex: none;
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--sp-5);
    padding: var(--sp-6) var(--sp-9);
    border-bottom: 1px solid var(--border-hairline);
  }
  .mx__lead {
    display: flex;
    align-items: baseline;
    gap: var(--sp-3);
  }
  .mx__eyebrow {
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .mx__n {
    font-size: var(--fs-label);
    color: var(--text-ghost);
  }
  .mx__hint {
    margin: 0;
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .mx__code {
    font: var(--fw-regular) var(--fs-code-sm) / 1 var(--font-mono);
    color: var(--text-secondary);
    background: var(--bg-well);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-xs);
    padding: 1px var(--sp-2);
  }
  .mx__scroll {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-7) var(--sp-9);
  }
  .mx__grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: var(--sp-5);
  }
  .mx__grid--pad {
    padding: var(--sp-7) var(--sp-9);
  }
  .mx__skel {
    height: 132px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: mx-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes mx-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .mc {
    padding: var(--sp-5);
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .mc__top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-4);
  }
  .mc__name {
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mc__tags {
    display: flex;
    gap: var(--sp-2);
    flex: none;
  }
  .mc__ssh {
    font: var(--fw-regular) var(--fs-code-sm) / 1.4 var(--font-mono);
    color: var(--text-secondary);
    word-break: break-all;
  }
  .mc__meta {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: var(--sp-2) var(--sp-5);
    margin: 0;
  }
  .mc__meta dt {
    color: var(--text-faint);
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }
  .mc__mono {
    margin: 0;
    font: var(--fw-regular) var(--fs-code-sm) / 1.3 var(--font-mono);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mc__badges {
    display: flex;
    gap: var(--sp-2);
    flex-wrap: wrap;
    margin-top: auto;
  }

  /* Slide-over drill-in. */
  .mx__sheet-title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-h3) / 1.2 var(--font-display);
    color: var(--text-primary);
  }
  .mx__sheet-ssh {
    font: var(--fw-regular) var(--fs-code-sm) / 1.4 var(--font-mono);
    color: var(--text-faint);
    word-break: break-all;
  }
  .mx__dialing {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    padding: var(--sp-6) 0;
  }
  .mx__spinner {
    width: 14px;
    height: 14px;
    border-radius: var(--r-full);
    border: 2px solid var(--border-subtle);
    border-top-color: var(--brand);
    animation: mx-spin 0.7s linear infinite;
    flex: none;
  }
  @keyframes mx-spin {
    to {
      transform: rotate(360deg);
    }
  }
  .mx__remote-err {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-5);
    background: var(--error-bg);
    border: 1px solid color-mix(in srgb, var(--error) 24%, transparent);
    border-radius: var(--r-md);
  }
  .mx__remote-err-title {
    margin: 0;
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body-sm);
    color: var(--error);
  }
  .mx__remote-err-line {
    margin: 0;
    font-size: var(--fs-label);
    color: var(--text-secondary);
    word-break: break-word;
  }
  .mx__remote-err-hint {
    margin: 0;
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .mx__sheet-empty {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    margin: 0;
  }
  .mx__remote-list {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .rrow {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    padding: var(--sp-4) var(--sp-5);
    background: var(--bg-well);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
  }
  .rrow__main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
  }
  .rrow__title {
    font-weight: var(--fw-medium);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rrow__dir {
    font-size: var(--fs-label);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rrow__turns {
    flex: none;
    font-size: var(--fs-label);
    color: var(--text-ghost);
    min-width: 56px;
    text-align: right;
  }
  @media (prefers-reduced-motion: reduce) {
    .mx__skel,
    .mx__spinner {
      animation: none;
    }
  }
</style>
