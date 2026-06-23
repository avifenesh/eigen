<script lang="ts">
  // Memory — the durable-notes browser. Two scopes (project / global) as a
  // segmented switch. Each scope shows: a distilled summary (the injected view),
  // the append-only notes as virtualized cards, ad-hoc manual saves, bans, and
  // (global only) the editable user profile. Reads memory directly via the
  // bridge (memory is local filesystem; no daemon round-trip).
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { MemoryDTO, MemoryScopeDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Markdown from "$lib/components/Markdown.svelte";
  import VirtualList from "$lib/components/VirtualList.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let data = $state<MemoryDTO | null>(null);
  let scope = $state<"project" | "global">("project");
  let loading = $state(true);
  let composing = $state(false);
  let draft = $state("");
  let saving = $state(false);
  let editingProfile = $state(false);
  let profileDraft = $state("");
  let savingProfile = $state(false);

  // Bans (banthis hard-prohibition layer). The Go MemoryScopeDTO already parses
  // bans.md into structured title/rule blocks under `banList`; the shared TS
  // type still only carries the raw `bans` blob, so narrow to read the typed
  // array here without touching another agent's types.ts.
  type BanDTO = { title: string; rule: string };
  let addingBan = $state(false);
  let banTitle = $state("");
  let banRule = $state("");
  let savingBan = $state(false);
  let removingBan = $state<string | null>(null);

  const current = $derived<MemoryScopeDTO | null>(
    scope === "project" ? (data?.project ?? null) : (data?.global ?? null),
  );
  const bans = $derived<BanDTO[]>(
    ((current as (MemoryScopeDTO & { banList?: BanDTO[] }) | null)?.banList ?? []),
  );

  // alive guard: a late Bridge.Memory() resolution must not write after unmount
  // or after a newer load() started.
  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    try {
      const d = await Bridge.Memory();
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

  async function saveNote() {
    const note = draft.trim();
    if (!note) return;
    saving = true;
    try {
      await Bridge.AppendMemory(scope, note);
      draft = "";
      composing = false;
      toasts.success("note saved");
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      saving = false;
    }
  }

  async function addBan() {
    const title = banTitle.trim();
    const rule = banRule.trim();
    if (!title || !rule) return;
    savingBan = true;
    try {
      const replaced = await Bridge.AddBan(scope, title, rule);
      banTitle = "";
      banRule = "";
      addingBan = false;
      toasts.success(replaced ? "ban updated" : "ban added");
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      savingBan = false;
    }
  }
  async function removeBan(title: string) {
    removingBan = title;
    try {
      const removed = await Bridge.RemoveBan(scope, title);
      if (removed) toasts.success("ban removed");
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      removingBan = null;
    }
  }

  function startProfile() {
    profileDraft = current?.profile ?? "";
    editingProfile = true;
  }
  async function saveProfile() {
    savingProfile = true;
    try {
      await Bridge.WriteUserProfile(profileDraft);
      editingProfile = false;
      toasts.success("profile saved");
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      savingProfile = false;
    }
  }

  function shortDir(d: string): string {
    const p = d.replace(/\/$/, "").split("/");
    return p[p.length - 1] || d;
  }
</script>

<div class="mem">
  <header class="mem__head">
    <div class="mem__scopes" role="tablist" aria-label="Memory scope">
      <button
        class="mem__scope"
        class:mem__scope--on={scope === "project"}
        role="tab"
        aria-selected={scope === "project"}
        onclick={() => (scope = "project")}
      >
        Project
        {#if data?.project}<span class="mem__scope-n tnum">{data.project.noteCount}</span>{/if}
      </button>
      <button
        class="mem__scope"
        class:mem__scope--on={scope === "global"}
        role="tab"
        aria-selected={scope === "global"}
        onclick={() => (scope = "global")}
      >
        Global
        {#if data?.global}<span class="mem__scope-n tnum">{data.global.noteCount}</span>{/if}
      </button>
    </div>
    <div class="mem__head-actions">
      {#if current}
        <span class="mem__dir" title={current.dir}>{shortDir(current.dir)}</span>
      {/if}
      <Button variant="primary" size="sm" disabled={!current} onclick={() => (composing = !composing)}>
        {composing ? "Cancel" : "Add note"}
      </Button>
    </div>
  </header>

  {#if composing}
    <div class="mem__compose">
      <textarea
        bind:value={draft}
        class="mem__textarea selectable"
        rows="3"
        placeholder={`A durable note for ${scope} memory…`}
      ></textarea>
      <div class="mem__compose-actions">
        <Button variant="ghost" size="sm" onclick={() => ((composing = false), (draft = ""))}>Discard</Button>
        <Button variant="primary" size="sm" loading={saving} disabled={draft.trim().length === 0} onclick={saveNote}>
          Save note
        </Button>
      </div>
    </div>
  {/if}

  {#if loading && !data}
    <div class="mem__loading">
      {#each Array(3) as _, i (i)}<div class="mem__skel"></div>{/each}
    </div>
  {:else if !current || (current.noteCount === 0 && current.adHoc.length === 0 && !current.summary && bans.length === 0 && !current.profile)}
    <EmptyState glyph="❖" title="No {scope} memory yet" line="Notes you save — or the agent distills — live here and carry across sessions.">
      {#snippet action()}
        <Button variant="primary" onclick={() => (composing = true)}>Add the first note</Button>
      {/snippet}
    </EmptyState>
  {:else}
    <div class="mem__body">
      <div class="mem__main">
        {#if current.summary}
          <section class="mem__section">
            <div class="mem__section-head">
              <h2 class="mem__section-title">Summary</h2>
              {#if current.hasSummary}<Badge tone="brand">distilled</Badge>{/if}
            </div>
            <Card>
              <div class="mem__summary selectable"><Markdown source={current.summary} /></div>
            </Card>
          </section>
        {/if}

        {#if current.adHoc.length > 0}
          <section class="mem__section">
            <div class="mem__section-head">
              <h2 class="mem__section-title">Saved notes</h2>
              <span class="mem__count tnum">{current.adHoc.length}</span>
              <Badge tone="neutral">manual</Badge>
            </div>
            <div class="mem__adhoc">
              {#each current.adHoc as note (note.index)}
                <Card>
                  <div class="mem__note selectable"><Markdown source={note.text} /></div>
                </Card>
              {/each}
            </div>
          </section>
        {/if}

        <section class="mem__section mem__section--grow">
          <div class="mem__section-head">
            <h2 class="mem__section-title">Notes</h2>
            <span class="mem__count tnum">{current.noteCount}</span>
          </div>
          {#if current.noteCount === 0}
            <p class="mem__empty-note">No distilled notes in this scope yet.</p>
          {:else}
            <div class="mem__notes">
              <VirtualList items={current.notes} estimateHeight={96} key={(n) => n.index}>
                {#snippet row(note)}
                  <div class="mem__note-wrap">
                    <Card>
                      <div class="mem__note selectable"><Markdown source={note.text} /></div>
                    </Card>
                  </div>
                {/snippet}
              </VirtualList>
            </div>
          {/if}
        </section>
      </div>

      <aside class="mem__side">
        {#if scope === "global"}
          <section class="mem__side-section">
            <div class="mem__section-head">
              <h2 class="mem__section-title">User profile</h2>
              <Badge tone="brand">USER.md</Badge>
              {#if !editingProfile}
                <Button variant="link" size="sm" onclick={startProfile}>Edit</Button>
              {/if}
            </div>
            <p class="mem__helper">
              Your durable personalization prompt — eigen keeps it current as it learns; your own additions sit alongside.
            </p>
            {#if editingProfile}
              <textarea bind:value={profileDraft} class="mem__textarea selectable" rows="6" placeholder="Add your own notes — eigen keeps the rest current…"></textarea>
              <div class="mem__compose-actions">
                <Button variant="ghost" size="sm" onclick={() => (editingProfile = false)}>Cancel</Button>
                <Button variant="primary" size="sm" loading={savingProfile} onclick={saveProfile}>Save</Button>
              </div>
            {:else if current.profile}
              <Card><div class="mem__profile selectable"><Markdown source={current.profile} /></div></Card>
            {:else}
              <p class="mem__empty-note">Nothing learned yet. <button class="mem__inline-link" onclick={startProfile}>Add your own.</button></p>
            {/if}
          </section>
        {/if}

        <section class="mem__side-section">
          <div class="mem__section-head">
            <h2 class="mem__section-title">Banned behaviors</h2>
            {#if bans.length > 0}<Badge tone="error">enforced</Badge>{/if}
            {#if !addingBan}
              <Button variant="link" size="sm" onclick={() => (addingBan = true)}>Add</Button>
            {/if}
          </div>

          {#if addingBan}
            <div class="mem__ban-form">
              <input
                bind:value={banTitle}
                class="mem__input selectable"
                type="text"
                placeholder="Short title"
                aria-label="Ban title"
              />
              <textarea
                bind:value={banRule}
                class="mem__textarea selectable"
                rows="3"
                placeholder={`What the agent must not do (${scope} scope)…`}
                aria-label="Ban rule"
              ></textarea>
              <div class="mem__compose-actions">
                <Button variant="ghost" size="sm" onclick={() => ((addingBan = false), (banTitle = ""), (banRule = ""))}>
                  Cancel
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  loading={savingBan}
                  disabled={banTitle.trim().length === 0 || banRule.trim().length === 0}
                  onclick={addBan}
                >
                  Save ban
                </Button>
              </div>
            </div>
          {/if}

          {#if bans.length > 0}
            <ul class="mem__bans-list">
              {#each bans as ban (ban.title)}
                <li class="mem__ban">
                  <Card>
                    <div class="mem__ban-row">
                      <div class="mem__ban-text selectable">
                        <h3 class="mem__ban-title">{ban.title}</h3>
                        <p class="mem__ban-rule">{ban.rule}</p>
                      </div>
                      <button
                        class="mem__ban-remove"
                        type="button"
                        title="Remove ban"
                        aria-label={`Remove ban: ${ban.title}`}
                        disabled={removingBan === ban.title}
                        onclick={() => removeBan(ban.title)}
                      >
                        {removingBan === ban.title ? "…" : "✕"}
                      </button>
                    </div>
                  </Card>
                </li>
              {/each}
            </ul>
          {:else if !addingBan}
            <p class="mem__empty-note">
              No bans in this scope. <button class="mem__inline-link" onclick={() => (addingBan = true)}>Add one.</button>
            </p>
          {/if}
        </section>

        <section class="mem__side-section">
          <div class="mem__section-head"><h2 class="mem__section-title">Scope</h2></div>
          <dl class="mem__meta">
            <dt>notes</dt><dd class="tnum">{current.noteCount}</dd>
            <dt>ad-hoc</dt><dd class="tnum">{current.adHoc.length}</dd>
            <dt>backups</dt><dd class="tnum">{current.backups}</dd>
            <dt>size</dt><dd class="tnum">{(current.bytes / 1024).toFixed(1)} KB</dd>
          </dl>
        </section>
      </aside>
    </div>
  {/if}
</div>

<style>
  .mem {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .mem__head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-5);
    padding: var(--sp-6) var(--sp-9);
    border-bottom: 1px solid var(--border-hairline);
  }
  .mem__scopes {
    display: inline-flex;
    background: var(--bg-well);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    padding: var(--sp-1);
    gap: var(--sp-1);
  }
  .mem__scope {
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
  .mem__scope:hover {
    color: var(--text-primary);
  }
  .mem__scope:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .mem__scope--on {
    background: var(--bg-raised-2);
    color: var(--text-primary);
  }
  .mem__scope-n {
    font-size: var(--fs-micro);
    color: var(--text-faint);
  }
  .mem__scope--on .mem__scope-n {
    color: var(--brand);
  }
  .mem__head-actions {
    display: flex;
    align-items: center;
    gap: var(--sp-5);
  }
  .mem__dir {
    font-size: var(--fs-label);
    color: var(--text-faint);
    max-width: 240px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mem__compose {
    flex: none;
    padding: var(--sp-5) var(--sp-9);
    border-bottom: 1px solid var(--border-hairline);
    background: var(--bg-well);
  }
  .mem__textarea {
    width: 100%;
    resize: vertical;
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body-sm) / var(--lh-snug) var(--font-sans);
    padding: var(--sp-4);
    outline: none;
  }
  .mem__textarea:focus {
    border-color: var(--border-brand-faint);
  }
  .mem__compose-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-3);
    margin-top: var(--sp-3);
  }
  .mem__body {
    flex: 1;
    display: flex;
    min-height: 0;
    overflow: hidden;
  }
  .mem__main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    padding: var(--sp-7) var(--sp-9);
    gap: var(--sp-7);
    overflow-y: auto;
  }
  .mem__section {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .mem__section--grow {
    flex: 1;
    min-height: 0;
  }
  .mem__section-head {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .mem__section-title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .mem__count {
    font-size: var(--fs-label);
    color: var(--text-ghost);
  }
  .mem__summary,
  .mem__note,
  .mem__profile {
    padding: var(--sp-5);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-prose);
  }
  .mem__notes {
    flex: 1;
    min-height: 0;
    /* VirtualList needs a bounded height to window against. */
    height: 100%;
  }
  .mem__note-wrap {
    padding-bottom: var(--sp-4);
  }
  /* Manual saves are few and unbounded in height — a plain stack reads better
     than windowing, and keeps a freshly-saved note immediately visible. */
  .mem__adhoc {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .mem__empty-note {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    margin: 0;
  }
  .mem__helper {
    margin: 0;
    color: var(--text-faint);
    font-size: var(--fs-label);
    line-height: var(--lh-snug);
  }
  .mem__inline-link {
    border: none;
    background: none;
    color: var(--accent);
    cursor: pointer;
    font: inherit;
    padding: 0;
  }
  .mem__inline-link:hover {
    color: var(--accent-bright);
    text-decoration: underline;
  }
  .mem__input {
    width: 100%;
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body-sm) / var(--lh-snug) var(--font-sans);
    padding: var(--sp-3) var(--sp-4);
    outline: none;
  }
  .mem__input:focus {
    border-color: var(--border-brand-faint);
  }
  .mem__ban-form {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .mem__bans-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .mem__ban-row {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-4);
    padding: var(--sp-5);
  }
  .mem__ban-text {
    flex: 1;
    min-width: 0;
  }
  .mem__ban-title {
    margin: 0 0 var(--sp-2);
    font: var(--fw-semibold) var(--fs-body-sm) / var(--lh-snug) var(--font-sans);
    color: var(--text-primary);
  }
  .mem__ban-rule {
    margin: 0;
    font-size: var(--fs-body-sm);
    line-height: var(--lh-prose);
    color: var(--text-secondary);
    overflow-wrap: anywhere;
  }
  .mem__ban-remove {
    flex: none;
    width: 24px;
    height: 24px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    border-radius: var(--r-sm);
    background: transparent;
    color: var(--text-ghost);
    cursor: pointer;
    font-size: var(--fs-body-sm);
    line-height: 1;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .mem__ban-remove:hover {
    background: var(--error-bg);
    color: var(--error);
  }
  .mem__ban-remove:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .mem__ban-remove:disabled {
    cursor: default;
    color: var(--text-faint);
    background: transparent;
  }
  .mem__side {
    width: 300px;
    flex: none;
    border-left: 1px solid var(--border-hairline);
    background: var(--bg-well);
    padding: var(--sp-7) var(--sp-6);
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--sp-7);
  }
  .mem__side-section {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .mem__meta {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: var(--sp-3) var(--sp-5);
    margin: 0;
  }
  .mem__meta dt {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  .mem__meta dd {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--fs-body-sm);
    text-align: right;
  }
  .mem__loading {
    padding: var(--sp-7) var(--sp-9);
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .mem__skel {
    height: 88px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: mem-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes mem-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .mem__skel {
      animation: none;
    }
  }
</style>
