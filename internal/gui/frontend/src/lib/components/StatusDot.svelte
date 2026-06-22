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

  /* Gentle, continuous breath — opacity only, so layout never reflows.
     will-change is scoped to animating dots so static dots don't hold a
     compositor layer for no reason. */
  .dot--breathe {
    animation: dot-breathe var(--breath) var(--ease-inout) infinite;
    will-change: opacity;
  }
  @keyframes dot-breathe {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.5;
    }
  }

  /* The warm working halo lives only on the working state. It pulses in
     sympathy with the breath but on its own track so the glow swell reads
     as a soft heartbeat rather than a hard blink. */
  .dot--glow {
    animation:
      dot-breathe var(--breath) var(--ease-inout) infinite,
      dot-glow var(--breath) var(--ease-inout) infinite;
    will-change: opacity, box-shadow;
  }
  @keyframes dot-glow {
    0%,
    100% {
      box-shadow: 0 0 0 0 transparent;
    }
    50% {
      box-shadow: var(--glow-working);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .dot--breathe,
    .dot--glow {
      animation: none;
    }
    /* Hold the working halo statically so the state still reads as alive. */
    .dot--glow {
      box-shadow: var(--glow-working);
    }
  }
</style>
