<script lang="ts">
  // FILE EXPLORER for the session's primary root. Reads a depth-limited tree via
  // FileTree(dir) and renders collapsible folders; clicking a file loads its text
  // via ReadFileForView(path) into a read-only viewer pane. Read-only by design —
  // this is for orienting + reading, not editing (the agent edits via its tools).
  import { Bridge } from "$lib/bridge";
  import type { FileTreeDTO, FileEntryDTO } from "$lib/types";
  import Button from "./Button.svelte";
  import EmptyState from "./EmptyState.svelte";
  import CodeBlock from "./CodeBlock.svelte";

  let { dir }: { dir: string } = $props();

  let tree = $state<FileTreeDTO | null>(null);
  let loading = $state(false);
  let error = $state<string | null>(null);
  // Open folder paths (collapsible). Top-level entries start expanded.
  let openDirs = $state<Set<string>>(new Set());

  // Selected file view.
  let viewPath = $state<string | null>(null);
  let viewText = $state("");
  let viewLoading = $state(false);
  let viewError = $state<string | null>(null);

  let seq = 0;
  async function load() {
    if (!dir) return;
    const s = ++seq;
    loading = true;
    error = null;
    try {
      const t = await Bridge.FileTree(dir);
      if (s === seq) {
        tree = t;
        // Expand the first level so the panel isn't a single collapsed root.
        openDirs = new Set((t?.entries ?? []).filter((e) => e.isDir).map((e) => e.path));
      }
    } catch (e) {
      if (s === seq) error = e instanceof Error ? e.message : String(e);
    } finally {
      if (s === seq) loading = false;
    }
  }

  $effect(() => {
    void dir;
    load();
  });

  function toggleDir(path: string) {
    const next = new Set(openDirs);
    if (next.has(path)) next.delete(path);
    else next.add(path);
    openDirs = next;
  }

  let viewSeq = 0;
  async function openFile(entry: FileEntryDTO) {
    if (entry.isDir) {
      toggleDir(entry.path);
      return;
    }
    const s = ++viewSeq;
    viewPath = entry.path;
    viewText = "";
    viewError = null;
    viewLoading = true;
    try {
      const text = await Bridge.ReadFileForView(entry.path);
      if (s === viewSeq) viewText = text;
    } catch (e) {
      if (s === viewSeq) viewError = e instanceof Error ? e.message : String(e);
    } finally {
      if (s === viewSeq) viewLoading = false;
    }
  }

  // Pick a Markdown/code language hint from the extension for the viewer.
  function langOf(path: string): string {
    const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
    const map: Record<string, string> = {
      ts: "typescript", js: "javascript", svelte: "svelte", go: "go", rs: "rust",
      py: "python", json: "json", md: "markdown", sh: "bash", css: "css", html: "html",
      yml: "yaml", yaml: "yaml", toml: "toml",
    };
    return map[ext] ?? "";
  }
  function baseName(p: string): string {
    return p.slice(p.lastIndexOf("/") + 1);
  }
</script>

<div class="fp">
  <div class="fp__bar">
    <span class="fp__title">files</span>
    <Button variant="ghost" size="sm" loading={loading} onclick={load} title="Reload the file tree">refresh</Button>
  </div>

  {#if loading && !tree}
    <div class="fp__note">Loading…</div>
  {:else if error}
    <EmptyState glyph="⊟" title="Couldn't read files" line={error}>
      {#snippet action()}<Button variant="secondary" onclick={load}>Retry</Button>{/snippet}
    </EmptyState>
  {:else if !tree || tree.entries.length === 0}
    <EmptyState glyph="⊟" title="No files" line="Nothing to show in this directory." />
  {:else}
    <div class="fp__split">
      <div class="fp__tree">
        {#each tree.entries as entry (entry.path)}
          {@render node(entry, 0)}
        {/each}
        {#if tree.truncated}<div class="fp__note fp__note--warn">Tree truncated (large directory).</div>{/if}
      </div>
      {#if viewPath}
        <div class="fp__view">
          <div class="fp__view-head">
            <span class="fp__view-name" title={viewPath}>{baseName(viewPath)}</span>
            <Button variant="icon" size="sm" title="Close" onclick={() => (viewPath = null)}>✕</Button>
          </div>
          <div class="fp__view-body">
            {#if viewLoading}
              <div class="fp__note">Loading…</div>
            {:else if viewError}
              <div class="fp__note fp__note--warn">{viewError}</div>
            {:else}
              <CodeBlock code={viewText} lang={langOf(viewPath)} />
            {/if}
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>

{#snippet node(entry: FileEntryDTO, depth: number)}
  <button
    class="fp__row"
    class:fp__row--sel={entry.path === viewPath}
    style="padding-left: calc({depth} * var(--sp-5) + var(--sp-4))"
    onclick={() => openFile(entry)}
    title={entry.path}
  >
    <span class="fp__glyph" aria-hidden="true">
      {#if entry.isDir}{openDirs.has(entry.path) ? "▾" : "▸"}{:else}·{/if}
    </span>
    <span class="fp__name">{entry.name}</span>
  </button>
  {#if entry.isDir && openDirs.has(entry.path) && entry.children}
    {#each entry.children as child (child.path)}
      {@render node(child, depth + 1)}
    {/each}
  {/if}
{/snippet}

<style>
  .fp {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .fp__bar {
    flex: none;
    display: flex;
    align-items: center;
    padding: var(--sp-4) var(--sp-5);
    border-bottom: 1px solid var(--border-hairline);
  }
  .fp__title {
    font: var(--fw-semibold) var(--fs-body-sm) / 1 var(--font-sans);
    color: var(--text-secondary);
  }
  .fp__bar :global(button) {
    margin-left: auto;
  }
  .fp__split {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .fp__tree {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--sp-3) 0;
  }
  .fp__row {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    width: 100%;
    padding: 3px var(--sp-4);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: pointer;
    text-align: left;
    font: var(--fw-regular) var(--fs-label) / 1.2 var(--font-sans);
  }
  .fp__row:hover {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .fp__row--sel {
    background: var(--state-selected);
    color: var(--brand-bright);
  }
  .fp__glyph {
    flex: none;
    width: 12px;
    text-align: center;
    color: var(--text-faint);
  }
  .fp__name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .fp__view {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    border-top: 1px solid var(--border-hairline);
  }
  .fp__view-head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-4);
    padding: var(--sp-3) var(--sp-5);
    border-bottom: 1px solid var(--border-hairline);
  }
  .fp__view-name {
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-mono, monospace);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .fp__view-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: var(--sp-4);
  }
  .fp__note {
    padding: var(--sp-5);
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  .fp__note--warn {
    color: var(--warn);
  }
</style>
