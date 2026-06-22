<script lang="ts">
  import { toasts } from "$lib/stores/toasts.svelte";
  import { fly } from "svelte/transition";
</script>

<div class="toast-host" aria-live="polite">
  {#each toasts.items as t (t.id)}
    <div class="toast toast--{t.kind}" transition:fly={{ y: 12, duration: 200 }}>
      <span class="toast__text">{t.text}</span>
      <button class="toast__close" onclick={() => toasts.dismiss(t.id)} aria-label="Dismiss">×</button>
    </div>
  {/each}
</div>

<style>
  .toast-host {
    position: fixed;
    bottom: var(--sp-7);
    right: var(--sp-7);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    z-index: 100;
    pointer-events: none;
  }
  .toast {
    pointer-events: auto;
    display: flex;
    align-items: center;
    gap: var(--sp-5);
    min-width: 240px;
    max-width: 380px;
    padding: var(--sp-4) var(--sp-5);
    background: var(--bg-overlay);
    border: 1px solid var(--border-subtle);
    border-left: 2px solid var(--text-muted);
    border-radius: var(--r-md);
    box-shadow: var(--shadow-toast);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
  }
  .toast--success {
    border-left-color: var(--success);
  }
  .toast--error {
    border-left-color: var(--error);
  }
  .toast--info {
    border-left-color: var(--info);
  }
  .toast--working {
    border-left-color: var(--working);
  }
  .toast__text {
    flex: 1;
  }
  .toast__close {
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 16px;
    line-height: 1;
    padding: 0;
  }
  .toast__close:hover {
    color: var(--text-primary);
  }
</style>
