<script lang="ts">
  // Sessions — the full session manager (the archive, not the live cockpit).
  // Type-to-search across title + dir, newest-first, capped with "show more"
  // because a long-lived install can carry 50+. Per-row: Resume (open chat),
  // Export (writes a transcript file → toast the path), Delete (inline confirm).
  // Header: "Prune empty" (drops zero-turn sessions) + a total count. Reuses the
  // shared `sessions` store; refreshed on mount and after any mutation.
  import { sessions } from "$lib/stores/sessions.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { router } from "$lib/router.svelte";
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { now } from "$lib/stores/clock.svelte";
  import { sessionDot } from "$lib/status";
  import { errText } from "$lib/errors";
  import type { SessionInfoDTO } from "$lib/types";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let query = $state("");
  let pruning = $state(false);
  let exporting = $state<Record<string, boolean>>({});
  let deleting = $state<Record<string, boolean>>({});
  let confirmDelete = $state<Record<string, boolean>>({});

  // Mutating actions (Export/Delete/Prune) all round-trip the daemon; gate them
  // on the connection the same way Chat gates send, so an offline daemon shows a
  // disabled control with a reason instead of an enabled button that throws.
  const online = $derived(daemon.status === "online");

  // The list can run long; reveal in batches rather than mounting every row.
  const PAGE = 40;
  let shown = $state(PAGE);

  $effect(() => {
    sessions.refresh();
  });

  // Newest-first, then filter by title/dir. Sort a copy so the store isn't
  // mutated under reactivity.
  const filtered = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const all = [...sessions.list].sort((a, b) => b.updated - a.updated);
    if (!q) return all;
    return all.filter(
      (s) => (s.title || "").toLowerCase().includes(q) || (s.dir || "").toLowerCase().includes(q),
    );
  });
  const visible = $derived(filtered.slice(0, shown));

  // Reset the visible window whenever the filter changes so it starts at the top.
  $effect(() => {
    query;
    shown = PAGE;
  });

  function rel(updatedNano: number): string {
    void now.ms;
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

  function resume(s: SessionInfoDTO) {
    router.go("chat", s.id);
  }

  async function exportSession(s: SessionInfoDTO) {
    exporting[s.id] = true;
    try {
      const path = await Bridge.ExportSession(s.id);
      toasts.success(`exported → ${path}`);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete exporting[s.id];
    }
  }

  async function del(s: SessionInfoDTO) {
    deleting[s.id] = true;
    try {
      await Bridge.RemoveSession(s.id);
      toasts.success("session deleted");
      delete confirmDelete[s.id];
      await sessions.refresh();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete deleting[s.id];
    }
  }

  async function prune() {
    pruning = true;
    try {
      const removed = await Bridge.PruneSessions();
      const n = removed.length;
      if (n === 0) toasts.info("nothing to prune");
      else toasts.success(`pruned ${n} empty session${n === 1 ? "" : "s"}`);
      await sessions.refresh();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      pruning = false;
    }
  }
</script>

<div class="sx">
  <header class="sx__head">
    <div class="sx__search">
      <input
        class="sx__input"
        type="text"
        placeholder="Search sessions…"
        bind:value={query}
        aria-label="Search sessions by title or directory"
      />
      <span class="sx__count tnum">{filtered.length}</span>
    </div>
    <Button
      variant="ghost"
      size="sm"
      loading={pruning}
      disabled={!online || sessions.count === 0}
      title={!online ? "daemon offline" : sessions.count === 0 ? "No sessions to prune" : "Remove sessions with no turns"}
      onclick={prune}
    >
      Prune empty
    </Button>
  </header>

  {#if sessions.loading && sessions.count === 0}
    <div class="sx__list sx__list--pad">
      {#each Array(6) as _, i (i)}<div class="sx__skel"></div>{/each}
    </div>
  {:else if sessions.error && sessions.count === 0}
    <EmptyState glyph="≡" title="Couldn't load sessions" line={sessions.error}>
      {#snippet action()}
        <Button variant="secondary" onclick={() => sessions.refresh()}>Retry</Button>
      {/snippet}
    </EmptyState>
  {:else if sessions.count === 0}
    <EmptyState glyph="≡" title="No sessions yet" line="Sessions you start are saved here and can be resumed any time." />
  {:else if filtered.length === 0}
    <div class="sx__list sx__list--pad">
      <p class="sx__empty-note">No sessions match “{query}”.</p>
    </div>
  {:else}
    <div class="sx__list">
      {#each visible as s (s.id)}
        <div class="srow" class:srow--working={s.status === "working"} class:srow--approval={s.status === "approval"}>
          <StatusDot state={sessionDot(s.status)} size={7} pulse={s.status === "working" || s.status === "approval"} />
          <button class="srow__main" onclick={() => resume(s)} title="Resume session">
            <span class="srow__title">{s.title || "untitled session"}</span>
            <span class="srow__dir" title={s.dir}>{base(s.dir)}</span>
          </button>
          {#if s.model}<Badge tone="neutral" truncate>{s.model}</Badge>{/if}
          <span class="srow__turns tnum">{s.turns} turn{s.turns === 1 ? "" : "s"}</span>
          <span class="srow__when">{rel(s.updated)}</span>
          <div class="srow__actions">
            <Button variant="ghost" size="sm" onclick={() => resume(s)}>Resume</Button>
            <Button
              variant="ghost"
              size="sm"
              loading={exporting[s.id]}
              disabled={!online}
              title={online ? "Write this session's transcript to a file" : "daemon offline"}
              onclick={() => exportSession(s)}
            >Export</Button>
            {#if confirmDelete[s.id]}
              <Button
                variant="danger"
                size="sm"
                loading={deleting[s.id]}
                disabled={!online}
                title={online ? "Permanently delete this session" : "daemon offline"}
                onclick={() => del(s)}
              >Confirm</Button>
              <Button variant="ghost" size="sm" disabled={deleting[s.id]} onclick={() => delete confirmDelete[s.id]}>Cancel</Button>
            {:else}
              <Button
                variant="ghost"
                size="sm"
                disabled={!online}
                title={online ? "Delete this session" : "daemon offline"}
                onclick={() => (confirmDelete[s.id] = true)}
              >Delete</Button>
            {/if}
          </div>
        </div>
      {/each}
      {#if shown < filtered.length}
        <div class="sx__more">
          <Button variant="ghost" size="sm" onclick={() => (shown += PAGE)}>
            Show {Math.min(PAGE, filtered.length - shown)} more · {filtered.length - shown} remaining
          </Button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .sx {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .sx__head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-5);
    padding: var(--sp-6) var(--sp-9);
    border-bottom: 1px solid var(--border-hairline);
  }
  .sx__search {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    flex: 1;
    max-width: 420px;
  }
  .sx__input {
    flex: 1;
    height: 32px;
    padding: 0 var(--sp-5);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body-sm) / 1 var(--font-sans);
    outline: none;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .sx__input:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .sx__input::placeholder {
    color: var(--text-ghost);
  }
  .sx__count {
    font-size: var(--fs-label);
    color: var(--text-faint);
  }
  .sx__list {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-6) var(--sp-9);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .sx__list--pad {
    gap: var(--sp-4);
  }
  .sx__skel {
    height: 48px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: sx-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes sx-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .sx__empty-note {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    margin: 0;
  }
  .srow {
    display: flex;
    align-items: center;
    gap: var(--sp-5);
    padding: var(--sp-4) var(--sp-5);
    border-radius: var(--r-md);
    border: 1px solid transparent;
    border-left: 2px solid transparent;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .srow:hover {
    background: var(--state-hover);
  }
  /* The archive echoes the cockpit's language quietly: a live row wears the
     teal (working) / warn (approval) seam, but does NOT breathe — this is a
     list to scan, not a surface to watch. The dot already pulses. */
  .srow--working {
    border-left-color: var(--brand);
  }
  .srow--approval {
    border-left-color: var(--warn);
  }
  .srow__main {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: var(--sp-5);
    border: none;
    background: transparent;
    cursor: pointer;
    text-align: left;
    padding: 0;
    border-radius: var(--r-xs);
  }
  .srow__main:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .srow__title {
    font-weight: var(--fw-medium);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 360px;
  }
  .srow__dir {
    font-size: var(--fs-label);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .srow__turns {
    flex: none;
    font-size: var(--fs-label);
    color: var(--text-ghost);
    min-width: 56px;
    text-align: right;
  }
  .srow__when {
    flex: none;
    font-size: var(--fs-label);
    color: var(--text-faint);
    min-width: 64px;
    text-align: right;
  }
  .srow__actions {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .sx__more {
    display: flex;
    justify-content: center;
    margin-top: var(--sp-4);
  }
  @media (prefers-reduced-motion: reduce) {
    .sx__skel {
      animation: none;
    }
    .srow {
      transition: none;
    }
  }
</style>
