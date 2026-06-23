<script lang="ts">
  // The top chrome: current page title + live daemon health. Quiet by design —
  // it states where you are and whether the engine is breathing. Nothing here
  // competes for attention; the single moving part is the health dot when the
  // connection is settling. An optional `actions` snippet lets a view hang
  // right-aligned controls off the bar without this component owning them.
  import type { Snippet } from "svelte";
  import { router } from "$lib/router.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import StatusDot from "./StatusDot.svelte";

  let { actions }: { actions?: Snippet } = $props();

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
  <div class="topbar__heading">
    <span class="topbar__eyebrow">Eigen</span>
    <h1 class="topbar__title">{router.route}</h1>
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
    padding: 0 var(--sp-7);
    /* A faint top-down lift so the bar reads as the one owned top band — its
       weight tapers into the content below rather than floating on a flat
       fill. The hairline + shadow seat it as a single edge, not a stack. */
    background: linear-gradient(to bottom, var(--bg-raised) 0%, var(--bg-base) 100%);
    border-bottom: 1px solid var(--border-hairline);
    box-shadow:
      0 1px 0 0 var(--divider),
      0 6px 16px -10px rgba(0, 0, 0, 0.55);
  }

  /* TITLE — an eyebrow wordmark sits flush above the page name, giving the
     bar a considered masthead feel rather than a lone capitalized word. The
     eyebrow and title share a 2px-stepped baseline so the cluster reads as
     one masthead block, tightly bound. */
  .topbar__heading {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: var(--sp-1);
    min-width: 0;
  }
  .topbar__eyebrow {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-ghost);
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
  .topbar__running-pulse {
    width: 7px;
    height: 7px;
    flex: 0 0 auto;
    border-radius: var(--r-full);
    background: var(--brand);
    box-shadow: 0 0 0 0 var(--brand);
    animation: topbar-run-pulse var(--breath) var(--ease-inout) infinite;
    will-change: opacity, box-shadow;
  }
  @keyframes topbar-run-pulse {
    0%,
    100% {
      opacity: 1;
      box-shadow: 0 0 0 0 rgba(105, 194, 184, 0.35);
    }
    50% {
      opacity: 0.6;
      box-shadow: 0 0 0 4px rgba(105, 194, 184, 0);
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
