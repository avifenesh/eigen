<script lang="ts">
  // Keyboard cheatsheet (?). A small overlay listing the app's global shortcuts,
  // so the keyboard-first audience can discover them. Toggled by "?" (when not
  // typing in a field) and closed by Escape / scrim. Window listener torn down
  // in the $effect cleanup.
  import { trapFocus } from "$lib/actions";

  let open = $state(false);

  const rows: { keys: string[]; desc: string }[] = [
    { keys: ["⌘/Ctrl", "K"], desc: "Command palette — run an action, jump anywhere" },
    { keys: ["?"], desc: "This shortcut sheet" },
    { keys: ["Esc"], desc: "Close palette / dialog / sheet" },
    { keys: ["↑", "↓"], desc: "Move within the palette / lists" },
    { keys: ["Enter"], desc: "Open selection (palette / lists)" },
    { keys: ["Enter"], desc: "Send the message (in Chat)" },
    { keys: ["Shift", "Enter"], desc: "Newline in the composer" },
  ];

  // The palette (⌘/Ctrl+K) is where the verbs live; the sheet only points at it
  // so the two are never out of sync.
  const paletteNote =
    "In the palette: start a session, prune empty sessions, refresh the feed — or type to fuzzy-jump to any session.";

  function isTyping(): boolean {
    const el = document.activeElement;
    if (!el) return false;
    const tag = el.tagName;
    return tag === "INPUT" || tag === "TEXTAREA" || (el as HTMLElement).isContentEditable;
  }
  function show() {
    // Yield the screen from any other top-level overlay (the command palette)
    // so the two never stack and fight over focus.
    dispatchEvent(new CustomEvent("eigen:overlay", { detail: "shortcuts" }));
    open = true;
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === "?" && !isTyping() && !e.metaKey && !e.ctrlKey) {
      e.preventDefault();
      open ? (open = false) : show();
    } else if (e.key === "Escape" && open) {
      open = false;
    }
  }
  // The command palette opened — close so the focused overlay stays on top.
  function onOverlay(e: Event) {
    if ((e as CustomEvent).detail !== "shortcuts") open = false;
  }
  $effect(() => {
    addEventListener("keydown", onKey);
    addEventListener("eigen:overlay", onOverlay);
    return () => {
      removeEventListener("keydown", onKey);
      removeEventListener("eigen:overlay", onOverlay);
    };
  });
</script>

{#if open}
  <div
    class="sc__scrim"
    role="button"
    tabindex="-1"
    aria-label="Close shortcuts"
    onclick={() => (open = false)}
    onkeydown={(e) => e.key === "Enter" && (open = false)}
  ></div>
  <div class="sc" role="dialog" aria-modal="true" aria-label="Keyboard shortcuts" tabindex="-1" use:trapFocus>
    <h2 class="sc__title">Keyboard shortcuts</h2>
    <dl class="sc__list">
      {#each rows as r (r.desc + r.keys.join())}
        <div class="sc__row">
          <dt class="sc__keys">
            {#each r.keys as k (k)}<kbd>{k}</kbd>{/each}
          </dt>
          <dd class="sc__desc">{r.desc}</dd>
        </div>
      {/each}
    </dl>
    <p class="sc__note">{paletteNote}</p>
    <button class="sc__close" onclick={() => (open = false)}>Close</button>
  </div>
{/if}

<style>
  .sc__scrim {
    position: fixed;
    inset: 0;
    background: var(--bg-scrim);
    z-index: 90;
    animation: sc-scrim var(--dur-fast) var(--ease-out);
  }
  .sc {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(440px, 90vw);
    background: var(--bg-overlay);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-lg);
    box-shadow: var(--shadow-3);
    z-index: 91;
    padding: var(--sp-7);
    animation: sc-in var(--dur-base) var(--ease-out);
  }
  .sc__title {
    margin: 0 0 var(--sp-5);
    font: var(--fw-semibold) var(--fs-h3) / 1 var(--font-display);
    color: var(--text-primary);
  }
  .sc__list {
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .sc__row {
    display: flex;
    align-items: center;
    gap: var(--sp-5);
  }
  .sc__keys {
    flex: none;
    display: flex;
    gap: var(--sp-2);
    min-width: 120px;
    justify-content: flex-end;
  }
  .sc__keys kbd {
    font: var(--fw-medium) var(--fs-micro) / 1 var(--font-mono);
    background: var(--bg-raised-2);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-xs);
    padding: var(--sp-2) var(--sp-3);
    color: var(--text-secondary);
  }
  .sc__desc {
    margin: 0;
    font-size: var(--fs-body-sm);
    color: var(--text-secondary);
  }
  .sc__note {
    margin: var(--sp-5) 0 0;
    padding-top: var(--sp-5);
    border-top: 1px solid var(--border-hairline);
    font-size: var(--fs-body-sm);
    line-height: 1.5;
    color: var(--text-muted);
  }
  .sc__close {
    margin-top: var(--sp-6);
    width: 100%;
    height: 32px;
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised);
    color: var(--text-primary);
    border-radius: var(--r-sm);
    cursor: pointer;
    font: var(--fw-semibold) var(--fs-body-sm) / 1 var(--font-sans);
  }
  .sc__close:hover {
    background: var(--bg-raised-2);
  }
  .sc__close:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  @keyframes sc-scrim {
    from {
      opacity: 0;
    }
  }
  @keyframes sc-in {
    from {
      transform: translate(-50%, -48%);
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .sc__scrim,
    .sc {
      animation: none;
    }
  }
</style>
