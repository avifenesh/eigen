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
  import type { FeedItemDTO, SessionInfoDTO, DashboardDTO } from "$lib/types";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";

  let starting = $state(false);
  let acting = $state<Record<string, boolean>>({});

  // Working-station command center: today's calendar + unread mail + machine
  // health. One round-trip; refreshed on mount and every 60s.
  let dash = $state<DashboardDTO | null>(null);
  // GPU util/temp history keyed "index|name" → sparklines on the GPU cards.
  let gpuHist = $state<Record<string, { utilPct: number; tempC: number }[] | undefined>>({});
  async function loadDash() {
    try {
      const d = await Bridge.Dashboard();
      if (d) dash = d;
    } catch {
      /* dashboard is best-effort; never block Home */
    }
  }
  async function loadGpuHist() {
    try {
      const h = await Bridge.GPUHistory();
      if (h) gpuHist = h;
    } catch {
      /* no GPU / sampler — sparklines just don't render */
    }
  }

  $effect(() => {
    sessions.refresh();
    loadDash();
    loadGpuHist();
    const t = setInterval(loadDash, 60_000);
    // History refreshes faster (the sampler runs every 5s) so the trend moves.
    const tg = setInterval(loadGpuHist, 6_000);
    return () => {
      clearInterval(t);
      clearInterval(tg);
    };
  });

  // sparkPath builds an SVG polyline path for a series over a 0..max range.
  function sparkPath(vals: number[], max: number, w: number, h: number): string {
    if (vals.length < 2) return "";
    const n = vals.length;
    return vals
      .map((v, i) => {
        const x = (i / (n - 1)) * w;
        const y = h - Math.min(1, Math.max(0, v / max)) * h;
        return `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .join(" ");
  }
  function gpuKey(i: number, name: string): string {
    return `${i}|${name}`;
  }
  function histFor(i: number, name: string): { utilPct: number; tempC: number }[] {
    return gpuHist[gpuKey(i, name)] ?? [];
  }

  // Health bar tone: calm under 70%, warn 70–90, hot above.
  function tone(pct: number): "ok" | "warn" | "hot" {
    if (pct >= 90) return "hot";
    if (pct >= 70) return "warn";
    return "ok";
  }
  function eventTime(e: { start: string; allDay: boolean }): string {
    if (e.allDay) return "all day";
    const d = new Date(e.start);
    if (isNaN(d.getTime())) return e.start;
    const today = new Date();
    const sameDay = d.toDateString() === today.toDateString();
    const t = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    return sameDay ? t : `${d.toLocaleDateString([], { weekday: "short" })} ${t}`;
  }
  function fromName(from: string): string {
    // "Name <email>" → Name; bare email → the local part.
    const m = from.match(/^\s*"?([^"<]+?)"?\s*</);
    if (m) return m[1].trim();
    const at = from.indexOf("@");
    return at > 0 ? from.slice(0, at) : from;
  }

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

  // "Start working →": commit to an idea as real work (plan + implement), not
  // just explore it. Distinct intent from actOn (which researches/scopes).
  async function startWorking(it: FeedItemDTO) {
    if (!it.task) return;
    acting[it.key] = true;
    try {
      const id = await Bridge.StartWorkingFromFeed(it.dir ?? "", it.task);
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
    <Button variant="primary" loading={starting} onclick={startSession}>Start a session</Button>
  </header>

  <button class="strip" onclick={() => router.go("observe")} title="Open Observe" aria-label="Open Observe — telemetry">
    <div class="strip__stat"><span class="strip__v tnum">{stats?.sessions ?? sessions.count}</span><span class="strip__l">sessions</span></div>
    <div class="strip__sep"></div>
    <div class="strip__stat"><span class="strip__v tnum" class:strip__v--live={(stats?.running_turns ?? 0) > 0}>{stats?.running_turns ?? 0}</span><span class="strip__l">running</span></div>
    <div class="strip__sep"></div>
    <div class="strip__stat"><span class="strip__v tnum">{stats?.bg_tasks ?? 0}</span><span class="strip__l">tasks</span></div>
    <div class="strip__sep"></div>
    <div class="strip__stat"><span class="strip__v tnum">{cacheHit}%</span><span class="strip__l">cache hit</span></div>
  </button>

  <!-- ZONE 1.5 · TODAY (command center: calendar · mail · machine) -->
  {#if dash}
    <section class="today">
      <!-- Calendar -->
      <button class="panel" onclick={() => router.go("connectors")} title="Manage Google in Connectors">
        <div class="panel__head"><span class="panel__icon">📅</span><span class="panel__title">Today</span></div>
        {#if !dash.googleConnected}
          <p class="panel__empty">Connect Google to see your calendar.</p>
        {:else if dash.events.length === 0}
          <p class="panel__empty">No upcoming events.</p>
        {:else}
          <ul class="panel__list">
            {#each dash.events.slice(0, 5) as e (e.summary + e.start)}
              <li class="evt">
                <span class="evt__time tnum">{eventTime(e)}</span>
                <span class="evt__sum">{e.summary}</span>
              </li>
            {/each}
          </ul>
        {/if}
      </button>

      <!-- Mail -->
      <button class="panel" onclick={() => router.go("connectors")} title="Manage Google in Connectors">
        <div class="panel__head">
          <span class="panel__icon">✉</span><span class="panel__title">Inbox</span>
          {#if dash.googleConnected && dash.unreadCount > 0}<span class="panel__badge tnum">{dash.unreadCount}</span>{/if}
        </div>
        {#if !dash.googleConnected}
          <p class="panel__empty">Connect Google to see your inbox.</p>
        {:else if dash.unread.length === 0}
          <p class="panel__empty">Inbox zero — nothing unread.</p>
        {:else}
          <ul class="panel__list">
            {#each dash.unread.slice(0, 5) as m (m.from + m.subject)}
              <li class="mail">
                <span class="mail__from">{fromName(m.from)}</span>
                <span class="mail__subj">{m.subject}</span>
              </li>
            {/each}
          </ul>
        {/if}
      </button>

      <!-- Machine health -->
      <div class="panel">
        <div class="panel__head">
          <span class="panel__icon">▦</span><span class="panel__title">Machine</span>
          {#if dash.health.cpuTempC > 0}<span class="panel__temp {dash.health.cpuTempC >= 90 ? 'hot' : dash.health.cpuTempC >= 80 ? 'warm' : ''}">{Math.round(dash.health.cpuTempC)}°C</span>{/if}
        </div>
        <div class="metrics">
          <div class="metric">
            <div class="metric__top"><span class="metric__k">CPU load</span><span class="metric__v tnum">{Math.round(dash.health.loadPerCpu * 100)}%</span></div>
            <div class="bar"><div class="bar__fill bar__fill--{tone(dash.health.loadPerCpu * 100)}" style="width:{Math.min(100, dash.health.loadPerCpu * 100)}%"></div></div>
          </div>
          <div class="metric">
            <div class="metric__top"><span class="metric__k">Memory</span><span class="metric__v tnum">{dash.health.memUsedGb}/{dash.health.memTotalGb} GB</span></div>
            <div class="bar"><div class="bar__fill bar__fill--{tone(dash.health.memUsedPct)}" style="width:{dash.health.memUsedPct}%"></div></div>
          </div>
          {#if dash.health.swapTotalGb > 0}
            <div class="metric">
              <div class="metric__top"><span class="metric__k">Swap</span><span class="metric__v tnum">{dash.health.swapUsedGb}/{dash.health.swapTotalGb} GB</span></div>
              <div class="bar"><div class="bar__fill bar__fill--{tone(dash.health.swapUsedPct)}" style="width:{dash.health.swapUsedPct}%"></div></div>
            </div>
          {/if}
          <div class="metric">
            <div class="metric__top"><span class="metric__k">Disk /</span><span class="metric__v tnum">{dash.health.diskUsedPct}%</span></div>
            <div class="bar"><div class="bar__fill bar__fill--{tone(dash.health.diskUsedPct)}" style="width:{dash.health.diskUsedPct}%"></div></div>
          </div>
        </div>
      </div>
    </section>

    <!-- GPUs — full-width below the today grid (training-rig signals). -->
    {#if dash.health.gpus && dash.health.gpus.length > 0}
      <section class="gpus">
        {#each dash.health.gpus as g, i (i)}
          <div class="gpu">
            <div class="gpu__head">
              <span class="gpu__icon">▣</span>
              <span class="gpu__name">{g.name}</span>
              <span class="gpu__spacer"></span>
              {#if g.powerW > 0}<span class="gpu__pow tnum">{Math.round(g.powerW)}W</span>{/if}
              <span class="gpu__temp {g.tempC >= 90 ? 'hot' : g.tempC >= 80 ? 'warm' : ''} tnum">{Math.round(g.tempC)}°C</span>
            </div>
            <div class="gpu__metrics">
              <div class="metric">
                <div class="metric__top"><span class="metric__k">GPU util</span><span class="metric__v tnum">{Math.round(g.utilPct)}%</span></div>
                <div class="bar"><div class="bar__fill bar__fill--{tone(g.utilPct)}" style="width:{g.utilPct}%"></div></div>
              </div>
              <div class="metric">
                <div class="metric__top"><span class="metric__k">VRAM</span><span class="metric__v tnum">{g.memUsedGb}/{g.memTotalGb} GB</span></div>
                <div class="bar"><div class="bar__fill bar__fill--{tone(g.memUsedPct)}" style="width:{g.memUsedPct}%"></div></div>
              </div>
            </div>
            {#if histFor(i, g.name).length > 1}
              <div class="spark">
                <svg viewBox="0 0 100 24" preserveAspectRatio="none" class="spark__svg" aria-label="GPU util + temp trend">
                  <path class="spark__util" d={sparkPath(histFor(i, g.name).map((p) => p.utilPct), 100, 100, 24)} />
                  <path class="spark__temp" d={sparkPath(histFor(i, g.name).map((p) => p.tempC), 100, 100, 24)} />
                </svg>
                <div class="spark__legend"><span class="spark__l spark__l--util">util</span><span class="spark__l spark__l--temp">temp</span></div>
              </div>
            {/if}
          </div>
        {/each}
      </section>
    {/if}
  {/if}

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
              <Button variant="secondary" size="sm" loading={acting[it.key]} onclick={() => startWorking(it)}>Start working →</Button>
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
  /* Tighter cockpit: this is the landing view, so its density sets the app's
     first impression. Trimmed outer padding (was sp-9/sp-10) and zone gap (was
     sp-9) so it reads as a dense cockpit, not a spacious marketing page. */
  .home {
    height: 100%;
    overflow-y: auto;
    padding: var(--sp-6) var(--sp-7) var(--sp-7);
    display: flex;
    flex-direction: column;
    gap: var(--sp-6);
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
    /* was --fs-display (28px) — a 28px bold greeting ate a big vertical band
       before any useful info. h2 (18px) still leads, far less bulk. */
    font: var(--fw-semibold) var(--fs-h2) / var(--lh-tight) var(--font-display);
    letter-spacing: var(--ls-heading);
    color: var(--text-primary);
  }
  .cockpit__sub {
    margin: var(--sp-2) 0 0;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  .strip {
    display: flex;
    align-items: center;
    gap: var(--sp-6);
    padding: var(--sp-4) var(--sp-6);
    /* sits ON the page, not in a trench — was --bg-well (darker than the
       --bg-base page), which read as a heavy recessed band across the top. */
    background: var(--bg-raised);
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
    /* was --fs-h1 (22px) bold — oversized for a stat chip; h3 (15px) scans
       like a real cockpit metric, not a hero number. */
    font: var(--fw-semibold) var(--fs-h3) / 1 var(--font-display);
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

  /* ZONE 1.5 · today (command center) */
  .today {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: var(--sp-5);
  }
  .panel {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-4) var(--sp-5);
    min-height: 110px;
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-lg);
    text-align: left;
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  /* The machine panel is a div (not navigable) — no hover affordance. */
  button.panel:hover {
    border-color: var(--border-subtle);
    background: var(--bg-raised-2);
  }
  button.panel:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .panel__head {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .panel__icon {
    font-size: var(--fs-body);
  }
  .panel__title {
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .panel__badge {
    margin-left: auto;
    font-size: var(--fs-micro);
    font-weight: var(--fw-bold);
    color: var(--brand-bright);
    background: var(--state-selected);
    border: 1px solid var(--border-brand-faint);
    border-radius: var(--r-full);
    padding: 1px var(--sp-3);
  }
  .panel__empty {
    margin: auto 0;
    color: var(--text-ghost);
    font-size: var(--fs-label);
  }
  .panel__list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .evt,
  .mail {
    display: flex;
    gap: var(--sp-4);
    align-items: baseline;
    min-width: 0;
  }
  .evt__time {
    flex: none;
    width: 64px;
    font-size: var(--fs-label);
    color: var(--brand-bright);
  }
  .evt__sum,
  .mail__subj {
    flex: 1;
    font-size: var(--fs-body-sm);
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mail__from {
    flex: none;
    width: 96px;
    font-size: var(--fs-label);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .panel__temp {
    margin-left: auto;
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-mono);
    color: var(--text-muted);
  }
  .panel__temp.warm {
    color: var(--warn);
  }
  .panel__temp.hot {
    color: var(--error);
    font-weight: var(--fw-bold);
  }
  .metrics {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }

  /* GPUs — full-width row(s) of accelerator cards. */
  .gpus {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: var(--sp-5);
  }
  .gpu {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
    padding: var(--sp-4) var(--sp-6);
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-lg);
  }
  .gpu__head {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  /* neutral icon — the GPU card is informational, not a separate semantic zone;
     the blue --accent left-rail + icon was a competing hue on Home (the only
     blue among teal chrome). */
  .gpu__icon {
    color: var(--text-muted);
  }
  .gpu__name {
    font: var(--fw-semibold) var(--fs-body-sm) / 1 var(--font-sans);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .gpu__spacer {
    flex: 1;
  }
  .gpu__pow {
    font-size: var(--fs-label);
    color: var(--text-faint);
  }
  .gpu__temp {
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .gpu__temp.warm {
    color: var(--warn);
  }
  .gpu__temp.hot {
    color: var(--error);
    font-weight: var(--fw-bold);
  }
  .gpu__metrics {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .spark {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .spark__svg {
    flex: 1;
    height: 24px;
    width: 100%;
  }
  .spark__util,
  .spark__temp {
    fill: none;
    stroke-width: 1.5;
    vector-effect: non-scaling-stroke;
  }
  .spark__util {
    stroke: var(--brand);
  }
  .spark__temp {
    stroke: var(--warn);
  }
  .spark__legend {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .spark__l {
    font-size: var(--fs-micro);
  }
  .spark__l--util {
    color: var(--brand);
  }
  .spark__l--temp {
    color: var(--warn);
  }
  .metric__top {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    margin-bottom: var(--sp-2);
  }
  .metric__k {
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .metric__v {
    font-size: var(--fs-label);
    color: var(--text-secondary);
  }
  .bar {
    height: 6px;
    border-radius: var(--r-full);
    background: var(--bg-inset);
    overflow: hidden;
  }
  .bar__fill {
    height: 100%;
    border-radius: var(--r-full);
    transition: width var(--dur-slow) var(--ease-out);
  }
  .bar__fill--ok {
    background: var(--brand);
  }
  .bar__fill--warn {
    background: var(--warn);
  }
  .bar__fill--hot {
    background: var(--danger, var(--err));
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
  /* Uniform hairline edge — the per-kind color now lives on the glyph only.
     A column of feed cards used to show amber/blue/teal/green 2px left stripes
     (one per kind), which was the busiest, most over-colored pattern on the
     screen and fought the "one living accent" intent. The glyph carries kind;
     the card edge stays neutral. */
  .fc {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-4) var(--sp-5);
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    transition: background var(--dur-fast) var(--ease-out);
  }
  .fc:hover {
    background: var(--bg-raised-2);
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
  /* Kind shows via the glyph color (a small accent) instead of a full-height
     left stripe — the cue stays, the visual weight drops. */
  .fc--warn .fc__glyph {
    color: var(--warn);
  }
  .fc--info .fc__glyph {
    color: var(--info);
  }
  .fc--brand .fc__glyph {
    color: var(--brand);
  }
  .fc--success .fc__glyph {
    color: var(--success);
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
  /* WORKING — alive: teal edge + a STATIC teal halo. The "alive" motion is
     carried by the row's StatusDot (which breathes via opacity/transform).
     Previously each working row animated box-shadow on its own infinite track —
     N working sessions = N per-frame main-thread repaints on the dashboard,
     stacked with the rail/topbar/dot animations. Static halo + the dot's
     breathe reads alive without the repaint storm. */
  .lr--working {
    border-left-color: var(--brand);
    box-shadow: var(--glow-live);
  }
  /* APPROVAL — blocked on the user: warn edge + static warn halo. */
  .lr--approval {
    border-left-color: var(--warn);
    box-shadow: var(--glow-warn);
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
