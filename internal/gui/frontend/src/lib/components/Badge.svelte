<script lang="ts">
  import type { Snippet } from "svelte";
  let {
    tone = "neutral",
    children,
  }: {
    tone?: "neutral" | "brand" | "success" | "warn" | "error" | "info";
    children: Snippet;
  } = $props();
</script>

<span class="badge badge--{tone} tnum">{@render children()}</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    height: 18px;
    padding: 0 var(--sp-3);
    border-radius: var(--r-full);
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    letter-spacing: var(--ls-normal);
    border: 1px solid transparent;
  }
  .badge--neutral {
    background: var(--bg-overlay);
    color: var(--text-secondary);
    border-color: var(--border-hairline);
  }
  .badge--brand {
    background: var(--state-selected);
    color: var(--brand-bright);
    border-color: var(--border-brand-faint);
  }
  .badge--success {
    background: var(--success-bg);
    color: var(--success);
  }
  .badge--warn {
    background: var(--warn-bg);
    color: var(--warn);
  }
  .badge--error {
    background: var(--error-bg);
    color: var(--error);
  }
  .badge--info {
    background: var(--info-bg);
    color: var(--info);
  }
</style>
