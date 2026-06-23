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
    onsend: (text: string, images: ImageDTO[]) => void;
    oninterrupt: () => void;
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
  // Count of in-flight drag-enters so nested children don't flicker the state.
  let dragDepth = $state(0);
  const dragging = $derived(dragDepth > 0 && !disabled);
  let seq = 0;

  const trimmed = $derived(text.trim());
  const canSend = $derived(
    !disabled && !running && (trimmed.length > 0 || attachments.length > 0),
  );
  // The affordance line: quiet shortcut hint by default, swapping to a live
  // character count once the author has typed something worth measuring.
  const hint = $derived(
    trimmed.length > 0
      ? `${trimmed.length} char${trimmed.length === 1 ? "" : "s"}`
      : attachments.length > 0
        ? `${attachments.length} image${attachments.length === 1 ? "" : "s"} · Enter to send`
        : "Enter to send · Shift+Enter for newline",
  );

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

  function send() {
    if (!canSend) return;
    onsend(trimmed, attachments.map((a) => a.image));
    text = "";
    clearAttachments();
    queueMicrotask(grow);
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      send();
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
  class:composer--running={running}
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

    <textarea
      bind:this={ta}
      bind:value={text}
      {onkeydown}
      oninput={grow}
      onpaste={onpaste}
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
    <Button
      variant="icon"
      size="md"
      disabled={disabled || running}
      title={disabled ? disabledReason : "Attach images"}
      onclick={() => fileInput?.click()}
    >⧉</Button>
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

  @media (prefers-reduced-motion: reduce) {
    .composer,
    .composer__hint,
    .thumb__remove {
      transition: none;
    }
  }
</style>
