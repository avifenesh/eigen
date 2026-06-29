<script lang="ts">
  import { onMount } from "svelte";
  import { fly } from "svelte/transition";
  import { Bridge } from "$lib/bridge";
  import { applyTheme } from "$lib/theme";
  import { installSmoothScroll } from "$lib/smoothscroll";
  import { router } from "$lib/router.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";
  import { feed } from "$lib/stores/feed.svelte";
  import { voice } from "$lib/stores/voice.svelte";
  import Rail from "$lib/components/Rail.svelte";
  import TopBar from "$lib/components/TopBar.svelte";
  import ToastHost from "$lib/components/ToastHost.svelte";
  import CommandPalette from "$lib/components/CommandPalette.svelte";
  import Shortcuts from "$lib/components/Shortcuts.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import Home from "./views/Home.svelte";
  import Chat from "./views/Chat.svelte";
  import Observe from "./views/Observe.svelte";
  import Memory from "./views/Memory.svelte";
  import Skills from "./views/Skills.svelte";
  import Tasks from "./views/Tasks.svelte";
  import Live from "./views/Live.svelte";
  import Sessions from "./views/Sessions.svelte";
  import Dreaming from "./views/Dreaming.svelte";
  import Routing from "./views/Routing.svelte";
  import Machines from "./views/Machines.svelte";
  import Profile from "./views/Profile.svelte";
  import Crons from "./views/Crons.svelte";
  import Plugins from "./views/Plugins.svelte";
  import Connectors from "./views/Connectors.svelte";
  import Board from "./views/Board.svelte";
  import Notes from "./views/Notes.svelte";
  import Reviewers from "./views/Reviewers.svelte";
  import Config from "./views/Config.svelte";

  // Root lifecycle: start the daemon health stream; its teardown runs on unmount.
  onMount(() => {
    const stopDaemon = daemon.start();
    const stopFeed = feed.start();
    const stopVoice = voice.start();
    // Smooth mouse-wheel scrolling: WebKitGTK leaves wheel scroll as discrete
    // accelerating notch jumps and Wails exposes no setting to change it, so we
    // ease it in JS (no-op for trackpads / reduced-motion). See smoothscroll.ts.
    const stopSmoothScroll = installSmoothScroll();
    sessions.refresh();
    // Apply the saved color theme (deepteal | nord | gruvbox) to the GUI. The
    // config key already drove the TUI; the GUI mirrors it via <html data-theme>.
    Bridge.Config()
      .then((c) => applyTheme(c?.fields?.find((f) => f.key === "theme")?.value))
      .catch(() => {});
    return () => {
      stopDaemon();
      stopFeed();
      stopVoice();
      stopSmoothScroll();
    };
  });

  // Refresh the session list whenever the daemon comes (back) online. Scoped to
  // an $effect so the reconnect callback is removed if this component unmounts.
  $effect(() => daemon.onReconnect(() => sessions.refresh()));

  // Poll session list while online so idle transitions on background chats are
  // noticed even when only one chat pump is subscribed.
  $effect(() => {
    if (daemon.status !== "online") return;
    const t = setInterval(() => void sessions.refresh(), 4000);
    return () => clearInterval(t);
  });

  // Honor prefers-reduced-motion: Svelte JS transitions don't check it on their
  // own, so collapse the route fly to 0ms for reduced-motion users.
  const reduceMotion =
    typeof matchMedia === "function" &&
    matchMedia("(prefers-reduced-motion: reduce)").matches;
</script>

<div class="shell">
  <Rail />
  <div class="main">
    <TopBar />
    <div class="outlet">
      <svelte:boundary>
      {#key router.route}
        <div class="outlet__page" in:fly={{ y: 6, duration: reduceMotion ? 0 : 180, opacity: 0 }}>
        {#if router.route === "home"}
          <Home />
        {:else if router.route === "chat"}
          <Chat param={router.param} />
        {:else if router.route === "observe"}
          <Observe />
        {:else if router.route === "memory"}
          <Memory />
        {:else if router.route === "notes"}
          <Notes />
        {:else if router.route === "reviewers"}
          <Reviewers />
        {:else if router.route === "skills"}
          <Skills />
        {:else if router.route === "tasks"}
          <Tasks />
        {:else if router.route === "live"}
          <Live />
        {:else if router.route === "sessions"}
          <Sessions />
        {:else if router.route === "board"}
          <Board />
        {:else if router.route === "dreaming"}
          <Dreaming />
        {:else if router.route === "routing"}
          <Routing />
        {:else if router.route === "machines"}
          <Machines />
        {:else if router.route === "profile"}
          <Profile />
        {:else if router.route === "crons"}
          <Crons />
        {:else if router.route === "plugins"}
          <Plugins />
        {:else if router.route === "connectors"}
          <Connectors />
        {:else if router.route === "config"}
          <Config />
        {:else}
          <EmptyState glyph="λ" title={router.route} line="This view is coming soon." />
        {/if}
        </div>
      {/key}
      <!-- One bad render (e.g. a Svelte each_key_duplicate from colliding feed
           keys) must not silently freeze the whole shell — the chrome would
           keep updating while the body stayed stuck. Catch it, show a quiet
           recoverable state, and let a click re-render the route. -->
      {#snippet failed(error, reset)}
        <div class="outlet__page boundary-fail">
          <div class="boundary-fail__glyph" aria-hidden="true">⚠</div>
          <h2 class="boundary-fail__title">This view hit a snag</h2>
          <p class="boundary-fail__line">{String(error?.message ?? error)}</p>
          <button class="boundary-fail__retry" onclick={reset}>Reload view</button>
        </div>
      {/snippet}
      </svelte:boundary>
    </div>
  </div>
  <CommandPalette />
  <Shortcuts />
  <ToastHost />
</div>

<style>
  .shell {
    display: flex;
    height: 100%;
    background: var(--bg-base);
  }
  .main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .outlet__page {
    height: 100%;
    min-height: 0;
  }
  .outlet {
    flex: 1;
    overflow: hidden;
    min-height: 0;
  }
  /* Error-boundary fallback: a calm, recoverable state when a route's render
     throws (rather than a silently frozen body). */
  .boundary-fail {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-3, 8px);
    height: 100%;
    text-align: center;
    padding: var(--sp-8, 32px);
  }
  .boundary-fail__glyph {
    font-size: 30px;
    color: var(--text-ghost, #888);
  }
  .boundary-fail__title {
    margin: 0;
    font-size: var(--fs-h3, 1.1rem);
    font-weight: var(--fw-semibold, 600);
    color: var(--text-secondary, #ccc);
  }
  .boundary-fail__line {
    margin: 0;
    max-width: 48ch;
    color: var(--text-muted, #999);
    font-size: var(--fs-body-sm, 0.85rem);
    word-break: break-word;
  }
  .boundary-fail__retry {
    margin-top: var(--sp-4, 16px);
    padding: 6px 14px;
    border-radius: var(--r-md, 8px);
    border: 1px solid var(--border-brand-faint, rgba(64,200,180,0.4));
    background: var(--state-selected, rgba(64,200,180,0.12));
    color: var(--brand, #40c8b4);
    font: inherit;
    cursor: pointer;
  }
</style>
