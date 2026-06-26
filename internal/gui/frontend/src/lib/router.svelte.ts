// Hash router (webview-safe — no history API quirks under file://-style asset
// serving). Routes map 1:1 to the rail. Reactive via a $state snapshot.

export const routes = [
  "home",
  "chat",
  "agents",
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
  return { route: (routes as readonly string[]).includes(r) ? (r as Route) : "home", param: p || undefined };
}

function createRouter() {
  let current = $state(parse());
  addEventListener("hashchange", () => {
    current = parse();
  });
  return {
    get route() {
      return current.route;
    },
    get param() {
      return current.param;
    },
    go(route: Route, param?: string) {
      location.hash = param ? `#/${route}/${param}` : `#/${route}`;
    },
  };
}

export const router = createRouter();
