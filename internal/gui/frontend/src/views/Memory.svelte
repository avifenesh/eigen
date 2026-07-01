<script lang="ts">
  // Memory — the durable-notes browser. Any known scope (Global plus every
  // project from session history + on-disk stores) is selectable via a picker.
  // Each scope shows: a distilled summary (the injected view), the append-only
  // notes as virtualized cards, ad-hoc manual saves, bans, and (global only) the
  // editable user profile. Reads memory directly via the bridge (memory is local
  // filesystem; no daemon round-trip).
  import { Bridge } from "$lib/bridge";
  import { errText } from "$lib/errors";
  import { router } from "$lib/router.svelte";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { viewCache } from "$lib/stores/viewCache.svelte";
  import type { MemoryScopeDTO, MemoryScopeRefDTO } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Dropdown from "$lib/components/Dropdown.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Markdown from "$lib/components/Markdown.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import Sheet from "$lib/components/Sheet.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";

  // The selectable scopes (Global first, then every known project). The picker
  // binds to `scope` — a scope KEY that round-trips through MemoryForScope (the
  // backend accepts "global", "project"/"", an abs dir, or an on-disk key). We
  // open the cwd project by default ("project") for session continuity.
  let scopes = $state<MemoryScopeRefDTO[]>(viewCache.get<MemoryScopeRefDTO[]>("memory:scopes") ?? []);
  let scope = $state<string>("project");
  // The opened scope's rich DTO — loaded on demand via MemoryForScope, replacing
  // the old two-field {project, global} payload. Prefilled from cache under the
  // default scope key ("project" — every mount starts there, see the mount
  // $effect below) so a revisit paints instantly instead of blanking first.
  let current = $state<MemoryScopeDTO | null>(viewCache.get<MemoryScopeDTO>("memory:scope:project") ?? null);
  function scopeCacheKey(key: string): string {
    return `memory:scope:${key}`;
  }
  let loadError = $state<string | null>(null);
  let loading = $state(true);
  let composing = $state(false);
  let draft = $state("");
  let saving = $state(false);
  // The compose textarea element — focused the moment composing flips true so
  // the cursor lands in the field without a second click.
  let composeEl = $state<HTMLTextAreaElement | null>(null);
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
  let removingNote = $state<number | null>(null);
  let removingAdHoc = $state<number | null>(null);
  let confirmRemoveNote = $state<number | null>(null);
  let confirmRemoveAdHoc = $state<number | null>(null);

  // Backups (snapshot history of MEMORY.md). The scope DTO carries only the
  // count; Bridge.MemoryBackups(scope) lists the actual snapshot paths. Lazy:
  // fetched on first reveal, re-fetched when the scope switches.
  let backupsOpen = $state(false);
  let backupPaths = $state<string[]>([]);
  let backupsLoading = $state(false);

  const bans = $derived<BanDTO[]>(
    ((current as (MemoryScopeDTO & { banList?: BanDTO[] }) | null)?.banList ?? []),
  );
  // The profile editor (USER.md) only applies to the global scope — profile /
  // profileLearned are only populated there. Trust the DTO's own scope marker so
  // a project whose dir happens to be the home root can't masquerade as global.
  const isGlobal = $derived(current?.scope === "global");
  // The selected scope's ref, for the dir label / count next to the picker.
  const selectedRef = $derived<MemoryScopeRefDTO | null>(
    scopes.find((s) => s.key === scope) ?? null,
  );
  // A friendly name for prose (placeholders, empty-state titles). Prefer the
  // ref's name; fall back to the loaded DTO's scope or the bare key.
  const scopeLabel = $derived(selectedRef?.name || current?.scope || scope);
  // A scope with nothing injected — no summary, notes, ad-hoc saves, bans or
  // profile. Backups are *not* counted here: a scope can consolidate down to
  // nothing-injected yet still carry snapshot history, which the empty state
  // must acknowledge (see hasBackupHistory) rather than claim "no memory yet".
  const isEmpty = $derived(
    !current ||
      (current.noteCount === 0 &&
        current.adHoc.length === 0 &&
        !current.summary &&
        bans.length === 0 &&
        !current.profile &&
        !current.profileLearned),
  );
  const hasBackupHistory = $derived((current?.backups ?? 0) > 0);

  // The picker list — fetched once on mount. Reassigned wholesale for
  // reactivity. A late resolution after unmount is harmless (no UI clobber), but
  // guard with `alive` so we don't toast after teardown.
  let alive = true;
  async function loadScopes() {
    try {
      const refs = await viewCache.fetch("memory:scopes", () => Bridge.ListMemoryScopes());
      if (!alive) return;
      scopes = refs;
      // Snap the picker label to the canonical key of the cwd project (the ref
      // flagged `current`) so it shows "eigen (10)" rather than the placeholder.
      // We started the data load under the "project" alias; this only relabels.
      if (scope === "project") {
        const cur = refs.find((r) => r.current);
        if (cur) scope = cur.key;
      }
    } catch (e) {
      if (alive) toasts.error(errText(e));
    }
  }

  // alive guard: a slow MemoryForScope() resolution for an old selection must
  // not clobber the data of a newer one (or write after unmount). Monotonic seq.
  let loadSeq = 0;
  async function loadScope(key: string) {
    const seq = ++loadSeq;
    loading = true;
    loadError = null;
    try {
      const d = await viewCache.fetch(scopeCacheKey(key), () => Bridge.MemoryForScope(key));
      if (seq === loadSeq) current = d;
    } catch (e) {
      const msg = errText(e);
      if (seq === loadSeq) {
        loadError = msg;
        toasts.error(msg);
      }
    } finally {
      if (seq === loadSeq) loading = false;
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
  // Switching the picker re-opens the chosen scope. Swap `current` to the new
  // scope's cached snapshot (or null) immediately — otherwise the old scope's
  // data stays on screen until the new fetch resolves, since the `{#if loading
  // && !current}` skeleton gate only triggers when current is empty.
  function selectScope(key: string) {
    if (key === scope) return;
    scope = key;
    composing = false;
    confirmRemoveNote = null;
    confirmRemoveAdHoc = null;
    current = viewCache.get<MemoryScopeDTO>(scopeCacheKey(key)) ?? null;
    loadScope(key);
  }

  // When the composer opens, move focus into the textarea so typing starts
  // immediately — no extra click. composeEl is bound only while composing.
  $effect(() => {
    if (composing && composeEl) composeEl.focus();
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
      viewCache.invalidate(scopeCacheKey(scope));
      await loadScope(scope);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      saving = false;
    }
  }

  // Relocate a note to ANOTHER scope — global, the cwd project, or any other
  // project. A note can be cross-cutting (→ global), misfiled (global → a
  // project), or belong to a sibling project (project → project), so the
  // destination is PICKED from every other scope rather than a fixed toggle.
  // The source copy is superseded and drops on the next consolidation.
  let movingNote = $state<number | null>(null);
  let moveOpen = $state(false);
  let movePending = $state<{ text: string; idx: number } | null>(null);
  // Every scope except the one currently open — the move destinations.
  const moveTargets = $derived(scopes.filter((s) => s.key !== scope));

  function openMove(noteText: string, idx: number) {
    movePending = { text: noteText, idx };
    moveOpen = true;
  }
  async function moveTo(dstKey: string, dstName: string) {
    if (!movePending) return;
    const { text, idx } = movePending;
    moveOpen = false;
    movingNote = idx;
    try {
      await Bridge.MoveMemoryNote(scope, dstKey, text);
      toasts.success(`moved to ${dstName} memory`);
      // The note lands in dstKey's scope too — bust its cache so a later visit
      // there doesn't show a pre-move snapshot missing the moved note.
      viewCache.invalidate(scopeCacheKey(scope));
      viewCache.invalidate(scopeCacheKey(dstKey));
      await loadScope(scope);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      movingNote = null;
      movePending = null;
    }
  }
  // The per-note button label: a generic "Move" since the destination is chosen
  // in the picker (was a fixed →global/→project toggle).
  const moveLabel = "Move";

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
      viewCache.invalidate(scopeCacheKey(scope));
      await loadScope(scope);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      savingBan = false;
    }
  }
  async function removeBan(title: string) {
    removingBan = title;
    try {
      const removed = await Bridge.RemoveBan(scope, title);
      if (removed) toasts.success("ban removed");
      viewCache.invalidate(scopeCacheKey(scope));
      await loadScope(scope);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      removingBan = null;
    }
  }

  async function removeNote(index: number) {
    removingNote = index;
    confirmRemoveNote = null;
    try {
      await Bridge.RemoveMemoryNote(scope, index);
      toasts.success("note removed");
      viewCache.invalidate(scopeCacheKey(scope));
      await loadScope(scope);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      removingNote = null;
    }
  }

  async function removeAdHoc(index: number) {
    removingAdHoc = index;
    confirmRemoveAdHoc = null;
    try {
      await Bridge.RemoveAdHocMemoryNote(scope, index);
      toasts.success("saved note deleted");
      viewCache.invalidate(scopeCacheKey(scope));
      await loadScope(scope);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      removingAdHoc = null;
    }
  }

  // alive guard: a late MemoryBackups() resolution must not write after a scope
  // switch or unmount started a newer fetch.
  let backupsSeq = 0;
  async function loadBackups() {
    const seq = ++backupsSeq;
    backupsLoading = true;
    try {
      const paths = await Bridge.MemoryBackups(scope);
      // Go returns oldest-first; reverse for newest-first.
      if (seq === backupsSeq) backupPaths = [...paths].reverse();
    } catch (e) {
      if (seq === backupsSeq) toasts.error(errText(e));
    } finally {
      if (seq === backupsSeq) backupsLoading = false;
    }
  }
  function toggleBackups() {
    backupsOpen = !backupsOpen;
    if (backupsOpen) loadBackups();
  }
  // Switching scope invalidates the open list — collapse and drop stale paths.
  $effect(() => {
    scope;
    backupsOpen = false;
    backupPaths = [];
    backupsSeq++;
  });

  function backupName(path: string): string {
    const p = path.split("/");
    return p[p.length - 1] || path;
  }
  // Backup files are named MEMORY.md.20060102-150405.bak — surface the snapshot
  // moment in a readable form, falling back to the bare filename.
  function backupWhen(path: string): string {
    const m = backupName(path).match(/\.(\d{8})-(\d{6})\.bak$/);
    if (!m) return backupName(path);
    const [, d, t] = m;
    const date = `${d.slice(0, 4)}-${d.slice(4, 6)}-${d.slice(6, 8)}`;
    const time = `${t.slice(0, 2)}:${t.slice(2, 4)}:${t.slice(4, 6)}`;
    return `${date} ${time}`;
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
      viewCache.invalidate(scopeCacheKey(scope));
      await loadScope(scope);
    } catch (e) {
      toasts.error(errText(e));
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
    <div class="mem__picker">
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
        <span class="mem__dir" title={selectedRef.dir}>{shortDir(selectedRef.dir)}</span>
      {:else if current?.dir}
        <span class="mem__dir" title={current.dir}>{shortDir(current.dir)}</span>
      {/if}
    </div>
    <div class="mem__head-actions">
      <Button variant="primary" size="sm" disabled={!current} onclick={() => (composing = !composing)}>
        {composing ? "Cancel" : "Add note"}
      </Button>
    </div>
  </header>

  {#if composing}
    <div class="mem__compose">
      <textarea
        bind:this={composeEl}
        bind:value={draft}
        class="mem__textarea selectable"
        rows="3"
        placeholder={`A durable note for ${scopeLabel} memory…`}
        onkeydown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.isComposing) {
            e.preventDefault();
            saveNote();
          }
        }}
      ></textarea>
      <div class="mem__compose-actions">
        <Button variant="ghost" size="sm" onclick={() => ((composing = false), (draft = ""))}>Discard</Button>
        <Button variant="primary" size="sm" loading={saving} disabled={draft.trim().length === 0} onclick={saveNote}>
          Save note
        </Button>
      </div>
    </div>
  {/if}

  {#if loading && !current}
    <div class="mem__loading">
      <Skeleton count={3} height="88px" gap="var(--sp-5)" />
    </div>
  {:else if loadError && !current}
    <EmptyState glyph="☾" title="Couldn't load {scopeLabel} memory" line={loadError}>
      {#snippet action()}
        <Button variant="secondary" onclick={() => loadScope(scope)}>Retry</Button>
      {/snippet}
    </EmptyState>
  {:else if isEmpty && hasBackupHistory}
    <EmptyState
      glyph="☾"
      title="Nothing injected in {scopeLabel} memory"
      line="This scope consolidated down to nothing currently injected — but its snapshot history is preserved. Review what changed in Dreaming, or start fresh below."
    >
      {#snippet action()}
        <Button variant="primary" onclick={() => router.go("dreaming")}>View in Dreaming</Button>
      {/snippet}
    </EmptyState>
  {:else if isEmpty}
    <EmptyState glyph="❖" title="No {scopeLabel} memory yet" line="Notes you save — or the agent distills — live here and carry across sessions.">
      {#snippet action()}
        <Button variant="primary" onclick={() => (composing = true)}>Add the first note</Button>
      {/snippet}
    </EmptyState>
  {:else if current}
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
                  <div class="mem__note-row">
                    <div class="mem__note selectable"><Markdown source={note.text} /></div>
                    <Button
                      variant="ghost"
                      size="sm"
                      loading={movingNote === note.index}
                      title="Relocate this note to another scope"
                      onclick={() => openMove(note.text, note.index)}>{moveLabel}</Button>
                    {#if confirmRemoveAdHoc === note.index}
                      <Button variant="danger" size="sm" loading={removingAdHoc === note.index} onclick={() => removeAdHoc(note.index)}>Confirm</Button>
                      <Button variant="ghost" size="sm" disabled={removingAdHoc === note.index} onclick={() => (confirmRemoveAdHoc = null)}>Cancel</Button>
                    {:else}
                      <Button
                        variant="ghost"
                        size="sm"
                        title="Delete this saved note"
                        onclick={() => (confirmRemoveAdHoc = note.index)}>Remove</Button>
                    {/if}
                  </div>
                </Card>
              {/each}
            </div>
          </section>
        {/if}

        <section class="mem__section">
          <div class="mem__section-head">
            <h2 class="mem__section-title">Notes</h2>
            <span class="mem__count tnum">{current.noteCount}</span>
          </div>
          {#if current.noteCount === 0}
            <p class="mem__empty-note">No distilled notes in this scope yet.</p>
          {:else}
            <!-- A plain stack in the single page scroll (.mem__main). Curated
                 notes are section-cards (dozens), not the 10k-row case VirtualList
                 exists for — and nesting VirtualList's own overflow inside the
                 scrolling column collapsed the notes viewport and made the wheel
                 fight between two scrollers. -->
            <div class="mem__notes">
              {#each current.notes as note (note.index)}
                <Card>
                  <div class="mem__note-row">
                    <div class="mem__note selectable"><Markdown source={note.text} /></div>
                    <Button
                      variant="ghost"
                      size="sm"
                      loading={movingNote === note.index}
                      title="Relocate this note to another scope"
                      onclick={() => openMove(note.text, note.index)}>{moveLabel}</Button>
                    {#if confirmRemoveNote === note.index}
                      <Button variant="danger" size="sm" loading={removingNote === note.index} onclick={() => removeNote(note.index)}>Confirm</Button>
                      <Button variant="ghost" size="sm" disabled={removingNote === note.index} onclick={() => (confirmRemoveNote = null)}>Cancel</Button>
                    {:else}
                      <Button
                        variant="ghost"
                        size="sm"
                        title="Remove this note"
                        onclick={() => (confirmRemoveNote = note.index)}>Remove</Button>
                    {/if}
                  </div>
                </Card>
              {/each}
            </div>
          {/if}
        </section>
      </div>

      <aside class="mem__side">
        {#if isGlobal}
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
            {#if current.profileLearned}
              <div class="mem__learned">
                <div class="mem__learned-head">
                  <span class="mem__learned-tag">✧ learned by eigen</span>
                  <span class="mem__learned-sub">auto-maintained from your sessions</span>
                </div>
                <div class="mem__profile selectable"><Markdown source={current.profileLearned} /></div>
              </div>
            {/if}
            {#if editingProfile}
              <textarea
                bind:value={profileDraft}
                class="mem__textarea selectable"
                rows="6"
                placeholder="Add your own notes — eigen keeps the rest current…"
                onkeydown={(e) => {
                  if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.isComposing) {
                    e.preventDefault();
                    saveProfile();
                  }
                }}
              ></textarea>
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
                placeholder={`What the agent must not do (${scopeLabel} scope)…`}
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
            <dt>backups</dt>
            <dd>
              {#if current.backups > 0}
                <button
                  class="mem__backups-toggle tnum"
                  class:mem__backups-toggle--on={current.backups > 0}
                  type="button"
                  aria-expanded={backupsOpen}
                  onclick={toggleBackups}
                >
                  {current.backups}
                  <span class="mem__backups-caret" class:mem__backups-caret--open={backupsOpen} aria-hidden="true">▸</span>
                </button>
              {:else}
                <span class="tnum">0</span>
              {/if}
            </dd>
            <dt>size</dt><dd class="tnum">{(current.bytes / 1024).toFixed(1)} KB</dd>
          </dl>

          {#if backupsOpen}
            <div class="mem__backups">
              {#if backupsLoading && backupPaths.length === 0}
                <p class="mem__empty-note">Loading backups…</p>
              {:else if backupPaths.length === 0}
                <p class="mem__empty-note">No backup snapshots on disk.</p>
              {:else}
                <ul class="mem__backups-list">
                  {#each backupPaths as path (path)}
                    <li class="mem__backup">
                      <span class="mem__backup-when tnum">{backupWhen(path)}</span>
                      <span class="mem__backup-path selectable" title={path}>{backupName(path)}</span>
                    </li>
                  {/each}
                </ul>
              {/if}
            </div>
          {/if}
        </section>
      </aside>
    </div>
  {/if}

  <!-- Move-destination picker: choose any OTHER scope (global, the cwd project,
       or a sibling project) to relocate the selected note into. -->
  <Sheet open={moveOpen} label="Move note to scope" width={420} onclose={() => (moveOpen = false)}>
    {#snippet title()}Move note to…{/snippet}
    {#if movePending}
      <p class="mem__move-note selectable">{movePending.text}</p>
    {/if}
    {#if moveTargets.length === 0}
      <p class="mem__empty-note">No other scope to move into.</p>
    {:else}
      <div class="mem__move-targets">
        {#each moveTargets as t (t.key)}
          <button class="mem__move-target" onclick={() => moveTo(t.key, t.name)}>
            <span class="mem__move-target-name">{t.name}</span>
            {#if t.dir}<span class="mem__move-target-dir">{shortDir(t.dir)}</span>{/if}
          </button>
        {/each}
      </div>
    {/if}
  </Sheet>
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
    padding: var(--sp-6) var(--sp-7);
    border-bottom: 1px solid var(--border-hairline);
  }
  /* Scope picker — a styled <select> over N scopes (Global + every project).
     The dir label sits beside it so projects sharing a basename stay distinct. */
  .mem__picker {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-5);
    min-width: 0;
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
    padding: var(--sp-7) var(--sp-7);
    gap: var(--sp-7);
    overflow-y: auto;
  }
  .mem__section {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
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
  /* The eigen-maintained learned block — a quiet teal-edged card above the
     user's own editor, marking what eigen distilled (read-only here). */
  .mem__learned {
    border: 1px solid var(--border-hairline);
    border-left: 2px solid var(--brand);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    margin-bottom: var(--sp-4);
  }
  .mem__learned-head {
    display: flex;
    align-items: baseline;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-5) 0;
  }
  .mem__learned-tag {
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    color: var(--brand);
  }
  .mem__learned-sub {
    font-size: var(--fs-micro);
    color: var(--text-faint);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }
  /* Plain stack inside the page scroll (.mem__main) — no inner overflow, so the
     wheel never fights a nested scroller. */
  .mem__notes {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  /* Note card body + its move-to-other-scope action on one row; the button is
     quiet until hover so the note text stays the focus. */
  .mem__note-row {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-2);
  }
  .mem__note-row .mem__note {
    flex: 1;
    min-width: 0;
  }
  .mem__note-row :global(button) {
    flex: none;
    margin: var(--sp-3) var(--sp-3) 0 0;
    opacity: 0.45;
    transition: opacity 0.12s ease;
  }
  .mem__note-row:hover :global(button) {
    opacity: 1;
  }
  /* Move-to-scope picker (inside the Sheet). */
  .mem__move-note {
    margin: 0 0 var(--sp-5);
    padding: var(--sp-4);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    color: var(--text-secondary);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
    max-height: 8em;
    overflow-y: auto;
  }
  .mem__move-targets {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .mem__move-target {
    display: flex;
    flex-direction: column;
    gap: 2px;
    text-align: left;
    padding: var(--sp-4) var(--sp-5);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    color: var(--text-primary);
    cursor: pointer;
    transition: background 0.1s ease, border-color 0.1s ease;
  }
  .mem__move-target:hover {
    background: var(--bg-raised-2);
    border-color: var(--border-brand-faint);
  }
  .mem__move-target-name {
    font: var(--fw-medium) var(--fs-body-sm) / 1.2 var(--font-sans);
  }
  .mem__move-target-dir {
    font-size: var(--fs-micro);
    color: var(--text-faint);
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
  .mem__backups-toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fs-body-sm);
    padding: 0;
    cursor: pointer;
    border-radius: var(--r-sm);
  }
  /* teal = alive: a scope with backups on disk reads as live history. */
  .mem__backups-toggle--on {
    color: var(--brand);
  }
  .mem__backups-toggle:hover {
    color: var(--accent-bright);
  }
  .mem__backups-toggle:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .mem__backups-caret {
    display: inline-block;
    font-size: var(--fs-micro);
    color: var(--text-faint);
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .mem__backups-caret--open {
    transform: rotate(90deg);
  }
  @media (prefers-reduced-motion: reduce) {
    .mem__backups-caret {
      transition: none;
    }
  }
  .mem__backups-list {
    list-style: none;
    margin: var(--sp-3) 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .mem__backup {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    padding: var(--sp-3) var(--sp-4);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-sm);
    background: var(--bg-raised);
  }
  .mem__backup-when {
    font-size: var(--fs-body-sm);
    color: var(--text-secondary);
  }
  .mem__backup-path {
    font-size: var(--fs-micro);
    color: var(--text-faint);
    overflow-wrap: anywhere;
  }
  .mem__loading {
    padding: var(--sp-7) var(--sp-7);
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
</style>
