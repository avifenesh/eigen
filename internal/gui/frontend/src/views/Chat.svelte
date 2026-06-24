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
  import type { SessionStateDTO, ModelDTO, ImageDTO } from "$lib/types";
  import Composer from "$lib/components/Composer.svelte";
  import ToolCallCard from "$lib/components/ToolCallCard.svelte";
  import Markdown from "$lib/components/Markdown.svelte";
  import VirtualList from "$lib/components/VirtualList.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Button from "$lib/components/Button.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import Popover from "$lib/components/Popover.svelte";

  let { param }: { param?: string } = $props();

  // Resolve the session to show: route param, else the newest session.
  const sessionId = $derived(param ?? sessions.list[0]?.id ?? "");

  // A routed session that no longer exists (removed/pruned while open here):
  // param pins a concrete id, sessions have loaded, but it's not in the list.
  // Without this guard Chat would render a live composer over a dead session.
  const missing = $derived(
    !!param && sessions.loaded && !sessions.list.some((s) => s.id === param),
  );

  let store = $state<Transcript | null>(null);
  let sess = $state<SessionStateDTO | null>(null);
  // True from the moment we kick off attach() until the first State() snapshot
  // lands (or fails). Drives the transcript skeleton so a session with history
  // on a slow daemon/ssh shows a loading shimmer rather than a bare empty list
  // for the round-trip. Cleared in both the .then and .catch arms of State().
  let loading = $state(false);

  // Per-session lifecycle. Re-runs when sessionId changes; cleanup tears the
  // previous session down completely before the next is set up.
  $effect(() => {
    const id = sessionId;
    if (!id || missing) {
      store = null;
      sess = null;
      loading = false;
      return;
    }
    // Drop the PRIOR session's snapshot before we attach to this id. Until the
    // State() round-trip below lands, sess belongs to the session we just left
    // (or is null on first mount) — rendering dock values or deriving isEmpty
    // from it would show stale data for the new session (GUI-064/GUI-066). The
    // skeleton (transcriptLoading) covers the gap; isEmpty stays false while
    // sess is null so the warm starter never flashes against the wrong session.
    sess = null;
    let alive = true;
    const t = createTranscript(id);

    // attach: (re)open the backend pump for this session and (re)seed the
    // transcript from the authoritative State snapshot. The frontend event
    // listener is registered ONCE in t.start() on a stable event name, so this
    // is safe to call again on reconnect without double-subscribing.
    function attach() {
      loading = true;
      Bridge.Subscribe(id).catch((e) => toasts.error("subscribe: " + (e instanceof Error ? e.message : String(e))));
      Bridge.State(id)
        .then((s) => {
          if (!alive) return;
          loading = false;
          if (!s) return;
          sess = s;
          t.seed(s.messages, s.running ?? false);
        })
        .catch((e) => {
          if (alive) loading = false;
          toasts.error("state: " + (e instanceof Error ? e.message : String(e)));
        });
    }

    attach();
    t.start();
    store = t;

    // The daemon signals view-close if the connection drops underneath us.
    const offClosed = on(ev.sessionClosed(id), () => {
      if (alive) toasts.info("session stream closed — reconnecting…");
    });
    // When the daemon comes back, the old pump is gone (its connection died);
    // re-attach so the stream and the running/idle state recover rather than
    // wedging on a stale "working" snapshot. Remover runs in this cleanup.
    const offReconnect = daemon.onReconnect(() => {
      if (alive) attach();
    });

    return () => {
      alive = false;
      offClosed();
      offReconnect();
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

  // The completed history, fed to VirtualList as a STABLE reference (windowed;
  // only visible rows mount). The in-flight `live` block is NOT concatenated in
  // here — spreading the whole history (up to CAP=2000) on every live delta
  // (~60×/sec while streaming) re-derived the windowed offsets each frame
  // (GUI-069). Instead `live` is rendered as a single fixed trailing row below
  // the list, so a live token append touches one node, not the list geometry.
  const history = $derived(store?.history ?? []);
  const live = $derived(store?.live ?? null);

  const online = $derived(daemon.status === "online");

  // ── turn I/O ──────────────────────────────────────────────────────────────
  // Steer routing: mid-turn input is injected into the running turn (steered)
  // rather than queued as a fresh turn. Images (composer attachments) ride along
  // on both paths; starter chips call send(text) with none, so the param
  // defaults to an empty list.
  //
  // The local `running` flag is a reactive view of stream events and can be
  // stale right at a turn boundary: between the 'done' event and an Enter, or
  // before the first delta of a server-restarted turn flips running=true. So we
  // don't branch on it. The daemon is the only authority on whether a turn is
  // live, so always try SteerInput first — it returns true when the text was
  // injected into a running turn, false when there was no turn to steer — and
  // only fall back to SendInput (a fresh queued turn) when it returns false.
  // That closes the window where a stale running view queues a duplicate or
  // competing turn.
  // Returns true on a successful send so the Composer clears its draft + image
  // blobs only then; on any failure it returns false and the draft survives for
  // a retry (the Composer no longer fire-and-forgets the clear).
  async function send(text: string, images: ImageDTO[] = []): Promise<boolean> {
    if (!sessionId) return false;
    // Slash-command routing: a leading "/<name> [args]" that matches an authored
    // custom command runs through RunCommand (the command's expanded prompt is
    // submitted as the turn) rather than being sent as literal chat text. Plain
    // text — and an unknown /name — falls through to the normal send path.
    if (images.length === 0) {
      const handled = await maybeRunCommand(text);
      if (handled !== null) return handled;
    }
    // The local running flag only colors the messaging below; it never decides
    // the RPC. Capture it before the await so the toast reflects the user's
    // intent at send time, not whatever the stream has done by the time the
    // round-trip lands.
    const expectedSteer = store?.running ?? false;
    try {
      const steered = await Bridge.SteerInput(sessionId, text, images);
      if (steered) {
        toasts.info("steered into the running turn");
      } else {
        // No running turn to steer into: send as a fresh turn. When the user
        // typed expecting to steer mid-turn this is a behavior change, so the
        // toast surfaces it rather than queuing silently — but on a plain idle
        // send (the common case) there's nothing to surface, so stay quiet.
        // (GUI-034 steer-fallback toast — keep it.)
        await Bridge.SendInput(sessionId, text, images, []);
        if (expectedSteer) toasts.info("couldn't steer — queued as a new turn");
      }
      return true;
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
      return false;
    }
  }

  // Custom slash commands (~/.eigen/commands + project .eigen/commands), loaded
  // once on demand. A user-authored "/review" should run its expanded prompt as
  // a turn, not be sent as the literal string "/review" — the TUI does this; the
  // GUI did not. Returns: true (ran ok), false (matched but failed — keep draft),
  // or null (not a slash command / no match → caller sends as normal text).
  let commandNames = $state<Set<string> | null>(null);
  async function maybeRunCommand(text: string): Promise<boolean | null> {
    const m = text.match(/^\/([A-Za-z0-9][\w-]*)(?:\s+([\s\S]*))?$/);
    if (!m) return null;
    const [, name, args = ""] = m;
    if (!commandNames) {
      try {
        const cmds = await Bridge.Commands();
        commandNames = new Set((cmds ?? []).map((c) => c.name));
      } catch {
        commandNames = new Set();
      }
    }
    if (!commandNames.has(name)) return null;
    if (!sessionId) return false;
    try {
      await Bridge.RunCommand(sessionId, name, args.trim());
      return true;
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
      return false;
    }
  }

  // Start a fresh session without leaving Chat — new-chat from the control bar.
  let startingNew = $state(false);
  async function newChat() {
    if (startingNew) return;
    startingNew = true;
    try {
      const id = await Bridge.NewSession("", "", "");
      await sessions.refresh();
      router.go("chat", id);
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      startingNew = false;
    }
  }

  // Stop the running turn. Re-entrant guarded: without it the user can mash
  // Stop and fire Bridge.Interrupt repeatedly while the first RPC is still in
  // flight (the button stays "Stop" until `done` clears running) — GUI-068.
  // `interrupting` flips the composer's primary action to a disabled
  // "stopping…" affordance; it clears on RPC resolve here AND on running→false
  // (the $effect below), whichever lands first, so a turn that ends before the
  // RPC returns still releases the button.
  let interrupting = $state(false);
  async function interrupt() {
    if (!sessionId || interrupting) return;
    interrupting = true;
    try {
      await Bridge.Interrupt(sessionId);
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      interrupting = false;
    }
  }
  // Belt-and-suspenders: if the turn ends (running→false) while the Interrupt
  // RPC is still pending, drop the interrupting state so the composer doesn't
  // wedge on "stopping…". Harmless when the finally arm already cleared it.
  $effect(() => {
    if (store?.running === false && interrupting) interrupting = false;
  });
  // Background the turn's foreground shell so a turn wedged on a long-running
  // command is freed WITHOUT killing it (vs. Interrupt, which kills the turn).
  // The backgrounded shell then shows up in the shells dock — refresh so it
  // lands without waiting for the next State trigger.
  let detaching = $state(false);
  async function detachBash() {
    if (!sessionId || detaching) return;
    detaching = true;
    try {
      const ok = await Bridge.DetachBash(sessionId);
      if (ok) toasts.info("backgrounded the shell — turn freed");
      else toasts.info("no foreground shell to background");
      refreshState();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    } finally {
      detaching = false;
    }
  }
  // Resolve a gated approval; surface failures (a dropped daemon between gate
  // and click) instead of swallowing them, then refresh so the card clears.
  async function approve(approvalID: string, allow: boolean) {
    if (!sessionId) return;
    try {
      await Bridge.Approve(sessionId, approvalID, allow);
      refreshState();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
    }
  }

  const pct = $derived(
    sess && sess.maxTokens > 0 ? Math.min(100, Math.round((sess.tokens / sess.maxTokens) * 100)) : 0,
  );
  // Near-context-limit nudge: surface only once we're genuinely close and the
  // max is known, so it reads as a real prompt to compact rather than chrome.
  const nearLimit = $derived(sess != null && sess.maxTokens > 0 && sess.tokens / sess.maxTokens > 0.85);

  // ── settings panel: capability-gated mutators ──────────────────────────────
  // Each mutator returns the FRESH state — assign it to `sess` so the dock
  // badges reconcile in place without a round-trip through refreshState().
  // `forId` is the session current when the RPC was issued: if the user switched
  // sessions while it was in flight, the stale result is dropped (mirrors the id
  // guard in refreshState — Chat is keyed on route, not session id).
  function applyState(forId: string, s: SessionStateDTO | null) {
    if (s && forId === sessionId) sess = s;
  }
  async function run<T>(fn: () => Promise<T>): Promise<T | undefined> {
    try {
      return await fn();
    } catch (e) {
      toasts.error(e instanceof Error ? e.message : String(e));
      return undefined;
    }
  }

  let settingsOpen = $state(false);
  let menuOpen = $state(false);
  // Routing model ids load once when the settings panel first opens.
  let models = $state<ModelDTO[]>([]);
  let modelsLoaded = $state(false);
  // The current model's own effort ladder (when reasoning is supported), so the
  // effort selector offers exactly what the model takes rather than a guess.
  const EFFORT_FALLBACK = ["off", "minimal", "low", "medium", "high", "xhigh", "max"];
  const effortLevels = $derived.by(() => {
    const m = models.find((x) => x.id === sess?.model);
    const lv = m?.effortLevels;
    return lv && lv.length > 0 ? lv : EFFORT_FALLBACK;
  });
  // Search modes: a model either takes them or not; offer off + the common
  // provider modes when search is active on the session.
  const SEARCH_MODES = ["off", "auto", "on"];

  async function loadModels() {
    if (modelsLoaded) return;
    modelsLoaded = true;
    const r = await run(() => Bridge.Routing());
    if (r?.models) models = r.models;
  }
  $effect(() => {
    if (settingsOpen) loadModels();
  });

  async function onModel(e: Event) {
    const v = (e.currentTarget as HTMLSelectElement).value;
    const id = sessionId;
    if (!id || v === sess?.model) return;
    applyState(id, await run(() => Bridge.SetModel(id, v)) ?? null);
  }
  async function onPerm() {
    const id = sessionId;
    if (!id) return;
    const next = sess?.perm === "auto" ? "gated" : "auto";
    applyState(id, await run(() => Bridge.SetPerm(id, next)) ?? null);
  }
  async function onEffort(e: Event) {
    const v = (e.currentTarget as HTMLSelectElement).value;
    const id = sessionId;
    if (!id) return;
    applyState(id, await run(() => Bridge.SetEffort(id, v)) ?? null);
  }
  async function onSearch(e: Event) {
    const v = (e.currentTarget as HTMLSelectElement).value;
    const id = sessionId;
    if (!id) return;
    applyState(id, await run(() => Bridge.SetSearch(id, v)) ?? null);
  }
  async function onFast() {
    const id = sessionId;
    if (!id) return;
    applyState(id, await run(() => Bridge.SetFast(id, !sess?.fast)) ?? null);
  }

  // ── goal + title editing ────────────────────────────────────────────────
  let editingGoal = $state(false);
  let goalDraft = $state("");
  let editingTitle = $state(false);
  let titleDraft = $state("");
  // The title shown when the session hasn't been explicitly named.
  const derivedTitle = $derived(sessions.list.find((s) => s.id === sessionId)?.title || "untitled session");

  function startGoal() {
    goalDraft = sess?.goal ?? "";
    editingGoal = true;
  }
  async function commitGoal() {
    editingGoal = false;
    const id = sessionId;
    if (!id) return;
    const next = goalDraft.trim();
    if (next === (sess?.goal ?? "")) return;
    applyState(id, await run(() => Bridge.SetGoal(id, next)) ?? null);
  }
  function startTitle() {
    titleDraft = sess?.title ?? "";
    editingTitle = true;
  }
  async function commitTitle() {
    editingTitle = false;
    const id = sessionId;
    if (!id) return;
    const next = titleDraft.trim();
    if (next === (sess?.title ?? "")) return;
    applyState(id, await run(() => Bridge.SetTitle(id, next)) ?? null);
  }

  // ── sandbox / roots ───────────────────────────────────────────────────────
  let dirDraft = $state("");
  let addingDir = $state(false);
  async function addDir() {
    const path = dirDraft.trim();
    if (!sessionId || !path || addingDir) return;
    addingDir = true;
    const root = await run(() => Bridge.AddDir(sessionId, path));
    addingDir = false;
    if (root !== undefined) {
      dirDraft = "";
      toasts.info("added " + root);
      refreshState();
    }
  }

  // ── shells ────────────────────────────────────────────────────────────────
  async function killShell(shellID: string) {
    if (!sessionId) return;
    const ok = await run(() => Bridge.KillShell(sessionId, shellID));
    if (ok) refreshState();
  }

  // ── session menu: compact / clear / resend ─────────────────────────────────
  let compacting = $state(false);
  let confirmClear = $state(false);
  async function compact() {
    if (!sessionId || compacting) return;
    compacting = true;
    const res = await run(() => Bridge.Compact(sessionId, 0));
    compacting = false;
    if (res) {
      toasts.info(`compacted ${res.before.toLocaleString()}→${res.after.toLocaleString()}`);
      refreshState();
    }
    menuOpen = false;
  }
  async function clearSession() {
    if (!sessionId) return;
    confirmClear = false;
    menuOpen = false;
    const r = await run(() => Bridge.Clear(sessionId));
    if (r !== undefined) {
      // The daemon dispatches NO event on clear, and refreshState only updates
      // `sess` — neither touches the transcript store, so the cleared blocks
      // would stay on screen until a remount. Re-seed the transcript to empty
      // here (seed() also resets the live/pending/raf state) so the view
      // converges with the now-empty session immediately.
      store?.seed([], false);
      refreshState();
    }
  }
  async function resend() {
    if (!sessionId) return;
    menuOpen = false;
    await run(() => Bridge.Resend(sessionId));
  }

  // ── approval args ───────────────────────────────────────────────────────
  // Each pending approval carries the raw tool args (a JSON blob from the
  // daemon). Surface them under the tool name so the user sees WHAT they are
  // allowing rather than approving blind. Pretty-print when it parses as JSON,
  // else show the raw string. Collapsed to a short preview by default; an
  // expandable map (keyed by approval id) toggles the full scrollable block.
  // The map is a $state object — Svelte 5 proxies it, so in-place writes stay
  // reactive and no teardown is needed (no listeners/timers/observers here).
  let argsOpen = $state<Record<string, boolean>>({});
  function toggleArgs(id: string) {
    argsOpen[id] = !argsOpen[id];
  }
  function prettyArgs(raw: string): string {
    const s = (raw ?? "").trim();
    if (!s) return "";
    try {
      const v = JSON.parse(s);
      return JSON.stringify(v, null, 2);
    } catch {
      return s;
    }
  }

  // ── empty-session starters ──────────────────────────────────────────────
  // Genuinely empty session: the authoritative State snapshot for the CURRENT
  // id has landed (sess != null, not loading) with no messages, and nothing has
  // streamed/seeded into the transcript (history empty, no live block, idle).
  // The loading guard + sess reset on id change (GUI-064) keep sess from being a
  // PRIOR non-empty snapshot during the async gap, so the warm starter only
  // shows for a real empty session — never as a flash while State resolves
  // (GUI-066).
  const isEmpty = $derived(
    !loading &&
      sess != null &&
      sess.messages.length === 0 &&
      history.length === 0 &&
      !live &&
      !store?.running,
  );
  // The transcript is still being fetched: attach() fired but the first State()
  // snapshot hasn't landed (sess null) and nothing has streamed in yet (no
  // rows). Distinct from isEmpty — a session WITH history shows the skeleton for
  // the round-trip instead of a bare empty list, then pops to the transcript.
  const transcriptLoading = $derived(loading && sess == null && history.length === 0 && !live);
  const starters = [
    "Give me a tour of this codebase.",
    "What changed in the last few commits?",
    "Find and explain the riskiest function here.",
  ];

  // Screen-reader completion mirror. VirtualList only mounts visible rows, and
  // the streamed assistant tokens append silently to off-screen DOM, so a SR
  // user gets no notification a turn finished. This sr-only aria-live region
  // carries the latest FINALIZED assistant prose: once the turn ends (no live
  // block streaming) and the newest history block is assistant text, announce
  // it. Empty while a turn streams or when the tail isn't assistant prose, so
  // mid-stream churn and user/tool/reasoning rows don't spam the announcer.
  const lastAssistant = $derived.by(() => {
    if (!store || store.running || store.live) return "";
    const h = store.history;
    const tail = h[h.length - 1];
    return tail && tail.kind === "text" ? tail.text : "";
  });

  // A human label for a transcript block's kind, voiced as the row's aria-label
  // so a SR user hears what each row is (assistant prose, the model's reasoning,
  // a tool call, or a system note) rather than an unlabeled region. Tool rows
  // delegate their detail to ToolCallCard; this names the row around it.
  function rowLabel(kind: string): string {
    switch (kind) {
      case "reasoning":
        return "Assistant reasoning";
      case "tool":
        return "Tool call";
      case "note":
        return "System note";
      default:
        return "Assistant message";
    }
  }

  // A note's tone, derived from its text prefix. finishTurn (daemon-side) emits
  // an abnormal turn end as a plain note — "error: <msg>" for a provider/tool
  // failure, "interrupted" for a user stop — using the SAME text channel as
  // benign notes (route changes, "task → background", compaction). Without a
  // tone a provider failure renders identically to a routine note (GUI-093). We
  // can't add an EventError kind frontend-side (that's APP work), so we read the
  // tone from the existing text, matching the daemon/TUI prefix convention
  // (tui.go: `interrupted` || prefix `error: `) and render an error tone warmly.
  function noteTone(text: string): "error" | "info" {
    const t = (text ?? "").trim();
    return t === "interrupted" || t.startsWith("error:") ? "error" : "info";
  }
</script>

{#if missing}
  <EmptyState glyph="▶" title="Session no longer exists" line="It was removed or pruned. Pick another session or start a new one.">
    {#snippet action()}
      <Button variant="primary" onclick={() => router.go("sessions")}>All sessions</Button>
    {/snippet}
  </EmptyState>
{:else if !sessionId}
  <EmptyState glyph="▶" title="No session selected" line="Start one from Home, or pick a session.">
    {#snippet action()}
      <Button variant="primary" onclick={() => router.go("home")}>Go to Home</Button>
    {/snippet}
  </EmptyState>
{:else}
  <div class="chat">
    <div class="chat__main">
      <!-- CONTROL BAR — the always-visible session controls: a New-chat action,
           the model, and the capability quick-toggles (perm/effort/fast/search).
           These were buried in the dock's "edit" popover; surfacing them here
           makes switching one click, no hunting. The dock keeps goal/dirs/shells. -->
      <div class="ctl">
        <button class="ctl__new" onclick={newChat} disabled={startingNew} title="Start a new chat">
          {#if startingNew}<span class="dock__spinner" aria-hidden="true"></span>{:else}<span class="ctl__plus" aria-hidden="true">+</span>{/if}
          New chat
        </button>
        <div class="ctl__sep"></div>
        <label class="ctl__field" title="Model for this session">
          <span class="ctl__k">model</span>
          <select class="ctl__sel" value={sess?.model ?? ""} onchange={onModel} onfocus={loadModels}>
            {#if models.length === 0}
              <option value={sess?.model ?? ""}>{sess?.model || "—"}</option>
            {:else}
              {#each models as m (m.id)}
                <option value={m.id} disabled={!m.available}>{m.id}{m.available ? "" : " (unavailable)"}</option>
              {/each}
            {/if}
          </select>
        </label>
        <button class="ctl__pill" class:ctl__pill--warn={sess?.perm === "auto"} onclick={onPerm} title="Toggle gated approvals vs auto-approve">
          {sess?.perm === "auto" ? "auto" : "gated"}
        </button>
        {#if sess?.effort}
          <label class="ctl__field" title="Reasoning effort">
            <span class="ctl__k">effort</span>
            <select class="ctl__sel ctl__sel--mini" value={sess.effort} onchange={onEffort}>
              {#each effortLevels as lv (lv)}<option value={lv}>{lv}</option>{/each}
            </select>
          </label>
        {/if}
        {#if sess?.search}
          <label class="ctl__field" title="Live search mode">
            <span class="ctl__k">search</span>
            <select class="ctl__sel ctl__sel--mini" value={sess.search} onchange={onSearch}>
              {#each SEARCH_MODES as mode (mode)}<option value={mode}>{mode}</option>{/each}
            </select>
          </label>
        {/if}
        {#if sess?.fastOk}
          <button class="ctl__pill" class:ctl__pill--on={sess.fast} onclick={onFast} title="Route eligible turns to the fast tier">
            fast {sess.fast ? "on" : "off"}
          </button>
        {/if}
      </div>
      <div class="chat__scroll selectable">
        {#if transcriptLoading}
          <!-- Transcript skeleton: the State() snapshot is still in flight, so
               we can't yet tell an empty session from one with history. Shimmer
               rows + a quiet label stand in for the round-trip rather than a
               bare empty list. Matches the skeletons in Sessions/Live/Machines. -->
          <div class="tload" aria-busy="true" aria-live="polite">
            <div class="tload__rows">
              {#each Array(4) as _, i (i)}
                <div class="tload__line" class:tload__line--short={i % 2 === 1}></div>
              {/each}
            </div>
            <div class="tload__label">Loading conversation…</div>
          </div>
        {:else if isEmpty}
          <!-- Warm starter state: a one-line prompt + clickable example chips
               that send straight into the session. Replaced by the transcript
               the moment the first message lands. -->
          <div class="starter">
            <div class="starter__title">Ready when you are.</div>
            <div class="starter__line">Ask anything, or start from one of these:</div>
            <div class="starter__chips">
              {#each starters as s (s)}
                <button class="chip" disabled={!online} title={online ? "Send this" : "daemon offline"} onclick={() => send(s)}>
                  {s}
                </button>
              {/each}
            </div>
          </div>
        {:else}
          {#if store?.truncated}
            <div class="chat__earlier">Showing the most recent messages.</div>
          {/if}
          <!-- role=log + polite live region: VirtualList only mounts visible
               rows, so without this a SR user gets no signal that the agent
               replied or a turn finished. aria-relevant additions+text covers
               both new rows and the in-place text growth of the live block. -->
          <div
            class="chat__log"
            role="log"
            aria-label="Conversation"
            aria-live="polite"
            aria-relevant="additions text"
          >
            <!-- Only COMPLETED history feeds the windowed list. The in-flight
                 `live` block is rendered as a fixed trailing row below (see the
                 live region after this list), so a live token append no longer
                 rebuilds a [...history, live] array each frame and re-derives
                 every offset (~60×/sec) — GUI-069. Keys stay the stable per-
                 block uid; history rows are all committed, so each renders its
                 final form (note tone / reasoning / Markdown prose). -->
            <VirtualList items={history} estimateHeight={120} pin key={(b) => b.uid}>
              {#snippet row(block)}
                <div class="chat__row" role="article" aria-label={rowLabel(block.kind)}>
                  {#if block.kind === "tool"}
                    <ToolCallCard {block} />
                  {:else if block.kind === "note"}
                    <!-- Note tone (GUI-093): a terminal failure/interrupt note
                         (text prefixed "error:" / "interrupted") gets a warn/
                         error treatment so a provider failure is not visually
                         indistinguishable from a benign "task → background"
                         note. Tone is read from the text, not a separate kind. -->
                    <div class="msg msg--note" class:msg--note-error={noteTone(block.text) === "error"}>{block.text}</div>
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
                </div>
              {/snippet}
            </VirtualList>
          </div>
          {#if live}
            <!-- The in-flight assistant block, OUTSIDE the windowed list — it
                 streams as plain text for speed and finalizes to Markdown once
                 committed to history (where it joins the list above). Rendering
                 it here, not inside VirtualList, is what keeps a per-frame token
                 append from re-deriving the whole list's geometry (GUI-069). A
                 subtle caret trails the live text while it streams. -->
            <div class="chat__live">
              <div class="chat__row" role="article" aria-label={rowLabel(live.kind)}>
                {#if live.kind === "reasoning"}
                  <div class="msg msg--reasoning msg--live">
                    <span class="msg__tag">reasoning</span>
                    {live.text}<span class="caret" aria-hidden="true"></span>
                  </div>
                {:else}
                  <div class="msg msg--text msg--live">{live.text}<span class="caret" aria-hidden="true"></span></div>
                {/if}
              </div>
            </div>
          {/if}
          <!-- Off-screen completion announcer. The windowed transcript above
               may not have the just-finalized assistant block mounted, so mirror
               its text here once the turn settles — that gives the SR user the
               "the agent replied / turn finished" cue the visual caret gives a
               sighted user. Empty while streaming, so it fires once per turn. -->
          <div class="sr-only" role="status" aria-live="polite" aria-atomic="true">
            {#if lastAssistant}Assistant replied: {lastAssistant}{/if}
          </div>
        {/if}
      </div>

      {#if store?.running}
        <div class="chat__working">
          <StatusDot state="working" size={7} pulse />
          <!-- While an Interrupt RPC is in flight the indicator reads "stopping…"
               so the user sees the stop landed and the turn is winding down
               (GUI-068) — the Interrupt is also re-entry guarded in interrupt()
               so mashing Stop can't fire multiple RPCs. -->
          <span class="chat__working-label">{interrupting ? "stopping…" : "working…"}</span>
          <!-- Detach-bash: free a turn stuck on a long shell by backgrounding it
               (it reappears in the shells dock) — distinct from interrupt, which
               kills the whole turn. Disabled while stopping — the turn is already
               being killed. -->
          <button
            class="chat__detach"
            onclick={detachBash}
            disabled={detaching || interrupting || !online}
            title={online ? "Background the running shell to free this turn (without killing it)" : "daemon offline"}
          >detach shell</button>
          <!-- Steer/queue clarity: while a turn runs, typed input is STEERED into
               it (injected mid-turn); if the daemon can't steer it queues as the
               next turn. Surfacing the mode removes the "where did my message go"
               surprise. -->
          <span class="chat__steerhint" title="Type now to steer the running turn; if it can't be steered it queues as the next turn">↳ steers the running turn</span>
        </div>
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
          {#if nearLimit}
            <button
              class="dock__nudge"
              class:dock__nudge--busy={compacting}
              onclick={compact}
              disabled={compacting || (store?.running ?? false)}
              title={store?.running ? "finish the current turn first" : "Compact the conversation to free context"}
            >
              <!-- In-progress affordance (GUI-055): Compact takes seconds, so a
                   greyed label with no change reads as a dead button. Swap to a
                   spinner + "compacting…" while it runs. -->
              {#if compacting}
                <span class="dock__spinner" aria-hidden="true"></span>compacting…
              {:else}
                near context limit — compact?
              {/if}
            </button>
          {/if}
        </div>
      {/if}

      <!-- STATUS + SETTINGS: the read-only pills double as the trigger for an
           anchored settings panel where each capability is editable. -->
      <div class="dock__group">
        <div class="dock__head">
          <span class="dock__label">status</span>
          <Popover label="Session settings" align="end" width={272} bind:open={settingsOpen}>
            {#snippet trigger(toggle)}
              <Button variant="ghost" size="sm" onclick={toggle} title="Session settings">edit</Button>
            {/snippet}
            <div class="set">
              <div class="set__row">
                <label class="set__label" for="set-model">model</label>
                <select id="set-model" class="set__ctl" value={sess?.model ?? ""} onchange={onModel}>
                  {#if models.length === 0}
                    <option value={sess?.model ?? ""}>{sess?.model || "—"}</option>
                  {:else}
                    {#each models as m (m.id)}
                      <option value={m.id} disabled={!m.available}>{m.id}{m.available ? "" : " (unavailable)"}</option>
                    {/each}
                  {/if}
                </select>
              </div>

              <div class="set__row">
                <span class="set__label">permissions</span>
                <button
                  class="set__toggle"
                  role="switch"
                  aria-checked={sess?.perm === "auto"}
                  onclick={onPerm}
                  title="Toggle auto-approve vs gated approvals"
                >
                  <span class="set__toggle-track"><span class="set__toggle-knob"></span></span>
                  <span class="set__toggle-text">{sess?.perm === "auto" ? "auto" : "gated"}</span>
                </button>
              </div>

              {#if sess?.effort}
                <div class="set__row">
                  <label class="set__label" for="set-effort">effort</label>
                  <select id="set-effort" class="set__ctl" value={sess.effort} onchange={onEffort}>
                    {#each effortLevels as lv (lv)}
                      <option value={lv}>{lv}</option>
                    {/each}
                  </select>
                </div>
              {/if}

              {#if sess?.search}
                <div class="set__row">
                  <label class="set__label" for="set-search">search</label>
                  <select id="set-search" class="set__ctl" value={sess.search} onchange={onSearch}>
                    {#each SEARCH_MODES as mode (mode)}
                      <option value={mode}>{mode}</option>
                    {/each}
                  </select>
                </div>
              {/if}

              {#if sess?.fastOk}
                <div class="set__row">
                  <span class="set__label">fast tier</span>
                  <button
                    class="set__toggle"
                    role="switch"
                    aria-checked={sess.fast ?? false}
                    onclick={onFast}
                    title="Route eligible turns to the fast tier"
                  >
                    <span class="set__toggle-track"><span class="set__toggle-knob"></span></span>
                    <span class="set__toggle-text">{sess.fast ? "on" : "off"}</span>
                  </button>
                </div>
              {/if}
            </div>
          </Popover>
        </div>
        <div class="dock__pills">
          <Badge tone={sess?.perm === "auto" ? "warn" : "neutral"}>{sess?.perm || "gated"}</Badge>
          {#if sess?.effort}<Badge tone="info">effort: {sess.effort}</Badge>{/if}
          {#if sess?.search}<Badge tone="info">search: {sess.search}</Badge>{/if}
          {#if sess?.fastOk}<Badge tone={sess.fast ? "brand" : "neutral"}>fast</Badge>{/if}
        </div>
      </div>

      <!-- TITLE: inline rename; empty reverts to the derived session title. -->
      <div class="dock__group">
        <div class="dock__head">
          <span class="dock__label">title</span>
          {#if !editingTitle}
            <Button variant="ghost" size="sm" onclick={startTitle} title="Rename session">rename</Button>
          {/if}
        </div>
        {#if editingTitle}
          <!-- svelte-ignore a11y_autofocus -->
          <input
            class="dock__input"
            bind:value={titleDraft}
            placeholder={derivedTitle}
            autofocus
            onblur={commitTitle}
            onkeydown={(e) => {
              if (e.key === "Enter") { e.preventDefault(); commitTitle(); }
              else if (e.key === "Escape") { editingTitle = false; }
            }}
          />
        {:else}
          <div class="dock__value dock__value--soft">{sess?.title || derivedTitle}</div>
        {/if}
      </div>

      <!-- GOAL: editable; empty clears it. -->
      <div class="dock__group">
        <div class="dock__head">
          <span class="dock__label">goal</span>
          {#if !editingGoal}
            <Button variant="ghost" size="sm" onclick={startGoal} title={sess?.goal ? "Edit goal" : "Set a goal"}>
              {sess?.goal ? "edit" : "set"}
            </Button>
          {/if}
        </div>
        {#if editingGoal}
          <!-- svelte-ignore a11y_autofocus -->
          <textarea
            class="dock__input dock__input--area"
            bind:value={goalDraft}
            rows="3"
            placeholder="What should this session accomplish? (empty clears)"
            autofocus
            onblur={commitGoal}
            onkeydown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); commitGoal(); }
              else if (e.key === "Escape") { editingGoal = false; }
            }}
          ></textarea>
        {:else if sess?.goal}
          <div class="dock__goal">{sess.goal}</div>
        {:else}
          <div class="dock__empty">No goal set.</div>
        {/if}
      </div>

      <!-- WORKING DIRS: the sandbox roots, primary first, with an add control. -->
      {#if sess}
        <div class="dock__group">
          <div class="dock__label">working dirs</div>
          {#if sess.roots && sess.roots.length > 0}
            <ul class="roots">
              {#each sess.roots as root, i (root)}
                <li class="roots__item" class:roots__item--primary={i === 0}>
                  <span class="roots__path" title={root}>{root}</span>
                  {#if i === 0}<span class="roots__tag">primary</span>{/if}
                </li>
              {/each}
            </ul>
          {:else}
            <div class="dock__empty">No directories.</div>
          {/if}
          <div class="dock__addrow">
            <input
              class="dock__input"
              bind:value={dirDraft}
              placeholder="Add directory…"
              onkeydown={(e) => e.key === "Enter" && addDir()}
            />
            <Button
              variant="secondary"
              size="sm"
              loading={addingDir}
              disabled={dirDraft.trim().length === 0}
              title={dirDraft.trim() ? "Add this directory to the sandbox" : "Enter a path first"}
              onclick={addDir}
            >Add</Button>
          </div>
        </div>
      {/if}

      <!-- SHELLS: live background shells, with a per-shell kill control. -->
      {#if sess?.shells && sess.shells.length > 0}
        <div class="dock__group">
          <div class="dock__label">shells</div>
          <ul class="shells">
            {#each sess.shells as sh (sh.id)}
              <li class="shell">
                <div class="shell__top">
                  <code class="shell__cmd" title={sh.command}>{sh.command}</code>
                  <Button variant="danger" size="sm" onclick={() => killShell(sh.id)} title="Kill this shell">Kill</Button>
                </div>
                <div class="shell__meta">
                  <Badge tone={sh.status === "running" ? "info" : sh.exit_code === 0 ? "success" : "error"}>
                    {sh.status}{sh.status !== "running" ? ` · exit ${sh.exit_code}` : ""}
                  </Badge>
                </div>
                {#if sh.last_line}<div class="shell__line" title={sh.last_line}>{sh.last_line}</div>{/if}
              </li>
            {/each}
          </ul>
        </div>
      {/if}

      <!-- SESSION MENU: maintenance actions, anchored. -->
      <div class="dock__group">
        <div class="dock__head">
          <span class="dock__label">session</span>
          <Popover label="Session actions" align="end" bind:open={menuOpen}>
            {#snippet trigger(toggle)}
              <Button variant="icon" size="sm" onclick={toggle} title="Session actions">⋯</Button>
            {/snippet}
            {@const busy = store?.running ?? false}
            <div class="menu">
              <button
                class="menu__item"
                onclick={compact}
                disabled={compacting || busy}
                title={busy ? "finish the current turn first" : undefined}
              >
                <!-- In-progress affordance (GUI-055): Compact takes seconds, so
                     swap the glyph for a spinner and the labels for "compacting…"
                     while it runs rather than leaving a static greyed item. -->
                {#if compacting}
                  <span class="dock__spinner" aria-hidden="true"></span>
                  <span class="menu__label">Compacting…</span>
                {:else}
                  <span class="menu__glyph" aria-hidden="true">⊟</span>
                  <span class="menu__label">Compact context</span>
                  <span class="menu__hint">free tokens</span>
                {/if}
              </button>
              <button
                class="menu__item"
                onclick={resend}
                disabled={busy}
                title={busy ? "finish the current turn first" : undefined}
              >
                <span class="menu__glyph" aria-hidden="true">↻</span>
                <span class="menu__label">Resend last turn</span>
                <span class="menu__hint">retry</span>
              </button>
              {#if confirmClear}
                <div class="menu__confirm">
                  <span class="menu__confirm-q">Clear all messages?</span>
                  <div class="menu__confirm-actions">
                    <Button variant="danger" size="sm" onclick={clearSession} title="Clear the conversation">Clear</Button>
                    <Button variant="ghost" size="sm" onclick={() => (confirmClear = false)}>Cancel</Button>
                  </div>
                </div>
              {:else}
                <button
                  class="menu__item menu__item--danger"
                  onclick={() => (confirmClear = true)}
                  disabled={busy}
                  title={busy ? "finish the current turn first" : undefined}
                >
                  <span class="menu__glyph" aria-hidden="true">⌫</span>
                  <span class="menu__label">Clear conversation</span>
                  <span class="menu__hint">destructive</span>
                </button>
              {/if}
            </div>
          </Popover>
        </div>
      </div>

      {#if sess?.pending && sess.pending.length > 0}
        <div class="dock__group dock__group--approve">
          <div class="dock__label">awaiting approval</div>
          {#each sess.pending as ap (ap.id)}
            {@const args = prettyArgs(ap.args)}
            {@const open = argsOpen[ap.id] ?? false}
            <div class="approve">
              <div class="approve__tool">{ap.tool}</div>
              {#if args}
                <!-- What you are allowing: the tool's args. Collapsed to a
                     truncated preview; expand for the full scrollable block. -->
                <div class="approve__args" class:approve__args--open={open}>
                  <pre class="approve__args-pre selectable">{args}</pre>
                  <button
                    class="approve__args-toggle"
                    onclick={() => toggleArgs(ap.id)}
                    aria-expanded={open}
                  >{open ? "show less" : "show full args"}</button>
                </div>
              {/if}
              <div class="approve__actions">
                <Button variant="primary" size="sm" onclick={() => approve(ap.id, true)}>Allow</Button>
                <Button variant="danger" size="sm" onclick={() => approve(ap.id, false)}>Deny</Button>
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

  /* CONTROL BAR — slim always-on row above the transcript. */
  .ctl {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-6);
    border-bottom: 1px solid var(--border-hairline);
    overflow-x: auto;
    scrollbar-width: none;
  }
  .ctl::-webkit-scrollbar {
    display: none;
  }
  .ctl__new {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    flex: none;
    height: 26px;
    padding: 0 var(--sp-4);
    border: 1px solid var(--border-brand-faint);
    background: var(--state-selected);
    color: var(--brand-bright);
    border-radius: var(--r-sm);
    font: var(--fw-semibold) var(--fs-body-sm) / 1 var(--font-sans);
    cursor: pointer;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .ctl__new:hover {
    background: var(--brand-dim);
    color: var(--text-on-brand);
  }
  .ctl__new:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .ctl__new:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .ctl__plus {
    font-size: var(--fs-body);
    line-height: 1;
  }
  .ctl__sep {
    flex: none;
    width: 1px;
    align-self: stretch;
    margin: var(--sp-1) var(--sp-1);
    background: var(--divider);
  }
  .ctl__field {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    flex: none;
  }
  .ctl__k {
    font: var(--fw-medium) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  .ctl__sel {
    height: 26px;
    max-width: 200px;
    padding: 0 var(--sp-6) 0 var(--sp-3);
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised);
    color: var(--text-primary);
    border-radius: var(--r-sm);
    font: var(--fw-regular) var(--fs-body-sm) / 1 var(--font-sans);
    cursor: pointer;
    /* webkit2gtk paints native selects white without this — own chevron */
    -webkit-appearance: none;
    appearance: none;
    color-scheme: dark;
    background-image: linear-gradient(45deg, transparent 50%, var(--text-muted) 50%),
      linear-gradient(135deg, var(--text-muted) 50%, transparent 50%);
    background-position:
      calc(100% - 13px) center,
      calc(100% - 8px) center;
    background-size:
      5px 5px,
      5px 5px;
    background-repeat: no-repeat;
  }
  .ctl__sel--mini {
    max-width: 110px;
  }
  .ctl__sel:hover {
    border-color: var(--border-strong);
  }
  .ctl__sel:focus-visible {
    outline: none;
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .ctl__sel option {
    background: var(--bg-overlay);
    color: var(--text-primary);
  }
  .ctl__pill {
    flex: none;
    height: 26px;
    padding: 0 var(--sp-3);
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised);
    color: var(--text-secondary);
    border-radius: var(--r-sm);
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-sans);
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .ctl__pill:hover {
    border-color: var(--border-strong);
    color: var(--text-primary);
  }
  .ctl__pill:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  /* auto-perm is a posture worth seeing (tools run unprompted) → warm; fast on → brand. */
  .ctl__pill--warn {
    border-color: color-mix(in srgb, var(--warn) 40%, transparent);
    color: var(--warn);
  }
  .ctl__pill--on {
    border-color: var(--border-brand-faint);
    background: var(--state-selected);
    color: var(--brand-bright);
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
  /* The role=log wrapper takes the flex space VirtualList used to fill directly
     and hands it down (VirtualList sizes to height:100%), so windowing geometry
     is unchanged — this layer only adds the live-region semantics. */
  .chat__log {
    flex: 1;
    min-height: 0;
  }
  /* The in-flight live block, rendered OUTSIDE VirtualList (GUI-069) so a token
     append touches one node instead of re-deriving the whole list's geometry.
     It sits just below the windowed history, where the streaming reply belongs.
     flex:none keeps it from stealing the list's space; a bounded height with
     its own scroll keeps a long live reasoning stream from shoving the composer
     off-screen — it commits into the windowed list above the moment the turn
     ends. */
  .chat__live {
    flex: none;
    max-height: 42vh;
    overflow-y: auto;
  }
  /* Off-screen completion announcer — present in the a11y tree, invisible on
     screen. Standard clip pattern (not display:none, which drops it from the
     tree); mirrors .diff__sr. */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    margin: -1px;
    padding: 0;
    overflow: hidden;
    clip: rect(0 0 0 0);
    clip-path: inset(50%);
    white-space: nowrap;
    border: 0;
  }

  /* ── empty-session starter ─────────────────────────────────────────────── */
  .starter {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-4);
    max-width: 820px;
    width: 100%;
    margin: 0 auto;
    padding: var(--sp-9) var(--sp-8);
    text-align: center;
  }
  .starter__title {
    font: var(--fw-semibold) var(--fs-h2) / var(--lh-tight) var(--font-display);
    letter-spacing: var(--ls-heading);
    color: var(--text-primary);
  }
  .starter__line {
    font-size: var(--fs-body-sm);
    color: var(--text-muted);
  }
  .starter__chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-3);
    justify-content: center;
    margin-top: var(--sp-3);
  }
  .chip {
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised);
    color: var(--text-secondary);
    border-radius: var(--r-full);
    padding: var(--sp-3) var(--sp-5);
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-sans);
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .chip:hover:not(:disabled) {
    background: var(--bg-raised-2);
    border-color: var(--border-strong);
    color: var(--text-primary);
  }
  .chip:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .chip:disabled {
    color: var(--text-ghost);
    cursor: not-allowed;
  }

  /* ── transcript skeleton ───────────────────────────────────────────────── */
  .tload {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-6);
    max-width: 820px;
    width: 100%;
    margin: 0 auto;
    padding: var(--sp-9) var(--sp-8);
  }
  .tload__rows {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .tload__line {
    height: 16px;
    border-radius: var(--r-sm);
    background: linear-gradient(90deg, var(--bg-raised) 0%, var(--bg-raised-2) 50%, var(--bg-raised) 100%);
    background-size: 200% 100%;
    animation: tload-shimmer 1.4s ease-in-out infinite;
  }
  .tload__line--short {
    width: 62%;
  }
  .tload__label {
    font-size: var(--fs-body-sm);
    color: var(--text-muted);
  }
  @keyframes tload-shimmer {
    to {
      background-position: -200% 0;
    }
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
  /* A terminal failure/interrupt note (GUI-093). Same shape as a benign note —
     it's still a note, not an alarm banner — but tinted with the error palette
     and a heavier left rule so a provider failure reads as a failure rather than
     vanishing into the neutral note styling of "task → background". */
  .msg--note-error {
    color: var(--error);
    background: var(--error-bg);
    border-color: var(--error);
    border-left-width: 3px;
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
  /* A slim blinking caret trailing the live stream — a quiet sign of presence.
     Inline so it sits right after the last glyph; stilled under reduced-motion. */
  .caret {
    display: inline-block;
    width: 2px;
    height: 1.05em;
    margin-left: 1px;
    vertical-align: text-bottom;
    background: var(--brand);
    border-radius: 1px;
    animation: caret-blink 1.1s steps(1, end) infinite;
  }
  @keyframes caret-blink {
    0%,
    50% {
      opacity: 1;
    }
    50.01%,
    100% {
      opacity: 0;
    }
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
  /* The label gently breathes in sympathy with the dot — opacity only, so no
     reflow; stilled under reduced-motion. */
  .chat__working-label {
    animation: work-breathe var(--breath) var(--ease-inout) infinite;
  }
  @keyframes work-breathe {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.55;
    }
  }
  /* Steer hint: a quiet note that typing now steers the running turn. */
  .chat__steerhint {
    font: var(--fw-regular) var(--fs-label) / 1 var(--font-sans);
    color: var(--text-faint);
  }
  /* Detach-bash control: a quiet inline link beside the working indicator, not
     an alarm. Reads as secondary text until hovered, then warms. */
  .chat__detach {
    margin-left: auto;
    border: none;
    background: transparent;
    padding: 0;
    color: var(--text-muted);
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
    cursor: pointer;
    border-radius: var(--r-xs);
    transition: color var(--dur-fast) var(--ease-out);
  }
  .chat__detach:hover:not(:disabled) {
    color: var(--text-primary);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .chat__detach:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .chat__detach:disabled {
    color: var(--text-ghost);
    cursor: not-allowed;
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
  .dock__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-4);
    margin-bottom: var(--sp-3);
  }
  .dock__head .dock__label {
    margin-bottom: 0;
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
  .dock__value--soft {
    font-weight: var(--fw-medium);
    color: var(--text-secondary);
  }
  .dock__sub {
    font-size: var(--fs-label);
    color: var(--text-muted);
    margin-top: var(--sp-2);
  }
  .dock__empty {
    font-size: var(--fs-label);
    color: var(--text-ghost);
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
  /* The context nudge: a quiet warm prompt, not an alarm. Reads as a link. */
  .dock__nudge {
    margin-top: var(--sp-3);
    border: none;
    background: transparent;
    padding: 0;
    text-align: left;
    color: var(--working);
    font: var(--fw-medium) var(--fs-label) / var(--lh-snug) var(--font-sans);
    cursor: pointer;
    border-radius: var(--r-xs);
  }
  .dock__nudge:hover:not(:disabled) {
    color: var(--brand-bright);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .dock__nudge:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .dock__nudge:disabled {
    color: var(--text-ghost);
    cursor: not-allowed;
  }
  /* While compacting the nudge is disabled but must read as IN-PROGRESS, not
     dead — hold the working tint (override the ghosted disabled color) and pair
     it with the inline spinner. */
  .dock__nudge--busy:disabled {
    color: var(--working);
    display: inline-flex;
    align-items: center;
    gap: var(--sp-3);
  }
  /* Inline progress spinner shared by the compact nudge + menu item (GUI-055).
     Same ring construction as Button's .btn__spinner, sized for inline text and
     stilled under reduced-motion. */
  .dock__spinner {
    flex: none;
    width: 12px;
    height: 12px;
    border-radius: var(--r-full);
    border: 2px solid var(--working-bg);
    border-top-color: var(--working);
    animation: dock-spin 0.7s linear infinite;
  }
  @keyframes dock-spin {
    to {
      transform: rotate(360deg);
    }
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

  /* Inline edit fields in the dock — proportional sans, never mono. */
  .dock__input {
    width: 100%;
    box-sizing: border-box;
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised);
    color: var(--text-primary);
    border-radius: var(--r-sm);
    padding: var(--sp-3) var(--sp-4);
    font: var(--fw-regular) var(--fs-body-sm) / var(--lh-snug) var(--font-sans);
    outline: none;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out);
  }
  .dock__input:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .dock__input::placeholder {
    color: var(--text-ghost);
  }
  .dock__input--area {
    resize: vertical;
    min-height: 56px;
  }
  .dock__addrow {
    display: flex;
    gap: var(--sp-3);
    margin-top: var(--sp-3);
    align-items: stretch;
  }
  .dock__addrow .dock__input {
    flex: 1;
    min-width: 0;
  }

  /* ── roots ─────────────────────────────────────────────────────────────── */
  .roots {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .roots__item {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    font-size: var(--fs-label);
    color: var(--text-muted);
  }
  .roots__item--primary {
    color: var(--text-secondary);
  }
  .roots__path {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl;
    text-align: left;
  }
  .roots__tag {
    flex: none;
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--brand);
  }

  /* ── shells ────────────────────────────────────────────────────────────── */
  .shells {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .shell {
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-sm);
    background: var(--bg-raised);
    padding: var(--sp-4);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .shell__top {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
  }
  .shell__cmd {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--fw-regular) var(--fs-code-sm) / 1.2 var(--font-mono);
    color: var(--text-primary);
  }
  .shell__meta {
    display: flex;
  }
  .shell__line {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--fw-regular) var(--fs-code-sm) / var(--lh-snug) var(--font-mono);
    color: var(--text-muted);
  }

  /* ── settings panel ────────────────────────────────────────────────────── */
  .set {
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
  }
  .set__row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-5);
  }
  .set__label {
    font-size: var(--fs-label);
    color: var(--text-secondary);
    flex: none;
  }
  .set__ctl {
    flex: 1;
    min-width: 0;
    max-width: 170px;
    box-sizing: border-box;
    border: 1px solid var(--border-subtle);
    background: var(--bg-raised);
    color: var(--text-primary);
    border-radius: var(--r-sm);
    /* room for the custom chevron on the right */
    padding: var(--sp-2) var(--sp-7) var(--sp-2) var(--sp-3);
    font: var(--fw-regular) var(--fs-body-sm) / 1 var(--font-sans);
    outline: none;
    cursor: pointer;
    /* webkit2gtk paints native selects white without this — reset + own chevron */
    -webkit-appearance: none;
    appearance: none;
    color-scheme: dark;
    background-image: linear-gradient(45deg, transparent 50%, var(--text-muted) 50%),
      linear-gradient(135deg, var(--text-muted) 50%, transparent 50%);
    background-position:
      calc(100% - 14px) center,
      calc(100% - 9px) center;
    background-size:
      5px 5px,
      5px 5px;
    background-repeat: no-repeat;
  }
  .set__ctl:hover {
    border-color: var(--border-strong);
  }
  .set__ctl:focus-visible {
    border-color: var(--border-brand-faint);
    box-shadow: var(--shadow-focus);
  }
  .set__ctl option {
    background: var(--bg-overlay);
    color: var(--text-primary);
  }
  /* A compact switch styled from tokens; the track tints brand when on. */
  .set__toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-3);
    border: none;
    background: transparent;
    padding: 0;
    cursor: pointer;
    color: var(--text-secondary);
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
  }
  .set__toggle:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
    border-radius: var(--r-xs);
  }
  .set__toggle-track {
    position: relative;
    width: 30px;
    height: 16px;
    border-radius: var(--r-full);
    background: var(--bg-inset);
    border: 1px solid var(--border-subtle);
    transition: background var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out);
  }
  .set__toggle-knob {
    position: absolute;
    top: 1px;
    left: 1px;
    width: 12px;
    height: 12px;
    border-radius: var(--r-full);
    background: var(--text-muted);
    transition: transform var(--dur-fast) var(--ease-out), background var(--dur-fast) var(--ease-out);
  }
  .set__toggle[aria-checked="true"] .set__toggle-track {
    background: var(--state-selected);
    border-color: var(--border-brand);
  }
  .set__toggle[aria-checked="true"] .set__toggle-knob {
    transform: translateX(14px);
    background: var(--brand);
  }
  .set__toggle-text {
    min-width: 3ch;
    text-align: left;
  }
  @media (prefers-reduced-motion: reduce) {
    .set__toggle-track,
    .set__toggle-knob {
      transition: none;
    }
    /* hold the live caret solid rather than blinking */
    .caret {
      animation: none;
      opacity: 0.8;
    }
    .tload__line {
      animation: none;
    }
    /* still the compact spinner — the "compacting…" label still conveys the
       in-progress state without motion. */
    .dock__spinner {
      animation: none;
    }
  }

  /* ── session menu ──────────────────────────────────────────────────────── */
  .menu {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    min-width: 200px;
  }
  .menu__item {
    display: flex;
    align-items: center;
    gap: var(--sp-4);
    width: 100%;
    padding: var(--sp-3) var(--sp-4);
    border: none;
    background: transparent;
    border-radius: var(--r-sm);
    cursor: pointer;
    text-align: left;
    color: var(--text-secondary);
    font: var(--fw-medium) var(--fs-body-sm) / 1 var(--font-sans);
  }
  .menu__item:hover:not(:disabled) {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .menu__item:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .menu__item:disabled {
    color: var(--text-ghost);
    cursor: not-allowed;
  }
  .menu__item--danger {
    color: var(--error);
  }
  .menu__item--danger:hover:not(:disabled) {
    background: var(--error-bg);
    color: var(--error);
  }
  .menu__glyph {
    width: 16px;
    text-align: center;
    color: var(--text-ghost);
  }
  .menu__item--danger .menu__glyph {
    color: var(--error);
  }
  .menu__label {
    flex: 1;
  }
  .menu__hint {
    font-size: var(--fs-micro);
    color: var(--text-faint);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }
  .menu__confirm {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-4);
    border-radius: var(--r-sm);
    background: var(--error-bg);
    border: 1px solid var(--error);
  }
  .menu__confirm-q {
    font-size: var(--fs-body-sm);
    color: var(--text-primary);
  }
  .menu__confirm-actions {
    display: flex;
    gap: var(--sp-3);
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
  /* WHAT you are allowing: the tool args. A quiet inset code well, collapsed to
     a short preview with a fade so a large blob doesn't shove the Allow/Deny
     buttons off-screen; the toggle expands it into a scrollable block. Mono is
     permitted here — it's code, like CodeBlock/DiffView. */
  .approve__args {
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-sm);
    background: var(--bg-inset);
    overflow: hidden;
  }
  .approve__args-pre {
    margin: 0;
    padding: var(--sp-3) var(--sp-4);
    max-height: 4.2em;
    overflow: hidden;
    font: var(--fw-regular) var(--fs-code-sm) / var(--lh-code) var(--font-mono);
    color: var(--text-secondary);
    tab-size: var(--tab-size);
    white-space: pre-wrap;
    word-break: break-word;
    /* Fade the clipped tail so the truncation reads as intentional. */
    mask-image: linear-gradient(180deg, #000 60%, transparent 100%);
    -webkit-mask-image: linear-gradient(180deg, #000 60%, transparent 100%);
  }
  .approve__args--open .approve__args-pre {
    max-height: 220px;
    overflow: auto;
    mask-image: none;
    -webkit-mask-image: none;
  }
  .approve__args-toggle {
    display: block;
    width: 100%;
    border: none;
    border-top: 1px solid var(--border-hairline);
    background: transparent;
    padding: var(--sp-2) var(--sp-4);
    text-align: left;
    cursor: pointer;
    color: var(--text-muted);
    font: var(--fw-medium) var(--fs-micro) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    transition: color var(--dur-fast) var(--ease-out);
  }
  .approve__args-toggle:hover {
    color: var(--text-primary);
  }
  .approve__args-toggle:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .approve__actions {
    display: flex;
    gap: var(--sp-3);
  }
</style>
