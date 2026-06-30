<script lang="ts">
  // Reviewers — the revuto cockpit: every repo the AI PR-reviewer watches, with
  // per-repo Review-now / Learn / Pause-Resume. The human peer of the revuto_*
  // agent tools + the Connectors Manage panel, as a full surface.
  import { Bridge } from "$lib/bridge";
  import { errText } from "$lib/errors";
  import { toasts } from "$lib/stores/toasts.svelte";
  import type { RevutoStatusDTO, RevutoReviewerDTO } from "$lib/types";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";

  let status = $state<RevutoStatusDTO | null>(null);
  let reviewers = $state<RevutoReviewerDTO[]>([]);
  let loading = $state(true);
  let busy = $state<Record<string, boolean>>({});

  let alive = true;
  async function load() {
    loading = true;
    try {
      status = await Bridge.RevutoStatus();
      if (status?.available) reviewers = await Bridge.RevutoReviewers();
    } catch (e) {
      if (alive) toasts.error(errText(e));
    } finally {
      if (alive) loading = false;
    }
  }
  $effect(() => {
    load();
    return () => {
      alive = false;
    };
  });

  async function trigger(r: RevutoReviewerDTO, job: string) {
    busy[r.repo] = true;
    toasts.info(`revuto ${job} ${r.repo} — running (may take a while)…`);
    try {
      await Bridge.RevutoTrigger(r.repo, job);
      toasts.success(`revuto ${job} done: ${r.repo}`);
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete busy[r.repo];
    }
  }
  async function togglePause(r: RevutoReviewerDTO) {
    busy[r.repo] = true;
    try {
      await Bridge.RevutoSetPaused(r.repo, !r.paused);
      reviewers = await Bridge.RevutoReviewers();
      status = await Bridge.RevutoStatus();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete busy[r.repo];
    }
  }
</script>

<div class="rev">
  <header class="rev__head">
    <div>
      <h2 class="rev__title">Reviewers</h2>
      <p class="rev__sub">
        Your revuto AI PR-reviewer — {status?.available ? `${status.count} repo${status.count === 1 ? "" : "s"}${status.paused ? `, ${status.paused} paused` : ""}` : "CLI not found"}.
      </p>
    </div>
    {#if status?.available}<Button variant="secondary" size="sm" onclick={() => load()}>Refresh</Button>{/if}
  </header>

  {#if loading && reviewers.length === 0}
    <div class="rev__rows"><Skeleton count={6} height="44px" gap="var(--sp-2)" /></div>
  {:else if !status?.available}
    <EmptyState glyph="⌕" title="Revuto not installed" line="Install the `revuto` CLI to manage your AI PR-reviewer from here." />
  {:else if reviewers.length === 0}
    <EmptyState glyph="⌕" title="No reviewers registered" line="Register a repo with `revuto init owner/repo`." />
  {:else}
    <div class="rev__rows">
      {#each reviewers as r (r.repo)}
        <div class="rrow">
          <span class="rrow__repo">{r.repo}</span>
          {#if r.paused}<Badge tone="neutral">paused</Badge>{:else}<Badge tone="success">active</Badge>{/if}
          <span class="rrow__sp"></span>
          <Button variant="secondary" size="sm" loading={busy[r.repo]} onclick={() => trigger(r, "review")}>Review now</Button>
          <Button variant="ghost" size="sm" disabled={busy[r.repo]} onclick={() => trigger(r, "learn")}>Learn</Button>
          <Button variant="ghost" size="sm" disabled={busy[r.repo]} onclick={() => togglePause(r)}>{r.paused ? "Resume" : "Pause"}</Button>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .rev {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .rev__head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--sp-5);
    padding: var(--sp-6) var(--sp-7) var(--sp-5);
  }
  .rev__title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-h3) / 1.2 var(--font-sans);
    color: var(--text-primary);
  }
  .rev__sub {
    margin: var(--sp-2) 0 0;
    color: var(--text-muted);
    font-size: var(--fs-label);
  }
  .rev__rows {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: 0 var(--sp-7) var(--sp-8);
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .rrow {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-4) var(--sp-5);
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-md);
  }
  .rrow__repo {
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-mono);
    color: var(--text-primary);
  }
  .rrow__sp {
    flex: 1;
  }
</style>
