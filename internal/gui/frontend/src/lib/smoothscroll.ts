// Smooth mouse-wheel scrolling for the desktop webview.
//
// WebKitGTK ships WebKitSettings:enable-smooth-scrolling = FALSE and Wails v3
// exposes no hook to flip it (NativeWindow() hands back the GtkWindow, not the
// WebKitWebView, and there's no settings callback). So a mouse wheel scrolls in
// discrete notch jumps with libinput acceleration stacked on top — the "slow to
// fast in jumps instead of smooth changes" feel. CSS scroll-behavior only
// affects scripted/anchor scrolls, never the wheel, so it can't fix this.
//
// This shim intercepts a mouse wheel and animates the nearest scrollable
// ancestor's scrollTop toward an accumulated target with rAF easing, so a notch
// glides instead of teleporting. It is deliberately conservative:
//   - Trackpads already deliver smooth high-frequency pixel deltas; those are
//     detected and left on the native path (re-animating them adds lag/rubber-
//     band).
//   - prefers-reduced-motion users are left fully native.
//   - At a scroll boundary the event isn't swallowed, so parent scroll / native
//     overscroll still works.
// Engine-independent: works the same on the gtk3/webkit-4.1 and gtk4/webkit-6.0
// builds, since it never touches the native webview.

// Per-element scroll target + an in-flight flag, keyed weakly so detached nodes
// (e.g. VirtualList rows, route swaps) are GC'd without bookkeeping.
const target = new WeakMap<HTMLElement, number>();
const animating = new WeakSet<HTMLElement>();

// Per-frame lerp toward the target (higher = snappier, lower = floatier) and the
// px threshold at which we snap to the goal and stop. 0.25 converges a notch in
// ~300ms — smooth without feeling laggy. Tunable.
const EASE = 0.25;
const MIN_STEP = 0.5;

// The nearest ancestor that actually scrolls vertically, starting from the wheel
// event's target. Walks to (not past) the document root.
function scrollableAncestor(node: EventTarget | null): HTMLElement | null {
  let el = node instanceof HTMLElement ? node : null;
  while (el && el !== document.body && el !== document.documentElement) {
    if (el.scrollHeight > el.clientHeight) {
      const oy = getComputedStyle(el).overflowY;
      if (oy === "auto" || oy === "scroll") return el;
    }
    el = el.parentElement;
  }
  return null;
}

function animate(el: HTMLElement): void {
  if (animating.has(el)) return; // a single rAF loop per element drains its target
  animating.add(el);
  const step = () => {
    const goal = target.get(el);
    if (goal === undefined) {
      animating.delete(el);
      return;
    }
    const cur = el.scrollTop;
    const diff = goal - cur;
    if (Math.abs(diff) <= MIN_STEP) {
      el.scrollTop = goal;
      target.delete(el);
      animating.delete(el);
      return;
    }
    el.scrollTop = cur + diff * EASE;
    requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
}

function onWheel(e: WheelEvent): void {
  if (e.ctrlKey) return; // pinch / zoom gesture — not a scroll
  // Trackpad / precise scroll: pixel-mode, small magnitude, often fractional or
  // with a horizontal component — already smooth, so leave it native. A mouse
  // wheel reports line/page mode, or pixel-mode in big integer notches.
  const isMouseWheel =
    e.deltaMode !== 0 || (e.deltaX === 0 && Math.abs(e.deltaY) >= 40 && Number.isInteger(e.deltaY));
  if (!isMouseWheel) return;

  const el = scrollableAncestor(e.target);
  if (!el) return;
  // Chat transcript (VirtualList): JS wheel easing fights rAF-batched scroll +
  // row remeasure — feels laggy/rubber-bandy. Native scroll keeps WebKitGTK async.
  if (el.classList.contains("vlist") || el.dataset.nativeScroll === "1") return;

  // Normalize line/page deltas to pixels (pixel mode passes through).
  let dy = e.deltaY;
  if (e.deltaMode === 1) dy *= 16; // lines → px (approx line height)
  else if (e.deltaMode === 2) dy *= el.clientHeight; // pages → px

  const max = el.scrollHeight - el.clientHeight;
  // Accumulate onto any in-flight target so a fast spin keeps gliding forward
  // instead of restarting from the current (lagging) scrollTop each notch.
  const base = target.has(el) ? (target.get(el) as number) : el.scrollTop;
  const next = Math.max(0, Math.min(max, base + dy));
  // At a boundary with nowhere to go: don't swallow the event — let the parent
  // (or native overscroll) handle it.
  if (next === el.scrollTop && (next <= 0 || next >= max)) return;

  e.preventDefault();
  target.set(el, next);
  animate(el);
}

// Install the shim; returns a teardown. No-op (native scroll) under SSR or
// reduced-motion. Listener is non-passive so it can preventDefault the notch.
export function installSmoothScroll(): () => void {
  if (typeof window === "undefined") return () => {};
  if (typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches) {
    return () => {};
  }
  window.addEventListener("wheel", onWheel, { passive: false });
  return () => window.removeEventListener("wheel", onWheel);
}
