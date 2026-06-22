<script lang="ts">
  // A tool invocation in the transcript. The tool NAME and a one-line human
  // summary render in sans; the raw args/result body sits in a collapsed mono
  // well (the only monospace surface here). Status dot reflects running /
  // done / error / indeterminate.
  import type { ToolBlock } from "$lib/stores/transcript.svelte";
  import StatusDot from "./StatusDot.svelte";

  let { block }: { block: ToolBlock } = $props();
  let open = $state(false);

  // Indeterminate: a tool_result that never arrived (dropped on a full buffer)
  // — after the turn ends a still-undone tool is shown as "result not received"
  // rather than spinning forever. We approximate via done flag + presence.
  const dotState = $derived(
    block.isError ? "error" : block.done ? "ok" : "working",
  );
  const summary = $derived(firstLine(block.args || block.result || ""));

  function firstLine(s: string): string {
    const i = s.indexOf("\n");
    const line = i >= 0 ? s.slice(0, i) : s;
    return line.length > 80 ? line.slice(0, 80) + "…" : line;
  }
  const body = $derived(prettyArgs(block.args));
  function prettyArgs(s: string): string {
    if (!s) return "";
    try {
      return JSON.stringify(JSON.parse(s), null, 2);
    } catch {
      return s;
    }
  }
</script>

<div class="tool" class:tool--error={block.isError}>
  <button class="tool__head" onclick={() => (open = !open)} aria-expanded={open}>
    <StatusDot state={dotState} size={7} />
    <span class="tool__name">{block.name}</span>
    {#if summary}<span class="tool__summary">{summary}</span>{/if}
    <span class="tool__chevron" class:tool__chevron--open={open} aria-hidden="true">›</span>
  </button>
  {#if open}
    <div class="tool__body">
      {#if body}
        <div class="tool__section-label">arguments</div>
        <pre class="tool__well selectable">{body}</pre>
      {/if}
      {#if block.result !== undefined}
        <div class="tool__section-label">{block.isError ? "error" : "result"}</div>
        <pre class="tool__well selectable" class:tool__well--error={block.isError}>{block.result}</pre>
      {:else if block.done === false}
        <div class="tool__pending">running…</div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .tool {
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    overflow: hidden;
  }
  .tool--error {
    border-color: var(--error-bg);
  }
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
  }
  .tool__head:hover {
    background: var(--state-hover);
  }
  .tool__head:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .tool__name {
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body-sm);
  }
  .tool__summary {
    flex: 1;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tool__chevron {
    color: var(--text-ghost);
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .tool__chevron--open {
    transform: rotate(90deg);
  }
  .tool__body {
    padding: 0 var(--sp-5) var(--sp-5);
    border-top: 1px solid var(--divider);
  }
  .tool__section-label {
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    margin: var(--sp-5) 0 var(--sp-2);
  }
  .tool__well {
    margin: 0;
    padding: var(--sp-4);
    background: var(--syn-bg);
    border-radius: var(--r-sm);
    font: var(--fw-regular) var(--fs-code-sm) / var(--lh-code) var(--font-mono);
    color: var(--syn-text);
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 320px;
    overflow: auto;
    tab-size: var(--tab-size);
  }
  .tool__well--error {
    color: var(--error);
  }
  .tool__pending {
    margin-top: var(--sp-4);
    color: var(--working);
    font-size: var(--fs-body-sm);
  }
</style>
