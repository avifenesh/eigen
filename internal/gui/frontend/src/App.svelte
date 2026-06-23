<script lang="ts">
  import { onMount } from "svelte";
  import { router } from "$lib/router.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";
  import Rail from "$lib/components/Rail.svelte";
  import TopBar from "$lib/components/TopBar.svelte";
  import ToastHost from "$lib/components/ToastHost.svelte";
  import CommandPalette from "$lib/components/CommandPalette.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import Home from "./views/Home.svelte";
  import Chat from "./views/Chat.svelte";
  import Observe from "./views/Observe.svelte";
  import Memory from "./views/Memory.svelte";
  import Skills from "./views/Skills.svelte";
  import Agents from "./views/Agents.svelte";
  import Dreaming from "./views/Dreaming.svelte";
  import Routing from "./views/Routing.svelte";
  import Crons from "./views/Crons.svelte";
  import Plugins from "./views/Plugins.svelte";
  import Config from "./views/Config.svelte";

  // Root lifecycle: start the daemon health stream; its teardown runs on unmount.
  onMount(() => {
    const stop = daemon.start();
    sessions.refresh();
    return stop;
  });

  // Refresh the session list whenever the daemon comes (back) online. Scoped to
  // an $effect so the reconnect callback is removed if this component unmounts.
  $effect(() => daemon.onReconnect(() => sessions.refresh()));
</script>

<div class="shell">
  <Rail />
  <div class="main">
    <TopBar />
    <div class="outlet">
      {#key router.route}
        {#if router.route === "home"}
          <Home />
        {:else if router.route === "chat"}
          <Chat param={router.param} />
        {:else if router.route === "observe"}
          <Observe />
        {:else if router.route === "memory"}
          <Memory />
        {:else if router.route === "skills"}
          <Skills />
        {:else if router.route === "agents"}
          <Agents />
        {:else if router.route === "dreaming"}
          <Dreaming />
        {:else if router.route === "routing"}
          <Routing />
        {:else if router.route === "crons"}
          <Crons />
        {:else if router.route === "plugins"}
          <Plugins />
        {:else if router.route === "config"}
          <Config />
        {:else}
          <EmptyState glyph="λ" title={router.route} line="This view is coming soon." />
        {/if}
      {/key}
    </div>
  </div>
  <CommandPalette />
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
  .outlet {
    flex: 1;
    overflow: hidden;
    min-height: 0;
  }
</style>
