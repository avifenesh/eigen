<script lang="ts">
  // Assistant prose → real Markdown, rendered in SANS. This is the one surface
  // where the model's words become typeset text, so it must read like an
  // essay, not a terminal dump: generous paragraph rhythm, scaled headings,
  // quiet rules, and code that hands off to the dedicated mono surface.
  //
  // SAFETY: we never feed unsanitized model HTML to the DOM. Instead of
  // {@html}, we walk marked's token tree and emit native Svelte markup —
  // every text node and stray `<tag>` from the source is auto-escaped by
  // Svelte's interpolation, so there is no injection path. Fenced code is the
  // ONLY place monospace appears at block level, and it delegates to CodeBlock.
  import type { Snippet } from "svelte";
  import { marked, type Token, type Tokens } from "marked";
  import { Browser } from "@wailsio/runtime";
  import CodeBlock from "./CodeBlock.svelte";

  let { source }: { source: string } = $props();

  // Lex to a token tree (no HTML output). Configured to NOT trust raw HTML —
  // marked still emits `html`/`text` tokens, but we render their literal text.
  const tokens = $derived.by<Token[]>(() => {
    if (!source) return [];
    try {
      return marked.lexer(source, { gfm: true, breaks: false });
    } catch {
      return [];
    }
  });

  function openLink(e: MouseEvent, href: string | undefined) {
    e.preventDefault();
    if (!href) return;
    try {
      Browser.OpenURL(href);
    } catch {
      try {
        window.open(href, "_blank", "noopener,noreferrer");
      } catch {
        /* swallow — never navigate the host webview away */
      }
    }
  }

  // Only allow href schemes that can't navigate/execute in-app.
  function safeHref(href: string | undefined): string | undefined {
    if (!href) return undefined;
    const v = href.trim();
    if (/^(https?:|mailto:|tel:)/i.test(v)) return v;
    if (v.startsWith("#") || v.startsWith("/") || v.startsWith("./")) return v;
    return undefined;
  }
</script>

<!-- INLINE: recursive walk over inline tokens (strong/em/code/link/…). -->
{#snippet inline(toks: Token[] | undefined)}
  {#if toks}
    {#each toks as tok, i (i)}
      {#if tok.type === "text"}
        {#if (tok as Tokens.Text).tokens?.length}
          {@render inline((tok as Tokens.Text).tokens)}
        {:else}{(tok as Tokens.Text).text}{/if}
      {:else if tok.type === "escape"}
        {(tok as Tokens.Escape).text}
      {:else if tok.type === "strong"}
        <strong>{@render inline((tok as Tokens.Strong).tokens)}</strong>
      {:else if tok.type === "em"}
        <em>{@render inline((tok as Tokens.Em).tokens)}</em>
      {:else if tok.type === "del"}
        <del>{@render inline((tok as Tokens.Del).tokens)}</del>
      {:else if tok.type === "codespan"}
        <code class="md-code">{(tok as Tokens.Codespan).text}</code>
      {:else if tok.type === "br"}
        <br />
      {:else if tok.type === "link"}
        {@const lk = tok as Tokens.Link}
        {@const href = safeHref(lk.href)}
        {#if href}
          <a class="md-link" {href} onclick={(e) => openLink(e, href)}>
            {@render inline(lk.tokens)}
          </a>
        {:else}{@render inline(lk.tokens)}{/if}
      {:else if tok.type === "image"}
        <!-- Render alt text only; never load remote model-supplied images. -->
        <span class="md-img">{(tok as Tokens.Image).text || "image"}</span>
      {:else if "tokens" in tok && (tok as { tokens?: Token[] }).tokens}
        {@render inline((tok as { tokens?: Token[] }).tokens)}
      {:else if "text" in tok}
        {(tok as { text: string }).text}
      {:else if "raw" in tok}
        {(tok as { raw: string }).raw}
      {/if}
    {/each}
  {/if}
{/snippet}

<!-- BLOCK: recursive walk over block tokens; reused for blockquote/list bodies. -->
{#snippet block(toks: Token[] | undefined)}
  {#if toks}
    {#each toks as tok, i (i)}
      {#if tok.type === "heading"}
        {@const h = tok as Tokens.Heading}
        {#if h.depth <= 1}
          <h1 class="md-h md-h1">{@render inline(h.tokens)}</h1>
        {:else if h.depth === 2}
          <h2 class="md-h md-h2">{@render inline(h.tokens)}</h2>
        {:else if h.depth === 3}
          <h3 class="md-h md-h3">{@render inline(h.tokens)}</h3>
        {:else}
          <h4 class="md-h md-h4">{@render inline(h.tokens)}</h4>
        {/if}
      {:else if tok.type === "paragraph"}
        <p class="md-p">{@render inline((tok as Tokens.Paragraph).tokens)}</p>
      {:else if tok.type === "blockquote"}
        <blockquote class="md-quote">
          {@render block((tok as Tokens.Blockquote).tokens)}
        </blockquote>
      {:else if tok.type === "list"}
        {@const list = tok as Tokens.List}
        {#if list.ordered}
          <ol class="md-list md-ol" start={Number(list.start) || 1}>
            {#each list.items as item, j (j)}
              {@render listItem(item)}
            {/each}
          </ol>
        {:else}
          <ul class="md-list md-ul">
            {#each list.items as item, j (j)}
              {@render listItem(item)}
            {/each}
          </ul>
        {/if}
      {:else if tok.type === "code"}
        {@const c = tok as Tokens.Code}
        <div class="md-codeblock">
          <CodeBlock code={c.text} lang={c.lang || undefined} />
        </div>
      {:else if tok.type === "table"}
        {@render table(tok as Tokens.Table)}
      {:else if tok.type === "hr"}
        <hr class="md-hr" />
      {:else if tok.type === "html"}
        <!-- Raw HTML from the model: emit its literal text (auto-escaped). -->
        {#if (tok as Tokens.HTML).text.trim()}
          <p class="md-p">{(tok as Tokens.HTML).text}</p>
        {/if}
      {:else if tok.type === "space"}
        <!-- intentional blank: paragraph rhythm comes from CSS margins -->
      {:else if "tokens" in tok && (tok as { tokens?: Token[] }).tokens}
        <p class="md-p">{@render inline((tok as { tokens?: Token[] }).tokens)}</p>
      {:else if "text" in tok}
        <p class="md-p">{(tok as { text: string }).text}</p>
      {/if}
    {/each}
  {/if}
{/snippet}

{#snippet listItem(item: Tokens.ListItem)}
  <li class="md-li" class:md-li--task={item.task}>
    {#if item.task}
      <span
        class="md-task"
        class:md-task--done={item.checked}
        aria-hidden="true"
      ></span>
    {/if}
    <span class="md-li__body">{@render block(item.tokens)}</span>
  </li>
{/snippet}

{#snippet table(t: Tokens.Table)}
  <div class="md-table-wrap">
    <table class="md-table">
      <thead>
        <tr>
          {#each t.header as cell, ci (ci)}
            <th style:text-align={t.align[ci] || "left"}>
              {@render inline(cell.tokens)}
            </th>
          {/each}
        </tr>
      </thead>
      <tbody>
        {#each t.rows as row, ri (ri)}
          <tr>
            {#each row as cell, ci (ci)}
              <td style:text-align={t.align[ci] || "left"}>
                {@render inline(cell.tokens)}
              </td>
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/snippet}

<div class="md">
  {@render block(tokens)}
</div>

<style>
  /* The prose container sets the reading rhythm; children inherit the sans
     stack and only override what makes each block distinct. */
  .md {
    font-family: var(--font-sans);
    font-size: var(--fs-body);
    line-height: var(--lh-prose);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }
  /* Collapse the outer margins so the block hugs its container; inner
     spacing between siblings carries the rhythm. */
  .md > :global(:first-child) {
    margin-top: 0;
  }
  .md > :global(:last-child) {
    margin-bottom: 0;
  }

  /* HEADINGS — scaled via type tokens, tight tracking, a touch more air above. */
  .md :global(.md-h) {
    font-weight: var(--fw-semibold);
    line-height: var(--lh-snug);
    letter-spacing: var(--ls-heading);
    color: var(--text-primary);
    margin: var(--sp-8) 0 var(--sp-4);
  }
  .md :global(.md-h1) {
    font-size: var(--fs-h1);
  }
  .md :global(.md-h2) {
    font-size: var(--fs-h2);
    padding-bottom: var(--sp-3);
    border-bottom: 1px solid var(--divider);
  }
  .md :global(.md-h3) {
    font-size: var(--fs-h3);
  }
  .md :global(.md-h4) {
    font-size: var(--fs-body);
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }

  /* PARAGRAPHS — the generous, readable default. */
  .md :global(.md-p) {
    margin: var(--sp-5) 0;
  }

  /* EMPHASIS */
  .md :global(strong) {
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }
  .md :global(em) {
    font-style: italic;
  }
  .md :global(del) {
    color: var(--text-muted);
    text-decoration-color: var(--text-ghost);
  }

  /* INLINE CODE — a subtle inset chip; the only inline mono surface. */
  .md :global(.md-code) {
    font-family: var(--font-mono);
    font-size: var(--fs-code-sm);
    background: var(--bg-inset);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-xs);
    padding: 0.08em 0.36em;
    color: var(--syn-text);
    white-space: break-spaces;
    word-break: break-word;
  }

  /* LINKS — accent, underline-on-intent; opened in the system browser. */
  .md :global(.md-link) {
    color: var(--accent);
    text-decoration: none;
    text-underline-offset: 0.16em;
    border-radius: var(--r-xs);
    transition: color var(--dur-instant) var(--ease-out);
  }
  .md :global(.md-link:hover) {
    color: var(--accent-bright);
    text-decoration: underline;
  }
  .md :global(.md-link:focus-visible) {
    outline: none;
    box-shadow: var(--shadow-focus);
  }

  .md :global(.md-img) {
    color: var(--text-muted);
    font-style: italic;
  }

  /* BLOCKQUOTE — left brand rule, quieted text, inset slightly. */
  .md :global(.md-quote) {
    margin: var(--sp-6) 0;
    padding: var(--sp-2) 0 var(--sp-2) var(--sp-6);
    border-left: 2px solid var(--border-brand-faint);
    color: var(--text-secondary);
  }
  .md :global(.md-quote .md-p) {
    margin: var(--sp-3) 0;
  }

  /* LISTS — restrained indent, comfortable item spacing. */
  .md :global(.md-list) {
    margin: var(--sp-5) 0;
    padding-left: var(--sp-7);
  }
  .md :global(.md-ul) {
    list-style: none;
    padding-left: var(--sp-6);
  }
  .md :global(.md-ol) {
    list-style: decimal;
  }
  .md :global(.md-li) {
    margin: var(--sp-2) 0;
    padding-left: var(--sp-1);
  }
  .md :global(.md-ol .md-li) {
    padding-left: var(--sp-2);
  }
  .md :global(.md-ol .md-li::marker) {
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }
  /* Custom bullet for unordered items: a small brand tick, vertically centered. */
  .md :global(.md-ul > .md-li:not(.md-li--task)) {
    position: relative;
  }
  .md :global(.md-ul > .md-li:not(.md-li--task))::before {
    content: "";
    position: absolute;
    left: calc(-1 * var(--sp-5));
    top: 0.66em;
    width: 4px;
    height: 4px;
    border-radius: var(--r-full);
    background: var(--brand-dim);
  }
  /* Body wrapper lets a list item hold multiple blocks without breaking flow. */
  .md :global(.md-li__body > .md-p:first-child) {
    margin-top: 0;
  }
  .md :global(.md-li__body > .md-p:last-child) {
    margin-bottom: 0;
  }
  /* nested lists snug up against their parent item */
  .md :global(.md-li .md-list) {
    margin: var(--sp-2) 0;
  }

  /* TASK LIST checkboxes — read-only glyphs, never interactive here. */
  .md :global(.md-li--task) {
    list-style: none;
    display: flex;
    align-items: baseline;
    gap: var(--sp-3);
  }
  .md :global(.md-li--task)::before {
    content: none;
  }
  .md :global(.md-task) {
    flex: none;
    position: relative;
    top: 0.18em;
    width: 13px;
    height: 13px;
    border: 1px solid var(--border-strong);
    border-radius: var(--r-xs);
    background: var(--bg-inset);
  }
  .md :global(.md-task--done) {
    background: var(--brand-dim);
    border-color: var(--brand);
  }
  .md :global(.md-task--done)::after {
    content: "";
    position: absolute;
    left: 4px;
    top: 1px;
    width: 3px;
    height: 6px;
    border: solid var(--text-on-brand);
    border-width: 0 1.5px 1.5px 0;
    transform: rotate(45deg);
  }

  /* FENCED CODE — delegated to CodeBlock; just give it breathing room. */
  .md :global(.md-codeblock) {
    margin: var(--sp-6) 0;
  }

  /* HR — a quiet hairline, not a heavy bar. */
  .md :global(.md-hr) {
    margin: var(--sp-7) 0;
    border: none;
    border-top: 1px solid var(--divider);
  }

  /* TABLES — raised surface, hairline grid, header on a deeper tint. */
  .md :global(.md-table-wrap) {
    margin: var(--sp-6) 0;
    overflow-x: auto;
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
  }
  .md :global(.md-table) {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-body-sm);
  }
  .md :global(.md-table th),
  .md :global(.md-table td) {
    padding: var(--sp-3) var(--sp-5);
    border-bottom: 1px solid var(--divider);
    border-right: 1px solid var(--divider);
  }
  .md :global(.md-table th:last-child),
  .md :global(.md-table td:last-child) {
    border-right: none;
  }
  .md :global(.md-table tbody tr:last-child td) {
    border-bottom: none;
  }
  .md :global(.md-table thead th) {
    background: var(--bg-raised-2);
    color: var(--text-secondary);
    font-weight: var(--fw-semibold);
    font-size: var(--fs-label);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }
  .md :global(.md-table tbody tr:nth-child(even)) {
    background: var(--bg-inset);
  }

  @media (prefers-reduced-motion: reduce) {
    .md :global(.md-link) {
      transition: none;
    }
  }
</style>
