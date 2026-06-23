<script lang="ts">
  // A small status indicator. `working` gently breathes with a warm glow;
  // any state can opt into a quiet breathing pulse via `pulse`. The dot is
  // perfectly round at any size and stills entirely under reduced-motion.
  let {
    state = "idle",
    size = 8,
    pulse = false,
  }: {
    state?: "working" | "idle" | "ok" | "warn" | "error";
    size?: number;
    pulse?: boolean;
  } = $props();

  const dotState = $derived(state);
  const isWorking = $derived(dotState === "working");
  const breathing = $derived(pulse || isWorking);
</script>

<span
  class="dot dot--{dotState}"
  class:dot--breathe={breathing}
  class:dot--glow={isWorking}
  style="--dot-size:{size}px"
  aria-hidden="true"
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

  /* The warm working halo lives only on the working state. It swells in
     sympathy with the breath — sharing the same track and curve so the glow
     reads as one soft heartbeat, cresting with the dot rather than blinking
     against it. */
  .dot--glow {
    animation:
      dot-breathe var(--breath) var(--ease-inout) infinite,
      dot-glow var(--breath) var(--ease-inout) infinite;
    will-change: opacity, transform, box-shadow;
  }
  @keyframes dot-glow {
    0%,
    100% {
      box-shadow: 0 0 0 0 transparent;
    }
    45% {
      box-shadow: var(--glow-working);
    }
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
      box-shadow: var(--glow-working);
    }
  }
</style>
