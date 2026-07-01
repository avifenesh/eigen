<script lang="ts">
  // A lightweight in-app BROWSER: a URL bar + an <iframe> viewport, so a page the
  // agent references (docs, a PR, a localhost dev server) opens beside the chat
  // without leaving the app. It's a real webview frame (the Wails runtime is
  // Chromium/WebKit), so most sites render; some send X-Frame-Options/CSP that
  // refuse framing — we surface an "open externally" fallback for those via the
  // Wails Browser.OpenURL. No history/tabs — one navigable frame, kept simple.
  import { untrack } from "svelte";
  import { Browser } from "@wailsio/runtime";
  import Tooltip from "./Tooltip.svelte";

  let { initialUrl = "" }: { initialUrl?: string } = $props();

  // Seed from the prop once at construction; `initialUrl` is a starting value,
  // not a live binding (reading it in the initializer is intentional).
  let draft = $state(untrack(() => initialUrl));
  let url = $state(untrack(() => initialUrl)); // the committed src
  let loading = $state(false);
  let frame = $state<HTMLIFrameElement | undefined>(undefined);

  // Normalize a typed address into a navigable URL: bare host → https://,
  // a search-looking string left alone only if it has a scheme/host shape.
  function normalize(s: string): string {
    const t = s.trim();
    if (!t) return "";
    if (/^https?:\/\//i.test(t)) return t;
    if (/^localhost(:\d+)?(\/|$)/i.test(t) || /^\d{1,3}(\.\d{1,3}){3}(:\d+)?/.test(t)) return "http://" + t;
    if (/^[\w-]+(\.[\w-]+)+/.test(t)) return "https://" + t;
    return "https://" + t;
  }

  function go() {
    const next = normalize(draft);
    if (!next) return;
    draft = next;
    loading = true;
    // Reassign even if unchanged so a reload re-navigates.
    url = "";
    queueMicrotask(() => (url = next));
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      go();
    }
  }

  function openExternal() {
    if (url) Browser.OpenURL(url).catch(() => {});
  }
</script>

<div class="bp">
  <div class="bp__bar">
    <input
      class="bp__url"
      bind:value={draft}
      {onkeydown}
      placeholder="Enter a URL (docs, a PR, localhost…)"
      aria-label="URL address"
      spellcheck="false"
      autocapitalize="off"
      autocomplete="off"
    />
    <button class="bp__btn" onclick={go} title="Go">go</button>
    {#if url}
      <Tooltip text="Open in your default browser">
        <button class="bp__btn" onclick={openExternal} aria-label="Open in your default browser">↗</button>
      </Tooltip>
    {/if}
  </div>
  <div class="bp__view">
    {#if url}
      <!-- sandboxed but allowed scripts/forms/same-origin so dev servers + docs
           work; the frame can't reach into the app shell. -->
      <iframe
        bind:this={frame}
        class="bp__frame"
        src={url}
        title="In-app browser"
        sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        onload={() => (loading = false)}
      ></iframe>
    {:else}
      <div class="bp__empty">
        <p class="bp__empty-t">In-app browser</p>
        <p class="bp__empty-l">Type a URL above to open a page beside the chat. Some sites refuse to be framed — use ↗ to open them in your default browser.</p>
      </div>
    {/if}
    {#if loading && url}<div class="bp__loading">loading…</div>{/if}
  </div>
</div>

<style>
  .bp {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .bp__bar {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-4);
    border-bottom: 1px solid var(--border-hairline);
  }
  .bp__url {
    flex: 1;
    min-width: 0;
    height: 28px;
    padding: 0 var(--sp-4);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-sm);
    background: var(--bg-well);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-label) / 1 var(--font-mono, monospace);
    outline: none;
  }
  .bp__url:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .bp__btn {
    flex: none;
    height: 28px;
    padding: 0 var(--sp-4);
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-sm);
    background: var(--bg-raised);
    color: var(--text-secondary);
    cursor: pointer;
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
  }
  .bp__btn:hover {
    color: var(--text-primary);
    border-color: var(--border-strong);
  }
  .bp__view {
    flex: 1;
    min-height: 0;
    position: relative;
    background: #fff;
  }
  .bp__frame {
    width: 100%;
    height: 100%;
    border: none;
    display: block;
  }
  .bp__empty {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-3);
    padding: var(--sp-8);
    background: var(--bg-base);
    text-align: center;
  }
  .bp__empty-t {
    margin: 0;
    font: var(--fw-semibold) var(--fs-body) / 1 var(--font-sans);
    color: var(--text-secondary);
  }
  .bp__empty-l {
    margin: 0;
    max-width: 320px;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-prose);
  }
  .bp__loading {
    position: absolute;
    top: var(--sp-4);
    right: var(--sp-4);
    padding: 2px var(--sp-4);
    background: var(--bg-overlay);
    border-radius: var(--r-full);
    color: var(--text-muted);
    font-size: var(--fs-micro);
  }
</style>
