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
  import VirtualList from "$lib/components/VirtualList.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Button from "$lib/components/Button.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";

  let { param }: { param?: string } = $props();

  // Resolve the session to show: route param, else the newest session.
  const sessionId = $derived(param ?? sessions.list[0]?.id ?? "");

  let store = $state<Transcript | null>(null);
  let sess = $state<SessionStateDTO | null>(null);

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

  // Refresh the static state snapshot (token counts, pending approvals) on two
  // triggers: (1) a turn ending, and (2) a gated approval landing — the turn
  // stays "running" while the daemon blocks on the user's decision, so we can't
  // wait for running→false to surface the Allow/Deny dock. Both reads capture
  // the id and bail if it changed before the RPC resolves.
  function refreshState() {
    if (!sessionId) return;
    const id = sessionId;
    Bridge.State(id)
      .then((s) => {
        if (s && id === sessionId) sess = s;
      })
      .catch(() => {});
  }
  $effect(() => {
    if (store && store.running === false) refreshState();
  });
  $effect(() => {
    // watch the approval signal; refetch so sess.pending populates the gate UI.
    if (store && store.approvalSeq > 0) refreshState();
  });

  // The rendered transcript: completed history plus the in-flight live block,
  // as one keyed list fed to VirtualList (windowed; only visible rows mount).
  const rows = $derived.by(() => {
    const h = store?.history ?? [];
    const live = store?.live;
    return live ? [...h, live] : h;
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
      <div class="chat__scroll selectable">
        {#if store?.truncated}
          <div class="chat__earlier">Showing the most recent messages.</div>
        {/if}
        <VirtualList items={rows} estimateHeight={120} pin key={(b) => b.uid}>
          {#snippet row(block)}
            {@const isLive = block === store?.live}
            <div class="chat__row">
              {#if block.kind === "tool"}
                <ToolCallCard {block} />
              {:else if block.kind === "note"}
                <div class="msg msg--note">{block.text}</div>
              {:else if block.kind === "reasoning"}
                <div class="msg msg--reasoning" class:msg--live={isLive}>
                  <span class="msg__tag">reasoning</span>
                  {block.text}
                </div>
              {:else if isLive}
                <!-- The in-flight assistant block streams as plain text for
                     speed; it finalizes to Markdown once committed to history. -->
                <div class="msg msg--text msg--live">{block.text}</div>
              {:else}
                <!-- Completed assistant prose renders as Markdown (sans; fenced
                     code delegates to CodeBlock). -->
                <div class="msg msg--text"><Markdown source={block.text} /></div>
              {/if}
            </div>
          {/snippet}
        </VirtualList>
      </div>

      {#if store?.running}
        <div class="chat__working"><StatusDot state="working" size={7} /> working…</div>
      {/if}

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
  /* The scroll region hosts VirtualList, which owns its own internal scroll +
     pin-to-bottom. We give it a bounded height to window against. */
  .chat__scroll {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    position: relative;
  }
  /* Each virtual row is full-width; the content centers to a readable measure. */
  .chat__row {
    max-width: 820px;
    margin: 0 auto;
    padding: var(--sp-3) var(--sp-8);
  }
  .chat__earlier {
    flex: none;
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
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    max-width: 820px;
    width: 100%;
    margin: 0 auto;
    padding: var(--sp-2) var(--sp-8);
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
