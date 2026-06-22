<script lang="ts">
  // Unified-diff renderer — the centerpiece detail. This should read BETTER than
  // a terminal diff, not the same: a quiet two-column body (narrow sign gutter +
  // the line), crisp add/del bands with a single bright edge marker instead of a
  // loud full-bleed wash, dim @@ hunk bands, and a sans diffstat eyebrow above.
  // Mono lives ONLY inside the diff body (the permitted code surface); the
  // diffstat, toggle, and hunk-range chrome are all sans.

  type Kind = "add" | "del" | "ctx" | "hunk" | "meta";
  interface Row {
    id: number;
    kind: Kind;
    sign: string; // the leading +/-/space, normalized for the gutter
    text: string; // line content with the leading marker stripped
  }

  let { patch }: { patch: string } = $props();

  // ── Parse ────────────────────────────────────────────────────────────────
  // One pass: classify every line. File headers (---/+++/diff/index) read as
  // quiet meta so a raw `git diff` paste renders gracefully, not as add/del.
  const rows = $derived.by<Row[]>(() => {
    const src = patch ?? "";
    if (!src.trim()) return [];
    const lines = src.replace(/\n$/, "").split("\n");
    const out: Row[] = [];
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      let kind: Kind;
      let sign = "";
      let text = line;
      if (line.startsWith("@@")) {
        kind = "hunk";
        text = line;
      } else if (
        line.startsWith("+++") ||
        line.startsWith("---") ||
        line.startsWith("diff ") ||
        line.startsWith("index ") ||
        line.startsWith("new file") ||
        line.startsWith("deleted file") ||
        line.startsWith("rename ") ||
        line.startsWith("similarity ") ||
        line.startsWith("\\ ")
      ) {
        kind = "meta";
        text = line;
      } else if (line.startsWith("+")) {
        kind = "add";
        sign = "+";
        text = line.slice(1);
      } else if (line.startsWith("-")) {
        kind = "del";
        sign = "−"; // U+2212 minus — optically matches the + weight
        text = line.slice(1);
      } else {
        kind = "ctx";
        text = line.startsWith(" ") ? line.slice(1) : line;
      }
      out.push({ id: i, kind, sign, text });
    }
    return out;
  });

  const additions = $derived(rows.filter((r) => r.kind === "add").length);
  const deletions = $derived(rows.filter((r) => r.kind === "del").length);
  const hunks = $derived(rows.filter((r) => r.kind === "hunk").length);
  const empty = $derived(rows.length === 0);

  // ── Collapse very large diffs ──────────────────────────────────────────────
  const LARGE = 400;
  const PREVIEW = 120; // lines shown while collapsed
  const isLarge = $derived(rows.length > LARGE);
  let expanded = $state(false);
  const visibleRows = $derived(
    isLarge && !expanded ? rows.slice(0, PREVIEW) : rows,
  );
  const hiddenCount = $derived(rows.length - visibleRows.length);

  // Humanize a hunk header — pull the `@@ -a,b +c,d @@` range out so the band
  // can show a calm sans label alongside the raw mono coordinates.
  function hunkRange(text: string): string {
    const m = text.match(/@@\s*(-\d+(?:,\d+)?\s+\+\d+(?:,\d+)?)\s*@@/);
    return m ? m[1] : text.replace(/@@/g, "").trim();
  }
  function hunkLabel(text: string): string {
    const m = text.match(/@@.*?@@\s?(.*)$/);
    return m && m[1] ? m[1].trim() : "";
  }
</script>

{#if empty}
  <div class="diff diff--empty">
    <span class="diff__empty-text">No changes</span>
  </div>
{:else}
  <figure class="diff">
    <figcaption class="diff__stat">
      <span class="diff__stat-counts">
        <span class="diff__stat-add">+{additions}</span>
        <span class="diff__stat-del">−{deletions}</span>
      </span>
      <span class="diff__stat-meta">
        {additions === 1 ? "1 addition" : `${additions} additions`},
        {deletions === 1 ? "1 deletion" : `${deletions} deletions`}
        {#if hunks > 1}<span class="diff__stat-dot">·</span>{hunks} hunks{/if}
      </span>
      <!-- Tiny proportional add/del bar — a glanceable sense of the balance. -->
      {#if additions + deletions > 0}
        <span class="diff__bar" aria-hidden="true">
          <span
            class="diff__bar-add"
            style="flex:{additions}"
          ></span>
          <span
            class="diff__bar-del"
            style="flex:{deletions}"
          ></span>
        </span>
      {/if}
    </figcaption>

    <div class="diff__scroll" class:diff__scroll--clipped={isLarge && !expanded}>
      <table class="diff__table">
        <tbody>
          {#each visibleRows as row (row.id)}
            {#if row.kind === "hunk"}
              <tr class="diff__row diff__row--hunk">
                <td class="diff__gutter" aria-hidden="true"></td>
                <td class="diff__line diff__line--hunk">
                  <span class="diff__hunk-range">{hunkRange(row.text)}</span>
                  {#if hunkLabel(row.text)}
                    <span class="diff__hunk-label">{hunkLabel(row.text)}</span>
                  {/if}
                </td>
              </tr>
            {:else if row.kind === "meta"}
              <tr class="diff__row diff__row--meta">
                <td class="diff__gutter" aria-hidden="true"></td>
                <td class="diff__line diff__line--meta">{row.text}</td>
              </tr>
            {:else}
              <tr class="diff__row diff__row--{row.kind}">
                <td class="diff__gutter" aria-hidden="true">{row.sign}</td>
                <td class="diff__line">{row.text}</td>
              </tr>
            {/if}
          {/each}
        </tbody>
      </table>

      {#if isLarge && !expanded}
        <div class="diff__fade" aria-hidden="true"></div>
      {/if}
    </div>

    {#if isLarge}
      <button
        type="button"
        class="diff__toggle"
        aria-expanded={expanded}
        onclick={() => (expanded = !expanded)}
      >
        {#if expanded}
          Collapse diff
        {:else}
          Show full diff
          <span class="diff__toggle-count">+{hiddenCount} more lines</span>
        {/if}
      </button>
    {/if}
  </figure>
{/if}

<style>
  .diff {
    margin: 0;
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--syn-bg);
    overflow: hidden;
  }
  .diff--empty {
    padding: var(--sp-5) var(--sp-6);
  }
  .diff__empty-text {
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-sans);
    color: var(--text-ghost);
  }

  /* ── Diffstat eyebrow (SANS) ───────────────────────────────────────────── */
  .diff__stat {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    padding: var(--sp-3) var(--sp-5);
    background: var(--bg-raised);
    border-bottom: 1px solid var(--divider);
    font-family: var(--font-sans);
    font-size: var(--fs-label);
    line-height: 1;
  }
  .diff__stat-counts {
    display: inline-flex;
    align-items: baseline;
    gap: var(--sp-3);
    font-weight: var(--fw-semibold);
    font-variant-numeric: tabular-nums;
  }
  .diff__stat-add {
    color: var(--success);
  }
  .diff__stat-del {
    color: var(--error);
  }
  .diff__stat-meta {
    color: var(--text-muted);
    font-weight: var(--fw-regular);
  }
  .diff__stat-dot {
    margin: 0 var(--sp-3);
    color: var(--text-faint);
  }
  /* Proportional balance bar — pushed to the far right. */
  .diff__bar {
    margin-left: auto;
    display: flex;
    width: var(--sp-11);
    height: 4px;
    border-radius: var(--r-full);
    overflow: hidden;
    background: var(--bg-inset);
  }
  .diff__bar-add {
    background: var(--success);
    min-width: 2px;
  }
  .diff__bar-del {
    background: var(--error);
    min-width: 2px;
  }

  /* ── Diff body (MONO — the permitted code surface) ─────────────────────── */
  .diff__scroll {
    position: relative;
    max-height: 520px;
    overflow: auto;
    tab-size: var(--tab-size);
    -moz-tab-size: var(--tab-size);
  }
  .diff__scroll--clipped {
    overflow: hidden;
  }
  .diff__table {
    width: 100%;
    border-collapse: collapse;
    font: var(--fw-regular) var(--fs-code-sm) / var(--lh-code) var(--font-mono);
    color: var(--syn-text);
  }
  /* Bands paint on the cells (.diff__gutter + .diff__line); the gutter marker
     provides the crisp left edge. */
  .diff__gutter {
    width: 1.75ch;
    padding: 0 var(--sp-3) 0 var(--sp-4);
    text-align: center;
    color: var(--text-ghost);
    user-select: none;
    white-space: nowrap;
    vertical-align: top;
    /* The fine vertical edge that separates sign from content. */
    border-right: 1px solid var(--border-hairline);
  }
  .diff__line {
    padding: 0 var(--sp-5) 0 var(--sp-5);
    white-space: pre;
    vertical-align: top;
    /* Wrap is opt-out by default — diffs read truest with horizontal scroll. */
  }

  /* Context — quiet, lets add/del carry the attention. */
  .diff__row--ctx .diff__line {
    color: var(--text-secondary);
  }

  /* Addition band — soft fill + one bright left edge on the gutter cell. */
  .diff__row--add .diff__gutter {
    background: var(--diff-add-bg);
    color: var(--success);
    font-weight: var(--fw-semibold);
    box-shadow: inset 2px 0 0 var(--diff-add-gutter);
    border-right-color: color-mix(in srgb, var(--diff-add-gutter) 40%, transparent);
  }
  .diff__row--add .diff__line {
    background: var(--diff-add-bg);
    color: var(--text-primary);
  }

  /* Deletion band. */
  .diff__row--del .diff__gutter {
    background: var(--diff-del-bg);
    color: var(--error);
    font-weight: var(--fw-semibold);
    box-shadow: inset 2px 0 0 var(--diff-del-gutter);
    border-right-color: color-mix(in srgb, var(--diff-del-gutter) 40%, transparent);
  }
  .diff__row--del .diff__line {
    background: var(--diff-del-bg);
    color: var(--text-primary);
  }

  /* Hunk header — its own dim band; range stays mono, label drops to sans. */
  .diff__row--hunk .diff__line--hunk {
    background: var(--bg-base);
    padding: var(--sp-2) var(--sp-5);
    border-top: 1px solid var(--divider);
    border-bottom: 1px solid var(--divider);
    color: var(--text-faint);
    white-space: normal;
  }
  .diff__row--hunk:first-child .diff__line--hunk {
    border-top: none;
  }
  .diff__row--hunk .diff__gutter {
    background: var(--bg-base);
    border-right: none;
    border-top: 1px solid var(--divider);
    border-bottom: 1px solid var(--divider);
  }
  .diff__hunk-range {
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }
  .diff__hunk-label {
    margin-left: var(--sp-5);
    font-family: var(--font-sans);
    font-size: var(--fs-micro);
    color: var(--text-ghost);
  }

  /* File-header / meta lines — quiet, never colored as a change. */
  .diff__row--meta .diff__line--meta {
    color: var(--text-ghost);
    background: var(--bg-base);
  }
  .diff__row--meta .diff__gutter {
    background: var(--bg-base);
    border-right: none;
  }

  /* ── Collapse affordance ───────────────────────────────────────────────── */
  .diff__fade {
    position: absolute;
    inset: auto 0 0 0;
    height: 56px;
    pointer-events: none;
    background: linear-gradient(
      to bottom,
      transparent,
      var(--syn-bg)
    );
  }
  .diff__toggle {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    width: 100%;
    padding: var(--sp-4) var(--sp-5);
    background: var(--bg-raised);
    border: none;
    border-top: 1px solid var(--divider);
    cursor: pointer;
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    color: var(--brand);
    transition:
      background var(--dur-instant) var(--ease-out),
      color var(--dur-instant) var(--ease-out);
  }
  .diff__toggle:hover {
    background: var(--bg-raised-2);
    color: var(--brand-bright);
  }
  .diff__toggle:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .diff__toggle-count {
    margin-left: auto;
    font-weight: var(--fw-regular);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }

  @media (prefers-reduced-motion: reduce) {
    .diff__toggle {
      transition: none;
    }
  }
</style>
