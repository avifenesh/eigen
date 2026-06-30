<script lang="ts">
  // Primary navigation. Zones group the rail by intent (Work / Knowledge /
  // System). The active item gets a teal left-edge + selected wash. Badges
  // surface live counts (sessions, running turns, background tasks).
  import { router, type Route } from "$lib/router.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";
  import { sessionUnread } from "$lib/stores/sessionUnread.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { feed } from "$lib/stores/feed.svelte";
  import { ui } from "$lib/stores/ui.svelte";

  type Item = { route: Route; label: string; glyph: string };
  // collapsible zones fold behind their label (a click toggles). The System
  // zone (8 configure-once/check-occasionally routes) defaults folded so the
  // standing rail reads as a workspace, not a daemon control panel — routes
  // stay valid as deep-links and the label shows a chevron + count when folded.
  type Zone = { name: string; items: Item[]; collapsible?: boolean };

  // Collapsed = icon-only rail (glyphs + badges, no labels/zone headings).
  const collapsed = $derived(ui.railCollapsed);

  // Live sessions surfaced as a sub-list under Chat: working or awaiting an
  // approval — the ones worth hopping between. Sorted newest-first (the list is
  // already newest-first from the store). The Chat item expands to show them so
  // the user can navigate several running sessions at once without leaving the
  // rail. Collapsed into icon-only mode, the sub-list is hidden (no room).
  const running = $derived(
    sessions.list.filter((s) => s.status === "working" || s.status === "approval"),
  );
  /** Running + idle sessions with an unread reply (stay visible after turn ends). */
  const chatRailSessions = $derived.by(() => {
    const ids = new Set<string>();
    const out: typeof sessions.list = [];
    for (const s of running) {
      if (!ids.has(s.id)) {
        ids.add(s.id);
        out.push(s);
      }
    }
    for (const s of sessions.list) {
      if (sessionUnread.isUnread(s.id) && !ids.has(s.id)) {
        ids.add(s.id);
        out.push(s);
      }
    }
    return out;
  });
  // The session the Chat view currently shows (route param), so the matching
  // sub-row reads as selected.
  const activeSession = $derived(router.route === "chat" ? router.param : undefined);

  function shortTitle(s: { title: string; dir: string }): string {
    const t = (s.title ?? "").trim();
    if (t) return t;
    const d = (s.dir ?? "").replace(/\/+$/, "");
    return d.slice(d.lastIndexOf("/") + 1) || "session";
  }

  const zones: Zone[] = [
    {
      name: "Work",
      items: [
        { route: "home", label: "Home", glyph: "◆" },
        { route: "chat", label: "Chat", glyph: "▶" },
        { route: "board", label: "Board", glyph: "▤" },
        { route: "tasks", label: "Tasks", glyph: "⋔" },
        { route: "live", label: "Live", glyph: "◐" },
        { route: "sessions", label: "Sessions", glyph: "≡" },
      ],
    },
    {
      name: "Knowledge",
      items: [
        { route: "memory", label: "Memory", glyph: "❖" },
        { route: "notes", label: "Notes", glyph: "🗒" },
        { route: "dreaming", label: "Dreaming", glyph: "☾" },
        { route: "skills", label: "Skills", glyph: "✦" },
        { route: "reviewers", label: "Reviewers", glyph: "🔍" },
      ],
    },
    {
      name: "System",
      collapsible: true,
      items: [
        { route: "observe", label: "Observe", glyph: "◉" },
        { route: "routing", label: "Routing", glyph: "⇄" },
        { route: "machines", label: "Machines", glyph: "⊟" },
        { route: "crons", label: "Crons", glyph: "◷" },
        { route: "plugins", label: "Plugins", glyph: "⊞" },
        { route: "connectors", label: "Connectors", glyph: "⟐" },
        { route: "profile", label: "Profile", glyph: "◑" },
        { route: "config", label: "Config", glyph: "⚙" },
      ],
    },
  ];

  // Folded state for collapsible zones, keyed by name, persisted. Defaults
  // folded. A zone auto-expands while one of its own routes is active so the
  // current page is never hidden.
  let zoneFolded = $state<Record<string, boolean>>(loadZoneFolded());
  function loadZoneFolded(): Record<string, boolean> {
    const out: Record<string, boolean> = {};
    for (const z of zones) {
      if (!z.collapsible) continue;
      let v = true; // default folded
      try {
        const s = localStorage.getItem("eigen.rail.zone." + z.name);
        if (s != null) v = s === "1";
      } catch {}
      out[z.name] = v;
    }
    return out;
  }
  function zoneIsFolded(z: Zone): boolean {
    if (!z.collapsible) return false;
    // never hide the active page: if the current route lives in this zone, show it.
    if (z.items.some((it) => it.route === router.route)) return false;
    return zoneFolded[z.name] ?? true;
  }
  function toggleZone(z: Zone): void {
    const next = !(zoneFolded[z.name] ?? true);
    zoneFolded[z.name] = next;
    try {
      localStorage.setItem("eigen.rail.zone." + z.name, next ? "1" : "0");
    } catch {}
  }

  // Home surfaces the proactive-feed "act on" count (what needs attention),
  // not the raw session total — the rail nudges toward action.
  function badge(route: Route): number {
    if (route === "home") return feed.actOn.length;
    if (route === "chat") return Math.max(daemon.stats?.running_turns ?? 0, sessionUnread.count);
    if (route === "tasks") return daemon.stats?.bg_tasks ?? 0;
    if (route === "live") return sessions.list.filter((s) => s.status === "working" || s.status === "approval").length;
    if (route === "sessions") return sessions.count;
    return 0;
  }

  // These routes count *active work* — when their badge is non-zero the count
  // is teal and breathing, so the eye is drawn to what is running/fresh. Other
  // badges (e.g. total sessions) stay neutral: a tally, not a live signal.
  const liveRoutes = new Set<Route>(["home", "chat", "tasks", "live"]);

  // Rail footer mirrors the daemon connection so the chrome bookends: brand at
  // the top, engine status at the bottom. Version is shown only when known.
  const online = $derived(daemon.status === "online");
  const offline = $derived(daemon.status === "offline");
  const footState = $derived(online ? "online" : offline ? "offline" : "connecting");
  // Daemon version when known, else this gui binary's own. A mismatch (daemon
  // built from a different revision than the running GUI) is surfaced so a stale
  // daemon isn't mistaken for the current build.
  const version = $derived(daemon.daemonVersion || daemon.guiVersion);
  const mismatch = $derived(daemon.versionMismatch);
  const versionTitle = $derived(
    mismatch
      ? `version mismatch — daemon ${daemon.daemonVersion}, gui ${daemon.guiVersion}`
      : version
        ? `eigen ${version}`
        : "",
  );
</script>

<nav class="rail" class:rail--collapsed={collapsed} aria-label="Primary">
  <div class="rail__brand">
    <span class="rail__mark">
      <!-- λ is eigen's signature (the eigenvalue mark, shared with the TUI
           wordmark): spectrum-filled, with a slow living glow so the chrome
           reads as alive at rest. -->
      <span class="rail__lambda" aria-hidden="true">λ</span>
      {#if !collapsed}eigen{/if}
    </span>
    <!-- Collapse toggle: shrinks the rail to an icon-only strip and back. The
         chevron points the way it will move. -->
    <button
      class="rail__collapse"
      onclick={() => ui.toggleRail()}
      title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
      aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
      aria-pressed={collapsed}
    >{collapsed ? "»" : "«"}</button>
  </div>
  <div class="rail__scroll">
    {#each zones as zone (zone.name)}
      {@const folded = zoneIsFolded(zone)}
      <div class="rail__zone">
        {#if !collapsed}
          {#if zone.collapsible}
            <button
              class="rail__zone-label rail__zone-label--toggle"
              class:rail__zone-label--folded={folded}
              aria-expanded={!folded}
              onclick={() => toggleZone(zone)}
            >
              <span class="rail__zone-chev" aria-hidden="true">{folded ? "›" : "⌄"}</span>
              {zone.name}
              {#if folded}<span class="rail__zone-count tnum">{zone.items.length}</span>{/if}
            </button>
          {:else}
            <div class="rail__zone-label">{zone.name}</div>
          {/if}
        {/if}
        {#if !folded}
        {#each zone.items as item (item.route)}
          {@const active = router.route === item.route}
          {@const n = badge(item.route)}
          {@const live = liveRoutes.has(item.route) && n > 0}
          <button
            class="rail__item"
            class:rail__item--active={active}
            aria-current={active ? "page" : undefined}
            title={collapsed ? item.label : undefined}
            onclick={() => router.go(item.route)}
          >
            <span class="rail__edge" aria-hidden="true"></span>
            <span class="rail__glyph" aria-hidden="true">{item.glyph}</span>
            {#if !collapsed}<span class="rail__label">{item.label}</span>{/if}
            {#if n > 0}
              <span class="rail__badge tnum" class:rail__badge--live={live}>{n}</span>
            {/if}
          </button>
          <!-- RUNNING SESSIONS — a live sub-list under Chat so several active
               sessions can be navigated at once. Only when expanded + there are
               running sessions; the selected one (Chat's current param) is lit. -->
          {#if item.route === "chat" && !collapsed && chatRailSessions.length > 0}
            <div class="rail__subs" role="group" aria-label="Active and unread chats">
              {#each chatRailSessions as s (s.id)}
                {@const unread = sessionUnread.isUnread(s.id)}
                <button
                  class="rail__sub"
                  class:rail__sub--active={activeSession === s.id}
                  class:rail__sub--unread={unread}
                  title={shortTitle(s)}
                  onclick={() => router.go("chat", s.id)}
                >
                  <span
                    class="rail__sub-dot"
                    class:rail__sub-dot--approval={s.status === "approval"}
                    class:rail__sub-dot--unread={unread && s.status === "idle"}
                  ></span>
                  <span class="rail__sub-label">{shortTitle(s)}</span>
                  {#if unread}<span class="rail__sub-unread" aria-label="Unread reply">●</span>{/if}
                </button>
              {/each}
            </div>
          {/if}
        {/each}
        {/if}
      </div>
    {/each}
  </div>

  <!-- FOOTER — the rail's bottom bookend: engine status + version. Quiet by
       default; the dot carries the living color when online. -->
  <div class="rail__foot rail__foot--{footState}" title={collapsed ? `Daemon ${footState}${version ? ` · ${version}` : ""}` : `Daemon ${footState}`}>
    <span class="rail__foot-dot" aria-hidden="true"></span>
    {#if !collapsed}
      <span class="rail__foot-status">{footState}</span>
      {#if version}
        <span class="rail__foot-version tnum" class:rail__foot-version--mismatch={mismatch} title={versionTitle}>
          {version}{#if mismatch}<span class="rail__foot-warn" aria-hidden="true"> ⚠</span>{/if}
        </span>
      {/if}
    {/if}
  </div>
</nav>

<style>
  .rail {
    width: var(--rail-w);
    flex: none;
    background: var(--bg-well);
    border-right: 1px solid var(--border-hairline);
    display: flex;
    flex-direction: column;
    height: 100%;
    transition: width var(--dur-base) var(--ease-inout);
  }
  /* COLLAPSED — an icon-only strip just wide enough for the glyph column + its
     inset, so badges still sit at the row's right edge. */
  .rail--collapsed {
    width: var(--rail-w-collapsed, 60px);
  }

  /* BRAND — a single living teal dot beside the wordmark; optically
     centered with the rail items below by sharing their inset rhythm. The
     collapse toggle is pushed to the far edge. */
  .rail__brand {
    height: var(--topbar-h);
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: 0 var(--sp-5);
    border-bottom: 1px solid var(--border-hairline);
  }
  .rail__mark {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-3);
    min-width: 0;
    font: var(--fw-bold) var(--fs-h2) / 1 var(--font-display);
    color: var(--text-primary);
    letter-spacing: var(--ls-heading);
  }
  /* Collapse toggle: quiet ghost button, pushed to the rail's right edge when
     expanded; centered with the brand when collapsed. */
  .rail__collapse {
    margin-left: auto;
    flex: none;
    width: 24px;
    height: 24px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 1px solid transparent;
    border-radius: var(--r-sm);
    background: transparent;
    color: var(--text-faint);
    font-size: var(--fs-body);
    line-height: 1;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .rail__collapse:hover {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .rail__collapse:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .rail--collapsed .rail__brand {
    padding: 0;
    justify-content: center;
  }
  .rail--collapsed .rail__mark {
    display: none;
  }
  .rail--collapsed .rail__collapse {
    margin-left: 0;
  }
  /* λ — eigen's signature mark. The brand spectrum (teal→aqua→cyan→indigo) is
     clipped to the glyph so it shimmers like the TUI wordmark. STATIC — it no
     longer breathes at rest. An always-on idle animation (this + the footer
     dot) is the "the UI is never still" feel; reserve motion for transient,
     meaningful states (a running turn), not the brand mark sitting idle. */
  .rail__lambda {
    font: var(--fw-bold) calc(var(--fs-h2) + 2px) / 1 var(--font-display);
    background: var(--spectrum);
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
  }

  .rail__scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--sp-4) var(--sp-3) var(--sp-5);
  }

  /* ZONE RHYTHM — tighter than before: a 19-item rail with generous zone gaps
     scrolled and read as a control panel. Shorter gaps + label margins pack the
     nav so it fits without its own scrollbar. */
  .rail__zone + .rail__zone {
    margin-top: var(--sp-5);
  }
  .rail__zone-label {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    /* align label text to the glyph column, not the item padding edge */
    padding: 0 var(--sp-5);
    margin-bottom: var(--sp-2);
    user-select: none;
  }
  /* Collapsible zone header — a full-width clickable row with a chevron and,
     when folded, a count of the hidden items. */
  .rail__zone-label--toggle {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    border: none;
    background: transparent;
    text-align: left;
    cursor: pointer;
    transition: color var(--dur-fast) var(--ease-out);
  }
  .rail__zone-label--toggle:hover {
    color: var(--text-muted);
  }
  .rail__zone-chev {
    font-size: var(--fs-label);
    line-height: 1;
    color: var(--text-ghost);
  }
  .rail__zone-count {
    margin-left: auto;
    font-variant-numeric: tabular-nums;
    color: var(--text-ghost);
  }
  .rail__zone-label--toggle:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
    border-radius: var(--r-xs);
  }

  /* ITEM — fixed-height instrument row. The left edge, glyph, label and
     badge each own a column so everything stays optically aligned. */
  .rail__item {
    position: relative;
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    height: 30px;
    padding: 0 var(--sp-4) 0 var(--sp-5);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    border-radius: var(--r-sm);
    cursor: pointer;
    text-align: left;
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-sans);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .rail__item + .rail__item {
    margin-top: var(--sp-1);
  }

  /* HOVER — quiet wash in, slower fade-out so the cursor leaves a soft
     trail rather than a hard flicker between rows. */
  .rail__item:hover {
    background: var(--state-hover);
    color: var(--text-primary);
    transition-duration: var(--dur-instant);
  }
  .rail__item:active {
    background: var(--state-active);
  }
  .rail__item:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }

  /* ACTIVE — selected wash + bright teal label. Distinct from hover by
     the persistent left-edge and color, not just background weight. */
  .rail__item--active {
    background: var(--state-selected);
    color: var(--brand-bright);
  }
  .rail__item--active:hover {
    background: var(--state-selected);
  }
  .rail__item--active .rail__glyph {
    color: var(--brand);
    opacity: 1;
  }

  /* LEFT EDGE — its own element so it can spring-grow from the row's vertical
     center instead of just appearing. The bar is a soft teal gradient (bright
     at the top, settling below) and carries a faint glow so the active marker
     reads as a lit filament, not a flat tick. */
  .rail__edge {
    position: absolute;
    left: calc(-1 * var(--sp-4));
    top: 50%;
    width: 2px;
    height: var(--sp-6);
    border-radius: var(--r-full);
    background: linear-gradient(to bottom, var(--brand-bright), var(--brand));
    box-shadow: 0 0 6px -1px rgba(105, 194, 184, 0.5);
    transform: translateY(-50%) scaleY(0);
    transform-origin: center;
    opacity: 0;
    transition:
      transform var(--dur-base) var(--ease-spring),
      opacity var(--dur-fast) var(--ease-out);
  }
  .rail__item--active .rail__edge {
    transform: translateY(-50%) scaleY(1);
    opacity: 1;
  }

  /* GLYPH — fixed optical column; nudged up a hair so heavier marks sit
     on the label's visual baseline rather than its box center. */
  .rail__glyph {
    flex: none;
    width: 18px;
    text-align: center;
    font-size: var(--fs-body-sm);
    line-height: 1;
    color: var(--text-muted);
    opacity: 0.85;
    transition:
      color var(--dur-fast) var(--ease-out),
      opacity var(--dur-fast) var(--ease-out);
  }
  .rail__item:hover .rail__glyph {
    color: var(--text-secondary);
    opacity: 1;
  }
  .rail__label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* COLLAPSED ITEM — center the glyph in the narrow strip; the badge floats to
     the top-right corner as a compact marker (no label column to anchor it). */
  .rail--collapsed .rail__item {
    justify-content: center;
    padding: 0;
    gap: 0;
  }
  .rail--collapsed .rail__glyph {
    width: auto;
  }
  .rail--collapsed .rail__badge {
    position: absolute;
    top: 2px;
    right: 6px;
    min-width: 15px;
    height: 15px;
    padding: 0 4px;
    font-size: 9px;
  }

  /* RUNNING-SESSION SUB-LIST — indented under Chat, a quiet column of live
     sessions to hop between. Each row is a small dot (teal=working,
     warn=approval) + a truncated title; the open one is lit. */
  .rail__subs {
    display: flex;
    flex-direction: column;
    gap: 1px;
    margin: var(--sp-1) 0 var(--sp-2);
    padding-left: var(--sp-7);
  }
  .rail__sub {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    width: 100%;
    height: 26px;
    padding: 0 var(--sp-4);
    border: none;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--r-sm);
    cursor: pointer;
    text-align: left;
    font: var(--fw-regular) var(--fs-label) / 1 var(--font-sans);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .rail__sub:hover {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .rail__sub:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .rail__sub--active {
    background: var(--state-selected);
    color: var(--brand-bright);
  }
  .rail__sub-dot {
    flex: none;
    width: 6px;
    height: 6px;
    border-radius: var(--r-full);
    background: var(--brand);
    animation: rail-badge-breathe var(--breath) var(--ease-inout) infinite;
    will-change: opacity;
  }
  .rail__sub-dot--approval {
    background: var(--warn);
  }
  .rail__sub-dot--unread {
    background: var(--brand-bright);
    animation: none;
  }
  .rail__sub--unread .rail__sub-label {
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }
  .rail__sub-unread {
    flex: none;
    font-size: 8px;
    line-height: 1;
    color: var(--brand-bright);
    margin-left: auto;
  }
  .rail__sub-label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* BADGE — pill geometry: perfectly round at one digit, smoothly capsule
     beyond. Sits flush to the right edge, optically centered on the row. */
  .rail__badge {
    flex: none;
    min-width: 18px;
    height: 18px;
    padding: 0 var(--sp-3);
    box-sizing: border-box;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-overlay);
    color: var(--text-secondary);
    border-radius: var(--r-full);
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    letter-spacing: var(--ls-normal);
  }
  .rail__item:hover .rail__badge {
    background: var(--bg-overlay-2);
    color: var(--text-primary);
  }
  .rail__item--active .rail__badge {
    background: var(--brand-dim);
    color: var(--text-on-brand);
  }

  /* LIVE BADGE — counts that mean active work (running turns, bg tasks, live
     sessions, items to act on) go teal and breathe, drawing the eye to what
     is moving. A neutral tally (total sessions) keeps the quiet pill above. */
  .rail__badge--live {
    background: var(--state-selected);
    color: var(--brand-bright);
    box-shadow: inset 0 0 0 1px var(--border-brand-faint);
    animation: rail-badge-breathe var(--breath) var(--ease-inout) infinite;
    will-change: opacity;
  }
  .rail__item:hover .rail__badge--live {
    background: var(--state-selected);
    color: var(--brand-bright);
  }
  .rail__item--active .rail__badge--live {
    background: var(--brand-dim);
    color: var(--text-on-brand);
    box-shadow: none;
  }
  @keyframes rail-badge-breathe {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.62;
    }
  }

  /* FOOTER — the bottom bookend. A thin hairline lid, then a status dot +
     word + version, all whisper-quiet. The dot is the only color: teal and
     softly breathing when the engine is online, error when it has dropped. */
  .rail__foot {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    height: var(--sp-9);
    padding: 0 var(--sp-6);
    border-top: 1px solid var(--border-hairline);
    font: var(--fw-medium) var(--fs-micro) / 1 var(--font-sans);
    color: var(--text-ghost);
    user-select: none;
  }
  .rail__foot-dot {
    width: 6px;
    height: 6px;
    flex: 0 0 auto;
    border-radius: var(--r-full);
    background: var(--text-faint);
  }
  /* Online = static teal dot. Color alone signals the state; the dot no longer
     breathes at rest (idle ambient motion is the "never still" feel). */
  .rail__foot--online .rail__foot-dot {
    background: var(--brand);
  }
  .rail__foot--offline .rail__foot-dot {
    background: var(--error);
  }
  .rail__foot-status {
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }
  .rail__foot--online .rail__foot-status {
    color: var(--text-muted);
  }
  .rail__foot--offline .rail__foot-status {
    color: var(--error);
  }
  /* Version reads as a quiet build stamp — pushed to the far edge, ghosted. */
  .rail__foot-version {
    margin-left: auto;
    color: var(--text-faint);
    letter-spacing: var(--ls-normal);
  }
  /* A daemon/gui version mismatch is worth noticing — warn-tinted, not ghosted. */
  .rail__foot-version--mismatch {
    color: var(--warn);
  }
  .rail__foot-warn {
    color: var(--warn);
  }

  @media (prefers-reduced-motion: reduce) {
    .rail,
    .rail__item,
    .rail__edge,
    .rail__glyph,
    .rail__badge,
    .rail__sub {
      transition: none;
    }
    .rail__lambda,
    .rail__badge--live,
    .rail__sub-dot,
    .rail__foot--online .rail__foot-dot {
      animation: none;
    }
  }
</style>
