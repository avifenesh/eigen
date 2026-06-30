<script lang="ts">
  // Live — the working-now command surface. The live cockpit: every session at
  // a glance, sorted so what's running and what's blocked on approval floats to
  // the top. Reuses the shared `sessions` store and polls it on a ~2s self-
  // scheduling timer (cleared on unmount). Per-row: Open, Interrupt (only while
  // working / awaiting approval), Remove (inline confirm). A KPI line counts the
  // four statuses. This is a control surface — every action does real work.
  import { sessions } from "$lib/stores/sessions.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { router } from "$lib/router.svelte";
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { now } from "$lib/stores/clock.svelte";
  import { sessionDot } from "$lib/status";
  import { errText } from "$lib/errors";
  import type { SessionInfoDTO, ApprovalInfo } from "$lib/types";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";

  let starting = $state(false);
  // Per-id in-flight guards so a row's buttons disable while acting.
  let interrupting = $state<Record<string, boolean>>({});
  let removing = $state<Record<string, boolean>>({});
  // Per-id inline-confirm latch for the destructive Remove action.
  let confirmRemove = $state<Record<string, boolean>>({});
  // Inline approval resolution: for an `approval` row the user can expand a
  // gate right here (fetch State → Allow/Deny via Bridge.Approve) instead of
  // round-tripping through Chat. Per-id: which row is expanded, the pending
  // approvals fetched for it, a fetch-in-flight flag, and per-approval acting.
  let gateOpen = $state<Record<string, boolean>>({});
  let gatePending = $state<Record<string, ApprovalInfo[]>>({});
  let gateLoading = $state<Record<string, boolean>>({});
  let gateError = $state<Record<string, string>>({});
  let acting = $state<Record<string, boolean>>({});

  // Poll the shared store while mounted. A self-scheduling timeout (not a
  // re-running interval) refreshes ~every 2s — but only does real work when the
  // daemon is online AND the document is visible. Offline every tick is a
  // guaranteed-failing RPC; backgrounded it just hammers the daemon for a view
  // nobody is looking at. So the timer stays cheap (re-checks every 2s) while
  // gated, and two signals kick an immediate catch-up refresh: daemon reconnect
  // and the tab regaining visibility. Cleanup clears the timer + both listeners.
  $effect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;
    async function tick() {
      if (daemon.status === "online" && !document.hidden) {
        await sessions.refresh();
        if (stopped) return;
      }
      timer = setTimeout(tick, 2000);
    }
    tick();
    // Resume the moment the daemon comes back rather than waiting out the tick.
    const offReconnect = daemon.onReconnect(() => {
      if (!stopped && !document.hidden) sessions.refresh();
    });
    // Catch up immediately when the tab is brought back into view.
    function onVisibility() {
      if (!stopped && !document.hidden && daemon.status === "online") sessions.refresh();
    }
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      stopped = true;
      clearTimeout(timer);
      offReconnect();
      document.removeEventListener("visibilitychange", onVisibility);
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
      toasts.error(errText(e));
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
      toasts.error(errText(e));
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
      toasts.error(errText(e));
    } finally {
      delete removing[s.id];
    }
  }

  // Open the inline approval gate for a row: fetch its State, pull the pending
  // approvals. If the gate has nothing resolvable (state gone, no pending), fall
  // back to opening Chat — the gate lives there too. Keeps the user one click
  // from resolving the block without leaving Live.
  async function openGate(s: SessionInfoDTO) {
    gateOpen[s.id] = true;
    gateLoading[s.id] = true;
    delete gateError[s.id];
    try {
      const st = await Bridge.State(s.id);
      const pending = st?.pending ?? [];
      if (pending.length === 0) {
        // Nothing inline to resolve (raced away, or gate not exposed) — defer
        // to Chat where the live stream + gate render.
        closeGate(s.id);
        open(s);
        return;
      }
      gatePending[s.id] = pending;
    } catch (e) {
      gateError[s.id] = errText(e);
    } finally {
      delete gateLoading[s.id];
    }
  }

  function closeGate(id: string) {
    delete gateOpen[id];
    delete gatePending[id];
    delete gateLoading[id];
    delete gateError[id];
  }

  // Resolve a single gated approval inline. On success refresh the store (the
  // row's status flips off `approval` once the daemon clears the gate) and drop
  // the gate; surface failures rather than swallowing them.
  async function decide(s: SessionInfoDTO, approvalID: string, allow: boolean) {
    const key = `${s.id}:${approvalID}`;
    acting[key] = true;
    try {
      await Bridge.Approve(s.id, approvalID, allow);
      toasts.info(allow ? "approved" : "denied");
      closeGate(s.id);
      await sessions.refresh();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete acting[key];
    }
  }
</script>

<div class="live">
  <header class="live__head">
    <div class="live__kpis">
      <div class="kpi" class:kpi--working={counts.working > 0}><span class="kpi__v tnum">{counts.working}</span><span class="kpi__l">working</span></div>
      <div class="kpi" class:kpi--approval={counts.approval > 0}><span class="kpi__v tnum">{counts.approval}</span><span class="kpi__l">approval</span></div>
      <div class="kpi kpi--idle"><span class="kpi__v tnum">{counts.idle}</span><span class="kpi__l">idle</span></div>
      <div class="kpi" class:kpi--error={counts.error > 0}><span class="kpi__v tnum">{counts.error}</span><span class="kpi__l">error</span></div>
    </div>
    <Button variant="primary" size="sm" loading={starting} onclick={startSession}>New session</Button>
  </header>

  {#if sessions.loading && sessions.count === 0}
    <div class="live__list live__list--pad">
      <Skeleton count={4} height="52px" gap="var(--sp-4)" />
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
        <div class="lcell" class:lcell--gated={gateOpen[s.id]}>
        <div class="lrow" class:lrow--working={s.status === "working"} class:lrow--approval={s.status === "approval"}>
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
            {#if s.status === "approval"}
              {#if gateOpen[s.id]}
                <Button variant="ghost" size="sm" onclick={() => closeGate(s.id)}>Close</Button>
              {:else}
                <Button variant="primary" size="sm" onclick={() => openGate(s)}>Approve…</Button>
              {/if}
            {/if}
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
        {#if gateOpen[s.id]}
          <div class="gate">
            {#if gateLoading[s.id]}
              <div class="gate__status">Loading approval…</div>
            {:else if gateError[s.id]}
              <div class="gate__status gate__status--error">{gateError[s.id]}</div>
              <div class="gate__retry">
                <Button variant="secondary" size="sm" onclick={() => openGate(s)}>Retry</Button>
                <Button variant="ghost" size="sm" onclick={() => open(s)}>Open in Chat</Button>
              </div>
            {:else}
              {#each gatePending[s.id] ?? [] as ap (ap.id)}
                {@const key = `${s.id}:${ap.id}`}
                <div class="gate__item">
                  <div class="gate__tool">{ap.tool}</div>
                  {#if ap.args}<div class="gate__args selectable" title={ap.args}>{ap.args}</div>{/if}
                  <div class="gate__actions">
                    <Button variant="primary" size="sm" loading={acting[key]} onclick={() => decide(s, ap.id, true)}>Allow</Button>
                    <Button variant="danger" size="sm" loading={acting[key]} onclick={() => decide(s, ap.id, false)}>Deny</Button>
                  </div>
                </div>
              {/each}
            {/if}
          </div>
        {/if}
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
    padding: var(--sp-6) var(--sp-7);
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
    color: var(--brand-bright);
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
    padding: var(--sp-6) var(--sp-7);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .live__list--pad {
    gap: var(--sp-4);
  }
  /* A cell wraps the row plus its (optional) inline approval gate so the gate
     tucks directly under the row it belongs to and they share one outline when
     expanded. */
  .lcell {
    display: flex;
    flex-direction: column;
  }
  .lcell--gated {
    border-radius: var(--r-md);
    box-shadow: 0 0 0 1px var(--warn-bg);
  }
  .lcell--gated .lrow {
    border-bottom-left-radius: 0;
    border-bottom-right-radius: 0;
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
  /* WORKING — teal edge + a STATIC teal halo. The "alive" motion is the row's
     StatusDot (opacity/transform breathe). Previously each working/approval row
     animated box-shadow on its own infinite track — N live sessions = N
     per-frame main-thread repaints (WebKitGTK can't composite box-shadow). This
     is the last holdout of the pattern the other views already dropped. */
  .lrow--working {
    border-left-color: var(--brand);
    box-shadow: var(--glow-live);
  }
  /* APPROVAL — blocked on the user. Warn edge + static warn halo, a distinct
     register from "running". */
  .lrow--approval {
    border-left-color: var(--warn);
    box-shadow: var(--glow-warn);
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
  /* ── inline approval gate ─────────────────────────────────────────────
     Lives under an `approval` row when expanded. Warn register (matches the
     row's left edge + badge) so it reads as "something is waiting on you". */
  .gate {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-4) var(--sp-5);
    background: var(--warn-bg);
    border: 1px solid var(--border-hairline);
    border-top: none;
    border-left: 2px solid var(--warn);
    border-bottom-left-radius: var(--r-md);
    border-bottom-right-radius: var(--r-md);
  }
  .gate__status {
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .gate__status--error {
    color: var(--error);
  }
  .gate__retry {
    display: flex;
    gap: var(--sp-2);
  }
  .gate__item {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .gate__tool {
    flex: none;
    font: var(--fw-medium) var(--fs-body-sm) / 1.2 var(--font-mono, var(--font-sans));
    color: var(--text-primary);
  }
  .gate__args {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-micro);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .gate__actions {
    flex: none;
    display: flex;
    gap: var(--sp-2);
  }
  @media (prefers-reduced-motion: reduce) {
    .lrow,
    .lrow--working,
    .lrow--approval {
      animation: none;
      transition: none;
    }
    /* hold the live/approval glow steady rather than breathing */
    .lrow--working {
      box-shadow: var(--glow-live);
    }
    .lrow--approval {
      box-shadow: 0 0 0 1px rgba(224, 179, 106, 0.35);
    }
  }
</style>
