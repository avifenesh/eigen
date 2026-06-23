<script lang="ts">
  // Dreaming — the memory-consolidation timeline. "Dreaming" is the background
  // distillation of sessions into durable memory. This view shows two strands
  // per scope: per-session rollout summaries (what each session distilled to)
  // and consolidation snapshots (each time MEMORY.md was rewritten). A
  // consolidation opens a diff of that snapshot against the current memory, so
  // you can see exactly what a dream changed. All local files — read directly.
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { DreamingDTO, DreamingScopeDTO, ConsolidationDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Markdown from "$lib/components/Markdown.svelte";
  import DiffView from "$lib/components/DiffView.svelte";
  import VirtualList from "$lib/components/VirtualList.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import { trapFocus } from "$lib/actions";

  let data = $state<DreamingDTO | null>(null);
  let scope = $state<"project" | "global">("project");
  let strand = $state<"rollouts" | "consolidations">("rollouts");
  let loading = $state(true);

  // Diff slide-over state.
  let openCons = $state<ConsolidationDTO | null>(null);
  let diffPatch = $state("");
  let diffLoading = $state(false);

  const current = $derived<DreamingScopeDTO | null>(
    scope === "project" ? (data?.project ?? null) : (data?.global ?? null),
  );

  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    try {
      const d = await Bridge.Dreaming();
      if (seq === loadSeq) data = d;
    } catch (e) {
      if (seq === loadSeq) toasts.error(e instanceof Error ? e.message : String(e));
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

  // Build a unified-diff string between a consolidation snapshot (before) and
  // the current memory (after), so DiffView can render what the dream changed.
  async function openDiff(c: ConsolidationDTO) {
    openCons = c;
    diffPatch = "";
    diffLoading = true;
    try {
      const [before, after] = await Promise.all([
        Bridge.ConsolidationContent(c.path),
        Bridge.CurrentMemory(scope),
      ]);
      diffPatch = makeUnifiedDiff(before, after, c.label, "current");
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      diffLoading = false;
    }
  }
  function closeDiff() {
    openCons = null;
    diffPatch = "";
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
  function relTime(ms: number): string {
    if (!ms) return "";
    const diff = Date.now() - ms;
    const m = Math.floor(diff / 60000);
    if (m < 1) return "just now";
    if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h ago`;
    return `${Math.floor(h / 24)}d ago`;
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
    <div class="dream__scopes" role="tablist" aria-label="Memory scope">
      <button class="dream__seg" class:dream__seg--on={scope === "project"} role="tab" aria-selected={scope === "project"} onclick={() => (scope = "project")}>Project</button>
      <button class="dream__seg" class:dream__seg--on={scope === "global"} role="tab" aria-selected={scope === "global"} onclick={() => (scope = "global")}>Global</button>
    </div>
    <div class="dream__strands" role="tablist" aria-label="Timeline strand">
      <button class="dream__seg" class:dream__seg--on={strand === "rollouts"} role="tab" aria-selected={strand === "rollouts"} onclick={() => (strand = "rollouts")}>
        Rollouts {#if current}<span class="dream__n tnum">{current.rollouts.length}</span>{/if}
      </button>
      <button class="dream__seg" class:dream__seg--on={strand === "consolidations"} role="tab" aria-selected={strand === "consolidations"} onclick={() => (strand = "consolidations")}>
        Consolidations {#if current}<span class="dream__n tnum">{current.consolidations.length}</span>{/if}
      </button>
    </div>
  </header>

  {#if loading && !data}
    <div class="dream__body">
      {#each Array(4) as _, i (i)}<div class="dream__skel"></div>{/each}
    </div>
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
                      {#if r.outcome}<Badge tone={outcomeTone(r.outcome)}>{r.outcome}</Badge>{/if}
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
                  {#if c.whenMs}<span class="cons__when">{relTime(c.whenMs)}</span>{/if}
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
    padding: var(--sp-6) var(--sp-9);
    border-bottom: 1px solid var(--border-hairline);
  }
  .dream__scopes,
  .dream__strands {
    display: inline-flex;
    background: var(--bg-well);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    padding: var(--sp-1);
    gap: var(--sp-1);
  }
  .dream__seg {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-3);
    height: 28px;
    padding: 0 var(--sp-5);
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
  .dream__seg:hover {
    color: var(--text-primary);
  }
  .dream__seg:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .dream__seg--on {
    background: var(--bg-raised-2);
    color: var(--text-primary);
  }
  .dream__n {
    font-size: var(--fs-micro);
    color: var(--text-faint);
  }
  .dream__seg--on .dream__n {
    color: var(--brand);
  }
  .dream__body {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-7) var(--sp-9);
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
    padding: 0 var(--sp-9);
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
  .dream__skel {
    height: 120px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: dream-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes dream-shimmer {
    to {
      background-position: -200% 0;
    }
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
    .sheet,
    .dream__skel {
      animation: none;
    }
  }
</style>
