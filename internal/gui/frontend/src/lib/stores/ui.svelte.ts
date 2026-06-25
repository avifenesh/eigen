// UI chrome state that outlives a single view — currently the left rail's
// collapsed/expanded mode. Kept in its own tiny store (not inside Rail) so the
// preference persists across reloads via localStorage and any component can
// read it. localStorage access is guarded: the webview always has it, but a
// hostile/again-headless context shouldn't throw on read.
const RAIL_KEY = "eigen.railCollapsed";

function readBool(key: string, fallback: boolean): boolean {
  try {
    const v = localStorage.getItem(key);
    return v == null ? fallback : v === "1";
  } catch {
    return fallback;
  }
}
function writeBool(key: string, val: boolean) {
  try {
    localStorage.setItem(key, val ? "1" : "0");
  } catch {
    /* private mode / no storage — preference just won't persist */
  }
}

function createUI() {
  let railCollapsed = $state(readBool(RAIL_KEY, false));

  return {
    get railCollapsed() {
      return railCollapsed;
    },
    toggleRail() {
      railCollapsed = !railCollapsed;
      writeBool(RAIL_KEY, railCollapsed);
    },
    setRailCollapsed(v: boolean) {
      railCollapsed = v;
      writeBool(RAIL_KEY, v);
    },
  };
}

export const ui = createUI();
