<script lang="ts">
  // The reference primitive. Every control in the app composes from this.
  // States: default · hover · active · focus-visible · disabled · loading.
  //
  // TEXT ARRANGEMENT is the soul of this component:
  //  · The label sits on its own optical baseline — a hair of top padding-trim
  //    via line-height:1 + flex centering keeps Inter's ascender/descender box
  //    visually centered rather than mathematically centered.
  //  · An optional leading `icon` Snippet aligns flush with the label, sharing
  //    one baseline; the gap between glyph and word is tuned per size.
  //  · Long labels ellipsis-truncate while the button keeps a sane min-width so
  //    it never collapses to nothing.
  //  · Loading swaps the label for a centered spinner WITHOUT moving anything:
  //    the label keeps its box (visibility:hidden), so width is locked and the
  //    text never shifts. Reduced-motion stills the spinner.
  import type { Snippet } from "svelte";

  let {
    variant = "secondary",
    size = "md",
    loading = false,
    disabled = false,
    type = "button",
    title,
    ariaLabel,
    full = false,
    icon,
    onclick,
    children,
  }: {
    variant?: "primary" | "secondary" | "ghost" | "danger" | "icon" | "link";
    size?: "sm" | "md" | "lg";
    loading?: boolean;
    disabled?: boolean;
    type?: "button" | "submit";
    title?: string;
    // Accessible name for icon-only buttons whose visible content is a glyph,
    // not a word — screen readers read glyph text content literally (e.g. "✕"
    // as "multiplication x"), so a real label needs to override that.
    ariaLabel?: string;
    full?: boolean;
    icon?: Snippet;
    onclick?: (e: MouseEvent) => void;
    children: Snippet;
  } = $props();
</script>

<button
  {type}
  {title}
  aria-label={ariaLabel}
  class="btn btn--{variant} btn--{size}"
  class:btn--full={full}
  class:btn--has-icon={icon && variant !== "icon"}
  disabled={disabled || loading}
  data-loading={loading || undefined}
  aria-busy={loading}
  {onclick}
>
  <span class="btn__content">
    {#if icon}<span class="btn__icon" aria-hidden="true">{@render icon()}</span>{/if}
    <span class="btn__label">{@render children()}</span>
  </span>
  {#if loading}<span class="btn__spinner" aria-hidden="true"></span>{/if}
</button>

<style>
  .btn {
    /* line-height:1 collapses the line box so flex centering is the only
       vertical authority — the label sits optically centered, not floated. */
    font: var(--fw-semibold) var(--fs-body-sm) / 1 var(--font-sans);
    letter-spacing: var(--ls-normal);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    box-sizing: border-box;
    max-width: 100%;
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised);
    color: var(--text-primary);
    border-radius: var(--r-sm);
    cursor: pointer;
    position: relative;
    user-select: none;
    /* keep the descender from nudging the optical center down */
    text-rendering: optimizeLegibility;
    -webkit-font-smoothing: antialiased;
    transition:
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      transform var(--dur-instant) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out);
  }
  .btn--full {
    width: 100%;
  }

  /* ── content row: icon + label share one baseline ─────────────────── */
  .btn__content {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--btn-gap, var(--sp-3));
    min-width: 0; /* let the label truncate, not overflow */
    max-width: 100%;
  }
  .btn__label {
    display: block;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    /* optical baseline trim: nudge text up a sub-pixel so the visual mass of
       the x-height centers in the control rather than the full line box. */
    transform: translateY(-0.5px);
  }
  .btn__icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    /* glyph optically aligns with the cap-height of the label */
    width: var(--btn-icon, 1em);
    height: var(--btn-icon, 1em);
    line-height: 0;
  }
  /* normalize any svg/glyph the icon snippet renders */
  .btn__icon :global(svg) {
    display: block;
    width: 100%;
    height: 100%;
  }

  /* ── sizes: height, padding, type scale, icon size, gap ───────────── */
  .btn--sm {
    height: 26px;
    padding: 0 var(--sp-4);
    font-size: var(--fs-micro);
    min-width: 26px;
    --btn-gap: var(--sp-2);
    --btn-icon: 13px;
  }
  .btn--md {
    height: 32px;
    padding: 0 var(--sp-5);
    min-width: 32px;
    --btn-gap: var(--sp-3);
    --btn-icon: 15px;
  }
  .btn--lg {
    height: 40px;
    padding: 0 var(--sp-6);
    font-size: var(--fs-body);
    min-width: 40px;
    --btn-gap: var(--sp-4);
    --btn-icon: 17px;
  }
  /* a leading icon wants a touch less padding on its side for optical balance */
  .btn--has-icon.btn--sm {
    padding-left: var(--sp-3);
  }
  .btn--has-icon.btn--md {
    padding-left: var(--sp-4);
  }
  .btn--has-icon.btn--lg {
    padding-left: var(--sp-5);
  }

  /* ── shared states ────────────────────────────────────────────────── */
  .btn:hover:not(:disabled) {
    background: var(--bg-raised-2);
    border-color: var(--border-strong);
  }
  .btn:active:not(:disabled) {
    background: var(--bg-inset);
    transform: translateY(0.5px) scale(0.985);
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

  /* ── primary ──────────────────────────────────────────────────────── */
  .btn--primary {
    background: var(--brand);
    color: var(--text-on-brand);
    border-color: transparent;
  }
  .btn--primary:hover:not(:disabled) {
    background: var(--brand-bright);
    border-color: transparent;
  }
  .btn--primary:active:not(:disabled) {
    background: var(--brand-dim);
  }
  .btn--primary:disabled {
    background: var(--brand-dim);
    color: var(--text-on-brand);
    border-color: transparent;
    opacity: 0.5;
  }

  /* ── danger ───────────────────────────────────────────────────────── */
  .btn--danger {
    color: var(--error);
    border-color: var(--error-bg);
    background: transparent;
  }
  .btn--danger:hover:not(:disabled) {
    background: var(--error-bg);
    border-color: var(--error);
  }
  .btn--danger:active:not(:disabled) {
    background: var(--error-bg);
    border-color: var(--error);
  }

  /* ── ghost ────────────────────────────────────────────────────────── */
  .btn--ghost {
    background: transparent;
    border-color: transparent;
    color: var(--text-secondary);
  }
  .btn--ghost:hover:not(:disabled) {
    background: var(--state-hover);
    border-color: transparent;
    color: var(--text-primary);
  }
  .btn--ghost:active:not(:disabled) {
    background: var(--state-active);
  }
  .btn--ghost:disabled {
    background: transparent;
    border-color: transparent;
  }

  /* ── icon: perfect square, glyph optically centered ───────────────── */
  .btn--icon {
    padding: 0;
    background: transparent;
    border-color: transparent;
    color: var(--text-secondary);
    border-radius: var(--r-sm);
  }
  .btn--icon.btn--sm {
    width: 26px;
  }
  .btn--icon.btn--md {
    width: 32px;
  }
  .btn--icon.btn--lg {
    width: 40px;
  }
  .btn--icon .btn__content {
    gap: 0;
  }
  /* in a square icon button the glyph owns the box and centers exactly */
  .btn--icon .btn__label {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    line-height: 0;
    overflow: visible;
    transform: none;
  }
  .btn--icon:hover:not(:disabled) {
    background: var(--state-hover);
    border-color: transparent;
    color: var(--text-primary);
  }
  .btn--icon:active:not(:disabled) {
    background: var(--state-active);
  }
  .btn--icon:disabled {
    background: transparent;
    border-color: transparent;
  }

  /* ── link: inline, no chrome ──────────────────────────────────────── */
  .btn--link {
    background: transparent;
    border-color: transparent;
    color: var(--accent);
    height: auto;
    min-width: 0;
    padding: 0;
    border-radius: var(--r-xs);
  }
  .btn--link .btn__label {
    transform: none;
  }
  .btn--link:hover:not(:disabled) {
    color: var(--accent-bright);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .btn--link:active:not(:disabled) {
    color: var(--accent-strong);
    transform: none;
  }
  .btn--link:disabled {
    color: var(--text-ghost);
    background: transparent;
    border-color: transparent;
  }

  /* ── loading: width-locked, label box preserved, spinner centered ─── */
  .btn[data-loading] {
    pointer-events: none;
  }
  .btn[data-loading] .btn__content {
    /* keep the box (and thus width) but hide it so nothing reflows */
    visibility: hidden;
  }
  .btn__spinner {
    position: absolute;
    top: 50%;
    left: 50%;
    width: 14px;
    height: 14px;
    margin: -7px 0 0 -7px;
    border-radius: var(--r-full);
    border: 2px solid var(--working-bg);
    border-top-color: var(--working);
    animation: btn-spin 0.7s linear infinite;
  }
  /* spinner inherits the foreground tint on solid variants for contrast */
  .btn--primary .btn__spinner {
    border-color: color-mix(in srgb, var(--text-on-brand) 25%, transparent);
    border-top-color: var(--text-on-brand);
  }
  .btn--sm .btn__spinner {
    width: 12px;
    height: 12px;
    margin: -6px 0 0 -6px;
  }
  .btn--lg .btn__spinner {
    width: 16px;
    height: 16px;
    margin: -8px 0 0 -8px;
  }
  @keyframes btn-spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .btn {
      transition:
        background var(--dur-fast) var(--ease-out),
        border-color var(--dur-fast) var(--ease-out),
        color var(--dur-fast) var(--ease-out);
    }
    .btn:active:not(:disabled) {
      transform: none;
    }
    .btn__spinner {
      animation: none;
      opacity: 0.7;
    }
  }
</style>
