<script lang="ts">
  // Home — the session board. Live sessions as actionable cards; a hero action
  // to start one. This is the "home base": open it and act. (The proactive feed
  // card variant arrives once a Bridge.Feed op exists — until then the board
  // shows real sessions, never a dead surface.)
  import { sessions } from "$lib/stores/sessions.svelte";
  import { router } from "$lib/router.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { Bridge } from "$lib/bridge";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let starting = $state(false);

  async function startSession() {
    starting = true;
    try {
      const id = await Bridge.NewSession("", "", "");
      await sessions.refresh();
      router.go("chat", id);
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      starting = false;
    }
  }

  async function remove(id: string, ev: MouseEvent) {
    ev.stopPropagation();
    try {
      await Bridge.RemoveSession(id);
      await sessions.refresh();
      toasts.info("session removed");
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    }
  }

  function rel(updatedNano: number): string {
    const ms = Date.now() - updatedNano / 1e6;
    const m = Math.floor(ms / 60000);
    if (m < 1) return "just now";
    if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h ago`;
    return `${Math.floor(h / 24)}d ago`;
  }

  function base(dir: string): string {
    const p = dir.replace(/\/$/, "").split("/");
    return p[p.length - 1] || dir;
  }
</script>

<div class="home selectable">
  <header class="home__hero">
    <div>
      <h1 class="home__greet">Your agent, everywhere.</h1>
      <p class="home__sub">
        {sessions.count} session{sessions.count === 1 ? "" : "s"} · daemon {daemon.status}
      </p>
    </div>
    <Button variant="primary" size="lg" loading={starting} onclick={startSession}>Start a session</Button>
  </header>

  {#if sessions.loading && sessions.count === 0}
    <div class="home__grid">
      {#each Array(3) as _, i (i)}
        <div class="skeleton"></div>
      {/each}
    </div>
  {:else if sessions.count === 0}
    <EmptyState glyph="◆" title="No sessions yet" line="Start one and it shows up here — resumable, durable, always live.">
      {#snippet action()}
        <Button variant="primary" loading={starting} onclick={startSession}>Start a session</Button>
      {/snippet}
    </EmptyState>
  {:else}
    <div class="home__grid">
      {#each sessions.list as s (s.id)}
        <Card interactive onclick={() => router.go("chat", s.id)} title={s.dir}>
          <div class="sc">
            <div class="sc__top">
              <StatusDot state={s.status === "running" ? "working" : "idle"} size={7} />
              <span class="sc__title">{s.title || "untitled session"}</span>
              <button class="sc__x" title="Remove session" onclick={(e) => remove(s.id, e)} aria-label="Remove">×</button>
            </div>
            <div class="sc__dir">{base(s.dir)}</div>
            <div class="sc__meta">
              {#if s.model}<Badge tone="brand">{s.model}</Badge>{/if}
              <span class="sc__msgs tnum">{s.turns} turn{s.turns === 1 ? "" : "s"}</span>
              <span class="sc__when">{rel(s.updated)}</span>
            </div>
          </div>
        </Card>
      {/each}
    </div>
  {/if}
</div>

<style>
  .home {
    height: 100%;
    overflow-y: auto;
    padding: var(--sp-9) var(--sp-10);
  }
  .home__hero {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--sp-6);
    margin-bottom: var(--sp-9);
  }
  .home__greet {
    margin: 0;
    font: var(--fw-bold) var(--fs-display) / var(--lh-tight) var(--font-display);
    letter-spacing: var(--ls-display);
    color: var(--text-primary);
  }
  .home__sub {
    margin: var(--sp-3) 0 0;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  .home__grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: var(--sp-5);
  }
  .skeleton {
    height: 108px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: shimmer 1.4s ease-in-out infinite;
  }
  @keyframes shimmer {
    to {
      background-position: -200% 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .skeleton {
      animation: none;
    }
  }
  .sc {
    padding: var(--sp-5);
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .sc__top {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .sc__title {
    flex: 1;
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sc__x {
    border: none;
    background: transparent;
    color: var(--text-ghost);
    cursor: pointer;
    font-size: 16px;
    line-height: 1;
    padding: 0 var(--sp-1);
    border-radius: var(--r-xs);
  }
  .sc__x:hover {
    color: var(--error);
    background: var(--error-bg);
  }
  .sc__dir {
    font-size: var(--fs-body-sm);
    color: var(--text-muted);
  }
  .sc__meta {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    font-size: var(--fs-label);
    color: var(--text-ghost);
  }
  .sc__msgs {
    margin-left: auto;
  }
</style>
