<script lang="ts">
  // The top chrome: current page title + live daemon health. Quiet by design —
  // it states where you are and whether the engine is breathing.
  import { router } from "$lib/router.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import StatusDot from "./StatusDot.svelte";

  const statusLabel = $derived(
    daemon.status === "online" ? "daemon online" : daemon.status === "offline" ? "daemon offline" : "connecting…",
  );
  const statusState = $derived(
    daemon.status === "online" ? "ok" : daemon.status === "offline" ? "error" : "idle",
  );
</script>

<header class="topbar">
  <div class="topbar__title">{router.route}</div>
  <div class="topbar__spacer"></div>
  <div class="topbar__health" title={statusLabel}>
    <StatusDot state={statusState as "ok" | "error" | "idle"} pulse={daemon.status === "connecting"} />
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
    border-bottom: 1px solid var(--border-hairline);
    background: var(--bg-base);
  }
  .topbar__title {
    font: var(--fw-semibold) var(--fs-h3) / 1 var(--font-display);
    color: var(--text-primary);
    text-transform: capitalize;
    letter-spacing: var(--ls-heading);
  }
  .topbar__spacer {
    flex: 1;
  }
  .topbar__health {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
</style>
