<script lang="ts">
  // Dreaming — the memory-consolidation timeline. "Dreaming" is the background
  // distillation of sessions into durable memory. This view shows two strands
  // per scope: per-session rollout summaries (what each session distilled to)
  // and consolidation snapshots (each time MEMORY.md was rewritten). A
  // consolidation opens a diff of that snapshot against the current memory, so
  // you can see exactly what a dream changed. All local files — read directly.
  import { Bridge } from "$lib/bridge";
  import { errText } from "$lib/errors";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { relTime as sharedRelTime } from "$lib/status";
  import type { DreamingScopeDTO, ConsolidationDTO, MemoryScopeRefDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Dropdown from "$lib/components/Dropdown.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Markdown from "$lib/components/Markdown.svelte";
  import DiffView from "$lib/components/DiffView.svelte";
  import VirtualList from "$lib/components/VirtualList.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import Segmented from "$lib/components/Segmented.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";
  import { trapFocus } from "$lib/actions";

  // The selectable scopes (Global first, then every known project). The picker
  // binds to `scope` — a scope KEY that round-trips through DreamingForScope (the
  // backend accepts "global", "project"/"", an abs dir, or an on-disk key). We
  // open the cwd project by default ("project") for session continuity.
  let scopes = $state<MemoryScopeRefDTO[]>([]);
  let scope = $state<string>("project");
  // The opened scope's dreaming DTO — loaded on demand via DreamingForScope,
  // replacing the old two-field {project, global} payload.
  let current = $state<DreamingScopeDTO | null>(null);
  let strand = $state<"rollouts" | "consolidations">("rollouts");
  let loading = $state(true);
  let error = $state<string | null>(null);

  // Diff slide-over state.
  let openCons = $state<ConsolidationDTO | null>(null);
  let diffPatch = $state("");
  let diffLoading = $state(false);
  let diffError = $state<string | null>(null);

  // The selected scope's ref — for the dir label next to the picker.
  const selectedRef = $derived<MemoryScopeRefDTO | null>(
    scopes.find((s) => s.key === scope) ?? null,
  );

  // The picker list — fetched once on mount. Reassigned wholesale for
  // reactivity. A late resolution after unmount is harmless; guard with `alive`
  // so we don't toast after teardown.
  let alive = true;
  async function loadScopes() {
    try {
      const refs = await Bridge.ListMemoryScopes();
      if (!alive) return;
      scopes = refs;
      // Relabel the picker from the "project" alias to the canonical key of the
      // cwd project (the ref flagged `current`), so it shows "eigen (10)" rather
      // than a placeholder. The dreaming data already loads under the alias.
      if (scope === "project") {
        const cur = refs.find((r) => r.current);
        if (cur) scope = cur.key;
      }
    } catch (e) {
      if (alive) toasts.error(errText(e));
    }
  }

  // alive guard: a slow DreamingForScope() resolution for an old selection must
  // not clobber the data of a newer one (or write after unmount). Monotonic seq.
  let loadSeq = 0;
  async function loadScope(key: string) {
    const seq = ++loadSeq;
    loading = true;
    error = null;
    try {
      const d = await Bridge.DreamingForScope(key);
      if (seq === loadSeq) current = d;
    } catch (e) {
      if (seq === loadSeq) error = errText(e);
    } finally {
      if (seq === loadSeq) loading = false;
    }
  }
  // Switching the picker re-opens the chosen scope.
  function selectScope(key: string) {
    if (key === scope) return;
    scope = key;
    loadScope(key);
  }
  function shortDir(d: string): string {
    const p = d.replace(/\/$/, "").split("/");
    return p[p.length - 1] || d;
  }
  // Run consolidation + summary for the current scope on demand. The daemon
  // dreams on its own cadence; this lets the user fold pending notes into
  // MEMORY.md + regenerate the injected summary now, then reloads the timeline.
  let dreaming = $state(false);
  async function dreamNow() {
    if (dreaming) return;
    dreaming = true;
    try {
      const r = await Bridge.DreamNow(scope);
      toasts.success(r?.report || (r?.changed ? "consolidated memory" : "nothing new to consolidate"));
      await loadScope(scope);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      dreaming = false;
    }
  }

  // Mount: enumerate scopes, then open the initial selection.
  $effect(() => {
    alive = true;
    loadScopes();
    loadScope(scope);
    return () => {
      alive = false;
      loadSeq++;
    };
  });

  // Build a unified-diff string between a consolidation snapshot (before) and
  // the current memory (after), so DiffView can render what the dream changed.
  // diffSeq: opening consolidation A then quickly B before A's Promise.all
  // resolves must not let A's now-stale result land after B's and overwrite
  // B's correct diff.
  let diffSeq = 0;
  async function openDiff(c: ConsolidationDTO) {
    const s = ++diffSeq;
    openCons = c;
    diffPatch = "";
    diffError = null;
    diffLoading = true;
    try {
      const [before, after] = await Promise.all([
        Bridge.ConsolidationContent(c.path),
        Bridge.CurrentMemory(scope),
      ]);
      if (s !== diffSeq) return;
      diffPatch = makeUnifiedDiff(before, after, c.label, "current");
    } catch (e) {
      if (s !== diffSeq) return;
      diffError = errText(e);
      toasts.error(diffError);
    } finally {
      if (s === diffSeq) diffLoading = false;
    }
  }
  function closeDiff() {
    openCons = null;
    diffPatch = "";
    diffError = null;
  }
  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && openCons) closeDiff();
  }

  function outcomeTone(o: string): "success" | "warn" | "error" | "neutral" {
    if (o === "success") return "success";
    if (o === "partial") return "warn";
    if (o === "failed") return "error";
    return "neutral";
  }
  // Local wrapper: whenMs is unix MILLISECONDS here (unlike the rest of the app,
  // which carries unix nanos), so convert before calling the shared relTime.
  function relTimeMs(ms: number): string {
    return sharedRelTime(ms * 1e6);
  }
  function title(text: string): string {
    const firstLine = text.split("\n").find((l) => l.trim()) ?? "";
    const stripped = firstLine.replace(/^#+\s*/, "").replace(/\*\*/g, "").trim();
    return stripped.length > 90 ? stripped.slice(0, 90) + "…" : stripped || "rollout";
  }
</script>

<svelte:window {onkeydown} />

<div class="dream">
  <header class="dream__head">
    <div class="dream__picker">
      <!-- N-scope picker: Global first (always present), then every known
           project. A custom Dropdown (NOT a native <select> — webkit2gtk draws
           the native option list black-on-black). Each option's `sub` carries
           the dir so two projects sharing a basename stay distinguishable. -->
      <Dropdown
        label="Memory scope"
        width={300}
        value={scope}
        options={scopes.map((s) => ({
          value: s.key,
          label: s.noteCount > 0 ? `${s.name} (${s.noteCount})` : s.name,
          sub: s.dir ? shortDir(s.dir) : undefined,
        }))}
        onchange={(v) => selectScope(v)}
      />
      {#if selectedRef?.dir}
        <span class="dream__dir" title={selectedRef.dir}>{shortDir(selectedRef.dir)}</span>
      {/if}
    </div>
    <Segmented
      ariaLabel="Timeline strand"
      variant="surface"
      value={strand}
      onChange={(v) => (strand = v as typeof strand)}
      options={[
        { value: "rollouts", label: "Rollouts", count: current?.rollouts.length },
        { value: "consolidations", label: "Consolidations", count: current?.consolidations.length },
      ]}
    />
    <div class="dream__actions">
      <Button variant="secondary" size="sm" loading={dreaming} title="Consolidate this scope's notes into memory + regenerate the injected summary now" onclick={dreamNow}>
        Dream now
      </Button>
    </div>
  </header>

  {#if loading && !current}
    <div class="dream__body">
      <Skeleton count={4} height="120px" gap="var(--sp-5)" />
    </div>
  {:else if error && !current}
    <EmptyState glyph="☾" title="Couldn't load dreaming" line={error}>
      {#snippet action()}
        <Button variant="secondary" onclick={() => loadScope(scope)}>Retry</Button>
      {/snippet}
    </EmptyState>
  {:else if !current || (current.rollouts.length === 0 && current.consolidations.length === 0)}
    <EmptyState glyph="☾" title="Nothing dreamed yet" line="As eigen reflects over recent sessions, distilled rollouts and memory consolidations appear here." />
  {:else if strand === "rollouts"}
    {#if current.rollouts.length === 0}
      <div class="dream__body"><p class="dream__empty-note">No rollout summaries in this scope yet.</p></div>
    {:else}
      <div class="dream__list">
        <VirtualList items={current.rollouts} estimateHeight={160} key={(r) => r.index}>
          {#snippet row(r)}
            <div class="dream__row">
              <div class="tl">
                <div class="tl__rail"><span class="tl__dot"></span></div>
                <Card>
                  <div class="roll">
                    <div class="roll__head">
                      <span class="roll__title">{title(r.text)}</span>
                      <div class="roll__meta">
                        {#if r.whenMs}<span class="roll__when">{relTimeMs(r.whenMs)}</span>{/if}
                        {#if r.outcome}<Badge tone={outcomeTone(r.outcome)}>{r.outcome}</Badge>{/if}
                      </div>
                    </div>
                    <div class="roll__body selectable"><Markdown source={r.text} /></div>
                  </div>
                </Card>
              </div>
            </div>
          {/snippet}
        </VirtualList>
      </div>
    {/if}
  {:else if current.consolidations.length === 0}
    <div class="dream__body"><p class="dream__empty-note">No consolidation snapshots yet. Memory is rewritten as it grows.</p></div>
  {:else}
    <div class="dream__body">
      <p class="dream__hint">Each snapshot is a point where memory was consolidated. Open one to diff it against the current memory.</p>
      <div class="cons">
        {#each current.consolidations as c, i (c.path)}
          <div class="tl">
            <div class="tl__rail"><span class="tl__dot" class:tl__dot--head={i === 0}></span></div>
            <Card interactive onclick={() => openDiff(c)} title="Diff against current memory">
              <div class="cons__row">
                <div class="cons__main">
                  <span class="cons__label">{c.label}</span>
                  {#if i === 0}<Badge tone="brand">latest</Badge>{/if}
                </div>
                <div class="cons__meta">
                  {#if c.whenMs}<span class="cons__when">{relTimeMs(c.whenMs)}</span>{/if}
                  <span class="cons__size tnum">{(c.bytes / 1024).toFixed(1)} KB</span>
                  <span class="cons__action">Diff →</span>
                </div>
              </div>
            </Card>
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>

{#if openCons}
  <div
    class="sheet__scrim"
    role="button"
    tabindex="0"
    aria-label="Close diff"
    onclick={closeDiff}
    onkeydown={(e) => (e.key === "Enter" || e.key === " ") && closeDiff()}
  ></div>
  <div class="sheet" role="dialog" aria-modal="true" tabindex="-1" use:trapFocus aria-label="Consolidation diff">
    <header class="sheet__head">
      <div class="sheet__title-wrap">
        <h2 class="sheet__title">Consolidation diff</h2>
        <Badge tone="neutral">{openCons.label} → current</Badge>
      </div>
      <Button variant="icon" size="md" title="Close" onclick={closeDiff}>✕</Button>
    </header>
    <div class="sheet__body">
      {#if diffLoading}
        <div class="sheet__loading">Loading…</div>
      {:else if diffError}
        {@const retryCons = openCons}
        <EmptyState glyph="☾" title="Couldn't load diff" line={diffError}>
          {#snippet action()}
            {#if retryCons}<Button variant="secondary" onclick={() => openDiff(retryCons)}>Retry</Button>{/if}
          {/snippet}
        </EmptyState>
      {:else if diffPatch}
        <DiffView patch={diffPatch} />
      {:else}
        <p class="dream__empty-note">No differences — this snapshot matches the current memory.</p>
      {/if}
    </div>
  </div>
{/if}

<script module lang="ts">
  // A minimal line-based unified diff (LCS) so the dreaming view can show what a
  // consolidation changed, rendered by the shared DiffView. Kept here (not a
  // dep) — memory files are small, so an O(n·m) LCS is fine.
  export function makeUnifiedDiff(before: string, after: string, aLabel: string, bLabel: string): string {
    const a = before.split("\n");
    const b = after.split("\n");
    if (before === after) return "";
    const n = a.length;
    const m = b.length;
    // LCS table
    const lcs: number[][] = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
      }
    }
    const out: string[] = [`--- ${aLabel}`, `+++ ${bLabel}`, "@@ memory @@"];
    let i = 0;
    let j = 0;
    while (i < n && j < m) {
      if (a[i] === b[j]) {
        out.push(" " + a[i]);
        i++;
        j++;
      } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
        out.push("-" + a[i]);
        i++;
      } else {
        out.push("+" + b[j]);
        j++;
      }
    }
    while (i < n) out.push("-" + a[i++]);
    while (j < m) out.push("+" + b[j++]);
    return out.join("\n");
  }
</script>

<style>
  .dream {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .dream__head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-5);
    padding: var(--sp-6) var(--sp-7);
    border-bottom: 1px solid var(--border-hairline);
  }
  .dream__actions {
    margin-left: auto;
  }
  /* Scope picker — a custom Dropdown over N scopes (Global + every project).
     The dir label sits beside it so projects sharing a basename stay distinct. */
  .dream__picker {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-5);
    min-width: 0;
  }
  .dream__dir {
    font-size: var(--fs-label);
    color: var(--text-faint);
    max-width: 240px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dream__body {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-7) var(--sp-7);
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .dream__list {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .dream__row {
    padding: 0 var(--sp-7);
  }
  .dream__hint {
    margin: 0;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  .dream__empty-note {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    margin: 0;
  }

  /* TIMELINE — a left rail with a node per entry. */
  .tl {
    display: flex;
    gap: var(--sp-5);
    padding-bottom: var(--sp-5);
  }
  .tl__rail {
    flex: none;
    width: 12px;
    display: flex;
    justify-content: center;
    position: relative;
  }
  .tl__rail::before {
    content: "";
    position: absolute;
    top: 0;
    bottom: 0;
    width: 1px;
    background: var(--border-subtle);
  }
  .tl__dot {
    position: relative;
    width: 9px;
    height: 9px;
    margin-top: var(--sp-4);
    border-radius: var(--r-full);
    background: var(--bg-overlay-2);
    border: 1px solid var(--border-strong);
  }
  .tl__dot--head {
    background: var(--brand);
    border-color: var(--brand);
  }
  .tl > :global(.card) {
    flex: 1;
    min-width: 0;
  }
  .roll {
    padding: var(--sp-5);
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .roll__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-4);
  }
  .roll__meta {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .roll__when {
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .roll__title {
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .roll__body {
    font-size: var(--fs-body-sm);
    line-height: var(--lh-prose);
    max-height: 220px;
    overflow-y: auto;
  }
  .cons {
    display: flex;
    flex-direction: column;
  }
  .cons__row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-5);
    padding: var(--sp-5);
  }
  .cons__main {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .cons__label {
    font-weight: var(--fw-medium);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
  }
  .cons__meta {
    display: flex;
    align-items: center;
    gap: var(--sp-5);
  }
  .cons__when {
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .cons__size {
    font-size: var(--fs-label);
    color: var(--text-faint);
  }
  .cons__action {
    font-size: var(--fs-label);
    color: var(--accent);
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
    width: min(680px, 86vw);
    background: var(--bg-raised);
    border-left: 1px solid var(--border-subtle);
    box-shadow: var(--shadow-3);
    z-index: 51;
    display: flex;
    flex-direction: column;
    padding: var(--sp-7);
    gap: var(--sp-5);
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
    .sheet {
      animation: none;
    }
  }
</style>
