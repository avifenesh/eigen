<script lang="ts">
  // The message composer. Enter sends, Shift+Enter newlines. The textarea
  // auto-grows from one row up to a cap, then scrolls. The primary action flips
  // to Stop while a turn runs; when the daemon is offline the whole surface is
  // disabled and the reason is surfaced inline + as a title. Proportional sans
  // input — never monospace.
  //
  // Image intake: paste image blobs, drop image files onto the surface, or pick
  // them with the attach button. Each becomes an ImageDTO (mediaType + raw
  // base64, the data: prefix stripped) plus a thumbnail object-URL we OWN and
  // must revoke — every attachment carries its url; removing one revokes it,
  // sending revokes the batch, and onDestroy revokes whatever is left. No leak.
  import { onDestroy } from "svelte";
  import type { ImageDTO } from "$lib/types";
  import { slashLabel, type SlashCommandEntry } from "$lib/slash";
  import { voice } from "$lib/stores/voice.svelte";
  import Button from "./Button.svelte";

  let {
    running = false,
    stoppable,
    interrupting = false,
    disabled = false,
    disabledReason = "",
    voiceModeOn = false,
    slashCommands = [],
    onsend,
    oninterrupt,
    onvoicemode,
  }: {
    running?: boolean;
    // Whether this composer should expose Stop. Defaults to `running`, but the
    // chat view passes `working` so the user can interrupt a just-sent turn
    // before the first stream event flips running=true.
    stoppable?: boolean;
    // True after the parent has sent an interrupt request and before the turn
    // has actually settled. Disables the Stop affordance and changes its label
    // so repeated clicks/keys don't look ignored.
    interrupting?: boolean;
    disabled?: boolean;
    disabledReason?: string;
    // Whether the hands-free conversation loop is active (parent owns the session
    // it runs against; the composer only reflects + toggles it).
    voiceModeOn?: boolean;
    // Slash command menu entries supplied by the Chat view (TUI built-ins +
    // project/user custom commands). Empty keeps the composer as a plain input.
    slashCommands?: SlashCommandEntry[];
    // Returns whether the send was accepted; the composer clears the draft +
    // attachments ONLY on success, so a failed RPC never destroys what the user
    // typed. A void/undefined return is treated as success (back-compat).
    onsend: (text: string, images: ImageDTO[]) => boolean | void | Promise<boolean | void>;
    oninterrupt: () => void;
    // Toggle hands-free voice mode. Absent → the voice-mode button is hidden
    // (e.g. no live session). The composer still offers one-shot dictation.
    onvoicemode?: () => void;
  } = $props();

  // Auto-grow cap, expressed as rows of the input's line-box so the geometry
  // tracks the font tokens rather than a magic pixel number.
  const MAX_ROWS = 8;

  // An attachment pairs the wire DTO with a locally-owned object URL for its
  // thumbnail and a stable id for keyed iteration. The url is revoked on remove,
  // send, and teardown — the leak contract for this view.
  type Attachment = { id: string; image: ImageDTO; url: string };

  let text = $state("");
  let ta: HTMLTextAreaElement | undefined = $state(undefined);
  let fileInput: HTMLInputElement | undefined = $state(undefined);
  let focused = $state(false);
  let attachments = $state<Attachment[]>([]);
  // SSR-stable, per-instance id wiring the textarea's aria-describedby to the
  // status line below it, so AT reads the offline reason / char count.
  const statusId = $props.id();
  // Count of in-flight drag-enters so nested children don't flicker the state.
  let dragDepth = $state(0);
  const dragging = $derived(dragDepth > 0 && !disabled);
  let seq = 0;

  const trimmed = $derived(text.trim());
  // canSubmit gates the KEYBOARD path (Enter) + the actual send(): it does NOT
  // require !running, because typing while a turn runs is how you STEER the
  // running turn (the parent's onsend tries SteerInput → falls back to a queued
  // turn). The old `!running` guard here is exactly what made mid-turn input do
  // nothing. canSend (the visible Send button's enabled state) still excludes
  // running, but the button is swapped for Stop while running anyway, so the two
  // never conflict.
  const canSubmit = $derived(!disabled && (trimmed.length > 0 || attachments.length > 0));
  const canStopTurn = $derived(stoppable ?? running);
  const canSend = $derived(canSubmit && !canStopTurn);
  const turnHint = $derived(
    interrupting
      ? "stopping…"
      : running
        ? "Enter steers · Esc stops"
        : "Turn starting · Esc stops",
  );
  // The affordance line: quiet shortcut hint by default, swapping to a live
  // character count once the author has typed something worth measuring.
  const hint = $derived(
    trimmed.length > 0
      ? `${trimmed.length} char${trimmed.length === 1 ? "" : "s"}${canStopTurn ? ` · ${turnHint}` : ""}`
      : attachments.length > 0
        ? `${attachments.length} image${attachments.length === 1 ? "" : "s"} · ${canStopTurn ? turnHint : "Enter to send"}`
        : canStopTurn
          ? interrupting
            ? "Stopping the running turn…"
            : running
              ? "Enter steers · Esc stops · Shift+Enter for newline"
              : "Turn starting · Esc stops"
          : "Enter to send · Shift+Enter for newline",
  );
  // The accessible description carried on the textarea (and announced live).
  // When offline it is the disabled reason; otherwise it mirrors the hint so
  // the char count / image count reaches AT instead of dying in an aria-hidden
  // footer.
  const offlineReason = $derived(disabledReason || "Composer unavailable");
  const status = $derived(disabled ? offlineReason : hint);

  // Slash-command completion. Mirrors the TUI affordance: a leading "/" opens a
  // command list; Tab / arrows complete the selected command, and exact commands
  // still submit with Enter. The menu only covers the command token — once the
  // user types a space for arguments, it hides and the draft sends normally.
  let slashIndex = $state(0);
  const slashQuery = $derived.by(() => {
    if (attachments.length > 0) return null;
    const m = text.match(/^\/([^\s/]*)$/);
    return m ? m[1].toLowerCase() : null;
  });
  const slashSuggestions = $derived.by(() => {
    if (slashQuery == null) return [];
    const query = slashQuery;
    return slashCommands
      .map((cmd, order) => ({ cmd, order, name: cmd.name.toLowerCase() }))
      .filter((x) => !query || x.name.includes(query))
      .sort((a, b) => {
        const ap = a.name.startsWith(query) ? 0 : 1;
        const bp = b.name.startsWith(query) ? 0 : 1;
        return ap - bp || a.order - b.order;
      })
      .slice(0, 12)
      .map((x) => x.cmd);
  });
  const slashOpen = $derived(!disabled && slashSuggestions.length > 0 && slashQuery != null);
  $effect(() => {
    if (slashIndex >= slashSuggestions.length) slashIndex = 0;
  });
  function completeSlash(cmd: SlashCommandEntry) {
    const label = slashLabel(cmd);
    const wantsArgs = !!cmd.argHint || /[<[]/.test(cmd.usage ?? "");
    text = wantsArgs ? `${label} ` : label;
    slashIndex = 0;
    queueMicrotask(() => {
      grow();
      ta?.focus();
      ta?.setSelectionRange(text.length, text.length);
    });
  }

  function grow() {
    if (!ta) return;
    // Measure against a clean slate, then clamp to the row cap. The cap is the
    // MAX_ROWS JS constant, multiplied by the line-box height we read from the
    // computed style so the geometry still tracks the font tokens.
    ta.style.height = "auto";
    const cs = getComputedStyle(ta);
    const line = parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.35;
    const cap = line * MAX_ROWS;
    const next = Math.min(ta.scrollHeight, cap);
    ta.style.height = `${next}px`;
    ta.style.overflowY = ta.scrollHeight > cap ? "auto" : "hidden";
  }

  // Read a file/blob into an ImageDTO (raw base64, no data: prefix) and a
  // thumbnail object-URL. Rejects non-images and unreadable blobs quietly.
  function intake(file: File | Blob) {
    if (!file.type.startsWith("image/")) return;
    const reader = new FileReader();
    reader.onload = () => {
      const out = reader.result;
      if (typeof out !== "string") return;
      // data:<mediaType>;base64,<payload> — strip the prefix, keep raw base64.
      const comma = out.indexOf(",");
      if (comma < 0) return;
      const base64 = out.slice(comma + 1);
      const mediaType = file.type || "image/png";
      const url = URL.createObjectURL(file);
      attachments = [
        ...attachments,
        { id: `img-${seq++}`, image: { mediaType, data: base64 }, url },
      ];
    };
    reader.readAsDataURL(file);
  }

  function removeAttachment(id: string) {
    const next: Attachment[] = [];
    for (const a of attachments) {
      if (a.id === id) URL.revokeObjectURL(a.url);
      else next.push(a);
    }
    attachments = next;
  }

  // Revoke every owned object URL. Used on send and teardown.
  function clearAttachments() {
    for (const a of attachments) URL.revokeObjectURL(a.url);
    attachments = [];
  }

  // Sending is gated on canSubmit (NOT canSend) so Enter works mid-turn to steer;
  // while the RPC is in flight `sending` disables the surface so a double-fire
  // can't duplicate or race. The draft + image blobs are cleared ONLY after
  // onsend resolves truthy — a transient daemon failure leaves everything in
  // place to retry rather than silently destroying a carefully-composed prompt
  // (the data-loss the old fire-and-forget had).
  let sending = $state(false);
  async function send() {
    if (!canSubmit || sending) return;
    sending = true;
    try {
      const ok = await onsend(trimmed, attachments.map((a) => a.image));
      if (ok !== false) {
        text = "";
        clearAttachments();
        queueMicrotask(grow);
      }
    } finally {
      sending = false;
    }
  }

  function onkeydown(e: KeyboardEvent) {
    if (slashOpen && slashSuggestions.length > 0) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        slashIndex = (slashIndex + 1) % slashSuggestions.length;
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        slashIndex = (slashIndex - 1 + slashSuggestions.length) % slashSuggestions.length;
        return;
      }
      if (e.key === "Tab") {
        e.preventDefault();
        completeSlash(slashSuggestions[slashIndex]);
        return;
      }
      if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
        const selected = slashSuggestions[slashIndex];
        const exact = text.trim().toLowerCase() === slashLabel(selected).toLowerCase();
        if (!exact) {
          e.preventDefault();
          completeSlash(selected);
          return;
        }
      }
    }
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      send();
    }
  }

  // One-shot dictation: record a single utterance and append the transcript to
  // the draft (does NOT auto-send — the author still reviews + presses Enter).
  // Pressing the mic again while listening cancels the capture. Independent of
  // voice MODE (the hands-free loop the parent drives).
  const listening = $derived(voice.listening && !voice.modeOn);
  async function dictate() {
    if (disabled) return;
    if (voice.listening) {
      await voice.cancelDictate();
      return;
    }
    const heard = await voice.dictate();
    if (heard) {
      text = text ? `${text.replace(/\s+$/, "")} ${heard}` : heard;
      queueMicrotask(() => {
        grow();
        ta?.focus();
      });
    }
  }

  function onpaste(e: ClipboardEvent) {
    if (disabled) return;
    const items = e.clipboardData?.items;
    if (!items) return;
    let took = false;
    for (const it of items) {
      if (it.kind === "file" && it.type.startsWith("image/")) {
        const f = it.getAsFile();
        if (f) {
          intake(f);
          took = true;
        }
      }
    }
    // Only swallow the paste when we actually consumed an image — let text
    // pastes fall through to the textarea untouched.
    if (took) e.preventDefault();
  }

  function onDragEnter(e: DragEvent) {
    if (disabled) return;
    if (!e.dataTransfer?.types.includes("Files")) return;
    e.preventDefault();
    dragDepth++;
  }
  function onDragOver(e: DragEvent) {
    if (disabled || dragDepth === 0) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
  }
  function onDragLeave() {
    if (dragDepth > 0) dragDepth--;
  }
  function onDrop(e: DragEvent) {
    dragDepth = 0;
    if (disabled) return;
    const files = e.dataTransfer?.files;
    if (!files || files.length === 0) return;
    e.preventDefault();
    for (const f of files) intake(f);
  }

  function onPick(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const files = input.files;
    if (files) for (const f of files) intake(f);
    // Reset so re-picking the same file fires change again.
    input.value = "";
  }

  // Leak contract: anything still attached when the composer unmounts gets its
  // object URL revoked.
  onDestroy(clearAttachments);
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="composer"
  class:composer--disabled={disabled}
  class:composer--focused={focused && !disabled}
  class:composer--running={canStopTurn}
  class:composer--dragging={dragging}
  ondragenter={onDragEnter}
  ondragover={onDragOver}
  ondragleave={onDragLeave}
  ondrop={onDrop}
>
  <div class="composer__field">
    {#if attachments.length > 0}
      <ul class="thumbs" aria-label="Attached images">
        {#each attachments as a (a.id)}
          <li class="thumb">
            <img class="thumb__img" src={a.url} alt="attachment preview" />
            <button
              type="button"
              class="thumb__remove"
              onclick={() => removeAttachment(a.id)}
              title="Remove image"
              aria-label="Remove image"
            >×</button>
          </li>
        {/each}
      </ul>
    {/if}

    {#if slashOpen}
      <div class="slash-menu" role="listbox" aria-label="Slash commands">
        {#each slashSuggestions as cmd, i (cmd.name)}
          <button
            type="button"
            class="slash-menu__item"
            class:slash-menu__item--active={i === slashIndex}
            role="option"
            aria-selected={i === slashIndex}
            onmouseenter={() => (slashIndex = i)}
            onmousedown={(e) => e.preventDefault()}
            onclick={() => completeSlash(cmd)}
          >
            <span class="slash-menu__name">{cmd.usage ?? slashLabel(cmd)}</span>
            <span class="slash-menu__desc">{cmd.description}</span>
            {#if cmd.source === "custom"}
              <span class="slash-menu__source">custom</span>
            {/if}
          </button>
        {/each}
      </div>
    {/if}

    <textarea
      bind:this={ta}
      bind:value={text}
      {onkeydown}
      oninput={grow}
      onpaste={onpaste}
      onfocus={() => (focused = true)}
      onblur={() => (focused = false)}
      {disabled}
      placeholder={disabled ? offlineReason : "Message eigen…"}
      rows="1"
      spellcheck="true"
      aria-label="Message eigen"
      aria-describedby={statusId}
      class="composer__input selectable"
    ></textarea>

    <div class="composer__footer">
      <span
        id={statusId}
        class="composer__hint"
        class:composer__hint--count={trimmed.length > 0}
        role="status"
        aria-live="polite"
      >
        {status}
      </span>
    </div>
  </div>

  <div class="composer__actions">
    <input
      bind:this={fileInput}
      type="file"
      accept="image/*"
      multiple
      class="composer__file"
      onchange={onPick}
      tabindex="-1"
      aria-hidden="true"
    />
    {#if voice.stt}
      <!-- Dictate: one utterance → appended to the draft. Pulses while the mic
           is open; pressing again cancels. Hidden entirely when no STT backend
           is installed (capability-gated server-side). -->
      <button
        type="button"
        class="composer__mic"
        class:composer__mic--live={listening}
        disabled={disabled}
        title={listening ? "Stop listening" : "Dictate (speak your message)"}
        aria-label={listening ? "Stop listening" : "Dictate a message"}
        aria-pressed={listening}
        onclick={dictate}
      >{listening ? "◉" : "○"}</button>
    {/if}
    {#if onvoicemode && voice.stt && voice.tts}
      <!-- Voice MODE: the hands-free conversation loop (listen → send → speak →
           listen). Needs both STT and TTS; the parent owns the session it runs
           against. Lit while active. -->
      <button
        type="button"
        class="composer__mic composer__mic--mode"
        class:composer__mic--live={voiceModeOn}
        disabled={disabled}
        title={voiceModeOn ? "Stop voice conversation" : "Start hands-free voice conversation"}
        aria-label={voiceModeOn ? "Stop voice conversation" : "Start voice conversation"}
        aria-pressed={voiceModeOn}
        onclick={onvoicemode}
      >◍</button>
    {/if}
    <Button
      variant="icon"
      size="md"
      disabled={disabled || canStopTurn}
      title={disabled ? disabledReason : "Attach images"}
      onclick={() => fileInput?.click()}
    >⧉</Button>
    {#if canStopTurn}
      <Button
        variant="danger"
        size="md"
        disabled={disabled || interrupting}
        onclick={oninterrupt}
        title={interrupting ? "Interrupt request sent — waiting for the turn to stop" : "Stop the running turn (Esc)"}
        >{interrupting ? "Stopping…" : "Stop"}</Button
      >
    {:else}
      <Button
        variant="primary"
        size="md"
        disabled={!canSend}
        title={disabled ? offlineReason : "Send message (Enter)"}
        onclick={send}
        >Send{#if disabled}<span class="sr-only"> — {offlineReason}</span
          >{/if}</Button
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
    position: relative;
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

  /* A live brand seam + faint inner wash while an image drag hovers the surface,
     so the drop target reads as alive without a heavy overlay. */
  .composer--dragging {
    border-color: var(--border-brand);
    box-shadow:
      var(--shadow-1),
      inset 0 0 0 1px var(--border-brand-faint),
      inset 0 1px 16px var(--state-focus-bg);
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

  /* ── slash command menu ────────────────────────────────────────────────── */
  .slash-menu {
    position: absolute;
    left: var(--pad-x);
    right: calc(var(--pad-x) + 112px);
    bottom: calc(100% + var(--sp-2));
    z-index: 20;
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    max-height: min(48vh, 340px);
    overflow-y: auto;
    padding: var(--sp-2);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--bg-overlay);
    box-shadow: var(--shadow-3);
  }
  .slash-menu__item {
    display: grid;
    grid-template-columns: minmax(120px, max-content) minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--sp-3);
    width: 100%;
    padding: var(--sp-2) var(--sp-3);
    border: none;
    border-radius: var(--r-sm);
    background: transparent;
    color: var(--text-secondary);
    font: var(--fw-regular) var(--fs-body-sm) / var(--lh-snug) var(--font-sans);
    text-align: left;
    cursor: pointer;
  }
  .slash-menu__item:hover,
  .slash-menu__item--active {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .slash-menu__name {
    color: var(--brand-bright);
    font-weight: var(--fw-semibold);
    white-space: nowrap;
  }
  .slash-menu__desc {
    min-width: 0;
    overflow: hidden;
    color: var(--text-muted);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .slash-menu__source {
    padding: 1px var(--sp-2);
    border-radius: var(--r-full);
    background: var(--bg-inset);
    color: var(--text-faint);
    font: var(--fw-medium) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }

  /* ── attached-image thumbnails ─────────────────────────────────────────── */
  .thumbs {
    list-style: none;
    margin: 0 0 var(--sp-3);
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-3);
  }
  .thumb {
    position: relative;
    width: 52px;
    height: 52px;
    border-radius: var(--r-sm);
    overflow: hidden;
    border: 1px solid var(--border-subtle);
    background: var(--bg-inset);
  }
  .thumb__img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  /* Remove affordance: a small scrim chip in the corner, brightening on hover. */
  .thumb__remove {
    position: absolute;
    top: 2px;
    right: 2px;
    width: 16px;
    height: 16px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    border-radius: var(--r-full);
    background: var(--bg-scrim);
    color: var(--text-secondary);
    font-size: var(--fs-label);
    line-height: 0;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .thumb__remove:hover {
    background: var(--error-bg);
    color: var(--error);
  }
  .thumb__remove:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }

  /* The native file input is the picker mechanism but never visible — the icon
     Button drives it via .click(). Off-flow so it adds no width to the column. */
  .composer__file {
    display: none;
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

  /* Pinned to the input's bottom row; flex-none keeps the buttons intrinsic.
     The attach + send/stop buttons share one baseline row. */
  .composer__actions {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    /* Nudge the buttons down so their box centres on the first input line rather
       than the footer hint — optical baseline alignment. */
    padding-bottom: calc(var(--fs-micro) + var(--sp-2));
  }

  /* Off-screen text that stays in the a11y tree — used to fold the offline
     reason into the Send button's accessible name without changing its visible
     label. Standard clip pattern (not display:none, which drops it from the
     tree); mirrors Chat's .sr-only. */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    margin: -1px;
    padding: 0;
    overflow: hidden;
    clip: rect(0 0 0 0);
    clip-path: inset(50%);
    white-space: nowrap;
    border: 0;
  }

  /* Mic + voice-mode buttons. Share the 32px icon geometry of the attach/send
     row so they sit on the same baseline. Quiet at rest; teal + breathing while
     capturing/active so the surface reads as alive when the mic is hot. */
  .composer__mic {
    flex: none;
    width: 32px;
    height: 32px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 1px solid transparent;
    border-radius: var(--r-md);
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-body);
    line-height: 1;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .composer__mic:hover:not(:disabled) {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .composer__mic:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .composer__mic:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  /* Live: the mic is open (dictation) or the conversation loop is running. */
  .composer__mic--live {
    color: var(--brand-bright);
    background: var(--state-selected);
    border-color: var(--border-brand-faint);
    animation: composer-mic-breathe var(--breath) var(--ease-inout) infinite;
    will-change: opacity;
  }
  .composer__mic--live:hover:not(:disabled) {
    color: var(--brand-bright);
    background: var(--state-selected);
  }
  @keyframes composer-mic-breathe {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.6;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .composer,
    .composer__hint,
    .thumb__remove {
      transition: none;
    }
    .composer__mic--live {
      animation: none;
    }
  }
</style>
