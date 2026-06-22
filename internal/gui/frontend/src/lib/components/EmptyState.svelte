<script lang="ts">
  // The honest placeholder for not-yet-built views and zero-data lists. Renders
  // a real, composed state — never a dead control. An optional action slot lets
  // a caller offer the one thing worth doing here.
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
  <div class="empty__glyph" aria-hidden="true">{glyph}</div>
  <div class="empty__title">{title}</div>
  {#if line}<p class="empty__line">{line}</p>{/if}
  {#if action}<div class="empty__action">{@render action()}</div>{/if}
</div>

<style>
  .empty {
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-4);
    padding: var(--sp-10);
    text-align: center;
  }
  .empty__glyph {
    font-size: 34px;
    color: var(--text-faint);
    line-height: 1;
    font-weight: var(--fw-regular);
  }
  .empty__title {
    font-size: var(--fs-h3);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
    text-transform: capitalize;
    letter-spacing: var(--ls-heading);
  }
  .empty__line {
    margin: 0;
    max-width: 38ch;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
  }
  .empty__action {
    margin-top: var(--sp-3);
  }
</style>
