<script lang="ts">
  // Board — the cross-project work board. eigen is a working station: this is
  // "what's going on across ALL my projects" in one place. One lane per project
  // with git state (branch · uncommitted / unpushed / behind · TODOs) and
  // actionable cards (open PRs/issues + git loose-ends from the proactive feed),
  // each one-click startable like Home. Reads the cached feed (instant) + light
  // per-lane git probes; no rescan triggered here.
  import { Bridge } from "$lib/bridge";
  import { errText } from "$lib/errors";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";
  import { router } from "$lib/router.svelte";
  import { Browser } from "@wailsio/runtime";
  import type { BoardDTO, BoardItemDTO, BoardLaneDTO } from "$lib/types";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let data = $state<BoardDTO | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let acting = $state<Record<string, boolean>>({});

  let alive = true;
  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    error = null;
    try {
      const d = await Bridge.Board();
      if (alive && seq === loadSeq) data = d;
    } catch (e) {
      if (alive && seq === loadSeq) error = errText(e);
    } finally {
      if (alive && seq === loadSeq) loading = false;
    }
  }
  $effect(() => {
    load();
    return () => {
      alive = false;
      loadSeq++;
    };
  });

  async function startItem(it: BoardItemDTO) {
    if (!it.task) {
      openURL(it.url);
      return;
    }
    acting[it.key] = true;
    try {
      const id = await Bridge.StartFromFeed(it.dir ?? "", it.task);
      await sessions.refresh();
      router.go("chat", id);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete acting[it.key];
    }
  }
  function openLaneChat(lane: BoardLaneDTO) {
    // Start a plain session rooted at the project (no task) for ad-hoc work.
    Bridge.NewSession(lane.dir, "", "")
      .then(async (id) => {
        await sessions.refresh();
        router.go("chat", id);
      })
      .catch((e) => toasts.error(errText(e)));
  }
  function openURL(url?: string) {
    if (!url) return;
    try {
      Browser.OpenURL(url);
    } catch {
      window.open(url, "_blank");
    }
  }
  function kindGlyph(kind: string): string {
    return kind === "github" ? "◉" : "±";
  }
</script>

<div class="board">
  <header class="board__head">
    <div>
      <h2 class="board__title">Work board</h2>
      <p class="board__sub">Every project at a glance — git state, open PRs/issues, loose ends. One place to pick up work.</p>
    </div>
    <Button variant="secondary" size="sm" onclick={() => load()}>Refresh</Button>
  </header>

  {#if loading && !data}
    <div class="board__lanes">
      {#each Array(3) as _, i (i)}<div class="lane lane--skel"></div>{/each}
    </div>
  {:else if error && !data}
    <EmptyState glyph="▤" title="Couldn't load the board" line={error}>
      {#snippet action()}<Button variant="secondary" onclick={() => load()}>Retry</Button>{/snippet}
    </EmptyState>
  {:else if !data || data.lanes.length === 0}
    <EmptyState glyph="▤" title="No projects yet" line="Open a few projects and the board fills in with their state." />
  {:else}
    <div class="board__lanes">
      {#each data.lanes as lane (lane.dir)}
        <section class="lane">
          <header class="lane__head">
            <button class="lane__name" title="Open a session in {lane.name}" onclick={() => openLaneChat(lane)}>{lane.name}</button>
            {#if lane.branch}<span class="lane__branch">{lane.branch}</span>{/if}
          </header>

          <div class="lane__stats">
            {#if lane.dirty > 0}<span class="stat stat--warn" title="uncommitted files">±{lane.dirty}</span>{/if}
            {#if lane.unpushed > 0}<span class="stat" title="unpushed commits">↑{lane.unpushed}</span>{/if}
            {#if lane.behind > 0}<span class="stat" title="behind upstream">↓{lane.behind}</span>{/if}
            {#if lane.todos > 0}<span class="stat stat--dim" title="TODO/FIXME markers">⊙{lane.todos}</span>{/if}
            {#if lane.openPrs > 0}<span class="stat stat--info" title="open PRs">PR {lane.openPrs}</span>{/if}
            {#if lane.openIss > 0}<span class="stat stat--info" title="open issues">⊘{lane.openIss}</span>{/if}
            {#if lane.dirty === 0 && lane.unpushed === 0 && lane.behind === 0 && lane.items.length === 0}
              <span class="stat stat--clean">clean</span>
            {/if}
          </div>

          <div class="lane__items">
            {#each lane.items as it (it.key)}
              <div class="card card--{it.kind}">
                <div class="card__top">
                  <span class="card__glyph">{kindGlyph(it.kind)}</span>
                  <span class="card__title">{it.title}</span>
                </div>
                {#if it.detail}<p class="card__detail">{it.detail}</p>{/if}
                <div class="card__foot">
                  {#if it.url}<Button variant="ghost" size="sm" onclick={() => openURL(it.url)}>Open</Button>{/if}
                  {#if it.task}<Button variant="secondary" size="sm" loading={acting[it.key]} onclick={() => startItem(it)}>Start →</Button>{/if}
                </div>
              </div>
            {/each}
            {#if lane.items.length === 0}
              <p class="lane__empty">Nothing loose here.</p>
            {/if}
          </div>
        </section>
      {/each}
    </div>
  {/if}
</div>

<style>
  .board {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .board__head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--sp-5);
    padding: var(--sp-8) var(--sp-9) var(--sp-5);
  }
  .board__title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-h3) / 1.2 var(--font-sans);
    color: var(--text-primary);
  }
  .board__sub {
    margin: var(--sp-2) 0 0;
    color: var(--text-muted);
    font-size: var(--fs-label);
    max-width: 70ch;
  }
  /* Lanes scroll horizontally — a board, not a list. */
  .board__lanes {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: var(--sp-5);
    overflow-x: auto;
    overflow-y: hidden;
    padding: 0 var(--sp-9) var(--sp-8);
    align-items: flex-start;
  }
  .lane {
    flex: none;
    width: 300px;
    max-height: 100%;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-5);
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-lg);
  }
  .lane--skel {
    height: 220px;
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: board-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes board-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .lane__head {
    display: flex;
    align-items: baseline;
    gap: var(--sp-3);
  }
  .lane__name {
    border: none;
    background: transparent;
    padding: 0;
    cursor: pointer;
    font: var(--fw-semibold) var(--fs-body-sm) / 1.2 var(--font-mono);
    color: var(--text-primary);
  }
  .lane__name:hover {
    color: var(--brand-bright);
  }
  .lane__branch {
    font: var(--fw-regular) var(--fs-micro) / 1 var(--font-mono);
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .lane__stats {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-2);
  }
  .stat {
    font: var(--fw-medium) var(--fs-micro) / 1 var(--font-mono);
    color: var(--text-secondary);
    background: var(--bg-raised-2);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-full);
    padding: 2px var(--sp-3);
  }
  .stat--warn {
    color: var(--warn);
    border-color: var(--warn);
  }
  .stat--info {
    color: var(--info);
    border-color: var(--border-brand-faint);
  }
  .stat--dim {
    color: var(--text-faint);
  }
  .stat--clean {
    color: var(--success);
    border-color: var(--success);
  }
  .lane__items {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    overflow-y: auto;
    min-height: 0;
  }
  .lane__empty {
    margin: var(--sp-2) 0;
    color: var(--text-ghost);
    font-size: var(--fs-label);
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding: var(--sp-4);
    background: var(--bg-raised-2);
    border: 1px solid var(--border-hairline);
    border-left: 2px solid var(--border-subtle);
    border-radius: var(--r-md);
  }
  .card--github {
    border-left-color: var(--info);
  }
  .card--git {
    border-left-color: var(--warn);
  }
  .card__top {
    display: flex;
    gap: var(--sp-2);
    align-items: baseline;
  }
  .card__glyph {
    color: var(--text-muted);
    font-size: var(--fs-label);
  }
  .card__title {
    flex: 1;
    font-size: var(--fs-label);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
    line-height: var(--lh-snug);
  }
  .card__detail {
    margin: 0;
    color: var(--text-muted);
    font-size: var(--fs-micro);
    line-height: var(--lh-snug);
  }
  .card__foot {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
  }
  @media (prefers-reduced-motion: reduce) {
    .lane--skel {
      animation: none;
    }
  }
</style>
