<script lang="ts">
  // Notes — browse + read + write the Obsidian vault, directly in eigen (the
  // human peer of the obsidian_* agent tools). Left: search box + note list.
  // Right: read pane with an Edit toggle + a New-note action. Working-station
  // idea/notes capture without leaving the app.
  import { Bridge } from "$lib/bridge";
  import { errText } from "$lib/errors";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { NoteDTO, ObsidianStatusDTO } from "$lib/types";
  import Button from "$lib/components/Button.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import Markdown from "$lib/components/Markdown.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";

  let status = $state<ObsidianStatusDTO | null>(null);
  let notes = $state<NoteDTO[]>([]);
  let query = $state("");
  let selected = $state<NoteDTO | null>(null);
  let content = $state("");
  let editing = $state(false);
  let draft = $state("");
  let loading = $state(true);
  let saving = $state(false);
  // Inline new-note composer (replaces the native prompt() modal — a GTK dialog
  // is jarring against the calm panel + can't be themed). `creating` reveals an
  // input row in the list header; Enter/Create commits, Esc/Cancel dismisses.
  let creating = $state(false);
  let newName = $state("");
  let createInput = $state<HTMLInputElement | null>(null);

  let alive = true;
  let seq = 0;
  async function loadNotes() {
    const s = ++seq;
    loading = true;
    try {
      const st = await Bridge.ObsidianStatus();
      if (alive && s === seq) status = st;
      if (st?.available) {
        const list = await Bridge.ObsidianNotes(query.trim());
        if (alive && s === seq) notes = list;
      }
    } catch (e) {
      if (alive && s === seq) toasts.error(errText(e));
    } finally {
      if (alive && s === seq) loading = false;
    }
  }
  $effect(() => {
    loadNotes();
    return () => {
      alive = false;
      seq++;
    };
  });

  // Debounced search: re-query the vault shortly after typing stops.
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  function onSearch() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(loadNotes, 250);
  }

  async function open(n: NoteDTO) {
    selected = n;
    editing = false;
    content = "";
    try {
      content = await Bridge.ObsidianRead(n.path);
    } catch (e) {
      toasts.error(errText(e));
    }
  }
  function startEdit() {
    draft = content;
    editing = true;
  }
  async function save() {
    if (!selected) return;
    saving = true;
    try {
      await Bridge.ObsidianWrite(selected.path, draft, false);
      content = draft;
      editing = false;
      toasts.success("Saved " + selected.path);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      saving = false;
    }
  }
  function startCreate() {
    creating = true;
    newName = "";
    // Focus the field once it renders.
    setTimeout(() => createInput?.focus(), 0);
  }
  function cancelCreate() {
    creating = false;
    newName = "";
  }
  function onCreateKey(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      newNote();
    } else if (e.key === "Escape") {
      e.preventDefault();
      cancelCreate();
    }
  }
  async function newNote() {
    const name = newName.trim();
    if (!name) return;
    try {
      const rel = await Bridge.ObsidianWrite(name, "# " + name.replace(/\.md$/, "") + "\n\n", false);
      toasts.success("Created " + rel);
      creating = false;
      newName = "";
      await loadNotes();
      await open({ path: rel, title: rel.replace(/\.md$/, "") });
      startEdit();
    } catch (e) {
      toasts.error(errText(e));
    }
  }
</script>

<div class="notes">
  {#if !loading && status && !status.available}
    <EmptyState glyph="≣" title="No Obsidian vault" line="Set a vault in Connectors → Obsidian (Choose vault), then notes show here." />
  {:else}
    <aside class="notes__list">
      <div class="notes__search">
        <input class="notes__q" placeholder="Search notes…" bind:value={query} oninput={onSearch} />
        <Button variant="secondary" size="sm" onclick={startCreate}>New</Button>
      </div>
      {#if creating}
        <!-- Inline note composer — replaces the native prompt() dialog. -->
        <div class="notes__create">
          <input
            bind:this={createInput}
            class="notes__q notes__create-input"
            placeholder="Inbox/Idea.md"
            aria-label="New note path"
            bind:value={newName}
            onkeydown={onCreateKey}
          />
          <Button variant="primary" size="sm" onclick={newNote}>Create</Button>
          <Button variant="ghost" size="sm" onclick={cancelCreate}>Cancel</Button>
        </div>
      {/if}
      <div class="notes__rows">
        {#if loading && notes.length === 0}
          <Skeleton count={8} height="38px" radius="var(--r-sm)" gap="1px" margin="2px var(--sp-2)" />
        {:else if notes.length === 0}
          <p class="notes__empty">No notes{query ? " match" : ""}.</p>
        {:else}
          {#each notes as n (n.path)}
            <button class="noterow" class:noterow--on={selected?.path === n.path} onclick={() => open(n)}>
              <span class="noterow__title">{n.title}</span>
              <span class="noterow__path">{n.path}</span>
            </button>
          {/each}
        {/if}
      </div>
    </aside>

    <section class="notes__pane">
      {#if !selected}
        <div class="notes__placeholder">Pick a note, or create one.</div>
      {:else}
        <header class="notes__panehead">
          <span class="notes__panetitle">{selected.title}</span>
          <span class="notes__panepath">{selected.path}</span>
          <span class="notes__sp"></span>
          {#if editing}
            <Button variant="primary" size="sm" loading={saving} onclick={save}>Save</Button>
            <Button variant="ghost" size="sm" onclick={() => (editing = false)}>Cancel</Button>
          {:else}
            <Button variant="secondary" size="sm" onclick={startEdit}>Edit</Button>
          {/if}
        </header>
        {#if editing}
          <textarea class="notes__editor selectable" bind:value={draft}></textarea>
        {:else}
          <!-- Obsidian notes are markdown — render them, don't dump raw text in a
               <pre>. Edit mode still shows the raw source in the textarea. -->
          <div class="notes__read selectable"><Markdown source={content} /></div>
        {/if}
      {/if}
    </section>
  {/if}
</div>

<style>
  .notes {
    height: 100%;
    display: flex;
    min-height: 0;
  }
  .notes__list {
    flex: none;
    width: 300px;
    border-right: 1px solid var(--divider);
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .notes__search {
    display: flex;
    gap: var(--sp-2);
    padding: var(--sp-5) var(--sp-5) var(--sp-3);
  }
  .notes__q {
    flex: 1;
    height: 32px;
    padding: 0 var(--sp-4);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-sm);
    background: var(--bg-raised-2);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body-sm) / 1 var(--font-sans);
    outline: none;
  }
  .notes__q:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  /* Inline new-note composer row, sits just under the search row. */
  .notes__create {
    display: flex;
    gap: var(--sp-2);
    padding: 0 var(--sp-5) var(--sp-3);
  }
  .notes__create-input {
    min-width: 0;
  }
  .notes__rows {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: 0 var(--sp-3) var(--sp-4);
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .notes__empty {
    color: var(--text-ghost);
    font-size: var(--fs-label);
    padding: var(--sp-4);
  }
  .noterow {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: var(--sp-3) var(--sp-4);
    border: none;
    background: transparent;
    border-radius: var(--r-sm);
    cursor: pointer;
    text-align: left;
  }
  .noterow:hover {
    background: var(--state-hover);
  }
  .noterow--on {
    background: var(--state-selected);
  }
  .noterow__title {
    font: var(--fw-medium) var(--fs-body-sm) / 1.2 var(--font-sans);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .noterow__path {
    font-size: var(--fs-micro);
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .notes__pane {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .notes__placeholder {
    margin: auto;
    color: var(--text-ghost);
  }
  .notes__panehead {
    display: flex;
    align-items: baseline;
    gap: var(--sp-3);
    padding: var(--sp-5) var(--sp-6);
    border-bottom: 1px solid var(--divider);
  }
  .notes__panetitle {
    font: var(--fw-semibold) var(--fs-body) / 1 var(--font-sans);
    color: var(--text-primary);
  }
  .notes__panepath {
    font: var(--fw-regular) var(--fs-micro) / 1 var(--font-mono);
    color: var(--text-faint);
  }
  .notes__sp {
    flex: 1;
  }
  /* Rendered note: a scrolling prose pane (Markdown owns its own typography). */
  .notes__read {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--sp-6);
  }
  /* Raw markdown source while editing: mono, preserve line breaks. */
  .notes__editor {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    margin: 0;
    padding: var(--sp-6);
    font: var(--fw-regular) var(--fs-body-sm) / var(--lh-prose) var(--font-mono);
    white-space: pre-wrap;
    word-break: break-word;
    border: none;
    background: var(--bg-base);
    resize: none;
    outline: none;
    color: var(--text-primary);
  }
  /* The editor clears the engine outline above; restore a keyboard-focus ring
     (inset, since it's a full-bleed pane with no border of its own). */
  .notes__editor:focus-visible {
    box-shadow: inset 0 0 0 2px var(--border-brand-faint);
  }
</style>
