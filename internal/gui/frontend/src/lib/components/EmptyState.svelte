<script lang="ts">
  // The honest placeholder for not-yet-built views and zero-data lists. It must
  // read as intentional and calm — a considered pause, never a broken control.
  // The glyph sits in a soft inset disc to feel placed rather than dropped; the
  // title leads, the line follows on a readable measure, and an optional action
  // offers the one thing worth doing here. A brief, reduced-motion-safe rise on
  // mount keeps arrivals gentle.
  import type { Snippet } from "svelte";

  let {
    glyph = "·",
    title,
    line = "",
    action,
  }: {
    glyph?: string;
    title: string;
    line?: string;
    action?: Snippet;
  } = $props();
</script>

<div class="empty">
  <div class="empty__glyph" aria-hidden="true">
    <span class="empty__glyph-mark">{glyph}</span>
  </div>
  <div class="empty__text">
    <h2 class="empty__title">{title}</h2>
    {#if line}<p class="empty__line">{line}</p>{/if}
  </div>
  {#if action}<div class="empty__action">{@render action()}</div>{/if}
</div>

<style>
  .empty {
    height: 100%;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-6);
    padding: var(--sp-10) var(--sp-8);
    text-align: center;
    animation: empty-rise var(--dur-slow) var(--ease-out);
  }

  /* GLYPH — large but quiet; a soft inset disc places it intentionally. */
  .empty__glyph {
    display: grid;
    place-items: center;
    width: 56px;
    height: 56px;
    border-radius: var(--r-full);
    background: var(--bg-inset);
    box-shadow: inset 0 0 0 1px var(--border-hairline);
    color: var(--text-faint);
  }
  .empty__glyph-mark {
    font-size: 30px;
    line-height: 1;
    font-weight: var(--fw-regular);
    /* Optically center common single-glyph marks within the disc. */
    transform: translateY(-0.02em);
  }

  /* TEXT BLOCK — tight pairing of title + line so they read as one unit. */
  .empty__text {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-3);
  }
  .empty__title {
    margin: 0;
    font-size: var(--fs-h3);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
    letter-spacing: var(--ls-heading);
    line-height: var(--lh-tight);
  }
  .empty__line {
    margin: 0;
    max-width: 36ch;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
    text-wrap: balance;
  }

  /* ACTION — the single thing worth doing; sits a touch lower, separated. */
  .empty__action {
    margin-top: var(--sp-2);
  }

  @keyframes empty-rise {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .empty {
      animation: none;
    }
  }
</style>
