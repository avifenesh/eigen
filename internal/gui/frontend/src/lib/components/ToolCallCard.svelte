<script lang="ts">
  // The richest transcript element: a precise, collapsible record of one tool
  // invocation. The header is a calm sans row — a per-tool glyph, the tool NAME,
  // a one-line human SUMMARY (the path / command / pattern), a status dot, and a
  // disclosure chevron; collapsed by default. The body picks a renderer by tool
  // family: file mutations synthesize a unified diff (DiffView); read/output
  // tools show their RESULT in a CodeBlock with an inferred language and a
  // compact args line above; everything else falls back to pretty-printed args
  // and result CodeBlocks. The ONLY monospace surfaces are inside DiffView and
  // CodeBlock — every label, name, and summary here stays in --font-sans.
  import type { ToolBlock } from "$lib/stores/transcript.svelte";
  import StatusDot from "./StatusDot.svelte";
  import CodeBlock from "./CodeBlock.svelte";
  import DiffView from "./DiffView.svelte";

  // `open` is optionally CONTROLLED: when a parent passes it (e.g. ToolGroupCard
  // driving the live/auto-open tool), the card reflects that value and reports
  // clicks via `ontoggle` instead of owning the state. When `open` is omitted
  // the card keeps its own internal toggle (standalone usage). This lets a group
  // auto-open the running tool and collapse it once superseded, while a user
  // click still wins through the parent's override map.
  let { block, open: openProp, ontoggle }: { block: ToolBlock; open?: boolean; ontoggle?: () => void } = $props();
  let internalOpen = $state(false);
  const open = $derived(openProp ?? internalOpen);
  function toggle() {
    if (ontoggle) ontoggle();
    else internalOpen = !internalOpen;
  }

  // ── Status ────────────────────────────────────────────────────────────────
  // Indeterminate: a tool_result that never arrived (dropped on a full buffer)
  // — once a turn ends a still-undone tool reads as "result not received"
  // rather than spinning forever. We surface that as a quiet "no result" note
  // instead of a perpetual running pulse.
  // done-without-result is an indeterminate outcome, not a success — the dot
  // reads warn so it agrees with the amber "result not received" note below.
  const dotState = $derived(
    block.isError
      ? "error"
      : block.done
        ? block.result
          ? "ok"
          : "warn"
        : ("working" as const),
  );
  // The header glyph color must agree with the dot: a done-without-result tool
  // is indeterminate (warn), not a success (ok). Mirror dotState's three-way
  // logic so the glyph never signals green while the dot/note signal amber.
  const tone = $derived(
    block.isError
      ? "error"
      : block.done
        ? block.result
          ? "ok"
          : "warn"
        : "running",
  );

  // ── Tool family classification ──────────────────────────────────────────────
  // Normalize the daemon's tool name to a lowercase key so casing/aliases don't
  // matter, then route it to one of three render families.
  const key = $derived((block.name || "").toLowerCase().trim());

  const MUTATION = new Set([
    "edit",
    "multi_edit",
    "multiedit",
    "write",
    "patch",
    "apply_patch",
    "move",
    "rename",
  ]);
  const OUTPUT = new Set([
    "read",
    "list",
    "ls",
    "glob",
    "grep",
    "search",
    "tree",
    "symbols",
    "bash",
    "shell",
    "bashoutput",
    "bash_output",
    "fetch",
    "webfetch",
    "web_fetch",
  ]);

  type Family = "mutation" | "output" | "generic";
  const family = $derived<Family>(
    MUTATION.has(key) ? "mutation" : OUTPUT.has(key) ? "output" : "generic",
  );

  // ── Args parsing ────────────────────────────────────────────────────────────
  // Parse args once; everything downstream reads from this. A malformed/partial
  // args string (mid-stream tool_start) just yields null and we degrade to raw.
  const argsObj = $derived.by<Record<string, unknown> | null>(() => {
    const s = block.args;
    if (!s || !s.trim()) return null;
    try {
      const v = JSON.parse(s);
      return v && typeof v === "object" && !Array.isArray(v)
        ? (v as Record<string, unknown>)
        : null;
    } catch {
      return null;
    }
  });

  function str(v: unknown): string {
    return typeof v === "string" ? v : v == null ? "" : String(v);
  }
  function pick(obj: Record<string, unknown> | null, ...keys: string[]): string {
    if (!obj) return "";
    for (const k of keys) {
      const v = obj[k];
      if (v !== undefined && v !== null && v !== "") return str(v);
    }
    return "";
  }

  // The file path most tools operate on, under any of its common arg spellings.
  const argPath = $derived(
    pick(argsObj, "path", "file", "file_path", "filename", "filepath", "target"),
  );

  // ── Header glyph (per family / tool) ──────────────────────────────────────────
  // Plain unicode marks — never box-drawing — sized as a quiet leading sigil.
  const glyph = $derived.by<string>(() => {
    switch (key) {
      case "edit":
      case "multi_edit":
      case "multiedit":
      case "patch":
      case "apply_patch":
        return "✎";
      case "write":
        return "＋";
      case "move":
      case "rename":
        return "→";
      case "read":
        return "▤";
      case "list":
      case "ls":
      case "tree":
        return "▦";
      case "glob":
        return "✲";
      case "grep":
      case "search":
        return "⌕";
      case "symbols":
        return "❮❯";
      case "bash":
      case "shell":
      case "bashoutput":
      case "bash_output":
        return "❯";
      case "fetch":
      case "webfetch":
      case "web_fetch":
        return "↗";
      default:
        return "•";
    }
  });

  // ── One-line human summary (SANS) ────────────────────────────────────────────
  // Tool-aware: read→path, bash→command, grep→pattern (in dir), edit/write→path.
  // Falls back to the first line of args, then result, so a card is never blank.
  const summary = $derived.by<string>(() => {
    const o = argsObj;
    let s = "";
    switch (key) {
      case "bash":
      case "shell":
        s = pick(o, "command", "cmd", "script");
        break;
      case "bashoutput":
      case "bash_output":
        s = pick(o, "bash_id", "id", "command") || "stream output";
        break;
      case "grep":
      case "search": {
        const pat = pick(o, "pattern", "query", "regex", "q");
        const where = pick(o, "path", "dir", "include", "glob");
        s = where ? `${pat}  in ${where}` : pat;
        break;
      }
      case "glob":
        s = pick(o, "pattern", "glob", "query");
        break;
      case "move":
      case "rename": {
        const from = pick(o, "from", "source", "src", "old_path", "path");
        const to = pick(o, "to", "dest", "destination", "new_path");
        s = from && to ? `${from} → ${to}` : from || to;
        break;
      }
      case "fetch":
      case "webfetch":
      case "web_fetch":
        s = pick(o, "url", "uri", "href");
        break;
      default:
        s = argPath || pick(o, "pattern", "query", "command", "url");
    }
    if (!s) s = block.args || block.result || "";
    return firstLine(s);
  });

  function firstLine(s: string): string {
    const trimmed = s.replace(/^\s+/, "");
    const i = trimmed.indexOf("\n");
    const line = i >= 0 ? trimmed.slice(0, i) : trimmed;
    return line.length > 120 ? line.slice(0, 119) + "…" : line;
  }

  // ── Language inference (for the result CodeBlock) ─────────────────────────────
  // Bash-family results read as shell output; otherwise infer from the file
  // extension so a read of foo.ts highlights as TypeScript. Empty = plain text.
  const EXT_LANG: Record<string, string> = {
    ts: "typescript",
    tsx: "tsx",
    js: "javascript",
    jsx: "jsx",
    mjs: "javascript",
    cjs: "javascript",
    json: "json",
    go: "go",
    rs: "rust",
    py: "python",
    rb: "ruby",
    java: "java",
    kt: "kotlin",
    c: "c",
    h: "c",
    cpp: "cpp",
    cc: "cpp",
    hpp: "cpp",
    cs: "csharp",
    php: "php",
    swift: "swift",
    sh: "bash",
    bash: "bash",
    zsh: "bash",
    yml: "yaml",
    yaml: "yaml",
    toml: "toml",
    md: "markdown",
    css: "css",
    scss: "scss",
    html: "html",
    svelte: "svelte",
    sql: "sql",
    xml: "xml",
  };
  const resultLang = $derived.by<string | undefined>(() => {
    if (key === "bash" || key === "shell" || key === "bashoutput" || key === "bash_output")
      return "bash";
    const p = argPath;
    const dot = p.lastIndexOf(".");
    if (dot < 0 || dot === p.length - 1) return undefined;
    const ext = p.slice(dot + 1).toLowerCase();
    return EXT_LANG[ext];
  });

  // ── Pretty-printed args (generic family, and mutation fallback) ───────────────
  const prettyArgs = $derived.by<string>(() => {
    if (argsObj) return JSON.stringify(argsObj, null, 2);
    return block.args ?? "";
  });

  // ── Synthesized unified diff (mutation family) ────────────────────────────────
  // edit:        old_string → new_string as a single del/add hunk
  // multi_edit:  concatenate each edit's hunk
  // write:       all-additions of the new content
  // patch:       pass a raw unified diff straight through
  // We emit a minimal but well-formed unified diff with a `--- / +++` header so
  // DiffView's parser lights up its bands. Returns "" when no clean diff exists.
  const diffPatch = $derived.by<string>(() => {
    const o = argsObj;
    if (!o) return "";
    const path = argPath || "file";

    // A raw patch arg — hand it over verbatim.
    const rawPatch = pick(o, "patch", "diff", "unified_diff");
    if (rawPatch) return rawPatch;

    if (key === "write") {
      const content = pick(o, "content", "text", "body", "data");
      if (!content) return "";
      return writeDiff(path, content);
    }

    if (key === "edit") {
      const oldS = str(o["old_string"] ?? o["old"] ?? o["search"]);
      const newS = str(o["new_string"] ?? o["new"] ?? o["replace"]);
      if (!oldS && !newS) return "";
      return header(path) + editHunk(oldS, newS);
    }

    if (key === "multi_edit" || key === "multiedit") {
      const edits = o["edits"];
      if (!Array.isArray(edits) || edits.length === 0) return "";
      let out = header(path);
      let any = false;
      for (const e of edits) {
        if (!e || typeof e !== "object") continue;
        const rec = e as Record<string, unknown>;
        const oldS = str(rec["old_string"] ?? rec["old"] ?? rec["search"]);
        const newS = str(rec["new_string"] ?? rec["new"] ?? rec["replace"]);
        if (!oldS && !newS) continue;
        out += editHunk(oldS, newS);
        any = true;
      }
      return any ? out : "";
    }

    return "";
  });

  function header(path: string): string {
    return `--- a/${path}\n+++ b/${path}\n`;
  }
  function editHunk(oldS: string, newS: string): string {
    const oldLines = splitLines(oldS);
    const newLines = splitLines(newS);
    let h = `@@ -1,${oldLines.length} +1,${newLines.length} @@\n`;
    for (const l of oldLines) h += `-${l}\n`;
    for (const l of newLines) h += `+${l}\n`;
    return h;
  }
  function writeDiff(path: string, content: string): string {
    const lines = splitLines(content);
    let h = `--- /dev/null\n+++ b/${path}\n@@ -0,0 +1,${lines.length} @@\n`;
    for (const l of lines) h += `+${l}\n`;
    return h;
  }
  // Split into lines for diff bodies; a sole trailing newline is dropped so we
  // don't emit a spurious empty trailing change line. An empty string is zero
  // lines (not [""]) — a pure insertion (empty old_string) or empty write must
  // emit no del/add line and a "0" side in the @@ header, so editHunk/writeDiff
  // (which derive their counts from .length) stay honest and DiffView's diffstat
  // doesn't report a phantom change.
  function splitLines(s: string): string[] {
    if (s === "") return [];
    return s.replace(/\n$/, "").split("\n");
  }

  // Mutation cards prefer the synthesized diff; if we couldn't build one (no
  // recognizable args yet), fall back to showing the raw args as a CodeBlock.
  const hasDiff = $derived(family === "mutation" && diffPatch.trim().length > 0);

  type ChangeStats = { additions: number; deletions: number };
  const changeStats = $derived.by<ChangeStats>(() => diffStats(diffPatch));
  const showChangeStats = $derived(
    hasDiff && !block.isError && (changeStats.additions > 0 || changeStats.deletions > 0),
  );

  function diffStats(patch: string): ChangeStats {
    let additions = 0;
    let deletions = 0;
    const src = patch ?? "";
    if (!src.trim()) return { additions, deletions };
    for (const line of src.replace(/\n$/, "").split("\n")) {
      // File headers are metadata, not changed source lines.
      if (line.startsWith("+++") || line.startsWith("---")) continue;
      if (line.startsWith("+")) additions++;
      else if (line.startsWith("-")) deletions++;
    }
    return { additions, deletions };
  }

  // Whether the args block is worth showing as its own compact line in the
  // output family (we already echo the gist in the summary, so keep it terse).
  const showArgsLine = $derived(
    family === "output" && !!argsObj && Object.keys(argsObj).length > 0,
  );
  const argsLine = $derived.by<string>(() => {
    if (!argsObj) return "";
    const parts: string[] = [];
    for (const [k, v] of Object.entries(argsObj)) {
      if (v === undefined || v === null || v === "") continue;
      const val = typeof v === "object" ? JSON.stringify(v) : str(v);
      parts.push(`${k}: ${val.length > 80 ? val.slice(0, 79) + "…" : val}`);
    }
    return parts.join("   ");
  });

  // Result presence, distinct from "done": a done tool with no result string is
  // the indeterminate "result not received" case.
  const hasResult = $derived(
    block.result !== undefined && block.result !== null && block.result !== "",
  );
  const noResultAfterDone = $derived(block.done && !hasResult);
</script>

<div class="tool" class:tool--error={block.isError} class:tool--open={open}>
  <button
    class="tool__head"
    onclick={toggle}
    aria-expanded={open}
    title={summary || block.name}
  >
    <span class="tool__glyph tool__glyph--{tone}" aria-hidden="true">{glyph}</span>
    <span class="tool__name">{block.name || "tool"}</span>
    {#if summary}
      <span class="tool__summary">{summary}</span>
    {:else}
      <span class="tool__summary tool__summary--empty">—</span>
    {/if}
    {#if showChangeStats}
      <span
        class="tool__delta"
        title={`${changeStats.deletions} deletion${changeStats.deletions === 1 ? "" : "s"}, ${changeStats.additions} addition${changeStats.additions === 1 ? "" : "s"}`}
        aria-label={`${changeStats.deletions} deletion${changeStats.deletions === 1 ? "" : "s"}, ${changeStats.additions} addition${changeStats.additions === 1 ? "" : "s"}`}
      >
        <span class="tool__delta-del">−{changeStats.deletions}</span>
        <span class="tool__delta-add">+{changeStats.additions}</span>
      </span>
    {/if}
    <span class="tool__status">
      <StatusDot state={dotState} size={7} pulse={!block.done && !block.isError} />
    </span>
    <span class="tool__chevron" class:tool__chevron--open={open} aria-hidden="true">›</span>
  </button>

  {#if open}
    <div class="tool__body">
      {#if family === "mutation"}
        {#if hasDiff}
          <div class="tool__section-label">change</div>
          <div class="tool__render"><DiffView patch={diffPatch} /></div>
          {#if hasResult && block.isError}
            <div class="tool__section-label tool__section-label--error">error</div>
            <div class="tool__render">
              <CodeBlock code={block.result ?? ""} />
            </div>
          {/if}
        {:else}
          <!-- No clean diff synthesizable yet — show the raw args. -->
          <div class="tool__section-label">arguments</div>
          <div class="tool__render"><CodeBlock code={prettyArgs} lang="json" /></div>
          {#if hasResult}
            <div
              class="tool__section-label"
              class:tool__section-label--error={block.isError}
            >
              {block.isError ? "error" : "result"}
            </div>
            <div class="tool__render"><CodeBlock code={block.result ?? ""} /></div>
          {/if}
        {/if}
      {:else if family === "output"}
        {#if showArgsLine}
          <div class="tool__args-line" title={argsLine}>{argsLine}</div>
        {/if}
        {#if hasResult}
          <div
            class="tool__section-label"
            class:tool__section-label--error={block.isError}
          >
            {block.isError ? "error" : "result"}
          </div>
          <div class="tool__render">
            <CodeBlock code={block.result ?? ""} lang={block.isError ? undefined : resultLang} />
          </div>
        {/if}
      {:else}
        {#if prettyArgs}
          <div class="tool__section-label">arguments</div>
          <div class="tool__render"><CodeBlock code={prettyArgs} lang="json" /></div>
        {/if}
        {#if hasResult}
          <div
            class="tool__section-label"
            class:tool__section-label--error={block.isError}
          >
            {block.isError ? "error" : "result"}
          </div>
          <div class="tool__render"><CodeBlock code={block.result ?? ""} /></div>
        {/if}
      {/if}

      <!-- Indeterminate / pending states, shared across families. The running
           indicator reuses StatusDot so it matches the header dot and every
           other working signal in the app. -->
      {#if !block.done}
        <div class="tool__pending">
          <StatusDot state="working" size={7} pulse />
          running…
        </div>
      {:else if noResultAfterDone}
        <div class="tool__noresult">result not received</div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .tool {
    /* One-off geometry: the glyph rail width keeps the name column aligned
       whether or not a card is open. */
    --tool-rail: 18px;
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    overflow: hidden;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .tool--open {
    background: var(--bg-raised-2);
    border-color: var(--border-subtle);
  }
  .tool--error {
    border-color: color-mix(in srgb, var(--error) 30%, transparent);
  }
  .tool--error.tool--open {
    border-color: color-mix(in srgb, var(--error) 40%, transparent);
  }

  /* ── Header row (SANS) ──────────────────────────────────────────────────── */
  .tool__head {
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-4) var(--sp-5);
    background: transparent;
    border: none;
    cursor: pointer;
    text-align: left;
    color: var(--text-primary);
    font-family: var(--font-sans);
    border-radius: var(--r-md);
    transition: background var(--dur-instant) var(--ease-out);
  }
  .tool__head:hover {
    background: var(--state-hover);
  }
  /* The card clips its body (overflow: hidden), which would also clip a
     box-shadow ring drawn on the inner button. Paint the ring on the card
     itself via :focus-within — a shadow on the clipping element is not
     clipped by its own overflow — so the ring stays fully visible. */
  .tool__head:focus-visible {
    outline: none;
  }
  /* Keyboard-only ring (not on mouse click) — :focus-visible scoping is
     preserved by matching it via :has() on the clipping card. */
  .tool:has(.tool__head:focus-visible) {
    box-shadow: var(--shadow-focus);
  }

  .tool__glyph {
    flex: 0 0 var(--tool-rail);
    width: var(--tool-rail);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: var(--fs-body-sm);
    line-height: 1;
    color: var(--text-ghost);
    /* The glyph is the one place the card hints the tool family in color. */
    transition: color var(--dur-fast) var(--ease-out);
  }
  .tool__glyph--ok {
    color: var(--brand);
  }
  .tool__glyph--warn {
    color: var(--warn);
  }
  .tool__glyph--running {
    color: var(--working);
  }
  .tool__glyph--error {
    color: var(--error);
  }

  .tool__name {
    flex: 0 0 auto;
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body-sm);
    letter-spacing: var(--ls-normal);
  }
  .tool__summary {
    flex: 1 1 auto;
    min-width: 0;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tool__summary--empty {
    color: var(--text-faint);
  }
  .tool__delta {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: baseline;
    gap: var(--sp-2);
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    letter-spacing: var(--ls-normal);
    white-space: nowrap;
  }
  .tool__delta-del {
    color: var(--error);
  }
  .tool__delta-add {
    color: var(--success);
  }

  .tool__status {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
  }
  .tool__chevron {
    flex: 0 0 auto;
    color: var(--text-ghost);
    font-size: var(--fs-body);
    line-height: 1;
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .tool__chevron--open {
    transform: rotate(90deg);
    color: var(--text-muted);
  }

  /* ── Body ───────────────────────────────────────────────────────────────── */
  .tool__body {
    padding: 0 var(--sp-5) var(--sp-5);
    border-top: 1px solid var(--divider);
  }
  .tool__section-label {
    font-family: var(--font-sans);
    font-size: var(--fs-micro);
    font-weight: var(--fw-semibold);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    margin: var(--sp-5) 0 var(--sp-3);
  }
  .tool__section-label--error {
    color: var(--error);
  }
  .tool__render {
    /* The mono surfaces (CodeBlock / DiffView) live entirely inside here. */
    min-width: 0;
  }

  /* A compact, single-line echo of the call's args above an output result.
     SANS — it's chrome, not code; the result well below carries the mono. */
  .tool__args-line {
    margin-top: var(--sp-5);
    padding: var(--sp-3) var(--sp-4);
    background: var(--bg-inset);
    border-radius: var(--r-sm);
    font-family: var(--font-sans);
    font-size: var(--fs-label);
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* ── Pending / indeterminate notes ──────────────────────────────────────── */
  .tool__pending {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    margin-top: var(--sp-5);
    color: var(--working);
    font-family: var(--font-sans);
    font-size: var(--fs-body-sm);
  }
  .tool__noresult {
    margin-top: var(--sp-5);
    padding: var(--sp-3) var(--sp-4);
    background: var(--warn-bg);
    border-radius: var(--r-sm);
    font-family: var(--font-sans);
    font-size: var(--fs-label);
    color: var(--warn);
  }

  @media (prefers-reduced-motion: reduce) {
    .tool,
    .tool__head,
    .tool__glyph,
    .tool__chevron {
      transition: none;
    }
  }
</style>
