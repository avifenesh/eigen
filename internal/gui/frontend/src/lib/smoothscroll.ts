// Mouse-wheel scrolling for the desktop webview — NATIVE ONLY.
//
// History: this module used to intercept the wheel (preventDefault on a
// non-passive listener) and animate the scrollable ancestor's scrollTop toward
// an accumulated target with rAF easing, to turn WebKitGTK's discrete notch
// jumps into a glide. In practice that FOUGHT the engine: writing scrollTop
// from JS forces WebKitGTK off its threaded/async compositor scroll onto
// main-thread scroll + synchronous layout every frame, and the 0.25 lerp made a
// single notch take ~300ms to settle. The result read as "scrolling is not
// smooth, bulky and slow" — the exact complaint it was meant to fix. Native
// WebKitGTK wheel scroll, even in notch steps, is snappier and stays on the
// compositor.
//
// So installSmoothScroll is now a no-op: every surface uses native scroll. The
// export is kept so App.svelte's onMount wiring is unchanged; revive JS easing
// only behind an explicit opt-in setting if notch-jumpiness ever proves worse
// than the main-thread cost it caused.
export function installSmoothScroll(): () => void {
  return () => {};
}
