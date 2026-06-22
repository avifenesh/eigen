<script lang="ts">
  import { onMount } from "svelte";
  import { router } from "$lib/router.svelte";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";
  import Rail from "$lib/components/Rail.svelte";
  import TopBar from "$lib/components/TopBar.svelte";
  import ToastHost from "$lib/components/ToastHost.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import Home from "./views/Home.svelte";
  import Chat from "./views/Chat.svelte";
  import Observe from "./views/Observe.svelte";
  import Memory from "./views/Memory.svelte";
  import Skills from "./views/Skills.svelte";
  import Agents from "./views/Agents.svelte";

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
        {:else}
          <EmptyState glyph="λ" title={router.route} line="This view is coming soon." />
        {/if}
      {/key}
    </div>
  </div>
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
