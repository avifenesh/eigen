<script lang="ts">
  // A folded run of consecutive tool calls — the "N tools" row. Collapsed it is
  // one calm summary line (count + aggregate status + the tools' glyphs); opened
  // it lists every tool in the run, each an independently-expandable ToolCallCard.
  //
  // Live behavior (the point of the feature): a tool that is RUNNING — or the
  // one that just finished and hasn't yet been superseded by the next tool call
  // or a text/reasoning stream — is shown OPEN (its detail visible) so the user
  // watches it work. The instant the next tool starts or the model streams prose,
  // it collapses back to the one-line summary (see showLive). The whole group
  // also auto-expands while it holds that live tool, then the user is free to
  // collapse it. Per-tool and per-group open are user-overridable and the
  // overrides are keyed by uid so they survive re-derive / CAP eviction.
  import type { ToolGroup, ToolBlock } from "$lib/stores/transcript.svelte";
  import ToolCallCard from "./ToolCallCard.svelte";
  import StatusDot from "./StatusDot.svelte";

  let {
    group,
    // True when the model is streaming text/reasoning into the live block right
    // now: a live stream "supersedes" the last tool, collapsing its detail.
    streaming = false,
    // True when this group is the LAST row in the transcript history. Only the
    // tail group can hold the "live / just-run" tool whose detail auto-shows; a
    // group with anything after it (a message, another group) is history and
    // collapses by default.
    isTail = false,
    // True while THIS session has an in-flight turn (running or just-sent). Gates
    // the "just-finished tail tool stays open" rule so a COLD-loaded idle
    // transcript (re-opened later) shows every group collapsed instead of
    // auto-expanding its last tool run. A genuinely running tool ignores this.
    turnActive = false,
    // Per-tool explicit open overrides (uid -> open) shared across the view so
    // a user's choice persists across re-derives. Group toggle override too.
    toolOpen,
    groupOpen,
    onToolToggle,
    onGroupToggle,
  }: {
    group: ToolGroup;
    streaming?: boolean;
    isTail?: boolean;
    turnActive?: boolean;
    toolOpen: Partial<Record<number, boolean>>;
    groupOpen: Partial<Record<number, boolean>>;
    onToolToggle: (uid: number, open: boolean) => void;
    onGroupToggle: (uid: number, open: boolean) => void;
  } = $props();

  const tools = $derived(group.tools);
  const count = $derived(tools.length);
  // Defensive cap: a consecutive run is normally a handful of calls, but a
  // pathological turn could emit very many. VirtualList windows ROWS, not the
  // cards inside one row, so render at most the most recent RENDER_CAP tools
  // when expanded; older ones collapse into a count note. Keeps one giant group
  // from mounting hundreds of cards in a single virtual row.
  const RENDER_CAP = 60;
  const overflow = $derived(Math.max(0, count - RENDER_CAP));
  const shownTools = $derived(overflow > 0 ? tools.slice(count - RENDER_CAP) : tools);

  // The "live" tool of the run: the last tool still running (not done), else —
  // if nothing is running — the last tool, treated as just-finished. It is the
  // one the auto-open rule targets. (Tools run strictly sequentially, so at most
  // one is undone at a time and the last is always the newest.)
  const liveTool = $derived.by<ToolBlock | null>(() => {
    for (let i = tools.length - 1; i >= 0; i--) {
      if (!tools[i].done) return tools[i];
    }
    return tools[tools.length - 1] ?? null;
  });
  // Whether the run still has work in flight (drives the auto-open + group dot).
  const anyRunning = $derived(tools.some((t) => !t.done));
  const anyError = $derived(tools.some((t) => t.isError));

  // The live tool's detail shows until SUPERSEDED. It is "live" while a tool is
  // actually running (anyRunning) — the turn is mid-run — OR while this run is
  // the transcript tail of an ACTIVE turn and the model hasn't moved on to prose
  // (isTail && turnActive && !streaming). Once a message/reasoning follows
  // (isTail=false) or the model streams (streaming) or the turn ends
  // (!turnActive), the detail folds away — exactly "open until the next tool
  // call or a stream collapses it". A cold-loaded idle transcript never
  // auto-opens (turnActive=false).
  const showLive = $derived(anyRunning || (isTail && turnActive && !streaming));

  // Group expansion: explicit user choice wins; otherwise auto-expand while the
  // run is live (so you watch it work), collapsed once it is history.
  const groupExpanded = $derived(groupOpen[group.uid] ?? showLive);

  // Resolve a single tool's open state: explicit user override wins; else the
  // live tool auto-opens while the run is live; else collapsed.
  function toolIsOpen(t: ToolBlock): boolean {
    const ov = toolOpen[t.uid];
    if (ov !== undefined) return ov;
    return t === liveTool && showLive;
  }

  const dotState = $derived(
    anyError ? "error" : anyRunning ? ("working" as const) : "ok",
  );

  // Distinct tool glyphs in the run, for a calm at-a-glance preview on the
  // collapsed header (deduped, capped). Mirrors ToolCallCard's glyph mapping but
  // only the few we want as a sigil row; unknown tools fall back to a dot.
  function glyphFor(name: string): string {
    const k = (name || "").toLowerCase().trim();
    if (k === "edit" || k === "multi_edit" || k === "multiedit" || k === "patch" || k === "apply_patch") return "✎";
    if (k === "write") return "＋";
    if (k === "move" || k === "rename") return "→";
    if (k === "read") return "▤";
    if (k === "list" || k === "ls" || k === "tree") return "▦";
    if (k === "glob") return "✲";
    if (k === "grep" || k === "search") return "⌕";
    if (k === "bash" || k === "shell" || k === "bashoutput" || k === "bash_output") return "❯";
    if (k === "fetch" || k === "webfetch" || k === "web_fetch") return "↗";
    if (k === "todo") return "☑";
    return "•";
  }
  const previewGlyphs = $derived.by<string[]>(() => {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const t of tools) {
      const g = glyphFor(t.name);
      if (seen.has(g)) continue;
      seen.add(g);
      out.push(g);
      if (out.length >= 6) break;
    }
    return out;
  });
  // Compact names line for the collapsed summary (first few, deduped).
  const namesLine = $derived.by<string>(() => {
    const names: string[] = [];
    const seen = new Set<string>();
    for (const t of tools) {
      const n = t.name || "tool";
      if (seen.has(n)) continue;
      seen.add(n);
      names.push(n);
      if (names.length >= 4) break;
    }
    const more = count - tools.filter((t) => names.includes(t.name || "tool")).length;
    return names.join(", ") + (more > 0 ? `, +${more}` : "");
  });
</script>

{#if count === 1}
  <!-- A lone tool needs no group chrome: render it directly, but still honor the
       auto-open (live until superseded) and the shared per-tool override so it
       behaves identically to a tool inside a multi-run group. -->
  {@const t = tools[0]}
  <ToolCallCard block={t} open={toolIsOpen(t)} ontoggle={() => onToolToggle(t.uid, !toolIsOpen(t))} />
{:else}
  <div class="tg" class:tg--open={groupExpanded} class:tg--error={anyError}>
    <button
      class="tg__head"
      onclick={() => onGroupToggle(group.uid, !groupExpanded)}
      aria-expanded={groupExpanded}
      title={namesLine}
    >
      <span class="tg__glyphs" aria-hidden="true">
        {#each previewGlyphs as g (g)}<span class="tg__glyph">{g}</span>{/each}
      </span>
      <span class="tg__count">{count} tools</span>
      <span class="tg__names">{namesLine}</span>
      <span class="tg__status">
        <StatusDot state={dotState} size={7} pulse={anyRunning} />
      </span>
      <span class="tg__chevron" class:tg__chevron--open={groupExpanded} aria-hidden="true">›</span>
    </button>

    {#if groupExpanded}
      <div class="tg__body">
        {#if overflow > 0}
          <div class="tg__overflow">{overflow} earlier tool{overflow === 1 ? "" : "s"} hidden</div>
        {/if}
        {#each shownTools as t (t.uid)}
          <ToolCallCard block={t} open={toolIsOpen(t)} ontoggle={() => onToolToggle(t.uid, !toolIsOpen(t))} />
        {/each}
      </div>
    {/if}
  </div>
{/if}

<style>
  .tg {
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    overflow: hidden;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .tg--open {
    background: var(--bg-raised-2);
    border-color: var(--border-subtle);
  }
  .tg--error {
    border-color: color-mix(in srgb, var(--error) 30%, transparent);
  }

  .tg__head {
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
  .tg__head:hover {
    background: var(--state-hover);
  }
  .tg__head:focus-visible {
    outline: none;
  }
  .tg:has(.tg__head:focus-visible) {
    box-shadow: var(--shadow-focus);
  }
  .tg__glyphs {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    color: var(--text-ghost);
  }
  .tg__glyph {
    font-size: var(--fs-body-sm);
    line-height: 1;
  }
  .tg__count {
    flex: 0 0 auto;
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body-sm);
  }
  .tg__names {
    flex: 1 1 auto;
    min-width: 0;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tg__status {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
  }
  .tg__chevron {
    flex: 0 0 auto;
    color: var(--text-ghost);
    font-size: var(--fs-body);
    line-height: 1;
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .tg__chevron--open {
    transform: rotate(90deg);
    color: var(--text-muted);
  }

  /* The body stacks the per-tool cards with a little breathing room. Each child
     ToolCallCard keeps its own border, so the group reads as a labeled rack. */
  .tg__body {
    padding: 0 var(--sp-4) var(--sp-4);
    border-top: 1px solid var(--divider);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding-top: var(--sp-4);
  }
  .tg__overflow {
    font-size: var(--fs-label);
    color: var(--text-faint);
    padding: var(--sp-1) var(--sp-2);
  }

  @media (prefers-reduced-motion: reduce) {
    .tg,
    .tg__head,
    .tg__chevron {
      transition: none;
    }
  }
</style>
