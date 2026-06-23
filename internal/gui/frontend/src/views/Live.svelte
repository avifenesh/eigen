<script lang="ts">
  // Live — the working-now command surface. The live cockpit: every session at
  // a glance, sorted so what's running and what's blocked on approval floats to
  // the top. Reuses the shared `sessions` store and polls it on a ~2s self-
  // scheduling timer (cleared on unmount). Per-row: Open, Interrupt (only while
  // working / awaiting approval), Remove (inline confirm). A KPI line counts the
  // four statuses. This is a control surface — every action does real work.
  import { sessions } from "$lib/stores/sessions.svelte";
  import { router } from "$lib/router.svelte";
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { now } from "$lib/stores/clock.svelte";
  import { sessionDot } from "$lib/status";
  import type { SessionInfoDTO } from "$lib/types";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let starting = $state(false);
  // Per-id in-flight guards so a row's buttons disable while acting.
  let interrupting = $state<Record<string, boolean>>({});
  let removing = $state<Record<string, boolean>>({});
  // Per-id inline-confirm latch for the destructive Remove action.
  let confirmRemove = $state<Record<string, boolean>>({});

  // Poll the shared store while mounted. A self-scheduling timeout (not a
  // re-running interval) refreshes ~every 2s; cleanup clears the pending timer.
  $effect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;
    async function tick() {
      await sessions.refresh();
      if (stopped) return;
      timer = setTimeout(tick, 2000);
    }
    tick();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  });

  // Running / approval first (the things to act on), then idle, then error —
  // newest within each bucket. Sort a copy so the store array isn't mutated.
  const rank: Record<string, number> = { working: 0, approval: 1, idle: 2, error: 3 };
  const ordered = $derived.by(() => {
    return [...sessions.list].sort((a, b) => {
      const ra = rank[a.status] ?? 4;
      const rb = rank[b.status] ?? 4;
      if (ra !== rb) return ra - rb;
      return b.updated - a.updated;
    });
  });

  const counts = $derived.by(() => {
    const c = { working: 0, idle: 0, approval: 0, error: 0 };
    for (const s of sessions.list) {
      if (s.status === "working") c.working++;
      else if (s.status === "approval") c.approval++;
      else if (s.status === "error") c.error++;
      else c.idle++;
    }
    return c;
  });

  function isLive(s: SessionInfoDTO): boolean {
    return s.status === "working" || s.status === "approval";
  }

  function rel(updatedNano: number): string {
    void now.ms; // tie to shared clock so the label ticks
    const ms = Date.now() - updatedNano / 1e6;
    const m = Math.floor(ms / 60000);
    if (m < 1) return "just now";
    if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h ago`;
    return `${Math.floor(h / 24)}d ago`;
  }
  function base(dir: string): string {
    const p = (dir ?? "").replace(/\/$/, "").split("/");
    return p[p.length - 1] || dir || "—";
  }

  async function startSession() {
    starting = true;
    try {
      const id = await Bridge.NewSession("", "", "");
      await sessions.refresh();
      router.go("chat", id);
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      starting = false;
    }
  }

  function open(s: SessionInfoDTO) {
    router.go("chat", s.id);
  }

  async function interrupt(s: SessionInfoDTO) {
    interrupting[s.id] = true;
    try {
      await Bridge.Interrupt(s.id);
      toasts.info("interrupt requested");
      await sessions.refresh();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      delete interrupting[s.id];
    }
  }

  async function remove(s: SessionInfoDTO) {
    removing[s.id] = true;
    try {
      await Bridge.RemoveSession(s.id);
      toasts.success("session removed");
      delete confirmRemove[s.id];
      await sessions.refresh();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      delete removing[s.id];
    }
  }
</script>

<div class="live">
  <header class="live__head">
    <div class="live__kpis">
      <div class="kpi kpi--working"><span class="kpi__v tnum">{counts.working}</span><span class="kpi__l">working</span></div>
      <div class="kpi kpi--approval"><span class="kpi__v tnum">{counts.approval}</span><span class="kpi__l">approval</span></div>
      <div class="kpi kpi--idle"><span class="kpi__v tnum">{counts.idle}</span><span class="kpi__l">idle</span></div>
      <div class="kpi kpi--error"><span class="kpi__v tnum">{counts.error}</span><span class="kpi__l">error</span></div>
    </div>
    <Button variant="primary" size="sm" loading={starting} onclick={startSession}>New session</Button>
  </header>

  {#if sessions.loading && sessions.count === 0}
    <div class="live__list live__list--pad">
      {#each Array(4) as _, i (i)}<div class="live__skel"></div>{/each}
    </div>
  {:else if sessions.error && sessions.count === 0}
    <EmptyState glyph="◐" title="Couldn't load sessions" line={sessions.error}>
      {#snippet action()}
        <Button variant="secondary" onclick={() => sessions.refresh()}>Retry</Button>
      {/snippet}
    </EmptyState>
  {:else if ordered.length === 0}
    <EmptyState glyph="◐" title="Nothing running" line="No live or idle sessions. Start one and it'll appear here the moment it's working.">
      {#snippet action()}
        <Button variant="primary" loading={starting} onclick={startSession}>New session</Button>
      {/snippet}
    </EmptyState>
  {:else}
    <div class="live__list">
      {#each ordered as s (s.id)}
        <div class="lrow" class:lrow--live={isLive(s)} class:lrow--approval={s.status === "approval"}>
          <StatusDot state={sessionDot(s.status)} size={9} pulse={isLive(s)} />
          <button class="lrow__main" onclick={() => open(s)} title="Open session">
            <span class="lrow__title">{s.title || "untitled session"}</span>
            <span class="lrow__sub">
              {#if s.status === "approval"}<Badge tone="warn">needs approval</Badge>{/if}
              {#if s.status === "error"}<Badge tone="error">error</Badge>{/if}
              <span class="lrow__dir" title={s.dir}>{base(s.dir)}</span>
            </span>
          </button>
          {#if s.model}<Badge tone="neutral" truncate>{s.model}</Badge>{/if}
          <span class="lrow__turns tnum">{s.turns} turn{s.turns === 1 ? "" : "s"}</span>
          <span class="lrow__when">{rel(s.updated)}</span>
          <div class="lrow__actions">
            <Button variant="ghost" size="sm" onclick={() => open(s)}>Open</Button>
            {#if isLive(s)}
              <Button variant="ghost" size="sm" loading={interrupting[s.id]} onclick={() => interrupt(s)}>Interrupt</Button>
            {/if}
            {#if confirmRemove[s.id]}
              <Button variant="danger" size="sm" loading={removing[s.id]} onclick={() => remove(s)}>Confirm</Button>
              <Button variant="ghost" size="sm" disabled={removing[s.id]} onclick={() => delete confirmRemove[s.id]}>Cancel</Button>
            {:else}
              <Button variant="ghost" size="sm" onclick={() => (confirmRemove[s.id] = true)}>Remove</Button>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .live {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .live__head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-5);
    padding: var(--sp-6) var(--sp-9);
    border-bottom: 1px solid var(--border-hairline);
  }
  .live__kpis {
    display: flex;
    gap: var(--sp-7);
  }
  .kpi {
    display: flex;
    align-items: baseline;
    gap: var(--sp-3);
  }
  .kpi__v {
    font: var(--fw-bold) var(--fs-h2) / 1 var(--font-display);
    color: var(--text-primary);
  }
  .kpi--working .kpi__v {
    color: var(--working);
  }
  .kpi--approval .kpi__v {
    color: var(--warn);
  }
  .kpi--error .kpi__v {
    color: var(--error);
  }
  .kpi__l {
    font-size: var(--fs-label);
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }
  .live__list {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-6) var(--sp-9);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .live__list--pad {
    gap: var(--sp-4);
  }
  .live__skel {
    height: 52px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: live-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes live-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .lrow {
    display: flex;
    align-items: center;
    gap: var(--sp-5);
    padding: var(--sp-4) var(--sp-5);
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-left: 2px solid var(--border-subtle);
    border-radius: var(--r-md);
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .lrow--live {
    border-left-color: var(--working);
  }
  .lrow--approval {
    border-left-color: var(--warn);
    box-shadow: var(--glow-working);
    animation: lrow-pulse var(--breath) var(--ease-inout) infinite;
  }
  @keyframes lrow-pulse {
    0%,
    100% {
      box-shadow: 0 0 0 1px var(--border-brand-faint);
    }
    50% {
      box-shadow: var(--glow-working);
    }
  }
  .lrow__main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    border: none;
    background: transparent;
    cursor: pointer;
    text-align: left;
    padding: 0;
    border-radius: var(--r-xs);
  }
  .lrow__main:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .lrow__title {
    font-weight: var(--fw-medium);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .lrow__sub {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .lrow__dir {
    font-size: var(--fs-label);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .lrow__turns {
    flex: none;
    font-size: var(--fs-label);
    color: var(--text-ghost);
    min-width: 56px;
    text-align: right;
  }
  .lrow__when {
    flex: none;
    font-size: var(--fs-label);
    color: var(--text-faint);
    min-width: 64px;
    text-align: right;
  }
  .lrow__actions {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  @media (prefers-reduced-motion: reduce) {
    .live__skel,
    .lrow,
    .lrow--approval {
      animation: none;
      transition: none;
    }
  }
</style>
