<script lang="ts">
  // Transient feedback host. Toasts stack bottom-right, never steal focus, and
  // never block the app (the host is pointer-transparent; only the cards catch
  // events). Each card carries a kind-tinted accent rule and an auto-dismiss
  // progress hairline that tracks the store's TTL — and pauses on hover so a
  // message can be read before it slips away.
  import { toasts, type ToastKind } from "$lib/stores/toasts.svelte";
  import { fly, scale } from "svelte/transition";

  // Mirrors TTL in the store; drives the progress hairline duration only.
  const TTL_MS = 4200;

  // Enter/exit durations track the motion-token scale (--dur-slow / --dur-fast).
  // Svelte transitions take raw numbers, so we cannot reference the CSS vars
  // directly — keep these aligned with tokens.css by hand.
  const ENTER_MS = 280; // --dur-slow
  const EXIT_MS = 140; // --dur-fast

  // Svelte JS transitions do not honour prefers-reduced-motion on their own,
  // so we gate the durations to 0 when the user has asked to reduce motion.
  // Stay live: the OS setting can flip at runtime, so we hold the query and
  // re-read it on every 'change' rather than snapshotting once at mount.
  const motionQuery =
    typeof window !== "undefined" && typeof window.matchMedia === "function"
      ? window.matchMedia("(prefers-reduced-motion: reduce)")
      : null;

  let reduceMotion = $state(motionQuery?.matches ?? false);

  $effect(() => {
    if (!motionQuery) return;
    const onChange = (e: MediaQueryListEvent) => {
      reduceMotion = e.matches;
    };
    // Re-sync once on subscribe in case the setting flipped before this ran.
    reduceMotion = motionQuery.matches;
    motionQuery.addEventListener("change", onChange);
    return () => motionQuery.removeEventListener("change", onChange);
  });

  const enterMs = $derived(reduceMotion ? 0 : ENTER_MS);
  const exitMs = $derived(reduceMotion ? 0 : EXIT_MS);

  const GLYPH: Record<ToastKind, string> = {
    success: "✓",
    error: "!",
    info: "i",
    working: "·",
  };
</script>

<!-- The host is a neutral container: each toast carries its own live-region
     role (assertive `alert` for errors, polite `status` otherwise), so the
     host must NOT also declare aria-live or nested live regions would
     double-announce and fight the error toasts' assertiveness. -->
<div class="toast-host">
  {#each toasts.items as t (t.id)}
    <div
      class="toast toast--{t.kind}"
      role={t.kind === "error" ? "alert" : "status"}
      in:fly={{ y: 14, duration: enterMs, opacity: 0 }}
      out:scale={{ start: 0.96, opacity: 0, duration: exitMs }}
      style:--ttl="{TTL_MS}ms"
    >
      <span class="toast__glyph" aria-hidden="true">{GLYPH[t.kind]}</span>
      <span class="toast__text">{t.text}</span>
      <button
        class="toast__close"
        onclick={() => toasts.dismiss(t.id)}
        aria-label="Dismiss notification"
        title="Dismiss"
      >
        <svg viewBox="0 0 14 14" width="12" height="12" aria-hidden="true">
          <path
            d="M3.5 3.5l7 7M10.5 3.5l-7 7"
            stroke="currentColor"
            stroke-width="1.4"
            stroke-linecap="round"
          />
        </svg>
      </button>
      <span class="toast__progress" aria-hidden="true"></span>
    </div>
  {/each}
</div>

<style>
  .toast-host {
    position: fixed;
    bottom: var(--sp-7);
    right: var(--sp-7);
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: var(--sp-4);
    z-index: 100;
    /* Host is a passive overlay — clicks fall through to the app beneath. */
    pointer-events: none;
    max-width: min(380px, calc(100vw - var(--sp-9)));
  }

  .toast {
    /* Only the card itself is interactive. */
    pointer-events: auto;
    position: relative;
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: start;
    column-gap: var(--sp-5);
    width: 100%;
    min-width: 248px;
    padding: var(--sp-4) var(--sp-4) var(--sp-4) var(--sp-6);
    background: var(--bg-overlay);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    box-shadow: var(--shadow-toast);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
    color: var(--text-primary);
    overflow: hidden;
  }

  /* Accent rule — a kind-colored spine with a soft tinted bleed inward. */
  .toast::before {
    content: "";
    position: absolute;
    top: 0;
    left: 0;
    bottom: 0;
    width: 2px;
    background: var(--toast-accent, var(--text-muted));
    box-shadow: 0 0 10px -1px var(--toast-accent-glow, transparent);
  }

  /* Glow is derived from the kind accent token via color-mix, so it tracks the
     semantic color if tokens.css changes (no hand-copied rgb channels). */
  .toast--success {
    --toast-accent: var(--success);
    --toast-accent-glow: color-mix(in srgb, var(--success) 40%, transparent);
    --toast-accent-bg: var(--success-bg);
  }
  .toast--error {
    --toast-accent: var(--error);
    --toast-accent-glow: color-mix(in srgb, var(--error) 42%, transparent);
    --toast-accent-bg: var(--error-bg);
  }
  .toast--info {
    --toast-accent: var(--info);
    --toast-accent-glow: color-mix(in srgb, var(--info) 40%, transparent);
    --toast-accent-bg: var(--info-bg);
  }
  .toast--working {
    --toast-accent: var(--working);
    --toast-accent-glow: color-mix(in srgb, var(--working) 42%, transparent);
    --toast-accent-bg: var(--working-bg);
  }

  /* Kind glyph in a tinted disc — proportional Inter, never mono. */
  .toast__glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    margin-top: 1px;
    flex: none;
    border-radius: var(--r-full);
    background: var(--toast-accent-bg, var(--state-hover));
    color: var(--toast-accent, var(--text-secondary));
    font-family: var(--font-sans);
    font-size: var(--fs-micro);
    font-weight: var(--fw-bold);
    line-height: 1;
  }
  .toast--working .toast__glyph {
    animation: toast-breathe var(--breath) var(--ease-inout) infinite;
  }

  .toast__text {
    min-width: 0;
    padding-top: 1px;
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .toast__close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    flex: none;
    margin: -2px -2px 0 0;
    border: none;
    background: transparent;
    border-radius: var(--r-sm);
    color: var(--text-muted);
    cursor: pointer;
    transition:
      color var(--dur-instant) var(--ease-out),
      background var(--dur-instant) var(--ease-out);
  }
  .toast__close:hover {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .toast__close:active {
    background: var(--state-active);
  }
  .toast__close:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }

  /* Auto-dismiss countdown — a kind-tinted hairline draining left to right.
     Pauses while hovered so the toast can be read in full. */
  .toast__progress {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    height: 1.5px;
    transform-origin: left center;
    background: var(--toast-accent, var(--text-muted));
    opacity: 0.55;
    animation: toast-drain var(--ttl, 4200ms) linear forwards;
  }
  .toast:hover .toast__progress,
  .toast:focus-within .toast__progress {
    animation-play-state: paused;
  }

  @keyframes toast-drain {
    from {
      transform: scaleX(1);
    }
    to {
      transform: scaleX(0);
    }
  }
  @keyframes toast-breathe {
    0%,
    100% {
      opacity: 0.55;
    }
    50% {
      opacity: 1;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .toast__progress {
      animation: none;
      transform: scaleX(1);
      opacity: 0.28;
    }
    .toast--working .toast__glyph {
      animation: none;
    }
  }
</style>
