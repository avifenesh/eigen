<script lang="ts">
  // The top chrome: current page title + live daemon health. Quiet by design —
  // it states where you are and whether the engine is breathing. Nothing here
  // competes for attention; the single moving part is the health dot when the
  // connection is settling. An optional `actions` snippet lets a view hang
  // right-aligned controls off the bar without this component owning them.
  import type { Snippet } from "svelte";
  import { router } from "$lib/router.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";
  import StatusDot from "./StatusDot.svelte";

  let { actions }: { actions?: Snippet } = $props();

  // The masthead title names where you are. On the Chat route a routed session
  // id should read as that session's title, not the generic "Chat" word — so
  // the bar tells you which conversation you're in. Falls back to "Chat" while
  // the list is still loading or the id is unknown; every other route is just
  // its (capitalized) name.
  const title = $derived.by(() => {
    if (router.route === "chat" && router.param) {
      const sess = sessions.list.find((s) => s.id === router.param);
      return sess?.title || "Chat";
    }
    return router.route;
  });
  // A resolved session title is free-form text and should keep its own casing;
  // only route names earn the masthead's word-initial capitalization.
  const verbatimTitle = $derived(
    router.route === "chat" && router.param
      ? !!sessions.list.find((s) => s.id === router.param)?.title
      : false,
  );

  // Status copy and dot state are derived straight from the daemon store so the
  // bar mirrors the connection without any local bookkeeping.
  const statusLabel = $derived(
    daemon.status === "online" ? "online" : daemon.status === "offline" ? "offline" : "connecting",
  );
  const dotState = $derived<"ok" | "error" | "idle">(
    daemon.status === "online" ? "ok" : daemon.status === "offline" ? "error" : "idle",
  );
  const connecting = $derived(daemon.status === "connecting");

  // The bar's one living signal: how many turns the engine is driving right
  // now. When > 0, a teal "running" capsule breathes in beside health so the
  // top band reflects work in flight without the eye hunting for it.
  const running = $derived(daemon.stats?.running_turns ?? 0);
</script>

<header class="topbar">
  <!-- Single-line page title. The "Eigen" eyebrow used to sit above it, but the
       rail already shows the λ eigen wordmark at the same top-left corner — the
       eyebrow was pure masthead decoration stuttering the brand. One line also
       lets --topbar-h stay short. -->
  <div class="topbar__heading">
    <h1 class="topbar__title" class:topbar__title--verbatim={verbatimTitle}>{title}</h1>
  </div>

  <div class="topbar__spacer"></div>

  {#if actions}
    <div class="topbar__actions">{@render actions()}</div>
  {/if}

  {#if running > 0}
    <div
      class="topbar__running"
      role="status"
      aria-live="polite"
      title={`${running} ${running === 1 ? "turn" : "turns"} running`}
    >
      <span class="topbar__running-pulse" aria-hidden="true"></span>
      <span class="topbar__running-count tnum">{running}</span>
      <span class="topbar__running-word">running</span>
    </div>
  {/if}

  <div
    class="topbar__health topbar__health--{dotState}"
    role="status"
    aria-live="polite"
    title={`Daemon ${statusLabel}`}
  >
    <StatusDot state={dotState} pulse={connecting} />
    <span class="topbar__health-label">{statusLabel}</span>
  </div>
</header>

<style>
  .topbar {
    height: var(--topbar-h);
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-5);
    /* --sp-7 (20px) so the page-title left edge lines up with the content
       gutter every view uses (was --sp-6/16px — the title sat 4px left of the
       content beneath it). */
    padding: 0 var(--sp-7);
    /* Flat fill + a single hairline seam. The bar is flush chrome, not a
       floating surface — the old gradient + 16px-blur drop shadow was pure
       weight on an always-visible band. */
    background: var(--bg-base);
    border-bottom: 1px solid var(--border-hairline);
  }

  /* TITLE — a single line naming the current page (the rail carries the brand). */
  .topbar__heading {
    display: flex;
    align-items: center;
    min-width: 0;
  }
  .topbar__title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-h3) / 1 var(--font-display);
    color: var(--text-primary);
    text-transform: capitalize;
    letter-spacing: var(--ls-heading);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* A resolved session title keeps its own casing — capitalize is for the
     one-word route names, not free-form titles. */
  .topbar__title--verbatim {
    text-transform: none;
  }

  .topbar__spacer {
    flex: 1;
    min-width: var(--sp-5);
  }

  .topbar__actions {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }

  /* RUNNING SIGNAL — the bar's one alive surface. A teal capsule that only
     exists while turns are in flight: a soft pulsing core, the count, then a
     quiet word. Teal here means "the engine is doing something right now." */
  .topbar__running {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-3);
    height: var(--sp-8);
    padding: 0 var(--sp-5) 0 var(--sp-4);
    border: 1px solid var(--border-brand-faint);
    border-radius: var(--r-full);
    background: var(--state-selected);
    color: var(--brand-bright);
  }
  /* Opacity-only pulse — composites, no per-frame repaint. The old version
     animated an expanding box-shadow ring (0→4px), which WebKitGTK repaints
     every frame on the main thread; this bar is persistent and the pulse runs
     the whole time ANY turn is active, so it competed with streaming/scroll
     exactly when it mattered. A soft opacity breath reads the same. */
  .topbar__running-pulse {
    width: 7px;
    height: 7px;
    flex: 0 0 auto;
    border-radius: var(--r-full);
    background: var(--brand);
    animation: topbar-run-pulse var(--breath) var(--ease-inout) infinite;
    will-change: opacity;
  }
  @keyframes topbar-run-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.5;
    }
  }
  .topbar__running-count {
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    color: var(--brand-bright);
  }
  .topbar__running-word {
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
    letter-spacing: var(--ls-normal);
    color: var(--brand);
  }

  /* HEALTH CLUSTER — dot + word, wrapped in a faint pill so it reads as a
     single quiet status object. The label tint shifts with the connection
     state but stays muted; the dot carries the actual color signal. */
  .topbar__health {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-3);
    height: var(--sp-8);
    padding: 0 var(--sp-4);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-full);
    background: var(--bg-raised);
    transition: border-color var(--dur-base) var(--ease-out);
  }
  .topbar__health-label {
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
    letter-spacing: var(--ls-normal);
    color: var(--text-muted);
    transition: color var(--dur-base) var(--ease-out);
  }
  .topbar__health--ok .topbar__health-label {
    color: var(--text-secondary);
  }
  .topbar__health--error {
    border-color: var(--error-bg);
    background: var(--error-bg);
  }
  .topbar__health--error .topbar__health-label {
    color: var(--error);
  }
  .topbar__health--idle .topbar__health-label {
    color: var(--text-muted);
  }

  @media (prefers-reduced-motion: reduce) {
    .topbar__health,
    .topbar__health-label {
      transition: none;
    }
    /* Hold the running signal lit but static — it still reads as alive. */
    .topbar__running-pulse {
      animation: none;
      box-shadow: 0 0 0 3px rgba(105, 194, 184, 0.18);
    }
  }
</style>
