<script lang="ts">
  // A syntax-tinted code surface — one of the only monospace areas in the app
  // (alongside DiffView and inline <code>). A quiet sans header carries the
  // language label and a copy affordance; the body is mono on --syn-bg with a
  // cheap regex-based tint (no highlighter dependency), horizontal scroll for
  // long lines, and a capped height with vertical scroll. Purely presentational.
  import { onDestroy } from "svelte";

  let { code, lang }: { code: string; lang?: string } = $props();

  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  // Cancel the one-shot reset on unmount so it never writes to a detached state.
  onDestroy(() => clearTimeout(copyTimer));

  // ── Huge-blob guard ─────────────────────────────────────────────────────────
  // Tool results are stored uncapped, so a full-page extract or a base64 payload
  // can be tens of thousands of lines / megabytes. The regex tint walks the whole
  // string, and {@html} forces the browser to lay out every line — together that
  // freezes the UI on a single result. So: above either threshold we render only
  // a head slice (tinted to highlight just that slice) behind a "show full"
  // expander. The full highlight is opted into on demand, never up front.
  const MAX_LINES = 2000;
  const MAX_BYTES = 200_000;

  // Cheap measurements: byte length (UTF-8) and a line count derived from it.
  // Both are needed because either dimension alone can blow up the renderer.
  const byteLen = $derived(new Blob([code ?? ""]).size);
  const lineCount = $derived.by(() => {
    const src = code ?? "";
    if (src === "") return 0;
    let n = 1;
    for (let i = 0; i < src.length; i++) if (src.charCodeAt(i) === 10) n++;
    return n;
  });
  const tooLarge = $derived(lineCount > MAX_LINES || byteLen > MAX_BYTES);

  // User opt-in to the full, uncapped highlight. Reset whenever the source
  // changes so a freshly-swapped huge blob re-collapses instead of inheriting
  // the previous result's "expanded" choice.
  let expanded = $state(false);
  $effect(() => {
    code; // track the source
    expanded = false;
  });

  // What actually gets tinted + rendered: the whole thing when small or when the
  // user expanded, otherwise just the head slice.
  const rendered = $derived(tooLarge && !expanded ? headSlice(code ?? "") : (code ?? ""));

  function headSlice(src: string): string {
    let cut = src.length;
    let seen = 0;
    for (let i = 0; i < src.length; i++) {
      if (src.charCodeAt(i) === 10) {
        seen++;
        if (seen >= MAX_LINES) {
          cut = i;
          break;
        }
      }
    }
    // Also honor the byte cap so a few mega-long lines can't slip through.
    return src.slice(0, Math.min(cut, MAX_BYTES));
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(code);
      copied = true;
      clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = false), 1400);
    } catch {
      // Clipboard denied — leave the label unchanged rather than lie.
    }
  }

  // Cheap, defensive syntax tint. We escape first (never inject raw code as
  // HTML), then wrap a few lexical classes in tinted spans. Comments and strings
  // are matched first so keyword/number passes don't reach inside them.
  const COMMENT = /(\/\/[^\n]*|#[^\n]*|\/\*[\s\S]*?\*\/)/;
  const STRING = /("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`)/;
  const KEYWORDS = new RegExp(
    "\\b(?:func|function|return|if|else|for|range|while|switch|case|default|break|continue|" +
      "var|let|const|type|struct|interface|map|chan|go|defer|import|package|class|extends|" +
      "new|async|await|try|catch|finally|throw|export|from|def|elif|lambda|nil|null|none|" +
      "true|false|in|of|not|and|or|is|public|private|static|void|int|string|bool|float)\\b",
    "g",
  );
  const NUMBER = /\b(0x[0-9a-fA-F]+|\d+(?:\.\d+)?)\b/g;

  function esc(s: string): string {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  // Tokenize on comments+strings boundaries (kept verbatim, just tinted), then
  // tint keywords/numbers only in the remaining plain segments.
  const html = $derived.by(() => {
    const src = rendered;
    const splitter = new RegExp(`${COMMENT.source}|${STRING.source}`, "g");
    let out = "";
    let last = 0;
    let m: RegExpExecArray | null;
    while ((m = splitter.exec(src)) !== null) {
      out += tintPlain(src.slice(last, m.index));
      const tok = esc(m[0]);
      out += m[1]
        ? `<span class="t-comment">${tok}</span>`
        : `<span class="t-string">${tok}</span>`;
      last = m.index + m[0].length;
    }
    out += tintPlain(src.slice(last));
    return out;
  });

  function tintPlain(seg: string): string {
    let s = esc(seg);
    s = s.replace(KEYWORDS, '<span class="t-kw">$&</span>');
    s = s.replace(NUMBER, '<span class="t-num">$&</span>');
    return s;
  }

  const label = $derived((lang ?? "").trim().toLowerCase());
</script>

<div class="code">
  <div class="code__head">
    <span class="code__lang">{label || "text"}</span>
    <button class="code__copy" onclick={copy} title="Copy to clipboard" aria-label="Copy code">
      <span class="code__copy-icon" aria-hidden="true">{copied ? "✓" : "⧉"}</span>
      <span class="code__copy-label">{copied ? "copied" : "copy"}</span>
    </button>
  </div>
  <pre class="code__body selectable" class:code__body--capped={tooLarge && !expanded}><code>{@html html}</code></pre>
  {#if tooLarge}
    <button
      class="code__expand"
      onclick={() => (expanded = !expanded)}
      aria-expanded={expanded}
    >
      {#if expanded}
        <span class="code__expand-icon" aria-hidden="true">▲</span>
        <span>collapse</span>
      {:else}
        <span class="code__expand-icon" aria-hidden="true">▾</span>
        <span>show full ({lineCount.toLocaleString()} lines)</span>
      {/if}
    </button>
  {/if}
</div>

<style>
  .code {
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-sm);
    background: var(--syn-bg);
    overflow: hidden;
  }
  /* HEADER — sans chrome: language label + copy. Never monospace. */
  .code__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 28px;
    padding: 0 var(--sp-3) 0 var(--sp-4);
    background: var(--bg-raised);
    border-bottom: 1px solid var(--border-hairline);
  }
  .code__lang {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .code__copy {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    height: 20px;
    padding: 0 var(--sp-3);
    border: none;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--r-xs);
    cursor: pointer;
    font: var(--fw-medium) var(--fs-micro) / 1 var(--font-sans);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .code__copy:hover {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .code__copy:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .code__copy-icon {
    font-size: 12px;
    line-height: 1;
  }
  /* BODY — the permitted mono surface. */
  .code__body {
    margin: 0;
    padding: var(--sp-4) var(--sp-5);
    max-height: 360px;
    overflow: auto;
    font: var(--fw-regular) var(--fs-code-sm) / var(--lh-code) var(--font-mono);
    color: var(--syn-text);
    tab-size: var(--tab-size);
    white-space: pre;
  }
  .code__body code {
    font: inherit;
  }
  /* CAPPED — a huge blob is shown head-only; a faint bottom fade hints there's
     more below, and the expander beneath opts into the full highlight. The fade
     is a flat scrim (not teal — nothing here is "alive"), purely a depth cue. */
  .code__body--capped {
    position: relative;
    -webkit-mask-image: linear-gradient(180deg, #000 calc(100% - 28px), transparent);
    mask-image: linear-gradient(180deg, #000 calc(100% - 28px), transparent);
  }
  /* EXPANDER — sans chrome, like the copy affordance: a quiet faint control that
     warms to muted on hover. Full-width so it reads as a seam under the slice. */
  .code__expand {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--sp-2);
    width: 100%;
    padding: var(--sp-3) var(--sp-4);
    border: none;
    border-top: 1px solid var(--border-hairline);
    background: var(--bg-raised);
    color: var(--text-muted);
    cursor: pointer;
    font: var(--fw-medium) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .code__expand:hover {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .code__expand:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .code__expand-icon {
    font-size: 9px;
    line-height: 1;
    color: var(--text-faint);
  }
  /* TINT CLASSES */
  .code__body :global(.t-kw) {
    color: var(--syn-keyword);
  }
  .code__body :global(.t-string) {
    color: var(--syn-string);
  }
  .code__body :global(.t-num) {
    color: var(--syn-number);
  }
  .code__body :global(.t-comment) {
    color: var(--syn-comment);
    font-style: italic;
  }
</style>
