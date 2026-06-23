// Small reusable Svelte actions.

// trapFocus: when a dialog/slide-over mounts, move focus into it and contain
// Tab within it (so aria-modal is honored and a keyboard user can't tab back
// into the obscured page). Restores focus to the previously-focused element on
// teardown. Use on the panel root: <div use:trapFocus role="dialog" ...>.
export function trapFocus(node: HTMLElement) {
  const previous = document.activeElement as HTMLElement | null;

  function focusables(): HTMLElement[] {
    return Array.from(
      node.querySelectorAll<HTMLElement>(
        'a[href],button:not([disabled]),textarea,input,select,[tabindex]:not([tabindex="-1"])',
      ),
    ).filter((el) => el.offsetParent !== null);
  }

  // Move initial focus into the panel (first focusable, else the panel itself).
  const first = focusables()[0];
  (first ?? node).focus();

  function onKey(e: KeyboardEvent) {
    if (e.key !== "Tab") return;
    const items = focusables();
    if (items.length === 0) {
      e.preventDefault();
      return;
    }
    const head = items[0];
    const tail = items[items.length - 1];
    const active = document.activeElement;
    if (e.shiftKey && active === head) {
      e.preventDefault();
      tail.focus();
    } else if (!e.shiftKey && active === tail) {
      e.preventDefault();
      head.focus();
    }
  }

  node.addEventListener("keydown", onKey);
  return {
    destroy() {
      node.removeEventListener("keydown", onKey);
      previous?.focus?.();
    },
  };
}
