// Toast store — transient feedback (success/error/info/working). Most kinds
// auto-dismiss after TTL; "working" persists (no timer) until dismissed or
// evicted. Bounded queue so a burst can't grow unbounded.
export type ToastKind = "success" | "error" | "info" | "working";
export type Toast = { id: number; kind: ToastKind; text: string };

const MAX = 5;
const TTL = 4200;

function createToasts() {
  let items = $state<Toast[]>([]);
  let seq = 0;
  const timers = new Map<number, ReturnType<typeof setTimeout>>();

  function dismiss(id: number) {
    const t = timers.get(id);
    if (t) {
      clearTimeout(t);
      timers.delete(id);
    }
    items = items.filter((i) => i.id !== id);
  }

  function push(kind: ToastKind, text: string) {
    const id = ++seq;
    items.push({ id, kind, text });
    if (items.length > MAX) {
      const dropped = items.shift();
      if (dropped) {
        const t = timers.get(dropped.id);
        if (t) clearTimeout(t);
        timers.delete(dropped.id);
      }
    }
    // "working" tracks a long-running op: it has no fixed lifetime, so it skips
    // the TTL timer and persists until explicitly dismissed (or evicted by MAX).
    if (kind !== "working") {
      timers.set(
        id,
        setTimeout(() => dismiss(id), TTL),
      );
    }
    return id;
  }

  return {
    get items() {
      return items;
    },
    push,
    dismiss,
    success: (t: string) => push("success", t),
    error: (t: string) => push("error", t),
    info: (t: string) => push("info", t),
    working: (t: string) => push("working", t),
  };
}

export const toasts = createToasts();
