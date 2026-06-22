<script lang="ts">
  // Primary navigation. Zones group the rail by intent (Work / Knowledge /
  // System). The active item gets a teal left-edge + selected wash. Badges
  // surface live counts (sessions, running turns, background tasks).
  import { router, type Route } from "$lib/router.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";

  type Item = { route: Route; label: string; glyph: string };
  type Zone = { name: string; items: Item[] };

  const zones: Zone[] = [
    {
      name: "Work",
      items: [
        { route: "home", label: "Home", glyph: "◆" },
        { route: "chat", label: "Chat", glyph: "▶" },
        { route: "agents", label: "Agents", glyph: "⋔" },
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
        { route: "crons", label: "Crons", glyph: "◷" },
        { route: "plugins", label: "Plugins", glyph: "⊞" },
        { route: "config", label: "Config", glyph: "⚙" },
      ],
    },
  ];

  function badge(route: Route): number {
    if (route === "home") return sessions.count;
    if (route === "chat") return daemon.stats?.running_turns ?? 0;
    if (route === "agents") return daemon.stats?.bg_tasks ?? 0;
    return 0;
  }
</script>

<nav class="rail" aria-label="Primary">
  <div class="rail__brand">
    <span class="rail__mark">eigen</span>
  </div>
  <div class="rail__scroll">
    {#each zones as zone (zone.name)}
      <div class="rail__zone">
        <div class="rail__zone-label">{zone.name}</div>
        {#each zone.items as item (item.route)}
          {@const active = router.route === item.route}
          {@const n = badge(item.route)}
          <button
            class="rail__item"
            class:rail__item--active={active}
            aria-current={active ? "page" : undefined}
            onclick={() => router.go(item.route)}
          >
            <span class="rail__glyph" aria-hidden="true">{item.glyph}</span>
            <span class="rail__label">{item.label}</span>
            {#if n > 0}<span class="rail__badge tnum">{n}</span>{/if}
          </button>
        {/each}
      </div>
    {/each}
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
  .rail__brand {
    height: var(--topbar-h);
    display: flex;
    align-items: center;
    padding: 0 var(--sp-6);
    border-bottom: 1px solid var(--border-hairline);
  }
  .rail__mark {
    font: var(--fw-bold) var(--fs-h2) / 1 var(--font-display);
    color: var(--brand);
    letter-spacing: var(--ls-heading);
  }
  .rail__scroll {
    flex: 1;
    overflow-y: auto;
    padding: var(--sp-5) var(--sp-4);
  }
  .rail__zone + .rail__zone {
    margin-top: var(--sp-6);
  }
  .rail__zone-label {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    padding: 0 var(--sp-4);
    margin-bottom: var(--sp-3);
  }
  .rail__item {
    position: relative;
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    height: 34px;
    padding: 0 var(--sp-4);
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
  .rail__item:hover {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .rail__item:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .rail__item--active {
    background: var(--state-selected);
    color: var(--brand-bright);
  }
  .rail__item--active::before {
    content: "";
    position: absolute;
    left: -4px;
    top: 7px;
    bottom: 7px;
    width: 2px;
    border-radius: var(--r-full);
    background: var(--brand);
  }
  .rail__glyph {
    width: 16px;
    text-align: center;
    font-size: 13px;
    opacity: 0.9;
  }
  .rail__label {
    flex: 1;
  }
  .rail__badge {
    min-width: 18px;
    height: 18px;
    padding: 0 var(--sp-2);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-overlay);
    color: var(--text-secondary);
    border-radius: var(--r-full);
    font-size: var(--fs-micro);
    font-weight: var(--fw-semibold);
  }
  .rail__item--active .rail__badge {
    background: var(--brand-dim);
    color: var(--text-on-brand);
  }
</style>
