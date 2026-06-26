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

  // Filters: owner (which GitHub owner / "local") + state (what kind of work).
  let ownerFilter = $state<string>("all");
  let stateFilter = $state<"all" | "prs" | "issues" | "dirty">("all");

  // Owners present on the board (from remote lane repos) → filter chips.
  const owners = $derived.by<string[]>(() => {
    const set = new Set<string>();
    for (const l of data?.lanes ?? []) {
      if (l.remote && l.repo.includes("/")) set.add(l.repo.split("/")[0]);
    }
    return [...set].sort();
  });

  function laneMatches(l: BoardLaneDTO): boolean {
    if (ownerFilter !== "all") {
      if (ownerFilter === "local") {
        if (l.remote) return false;
      } else if (!l.remote || !l.repo.startsWith(ownerFilter + "/")) {
        return false;
      }
    }
    switch (stateFilter) {
      case "prs":
        return l.openPrs > 0;
      case "issues":
        return l.openIss > 0;
      case "dirty":
        return l.dirty > 0 || l.unpushed > 0 || l.behind > 0;
      default:
        return true;
    }
  }
  const visibleLanes = $derived((data?.lanes ?? []).filter(laneMatches));

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
  function isPR(it: BoardItemDTO): boolean {
    return (it.detail ?? "").startsWith("PR");
  }

  async function togglePin(lane: BoardLaneDTO) {
    const key = lane.remote ? lane.repo : lane.dir;
    try {
      if (lane.pinned) await Bridge.UnpinLane(key);
      else await Bridge.PinLane(key);
      await load();
    } catch (e) {
      toasts.error(errText(e));
    }
  }
  // Per-card GitHub actions: Review a PR / Work an issue (start a focused session).
  async function ghAction(it: BoardItemDTO) {
    if (!it.url) return;
    acting[it.key] = true;
    try {
      const id = isPR(it) ? await Bridge.ReviewPR(it.url) : await Bridge.WorkIssue(it.url);
      await sessions.refresh();
      router.go("chat", id);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete acting[it.key];
    }
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

  {#if data && data.lanes.length > 0}
    <div class="filters">
      <div class="filtergroup">
        <button class="fchip" class:fchip--on={ownerFilter === "all"} onclick={() => (ownerFilter = "all")}>All</button>
        <button class="fchip" class:fchip--on={ownerFilter === "local"} onclick={() => (ownerFilter = "local")}>Local</button>
        {#each owners as o (o)}
          <button class="fchip" class:fchip--on={ownerFilter === o} onclick={() => (ownerFilter = o)}>{o}</button>
        {/each}
      </div>
      <div class="filtergroup">
        <button class="fchip" class:fchip--on={stateFilter === "all"} onclick={() => (stateFilter = "all")}>Everything</button>
        <button class="fchip" class:fchip--on={stateFilter === "prs"} onclick={() => (stateFilter = "prs")}>PRs</button>
        <button class="fchip" class:fchip--on={stateFilter === "issues"} onclick={() => (stateFilter = "issues")}>Issues</button>
        <button class="fchip" class:fchip--on={stateFilter === "dirty"} onclick={() => (stateFilter = "dirty")}>Uncommitted</button>
      </div>
    </div>
  {/if}

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
      {#each visibleLanes as lane (lane.remote ? lane.repo : lane.dir)}
        <section class="lane" class:lane--remote={lane.remote}>
          <header class="lane__head">
            {#if lane.remote}
              <button class="lane__name" title="Open {lane.repo} on GitHub" onclick={() => openURL(lane.url)}>{lane.name}</button>
              <span class="lane__tag" title={lane.repo}>GitHub</span>
            {:else}
              <button class="lane__name" title="Open a session in {lane.name}" onclick={() => openLaneChat(lane)}>{lane.name}</button>
              {#if lane.branch}<span class="lane__branch">{lane.branch}</span>{/if}
            {/if}
            <button
              class="lane__pin"
              class:lane__pin--on={lane.pinned}
              title={lane.pinned ? "Unpin (hide when idle)" : "Pin (always show)"}
              aria-label={lane.pinned ? "Unpin lane" : "Pin lane"}
              onclick={() => togglePin(lane)}
            >{lane.pinned ? "★" : "☆"}</button>
          </header>

          <div class="lane__stats">
            {#if lane.dirty > 0}<span class="stat stat--warn" title="uncommitted files">±{lane.dirty}</span>{/if}
            {#if lane.unpushed > 0}<span class="stat" title="unpushed commits">↑{lane.unpushed}</span>{/if}
            {#if lane.behind > 0}<span class="stat" title="behind upstream">↓{lane.behind}</span>{/if}
            {#if lane.todos > 0}<span class="stat stat--dim" title="TODO/FIXME markers">⊙{lane.todos}</span>{/if}
            {#if lane.openPrs > 0}<span class="stat stat--info" title="open PRs">PR {lane.openPrs}</span>{/if}
            {#if lane.openIss > 0}<span class="stat stat--info" title="open issues">⊘{lane.openIss}</span>{/if}
            {#if !lane.remote && lane.dirty === 0 && lane.unpushed === 0 && lane.behind === 0 && lane.items.length === 0}
              <span class="stat stat--clean">clean</span>
            {:else if lane.remote && lane.openPrs === 0 && lane.openIss === 0}
              <span class="stat stat--clean">no open work</span>
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
                  {#if it.kind === "github" && it.url}
                    <Button variant="secondary" size="sm" loading={acting[it.key]} onclick={() => ghAction(it)}>
                      {isPR(it) ? "Review →" : "Work →"}
                    </Button>
                  {:else if it.task}
                    <Button variant="secondary" size="sm" loading={acting[it.key]} onclick={() => startItem(it)}>Start →</Button>
                  {/if}
                </div>
              </div>
            {/each}
            {#if lane.items.length === 0 && !lane.remote}
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
  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-5);
    padding: 0 var(--sp-9) var(--sp-4);
  }
  .filtergroup {
    display: flex;
    gap: var(--sp-2);
    flex-wrap: wrap;
  }
  .fchip {
    height: 26px;
    padding: 0 var(--sp-4);
    border-radius: var(--r-full);
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised-2);
    color: var(--text-muted);
    cursor: pointer;
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
  }
  .fchip:hover {
    color: var(--text-primary);
  }
  .fchip--on {
    background: var(--state-selected);
    border-color: var(--border-brand-faint);
    color: var(--brand-bright);
  }
  .lane__pin {
    margin-left: auto;
    border: none;
    background: transparent;
    color: var(--text-faint);
    cursor: pointer;
    font-size: var(--fs-body);
    line-height: 1;
    padding: 0 var(--sp-1);
  }
  .lane__pin:hover {
    color: var(--text-secondary);
  }
  .lane__pin--on {
    color: var(--warn);
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
  /* Remote (GitHub) lanes read quieter than local checkouts. */
  .lane--remote {
    border-style: dashed;
  }
  .lane__tag {
    font: var(--fw-medium) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-full);
    padding: 1px var(--sp-2);
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
