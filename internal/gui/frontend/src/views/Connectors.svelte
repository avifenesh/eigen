<script lang="ts">
  // Connectors — the desktop "superapp" integrations surface. A connector is a
  // REMOTE MCP server (Streamable HTTP) authorized over OAuth: Google Workspace,
  // Slack, Notion, Linear, etc. Add one by name + URL → the OAuth flow opens the
  // browser → the token lands in the OS keychain and the connector goes live.
  // A second section is the raw MCP server wiring editor (stdio + remote), so
  // adding/editing/removing servers no longer means hand-editing mcp.json.
  import { Bridge } from "$lib/bridge";
  import { errText } from "$lib/errors";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { ev, on } from "$lib/events";
  import type {
    ConnectorsDTO,
    ConnectorDTO,
    ConnectorEventDTO,
    CatalogEntryDTO,
    MCPServersDTO,
    MCPServerDTO,
    GoogleStatusDTO,
  } from "$lib/types";
  import Card from "$lib/components/Card.svelte";
  import Button from "$lib/components/Button.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";

  let conns = $state<ConnectorsDTO | null>(null);
  let servers = $state<MCPServersDTO | null>(null);
  let gstatus = $state<GoogleStatusDTO | null>(null);
  let gBusy = $state(false);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let busy = $state<Record<string, boolean>>({});
  let connecting = $state<Record<string, boolean>>({}); // name → OAuth flow in flight

  // Add-connector form.
  let addOpen = $state(false);
  let addName = $state("");
  let addURL = $state("");
  let addDesc = $state("");
  let adding = $state(false);

  // Add-local-MCP-server form (stdio). Secrets routed to the OS keychain.
  let srvOpen = $state(false);
  let srvName = $state("");
  let srvCommand = $state(""); // shell-style: "node server.js" → split on spaces
  let srvDesc = $state("");
  let srvEnv = $state(""); // KEY=VALUE per line (plaintext)
  let srvSecret = $state(""); // KEY=VALUE per line (keychain)
  let srvSaving = $state(false);
  let secretsOk = $state(true);

  let alive = true;
  let loadSeq = 0;
  async function load() {
    const seq = ++loadSeq;
    loading = true;
    error = null;
    try {
      const [c, s, g] = await Promise.all([Bridge.Connectors(), Bridge.MCPServers(), Bridge.GoogleStatus()]);
      if (alive && seq === loadSeq) {
        conns = c;
        servers = s;
        gstatus = g;
      }
    } catch (e) {
      if (alive && seq === loadSeq) error = errText(e);
    } finally {
      if (alive && seq === loadSeq) loading = false;
    }
  }
  $effect(() => {
    load();
    Bridge.MCPSecretsAvailable()
      .then((ok) => {
        if (alive) secretsOk = ok;
      })
      .catch(() => {});
    // A background OAuth flow finishing fires eigen:connector — refresh + toast.
    const off = on<ConnectorEventDTO>(ev.connector, (d) => {
      if (!alive) return;
      connecting[d.name] = false;
      if (d.ok) toasts.success(`${d.name} connected`);
      else toasts.error(`${d.name}: ${d.error || "authorization failed"}`);
      load();
    });
    return () => {
      alive = false;
      loadSeq++;
      off();
    };
  });

  async function addConnector() {
    const name = addName.trim();
    const url = addURL.trim();
    if (!name || !url) {
      toasts.error("Name and URL are required");
      return;
    }
    adding = true;
    try {
      await Bridge.AddConnector(name, url, addDesc.trim());
      connecting[name] = true; // browser flow now running
      toasts.info(`Opening browser to authorize ${name}…`);
      addOpen = false;
      addName = addURL = addDesc = "";
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      adding = false;
    }
  }

  async function addFromCatalog(e: CatalogEntryDTO) {
    if (e.added) return;
    connecting[e.name] = true;
    try {
      await Bridge.AddCatalogConnector(e.name);
      toasts.info(`Opening browser to authorize ${e.display}…`);
      await load();
    } catch (err) {
      connecting[e.name] = false;
      toasts.error(errText(err));
    }
  }

  async function connect(c: ConnectorDTO) {
    connecting[c.name] = true;
    try {
      await Bridge.ConnectConnector(c.name);
      toasts.info(`Opening browser to authorize ${c.name}…`);
    } catch (e) {
      connecting[c.name] = false;
      toasts.error(errText(e));
    }
  }

  async function disconnect(c: ConnectorDTO) {
    busy[c.name] = true;
    try {
      await Bridge.DisconnectConnector(c.name);
      toasts.success(`${c.name} disconnected`);
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      busy[c.name] = false;
    }
  }

  async function removeConnector(c: ConnectorDTO) {
    busy[c.name] = true;
    try {
      await Bridge.RemoveConnector(c.name);
      toasts.success(`${c.name} removed`);
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      busy[c.name] = false;
    }
  }

  async function toggleServer(s: MCPServerDTO) {
    busy[s.name] = true;
    try {
      await Bridge.SetMCPServerDisabled(s.name, !s.disabled);
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      busy[s.name] = false;
    }
  }

  async function removeServer(s: MCPServerDTO) {
    busy[s.name] = true;
    try {
      await Bridge.RemoveMCPServer(s.name);
      toasts.success(`${s.name} removed`);
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      busy[s.name] = false;
    }
  }

  async function setupGoogle() {
    // Open the Cloud Console (create a Desktop OAuth client), then import the JSON.
    if (gstatus?.setupUrl) {
      try {
        const { Browser } = await import("@wailsio/runtime");
        Browser.OpenURL(gstatus.setupUrl);
      } catch {
        /* opening the console is a convenience; import still works without it */
      }
    }
    gBusy = true;
    try {
      const imported = await Bridge.ImportGoogleClient();
      if (imported) {
        toasts.success("Google client imported — now click Connect");
        await load();
      }
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      gBusy = false;
    }
  }

  async function connectGoogle() {
    gBusy = true;
    try {
      toasts.info("Opening browser to authorize Google…");
      await Bridge.ConnectGoogle();
      toasts.success("Google connected");
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      gBusy = false;
    }
  }
  async function disconnectGoogle() {
    gBusy = true;
    try {
      await Bridge.DisconnectGoogle();
      toasts.success("Google disconnected");
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      gBusy = false;
    }
  }

  function splitLines(s: string): string[] {
    return s
      .split("\n")
      .map((l) => l.trim())
      .filter(Boolean);
  }

  async function saveLocalServer() {
    const name = srvName.trim();
    const cmd = srvCommand.trim().split(/\s+/).filter(Boolean);
    if (!name || cmd.length === 0) {
      toasts.error("Name and command are required");
      return;
    }
    srvSaving = true;
    try {
      await Bridge.SaveMCPServer({
        name,
        command: cmd,
        description: srvDesc.trim(),
        disabled: false,
        remote: false,
        envPairs: splitLines(srvEnv),
        secretEnvPairs: secretsOk ? splitLines(srvSecret) : [],
        // when no keyring, fold "secret" lines into plaintext env rather than drop
        ...(secretsOk ? {} : { envPairs: [...splitLines(srvEnv), ...splitLines(srvSecret)] }),
      });
      toasts.success(`${name} saved`);
      srvOpen = false;
      srvName = srvCommand = srvDesc = srvEnv = srvSecret = "";
      await load();
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      srvSaving = false;
    }
  }

  function expiryLabel(c: ConnectorDTO): string {
    if (!c.connected) return "";
    if (!c.expiry) return "connected";
    const d = new Date(c.expiry);
    if (isNaN(d.getTime())) return "connected";
    return "token valid · renews automatically";
  }

  // Stdio servers (the non-connector half of the wiring editor).
  const stdioServers = $derived((servers?.servers ?? []).filter((s) => !s.remote));
</script>

<div class="cx">
  <div class="cx__scroll">
    <header class="cx__head">
      <h2 class="cx__title">Connectors</h2>
      <p class="cx__sub">
        Connect external apps over the Model Context Protocol. Each connector is a
        remote MCP server you authorize once with OAuth — the token is stored in
        your OS keychain and refreshes automatically.
      </p>
    </header>

    {#if loading && !conns}
      <div class="cx__skel-wrap">
        {#each Array(3) as _, i (i)}<div class="cx__skel"></div>{/each}
      </div>
    {:else if error && !conns}
      <EmptyState glyph="⟐" title="Couldn't load connectors" line={error}>
        {#snippet action()}<Button variant="secondary" onclick={() => load()}>Retry</Button>{/snippet}
      </EmptyState>
    {:else}
      <!-- Google (native built-in: Calendar + Gmail) -->
      {#if gstatus}
        <Card>
          <div class="conn">
            <div class="conn__info">
              <div class="conn__name">
                <span class="conn__glyph">📅</span>
                <span class="conn__title">Google</span>
                {#if gstatus.connected}
                  <span class="badge badge--ok">connected</span>
                {:else if gstatus.configured}
                  <span class="badge badge--off">not connected</span>
                {:else}
                  <span class="badge badge--off">setup needed</span>
                {/if}
              </div>
              <p class="conn__desc">Calendar + Gmail — read events &amp; email, create events.</p>
              {#if !gstatus.configured}<p class="conn__url">Set up opens Google Cloud Console — create a Desktop OAuth client, then pick the downloaded JSON.</p>{/if}
            </div>
            <div class="conn__ops">
              {#if gstatus.connected}
                <Button variant="secondary" disabled={gBusy} onclick={disconnectGoogle}>Disconnect</Button>
              {:else if gstatus.configured}
                <Button variant="primary" disabled={gBusy} onclick={connectGoogle}>
                  {gBusy ? "Authorizing…" : "Connect"}
                </Button>
              {:else}
                <Button variant="primary" disabled={gBusy} onclick={setupGoogle}>
                  {gBusy ? "Importing…" : "Set up"}
                </Button>
              {/if}
            </div>
          </div>
        </Card>
      {/if}

      <!-- Add connector -->
      <div class="cx__actions">
        {#if !addOpen}
          <Button variant="primary" onclick={() => (addOpen = true)}>+ Add connector</Button>
        {/if}
      </div>
      {#if addOpen}
        <Card>
          <div class="add">
            <div class="add__row">
              <label class="add__lbl" for="cx-name">Name</label>
              <input id="cx-name" class="add__in" bind:value={addName} placeholder="notion" />
            </div>
            <div class="add__row">
              <label class="add__lbl" for="cx-url">Remote MCP URL</label>
              <input id="cx-url" class="add__in" bind:value={addURL} placeholder="https://mcp.notion.com/mcp" />
            </div>
            <div class="add__row">
              <label class="add__lbl" for="cx-desc">Description</label>
              <input id="cx-desc" class="add__in" bind:value={addDesc} placeholder="Notion workspace (optional)" />
            </div>
            <div class="add__btns">
              <Button variant="primary" disabled={adding} onclick={addConnector}>
                {adding ? "Saving…" : "Add & authorize"}
              </Button>
              <Button variant="ghost" disabled={adding} onclick={() => (addOpen = false)}>Cancel</Button>
            </div>
          </div>
        </Card>
      {/if}

      <!-- Connector list -->
      {#if conns && conns.connectors.length > 0}
        <div class="list">
          {#each conns.connectors as c (c.name)}
            <Card>
              <div class="conn">
                <div class="conn__info">
                  <div class="conn__name">
                    {#if c.glyph}<span class="conn__glyph">{c.glyph}</span>{/if}
                    <span class="conn__title">{c.display || c.name}</span>
                    {#if connecting[c.name]}
                      <span class="badge badge--pending">authorizing…</span>
                    {:else if c.connected}
                      <span class="badge badge--ok">connected</span>
                    {:else}
                      <span class="badge badge--off">not connected</span>
                    {/if}
                    {#if c.disabled}<span class="badge badge--off">disabled</span>{/if}
                  </div>
                  {#if c.description}<p class="conn__desc">{c.description}</p>{/if}
                  <p class="conn__url">{c.url}</p>
                  {#if c.connected}<p class="conn__meta">{expiryLabel(c)}</p>{/if}
                </div>
                <div class="conn__ops">
                  {#if c.connected}
                    <Button variant="secondary" disabled={busy[c.name]} onclick={() => disconnect(c)}>Disconnect</Button>
                  {:else}
                    <Button variant="primary" disabled={connecting[c.name]} onclick={() => connect(c)}>
                      {connecting[c.name] ? "Authorizing…" : "Connect"}
                    </Button>
                  {/if}
                  <Button variant="ghost" disabled={busy[c.name]} onclick={() => removeConnector(c)}>Remove</Button>
                </div>
              </div>
            </Card>
          {/each}
        </div>
      {:else}
        <EmptyState glyph="⟐" title="No connectors yet" line="Pick one from the directory below, or add a remote MCP server by URL." />
      {/if}

      <!-- Directory: one-click browse & connect -->
      {#if conns && conns.directory.length > 0}
        <header class="cx__head cx__head--sub">
          <h3 class="cx__subtitle">Directory</h3>
          <p class="cx__sub">Popular connectors — one click to add and authorize.</p>
        </header>
        <div class="grid">
          {#each conns.directory as e (e.name)}
            <button
              class="tile"
              class:tile--added={e.added}
              disabled={e.added || connecting[e.name]}
              title={e.description}
              onclick={() => addFromCatalog(e)}
            >
              <span class="tile__glyph">{e.glyph}</span>
              <span class="tile__name">{e.display}</span>
              <span class="tile__cat">{e.category}</span>
              <span class="tile__cta">
                {#if e.added}added{:else if connecting[e.name]}authorizing…{:else}+ connect{/if}
              </span>
            </button>
          {/each}
        </div>
      {/if}

      <!-- Local MCP servers (wiring) -->
      <header class="cx__head cx__head--sub">
        <h3 class="cx__subtitle">Local MCP servers</h3>
        <p class="cx__sub">
          Stdio servers wired in mcp.json — add, toggle, or remove them here.
          {#if secretsOk}Secrets you mark are stored in your OS keychain, never in mcp.json.{:else}<span class="warn">No OS keychain detected — secret values would be stored as plaintext.</span>{/if}
        </p>
      </header>
      <div class="cx__actions">
        {#if !srvOpen}
          <Button variant="secondary" onclick={() => (srvOpen = true)}>+ Add MCP server</Button>
        {/if}
      </div>
      {#if srvOpen}
        <Card>
          <div class="add">
            <div class="add__row">
              <label class="add__lbl" for="srv-name">Name</label>
              <input id="srv-name" class="add__in" bind:value={srvName} placeholder="github" />
            </div>
            <div class="add__row">
              <label class="add__lbl" for="srv-cmd">Command</label>
              <input id="srv-cmd" class="add__in" bind:value={srvCommand} placeholder="docker run ghcr.io/github/github-mcp-server" />
            </div>
            <div class="add__row">
              <label class="add__lbl" for="srv-desc">Description</label>
              <input id="srv-desc" class="add__in" bind:value={srvDesc} placeholder="GitHub MCP (optional)" />
            </div>
            <div class="add__row">
              <label class="add__lbl" for="srv-env">Env (KEY=VALUE per line)</label>
              <textarea id="srv-env" class="add__ta" bind:value={srvEnv} rows="2" placeholder="LOG_LEVEL=info"></textarea>
            </div>
            <div class="add__row">
              <label class="add__lbl" for="srv-secret">
                {secretsOk ? "Secret env → keychain (KEY=VALUE per line)" : "Secret env (no keychain — stored as plaintext)"}
              </label>
              <textarea id="srv-secret" class="add__ta" bind:value={srvSecret} rows="2" placeholder="GITHUB_PERSONAL_ACCESS_TOKEN=ghp_…"></textarea>
            </div>
            <div class="add__btns">
              <Button variant="primary" disabled={srvSaving} onclick={saveLocalServer}>
                {srvSaving ? "Saving…" : "Save server"}
              </Button>
              <Button variant="ghost" disabled={srvSaving} onclick={() => (srvOpen = false)}>Cancel</Button>
            </div>
          </div>
        </Card>
      {/if}
      {#if stdioServers.length > 0}
        <div class="list">
          {#each stdioServers as s (s.name)}
            <Card>
              <div class="conn">
                <div class="conn__info">
                  <div class="conn__name">
                    <span class="conn__title">{s.name}</span>
                    {#if s.disabled}<span class="badge badge--off">disabled</span>{:else}<span class="badge badge--ok">enabled</span>{/if}
                    {#if s.secretEnvKeys && s.secretEnvKeys.length > 0}<span class="badge badge--secret" title={s.secretEnvKeys.join(", ")}>🔑 {s.secretEnvKeys.length} secret</span>{/if}
                  </div>
                  {#if s.description}<p class="conn__desc">{s.description}</p>{/if}
                  {#if s.command}<p class="conn__url">{s.command.join(" ")}</p>{/if}
                </div>
                <div class="conn__ops">
                  <Button variant="secondary" disabled={busy[s.name]} onclick={() => toggleServer(s)}>
                    {s.disabled ? "Enable" : "Disable"}
                  </Button>
                  <Button variant="ghost" disabled={busy[s.name]} onclick={() => removeServer(s)}>Remove</Button>
                </div>
              </div>
            </Card>
          {/each}
        </div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .cx {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .cx__scroll {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: var(--sp-8) var(--sp-9);
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
    max-width: 920px;
  }
  .cx__head {
    padding: 0 var(--sp-2);
  }
  .cx__head--sub {
    margin-top: var(--sp-6);
  }
  .cx__title {
    margin: 0;
    font: var(--fw-semibold) var(--fs-h3) / 1.2 var(--font-sans);
    color: var(--text-primary);
  }
  .cx__subtitle {
    margin: 0;
    font: var(--fw-semibold) var(--fs-body) / 1.2 var(--font-sans);
    color: var(--text-primary);
  }
  .cx__sub {
    margin: var(--sp-2) 0 0;
    color: var(--text-muted);
    font-size: var(--fs-label);
    line-height: var(--lh-snug);
    max-width: 72ch;
  }
  .cx__skel-wrap {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .cx__skel {
    height: 76px;
    border-radius: var(--r-md);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: cx-shimmer 1.4s ease-in-out infinite;
  }
  @keyframes cx-shimmer {
    to {
      background-position: -200% 0;
    }
  }
  .cx__actions {
    display: flex;
    justify-content: flex-end;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .conn {
    display: flex;
    align-items: center;
    gap: var(--sp-5);
    padding: var(--sp-5) var(--sp-6);
  }
  .conn__info {
    flex: 1;
    min-width: 0;
  }
  .conn__name {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .conn__title {
    font: var(--fw-semibold) var(--fs-body-sm) / 1.2 var(--font-mono);
    color: var(--text-primary);
  }
  .conn__desc {
    margin: var(--sp-2) 0 0;
    color: var(--text-muted);
    font-size: var(--fs-label);
  }
  .conn__url {
    margin: var(--sp-1) 0 0;
    color: var(--text-faint);
    font: var(--fw-regular) var(--fs-micro) / 1.3 var(--font-mono);
    word-break: break-all;
  }
  .conn__meta {
    margin: var(--sp-1) 0 0;
    color: var(--ok, var(--brand));
    font-size: var(--fs-micro);
  }
  .conn__ops {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .badge {
    font: var(--fw-semibold) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 2px var(--sp-3);
    border-radius: var(--r-full);
    border: 1px solid var(--border-subtle);
  }
  .badge--ok {
    background: var(--state-selected);
    color: var(--brand-bright);
    border-color: var(--border-brand-faint);
  }
  .badge--pending {
    background: var(--working-bg);
    color: var(--working);
    border-color: var(--working);
  }
  .badge--off {
    background: var(--bg-raised-2);
    color: var(--text-faint);
  }
  .add {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-5) var(--sp-6);
  }
  .add__row {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
  }
  .add__lbl {
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
    color: var(--text-muted);
  }
  .add__in {
    height: 34px;
    padding: 0 var(--sp-4);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-sm);
    background: var(--bg-raised-2);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body-sm) / 1 var(--font-mono);
    outline: none;
  }
  .add__in:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .add__ta {
    padding: var(--sp-2) var(--sp-4);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-sm);
    background: var(--bg-raised-2);
    color: var(--text-primary);
    font: var(--fw-regular) var(--fs-body-sm) / 1.4 var(--font-mono);
    outline: none;
    resize: vertical;
  }
  .add__ta:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .add__btns {
    display: flex;
    gap: var(--sp-2);
    margin-top: var(--sp-2);
  }
  .warn {
    color: var(--warn, var(--danger));
  }
  .badge--secret {
    background: var(--bg-raised-2);
    color: var(--text-muted);
    border-color: var(--border-brand-faint);
  }
  .conn__glyph {
    font-size: var(--fs-body);
    line-height: 1;
  }

  /* Directory grid */
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
    gap: var(--sp-3);
  }
  .tile {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--sp-1);
    padding: var(--sp-4) var(--sp-5);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--bg-raised-2);
    cursor: pointer;
    text-align: left;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .tile:hover:not(:disabled) {
    border-color: var(--border-brand-faint);
    background: var(--state-hover);
  }
  .tile:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .tile--added {
    opacity: 0.55;
    cursor: default;
  }
  .tile__glyph {
    font-size: 1.4rem;
    line-height: 1;
  }
  .tile__name {
    font: var(--fw-semibold) var(--fs-body-sm) / 1.2 var(--font-sans);
    color: var(--text-primary);
  }
  .tile__cat {
    font-size: var(--fs-micro);
    color: var(--text-faint);
  }
  .tile__cta {
    margin-top: var(--sp-2);
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
    color: var(--brand-bright);
  }
  .tile--added .tile__cta {
    color: var(--text-faint);
  }
  @media (prefers-reduced-motion: reduce) {
    .cx__skel {
      animation: none;
    }
  }
</style>
