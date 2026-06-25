<script lang="ts">
  // Home — the home base, NOT a session list. Opening it answers, top to
  // bottom: what should I act on, what's live right now, where did I leave off.
  // Five zones, each rendering independently so a slow/empty one never blanks
  // the page: cockpit (greeting + live stats) · Act On (proactive feed) ·
  // Ideas (LLM suggestions) · Working now (live sessions) · Resume (recent).
  import { sessions } from "$lib/stores/sessions.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { feed } from "$lib/stores/feed.svelte";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { router } from "$lib/router.svelte";
  import { now } from "$lib/stores/clock.svelte";
  import { sessionDot } from "$lib/status";
  import { errText } from "$lib/errors";
  import { Bridge } from "$lib/bridge";
  import { Browser } from "@wailsio/runtime";
  import type { FeedItemDTO, SessionInfoDTO } from "$lib/types";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";

  let starting = $state(false);
  let acting = $state<Record<string, boolean>>({});

  $effect(() => {
    sessions.refresh();
  });

  const stats = $derived(daemon.stats);
  // Cache-hit% = cached-read tokens over the FULL prompt size. The provider
  // reports input_tokens as the FRESH (uncached) portion only; cache_read and
  // cache_write are separate buckets, so the denominator is their sum (else a
  // mostly-cached prompt yields read/input > 100%). Clamped to [0,100].
  const cacheHit = $derived.by(() => {
    if (!stats) return 0;
    const read = stats.cache_read_tokens ?? 0;
    const total = (stats.input_tokens ?? 0) + read + (stats.cache_write_tokens ?? 0);
    if (total <= 0) return 0;
    return Math.min(100, Math.max(0, Math.round((read / total) * 100)));
  });

  // Live sessions = working or awaiting approval; recent = everything else.
  const live = $derived(sessions.list.filter((s) => s.status === "working" || s.status === "approval"));
  const recent = $derived(sessions.list.slice(0, 6));

  function greeting(): string {
    const h = new Date().getHours();
    if (h < 5) return "Burning the midnight oil";
    if (h < 12) return "Good morning";
    if (h < 18) return "Good afternoon";
    return "Good evening";
  }

  function kindGlyph(kind: string): string {
    switch (kind) {
      case "git": return "±";
      case "github": return "◉";
      case "memory": return "↺";
      case "suggest": return "✧";
      default: return "•";
    }
  }
  function kindTone(kind: string): "warn" | "info" | "brand" | "success" | "neutral" {
    switch (kind) {
      case "git": return "warn";
      case "github": return "info";
      case "memory": return "brand";
      case "suggest": return "success";
      default: return "neutral";
    }
  }

  async function startSession() {
    starting = true;
    try {
      const id = await Bridge.NewSession("", "", "");
      await sessions.refresh();
      router.go("chat", id);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      starting = false;
    }
  }

  async function actOn(it: FeedItemDTO) {
    // A task-less item is fundamentally a link, not work: StartFromFeed would
    // create the session then send nothing, leaving an orphan empty session per
    // click. Open the URL instead; only start a session when there's a task.
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

  function rel(updatedNano: number): string {
    void now.ms; // tie to shared clock so the label ticks
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
    return p[p.length - 1] || dir || "";
  }
  function openSession(s: SessionInfoDTO) {
    router.go("chat", s.id);
  }
  function openURL(url?: string) {
    if (!url) return;
    try {
      Browser.OpenURL(url);
    } catch {
      window.open(url, "_blank");
    }
  }
</script>

<div class="home selectable">
  <!-- ZONE 1 · COCKPIT -->
  <header class="cockpit">
    <div class="cockpit__lede">
      <h1 class="cockpit__greet">{greeting()}.</h1>
      <p class="cockpit__sub">Your agent, everywhere — here's what's worth your attention.</p>
    </div>
    <Button variant="primary" size="lg" loading={starting} onclick={startSession}>Start a session</Button>
  </header>

  <button class="strip" onclick={() => router.go("observe")} title="Open Observe" aria-label="Open Observe — telemetry">
    <div class="strip__stat"><span class="strip__v tnum">{stats?.sessions ?? sessions.count}</span><span class="strip__l">sessions</span></div>
    <div class="strip__sep"></div>
    <div class="strip__stat"><span class="strip__v tnum" class:strip__v--live={(stats?.running_turns ?? 0) > 0}>{stats?.running_turns ?? 0}</span><span class="strip__l">running</span></div>
    <div class="strip__sep"></div>
    <div class="strip__stat"><span class="strip__v tnum">{stats?.bg_tasks ?? 0}</span><span class="strip__l">agents</span></div>
    <div class="strip__sep"></div>
    <div class="strip__stat"><span class="strip__v tnum">{cacheHit}%</span><span class="strip__l">cache hit</span></div>
  </button>

  <!-- ZONE 2 · ACT ON (the proactive feed) -->
  <section class="zone">
    <div class="zone__head">
      <h2 class="zone__title">Act on</h2>
      {#if feed.actOn.length > 0}<span class="zone__n tnum">{feed.actOn.length}</span>{/if}
    </div>
    {#if feed.actOn.length === 0}
      <p class="zone__empty">
        {feed.fresh ? "Nothing loose to act on — clean tree, no open work." : "Scanning your projects for things to act on…"}
      </p>
    {:else}
      <div class="cards">
        {#each feed.actOn as it (it.key)}
          <div class="fc fc--{kindTone(it.kind)}">
            <div class="fc__head">
              <span class="fc__glyph">{kindGlyph(it.kind)}</span>
              <span class="fc__title">{it.title}</span>
              <button class="fc__x" title="Dismiss" aria-label="Dismiss" onclick={() => feed.dismiss(it.key)}>×</button>
            </div>
            {#if it.detail}<p class="fc__detail">{it.detail}</p>{/if}
            <div class="fc__foot">
              {#if it.dirName}<Badge tone="neutral" truncate>{it.dirName}</Badge>{/if}
              <span class="fc__spacer"></span>
              {#if it.url}<Button variant="ghost" size="sm" onclick={() => openURL(it.url)}>Open</Button>{/if}
              {#if it.task}<Button variant="secondary" size="sm" loading={acting[it.key]} onclick={() => actOn(it)}>Start →</Button>{/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </section>

  <!-- ZONE 3 · IDEAS (LLM suggestions) -->
  {#if feed.ideas.length > 0}
    <section class="zone">
      <div class="zone__head">
        <h2 class="zone__title">Ideas</h2>
        <Badge tone="success">suggested</Badge>
      </div>
      <div class="cards">
        {#each feed.ideas as it (it.key)}
          <div class="fc fc--success">
            <div class="fc__head">
              <span class="fc__glyph">✧</span>
              <span class="fc__title">{it.title}</span>
              <button class="fc__x" title="Dismiss" aria-label="Dismiss" onclick={() => feed.dismiss(it.key)}>×</button>
            </div>
            {#if it.detail}<p class="fc__detail">{it.detail}</p>{/if}
            <div class="fc__foot">
              {#if it.dirName}<Badge tone="neutral" truncate>{it.dirName}</Badge>{/if}
              <span class="fc__spacer"></span>
              <Button variant="ghost" size="sm" loading={acting[it.key]} onclick={() => actOn(it)}>Explore →</Button>
            </div>
          </div>
        {/each}
      </div>
    </section>
  {/if}

  <!-- ZONE 4 · WORKING NOW -->
  {#if live.length > 0}
    <section class="zone">
      <div class="zone__head">
        <h2 class="zone__title">Working now</h2>
        <span class="zone__n tnum">{live.length}</span>
      </div>
      <div class="live">
        {#each live as s (s.id)}
          <button
            class="lr"
            class:lr--working={s.status === "working"}
            class:lr--approval={s.status === "approval"}
            aria-label={`Open session ${s.title || "untitled"}`}
            onclick={() => openSession(s)}
          >
            <StatusDot state={sessionDot(s.status)} size={8} pulse={s.status === "working" || s.status === "approval"} />
            <span class="lr__title">{s.title || "untitled session"}</span>
            {#if s.status === "approval"}<Badge tone="warn">needs approval</Badge>{/if}
            <span class="lr__dir">{base(s.dir)}</span>
            {#if s.model}<Badge tone="neutral" truncate>{s.model}</Badge>{/if}
          </button>
        {/each}
      </div>
    </section>
  {/if}

  <!-- ZONE 5 · RESUME -->
  <section class="zone">
    <div class="zone__head">
      <h2 class="zone__title">Resume</h2>
      <Button variant="link" size="sm" onclick={() => router.go("sessions")}>All sessions</Button>
    </div>
    {#if sessions.loading && sessions.count === 0}
      <div class="rows">{#each Array(4) as _, i (i)}<div class="row-skel"></div>{/each}</div>
    {:else if sessions.error && sessions.count === 0}
      <p class="zone__empty">Couldn't load sessions — {sessions.error}</p>
    {:else if recent.length === 0}
      <p class="zone__empty">No sessions yet — start one above.</p>
    {:else}
      <div class="rows">
        {#each recent as s (s.id)}
          <button class="row" aria-label={`Open session ${s.title || "untitled"}`} onclick={() => openSession(s)}>
            <StatusDot state={sessionDot(s.status)} size={6} />
            <span class="row__title">{s.title || "untitled session"}</span>
            <span class="row__dir">{base(s.dir)}</span>
            <span class="row__meta tnum">{s.turns} turn{s.turns === 1 ? "" : "s"}</span>
            <span class="row__when">{rel(s.updated)}</span>
          </button>
        {/each}
      </div>
    {/if}
  </section>
</div>

<style>
  .home {
    height: 100%;
    overflow-y: auto;
    padding: var(--sp-9) var(--sp-10) var(--sp-10);
    display: flex;
    flex-direction: column;
    gap: var(--sp-9);
    max-width: 1080px;
  }

  /* ZONE 1 · cockpit */
  .cockpit {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--sp-6);
  }
  .cockpit__greet {
    margin: 0;
    font: var(--fw-bold) var(--fs-display) / var(--lh-tight) var(--font-display);
    letter-spacing: var(--ls-display);
    color: var(--text-primary);
  }
  .cockpit__sub {
    margin: var(--sp-3) 0 0;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  .strip {
    display: flex;
    align-items: center;
    gap: var(--sp-6);
    padding: var(--sp-5) var(--sp-7);
    background: var(--bg-well);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-lg);
    cursor: pointer;
    text-align: left;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .strip:hover {
    border-color: var(--border-subtle);
  }
  .strip:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .strip__stat {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
  }
  .strip__v {
    font: var(--fw-bold) var(--fs-h1) / 1 var(--font-display);
    color: var(--text-primary);
  }
  .strip__v--live {
    color: var(--brand-bright);
  }
  .strip__l {
    font-size: var(--fs-micro);
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }
  .strip__sep {
    width: 1px;
    align-self: stretch;
    background: var(--divider);
  }

  /* zones */
  .zone {
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .zone__head {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .zone__title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .zone__n {
    font-size: var(--fs-label);
    color: var(--text-ghost);
  }
  .zone__empty {
    margin: 0;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }

  /* ZONE 2/3 · feed cards */
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: var(--sp-5);
  }
  .fc {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-5);
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-left: 2px solid var(--border-subtle);
    border-radius: var(--r-md);
    transition:
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out),
      transform var(--dur-fast) var(--ease-out);
  }
  .fc:hover {
    background: var(--bg-raised-2);
    transform: translateY(-1px);
  }
  .fc--warn {
    border-left-color: var(--warn);
  }
  .fc--info {
    border-left-color: var(--info);
  }
  .fc--brand {
    border-left-color: var(--brand);
  }
  .fc--success {
    border-left-color: var(--success);
  }
  .fc--neutral {
    border-left-color: var(--border-strong);
  }
  .fc__head {
    display: flex;
    align-items: baseline;
    gap: var(--sp-3);
  }
  .fc__glyph {
    color: var(--text-secondary);
    font-size: var(--fs-body);
  }
  .fc__title {
    flex: 1;
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
    line-height: var(--lh-snug);
  }
  .fc__x {
    border: none;
    background: transparent;
    color: var(--text-ghost);
    cursor: pointer;
    font-size: 15px;
    line-height: 1;
    padding: 0 var(--sp-1);
    border-radius: var(--r-xs);
  }
  .fc__x:hover {
    color: var(--text-primary);
    background: var(--state-hover);
  }
  .fc__x:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
    color: var(--text-primary);
  }
  .fc__detail {
    margin: 0;
    color: var(--text-muted);
    font-size: var(--fs-label);
    line-height: var(--lh-snug);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .fc__foot {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    margin-top: var(--sp-2);
  }
  .fc__spacer {
    flex: 1;
  }

  /* ZONE 4 · working now */
  .live {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .lr {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    padding: var(--sp-4) var(--sp-5);
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-left: 2px solid var(--border-subtle);
    border-radius: var(--r-md);
    cursor: pointer;
    text-align: left;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .lr:hover {
    background: var(--bg-raised-2);
  }
  /* WORKING — alive: teal edge + a teal halo that breathes, matching Live. The
     home base's "working now" zone must read as the most alive surface here. */
  .lr--working {
    border-left-color: var(--brand);
    animation: lr-live var(--breath) var(--ease-inout) infinite;
  }
  @keyframes lr-live {
    0%,
    100% {
      box-shadow: 0 0 0 1px var(--border-brand-faint);
    }
    50% {
      box-shadow: var(--glow-live);
    }
  }
  /* APPROVAL — blocked on the user: warn edge + warn halo, a distinct register. */
  .lr--approval {
    border-left-color: var(--warn);
    animation: lr-wait var(--breath) var(--ease-inout) infinite;
  }
  @keyframes lr-wait {
    0%,
    100% {
      box-shadow: 0 0 0 1px rgba(224, 179, 106, 0.25);
    }
    50% {
      box-shadow: var(--glow-warn);
    }
  }
  .lr:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .lr__title {
    font-weight: var(--fw-medium);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
  }
  .lr__dir {
    margin-left: auto;
    font-size: var(--fs-label);
    color: var(--text-muted);
  }

  /* ZONE 5 · resume rows */
  .rows {
    display: flex;
    flex-direction: column;
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    padding: var(--sp-4) var(--sp-3);
    border: none;
    border-bottom: 1px solid var(--divider);
    background: transparent;
    cursor: pointer;
    text-align: left;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .row:last-child {
    border-bottom: none;
  }
  .row:hover {
    background: var(--state-hover);
  }
  .row:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .row__title {
    flex: 1;
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .row__dir {
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .row__meta {
    font-size: var(--fs-label);
    color: var(--text-ghost);
    min-width: 64px;
    text-align: right;
  }
  .row__when {
    font-size: var(--fs-label);
    color: var(--text-faint);
    min-width: 64px;
    text-align: right;
  }
  .row-skel {
    height: 34px;
    border-bottom: 1px solid var(--divider);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: home-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes home-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .fc,
    .lr,
    .row,
    .strip {
      transition: none;
    }
    .lr--working,
    .lr--approval,
    .row-skel {
      animation: none;
    }
    /* hold the live/approval glow steady rather than breathing */
    .lr--working {
      box-shadow: var(--glow-live);
    }
    .lr--approval {
      box-shadow: 0 0 0 1px rgba(224, 179, 106, 0.35);
    }
  }
</style>
