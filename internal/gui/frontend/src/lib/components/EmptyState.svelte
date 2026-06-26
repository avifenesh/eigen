<script lang="ts">
  // The honest placeholder for not-yet-built views and zero-data lists. It must
  // read as intentional and calm — a considered pause, never a broken control.
  //
  // VOICE: the glyph sits in a soft inset disc to feel placed rather than
  // dropped; the title leads, the line follows on a readable measure, and an
  // optional action offers the one thing worth doing here. When an action is
  // present the surface comes quietly alive — a faint teal ring and a hairline
  // tether draw the eye down to that next step; without one it reads as cool,
  // intentional calm. A brief, reduced-motion-safe rise on mount keeps arrivals
  // gentle, with the glyph leading and the text settling a beat behind.
  import type { Snippet } from "svelte";

  let {
    glyph = "·",
    title,
    line = "",
    action,
    headingLevel = 2,
  }: {
    glyph?: string;
    title: string;
    line?: string;
    action?: Snippet;
    // Heading level for the title so the empty state slots into the surrounding
    // document outline — a top-level view body wants <h2>, a card nested under a
    // section heading wants <h3>. Defaults to 2 so existing callers are unchanged.
    headingLevel?: 1 | 2 | 3 | 4 | 5 | 6;
  } = $props();

  // An action makes this a place to *do* something — let teal lead the eye.
  const hasAction = $derived(!!action);
  const titleTag = $derived(`h${headingLevel}` as const);
</script>

<div class="empty" class:empty--actionable={hasAction}>
  <div class="empty__glyph" aria-hidden="true">
    <span class="empty__glyph-mark">{glyph}</span>
  </div>
  <div class="empty__text">
    <svelte:element this={titleTag} class="empty__title">{title}</svelte:element>
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
    /* Glyph leads the rise; text + action settle a beat behind (see below). */
    animation: empty-rise var(--dur-slow) var(--ease-out) both;
  }

  /* GLYPH — large but quiet; a soft inset disc places it intentionally.
     A faint outer ring lifts it off the page without ever shouting. */
  .empty__glyph {
    position: relative;
    display: grid;
    place-items: center;
    width: 60px;
    height: 60px;
    border-radius: var(--r-full);
    background: var(--bg-inset);
    box-shadow:
      inset 0 0 0 1px var(--border-hairline),
      0 0 0 6px var(--state-hover);
    color: var(--text-ghost);
    transition:
      color var(--dur-base) var(--ease-out),
      box-shadow var(--dur-base) var(--ease-out);
  }
  .empty__glyph-mark {
    font-size: 30px;
    line-height: 1;
    font-weight: var(--fw-regular);
    /* Optically center common single-glyph marks within the disc. */
    transform: translateY(-0.02em);
  }

  /* When there's a next step, the glyph warms to teal and gains a quiet halo —
     the one living accent points the eye toward the action below. */
  .empty--actionable .empty__glyph {
    color: var(--brand);
    box-shadow:
      inset 0 0 0 1px var(--border-brand-faint),
      0 0 0 6px var(--state-selected),
      var(--glow-live);
  }

  /* TEXT BLOCK — tight pairing of title + line so they read as one unit. */
  .empty__text {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-3);
    /* Stagger: text arrives just after the glyph for a settled, crafted feel. */
    animation: empty-settle var(--dur-slow) var(--ease-out) both;
    animation-delay: 60ms;
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

  /* ACTION — the single thing worth doing. A short hairline tether reaches up
     toward the text so the button reads as the resolution of the prompt, not a
     loose control floating in space. */
  .empty__action {
    position: relative;
    margin-top: var(--sp-4);
    animation: empty-settle var(--dur-slow) var(--ease-out) both;
    animation-delay: 120ms;
  }
  .empty__action::before {
    content: "";
    position: absolute;
    left: 50%;
    bottom: calc(100% + var(--sp-2));
    width: 1px;
    height: var(--sp-3);
    background: linear-gradient(to bottom, transparent, var(--border-brand-faint));
    transform: translateX(-50%);
  }

  @keyframes empty-rise {
    from {
      opacity: 0;
      transform: translateY(6px) scale(0.985);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }
  @keyframes empty-settle {
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
    .empty,
    .empty__text,
    .empty__action {
      animation: none;
    }
  }
</style>
