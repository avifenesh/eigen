<script lang="ts">
  // A small status indicator. `working` gently breathes with a warm glow;
  // any state can opt into a quiet breathing pulse via `pulse`. The dot is
  // perfectly round at any size and stills entirely under reduced-motion.
  //
  // The dot is decorative by default (aria-hidden) — in most views adjacent
  // text already carries the state. But where the dot is the ONLY state
  // carrier, the color alone is invisible to screen readers. Pass `label` to
  // announce the state: `label` (truthy string) promotes the dot to
  // role="img" with that aria-label and drops aria-hidden; `label={true}`
  // uses the sensible default for the current state; omitting it (or false)
  // keeps the dot hidden.
  // `live` = present + alive but NOT actively working: same teal as working
  // but static (working breathes). Used for the daemon-online dot and the
  // freshest/unread row — the reason those read teal, not green `ok`.
  // `bright` lifts the fill to brand-bright for the one-per-view "freshest"
  // accent (e.g. an unread chat); it only changes the color, not the motion.
  type DotState = "working" | "live" | "idle" | "ok" | "warn" | "error";
  let {
    state = "idle",
    size = 8,
    pulse = false,
    bright = false,
    label = false,
  }: {
    state?: DotState;
    size?: number;
    pulse?: boolean;
    bright?: boolean;
    label?: string | boolean;
  } = $props();

  const dotState = $derived(state);
  const isWorking = $derived(dotState === "working");
  const breathing = $derived(pulse || isWorking);

  // Default-per-state announcement, used when `label={true}` is passed.
  const defaultLabels: Record<DotState, string> = {
    working: "Working",
    live: "Live",
    idle: "Idle",
    ok: "OK",
    warn: "Warning",
    error: "Error",
  };
  // Resolve the spoken label: an explicit string wins; `true` falls back to
  // the per-state default; anything falsy leaves the dot decorative.
  const ariaLabel = $derived(
    typeof label === "string" && label.length > 0
      ? label
      : label === true
        ? defaultLabels[dotState]
        : null,
  );
</script>

<span
  class="dot dot--{dotState}"
  class:dot--breathe={breathing}
  class:dot--glow={isWorking}
  class:dot--bright={bright}
  style="--dot-size:{size}px"
  role={ariaLabel ? "img" : undefined}
  aria-label={ariaLabel ?? undefined}
  aria-hidden={ariaLabel ? undefined : "true"}
></span>

<style>
  .dot {
    display: inline-block;
    width: var(--dot-size);
    height: var(--dot-size);
    border-radius: var(--r-full);
    /* Perfectly round even when a flex parent tries to stretch it. */
    flex: 0 0 auto;
    aspect-ratio: 1;
    background: var(--dot-fill, var(--dot-idle));
  }

  .dot--working {
    --dot-fill: var(--dot-working);
  }
  .dot--live {
    --dot-fill: var(--dot-live);
  }
  .dot--idle {
    --dot-fill: var(--dot-idle);
  }
  .dot--ok {
    --dot-fill: var(--dot-ok);
  }
  .dot--warn {
    --dot-fill: var(--dot-warn);
  }
  .dot--error {
    --dot-fill: var(--dot-error);
  }
  /* `bright` lifts the fill to brand-bright — the one-per-view freshest accent
     (e.g. an unread idle chat). Color only; motion is unchanged. */
  .dot--bright {
    --dot-fill: var(--brand-bright);
  }

  /* Gentle, continuous breath — opacity AND a hair of scale, so the dot reads
     as a living thing drawing breath rather than a blinking LED. The curve is
     asymmetric on purpose: a slow swell to full, a longer soft settle (the
     keyframes aren't centred on 50%), tuned with --ease-inout. Transform stays
     a sub-pixel scale so layout never reflows. will-change is scoped to
     animating dots so static dots don't hold a compositor layer for no reason. */
  .dot--breathe {
    animation: dot-breathe var(--breath) var(--ease-inout) infinite;
    will-change: opacity, transform;
  }
  @keyframes dot-breathe {
    0% {
      opacity: 0.55;
      transform: scale(0.92);
    }
    45% {
      opacity: 1;
      transform: scale(1);
    }
    100% {
      opacity: 0.55;
      transform: scale(0.92);
    }
  }

  /* The teal working halo lives only on the working state (matches the brand
     "alive" accent + the working dot fill). It is a STATIC
     box-shadow — the dot's own breathe (opacity + sub-pixel scale) makes the
     whole dot+halo swell and fade as one heartbeat. Previously the halo
     animated box-shadow on its own infinite track, which WebKitGTK cannot
     composite — it repaints the dot's bounding box every frame for the life of
     the animation, on the main thread, competing with scroll/stream exactly
     when a turn is working. Static shadow + animated opacity gets the same look
     for free. */
  .dot--glow {
    animation: dot-breathe var(--breath) var(--ease-inout) infinite;
    box-shadow: var(--glow-live);
    will-change: opacity, transform;
  }

  @media (prefers-reduced-motion: reduce) {
    .dot--breathe,
    .dot--glow {
      animation: none;
      opacity: 1;
      transform: none;
    }
    /* Hold the working halo statically so the state still reads as alive. */
    .dot--glow {
      box-shadow: var(--glow-live);
    }
  }
</style>
