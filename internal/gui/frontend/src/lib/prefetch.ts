// Hover-intent prefetch for the Rail nav. The router's {#if} chain fully
// unmounts/remounts a view on every nav switch (no keep-alive), so without
// this, the first thing a click does is start the fetch the user is about to
// wait on. Firing the same fetch on hover-intent — same delay window Tooltip
// uses, so it only fires on a deliberate aim, not a cursor sweep — means by
// the time the click lands the data is often already in viewCache (or close
// to it), and the view's own load() picks up the in-flight/cached result via
// the same cache key instead of double-fetching.
import { Bridge } from "$lib/bridge";
import { viewCache } from "$lib/stores/viewCache.svelte";
import type { Route } from "$lib/router.svelte";

// minAge guards against re-spawning the underlying work (systemctl subprocess
// fan-out, disk scans) on every hover flicker across a route's lifetime —
// a hover within this window of the last fetch (prefetch OR real load) just
// reuses what's cached rather than firing again.
const MIN_AGE_MS = 3000;

const PREFETCHERS: Partial<Record<Route, () => unknown>> = {
  crons: () => viewCache.fetch("crons", () => Bridge.Crons(), { minAge: MIN_AGE_MS }),
  skills: () => viewCache.fetch("skills", () => Bridge.Skills(), { minAge: MIN_AGE_MS }),
  plugins: () => viewCache.fetch("plugins", () => Bridge.Plugins(), { minAge: MIN_AGE_MS }),
  connectors: () =>
    viewCache.fetch(
      "connectors",
      async () => {
        const [c, s, g] = await Promise.all([Bridge.Connectors(), Bridge.MCPServers(), Bridge.GoogleStatus()]);
        return { c, s, g };
      },
      { minAge: MIN_AGE_MS },
    ),
  // Memory's per-scope data needs a scope key the rail doesn't have, so only
  // the scope list (the part every Memory mount fetches regardless of which
  // scope opens) is worth warming here.
  memory: () => viewCache.fetch("memory:scopes", () => Bridge.ListMemoryScopes(), { minAge: MIN_AGE_MS }),
};

export function prefetch(route: Route): void {
  PREFETCHERS[route]?.();
}
