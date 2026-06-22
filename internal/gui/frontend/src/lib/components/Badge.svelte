<script lang="ts">
  // A pill tag: a tone-coded label that sits legibly on raised surfaces.
  // Two duties — naming things (model ids, statuses) and counting things
  // (numeric counters). `truncate` clamps wide content (e.g. a full model
  // id like "us.anthropic.claude-opus-4-8") so it never blows out a row;
  // the caller is expected to pass a matching `title` on a wrapper for the
  // full text. Numeric content stays tabular so counts don't jitter.
  import type { Snippet } from "svelte";

  let {
    tone = "neutral",
    truncate = false,
    children,
  }: {
    tone?: "neutral" | "brand" | "success" | "warn" | "error" | "info";
    truncate?: boolean;
    children: Snippet;
  } = $props();
</script>

<span class="badge badge--{tone} tnum" class:badge--truncate={truncate}>
  <span class="badge__label">{@render children()}</span>
</span>

<style>
  .badge {
    /* Pill geometry: deliberate at --fs-micro. Fixed height keeps a row of
       mixed badges optically aligned; padding is a touch heavier on the x to
       balance the full radius so text never kisses the curve. The 1px hairline
       border per tone gives each pill a crisp edge on raised surfaces. */
    display: inline-flex;
    align-items: center;
    max-width: 100%;
    height: 18px;
    padding: 0 var(--sp-3);
    border-radius: var(--r-full);
    border: 1px solid transparent;
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    /* Tight tracking — small caps-height labels read denser and cleaner. */
    letter-spacing: 0.005em;
    white-space: nowrap;
    vertical-align: middle;
    box-sizing: border-box;
  }

  .badge__label {
    /* Optical vertical centering: Inter sits a hair high in a fixed box at
       this size; nudge the glyph baseline down so the text is visually
       centered in the pill rather than mathematically centered. */
    display: block;
    transform: translateY(0.5px);
    min-width: 0;
  }

  /* TRUNCATION — clamp wide content (model ids) and ellipsize. The flex item
     must be allowed to shrink (min-width:0) for ellipsis to engage. */
  .badge--truncate {
    max-width: 16ch;
  }
  .badge--truncate .badge__label {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* TONES — each tuned to stay legible on --bg-raised / --bg-raised-2.
     Neutral leans on an overlay fill + hairline; semantic tones use their
     translucent *-bg fills with a faint same-hue edge for definition. */
  .badge--neutral {
    background: var(--bg-overlay);
    color: var(--text-secondary);
    border-color: var(--border-hairline);
  }
  .badge--brand {
    background: var(--state-selected);
    color: var(--brand-bright);
    border-color: var(--border-brand-faint);
  }
  .badge--success {
    background: var(--success-bg);
    color: var(--success);
    border-color: color-mix(in srgb, var(--success) 22%, transparent);
  }
  .badge--warn {
    background: var(--warn-bg);
    color: var(--warn);
    border-color: color-mix(in srgb, var(--warn) 22%, transparent);
  }
  .badge--error {
    background: var(--error-bg);
    color: var(--error);
    border-color: color-mix(in srgb, var(--error) 24%, transparent);
  }
  .badge--info {
    background: var(--info-bg);
    color: var(--info);
    border-color: color-mix(in srgb, var(--info) 22%, transparent);
  }
</style>
