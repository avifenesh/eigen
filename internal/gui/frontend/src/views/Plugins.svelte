<script lang="ts">
  // Plugins — installed plugins + configured marketplaces (the Tier 27 plugin
  // layer). Each plugin shows what it wired in (skills / agents / mcp / commands
  // / hooks) and its install-scan status; marketplaces can be enabled/disabled
  // or removed. Adding a plugin happens here now (mirroring Skills): record a
  // marketplace source, list its installable plugins, then install one — the
  // backend scans every bundle before writing, with no force path in the GUI.
  // Uninstall + marketplace-remove are destructive, so they take an inline
  // confirm rather than acting on a stray click.
  import { Bridge } from "$lib/bridge";
  import { errText } from "$lib/errors";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { viewCache } from "$lib/stores/viewCache.svelte";
  import { relTime } from "$lib/status";
  import type { PluginsDTO, InstalledPluginDTO, PluginPreviewDTO } from "$lib/types";
  import { SvelteSet } from "svelte/reactivity";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import Skeleton from "$lib/components/Skeleton.svelte";

  const CACHE_KEY = "plugins";
  let data = $state<PluginsDTO | null>(viewCache.get<PluginsDTO>(CACHE_KEY) ?? null);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let acting = $state<Record<string, boolean>>({});
  let confirming = $state<string | null>(null); // key of the row awaiting confirm
  let expandedScans = new SvelteSet<string>(); // plugin names with their scan detail open
  function toggleScans(name: string) {
    if (expandedScans.has(name)) expandedScans.delete(name);
    else expandedScans.add(name);
  }

  // Add-a-plugin flow (mirrors Skills' add UX, in two slow steps because the
  // backend fetches over the network):
  //   1. AddMarketplace(source) records a catalog (owner/repo, https URL, or
  //      local path) — slow, so `addingMkt` drives a spinner. On success we
  //      immediately list its plugins.
  //   2. MarketplacePlugins(mktName) populates `previews` — one selectable row
  //      per installable plugin (also reachable via "Browse" on an already-
  //      recorded marketplace, no re-add needed).
  //   3. InstallPlugin(name, mkt) scans then writes — `installingPlugin` tracks
  //      the in-flight row. The three distinct rejects surface as toasts:
  //      RISKY (warn, no force offered), no-scanner (error), already-installed
  //      (info, still refresh). On success we refresh the installed list and
  //      drop the row from `previews`.
  let mktSource = $state("");
  let addingMkt = $state(false);
  let previews = $state<PluginPreviewDTO[]>([]);
  let previewMkt = $state(""); // which marketplace `previews` came from (for the heading)
  let installingPlugin = $state<string | null>(null);

  // Non-zero component counts as "3 skills · 1 agent · 2 mcp" for a preview row.
  function previewComponents(p: PluginPreviewDTO): string {
    const parts: string[] = [];
    if (p.skills) parts.push(`${p.skills} skill${p.skills === 1 ? "" : "s"}`);
    if (p.agents) parts.push(`${p.agents} agent${p.agents === 1 ? "" : "s"}`);
    if (p.mcpServers) parts.push(`${p.mcpServers} mcp`);
    if (p.commands) parts.push(`${p.commands} command${p.commands === 1 ? "" : "s"}`);
    if (p.hooks) parts.push(`${p.hooks} hook${p.hooks === 1 ? "" : "s"}`);
    return parts.join(" · ");
  }

  // Step 1+2: record the marketplace source, then list its plugins. AddMarketplace
  // is idempotent (re-adding refreshes), so this doubles as a re-fetch path.
  async function addSource() {
    const src = mktSource.trim();
    if (!src || addingMkt) return;
    addingMkt = true;
    try {
      const mkt = await Bridge.AddMarketplace(src);
      if (!mkt) {
        toasts.info("nothing added");
        return;
      }
      toasts.success(`added marketplace ${mkt.name}`);
      mktSource = "";
      await loadPreviews(mkt.name);
      viewCache.invalidate(CACHE_KEY);
      await load(); // the new marketplace lands in the Marketplaces section too
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      addingMkt = false;
    }
  }

  // Step 2 alone: list installable plugins from an already-recorded marketplace
  // (the "Browse" action). Shares `addingMkt` so the same spinner shows.
  async function loadPreviews(mktName: string) {
    addingMkt = true;
    previewMkt = mktName;
    try {
      previews = await Bridge.MarketplacePlugins(mktName);
      if (previews.length === 0) toasts.info(`${mktName} lists no plugins`);
    } catch (e) {
      previews = [];
      previewMkt = "";
      toasts.error(errText(e));
    } finally {
      addingMkt = false;
    }
  }

  // Step 3: scan + install one plugin. The backend rejects distinctly; map each
  // to the right toast tone. RISKY and no-scanner are honest failures (error);
  // already-installed is benign (info) and we still refresh + drop the row.
  async function installPlugin(p: PluginPreviewDTO) {
    if (installingPlugin) return;
    installingPlugin = p.name;
    try {
      const inst = await Bridge.InstallPlugin(p.name, p.marketplace);
      toasts.success(`installed ${inst?.name ?? p.name}`);
      previews = previews.filter((x) => x.name !== p.name);
      viewCache.invalidate(CACHE_KEY);
      await load();
    } catch (e) {
      const msg = errText(e);
      if (/already installed/.test(msg)) {
        // Benign: we already have it. Drop the row and refresh so it shows up.
        toasts.info(msg);
        previews = previews.filter((x) => x.name !== p.name);
        viewCache.invalidate(CACHE_KEY);
        await load();
      } else {
        // RISKY bundle (no force path by design) or no credentialed scanner —
        // both are real failures; the message tells the user what to do.
        toasts.error(msg);
      }
    } finally {
      installingPlugin = null;
    }
  }

  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    error = null;
    confirming = null; // a refresh clears any dangling inline Uninstall?/Remove?
    try {
      const d = await viewCache.fetch(CACHE_KEY, () => Bridge.Plugins());
      if (seq === loadSeq) data = d;
    } catch (e) {
      if (seq === loadSeq) error = errText(e);
    } finally {
      if (seq === loadSeq) loading = false;
    }
  }
  $effect(() => {
    load();
    return () => {
      loadSeq++;
    };
  });

  // Plugins() is local + instant (reads installed plugins + configured
  // marketplaces), so re-reading freely is cheap. There's no plugins push
  // event, so a plugin installed via the CLI (`eigen plugin install`) won't
  // appear until restart — re-read on window focus / tab-visible so it lands
  // without one. Skip while a load is in flight (no overlap); load() resets
  // confirming, so a refresh also clears any dangling inline confirm. Both
  // listeners are torn down on unmount — leak contract.
  function refreshOnReturn() {
    if (document.visibilityState !== "visible") return;
    if (loading) return;
    load();
  }
  $effect(() => {
    window.addEventListener("focus", refreshOnReturn);
    document.addEventListener("visibilitychange", refreshOnReturn);
    return () => {
      window.removeEventListener("focus", refreshOnReturn);
      document.removeEventListener("visibilitychange", refreshOnReturn);
    };
  });

  async function removePlugin(name: string) {
    acting["p:" + name] = true;
    confirming = null;
    try {
      const ok = await Bridge.RemovePlugin(name);
      if (ok) toasts.success(`uninstalled ${name}`);
      else toasts.info(`${name} was not installed`);
      viewCache.invalidate(CACHE_KEY);
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete acting["p:" + name];
    }
  }
  async function toggleMarket(name: string, enabled: boolean) {
    acting["m:" + name] = true;
    try {
      await Bridge.SetMarketEnabled(name, enabled);
      toasts.success(`${enabled ? "enabled" : "disabled"} ${name}`);
      viewCache.invalidate(CACHE_KEY);
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete acting["m:" + name];
    }
  }
  async function removeMarket(name: string) {
    acting["m:" + name] = true;
    confirming = null;
    try {
      const ok = await Bridge.RemoveMarketplace(name);
      toasts.info(ok ? `removed marketplace ${name}` : `${name} not found`);
      viewCache.invalidate(CACHE_KEY);
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      delete acting["m:" + name];
    }
  }

  // Component chips for a plugin (only those it actually wired in).
  function components(p: InstalledPluginDTO): { label: string; n: number }[] {
    const c: { label: string; n: number }[] = [];
    if (p.skills?.length) c.push({ label: "skills", n: p.skills.length });
    if (p.agents?.length) c.push({ label: "agents", n: p.agents.length });
    if (p.mcpServers?.length) c.push({ label: "mcp", n: p.mcpServers.length });
    if (p.commands?.length) c.push({ label: "commands", n: p.commands.length });
    if (p.hooks) c.push({ label: "hooks", n: p.hooks });
    return c;
  }
  // Uninstall blast radius — RemovePlugin reverses ALL the plugin's wiring and
  // deletes files, so the confirm names what goes (e.g. "Remove 3 skills, 2
  // agents, 1 mcp?") instead of a bare "Uninstall?". A plugin with nothing
  // wired (no components) falls back to the plain prompt.
  function consequence(p: InstalledPluginDTO): string {
    const c = components(p);
    if (c.length === 0) return "Uninstall?";
    return "Remove " + c.map((x) => `${x.n} ${x.label}`).join(", ") + "?";
  }
  function scanTone(s?: string): "success" | "warn" | "neutral" {
    if (s === "clean") return "success";
    if (s === "forced") return "warn";
    return "neutral";
  }
  // installedMs / addedMs are unix *milliseconds* (Go's UnixMilli); the shared
  // relTime() takes unix *nanos*, so scale up before handing it over — the same
  // /1e6 drift status.ts exists to prevent, just in the other direction. A 0 ms
  // means the daemon never stamped it, so render nothing rather than "57y ago".
  function relMs(ms: number): string {
    return ms > 0 ? relTime(ms * 1e6) : "";
  }
</script>

<div class="plug">
  {#if loading && !data}
    <div class="plug__pad">
      <Skeleton count={3} height="96px" gap="var(--sp-5)" />
    </div>
  {:else if error && !data}
    <EmptyState glyph="⊞" title="Couldn't load plugins" line={error}>
      {#snippet action()}
        <Button variant="secondary" onclick={() => load()}>Retry</Button>
      {/snippet}
    </EmptyState>
  {:else if !data}
    <!-- Only a genuine "no data object" (load returned nothing) falls here now.
         An empty-but-loaded result still renders the sections below so the add
         control stays reachable — a fresh user adds their first plugin here. -->
    <EmptyState glyph="⊞" title="No plugins" line="Plugins add tools, skills, agents, and MCP servers. Add a marketplace source below to install one (bundles are scanned first)." />
  {:else}
    <div class="plug__scroll">
      <section class="plug__section">
        <div class="plug__section-head">
          <h2 class="plug__section-title">Installed plugins</h2>
          <span class="plug__n tnum">{data.plugins.length}</span>
        </div>
        <!-- Add a plugin — record a marketplace source (owner/repo, https URL,
             or local path), then install from the listed bundles. Every install
             is scanned before it's written; a risky bundle is refused with no
             force path here by design. Both steps fetch over the network, so
             each carries its own spinner. -->
        <div class="plug__add">
          <input
            class="plug__add-input"
            type="text"
            placeholder="owner/repo, https URL, or local path"
            bind:value={mktSource}
            onkeydown={(e) => e.key === "Enter" && (e.preventDefault(), addSource())}
            disabled={addingMkt}
            aria-label="Marketplace source to add"
          />
          <Button variant="primary" size="sm" loading={addingMkt} disabled={!mktSource.trim()} onclick={addSource}>
            Add source
          </Button>
        </div>

        {#if previews.length > 0}
          <!-- Installable plugins from the browsed/added marketplace. Each row
               installs on its own spinner; the whole list dims while any one is
               in flight so a second install can't race the first. -->
          <div class="prev" class:prev--busy={installingPlugin !== null}>
            <div class="prev__head">
              <span class="prev__eyebrow">Installable from {previewMkt}</span>
              <span class="plug__n tnum">{previews.length}</span>
            </div>
            {#each previews as pv (pv.name)}
              <div class="prev__row">
                <div class="prev__main">
                  <div class="prev__top">
                    <span class="prev__name">{pv.name}</span>
                    {#if pv.version}<Badge tone="neutral">{pv.version}</Badge>{/if}
                  </div>
                  {#if pv.description}<p class="prev__desc">{pv.description}</p>{/if}
                  {#if previewComponents(pv)}<span class="prev__counts tnum">{previewComponents(pv)}</span>{/if}
                </div>
                <Button
                  variant="secondary"
                  size="sm"
                  loading={installingPlugin === pv.name}
                  disabled={installingPlugin !== null && installingPlugin !== pv.name}
                  onclick={() => installPlugin(pv)}
                >
                  Install
                </Button>
              </div>
            {/each}
          </div>
        {/if}
        {#if data.plugins.length === 0}
          <p class="plug__empty-note">No plugins installed.</p>
        {:else}
          <div class="plug__list">
            {#each data.plugins as p (p.name)}
              <Card>
                <div class="pl pl--scan-{p.scanStatus || 'unknown'}">
                  <div class="pl__main">
                    <div class="pl__top">
                      <span class="pl__name">{p.name}</span>
                      {#if p.version}<Badge tone="neutral">{p.version}</Badge>{/if}
                      {#if p.scanStatus}
                        {#if p.scans?.length}
                          <button
                            type="button"
                            class="pl__scan-toggle"
                            aria-expanded={expandedScans.has(p.name)}
                            onclick={() => toggleScans(p.name)}
                          >
                            <Badge tone={scanTone(p.scanStatus)}>scan: {p.scanStatus}</Badge>
                            <span class="pl__scan-caret" class:pl__scan-caret--open={expandedScans.has(p.name)} aria-hidden="true">▸</span>
                          </button>
                        {:else}
                          <Badge tone={scanTone(p.scanStatus)}>scan: {p.scanStatus}</Badge>
                        {/if}
                      {/if}
                      {#if p.marketplace}<span class="pl__market">from {p.marketplace}</span>{/if}
                      {#if p.scanCount}<span class="pl__meta tnum" title="components flagged at install">{p.scanCount} flagged</span>{/if}
                      {#if relMs(p.installedMs)}<span class="pl__meta" title="installed {relMs(p.installedMs)}">installed {relMs(p.installedMs)}</span>{/if}
                    </div>
                    {#if p.description}<p class="pl__desc">{p.description}</p>{/if}
                    <div class="pl__components">
                      {#each components(p) as c (c.label)}
                        <span class="comp"><span class="comp__n tnum">{c.n}</span> {c.label}</span>
                      {/each}
                    </div>
                    {#if p.warnings?.length}
                      <div class="pl__warn">⚠ {p.warnings.join("; ")}</div>
                    {/if}
                    {#if p.scans?.length && expandedScans.has(p.name)}
                      <div class="pl__scans">
                        <p class="pl__scans-note">Installed despite scan flags — each component that tripped the scanner:</p>
                        {#each p.scans as s (s.component)}
                          <div class="scan">
                            <span class="scan__comp selectable">{s.component}</span>
                            {#if s.reasons?.length}
                              <ul class="scan__reasons">
                                {#each s.reasons as r, ri (ri)}
                                  <li class="scan__reason">{r}</li>
                                {/each}
                              </ul>
                            {/if}
                          </div>
                        {/each}
                      </div>
                    {/if}
                  </div>
                  <div class="pl__actions">
                    {#if confirming === "p:" + p.name}
                      <span class="pl__confirm">{consequence(p)}</span>
                      <Button variant="danger" size="sm" loading={acting["p:" + p.name]} onclick={() => removePlugin(p.name)}>Confirm</Button>
                      <Button variant="ghost" size="sm" onclick={() => (confirming = null)}>Cancel</Button>
                    {:else}
                      <Button variant="ghost" size="sm" onclick={() => (confirming = "p:" + p.name)}>Uninstall</Button>
                    {/if}
                  </div>
                </div>
              </Card>
            {/each}
          </div>
        {/if}
      </section>

      <section class="plug__section">
        <div class="plug__section-head">
          <h2 class="plug__section-title">Marketplaces</h2>
          <span class="plug__n tnum">{data.marketplaces.length}</span>
        </div>
        {#if data.marketplaces.length === 0}
          <p class="plug__empty-note">No marketplaces configured.</p>
        {:else}
          <div class="plug__list">
            {#each data.marketplaces as m (m.name)}
              <Card>
                <div class="pl">
                  <div class="pl__main">
                    <div class="pl__top">
                      <StatusDot state={m.disabled ? "idle" : "ok"} size={7} />
                      <span class="pl__name">{m.name}</span>
                      {#if m.disabled}<Badge tone="neutral">disabled</Badge>{/if}
                      {#if m.owner}<span class="pl__meta">by {m.owner}</span>{/if}
                      {#if relMs(m.addedMs)}<span class="pl__meta" title="added {relMs(m.addedMs)}">added {relMs(m.addedMs)}</span>{/if}
                    </div>
                    <div class="pl__source selectable">{m.source}</div>
                  </div>
                  <div class="pl__actions">
                    {#if confirming === "m:" + m.name}
                      <span class="pl__confirm">Remove?</span>
                      <Button variant="danger" size="sm" loading={acting["m:" + m.name]} onclick={() => removeMarket(m.name)}>Confirm</Button>
                      <Button variant="ghost" size="sm" onclick={() => (confirming = null)}>Cancel</Button>
                    {:else}
                      <!-- Browse lists this marketplace's plugins into the add
                           area above without re-recording it (slow fetch). -->
                      <Button
                        variant="ghost"
                        size="sm"
                        loading={addingMkt && previewMkt === m.name}
                        disabled={addingMkt}
                        onclick={() => loadPreviews(m.name)}
                      >
                        Browse plugins
                      </Button>
                      <Button variant="ghost" size="sm" loading={acting["m:" + m.name]} onclick={() => toggleMarket(m.name, m.disabled)}>
                        {m.disabled ? "Enable" : "Disable"}
                      </Button>
                      <Button variant="ghost" size="sm" onclick={() => (confirming = "m:" + m.name)}>Remove</Button>
                    {/if}
                  </div>
                </div>
              </Card>
            {/each}
          </div>
        {/if}
      </section>
    </div>
  {/if}
</div>

<style>
  .plug {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .plug__scroll {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-6) var(--sp-7);
    display: flex;
    flex-direction: column;
    gap: var(--sp-8);
  }
  .plug__pad {
    padding: var(--sp-6) var(--sp-7);
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .plug__section {
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .plug__section-head {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .plug__section-title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .plug__n {
    font-size: var(--fs-label);
    color: var(--text-ghost);
  }
  /* Add control — a marketplace source input + Add (mirrors Skills' add row).
     Adding records a catalog, then lists its plugins below. */
  .plug__add {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    margin-bottom: var(--sp-2);
  }
  .plug__add-input {
    flex: 1;
    min-width: 0;
    max-width: 420px;
    height: 32px;
    padding: 0 var(--sp-5);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body-sm) / 1 var(--font-sans);
    outline: none;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .plug__add-input:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .plug__add-input::placeholder {
    color: var(--text-ghost);
  }
  .plug__add-input:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
  /* Installable-plugins panel — the bridge between "added a source" and "it's
     in my installed list". Each row offers one Install; the panel dims while
     any install is in flight. */
  .prev {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-4) var(--sp-5);
    margin-bottom: var(--sp-2);
    border-radius: var(--r-md);
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .prev--busy {
    opacity: 0.7;
  }
  .prev__head {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
  }
  .prev__eyebrow {
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .prev__row {
    display: flex;
    align-items: center;
    gap: var(--sp-5);
    padding: var(--sp-3) 0;
    border-top: 1px solid var(--divider);
  }
  .prev__main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .prev__top {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .prev__name {
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
  }
  .prev__desc {
    margin: 0;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
  }
  .prev__counts {
    font-size: var(--fs-label);
    color: var(--text-faint);
  }
  .plug__list {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .plug__empty-note {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    margin: 0;
  }
  .pl {
    display: flex;
    gap: var(--sp-5);
    padding: var(--sp-5);
    border-left: 2px solid transparent;
    border-radius: var(--r-md) 0 0 var(--r-md);
  }
  /* Scan status is the one axis that matters for an installed plugin — is this
     bundle trusted? It tints the left edge so the section reads as "what did I
     let in", the way Skills tints rows by source. */
  .pl--scan-clean {
    border-left-color: var(--success);
  }
  .pl--scan-forced {
    border-left-color: var(--warn);
  }
  .pl--scan-unknown {
    border-left-color: var(--border-strong);
  }
  .pl__main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .pl__top {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    flex-wrap: wrap;
  }
  .pl__name {
    font-weight: var(--fw-semibold);
    font-size: var(--fs-body);
    color: var(--text-primary);
  }
  .pl__market {
    font-size: var(--fs-label);
    color: var(--text-faint);
  }
  /* Provenance metadata (install/added date, flagged count, owner) — quiet,
     faint labels that sit alongside the name without competing with the scan
     badge that earns the row's color. */
  .pl__meta {
    font-size: var(--fs-label);
    color: var(--text-faint);
  }
  .pl__desc {
    margin: 0;
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
  }
  .pl__components {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-3);
  }
  /* Component counts as a capability ribbon: each bordered chip shows what the
     plugin wired in (3 skills · 2 agents …), so the grid reads as "what does
     this add" at a glance rather than a flat label run. */
  .comp {
    display: inline-flex;
    align-items: baseline;
    gap: var(--sp-2);
    height: 20px;
    padding: 0 var(--sp-3);
    border-radius: var(--r-xs);
    background: var(--bg-overlay);
    border: 1px solid var(--border-hairline);
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-muted);
  }
  .comp__n {
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    letter-spacing: var(--ls-normal);
    color: var(--brand);
  }
  .pl__warn {
    font-size: var(--fs-label);
    color: var(--warn);
  }
  /* The scan badge becomes a disclosure when a forced install kept findings:
     clicking it reveals which component tripped the scanner and why, mirroring
     the TUI's per-component reason list. */
  .pl__scan-toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 0;
    border: 0;
    background: none;
    cursor: pointer;
    border-radius: var(--r-xs);
    color: inherit;
  }
  .pl__scan-toggle:focus-visible {
    outline: 2px solid var(--brand);
    outline-offset: 2px;
  }
  .pl__scan-caret {
    font-size: var(--fs-micro);
    color: var(--warn);
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .pl__scan-caret--open {
    transform: rotate(90deg);
  }
  .pl__scans {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-4);
    border-radius: var(--r-sm);
    background: var(--bg-overlay);
    border: 1px solid var(--border-hairline);
  }
  .pl__scans-note {
    margin: 0;
    font-size: var(--fs-label);
    color: var(--warn);
  }
  .scan {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .scan__comp {
    font: var(--fw-medium) var(--fs-code-sm) / 1.4 var(--font-mono);
    color: var(--text-primary);
    word-break: break-all;
  }
  .scan__reasons {
    margin: 0;
    padding-left: var(--sp-5);
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
  }
  .scan__reason {
    font-size: var(--fs-body-sm);
    color: var(--text-muted);
    line-height: var(--lh-snug);
  }
  @media (prefers-reduced-motion: reduce) {
    .pl__scan-caret {
      transition: none;
    }
  }
  .pl__source {
    font: var(--fw-regular) var(--fs-code-sm) / 1.4 var(--font-mono);
    color: var(--text-muted);
    word-break: break-all;
  }
  .pl__actions {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .pl__confirm {
    font-size: var(--fs-body-sm);
    color: var(--error);
    font-weight: var(--fw-medium);
  }
</style>
