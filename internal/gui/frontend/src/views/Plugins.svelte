<script lang="ts">
  // Plugins — installed plugins + configured marketplaces (the Tier 27 plugin
  // layer). Each plugin shows what it wired in (skills / agents / mcp / commands
  // / hooks) and its install-scan status; marketplaces can be enabled/disabled
  // or removed. Installing is intentionally absent — untrusted bundles are
  // scanned at install via the CLI; the GUI only manages what's already there.
  // Uninstall + marketplace-remove are destructive, so they take an inline
  // confirm rather than acting on a stray click.
  import { Bridge } from "$lib/bridge";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { relTime } from "$lib/status";
  import type { PluginsDTO, InstalledPluginDTO } from "$lib/types";
  import { SvelteSet } from "svelte/reactivity";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let data = $state<PluginsDTO | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let acting = $state<Record<string, boolean>>({});
  let confirming = $state<string | null>(null); // key of the row awaiting confirm
  let expandedScans = new SvelteSet<string>(); // plugin names with their scan detail open
  function toggleScans(name: string) {
    if (expandedScans.has(name)) expandedScans.delete(name);
    else expandedScans.add(name);
  }

  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    error = null;
    confirming = null; // a refresh clears any dangling inline Uninstall?/Remove?
    try {
      const d = await Bridge.Plugins();
      if (seq === loadSeq) data = d;
    } catch (e) {
      if (seq === loadSeq) error = e instanceof Error ? e.message : String(e);
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
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      delete acting["p:" + name];
    }
  }
  async function toggleMarket(name: string, enabled: boolean) {
    acting["m:" + name] = true;
    try {
      await Bridge.SetMarketEnabled(name, enabled);
      toasts.success(`${enabled ? "enabled" : "disabled"} ${name}`);
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
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
      await load();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
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
      {#each Array(3) as _, i (i)}<div class="plug__skel"></div>{/each}
    </div>
  {:else if error && !data}
    <EmptyState glyph="⊞" title="Couldn't load plugins" line={error}>
      {#snippet action()}
        <Button variant="secondary" onclick={() => load()}>Retry</Button>
      {/snippet}
    </EmptyState>
  {:else if !data || (data.plugins.length === 0 && data.marketplaces.length === 0)}
    <EmptyState glyph="⊞" title="No plugins installed" line="Plugins add tools, skills, agents, and MCP servers. Install via `eigen plugin install` (bundles are scanned first)." />
  {:else}
    <div class="plug__scroll">
      <section class="plug__section">
        <div class="plug__section-head">
          <h2 class="plug__section-title">Installed plugins</h2>
          <span class="plug__n tnum">{data.plugins.length}</span>
        </div>
        <!-- No GUI install bridge for plugins: untrusted bundles are scanned at
             the CLI install seam, so adding a plugin happens there. Honest hint
             rather than a faked Add control. -->
        <p class="plug__add-hint">
          Add a plugin from the CLI: <code class="plug__cmd selectable">eigen plugin install &lt;name&gt;</code> — bundles are scanned before they're wired in.
        </p>
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
    padding: var(--sp-8) var(--sp-9);
    display: flex;
    flex-direction: column;
    gap: var(--sp-8);
  }
  .plug__pad {
    padding: var(--sp-8) var(--sp-9);
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .plug__skel {
    height: 96px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: plug-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes plug-shimmer {
    to {
      background-position: -200% 0;
    }
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
  .plug__add-hint {
    margin: 0 0 var(--sp-2);
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
  }
  .plug__cmd {
    padding: 1px var(--sp-3);
    border-radius: var(--r-xs);
    background: var(--bg-overlay);
    border: 1px solid var(--border-hairline);
    font: var(--fw-regular) var(--fs-code-sm) / 1.4 var(--font-mono);
    color: var(--text-secondary);
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
  @media (prefers-reduced-motion: reduce) {
    .plug__skel {
      animation: none;
    }
  }
</style>
