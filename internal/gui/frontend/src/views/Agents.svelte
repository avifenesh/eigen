<script lang="ts">
  // Agents — the multi-agent fan-out board. Background subtasks (task/task_group
  // delegations) persist to disk; this view polls them and shows live status:
  // running tasks with their current tool + elapsed time, completed/errored
  // results, model/route each ran on. A running task can be canceled; any task's
  // transcript can be opened in a slide-over. Polling lives in an $effect whose
  // cleanup clears the interval — no leaked timer on nav.
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { now } from "$lib/stores/clock.svelte";
  import type { AgentsDTO, BgTaskDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import CodeBlock from "$lib/components/CodeBlock.svelte";
  import VirtualList from "$lib/components/VirtualList.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let data = $state<AgentsDTO | null>(null);
  let loading = $state(true);
  let filter = $state<"all" | "running" | "done" | "error">("all");

  let openTask = $state<BgTaskDTO | null>(null);
  let transcript = $state("");
  let transcriptLoading = $state(false);
  let acting = $state<Record<string, boolean>>({});

  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    try {
      const d = await Bridge.Agents();
      if (seq === loadSeq) {
        data = d;
        loading = false;
      }
    } catch (e) {
      if (seq === loadSeq) {
        loading = false;
        toasts.error(e instanceof Error ? e.message : String(e));
      }
    }
  }

  // Poll while the view is mounted. A self-scheduling timeout reads the cadence
  // fresh after each load (fast while work is in flight, slow when idle) without
  // re-running the effect — so the timer isn't torn down and recreated on every
  // data change. Cleanup clears the pending timeout AND bumps loadSeq so a
  // late Bridge.Agents() resolution after unmount is dropped.
  $effect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;
    async function tick() {
      await load();
      if (stopped) return;
      const period = (data?.running ?? 0) > 0 ? 1500 : 4000;
      timer = setTimeout(tick, period);
    }
    tick();
    return () => {
      stopped = true;
      clearTimeout(timer);
      loadSeq++;
    };
  });

  const tasks = $derived.by(() => {
    const all = data?.tasks ?? [];
    if (filter === "all") return all;
    if (filter === "error") return all.filter((t) => t.status === "error" || t.status === "lost");
    return all.filter((t) => t.status === filter);
  });

  function dotState(s: string): "working" | "ok" | "error" | "idle" {
    if (s === "running") return "working";
    if (s === "done") return "ok";
    if (s === "error" || s === "lost") return "error";
    return "idle";
  }
  function tone(s: string): "brand" | "success" | "error" | "warn" | "neutral" {
    if (s === "running") return "brand";
    if (s === "done") return "success";
    if (s === "error" || s === "lost") return "error";
    if (s === "canceled") return "warn";
    return "neutral";
  }

  // Live elapsed for a running task, derived from the shared clock so it ticks.
  function elapsed(t: BgTaskDTO): string {
    const end = t.finishedMs && t.finishedMs > 0 ? t.finishedMs : now.ms;
    const secs = Math.max(0, Math.floor((end - t.startedMs) / 1000));
    if (secs < 60) return `${secs}s`;
    const m = Math.floor(secs / 60);
    if (m < 60) return `${m}m ${secs % 60}s`;
    return `${Math.floor(m / 60)}h ${m % 60}m`;
  }

  async function cancel(id: string) {
    acting[id] = true;
    try {
      await Bridge.CancelAgent(id);
      toasts.info("cancel requested");
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      delete acting[id];
    }
  }

  async function openTranscript(t: BgTaskDTO) {
    openTask = t;
    transcript = "";
    transcriptLoading = true;
    try {
      transcript = await Bridge.AgentTranscript(t.id);
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      transcriptLoading = false;
    }
  }
  function closeTranscript() {
    openTask = null;
    transcript = "";
  }
  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && openTask) closeTranscript();
  }

  const filters: { key: typeof filter; label: string }[] = [
    { key: "all", label: "All" },
    { key: "running", label: "Running" },
    { key: "done", label: "Done" },
    { key: "error", label: "Errored" },
  ];
</script>

<svelte:window {onkeydown} />

<div class="agents">
  <header class="agents__head">
    <div class="agents__kpis">
      <div class="kpi kpi--running"><span class="kpi__v tnum">{data?.running ?? 0}</span><span class="kpi__l">running</span></div>
      <div class="kpi kpi--done"><span class="kpi__v tnum">{data?.done ?? 0}</span><span class="kpi__l">done</span></div>
      <div class="kpi kpi--error"><span class="kpi__v tnum">{data?.errored ?? 0}</span><span class="kpi__l">errored</span></div>
    </div>
    <div class="agents__filters" role="tablist" aria-label="Filter tasks">
      {#each filters as f (f.key)}
        <button
          class="agents__filter"
          class:agents__filter--on={filter === f.key}
          role="tab"
          aria-selected={filter === f.key}
          onclick={() => (filter = f.key)}
        >
          {f.label}
        </button>
      {/each}
    </div>
  </header>

  {#if loading && !data}
    <div class="agents__list agents__list--pad">
      {#each Array(4) as _, i (i)}<div class="agents__skel"></div>{/each}
    </div>
  {:else if !data || data.tasks.length === 0}
    <EmptyState glyph="⋔" title="No agent tasks" line="When the agent delegates subtasks (task / task_group), their live fan-out shows up here." />
  {:else if tasks.length === 0}
    <div class="agents__list agents__list--pad">
      <p class="agents__empty-note">No {filter} tasks.</p>
    </div>
  {:else}
    <div class="agents__list">
      <VirtualList items={tasks} estimateHeight={132} key={(t) => t.id}>
        {#snippet row(t)}
          <div class="ag-wrap">
            <Card>
              <div class="ag">
                <div class="ag__main">
                  <div class="ag__top">
                    <StatusDot state={dotState(t.status)} size={8} pulse={t.status === "running"} />
                    <span class="ag__id tnum">{t.id}</span>
                    <Badge tone={tone(t.status)}>{t.canceling ? "canceling" : t.status}</Badge>
                    {#if t.role}<Badge tone="info">{t.role}</Badge>{/if}
                    {#if t.where}<Badge tone="neutral" truncate>{t.where}</Badge>{/if}
                    <span class="ag__elapsed tnum">{elapsed(t)}</span>
                  </div>
                  <p class="ag__task">{t.task}</p>
                  {#if t.status === "running"}
                    <div class="ag__live">
                      {#if t.lastTool}<span class="ag__tool">{t.lastTool}</span>{/if}
                      {#if t.steps}<span class="ag__metric tnum">{t.steps} steps</span>{/if}
                      {#if t.lastNote}<span class="ag__note">{t.lastNote}</span>{/if}
                    </div>
                  {:else if t.error}
                    <p class="ag__error">{t.error}</p>
                  {/if}
                  {#if (t.inTokens ?? 0) + (t.outTokens ?? 0) > 0}
                    <div class="ag__tokens tnum">
                      ↑{(t.inTokens ?? 0).toLocaleString()} ↓{(t.outTokens ?? 0).toLocaleString()}
                      {#if t.attempts && t.attempts > 1}· {t.attempts} attempts{/if}
                      {#if t.escalated}· escalated{/if}
                    </div>
                  {/if}
                </div>
                <div class="ag__actions">
                  <Button variant="ghost" size="sm" onclick={() => openTranscript(t)}>Transcript</Button>
                  {#if t.status === "running"}
                    <Button variant="danger" size="sm" loading={acting[t.id]} disabled={t.canceling} onclick={() => cancel(t.id)}>
                      {t.canceling ? "Stopping…" : "Cancel"}
                    </Button>
                  {/if}
                </div>
              </div>
            </Card>
          </div>
        {/snippet}
      </VirtualList>
    </div>
  {/if}
</div>

{#if openTask}
  <div
    class="sheet__scrim"
    role="button"
    tabindex="0"
    aria-label="Close transcript"
    onclick={closeTranscript}
    onkeydown={(e) => (e.key === "Enter" || e.key === " ") && closeTranscript()}
  ></div>
  <div class="sheet" role="dialog" aria-modal="true" aria-label="Task {openTask.id} transcript">
    <header class="sheet__head">
      <div class="sheet__title-wrap">
        <StatusDot state={dotState(openTask.status)} size={8} pulse={openTask.status === "running"} />
        <h2 class="sheet__title tnum">{openTask.id}</h2>
        <Badge tone={tone(openTask.status)}>{openTask.status}</Badge>
      </div>
      <Button variant="icon" size="md" title="Close" onclick={closeTranscript}>✕</Button>
    </header>
    <p class="sheet__task">{openTask.task}</p>
    {#if openTask.result}
      <div class="sheet__section-label">result</div>
      <div class="sheet__result selectable">{openTask.result}</div>
    {/if}
    <div class="sheet__section-label">transcript</div>
    <div class="sheet__body selectable">
      {#if transcriptLoading}
        <div class="sheet__loading">Loading…</div>
      {:else if transcript}
        <CodeBlock code={transcript} lang="json" />
      {:else}
        <p class="agents__empty-note">No transcript snapshot on disk for this task.</p>
      {/if}
    </div>
  </div>
{/if}

<style>
  .agents {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .agents__head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-5);
    padding: var(--sp-6) var(--sp-9);
    border-bottom: 1px solid var(--border-hairline);
  }
  .agents__kpis {
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
  }
  .kpi--running .kpi__v {
    color: var(--brand);
  }
  .kpi--done .kpi__v {
    color: var(--success);
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
  .agents__filters {
    display: inline-flex;
    background: var(--bg-well);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    padding: var(--sp-1);
    gap: var(--sp-1);
  }
  .agents__filter {
    height: 26px;
    padding: 0 var(--sp-4);
    border: none;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--r-sm);
    cursor: pointer;
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-sans);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .agents__filter:hover {
    color: var(--text-primary);
  }
  .agents__filter:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .agents__filter--on {
    background: var(--bg-raised-2);
    color: var(--text-primary);
  }
  /* Bounded region for VirtualList (which owns its own internal scroll). */
  .agents__list {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  /* Skeleton/empty branches scroll + pad normally (no VirtualList). */
  .agents__list--pad {
    overflow-y: auto;
    padding: var(--sp-7) var(--sp-9);
    gap: var(--sp-5);
  }
  /* Per-row vertical rhythm + the horizontal page margins VirtualList rows
     can't carry (rows are absolutely positioned full-width). */
  .ag-wrap {
    padding: var(--sp-3) var(--sp-9);
  }
  .agents__skel {
    height: 96px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: ag-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes ag-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .agents__empty-note {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    margin: 0;
  }
  .ag {
    display: flex;
    gap: var(--sp-5);
    padding: var(--sp-5);
  }
  .ag__main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .ag__top {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    flex-wrap: wrap;
  }
  .ag__id {
    font-size: var(--fs-body-sm);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
  }
  .ag__elapsed {
    margin-left: auto;
    font-size: var(--fs-label);
    color: var(--text-faint);
  }
  .ag__task {
    margin: 0;
    color: var(--text-primary);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .ag__live {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .ag__tool {
    color: var(--working);
    font-weight: var(--fw-medium);
  }
  .ag__note {
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ag__error {
    margin: 0;
    color: var(--error);
    font-size: var(--fs-label);
    line-height: var(--lh-snug);
  }
  .ag__tokens {
    font-size: var(--fs-micro);
    color: var(--text-ghost);
  }
  .ag__actions {
    flex: none;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    align-items: flex-end;
  }

  /* SLIDE-OVER */
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
    width: min(620px, 84vw);
    background: var(--bg-raised);
    border-left: 1px solid var(--border-subtle);
    box-shadow: var(--shadow-3);
    z-index: 51;
    display: flex;
    flex-direction: column;
    padding: var(--sp-7);
    gap: var(--sp-4);
    animation: sheet-in var(--dur-base) var(--ease-out);
  }
  .sheet__head {
    display: flex;
    align-items: center;
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
    font: var(--fw-semibold) var(--fs-h3) / 1.2 var(--font-display);
    color: var(--text-primary);
  }
  .sheet__task {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
  }
  .sheet__section-label {
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    margin-top: var(--sp-4);
  }
  .sheet__result {
    background: var(--bg-well);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-sm);
    padding: var(--sp-4);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
    color: var(--text-primary);
    white-space: pre-wrap;
    max-height: 200px;
    overflow-y: auto;
  }
  .sheet__body {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
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
    .sheet,
    .agents__skel {
      animation: none;
    }
  }
</style>
