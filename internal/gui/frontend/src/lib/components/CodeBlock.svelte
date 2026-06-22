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
    const src = code ?? "";
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
  <pre class="code__body selectable"><code>{@html html}</code></pre>
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
