<script lang="ts">
  // Working-tree DIFF of the current changes (vs HEAD) for the session's primary
  // root. Reads via WorkingDiff(dir) — a git subprocess on the host — and renders
  // the unified patch with the shared DiffView. Manual refresh + a per-file stat
  // strip. Not a live watcher: a Refresh button re-reads on demand (cheap).
  import { Bridge } from "$lib/bridge";
  import { errText } from "$lib/errors";
  import type { WorkingDiffDTO } from "$lib/types";
  import DiffView from "./DiffView.svelte";
  import Button from "./Button.svelte";
  import EmptyState from "./EmptyState.svelte";

  let { dir }: { dir: string } = $props();

  let data = $state<WorkingDiffDTO | null>(null);
  let loading = $state(false);
  let error = $state<string | null>(null);

  let seq = 0;
  async function load() {
    if (!dir) return;
    const s = ++seq;
    loading = true;
    error = null;
    try {
      const d = await Bridge.WorkingDiff(dir);
      if (s === seq) data = d;
    } catch (e) {
      if (s === seq) error = errText(e);
    } finally {
      if (s === seq) loading = false;
    }
  }

  // Reload whenever the target dir changes (session switch).
  $effect(() => {
    void dir;
    load();
  });
</script>

<div class="dp">
  <div class="dp__bar">
    <span class="dp__title">working changes</span>
    {#if data?.branch}<span class="dp__branch tnum">{data.branch}</span>{/if}
    <Button variant="ghost" size="sm" loading={loading} onclick={load} title="Refresh the working-tree diff">refresh</Button>
  </div>

  {#if loading && !data}
    <div class="dp__note">Loading…</div>
  {:else if error}
    <EmptyState glyph="⇄" title="Couldn't read changes" line={error}>
      {#snippet action()}<Button variant="secondary" onclick={load}>Retry</Button>{/snippet}
    </EmptyState>
  {:else if !data || !data.isRepo}
    <EmptyState glyph="⇄" title="Not a git repository" line="The working directory isn't under version control, so there's nothing to diff." />
  {:else if data.clean}
    <EmptyState glyph="✓" title="Working tree clean" line="No pending changes against HEAD." />
  {:else}
    {#if data.files.length > 0}
      <ul class="dp__files">
        {#each data.files as f (f.path)}
          <li class="dp__file">
            <span class="dp__path" title={f.path}>{f.path}</span>
            <span class="dp__stat">
              {#if f.adds > 0}<span class="dp__add tnum">+{f.adds}</span>{/if}
              {#if f.dels > 0}<span class="dp__del tnum">−{f.dels}</span>{/if}
            </span>
          </li>
        {/each}
      </ul>
    {/if}
    {#if data.truncated}<div class="dp__note dp__note--warn">Diff truncated (very large) — showing the start.</div>{/if}
    <div class="dp__patch"><DiffView patch={data.patch} /></div>
  {/if}
</div>

<style>
  .dp {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .dp__bar {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    padding: var(--sp-4) var(--sp-5);
    border-bottom: 1px solid var(--border-hairline);
  }
  .dp__title {
    font: var(--fw-semibold) var(--fs-body-sm) / 1 var(--font-sans);
    color: var(--text-secondary);
  }
  .dp__branch {
    color: var(--text-muted);
    font-size: var(--fs-label);
    padding: 2px var(--sp-3);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-sm);
  }
  .dp__bar :global(button) {
    margin-left: auto;
  }
  .dp__files {
    flex: none;
    list-style: none;
    margin: 0;
    padding: var(--sp-3) var(--sp-5);
    border-bottom: 1px solid var(--border-hairline);
    max-height: 28%;
    overflow-y: auto;
  }
  .dp__file {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    padding: 2px 0;
    font-size: var(--fs-label);
  }
  .dp__path {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl;
    text-align: left;
    color: var(--text-secondary);
  }
  .dp__stat {
    flex: none;
    display: flex;
    gap: var(--sp-3);
  }
  .dp__add {
    color: var(--success);
  }
  .dp__del {
    color: var(--error);
  }
  .dp__patch {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }
  .dp__note {
    padding: var(--sp-5);
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  .dp__note--warn {
    color: var(--warn);
  }
</style>
