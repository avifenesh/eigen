<script lang="ts">
  // Primary navigation. Zones group the rail by intent (Work / Knowledge /
  // System). The active item gets a teal left-edge + selected wash. Badges
  // surface live counts (sessions, running turns, background tasks).
  import { router, type Route } from "$lib/router.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { feed } from "$lib/stores/feed.svelte";

  type Item = { route: Route; label: string; glyph: string };
  type Zone = { name: string; items: Item[] };

  const zones: Zone[] = [
    {
      name: "Work",
      items: [
        { route: "home", label: "Home", glyph: "◆" },
        { route: "chat", label: "Chat", glyph: "▶" },
        { route: "agents", label: "Agents", glyph: "⋔" },
        { route: "live", label: "Live", glyph: "◐" },
        { route: "sessions", label: "Sessions", glyph: "≡" },
      ],
    },
    {
      name: "Knowledge",
      items: [
        { route: "memory", label: "Memory", glyph: "❖" },
        { route: "dreaming", label: "Dreaming", glyph: "☾" },
        { route: "skills", label: "Skills", glyph: "✦" },
      ],
    },
    {
      name: "System",
      items: [
        { route: "observe", label: "Observe", glyph: "◉" },
        { route: "routing", label: "Routing", glyph: "⇄" },
        { route: "machines", label: "Machines", glyph: "⊟" },
        { route: "crons", label: "Crons", glyph: "◷" },
        { route: "plugins", label: "Plugins", glyph: "⊞" },
        { route: "profile", label: "Profile", glyph: "◑" },
        { route: "config", label: "Config", glyph: "⚙" },
      ],
    },
  ];

  // Home surfaces the proactive-feed "act on" count (what needs attention),
  // not the raw session total — the rail nudges toward action.
  function badge(route: Route): number {
    if (route === "home") return feed.actOn.length;
    if (route === "chat") return daemon.stats?.running_turns ?? 0;
    if (route === "agents") return daemon.stats?.bg_tasks ?? 0;
    if (route === "live") return sessions.list.filter((s) => s.status === "working" || s.status === "approval").length;
    if (route === "sessions") return sessions.count;
    return 0;
  }

  // These routes count *active work* — when their badge is non-zero the count
  // is teal and breathing, so the eye is drawn to what is running/fresh. Other
  // badges (e.g. total sessions) stay neutral: a tally, not a live signal.
  const liveRoutes = new Set<Route>(["home", "chat", "agents", "live"]);

  // Rail footer mirrors the daemon connection so the chrome bookends: brand at
  // the top, engine status at the bottom. Version is shown only when known.
  const online = $derived(daemon.status === "online");
  const offline = $derived(daemon.status === "offline");
  const footState = $derived(online ? "online" : offline ? "offline" : "connecting");
  const version = $derived(daemon.stats?.version ?? "");
</script>

<nav class="rail" aria-label="Primary">
  <div class="rail__brand">
    <span class="rail__mark">
      <span class="rail__mark-dot" aria-hidden="true"></span>
      eigen
    </span>
  </div>
  <div class="rail__scroll">
    {#each zones as zone (zone.name)}
      <div class="rail__zone">
        <div class="rail__zone-label">{zone.name}</div>
        {#each zone.items as item (item.route)}
          {@const active = router.route === item.route}
          {@const n = badge(item.route)}
          {@const live = liveRoutes.has(item.route) && n > 0}
          <button
            class="rail__item"
            class:rail__item--active={active}
            aria-current={active ? "page" : undefined}
            onclick={() => router.go(item.route)}
          >
            <span class="rail__edge" aria-hidden="true"></span>
            <span class="rail__glyph" aria-hidden="true">{item.glyph}</span>
            <span class="rail__label">{item.label}</span>
            {#if n > 0}
              <span class="rail__badge tnum" class:rail__badge--live={live}>{n}</span>
            {/if}
          </button>
        {/each}
      </div>
    {/each}
  </div>

  <!-- FOOTER — the rail's bottom bookend: engine status + version. Quiet by
       default; the dot carries the living color when online. -->
  <div class="rail__foot rail__foot--{footState}" title={`Daemon ${footState}`}>
    <span class="rail__foot-dot" aria-hidden="true"></span>
    <span class="rail__foot-status">{footState}</span>
    {#if version}<span class="rail__foot-version tnum">{version}</span>{/if}
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
  }

  /* BRAND — a single living teal dot beside the wordmark; optically
     centered with the rail items below by sharing their inset rhythm. */
  .rail__brand {
    height: var(--topbar-h);
    flex: none;
    display: flex;
    align-items: center;
    padding: 0 var(--sp-6);
    border-bottom: 1px solid var(--border-hairline);
  }
  .rail__mark {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-3);
    font: var(--fw-bold) var(--fs-h2) / 1 var(--font-display);
    color: var(--text-primary);
    letter-spacing: var(--ls-heading);
  }
  .rail__mark-dot {
    width: 7px;
    height: 7px;
    border-radius: var(--r-full);
    background: var(--brand);
    /* A living halo that breathes slowly — the brand mark is the rail's
       heartbeat, the one accent that proves the chrome is alive even at rest. */
    box-shadow: 0 0 0 3px var(--state-selected);
    animation: rail-mark-breathe var(--breath) var(--ease-inout) infinite;
    will-change: box-shadow;
  }
  @keyframes rail-mark-breathe {
    0%,
    100% {
      box-shadow: 0 0 0 2px var(--state-selected);
    }
    50% {
      box-shadow: 0 0 0 4px rgba(105, 194, 184, 0.16);
    }
  }

  .rail__scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--sp-5) var(--sp-4) var(--sp-6);
  }

  /* ZONE RHYTHM — labels breathe above their group; generous gap between
     zones, tight binding within. */
  .rail__zone + .rail__zone {
    margin-top: var(--sp-7);
  }
  .rail__zone-label {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    /* align label text to the glyph column, not the item padding edge */
    padding: 0 var(--sp-5);
    margin-bottom: var(--sp-4);
    user-select: none;
  }

  /* ITEM — fixed-height instrument row. The left edge, glyph, label and
     badge each own a column so everything stays optically aligned. */
  .rail__item {
    position: relative;
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    height: 34px;
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
  .rail__foot--online .rail__foot-dot {
    background: var(--brand);
    animation: rail-foot-breathe var(--breath) var(--ease-inout) infinite;
    will-change: opacity;
  }
  .rail__foot--offline .rail__foot-dot {
    background: var(--error);
  }
  @keyframes rail-foot-breathe {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.45;
    }
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

  @media (prefers-reduced-motion: reduce) {
    .rail__item,
    .rail__edge,
    .rail__glyph,
    .rail__badge {
      transition: none;
    }
    .rail__mark-dot,
    .rail__badge--live,
    .rail__foot--online .rail__foot-dot {
      animation: none;
    }
  }
</style>
