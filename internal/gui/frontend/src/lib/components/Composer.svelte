<script lang="ts">
  // The message composer. Enter sends, Shift+Enter newlines. The textarea
  // auto-grows from one row up to a cap, then scrolls. The primary action flips
  // to Stop while a turn runs; when the daemon is offline the whole surface is
  // disabled and the reason is surfaced inline + as a title. Proportional sans
  // input — never monospace.
  import Button from "./Button.svelte";

  let {
    running = false,
    disabled = false,
    disabledReason = "",
    onsend,
    oninterrupt,
  }: {
    running?: boolean;
    disabled?: boolean;
    disabledReason?: string;
    onsend: (text: string) => void;
    oninterrupt: () => void;
  } = $props();

  // Auto-grow cap, expressed as rows of the input's line-box so the geometry
  // tracks the font tokens rather than a magic pixel number.
  const MAX_ROWS = 8;

  let text = $state("");
  let ta: HTMLTextAreaElement | undefined = $state(undefined);
  let focused = $state(false);

  const trimmed = $derived(text.trim());
  const canSend = $derived(!disabled && !running && trimmed.length > 0);
  // The affordance line: quiet shortcut hint by default, swapping to a live
  // character count once the author has typed something worth measuring.
  const hint = $derived(
    trimmed.length > 0
      ? `${trimmed.length} char${trimmed.length === 1 ? "" : "s"}`
      : "Enter to send · Shift+Enter for newline",
  );

  function grow() {
    if (!ta) return;
    // Measure against a clean slate, then clamp to the row cap. The cap is read
    // from a CSS custom property so the JS and the stylesheet never disagree.
    ta.style.height = "auto";
    const cs = getComputedStyle(ta);
    const line = parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.35;
    const cap = line * MAX_ROWS;
    const next = Math.min(ta.scrollHeight, cap);
    ta.style.height = `${next}px`;
    ta.style.overflowY = ta.scrollHeight > cap ? "auto" : "hidden";
  }

  function send() {
    if (!canSend) return;
    onsend(trimmed);
    text = "";
    queueMicrotask(grow);
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      send();
    }
  }
</script>

<div
  class="composer"
  class:composer--disabled={disabled}
  class:composer--focused={focused && !disabled}
  class:composer--running={running}
>
  <div class="composer__field">
    <textarea
      bind:this={ta}
      bind:value={text}
      {onkeydown}
      oninput={grow}
      onfocus={() => (focused = true)}
      onblur={() => (focused = false)}
      {disabled}
      placeholder={disabled
        ? disabledReason || "Composer unavailable"
        : "Message eigen…"}
      rows="1"
      spellcheck="true"
      aria-label="Message eigen"
      class="composer__input selectable"
    ></textarea>

    <div class="composer__footer" aria-hidden="true">
      <span class="composer__hint" class:composer__hint--count={trimmed.length > 0}>
        {disabled ? disabledReason || "Composer unavailable" : hint}
      </span>
    </div>
  </div>

  <div class="composer__actions">
    {#if running}
      <Button
        variant="danger"
        size="md"
        onclick={oninterrupt}
        title="Interrupt the running turn">Stop</Button
      >
    {:else}
      <Button
        variant="primary"
        size="md"
        disabled={!canSend}
        title={disabled ? disabledReason : "Send message (Enter)"}
        onclick={send}>Send</Button
      >
    {/if}
  </div>
</div>

<style>
  /* The whole surface is a single inset card. The action column is pinned to
     the bottom so the Send/Stop button rides the input's baseline row no matter
     how tall the textarea grows. --pad-y is the optical inset that lines the
     first text line up with the 32px button's vertical centre. */
  .composer {
    --pad-y: var(--sp-5);
    --pad-x: var(--sp-5);
    display: flex;
    align-items: flex-end;
    gap: var(--sp-4);
    padding: var(--pad-y) var(--pad-x);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-lg);
    background: var(--bg-raised);
    box-shadow: var(--shadow-1);
    transition:
      border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out),
      opacity var(--dur-fast) var(--ease-out);
  }

  /* Refined focus-within: a brand-faint edge plus a faint inner glow so the
     card feels lit from within rather than ringed. The base --shadow-1 hairline
     is preserved underneath. */
  .composer--focused {
    border-color: var(--border-brand-faint);
    box-shadow:
      var(--shadow-1),
      inset 0 0 0 1px var(--border-brand-faint),
      inset 0 1px 16px var(--state-focus-bg);
  }

  /* While a turn runs the edge picks up the warm working tint, kept subtle and
     derived straight from the --working token so it tracks the palette. */
  .composer--running {
    border-color: color-mix(in srgb, var(--working) 28%, transparent);
  }

  .composer--disabled {
    opacity: 0.62;
    box-shadow: var(--shadow-1);
  }

  .composer__field {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }

  .composer__input {
    width: 100%;
    resize: none;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body) / var(--lh-snug) var(--font-sans);
    /* One row at first; grow() clamps the upper bound to MAX_ROWS in JS. */
    min-height: calc(var(--fs-body) * var(--lh-snug));
    padding: 0;
    margin: 0;
    outline: none;
    overflow-y: hidden;
    /* Caret and selection inherit the brand so the input feels alive. */
    caret-color: var(--brand);
  }
  .composer__input::selection {
    background: var(--state-selected);
  }
  .composer__input::placeholder {
    color: var(--text-ghost);
    /* Hold the placeholder visible while focused — it reads as guidance. */
    opacity: 1;
  }
  .composer__input:disabled {
    cursor: not-allowed;
    color: var(--text-muted);
  }

  /* Quiet affordance line under the input. It only earns space when there is
     something to say — empty otherwise via the reserved single line. */
  .composer__footer {
    display: flex;
    align-items: center;
    min-height: var(--fs-micro);
  }
  .composer__hint {
    font: var(--fw-regular) var(--fs-micro) / 1 var(--font-sans);
    letter-spacing: var(--ls-normal);
    color: var(--text-faint);
    user-select: none;
    transition: color var(--dur-fast) var(--ease-out);
  }
  .composer--focused .composer__hint {
    color: var(--text-ghost);
  }
  /* The live count leans on the brand-dim so it reads as a measure, not noise. */
  .composer__hint--count {
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }
  .composer--disabled .composer__hint {
    color: var(--error);
  }

  /* Pinned to the input's bottom row; flex-none keeps the button intrinsic. */
  .composer__actions {
    flex: none;
    /* Nudge the button down so its box centres on the first input line rather
       than the footer hint — optical baseline alignment. */
    padding-bottom: calc(var(--fs-micro) + var(--sp-2));
  }

  @media (prefers-reduced-motion: reduce) {
    .composer,
    .composer__hint {
      transition: none;
    }
  }
</style>
