<script lang="ts">
  // Chat — the live agent conversation. The keystone of the no-leak contract:
  // Subscribe + transcript construction + listener start + State seed all live
  // inside ONE $effect, whose cleanup disposes the transcript (removing its
  // listener) and Unsubscribes (closing the pump → daemon releases the view).
  // Construction and teardown are symmetric; nothing is created at <script> top.
  import { Bridge } from "$lib/bridge";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { router } from "$lib/router.svelte";
  import { on, ev } from "$lib/events";
  import { createTranscript, type Transcript } from "$lib/stores/transcript.svelte";
  import type { SessionStateDTO } from "$lib/types";
  import Composer from "$lib/components/Composer.svelte";
  import ToolCallCard from "$lib/components/ToolCallCard.svelte";
  import Markdown from "$lib/components/Markdown.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Button from "$lib/components/Button.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";

  let { param }: { param?: string } = $props();

  // Resolve the session to show: route param, else the newest session.
  const sessionId = $derived(param ?? sessions.list[0]?.id ?? "");

  let store = $state<Transcript | null>(null);
  let sess = $state<SessionStateDTO | null>(null);
  let scroller = $state<HTMLDivElement | undefined>(undefined);
  let pinned = $state(true);

  // Per-session lifecycle. Re-runs when sessionId changes; cleanup tears the
  // previous session down completely before the next is set up.
  $effect(() => {
    const id = sessionId;
    if (!id) {
      store = null;
      sess = null;
      return;
    }
    let alive = true;
    const t = createTranscript(id);

    Bridge.Subscribe(id).catch((e) => toasts.error("subscribe: " + (e instanceof Error ? e.message : String(e))));
    Bridge.State(id)
      .then((s) => {
        if (!alive || !s) return;
        sess = s;
        t.seed(s.messages, s.running ?? false);
      })
      .catch((e) => toasts.error("state: " + (e instanceof Error ? e.message : String(e))));
    t.start();
    store = t;

    // The daemon signals view-close if the connection drops underneath us.
    const offClosed = on(ev.sessionClosed(id), () => {
      if (alive) toasts.info("session stream closed");
    });

    return () => {
      alive = false;
      offClosed();
      t.dispose();
      Bridge.Unsubscribe(id).catch(() => {});
    };
  });

  // Refresh the static state snapshot when a turn ends (token counts, pending).
  $effect(() => {
    if (store && store.running === false && sessionId) {
      Bridge.State(sessionId)
        .then((s) => {
          if (s) sess = s;
        })
        .catch(() => {});
    }
  });

  // Pin-to-bottom unless the user scrolled up.
  function onScroll() {
    if (!scroller) return;
    const gap = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
    pinned = gap < 48;
  }
  $effect(() => {
    // touch reactive deps so this runs on stream growth
    void store?.history.length;
    void store?.live?.text;
    if (pinned && scroller) {
      requestAnimationFrame(() => {
        if (scroller) scroller.scrollTop = scroller.scrollHeight;
      });
    }
  });

  const online = $derived(daemon.status === "online");

  async function send(text: string) {
    if (!sessionId) return;
    try {
      await Bridge.SendInput(sessionId, text, [], []);
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    }
  }
  async function interrupt() {
    if (!sessionId) return;
    try {
      await Bridge.Interrupt(sessionId);
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    }
  }

  const pct = $derived(
    sess && sess.maxTokens > 0 ? Math.min(100, Math.round((sess.tokens / sess.maxTokens) * 100)) : 0,
  );
</script>

{#if !sessionId}
  <EmptyState glyph="▶" title="No session selected" line="Start one from Home, or pick a session.">
    {#snippet action()}
      <Button variant="primary" onclick={() => router.go("home")}>Go to Home</Button>
    {/snippet}
  </EmptyState>
{:else}
  <div class="chat">
    <div class="chat__main">
      <div class="chat__scroll selectable" bind:this={scroller} onscroll={onScroll}>
        <div class="chat__thread">
          {#if store?.truncated}
            <div class="chat__earlier">Showing the most recent messages.</div>
          {/if}
          {#each store?.history ?? [] as block, i (i)}
            {#if block.kind === "tool"}
              <ToolCallCard {block} />
            {:else if block.kind === "note"}
              <div class="msg msg--note">{block.text}</div>
            {:else if block.kind === "reasoning"}
              <div class="msg msg--reasoning">
                <span class="msg__tag">reasoning</span>
                {block.text}
              </div>
            {:else}
              <!-- Completed assistant prose renders as Markdown (sans; fenced
                   code delegates to CodeBlock). -->
              <div class="msg msg--text"><Markdown source={block.text} /></div>
            {/if}
          {/each}
          {#if store?.live}
            <div class="msg msg--{store.live.kind === 'reasoning' ? 'reasoning' : 'text'} msg--live">
              {#if store.live.kind === "reasoning"}<span class="msg__tag">reasoning</span>{/if}
              {store.live.text}
            </div>
          {/if}
          {#if store?.running}
            <div class="chat__working"><StatusDot state="working" size={7} /> working…</div>
          {/if}
        </div>
      </div>

      <div class="chat__composer">
        <Composer
          running={store?.running ?? false}
          disabled={!online}
          disabledReason={online ? "" : "daemon offline"}
          onsend={send}
          oninterrupt={interrupt}
        />
      </div>
    </div>

    <aside class="chat__dock">
      <div class="dock__group">
        <div class="dock__label">model</div>
        <div class="dock__value">{sess?.model || "—"}</div>
        {#if sess?.provider}<div class="dock__sub">{sess.provider}</div>{/if}
      </div>

      {#if sess && sess.maxTokens > 0}
        <div class="dock__group">
          <div class="dock__label">context</div>
          <div class="dock__ring">
            <div class="dock__bar"><span style="width:{pct}%"></span></div>
            <span class="dock__pct tnum">{pct}%</span>
          </div>
          <div class="dock__sub tnum">{sess.tokens.toLocaleString()} / {sess.maxTokens.toLocaleString()}</div>
        </div>
      {/if}

      <div class="dock__group">
        <div class="dock__label">status</div>
        <div class="dock__pills">
          <Badge tone={sess?.perm === "auto" ? "warn" : "neutral"}>{sess?.perm || "gated"}</Badge>
          {#if sess?.effort}<Badge tone="info">effort: {sess.effort}</Badge>{/if}
          {#if sess?.search}<Badge tone="info">search: {sess.search}</Badge>{/if}
          {#if sess?.fastOk}<Badge tone={sess.fast ? "brand" : "neutral"}>fast</Badge>{/if}
        </div>
      </div>

      {#if sess?.goal}
        <div class="dock__group">
          <div class="dock__label">goal</div>
          <div class="dock__goal">{sess.goal}</div>
        </div>
      {/if}

      {#if sess?.pending && sess.pending.length > 0}
        <div class="dock__group dock__group--approve">
          <div class="dock__label">awaiting approval</div>
          {#each sess.pending as ap (ap.id)}
            <div class="approve">
              <div class="approve__tool">{ap.tool}</div>
              <div class="approve__actions">
                <Button
                  variant="primary"
                  size="sm"
                  onclick={() => Bridge.Approve(sessionId, ap.id, true).catch(() => {})}>Allow</Button
                >
                <Button
                  variant="danger"
                  size="sm"
                  onclick={() => Bridge.Approve(sessionId, ap.id, false).catch(() => {})}>Deny</Button
                >
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </aside>
  </div>
{/if}

<style>
  .chat {
    display: flex;
    height: 100%;
    min-height: 0;
  }
  .chat__main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .chat__scroll {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }
  .chat__thread {
    max-width: 820px;
    margin: 0 auto;
    padding: var(--sp-8) var(--sp-8) var(--sp-6);
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .chat__earlier {
    text-align: center;
    color: var(--text-faint);
    font-size: var(--fs-label);
    padding: var(--sp-3);
  }
  .msg {
    font: var(--fw-regular) var(--fs-body) / var(--lh-prose) var(--font-sans);
    color: var(--text-primary);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .msg--reasoning {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    border-left: 2px solid var(--border-subtle);
    padding-left: var(--sp-5);
  }
  .msg--note {
    color: var(--text-secondary);
    font-size: var(--fs-body-sm);
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-sm);
    padding: var(--sp-4) var(--sp-5);
  }
  .msg__tag {
    display: block;
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    margin-bottom: var(--sp-2);
  }
  .msg--live {
    border-left: 2px solid var(--border-brand-faint);
    padding-left: var(--sp-5);
  }
  .chat__working {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    color: var(--working);
    font-size: var(--fs-body-sm);
  }
  .chat__composer {
    flex: none;
    max-width: 820px;
    width: 100%;
    margin: 0 auto;
    padding: var(--sp-5) var(--sp-8) var(--sp-7);
  }
  .chat__dock {
    width: 268px;
    flex: none;
    border-left: 1px solid var(--border-hairline);
    background: var(--bg-well);
    padding: var(--sp-7) var(--sp-6);
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--sp-7);
  }
  .dock__label {
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
    margin-bottom: var(--sp-3);
  }
  .dock__value {
    font-size: var(--fs-body-sm);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
    word-break: break-all;
  }
  .dock__sub {
    font-size: var(--fs-label);
    color: var(--text-muted);
    margin-top: var(--sp-2);
  }
  .dock__ring {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .dock__bar {
    flex: 1;
    height: 6px;
    background: var(--bg-inset);
    border-radius: var(--r-full);
    overflow: hidden;
  }
  .dock__bar span {
    display: block;
    height: 100%;
    background: var(--spectrum);
    border-radius: var(--r-full);
    transition: width var(--dur-slow) var(--ease-out);
  }
  .dock__pct {
    font-size: var(--fs-label);
    color: var(--text-secondary);
  }
  .dock__pills {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-3);
  }
  .dock__goal {
    font-size: var(--fs-body-sm);
    color: var(--text-secondary);
    line-height: var(--lh-snug);
  }
  .dock__group--approve {
    background: var(--warn-bg);
    border: 1px solid var(--warn);
    border-radius: var(--r-md);
    padding: var(--sp-5);
  }
  .approve {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .approve + .approve {
    margin-top: var(--sp-4);
  }
  .approve__tool {
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body-sm);
  }
  .approve__actions {
    display: flex;
    gap: var(--sp-3);
  }
</style>
