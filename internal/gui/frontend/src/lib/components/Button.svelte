<script lang="ts">
  // The reference primitive. Every control in the app composes from this.
  // States: default · hover · active · focus-visible · disabled · loading.
  // Loading swaps the label for a spinner while preserving width (no layout
  // jump) and blocks pointer events; reduced-motion stills the spinner.
  import type { Snippet } from "svelte";

  let {
    variant = "secondary",
    size = "md",
    loading = false,
    disabled = false,
    type = "button",
    title,
    full = false,
    onclick,
    children,
  }: {
    variant?: "primary" | "secondary" | "ghost" | "danger" | "icon" | "link";
    size?: "sm" | "md" | "lg";
    loading?: boolean;
    disabled?: boolean;
    type?: "button" | "submit";
    title?: string;
    full?: boolean;
    onclick?: (e: MouseEvent) => void;
    children: Snippet;
  } = $props();
</script>

<button
  {type}
  {title}
  class="btn btn--{variant} btn--{size}"
  class:btn--full={full}
  disabled={disabled || loading}
  data-loading={loading || undefined}
  aria-busy={loading}
  {onclick}
>
  {#if loading}<span class="btn__spinner" aria-hidden="true"></span>{/if}
  <span class="btn__label">{@render children()}</span>
</button>

<style>
  .btn {
    font: var(--fw-semibold) var(--fs-body-sm) / 1 var(--font-sans);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--sp-3);
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised);
    color: var(--text-primary);
    border-radius: var(--r-sm);
    cursor: pointer;
    position: relative;
    white-space: nowrap;
    transition:
      background var(--dur-instant) var(--ease-out),
      border-color var(--dur-instant) var(--ease-out),
      transform var(--dur-instant) var(--ease-out),
      box-shadow var(--dur-instant) var(--ease-out);
  }
  .btn--full {
    width: 100%;
  }
  .btn--sm {
    height: 26px;
    padding: 0 var(--sp-4);
    font-size: var(--fs-micro);
  }
  .btn--md {
    height: 32px;
    padding: 0 var(--sp-5);
  }
  .btn--lg {
    height: 40px;
    padding: 0 var(--sp-6);
    font-size: var(--fs-body);
  }
  .btn:hover:not(:disabled) {
    background: var(--bg-raised-2);
    border-color: var(--border-strong);
  }
  .btn:active:not(:disabled) {
    background: var(--bg-inset);
    transform: translateY(0.5px) scale(0.99);
  }
  .btn:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .btn:disabled {
    color: var(--text-ghost);
    background: var(--bg-raised);
    border-color: var(--border-hairline);
    cursor: not-allowed;
  }

  .btn--primary {
    background: var(--brand);
    color: var(--text-on-brand);
    border-color: transparent;
  }
  .btn--primary:hover:not(:disabled) {
    background: var(--brand-bright);
  }
  .btn--primary:active:not(:disabled) {
    background: var(--brand-dim);
  }

  .btn--danger {
    color: var(--error);
    border-color: var(--error-bg);
  }
  .btn--danger:hover:not(:disabled) {
    background: var(--error-bg);
    border-color: var(--error);
  }

  .btn--ghost {
    background: transparent;
    border-color: transparent;
    color: var(--text-secondary);
  }
  .btn--ghost:hover:not(:disabled) {
    background: var(--state-hover);
    color: var(--text-primary);
  }

  .btn--icon {
    width: 32px;
    padding: 0;
    background: transparent;
    border-color: transparent;
    color: var(--text-secondary);
  }
  .btn--icon:hover:not(:disabled) {
    background: var(--state-hover);
    color: var(--text-primary);
  }

  .btn--link {
    background: transparent;
    border-color: transparent;
    color: var(--accent);
    height: auto;
    padding: 0;
  }
  .btn--link:hover:not(:disabled) {
    color: var(--accent-bright);
    text-decoration: underline;
  }

  .btn[data-loading] .btn__label {
    visibility: hidden;
  }
  .btn[data-loading] {
    pointer-events: none;
  }
  .btn__spinner {
    position: absolute;
    width: 14px;
    height: 14px;
    border-radius: var(--r-full);
    border: 2px solid var(--working-bg);
    border-top-color: var(--working);
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .btn__spinner {
      animation: none;
      opacity: 0.7;
    }
  }
</style>
