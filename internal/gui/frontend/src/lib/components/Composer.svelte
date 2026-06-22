<script lang="ts">
  // The message composer. Enter sends, Shift+Enter newlines. Auto-grows to a
  // cap. The primary action flips to Interrupt while a turn runs. Disabled (with
  // a reason title) when the daemon is offline. Proportional sans input — never
  // monospace.
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

  let text = $state("");
  let ta: HTMLTextAreaElement | undefined;

  function grow() {
    if (!ta) return;
    ta.style.height = "auto";
    ta.style.height = Math.min(ta.scrollHeight, 200) + "px";
  }

  function send() {
    const t = text.trim();
    if (!t || disabled) return;
    onsend(t);
    text = "";
    queueMicrotask(grow);
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  }
</script>

<div class="composer" class:composer--disabled={disabled}>
  <textarea
    bind:this={ta}
    bind:value={text}
    {onkeydown}
    oninput={grow}
    {disabled}
    placeholder={disabled ? disabledReason || "unavailable" : "Message eigen…  (Enter to send · Shift+Enter for newline)"}
    rows="1"
    class="composer__input selectable"
  ></textarea>
  <div class="composer__actions">
    {#if running}
      <Button variant="danger" size="md" onclick={oninterrupt} title="Interrupt the running turn">Stop</Button>
    {:else}
      <Button
        variant="primary"
        size="md"
        disabled={disabled || text.trim().length === 0}
        title={disabled ? disabledReason : undefined}
        onclick={send}>Send</Button
      >
    {/if}
  </div>
</div>

<style>
  .composer {
    display: flex;
    align-items: flex-end;
    gap: var(--sp-4);
    padding: var(--sp-5);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-lg);
    background: var(--bg-raised);
    box-shadow: var(--shadow-1);
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .composer:focus-within {
    border-color: var(--border-brand-faint);
  }
  .composer--disabled {
    opacity: 0.7;
  }
  .composer__input {
    flex: 1;
    resize: none;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body) / var(--lh-snug) var(--font-sans);
    max-height: 200px;
    outline: none;
  }
  .composer__input::placeholder {
    color: var(--text-ghost);
  }
  .composer__actions {
    flex: none;
  }
</style>
