<script lang="ts">
  // Tasks — the multi-agent fan-out board. Background subtasks (task/task_group
  // delegations) persist to disk; this view polls them and shows live status:
  // running tasks with their current tool + elapsed time, completed/errored
  // results, model/route each ran on. A running task can be canceled; any task's
  // transcript can be opened in a slide-over. Polling lives in an $effect whose
  // cleanup clears the interval — no leaked timer on nav.
  import { errText } from "$lib/errors";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { now } from "$lib/stores/clock.svelte";
  import { taskDot } from "$lib/status";
  import { trapFocus } from "$lib/actions";
  import type { TasksDTO, BgTaskDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import CodeBlock from "$lib/components/CodeBlock.svelte";
  import Markdown from "$lib/components/Markdown.svelte";
  import VirtualList from "$lib/components/VirtualList.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";

  let data = $state<TasksDTO | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let filter = $state<"all" | "running" | "done" | "error">("all");

  let openTask = $state<BgTaskDTO | null>(null);
  let transcript = $state("");
  let transcriptLoading = $state(false);
  let acting = $state<Record<string, boolean>>({});

  async function api<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(path, {
      ...init,
      headers: { Accept: "application/json", ...(init?.headers ?? {}) },
    });
    if (!res.ok) {
      let detail = "";
      try {
        const body = (await res.json()) as { error?: string };
        detail = body.error ?? "";
      } catch {
        detail = await res.text().catch(() => "");
      }
      throw new Error(detail || `${res.status} ${res.statusText}`);
    }
    return (await res.json()) as T;
  }

  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    error = null;
    try {
      const d = await api<TasksDTO>("/api/tasks");
      if (seq === loadSeq) {
        data = d;
        loading = false;
      }
    } catch (e) {
      if (seq === loadSeq) {
        loading = false;
        error = errText(e);
      }
    }
  }

  // Poll while the view is mounted. A self-scheduling timeout reads the cadence
  // fresh after each load (fast while work is in flight, slow when idle) without
  // re-running the effect — so the timer isn't torn down and recreated on every
  // data change. Cleanup clears the pending timeout AND bumps loadSeq so a
  // late /api/tasks resolution after unmount is dropped.
  $effect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;
    async function tick() {
      // Skip the /api/tasks round-trip while the window is hidden (other
      // workspace / minimized) — at 1.5s while a task runs this hammered the
      // daemon off-screen. Keep rescheduling so it resumes when shown again.
      if (typeof document === "undefined" || !document.hidden) {
        await load();
      }
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
      await api<{ ok: boolean }>(`/api/tasks/${encodeURIComponent(id)}/cancel`, { method: "POST" });
      toasts.info("cancel requested");
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete acting[id];
    }
  }

  async function openTranscript(t: BgTaskDTO) {
    openTask = t;
    transcript = "";
    transcriptLoading = true;
    try {
      const body = await api<{ transcript: string }>(`/api/tasks/${encodeURIComponent(t.id)}/transcript`);
      transcript = body.transcript;
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      transcriptLoading = false;
    }
  }
  function closeTranscript() {
    openTask = null;
    transcript = "";
  }

  // The transcript on disk is a .jsonl: one JSON message per line, same shape as
  // session files (Go llm.Message — capitalized field names, no omitempty). Parse
  // it line-by-line into role/content cards rather than dumping the whole blob as
  // one JSON code surface. JSON.parse is guarded per line so a single truncated/
  // corrupt line degrades to a raw entry instead of losing the whole transcript.
  type TxToolCall = { id: string; name: string; args: string };
  type TxEntry = {
    i: number;
    role: string;
    text: string;
    reasoning: string;
    toolCalls: TxToolCall[];
    toolName: string;
    toolError: boolean;
    raw?: string; // set only when the line failed to parse — shown verbatim
  };

  // Bound the rendered card count so a very long transcript can't unspool an
  // unbounded DOM in the sheet. We keep the tail (most recent exchanges) and
  // note how many earlier lines were elided.
  const TX_MAX = 200;

  function str(v: unknown): string {
    return typeof v === "string" ? v : "";
  }
  function asArgs(v: unknown): string {
    if (v == null) return "";
    if (typeof v === "string") return v;
    try {
      return JSON.stringify(v, null, 2);
    } catch {
      return "";
    }
  }
  // Read a field case-tolerantly: the Go encoder emits capitalized keys (Role,
  // Text, …), but tolerate lowercased keys too in case the on-disk shape shifts.
  function field(o: Record<string, unknown>, ...keys: string[]): unknown {
    for (const k of keys) {
      if (o[k] != null) return o[k];
      const lc = k.charAt(0).toLowerCase() + k.slice(1);
      if (o[lc] != null) return o[lc];
    }
    return undefined;
  }

  const txEntries = $derived.by<TxEntry[]>(() => {
    const src = transcript ?? "";
    if (!src.trim()) return [];
    const out: TxEntry[] = [];
    let i = 0;
    for (const line of src.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      const idx = i++;
      try {
        const o = JSON.parse(trimmed) as Record<string, unknown>;
        const rawCalls = field(o, "ToolCalls");
        const toolCalls: TxToolCall[] = Array.isArray(rawCalls)
          ? rawCalls.map((c) => {
              const cc = (c ?? {}) as Record<string, unknown>;
              return {
                id: str(field(cc, "ID", "Id")),
                name: str(field(cc, "Name")),
                args: asArgs(field(cc, "Arguments", "Args")),
              };
            })
          : [];
        out.push({
          i: idx,
          role: str(field(o, "Role")) || "message",
          text: str(field(o, "Text")),
          reasoning: str(field(o, "Reasoning")),
          toolCalls,
          toolName: str(field(o, "ToolName")),
          toolError: field(o, "ToolError") === true,
        });
      } catch {
        // Tolerate a bad line: keep it as a verbatim entry rather than dropping
        // the surrounding transcript or throwing.
        out.push({
          i: idx,
          role: "unparsed",
          text: "",
          reasoning: "",
          toolCalls: [],
          toolName: "",
          toolError: false,
          raw: trimmed,
        });
      }
    }
    return out;
  });

  const txTotal = $derived(txEntries.length);
  // Tail-bounded view: render at most TX_MAX cards, keeping the most recent.
  const txShown = $derived(txTotal > TX_MAX ? txEntries.slice(txTotal - TX_MAX) : txEntries);
  const txElided = $derived(Math.max(0, txTotal - txShown.length));

  function roleTone(role: string): "neutral" | "brand" | "success" | "warn" | "error" | "info" {
    if (role === "tool") return "info";
    if (role === "unparsed") return "warn";
    return "neutral";
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
      <Skeleton count={4} height="96px" gap="var(--sp-5)" />
    </div>
  {:else if error && !data}
    <EmptyState glyph="⋔" title="Couldn't load tasks" line={error}>
      {#snippet action()}
        <Button variant="secondary" onclick={() => load()}>Retry</Button>
      {/snippet}
    </EmptyState>
  {:else if !data || data.tasks.length === 0}
    <EmptyState glyph="⋔" title="No tasks" line="When Eigen delegates subtasks (task / task_group), their live fan-out shows up here." />
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
                    <StatusDot state={taskDot(t.status)} size={8} pulse={t.status === "running"} />
                    <span class="ag__id tnum">{t.id}</span>
                    <Badge tone={tone(t.status)}>{t.canceling ? "canceling" : t.status}</Badge>
                    {#if t.role}<Badge tone="info">{t.role}</Badge>{/if}
                    {#if t.model}<Badge tone="brand" truncate>{t.model}</Badge>{/if}
                    {#if t.where}<Badge tone="neutral" truncate>{t.where}</Badge>{/if}
                    <span class="ag__elapsed tnum">{elapsed(t)}</span>
                  </div>
                  {#if t.kind || t.difficulty}
                    <div class="ag__route tnum">
                      {#if t.kind}<span class="ag__route-item">{t.kind}</span>{/if}
                      {#if t.difficulty}<span class="ag__route-item">{t.difficulty}</span>{/if}
                    </div>
                  {/if}
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
  <div class="sheet" role="dialog" aria-modal="true" tabindex="-1" use:trapFocus aria-label="Task {openTask.id} transcript">
    <header class="sheet__head">
      <div class="sheet__title-wrap">
        <StatusDot state={taskDot(openTask.status)} size={8} pulse={openTask.status === "running"} />
        <h2 class="sheet__title tnum">{openTask.id}</h2>
        <Badge tone={tone(openTask.status)}>{openTask.status}</Badge>
      </div>
      <Button variant="icon" size="md" title="Close" onclick={closeTranscript}>✕</Button>
    </header>
    {#if openTask.model || openTask.kind || openTask.difficulty || openTask.where}
      <div class="sheet__route">
        {#if openTask.model}<Badge tone="brand" truncate>{openTask.model}</Badge>{/if}
        {#if openTask.kind}<Badge tone="neutral">{openTask.kind}</Badge>{/if}
        {#if openTask.difficulty}<Badge tone="neutral">{openTask.difficulty}</Badge>{/if}
        {#if openTask.where}<Badge tone="neutral" truncate>{openTask.where}</Badge>{/if}
      </div>
    {/if}
    <p class="sheet__task">{openTask.task}</p>
    {#if openTask.result}
      <div class="sheet__section-label">result</div>
      <div class="sheet__result selectable sheet__result--md"><Markdown source={openTask.result} /></div>
    {/if}
    <div class="sheet__section-label">transcript</div>
    <div class="sheet__body selectable">
      {#if transcriptLoading}
        <div class="sheet__loading">Loading…</div>
      {:else if txShown.length > 0}
        <!-- Parsed .jsonl: one role/content card per message line. Tail-bounded
             so a long run can't unspool an unbounded DOM in the sheet. -->
        {#if txElided > 0}
          <div class="tx__elided">{txElided.toLocaleString()} earlier {txElided === 1 ? "message" : "messages"} hidden — showing the most recent {txShown.length.toLocaleString()}.</div>
        {/if}
        <ol class="tx">
          {#each txShown as m (m.i)}
            <li class="tx__msg" class:tx__msg--error={m.toolError || m.role === "unparsed"}>
              <div class="tx__head">
                <Badge tone={roleTone(m.role)}>{m.role}</Badge>
                {#if m.toolName}<span class="tx__toolname">{m.toolName}</span>{/if}
                {#if m.toolError}<Badge tone="error">error</Badge>{/if}
              </div>
              {#if m.reasoning}
                <div class="tx__reasoning">
                  <span class="tx__tag">reasoning</span>
                  <div class="tx__md"><Markdown source={m.reasoning} /></div>
                </div>
              {/if}
              {#if m.text}
                {#if m.role === "assistant" || m.role === "user" || m.role === "system"}
                  <div class="tx__text tx__text--md"><Markdown source={m.text} /></div>
                {:else}
                  <div class="tx__text">{m.text}</div>
                {/if}
              {/if}
              {#each m.toolCalls as c (c.id || c.name)}
                <div class="tx__call">
                  <span class="tx__call-name">{c.name || "tool"}</span>
                  {#if c.args}<div class="tx__call-args"><CodeBlock code={c.args} lang="json" /></div>{/if}
                </div>
              {/each}
              {#if m.raw}
                <pre class="tx__raw" title="This line could not be parsed as JSON">{m.raw}</pre>
              {/if}
            </li>
          {/each}
        </ol>
      {:else if transcript}
        <!-- Snapshot exists but parsed to nothing usable — show it verbatim. -->
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
    padding: var(--sp-6) var(--sp-7);
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
    padding: var(--sp-7) var(--sp-7);
    gap: var(--sp-5);
  }
  /* Per-row vertical rhythm + the horizontal page margins VirtualList rows
     can't carry (rows are absolutely positioned full-width). */
  .ag-wrap {
    padding: var(--sp-3) var(--sp-9);
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
  .ag__route {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    font-size: var(--fs-label);
    color: var(--text-muted);
    flex-wrap: wrap;
  }
  .ag__route-item {
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
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
  .sheet__route {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    flex-wrap: wrap;
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
  .sheet__result--md {
    white-space: normal;
    line-height: var(--lh-prose);
  }
  .sheet__result--md :global(.md) {
    font-size: var(--fs-body-sm);
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

  /* ── parsed transcript ─────────────────────────────────────────────────── */
  .tx__elided {
    font-size: var(--fs-label);
    color: var(--text-faint);
    text-align: center;
    padding-bottom: var(--sp-4);
  }
  .tx {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .tx__msg {
    border: 1px solid var(--border-hairline);
    border-left: 2px solid var(--border-subtle);
    border-radius: var(--r-sm);
    background: var(--bg-raised);
    padding: var(--sp-4);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .tx__msg--error {
    border-left-color: var(--error);
  }
  .tx__head {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    flex-wrap: wrap;
  }
  .tx__toolname {
    font: var(--fw-regular) var(--fs-code-sm) / 1 var(--font-mono);
    color: var(--text-secondary);
  }
  .tx__tag {
    display: block;
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    margin-bottom: var(--sp-2);
  }
  .tx__reasoning {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
    word-break: break-word;
  }
  .tx__md :global(.md) {
    font-size: inherit;
    color: inherit;
    line-height: var(--lh-prose);
  }
  .tx__md :global(.md-p) {
    margin: var(--sp-2) 0;
  }
  .tx__text {
    color: var(--text-primary);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
    word-break: break-word;
  }
  .tx__text--md {
    line-height: var(--lh-prose);
  }
  .tx__text--md :global(.md) {
    font-size: var(--fs-body-sm);
  }
  .tx__call {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .tx__call-name {
    font: var(--fw-medium) var(--fs-code-sm) / 1 var(--font-mono);
    color: var(--info);
  }
  .tx__call-args {
    margin: 0;
    max-height: 280px;
    overflow: auto;
  }
  .tx__call-args :global(.code) {
    border-radius: var(--r-xs);
  }
  .tx__call-args :global(.code__body) {
    max-height: 240px;
  }
  .tx__raw {
    margin: 0;
    background: var(--syn-bg);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-xs);
    padding: var(--sp-3);
    max-height: 220px;
    overflow: auto;
    font: var(--fw-regular) var(--fs-code-sm) / var(--lh-code) var(--font-mono);
    color: var(--syn-text);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .tx__raw {
    color: var(--text-muted);
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
  }
</style>
