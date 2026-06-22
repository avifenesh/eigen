<script lang="ts">
  // A small status indicator. `working` gently breathes; others are static.
  let {
    state = "idle",
    size = 8,
    pulse = false,
  }: {
    state?: "working" | "idle" | "ok" | "warn" | "error";
    size?: number;
    pulse?: boolean;
  } = $props();
</script>

<span
  class="dot dot--{state}"
  class:dot--pulse={pulse || state === "working"}
  style="--dot-size:{size}px"
  aria-hidden="true"
></span>

<style>
  .dot {
    display: inline-block;
    width: var(--dot-size);
    height: var(--dot-size);
    border-radius: var(--r-full);
    flex: none;
  }
  .dot--working {
    background: var(--dot-working);
  }
  .dot--idle {
    background: var(--dot-idle);
  }
  .dot--ok {
    background: var(--dot-ok);
  }
  .dot--warn {
    background: var(--dot-warn);
  }
  .dot--error {
    background: var(--dot-error);
  }
  .dot--pulse {
    animation: breathe var(--breath) var(--ease-inout) infinite;
  }
  @keyframes breathe {
    0%,
    100% {
      opacity: 1;
      box-shadow: 0 0 0 0 transparent;
    }
    50% {
      opacity: 0.55;
      box-shadow: var(--glow-working);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .dot--pulse {
      animation: none;
    }
  }
</style>
