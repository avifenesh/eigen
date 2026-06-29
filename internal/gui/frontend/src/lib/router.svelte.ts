// Hash router (webview-safe — no history API quirks under file://-style asset
// serving). Routes map 1:1 to the rail. Reactive via a $state snapshot.

export const routes = [
  "home",
  "chat",
  "tasks",
  "live",
  "sessions",
  "board",
  "memory",
  "notes",
  "dreaming",
  "skills",
  "reviewers",
  "observe",
  "routing",
  "machines",
  "profile",
  "crons",
  "plugins",
  "connectors",
  "config",
] as const;
export type Route = (typeof routes)[number];

function parse(): { route: Route; param?: string } {
  const h = location.hash.replace(/^#\/?/, "");
  const [r, p] = h.split("/");
  if (r === "agents") return { route: "tasks", param: p || undefined };
  return { route: (routes as readonly string[]).includes(r) ? (r as Route) : "home", param: p || undefined };
}

function createRouter() {
  let current = $state(parse());
  // Reflect external hash changes (browser back/forward, deep-links) into
  // state. NOTE: setting location.hash from go() is NOT enough on its own —
  // under the embedded wails:// custom URI scheme WebKitGTK may not dispatch
  // a hashchange for a scripted fragment change, so go() also updates `current`
  // directly. This listener stays for the genuinely external cases.
  addEventListener("hashchange", () => {
    const next = parse();
    if (next.route !== current.route || next.param !== current.param) {
      current = next;
    }
  });
  return {
    get route() {
      return current.route;
    },
    get param() {
      return current.param;
    },
    go(route: Route, param?: string) {
      const next = { route, param };
      // Update state immediately so the view swaps without waiting on an event
      // round-trip that may never arrive under the webview's custom scheme.
      current = next;
      // Sync the URL fragment (best-effort; no-op if already there). Skipped
      // when the new fragment equals the current one so we don't push a
      // redundant history entry.
      const frag = param ? `#/${route}/${param}` : `#/${route}`;
      if (location.hash !== frag) location.hash = frag;
    },
  };
}

export const router = createRouter();
