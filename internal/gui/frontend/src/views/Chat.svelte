<script lang="ts">
  // Chat — the live agent conversation. The keystone of the no-leak contract:
  // Subscribe + transcript construction + listener start + State seed all live
  // inside ONE $effect, whose cleanup disposes the transcript (removing its
  // listener) and Unsubscribes (closing the pump → daemon releases the view).
  // Construction and teardown are symmetric; nothing is created at <script> top.
  import { Bridge } from "$lib/bridge";
  import { daemon } from "$lib/stores/daemon.svelte";
  import { sessions } from "$lib/stores/sessions.svelte";
  import { sessionUnread } from "$lib/stores/sessionUnread.svelte";
  import { toasts } from "$lib/stores/toasts.svelte";
  import { ui } from "$lib/stores/ui.svelte";
  import { voice } from "$lib/stores/voice.svelte";
  import { router } from "$lib/router.svelte";
  import { on, ev } from "$lib/events";
  import { errText } from "$lib/errors";
  import { builtinSlashCommands, parseSlashCommand, slashHelpText, type SlashCommandEntry } from "$lib/slash";
  import { createTranscript, type Transcript, type Row, groupRows } from "$lib/stores/transcript.svelte";
  import type { SessionStateDTO, ModelDTO, ImageDTO, RecentDirDTO, CommandInfoDTO } from "$lib/types";
  import Composer from "$lib/components/Composer.svelte";
  import ToolGroupCard from "$lib/components/ToolGroupCard.svelte";
  import Markdown from "$lib/components/Markdown.svelte";
  import VirtualList from "$lib/components/VirtualList.svelte";
  import Badge from "$lib/components/Badge.svelte";
  import Button from "$lib/components/Button.svelte";
  import EmptyState from "$lib/components/EmptyState.svelte";
  import StatusDot from "$lib/components/StatusDot.svelte";
  import Popover from "$lib/components/Popover.svelte";
  import Dropdown from "$lib/components/Dropdown.svelte";
  // Terminal is lazy-loaded: it statically pulls in xterm (~342KB JS + CSS),
  // which would otherwise be eager in the startup bundle even though the
  // terminal dock tab defaults closed and is often never opened. The import()
  // fires only when terminalOpened flips true (first time the tab is shown),
  // so xterm leaves the cold-start critical path entirely.
  const TerminalLazy = () => import("$lib/components/Terminal.svelte");
  import DiffPanel from "$lib/components/DiffPanel.svelte";
  import FilesPanel from "$lib/components/FilesPanel.svelte";
  import BrowserPanel from "$lib/components/BrowserPanel.svelte";

  let { param }: { param?: string } = $props();

  // Resolve the session to show: route param, else the newest session.
  const sessionId = $derived(param ?? sessions.list[0]?.id ?? "");

  // A REMOTE session ref (remote:<b64 target>:<realID>, opened from the Machines
  // board) lives on another host's daemon over ssh. It's NOT in the local
  // sessions.list, so the local-only "missing" guard below must not fire for it,
  // and the title/dock tools (which target the GUI's own machine) adapt.
  const isRemote = $derived(sessionId.startsWith("remote:"));

  // A routed LOCAL session that no longer exists (removed/pruned while open
  // here): param pins a concrete id, sessions have loaded, but it's not in the
  // list. Without this guard Chat would render a live composer over a dead
  // session. Remote refs are exempt — they're never in the local list; their
  // liveness surfaces through the State()/stream path instead.
  const missing = $derived(
    !isRemote && !!param && sessions.loaded && !sessions.list.some((s) => s.id === param),
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
    sessionUnread.markRead(id);
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
      Bridge.Subscribe(id).catch((e) => toasts.error("subscribe: " + errText(e)));
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
          toasts.error("state: " + errText(e));
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

  let transcriptListEl = $state<HTMLDivElement | undefined>(undefined);

  // The streaming reply is now a row INSIDE VirtualList (not a separate pane),
  // so VirtualList's own pin-to-bottom follows it as it grows — no separate
  // live-scroll handling needed.

  // Fold consecutive tool calls into "N tools" group rows (a burst of calls
  // renders as ONE collapsible item, each tool still individually expandable),
  // separated by any message / reasoning / note. groupRows is pure + O(n) and
  // derives from `history` only — which changes at block boundaries, never on
  // the ~60fps token path (that lands in `live`), so grouping never re-runs per
  // token. The VirtualList keys on the row uid (a group's uid is its first
  // tool's uid — stable as the run grows), so a growing run never remounts.
  // Sentinel uid for the synthetic live row. Real uids are ++uidSeq (always
  // ≥1), so -1 never collides — the live row keeps a stable VirtualList key
  // for the whole turn, then the committed block replaces it with its real uid.
  const LIVE_UID = -1;
  // History rows fold tool runs; the in-flight `live` block is appended as ONE
  // synthetic trailing row so the streaming reply flows in the SAME scroll as
  // the conversation (continuous — it pushes history up instead of floating in
  // a separate pane below it). It carries a STABLE key ("__live__"), so a token
  // append only re-measures that one row, never re-keys/​remounts the list
  // (the GUI-069 perf intent — avoid re-deriving every row's geometry per
  // token — holds: groupRows(history) does not re-run while only `live` grows).
  const historyRows = $derived(groupRows(history));

  // Throttled mirror of the live block's text. The live row renders Markdown,
  // and lexCached is keyed on the exact source string — so a growing live
  // message is a cache MISS every flush (~60fps), re-lexing the WHOLE message
  // (GFM + KaTeX) + re-walking its token tree every frame. That is O(message
  // length) work 60×/sec while streaming, competing with scroll on the main
  // thread. We re-lex at most ~every 110ms instead: visually still "live"
  // (~9 updates/sec) but ~6× less lexing. The committed block always carries
  // the full final text, so nothing is lost. text-only live rows (reasoning
  // marker) don't go through Markdown, so they're unaffected.
  const LIVE_RELEX_MS = 110;
  let liveTextThrottled = $state("");
  let liveRelexTimer = 0;
  $effect(() => {
    const t = live?.text ?? "";
    if (!live) {
      // turn ended — reset; the committed history block carries the full text.
      liveTextThrottled = "";
      if (liveRelexTimer) { clearTimeout(liveRelexTimer); liveRelexTimer = 0; }
      return;
    }
    if (liveTextThrottled === "") {
      liveTextThrottled = t; // first token shows immediately (leading edge)
    } else if (!liveRelexTimer) {
      // coalesce the next ~110ms of appends into one re-lex (trailing edge);
      // a timer already pending will read the latest live.text when it fires.
      liveRelexTimer = window.setTimeout(() => {
        liveRelexTimer = 0;
        liveTextThrottled = live?.text ?? "";
      }, LIVE_RELEX_MS);
    }
  });

  const rows = $derived.by<Row[]>(() => {
    if (!live) return historyRows;
    // Use the throttled text for the (Markdown-rendered) live row so per-frame
    // token appends don't re-lex the whole message; the row keeps live.uid via
    // the LIVE_UID sentinel and commits with full text when the turn ends.
    const liveRow = { ...live, text: liveTextThrottled || live.text, uid: LIVE_UID } as Row;
    return [...historyRows, liveRow];
  });
  // The model is streaming prose right now → the live tool's detail folds away.
  const streaming = $derived(!!live);

  // Per-tool / per-group OPEN overrides, keyed by uid so a user's expand/collapse
  // survives re-derive and CAP eviction (and a $state record stays reactive in
  // place under Svelte 5 proxies). Missing key = follow the auto-open rule (live
  // tool / running run open; history collapsed). A click writes an explicit
  // boolean that then wins over the rule. Pruned to live uids below so a long
  // session doesn't accumulate overrides for CAP-evicted blocks.
  let toolOpen = $state<Partial<Record<number, boolean>>>({});
  let groupOpen = $state<Partial<Record<number, boolean>>>({});
  // The child knows each row's EFFECTIVE open state (which may be auto-open), so
  // it passes the new desired value; the parent just records the explicit
  // override. Keyed by uid → survives re-derive / CAP eviction.
  function setToolOpen(uid: number, open: boolean) {
    toolOpen[uid] = open;
  }
  function setGroupOpen(uid: number, open: boolean) {
    groupOpen[uid] = open;
  }
  // Prune override maps to the uids still present (tool uids + group ids), so a
  // CAP-evicted block's stale override can't leak memory across a long session.
  // Cheap: runs only when `rows` changes (block boundaries), never per token.
  $effect(() => {
    const toolUids = new Set<number>();
    const groupUids = new Set<number>();
    for (const r of rows) {
      if (r.kind === "toolgroup") {
        groupUids.add(r.uid);
        for (const t of r.tools) toolUids.add(t.uid);
      }
    }
    for (const k of Object.keys(toolOpen)) {
      if (!toolUids.has(+k)) delete toolOpen[+k as keyof typeof toolOpen];
    }
    for (const k of Object.keys(groupOpen)) {
      if (!groupUids.has(+k)) delete groupOpen[+k as keyof typeof groupOpen];
    }
  });

  // Active task plan (the `todo` tool's list). Shown as a pinned panel while the
  // model is tracking a multi-step plan that isn't fully done. Hidden when empty
  // or every task is completed/cancelled (nothing left to surface).
  const todos = $derived(store?.todos ?? []);
  const todosActive = $derived(todos.some((t) => t.status === "pending" || t.status === "in_progress"));
  const todoDone = $derived(todos.filter((t) => t.status === "completed").length);
  // Plan card starts COLLAPSED — the collapsed head already shows "Plan N/M" +
  // the current in-progress step, which is the high-value bit. Expanded by
  // default, a 10-step plan claimed ~a quarter of the chat viewport above the
  // transcript (it's flex:none, pushing the conversation down by its full
  // height). The full checklist is reference; one click reveals it.
  let todoCollapsed = $state(true);
  function todoGlyph(status: string): string {
    if (status === "completed") return "✓";
    if (status === "in_progress") return "◐";
    if (status === "cancelled") return "✕";
    return "○";
  }

  const online = $derived(daemon.status === "online");

  // ── turn I/O ──────────────────────────────────────────────────────────────
  // Mid-turn input mode (mirrors the TUI's "steer" | "queue" setting): decides
  // what happens when the user sends WHILE A TURN IS RUNNING.
  //   • steer — inject into the running turn (try SteerInput, fall back to
  //     SendInput when there's nothing to steer). The default.
  //   • queue — do NOT inject mid-turn; hold the message client-side and submit
  //     it as a fresh turn once the current turn finishes.
  // This is a pure client-side concern — the daemon needs no change; queue is
  // just a hold buffer drained on turn-end. Persisted across reloads so the
  // posture sticks. localStorage is guarded (it exists in the webview, but the
  // guard keeps this honest if ever pre-rendered/tested headless).
  let inputMode = $state<"steer" | "queue">("steer");
  try {
    const saved = localStorage.getItem("eigen.inputMode");
    if (saved === "queue" || saved === "steer") inputMode = saved;
  } catch {}
  function setInputMode(m: "steer" | "queue") {
    inputMode = m;
    try {
      localStorage.setItem("eigen.inputMode", m);
    } catch {}
  }
  // Queued messages held in queue mode while a turn runs, drained one-per-turn-
  // end (each drained item starts a new turn that, when IT ends, drains the
  // next). Reassigned (never mutated in place) so the count badge stays reactive.
  let queued = $state<{ text: string; images: ImageDTO[] }[]>([]);

  // Steer routing: mid-turn input is injected into the running turn (steered)
  // rather than queued as a fresh turn. Images (composer attachments) ride along
  // on both paths; starter chips call send(text) with none, so the param
  // defaults to an empty list.
  //
  // The local `running` flag is a reactive view of stream events and can be
  // stale right at a turn boundary: between the 'done' event and an Enter, or
  // before the first delta of a server-restarted turn flips running=true. So we
  // don't branch on it. The daemon's input op is atomic: it starts a fresh turn
  // when idle and steers when a turn is running. `steered=false` means "accepted
  // as a fresh turn", not "nothing was sent".
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
    // Queue mode + a turn already running: hold the message rather than steer.
    // It drains as a fresh turn when the running turn finishes (drainQueue).
    // Returning true clears the composer draft just like a real send.
    if (inputMode === "queue" && (store?.running ?? false)) {
      queued = [...queued, { text, images }];
      toasts.info("queued — will send when the turn finishes");
      return true;
    }
    // The local running flag only colors the messaging below; it never decides
    // the RPC. Capture it before the await so the toast reflects the user's
    // intent at send time, not whatever the stream has done by the time the
    // round-trip lands.
    const expectedSteer = store?.running ?? false;
    // Echo the accepted human message before the RPC returns so a very fast
    // reply cannot race ahead of the user's text or attached screenshots. Roll
    // it back if the daemon rejects the input.
    const optimisticUid = store?.appendUserMessage(text, images);
    // Show the activity indicator immediately, before the model's first event,
    // so a slow streaming model never looks frozen. The first real turn event
    // (or done) clears it; clear on error below.
    store?.markPending();
    try {
      const steered = await Bridge.SteerInput(sessionId, text, images);
      if (steered) {
        toasts.info("steered into the running turn");
      } else if (expectedSteer) {
        toasts.info("sent as a new turn");
      }
      return true;
    } catch (e) {
      store?.removeBlock(optimisticUid);
      store?.clearPending(); // RPC failed — no turn is coming, drop the indicator
      toasts.error(errText(e));
      return false;
    }
  }

  // Drain ONE held message as a fresh turn when the running turn ends. The new
  // turn flips running=true again, and when IT ends this fires once more for the
  // next item — so the queue plays back in order without collapsing the holds
  // into a single turn. Fire-and-forget the toast on failure; a failed submit
  // drops that item rather than wedging the queue (the user still has the rest).
  function drainQueue() {
    if (queued.length === 0 || !sessionId) return;
    const next = queued[0];
    queued = queued.slice(1);
    const optimisticUid = store?.appendUserMessage(next.text, next.images);
    Bridge.SendInput(sessionId, next.text, next.images, []).catch((e) => {
      store?.removeBlock(optimisticUid);
      toasts.error(errText(e));
    });
  }

  // Slash commands — GUI peer of the TUI's internal/tui/commands.go. Built-ins
  // are handled first; user/plugin-authored custom commands fall through to the
  // existing Bridge.RunCommand path. Unknown /names are control input (not model
  // chat), so they show the same "try /help" note the TUI does.
  let customCommands = $state<CommandInfoDTO[]>([]);
  let customCommandsLoaded = $state(false);
  let customCommandsLoading: Promise<void> | null = null;
  const composerSlashCommands = $derived([
    ...builtinSlashCommands,
    ...customCommands.map((c) => ({
      name: c.name,
      usage: `/${c.name}${c.argHint ? ` ${c.argHint}` : ""}`,
      description: [c.description || "custom command", c.argHint].filter(Boolean).join("  "),
      argHint: c.argHint,
      source: "custom" as const,
    })),
  ] as SlashCommandEntry[]);

  async function loadCustomCommands() {
    if (customCommandsLoaded) return;
    if (customCommandsLoading) return customCommandsLoading;
    customCommandsLoading = Bridge.Commands()
      .then((cmds) => {
        customCommands = cmds ?? [];
        customCommandsLoaded = true;
      })
      .catch(() => {
        customCommands = [];
        customCommandsLoaded = true;
      })
      .finally(() => {
        customCommandsLoading = null;
      });
    return customCommandsLoading;
  }
  $effect(() => {
    if (sessionId) void loadCustomCommands();
  });

  // TUI safeWhileRunning(), translated to the GUI command names. Other commands
  // replace the session, start a fresh turn, or exit/navigate in ways that would
  // race an active agent turn, so they are refused until the turn is stopped.
  const safeWhileRunning = new Set([
    "effort", "search", "fast", "perm", "model", "help", "goal", "loop", "config", "route",
    "skills", "tools", "observe", "obs", "find", "copy", "read", "voice", "mute", "dictate", "talk", "speak",
    "rail", "changes", "term", "tasks", "shells", "tray", "workflow", "rename", "background", "bg", "add-dir",
    "mouse", "steer", "queue", "ban", "unban", "home", "sessions", "plugins", "hooks", "plugin", "marketplace",
  ]);

  function slashNote(text: string) {
    if (store?.appendNote(text) == null) toasts.info(text.split("\n")[0] ?? text);
  }
  function requireActiveSession(): string | null {
    if (!sessionId) {
      slashNote("no active session");
      return null;
    }
    return sessionId;
  }
  function missingArg(name: string, usage: string) {
    slashNote(`usage: /${name} ${usage}`.trim());
    return true;
  }
  function onOff(arg: string): boolean | null {
    switch (arg.toLowerCase()) {
      case "on":
      case "true":
      case "1":
      case "yes":
        return true;
      case "off":
      case "false":
      case "0":
      case "no":
        return false;
      default:
        return null;
    }
  }
  function splitBan(arg: string): { title: string; rule: string } {
    const i = arg.indexOf(":");
    if (i < 0) return { title: "", rule: "" };
    return { title: arg.slice(0, i).trim(), rule: arg.slice(i + 1).trim() };
  }
  function openDock(tab: "info" | "terminal" | "diff" | "files" | "browser") {
    setDockTab(tab);
    if (dockCollapsed) {
      dockCollapsed = false;
      try {
        localStorage.setItem("eigen.dockCollapsed", "0");
      } catch {}
    }
  }
  function reviewPrompt(target: string): string {
    const what = target || "the work you just did in this session";
    return "Use the review tool to get a cross-vendor critique of " + what + ". Package the relevant artifact (the plan, diff, or code) into the tool's `artifact` argument with enough context to judge it, set an appropriate `focus`, then act on the critique — fix real issues it raises and note anything you disagree with and why.";
  }
  async function submitSyntheticTurn(prompt: string): Promise<boolean> {
    const id = requireActiveSession();
    if (!id) return false;
    const optimisticUid = store?.appendUserMessage(prompt, []);
    store?.markPending();
    try {
      await Bridge.SendInput(id, prompt, [], []);
      return true;
    } catch (e) {
      store?.removeBlock(optimisticUid);
      store?.clearPending();
      toasts.error(errText(e));
      return false;
    }
  }

  async function runBuiltinSlashCommand(name: string, args: string): Promise<boolean | null> {
    if (!builtinSlashCommands.some((c) => c.name === name)) return null;
    if ((store?.running ?? false) && !safeWhileRunning.has(name)) {
      slashNote(`/${name} can't run mid-turn — press Stop first`);
      return true;
    }
    const id = sessionId;
    switch (name) {
      case "help": {
        const custom = customCommands.length
          ? "\n\nCustom commands:\n" + customCommands.map((c) => `  /${c.name}${c.argHint ? ` ${c.argHint}` : ""} — ${c.description || "custom command"}`).join("\n")
          : "";
        slashNote(slashHelpText() + custom);
        return true;
      }
      case "home":
        router.go("home");
        return true;
      case "sessions":
      case "resume":
        router.go("sessions");
        return true;
      case "tasks":
        router.go("tasks");
        return true;
      case "tray":
        router.go("live");
        return true;
      case "observe":
      case "obs":
        router.go("observe");
        return true;
      case "plugins":
      case "plugin":
      case "marketplace":
      case "hooks":
        router.go("plugins");
        if (name === "hooks") toasts.info("hooks live under Plugins / Observe in the GUI");
        return true;
      case "rail":
        ui.toggleRail();
        return true;
      case "changes":
        toggleDockCollapsed();
        return true;
      case "term":
        openDock("terminal");
        return true;
      case "shells":
        openDock("info");
        slashNote(sess?.shells?.length ? "background shells are shown in the Info dock" : "no background shells");
        return true;
      case "background":
      case "bg":
        if (!(store?.running ?? false)) {
          slashNote("nothing running to background");
          return true;
        }
        toasts.info("moved to background — the daemon keeps running it; reattach from Chat/Sessions to collect");
        router.go("home");
        return true;
      case "steer":
        setInputMode("steer");
        slashNote("input mode → steer (Enter injects into a running turn)");
        return true;
      case "queue":
        setInputMode("queue");
        slashNote("input mode → queue (Enter waits for the next turn)");
        return true;
      case "model": {
        if (!id) return false;
        if (!args) {
          await loadModels();
          const sample = models.slice(0, 8).map((m) => m.id).join(", ");
          slashNote(`model: ${sess?.model || "unknown"}${sample ? `\ntry: /model ${sample}` : ""}`);
          return true;
        }
        const parts = args.split(/\s+/).filter(Boolean);
        const model = parts.length >= 2 ? `${parts[0]}:${parts.slice(1).join(" ")}` : args;
        const st = await run(() => Bridge.SetModel(id, model));
        if (!st) return false;
        applyState(id, st);
        slashNote("model → " + st.model);
        return true;
      }
      case "perm": {
        if (!id) return false;
        const v = args.toLowerCase();
        if (!v) {
          slashNote(`permission posture: ${sess?.perm || "unknown"}  (use /perm gated|auto)`);
          return true;
        }
        if (v !== "gated" && v !== "auto") return missingArg("perm", "gated|auto");
        const st = await run(() => Bridge.SetPerm(id, v));
        if (!st) return false;
        applyState(id, st);
        slashNote("permission posture → " + st.perm);
        return true;
      }
      case "effort": {
        if (!id) return false;
        if (!args) {
          slashNote(sess?.effort ? `reasoning effort: ${sess.effort}   (/effort ${effortLevels.join("|")})` : "the current model does not support a reasoning-effort setting");
          return true;
        }
        const st = await run(() => Bridge.SetEffort(id, args));
        if (!st) return false;
        applyState(id, st);
        slashNote("reasoning effort → " + (st.effort || args));
        return true;
      }
      case "search": {
        if (!id) return false;
        if (!args) {
          slashNote(sess?.search ? `live search: ${sess.search}   (/search off|auto|on)` : "the current model does not support live search");
          return true;
        }
        const v = args.toLowerCase();
        if (!SEARCH_MODES.includes(v)) return missingArg("search", "off|auto|on");
        const st = await run(() => Bridge.SetSearch(id, v));
        if (!st) return false;
        applyState(id, st);
        slashNote("live search → " + (st.search || v));
        return true;
      }
      case "fast": {
        if (!id) return false;
        const want = args ? onOff(args) : !(sess?.fast ?? false);
        if (want == null) return missingArg("fast", "[on|off]");
        const st = await run(() => Bridge.SetFast(id, want));
        if (!st) return false;
        applyState(id, st);
        slashNote("fast mode → " + (st.fast ? "on" : "off"));
        return true;
      }
      case "goal": {
        if (!id) return false;
        if (!args) {
          slashNote(sess?.goal ? `goal: ${sess.goal}   (/goal <new text> · /goal clear)` : "no goal set  (/goal <text> to set a persistent north star)");
          return true;
        }
        const next = ["clear", "none", "off"].includes(args.toLowerCase()) ? "" : args;
        const st = await run(() => Bridge.SetGoal(id, next));
        if (!st) return false;
        applyState(id, st);
        slashNote(next ? "goal → " + next : "goal cleared");
        return true;
      }
      case "rename": {
        if (!id) return false;
        if (!args) {
          startTitle();
          slashNote("rename: edit the session title in the Info dock");
          return true;
        }
        const st = await run(() => Bridge.SetTitle(id, args));
        if (!st) return false;
        applyState(id, st);
        await sessions.refresh();
        slashNote("renamed → " + args);
        return true;
      }
      case "clear":
        if (!id) return false;
        await clearSession();
        slashNote("— cleared —");
        return true;
      case "compact":
        if (!id) return false;
        await compact();
        return true;
      case "save":
      case "export": {
        if (!id) return false;
        const path = await run(() => Bridge.ExportSession(id));
        if (!path) return false;
        slashNote(`exported → ${path}`);
        return true;
      }
      case "add-dir": {
        if (!id) return false;
        if (!args) {
          const roots = sess?.roots ?? [];
          slashNote(roots.length ? "working directories:\n  " + roots.join("\n  ") + "\n(/add-dir <path> to grant another)" : "no sandbox roots reported");
          return true;
        }
        const root = await run(() => Bridge.AddDir(id, args));
        if (!root) return false;
        slashNote("added working directory → " + root);
        refreshState();
        return true;
      }
      case "review":
        if (!id) return false;
        return submitSyntheticTurn(reviewPrompt(args));
      case "workflow": {
        if (!id) return false;
        const fields = args.split(/\s+/).filter(Boolean);
        if (fields.length === 0) {
          const wfs = await run(() => Bridge.Workflows());
          if (!wfs) return false;
          slashNote(wfs.length ? "workflows: " + wfs.map((w) => w.name).join(", ") + "   ·   /workflow <name> [k=v …]" : "no workflows yet — author one under ~/.eigen/workflows/<name>.md");
          return true;
        }
        const [wfName, ...pairs] = fields;
        const vars: Record<string, string> = {};
        for (const kv of pairs) {
          const eq = kv.indexOf("=");
          if (eq > 0) vars[kv.slice(0, eq)] = kv.slice(eq + 1);
        }
        toasts.info(`workflow ${wfName} started`);
        Bridge.RunWorkflow(id, wfName, vars)
          .then((res) => {
            if (res) toasts.success(`workflow ${wfName}: ${res.completed.length} step${res.completed.length === 1 ? "" : "s"} complete`);
            refreshState();
          })
          .catch((e) => toasts.error(errText(e)));
        slashNote(`workflow ${wfName} started`);
        return true;
      }
      case "tools": {
        const tools = sess?.tools ?? [];
        slashNote(tools.length ? "tools:\n" + tools.map((t) => `  ${t.read_only ? "·" : "✎"} ${t.name}`).join("\n") : "no tools");
        return true;
      }
      case "skills": {
        if (!args) {
          router.go("skills");
          return true;
        }
        const body = await run(() => Bridge.SkillBody(args));
        if (!body) return false;
        slashNote(`skill: ${args}\n\n${body}`);
        return true;
      }
      case "config": {
        if (!args) {
          router.go("config");
          return true;
        }
        const [key, ...rest] = args.split(/\s+/);
        if (!key || rest.length === 0) {
          const cfg = await run(() => Bridge.Config());
          const f = cfg?.fields.find((x) => x.key === key);
          slashNote(f ? `${f.key} = ${f.value || "(unset)"}\n${f.desc}${f.options?.length ? `\nvalues: ${f.options.join("|")}` : ""}` : "usage: /config <key> <value>  (or bare /config to open settings)");
          return true;
        }
        const value = rest.join(" ");
        const saved = await run(() => Bridge.SetConfig(key, value));
        if (saved === undefined) return false;
        slashNote(`config: ${key} = ${saved || value}   (applies to new sessions)`);
        return true;
      }
      case "route": {
        if (!args) {
          const cfg = await run(() => Bridge.Config());
          const route = cfg?.fields.find((f) => f.key === "route")?.value;
          const providers = cfg?.fields.find((f) => f.key === "route_providers")?.value;
          slashNote(`routing: ${route === "true" ? "on" : "off"}${providers ? ` (providers: ${providers})` : " (all credentialed providers)"}`);
          return true;
        }
        if (args !== "on" && args !== "off") return missingArg("route", "on|off");
        await run(() => Bridge.SetConfig("route", args === "on" ? "true" : "false"));
        slashNote(`model-assessed routing ${args.toUpperCase()}`);
        return true;
      }
      case "ban": {
        if (!args) {
          router.go("memory");
          slashNote("/ban <title>: <rule> records a hard prohibition in project memory");
          return true;
        }
        const { title, rule } = splitBan(args);
        if (!title || !rule) return missingArg("ban", "<title>: <rule>");
        const replaced = await run(() => Bridge.AddBan("project", title, rule));
        if (replaced === undefined) return false;
        slashNote(`${replaced ? "updated ban" : "banned"}: ${title}`);
        return true;
      }
      case "unban": {
        if (!args) return missingArg("unban", "<title>");
        const removed = await run(() => Bridge.RemoveBan("project", args));
        if (removed === undefined) return false;
        slashNote(removed ? `removed ban: ${args}` : `no ban titled ${args}`);
        return true;
      }
      case "copy": {
        if (!lastAssistant) {
          slashNote("nothing to copy");
          return true;
        }
        try {
          await navigator.clipboard.writeText(lastAssistant);
          slashNote(`copied ${lastAssistant.length} chars`);
        } catch (e) {
          toasts.error(errText(e));
          return false;
        }
        return true;
      }
      case "find": {
        if (!args) return missingArg("find", "<text>");
        const found = (window as unknown as { find?: (query: string) => boolean }).find?.(args);
        slashNote(found === false ? `no matches for ${args}` : `find: ${args}`);
        return true;
      }
      case "voice":
        if (args === "setup" || args === "doctor") {
          slashNote(`voice: STT ${voice.stt ? "available" : "missing"}, TTS ${voice.tts ? "available" : "missing"}`);
          return true;
        }
        toggleVoiceMode();
        return true;
      case "mute":
        await voice.stopMode();
        slashNote("voice mode off");
        return true;
      case "dictate":
      case "talk": {
        const heard = await voice.dictate();
        if (!heard) {
          slashNote("dictation heard nothing (or STT is unavailable)");
          return true;
        }
        return submitSyntheticTurn(heard);
      }
      case "speak":
        if (!lastAssistant) slashNote("nothing to speak");
        else await voice.speak(lastAssistant);
        return true;
      case "read":
        slashNote("GUI read-aloud is one-shot: use /speak or the 🔊 button on an answer.");
        return true;
      case "loop":
        slashNote("/loop is currently TUI-local; in the GUI, set a Goal for persistent autonomous follow-up.");
        return true;
      case "mouse":
        slashNote("/mouse is terminal-only; the GUI always allows normal text selection.");
        return true;
      case "rebuild":
        slashNote("/rebuild is available in the TUI/dev workflow; rebuild the GUI from a terminal to avoid disrupting daemon sessions.");
        return true;
      case "quit":
      case "exit":
        slashNote("Close this window from your desktop shell to quit the GUI.");
        return true;
      default:
        slashNote(`/${name} is not implemented in the GUI yet (try /help)`);
        return true;
    }
  }

  async function maybeRunCommand(text: string): Promise<boolean | null> {
    const parsed = parseSlashCommand(text);
    if (!parsed) return null;
    await loadCustomCommands();

    const builtin = await runBuiltinSlashCommand(parsed.name, parsed.args);
    if (builtin !== null) return builtin;

    const cmd = customCommands.find((c) => c.name.toLowerCase() === parsed.name);
    if (!cmd) {
      slashNote(`unknown command /${parsed.rawName} (try /help)`);
      return true;
    }
    if (!sessionId) return false;
    if (store?.running) {
      slashNote(`finish or stop the current turn before running /${cmd.name}`);
      return true;
    }
    store?.markPending();
    try {
      const prompt = await Bridge.RunCommand(sessionId, cmd.name, parsed.args);
      store?.appendUserMessage(prompt, []);
      return true;
    } catch (e) {
      store?.clearPending();
      toasts.error(errText(e));
      return false;
    }
  }

  // Start a fresh session without leaving Chat — new-chat from the control bar.
  // The New-chat button opens a tiny inline prompt for the working directory:
  // the session's primary root locks at creation (NewSession's first arg), so
  // this is the one chance to choose it. A blank dir is valid — NewSession falls
  // back to the daemon's cwd — so the Start action never blocks on an empty path.
  let startingNew = $state(false);
  let newChatOpen = $state(false);
  let newChatDir = $state("");
  // Recent project dirs for the new-chat quick-pick. Loaded once (guarded by
  // recentDirsLoaded) the first time the picker opens, so the popover doesn't
  // pay a daemon round-trip until the user actually reaches for it.
  let recentDirs = $state<RecentDirDTO[]>([]);
  let recentDirsLoaded = $state(false);
  async function loadRecentDirs() {
    if (recentDirsLoaded) return;
    recentDirsLoaded = true;
    const r = await run(() => Bridge.RecentDirs());
    if (r) recentDirs = r;
  }
  // Load when the popover opens (one-shot — the guard makes re-opens free).
  $effect(() => {
    if (newChatOpen) loadRecentDirs();
  });
  // Native OS folder picker — fills newChatDir on a real selection, no-op on
  // cancel (PickDirectory returns "" when the dialog is dismissed).
  async function pickDir() {
    const picked = await run(() => Bridge.PickDirectory());
    if (picked) newChatDir = picked;
  }
  async function startNewChat() {
    if (startingNew) return;
    startingNew = true;
    try {
      const id = await Bridge.NewSession(newChatDir.trim(), "", "");
      await sessions.refresh();
      newChatDir = "";
      newChatOpen = false;
      router.go("chat", id);
    } catch (e) {
      toasts.error(errText(e));
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
      const hit = await Bridge.Interrupt(sessionId);
      if (!hit) toasts.info("nothing running to stop");
    } catch (e) {
      toasts.error(errText(e));
    } finally {
      interrupting = false;
    }
  }
  // Belt-and-suspenders: if the turn ends (running→false) while the Interrupt
  // RPC is still pending, drop the interrupting state so the composer doesn't
  // wedge on "stopping…". Harmless when the finally arm already cleared it.
  // Use `working` (running OR pending) so a just-submitted-but-not-yet-streaming
  // turn can still show/keep the Stop affordance until the daemon settles it.
  $effect(() => {
    if ((store?.working ?? false) === false && interrupting) interrupting = false;
  });
  // Queue drain (queue input-mode): when the turn transitions running→false,
  // submit the next held message as a fresh turn. `wasRunning` tracks the prior
  // value so this fires once per turn-end edge, not on every effect re-run —
  // without it a re-render while idle (queued mutating, a State refresh) would
  // re-drain. Each drained item starts a turn that flips running back to true;
  // when THAT ends, the effect fires again for the next item.
  let wasRunning = $state(false);
  $effect(() => {
    const running = store?.running ?? false;
    if (wasRunning && !running && queued.length > 0) drainQueue();
    wasRunning = running;
  });

  // Hands-free voice mode runs against THIS session (listen → submit → speak →
  // listen). The composer reflects voice.modeOn and calls this to toggle.
  function toggleVoiceMode() {
    if (!sessionId) return;
    voice.toggleMode(sessionId);
  }
  // Plain-language cue for the voice-mode banner, mapped from the live phase.
  const voicePhaseLabel = $derived(
    voice.phase === "listening"
      ? "listening…"
      : voice.phase === "transcribing"
        ? "transcribing…"
        : voice.phase === "thinking"
          ? "thinking…"
          : voice.phase === "speaking"
            ? "speaking…"
            : voice.phase === "error"
              ? "voice error"
              : "voice mode",
  );
  // Stop the conversation loop when leaving this session (route change /
  // unmount) so it never keeps listening against a session no longer shown.
  $effect(() => {
    void sessionId;
    return () => {
      if (voice.modeOn) voice.stopMode();
    };
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
      toasts.error(errText(e));
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
      toasts.error(errText(e));
    }
  }

  // GUI-061: sess.tokens only updates on refreshState (turn end / approval). During
  // a long turn, stream events carry inTokens/outTokens into store.liveTokens —
  // use the larger figure so the dock ring moves while the model streams.
  const effectiveTokens = $derived(
    Math.max(sess?.tokens ?? 0, store?.liveTokens ?? 0),
  );
  const pct = $derived(
    sess && sess.maxTokens > 0
      ? Math.min(100, Math.round((effectiveTokens / sess.maxTokens) * 100))
      : 0,
  );
  // Near-context-limit nudge: surface only once we're genuinely close and the
  // max is known, so it reads as a real prompt to compact rather than chrome.
  const nearLimit = $derived(
    sess != null && sess.maxTokens > 0 && effectiveTokens / sess.maxTokens > 0.85,
  );

  // ── right-dock tabs: session Info + the tools panel ─────────────────────────
  // The dock is a tabbed tools panel: Info (the session groups) + Terminal /
  // Diff / Files / Browser. The selected tab persists across reloads. The tool
  // panels target the session's primary sandbox root; when there is none they
  // show their own empty/error states.
  type DockTab = "info" | "terminal" | "diff" | "files" | "browser";
  // Resolve the persisted tab once into a plain local before seeding the runes
  // (reading one $state inside another's initializer trips svelte-check).
  let initialDockTab: DockTab = "info";
  try {
    const saved = localStorage.getItem("eigen.dockTab");
    if (saved === "info" || saved === "terminal" || saved === "diff" || saved === "files" || saved === "browser") {
      initialDockTab = saved;
    }
  } catch {}
  let dockTab = $state<DockTab>(initialDockTab);
  // Browser keeps its page/navigation state across tab switches: once opened it
  // stays MOUNTED and is hidden (not unmounted) when another tab is active, so
  // the loaded page survives. Terminal/Diff/Files mount-when-active (Terminal so
  // a hidden tab never spawns a PTY; Diff/Files so no git/fs call until opened).
  let browserOpened = $state(initialDockTab === "browser");
  // Dock terminal: keep mounted once opened (like Browser) so switching tabs does
  // not kill the PTY and lose cwd/history in the user's real $SHELL.
  let terminalOpened = $state(initialDockTab === "terminal");
  function setDockTab(t: DockTab) {
    dockTab = t;
    if (t === "browser") browserOpened = true;
    if (t === "terminal") terminalOpened = true;
    try {
      localStorage.setItem("eigen.dockTab", t);
    } catch {}
  }
  const primaryRoot = $derived(sess?.roots?.[0] ?? "");
  // The tool tabs (Terminal/Diff/Files/Browser) act on the GUI's OWN machine —
  // a local PTY, the local filesystem at primaryRoot, a local browser. For a
  // REMOTE session those would target the wrong host (primaryRoot is a path on
  // the remote), so the dock is Info-only when remote: honest over misleading.
  const allDockTabs: { id: DockTab; label: string; glyph: string }[] = [
    { id: "info", label: "Info", glyph: "ⓘ" },
    { id: "terminal", label: "Terminal", glyph: "❯" },
    { id: "diff", label: "Diff", glyph: "⇄" },
    { id: "files", label: "Files", glyph: "⊟" },
    { id: "browser", label: "Browser", glyph: "◍" },
  ];
  const dockTabs = $derived(isRemote ? allDockTabs.slice(0, 1) : allDockTabs);
  // If a tool tab was persisted/active and we land on a remote session, fall
  // back to Info so the dock never shows a tab the remote session can't use.
  $effect(() => {
    if (isRemote && dockTab !== "info") setDockTab("info");
  });

  // ── dock collapse + resize ─────────────────────────────────────────────────
  // The tools dock collapses to a thin glyph rail (click a glyph to expand back
  // to that tab) and its width is drag-resizable. Both persist. Width is clamped
  // so the chat column always keeps usable space.
  const DOCK_MIN = 240;
  const DOCK_MAX = 680;
  function readDockWidth(): number {
    try {
      const v = parseInt(localStorage.getItem("eigen.dockWidth") ?? "", 10);
      if (!Number.isNaN(v)) return Math.min(DOCK_MAX, Math.max(DOCK_MIN, v));
    } catch {}
    return 300;
  }
  // Right dock (Info/Terminal/Diff/Files/Browser) defaults CLOSED — a 300px
  // standing column at rest is the biggest single chrome-bulk source, and at
  // rest it only shows model + context + a couple edit buttons. Collapsed = a
  // 48px glyph rail; click a glyph (or a pending approval, see the $effect
  // below) to expand. A stored value still wins, so a user who opens it keeps
  // it open.
  let dockCollapsed = $state(true);
  try {
    const v = localStorage.getItem("eigen.dockCollapsed");
    if (v != null) dockCollapsed = v === "1";
  } catch {}
  let dockWidth = $state(readDockWidth());

  function toggleDockCollapsed() {
    dockCollapsed = !dockCollapsed;
    try {
      localStorage.setItem("eigen.dockCollapsed", dockCollapsed ? "1" : "0");
    } catch {}
  }
  // Clicking a glyph while collapsed expands the dock to that tab.
  function pickCollapsedTab(t: DockTab) {
    setDockTab(t);
    dockCollapsed = false;
    try {
      localStorage.setItem("eigen.dockCollapsed", "0");
    } catch {}
  }

  // Drag-resize from the dock's left edge. Pointer events on the grip; we widen
  // as the pointer moves LEFT (negative dx → wider, since the dock is right-hung).
  let resizing = $state(false);
  function startResize(e: PointerEvent) {
    e.preventDefault();
    resizing = true;
    const startX = e.clientX;
    const startW = dockWidth;
    const onMove = (ev: PointerEvent) => {
      const next = Math.min(DOCK_MAX, Math.max(DOCK_MIN, startW + (startX - ev.clientX)));
      dockWidth = next;
    };
    const onUp = () => {
      resizing = false;
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      try {
        localStorage.setItem("eigen.dockWidth", String(dockWidth));
      } catch {}
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }

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
      toasts.error(errText(e));
      return undefined;
    }
  }

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

  // Routing model ids load once, lazily. The control bar's model selector is now
  // a custom <Dropdown> (no onfocus hook like the old native select), so we call
  // loadModels() once on mount — cheap and idempotent (modelsLoaded guards the
  // round-trip), and the bar's current value still shows before they land.
  async function loadModels() {
    if (modelsLoaded) return;
    modelsLoaded = true;
    const r = await run(() => Bridge.Routing());
    if (r?.models) models = r.models;
  }
  $effect(() => {
    loadModels();
  });

  // Control-bar Dropdown option lists, derived from the loaded routing models /
  // ladders. model: id label, marked + disabled when unavailable. effort/search:
  // the level/mode as both value and label.
  const modelOptions = $derived(
    models.length === 0
      ? [{ value: sess?.model ?? "", label: sess?.model || "—" }]
      : models.map((m) => ({
          value: m.id,
          label: m.id + (m.available ? "" : " (unavailable)"),
          disabled: !m.available,
        })),
  );
  const effortOptions = $derived(effortLevels.map((lv) => ({ value: lv, label: lv })));
  const searchOptions = SEARCH_MODES.map((mode) => ({ value: mode, label: mode }));

  // The control-bar selectors are the custom <Dropdown>, not native <select>s
  // (webkit2gtk paints a native option list black-on-black). Each takes the
  // chosen value directly via Dropdown's onchange, so these are value-taking
  // (not Event-reading) — the run()/Bridge.Set*/applyState logic is unchanged.
  async function setModel(v: string) {
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
  async function setEffort(v: string) {
    const id = sessionId;
    if (!id) return;
    applyState(id, await run(() => Bridge.SetEffort(id, v)) ?? null);
  }
  async function setSearch(v: string) {
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
  // The title shown when the session hasn't been explicitly named. A remote
  // session isn't in the local sessions.list, so fall back to its State() title
  // (sess.title) — the remote daemon's own session title — before the generic.
  const derivedTitle = $derived(
    sessions.list.find((s) => s.id === sessionId)?.title || sess?.title || "untitled session",
  );

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
  // Collapse a long absolute root to its last two path segments (…/parent/leaf)
  // so the dock reads as a clean label, not a wrapping URL. The full path stays
  // in the title attr for when the exact location matters. Short paths pass
  // through untouched. Trailing slash trimmed so the leaf isn't lost.
  function prettyPath(p: string): string {
    const trimmed = p.replace(/\/+$/, "");
    const segs = trimmed.split("/").filter(Boolean);
    if (segs.length <= 2) return trimmed || "/";
    return "…/" + segs.slice(-2).join("/");
  }
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
  // Jump to bottom on session switch / when skeleton → transcript (not per message).
  const transcriptScrollToken = $derived(`${sessionId}:${transcriptLoading ? "load" : "show"}`);
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
    return tail && tail.kind === "text" && (tail.role == null || tail.role === "assistant") ? tail.text : "";
  });

  // A human label for a transcript block's kind, voiced as the row's aria-label
  // so a SR user hears what each row is (assistant prose, the model's reasoning,
  // a tool call, or a system note) rather than an unlabeled region. Tool rows
  // delegate their detail to ToolCallCard; this names the row around it.
  const SAFE_IMAGE_TYPES = new Set(["image/png", "image/jpeg", "image/jpg", "image/webp", "image/gif", "image/bmp"]);

  function imageSrc(image: ImageDTO): string {
    const mediaType = (image.mediaType || "image/png").toLowerCase();
    if (!SAFE_IMAGE_TYPES.has(mediaType) || !image.data) return "";
    return `data:${mediaType};base64,${image.data}`;
  }

  function imageKey(image: ImageDTO, index: number): string {
    const data = image.data || "";
    return `${index}:${image.mediaType}:${data.length}:${data.slice(0, 24)}:${data.slice(-24)}`;
  }

  function rowLabel(block: { kind: string; role?: string; images?: ImageDTO[] }): string {
    switch (block.kind) {
      case "reasoning":
        return "Assistant reasoning";
      case "tool":
        return "Tool call";
      case "toolgroup":
        return "Tool calls";
      case "note":
        return "System note";
      default:
        if (block.role === "user") return block.images?.length ? "User message with image" : "User message";
        if (block.role === "system") return "System message";
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


  // Global Escape-to-stop for the chat view. Mirrors the TUI's "esc interrupts"
  // behavior, but yields to overlays and non-composer form fields so Escape can
  // still close a popover/dialog or cancel an edit. The composer textarea is the
  // intentional exception: while a turn is running, Esc stops the turn.
  function shouldEscStopTurn(e: KeyboardEvent): boolean {
    if (e.key !== "Escape" || e.defaultPrevented || e.repeat || e.isComposing || e.keyCode === 229) return false;
    if (!sessionId || !online || interrupting || !(store?.working ?? false)) return false;
    if (newChatOpen || menuOpen || editingTitle || editingGoal || confirmClear) return false;
    // If any shared popover/dialog is open, let that surface own Escape even if
    // focus is currently on the body or the trigger instead of inside the panel.
    if (document.querySelector('[role="dialog"][aria-modal="true"], .pop, .sheet, .pal, .sc')) return false;

    const target = e.target instanceof HTMLElement ? e.target : null;
    if (!target) return true;

    // Do not steal Escape from modal/popover surfaces or the embedded terminal
    // (where Esc may be a real shell/editor keystroke).
    if (target.closest('[role="dialog"], [aria-modal="true"], .pop, .sheet, .pal, .sc, .term')) return false;

    // Most text controls use Escape for their own cancel/clear semantics. The
    // composer textarea is different: the running-turn shortcut is expected
    // there, matching the terminal UI.
    const tag = target.tagName;
    const formControl = tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable;
    if (formControl && !target.closest(".composer")) return false;

    return true;
  }

  function onGlobalKeydown(e: KeyboardEvent) {
    if (!shouldEscStopTurn(e)) return;
    e.preventDefault();
    void interrupt();
  }

</script>

<svelte:window onkeydown={onGlobalKeydown} />

{#snippet attachedImages(images: ImageDTO[] | undefined)}
  {#if images?.length}
    <div class="msg__images" aria-label="Attached images">
      {#each images as image, i (imageKey(image, i))}
        {@const src = imageSrc(image)}
        {#if src}
          <img
            class="msg__image"
            src={src}
            alt={`attached screenshot ${i + 1}`}
            loading="lazy"
            decoding="async"
          />
        {:else}
          <span class="msg__image-fallback">unsupported image</span>
        {/if}
      {/each}
    </div>
  {/if}
{/snippet}

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
        <!-- New-chat: opens a tiny inline prompt to pick the working directory
             BEFORE the session starts. The primary root locks at creation, so
             choosing it here (vs. always rooting at the daemon's cwd) is the one
             chance to set it. Blank is valid — it falls back to the daemon cwd. -->
        <Popover label="New chat" align="start" width={272} bind:open={newChatOpen}>
          {#snippet trigger(toggle)}
            <button class="ctl__new" onclick={toggle} disabled={startingNew} title="Start a new chat">
              {#if startingNew}<span class="dock__spinner" aria-hidden="true"></span>{:else}<span class="ctl__plus" aria-hidden="true">+</span>{/if}
              New chat
            </button>
          {/snippet}
          <div class="newchat">
            <span class="newchat__label">working directory</span>
            <!-- Quick-pick recents: a click fills newChatDir without typing a
                 full path. Omitted entirely when there are no recents. -->
            {#if recentDirs.length > 0}
              <ul class="newchat__recents">
                {#each recentDirs as entry (entry.dir)}
                  <li>
                    <button
                      type="button"
                      class="newchat__recent"
                      class:newchat__recent--on={newChatDir === entry.dir}
                      title={entry.dir}
                      onclick={() => (newChatDir = entry.dir)}
                    >
                      <span class="newchat__recent-name">{entry.name}</span>
                      <span class="newchat__recent-dir">{prettyPath(entry.dir)}</span>
                    </button>
                  </li>
                {/each}
              </ul>
            {/if}
            <!-- svelte-ignore a11y_autofocus -->
            <input
              id="newchat-dir"
              class="dock__input"
              bind:value={newChatDir}
              placeholder="working directory (blank = default)"
              autofocus
              onkeydown={(e) => {
                if (e.key === "Enter") { e.preventDefault(); startNewChat(); }
                else if (e.key === "Escape") { newChatOpen = false; }
              }}
            />
            <div class="newchat__actions">
              <Button variant="ghost" size="sm" onclick={pickDir} title="Browse for a folder">Browse…</Button>
              <Button variant="primary" size="sm" loading={startingNew} onclick={startNewChat} title="Start a new chat in this directory">Start</Button>
            </div>
          </div>
        </Popover>
        <div class="ctl__sep"></div>
        <span class="ctl__field" title="Model for this session">
          <span class="ctl__k">model</span>
          <Dropdown
            value={sess?.model ?? ""}
            options={modelOptions}
            label="Model"
            width={220}
            onchange={setModel}
          />
        </span>
        <button class="ctl__pill" class:ctl__pill--warn={sess?.perm === "auto"} onclick={onPerm} title="Toggle gated approvals vs auto-approve">
          {sess?.perm === "auto" ? "auto" : "gated"}
        </button>
        {#if sess?.effort}
          <span class="ctl__field" title="Reasoning effort">
            <span class="ctl__k">effort</span>
            <Dropdown value={sess.effort} options={effortOptions} label="Effort" width={120} onchange={setEffort} />
          </span>
        {/if}
        {#if sess?.search}
          <span class="ctl__field" title="Live search mode">
            <span class="ctl__k">search</span>
            <Dropdown value={sess.search} options={searchOptions} label="Search mode" width={120} onchange={setSearch} />
          </span>
        {/if}
        {#if sess?.fastOk}
          <button class="ctl__pill" class:ctl__pill--on={sess.fast} onclick={onFast} title="Route eligible turns to the fast tier">
            fast {sess.fast ? "on" : "off"}
          </button>
        {/if}
        <!-- Steer/queue (mirrors the TUI inputMode): decides what a mid-turn
             send does — inject into the running turn, or hold and submit it as
             a fresh turn once the current one finishes. Lit when queue is on. -->
        <button
          class="ctl__pill"
          class:ctl__pill--on={inputMode === "queue"}
          onclick={() => setInputMode(inputMode === "queue" ? "steer" : "queue")}
          title="steer = inject into the running turn; queue = hold and send when the turn finishes"
        >
          {store?.running ? (inputMode === "queue" ? "⇊ queue" : "↳ steer") : inputMode === "queue" ? "⇊ queue mode" : "input mode"}
        </button>
      </div>
      <!-- ACTIVE PLAN — the `todo` tool's live task list, pinned above the
           transcript so the user always sees what the agent is working through.
           Shown only while there's unfinished work; collapsible to a one-line
           summary so it never crowds the conversation. -->
      {#if todosActive}
        <div class="plan" class:plan--collapsed={todoCollapsed}>
          <button class="plan__head" onclick={() => (todoCollapsed = !todoCollapsed)} title={todoCollapsed ? "Show plan" : "Collapse plan"}>
            <span class="plan__chev" class:plan__chev--open={!todoCollapsed} aria-hidden="true"></span>
            <span class="plan__title">Plan</span>
            <span class="plan__count tnum">{todoDone}/{todos.length}</span>
            {#if todoCollapsed}
              {@const cur = todos.find((t) => t.status === "in_progress")}
              {#if cur}<span class="plan__current">{cur.content}</span>{/if}
            {/if}
          </button>
          {#if !todoCollapsed}
            <ul class="plan__list">
              <!-- key by content+index: two plan tasks can carry identical text
                   (e.g. repeated "investigate" steps), and a bare content key
                   then throws each_key_duplicate and crashes the whole view. -->
              {#each todos as t, ti (t.content + " " + ti)}
                <li class="ptask ptask--{t.status}">
                  <span class="ptask__glyph">{todoGlyph(t.status)}</span>
                  <span class="ptask__text">{t.content}</span>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      {/if}
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
            <VirtualList
              items={rows}
              estimateHeight={120}
              pin
              scrollToken={transcriptScrollToken}
              bind:listRef={transcriptListEl}
              key={(r) => r.uid}
            >
              {#snippet row(block, i)}
                <div class="chat__row" role="article" aria-label={rowLabel(block)}>
                  {#if block.uid === LIVE_UID}
                    <!-- The in-flight streaming block, now a real row in the list
                         (not a separate pane), so it flows continuously with the
                         conversation. Same Markdown render as committed messages
                         + a trailing caret. Commits to history (real uid) when the
                         turn ends, which replaces this synthetic row in place. -->
                    {#if block.kind === "reasoning"}
                      <div class="msg msg--reasoning msg--live">
                        <span class="msg__tag">reasoning</span>
                        <div class="msg__reasoning-body msg__reasoning-body--live">
                          {block.text}<span class="caret" aria-hidden="true"></span>
                        </div>
                      </div>
                    {:else if block.kind === "text"}
                      <div class="msg msg--text msg--live">
                        <div class="msg__live-prose"><Markdown source={block.text} /><span class="caret" aria-hidden="true"></span></div>
                      </div>
                    {/if}
                  {:else if block.kind === "toolgroup"}
                    <!-- Consecutive tool calls fold into one "N tools" row, each
                         tool individually expandable. The running / just-run
                         tool's detail stays open until the next tool or a stream
                         supersedes it; isTail marks the live tail group. -->
                    <ToolGroupCard
                      group={block}
                      streaming={streaming}
                      isTail={i === rows.length - 1}
                      turnActive={store?.working ?? false}
                      {toolOpen}
                      {groupOpen}
                      onToolToggle={setToolOpen}
                      onGroupToggle={setGroupOpen}
                    />
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
                      <div class="msg__reasoning-body">
                        <Markdown source={block.text} />
                      </div>
                    </div>
                  {:else}
                    <!-- Completed assistant prose renders as Markdown (sans; fenced
                         code delegates to CodeBlock). When a TTS backend exists, a
                         hover-revealed read-aloud control speaks the block. -->
                    <div
                      class="msg msg--text"
                      class:msg--user={block.role === "user"}
                      class:msg--image-only={!block.text.trim() && !!block.images?.length}
                    >
                      {#if block.text.trim()}
                        <Markdown source={block.text} />
                      {/if}
                      {@render attachedImages(block.images)}
                      {#if voice.tts && block.role !== "user" && block.text.trim()}
                        <button
                          type="button"
                          class="msg__speak"
                          class:msg__speak--on={voice.speaking}
                          title={voice.speaking ? "Stop reading" : "Read aloud"}
                          aria-label={voice.speaking ? "Stop reading aloud" : "Read this message aloud"}
                          onclick={() => (voice.speaking ? voice.stopSpeak() : voice.speak(block.text))}
                        >{voice.speaking ? "◼" : "♪"}</button>
                      {/if}
                    </div>
                  {/if}
                </div>
              {/snippet}
            </VirtualList>
          </div>
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

      {#if store?.working}
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
          <!-- Steer/queue clarity: while a turn runs, the hint reflects the
               active inputMode — STEER injects typed input mid-turn; QUEUE holds
               it and sends it as the next turn once this one finishes. Surfacing
               the mode (and any held count) removes the "where did my message go"
               surprise. -->
          {#if store?.running}
            {#if inputMode === "queue"}
              <span class="chat__steerhint" title="Queue mode: typing now holds your message and sends it as a fresh turn when this one finishes">
                ⇊ queues for the next turn{queued.length > 0 ? ` — ${queued.length} queued` : ""}
              </span>
            {:else}
              <span class="chat__steerhint" title="Type now to steer the running turn; if it can't be steered it queues as the next turn">↳ steers the running turn</span>
            {/if}
          {/if}
        </div>
      {/if}

      {#if voice.modeOn}
        <!-- Hands-free voice mode: surface what the mic/speaker is doing so the
             loop never feels like a black box. The phase maps to a plain-language
             cue; the last transcript shows what eigen heard. -->
        <div class="chat__voicebar" role="status" aria-live="polite">
          <span class="chat__voicebar-dot" class:chat__voicebar-dot--live={voice.listening || voice.speaking}></span>
          <span class="chat__voicebar-label">{voicePhaseLabel}</span>
          {#if voice.lastText && voice.phase !== "speaking"}
            <span class="chat__voicebar-heard">“{voice.lastText}”</span>
          {/if}
          <button class="chat__voicebar-stop" onclick={() => voice.stopMode()} title="End voice conversation">end</button>
        </div>
      {/if}

      <div class="chat__composer">
        <Composer
          running={store?.running ?? false}
          stoppable={store?.working ?? false}
          interrupting={interrupting}
          disabled={!online}
          disabledReason={online ? "" : "daemon offline"}
          voiceModeOn={voice.modeOn}
          onsend={send}
          oninterrupt={interrupt}
          onvoicemode={online ? toggleVoiceMode : undefined}
          slashCommands={composerSlashCommands}
        />
      </div>
    </div>

    <aside
      class="chat__dock"
      class:chat__dock--collapsed={dockCollapsed}
      class:chat__dock--resizing={resizing}
      style={dockCollapsed ? "" : `width:${dockWidth}px`}
    >
      <!-- RESIZE GRIP — a hairline strip on the dock's left edge; drag to set the
           dock width (clamped). Hidden while collapsed. -->
      {#if !dockCollapsed}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="dock__grip"
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize tools panel"
          title="Drag to resize"
          onpointerdown={startResize}
        ></div>
      {/if}

      {#if dockCollapsed}
        <!-- COLLAPSED — a thin glyph rail. The toggle expands; a glyph expands to
             that tab. -->
        <div class="dock__rail">
          <button class="dock__railbtn" onclick={toggleDockCollapsed} title="Expand tools panel" aria-label="Expand tools panel">«</button>
          {#each dockTabs as t (t.id)}
            <button
              class="dock__railbtn"
              class:dock__railbtn--on={dockTab === t.id}
              title={t.label}
              aria-label={t.label}
              onclick={() => pickCollapsedTab(t.id)}
            >{t.glyph}</button>
          {/each}
        </div>
      {:else}
      <!-- TABS: the dock is a tools panel — Info (session groups) + Terminal /
           Diff / Files / Browser. A fixed strip over a flex-1 body so the
           height-filling panels (Terminal/Browser) get real space; the Info
           body scrolls, the tool bodies fill. -->
      <div class="dock__tabs" role="tablist" aria-label="Session tools">
        {#each dockTabs as t (t.id)}
          <button
            class="dock__tab"
            class:dock__tab--on={dockTab === t.id}
            role="tab"
            aria-selected={dockTab === t.id}
            onclick={() => setDockTab(t.id)}
          >{t.label}</button>
        {/each}
        <button class="dock__collapse" onclick={toggleDockCollapsed} title="Collapse tools panel" aria-label="Collapse tools panel">»</button>
      </div>
      {/if}

      {#if !dockCollapsed}
      {#if dockTab === "info"}
      <div class="dock__body dock__body--scroll">
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
          <div class="dock__sub tnum">{effectiveTokens.toLocaleString()} / {sess.maxTokens.toLocaleString()}</div>
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
                  <span class="roots__path" title={root}>{prettyPath(root)}</span>
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
      </div>
      {/if}

      <!-- TERMINAL: stays mounted once opened; hidden when another tab is active
           so the server PTY + shell session survive tab switches. -->
      {#if terminalOpened}
        <div
          class="dock__body dock__body--fill"
          class:dock__body--hidden={dockTab !== "terminal"}
        >
          {#await TerminalLazy() then { default: Terminal }}
            <Terminal active={dockTab === "terminal"} workdir={primaryRoot} />
          {/await}
        </div>
      {/if}

      <!-- DIFF: mount-when-active so no git subprocess runs until opened. -->
      {#if dockTab === "diff"}
        <div class="dock__body dock__body--fill">
          <DiffPanel dir={primaryRoot} />
        </div>
      {/if}

      <!-- FILES: mount-when-active so no filesystem read until opened. -->
      {#if dockTab === "files"}
        <div class="dock__body dock__body--fill">
          <FilesPanel dir={primaryRoot} />
        </div>
      {/if}

      <!-- BROWSER: stays MOUNTED once first opened (browserOpened) and is hidden
           rather than unmounted on tab switch, so navigation/page state survives
           switching away and back. -->
      {#if browserOpened}
        <div class="dock__body dock__body--fill" class:dock__body--hidden={dockTab !== "browser"}>
          <BrowserPanel />
        </div>
      {/if}
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
    padding: var(--sp-2) var(--sp-6);
    border-bottom: 1px solid var(--border-hairline);
    overflow-x: auto;
    /* scrollbar-width:none hides the bar natively; do NOT add a
       ::-webkit-scrollbar rule — that drops the row off WebKitGTK's async-scroll
       fast path (see base.css scroll-perf note). */
    scrollbar-width: none;
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
  /* ACTIVE PLAN panel — pinned above the transcript. */
  .plan {
    flex: none;
    margin: 0 auto;
    width: 100%;
    max-width: 820px;
    background: var(--bg-raised);
    border: 1px solid var(--border-hairline);
    border-left: 2px solid var(--brand);
    border-radius: var(--r-md);
    margin-bottom: var(--sp-3);
    overflow: hidden;
  }
  .plan__head {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    width: 100%;
    padding: var(--sp-3) var(--sp-4);
    border: none;
    background: transparent;
    cursor: pointer;
    text-align: left;
  }
  .plan__head:hover {
    background: var(--state-hover);
  }
  .plan__chev {
    width: 6px;
    height: 6px;
    border-right: 1.5px solid var(--text-muted);
    border-bottom: 1.5px solid var(--text-muted);
    transform: rotate(-45deg);
    transition: transform var(--dur-fast) var(--ease-out);
    flex: none;
  }
  .plan__chev--open {
    transform: rotate(45deg);
  }
  .plan__title {
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--brand-bright);
  }
  .plan__count {
    font-size: var(--fs-label);
    color: var(--text-faint);
  }
  .plan__current {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-label);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .plan__list {
    list-style: none;
    margin: 0;
    padding: 0 var(--sp-4) var(--sp-3) var(--sp-5);
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    /* Cap the expanded plan so a long (15-step) list can't claim a third of the
       viewport — scroll within instead of pushing the transcript down. */
    max-height: 32vh;
    overflow-y: auto;
  }
  .ptask {
    display: flex;
    gap: var(--sp-3);
    align-items: baseline;
    font-size: var(--fs-body-sm);
  }
  .ptask__glyph {
    flex: none;
    width: 14px;
    color: var(--text-muted);
  }
  .ptask__text {
    color: var(--text-secondary);
    line-height: var(--lh-snug);
  }
  .ptask--in_progress .ptask__glyph {
    color: var(--brand-bright);
  }
  .ptask--in_progress .ptask__text {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }
  .ptask--completed .ptask__glyph {
    color: var(--success);
  }
  .ptask--completed .ptask__text {
    color: var(--text-faint);
    text-decoration: line-through;
  }
  .ptask--cancelled .ptask__text {
    color: var(--text-ghost);
    text-decoration: line-through;
  }
  @media (prefers-reduced-motion: reduce) {
    .plan__chev {
      transition: none;
    }
  }

  .chat__scroll {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    position: relative;
    /* Clip the column: the windowed list (.chat__log) and the live block are
       stacked flex children; without clipping, a growing live block makes the
       column overflow and the list's absolutely-positioned rows render OVER the
       live area (the "streaming text tops over what's above, then snaps back on
       commit" bug). Clipping + min-height:0 on the list keeps each in its slot. */
    overflow: hidden;
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

     It must NOT be its own scroll island: when it had `max-height:42vh;
     overflow-y:auto` it became a separate scroller stacked on the windowed
     list, so a growing live block overlaid the history up to mid-viewport and
     only snapped into place when it committed into the list (the reported
     "covers the page, scrollable, then rearranges" bug). Instead it's a normal
     flow child capped to the column: `flex:none` so it never steals the list's
     space, `min-height:0` so the flex column can shrink the list (.chat__log)
     to make room, and the SHARED scroll lives on .chat__log's VirtualList. A
     long live reply scrolls with everything else and commits into the list the
     moment the turn ends. */
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
    word-break: break-word;
    overflow-wrap: anywhere;
  }
  .msg--note,
  .msg__reasoning-body--live {
    white-space: pre-wrap;
  }
  .msg--reasoning {
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    border-left: 2px solid var(--border-subtle);
    padding-left: var(--sp-5);
  }
  .msg__reasoning-body {
    margin-top: var(--sp-2);
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
  }
  .msg__reasoning-body :global(.md) {
    font-size: inherit;
    color: inherit;
    line-height: var(--lh-relaxed);
  }
  .msg__reasoning-body :global(.md-p) {
    margin: var(--sp-3) 0;
  }
  .msg__reasoning-body :global(.md-code) {
    font-size: var(--fs-code-sm);
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
  /* Read-aloud control on completed assistant prose. Anchored to the block's
     top-right, hidden until the row is hovered/focused (or actively speaking),
     so it never competes with the text at rest. */
  .msg--text {
    position: relative;
    white-space: normal;
  }
  /* The live row now renders Markdown (block elements), so the wrapper must NOT
     force pre-wrap — that would double the spacing the Markdown margins already
     give. It just carries the trailing caret inline after the prose. */
  .msg__live-prose {
    line-height: var(--lh-prose);
  }

  /* USER MESSAGES — visually distinct from assistant prose so the eye reads the
     transcript as a back-and-forth. A user turn sits in a quiet raised bubble,
     right-aligned to a comfortable measure, with a brand edge — the assistant's
     reply stays full-width plain prose. This is what makes a streamed reply read
     as a clear continuation of "your message → its answer". */
  .msg--user {
    display: block;
    margin-left: auto;
    max-width: 80%;
    width: fit-content;
    background: var(--bg-raised-2, var(--bg-inset));
    border: 1px solid var(--border-hairline);
    border-left: 2px solid var(--brand-dim, var(--brand));
    border-radius: var(--r-md, 10px);
    padding: var(--sp-3) var(--sp-4);
    color: var(--text-primary);
  }
  /* Collapse the user bubble's inner Markdown margins so a one-line message
     doesn't carry paragraph air inside the chip. */
  .msg--user :global(.md > :first-child) {
    margin-top: 0;
  }
  .msg--user :global(.md > :last-child) {
    margin-bottom: 0;
  }
  .msg--image-only {
    line-height: 0;
  }
  .msg__images {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(180px, 100%), 280px));
    gap: var(--sp-3);
    margin-top: var(--sp-4);
  }
  .msg__images:first-child {
    margin-top: 0;
  }
  .msg__image {
    display: block;
    width: 100%;
    max-height: 360px;
    object-fit: contain;
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    background: var(--bg-inset);
  }
  .msg__image-fallback {
    display: inline-flex;
    align-items: center;
    min-height: 40px;
    padding: 0 var(--sp-4);
    border: 1px solid var(--border-subtle);
    border-radius: var(--r-md);
    color: var(--text-muted);
    background: var(--bg-inset);
    font-size: var(--fs-body-sm);
    line-height: var(--lh-snug);
  }
  .msg__speak {
    position: absolute;
    top: 0;
    right: 0;
    width: 26px;
    height: 26px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 1px solid var(--border-hairline);
    border-radius: var(--r-sm);
    background: var(--bg-raised);
    color: var(--text-muted);
    font-size: var(--fs-body-sm);
    line-height: 1;
    cursor: pointer;
    opacity: 0;
    transition:
      opacity var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .chat__row:hover .msg__speak,
  .msg__speak:focus-visible,
  .msg__speak--on {
    opacity: 1;
  }
  .msg__speak:hover {
    color: var(--text-primary);
    background: var(--state-hover);
  }
  .msg__speak:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .msg__speak--on {
    color: var(--brand-bright);
    border-color: var(--border-brand-faint);
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
    animation: caret-blink 1.1s var(--ease-inout) infinite;
  }
  /* Soft eased pulse, not a hard steps() on/off blink. The caret sits right
     beside smoothly-arriving tokens during a turn; a hard terminal-cursor blink
     read as jittery against the rest of the system's soft motion. */
  @keyframes caret-blink {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.2;
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

  /* Voice-mode banner: a slim strip above the composer surfacing the live mic/
     speaker phase + the last transcript, so hands-free mode is legible. The dot
     breathes teal while the mic/speaker is active. */
  .chat__voicebar {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    margin-bottom: var(--sp-3);
    padding: var(--sp-3) var(--sp-4);
    border: 1px solid var(--border-brand-faint);
    border-radius: var(--r-md);
    background: var(--state-selected);
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
    color: var(--text-secondary);
  }
  .chat__voicebar-dot {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: var(--r-full);
    background: var(--text-faint);
  }
  .chat__voicebar-dot--live {
    background: var(--brand);
    animation: work-breathe var(--breath) var(--ease-inout) infinite;
    will-change: opacity;
  }
  .chat__voicebar-label {
    flex: none;
    color: var(--brand-bright);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
  }
  .chat__voicebar-heard {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-muted);
    font-style: italic;
  }
  .chat__voicebar-stop {
    flex: none;
    margin-left: auto;
    border: none;
    background: transparent;
    padding: 0;
    color: var(--text-muted);
    font: var(--fw-semibold) var(--fs-label) / 1 var(--font-sans);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    cursor: pointer;
    transition: color var(--dur-fast) var(--ease-out);
  }
  .chat__voicebar-stop:hover {
    color: var(--error);
  }
  .chat__voicebar-stop:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
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
  /* The dock is a tabbed tools panel: a fixed tab strip over a flex-1 body. The
     body is either the scrolling Info stack or a height-filling tool panel
     (Terminal/Browser manage their own internal scroll), so the aside itself no
     longer scrolls or pads — those move onto the Info body. Widened from 268 →
     340 so a terminal/diff has room while the chat column stays dominant. */
  .chat__dock {
    position: relative;
    width: 340px; /* default; overridden by an inline width when expanded */
    flex: none;
    border-left: 1px solid var(--border-hairline);
    background: var(--bg-well);
    display: flex;
    flex-direction: column;
    min-height: 0;
    overflow: hidden;
  }
  /* While dragging the grip, suppress text selection + the body's own pointer
     so the drag stays crisp. */
  .chat__dock--resizing {
    user-select: none;
  }
  .chat__dock--collapsed {
    width: 48px;
  }
  /* RESIZE GRIP — a 6px hit strip hugging the dock's left edge; a hairline
     brightens on hover/drag so the affordance reads without a heavy divider. */
  .dock__grip {
    position: absolute;
    top: 0;
    left: 0;
    width: 6px;
    height: 100%;
    cursor: col-resize;
    z-index: 5;
  }
  .dock__grip::after {
    content: "";
    position: absolute;
    top: 0;
    left: 0;
    width: 2px;
    height: 100%;
    background: transparent;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .dock__grip:hover::after,
  .chat__dock--resizing .dock__grip::after {
    background: var(--border-brand);
  }

  /* COLLAPSED RAIL — a thin vertical strip of tool glyphs; click one to expand
     the dock to that tab. The active tab's glyph is lit. */
  .dock__rail {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-3) 0;
  }
  .dock__railbtn {
    width: 34px;
    height: 34px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 1px solid transparent;
    border-radius: var(--r-sm);
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-body);
    line-height: 1;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .dock__railbtn:hover {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .dock__railbtn:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .dock__railbtn--on {
    background: var(--state-selected);
    color: var(--brand-bright);
  }
  /* The expanded-state collapse button rides the tab strip's right edge. */
  .dock__collapse {
    margin-left: auto;
    flex: none;
    width: 26px;
    height: 26px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    border-radius: var(--r-sm);
    background: transparent;
    color: var(--text-faint);
    font-size: var(--fs-body);
    line-height: 1;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .dock__collapse:hover {
    background: var(--state-hover);
    color: var(--text-primary);
  }
  .dock__collapse:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .dock__tabs {
    flex: none;
    display: flex;
    gap: var(--sp-1);
    padding: var(--sp-3) var(--sp-4);
    border-bottom: 1px solid var(--border-hairline);
  }
  .dock__tab {
    flex: 1;
    min-width: 0;
    height: 26px;
    padding: 0 var(--sp-2);
    border: 1px solid transparent;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--r-sm);
    font: var(--fw-medium) var(--fs-label) / 1 var(--font-sans);
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .dock__tab:hover {
    color: var(--text-primary);
    background: var(--state-hover);
  }
  .dock__tab:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .dock__tab--on {
    border-color: var(--border-brand-faint);
    background: var(--state-selected);
    color: var(--brand-bright);
  }
  /* The active tab's body. Info scrolls its stacked groups; the tool panels
     fill the height and own their internal scroll. */
  .dock__body {
    flex: 1;
    min-height: 0;
  }
  .dock__body--scroll {
    overflow-y: auto;
    padding: var(--sp-7) var(--sp-6);
    display: flex;
    flex-direction: column;
    gap: var(--sp-7);
  }
  .dock__body--fill {
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  /* Browser stays mounted across tab switches (page state survives) but is
     visually removed when another tab is active. display:none rather than
     unmount is the whole point — the page/navigation isn't torn down. */
  .dock__body--hidden {
    display: none;
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

  /* ── new-chat popover ──────────────────────────────────────────────────── */
  /* The inline working-directory prompt anchored under the New-chat button. */
  .newchat {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .newchat__label {
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: var(--ls-eyebrow);
    color: var(--text-faint);
  }
  /* Recent-dir quick-pick: a compact list of clickable rows, each a project
     name over a dimmed short path. The selected row carries the brand tint. */
  .newchat__recents {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    max-height: 168px;
    overflow-y: auto;
  }
  .newchat__recent {
    display: flex;
    flex-direction: column;
    gap: 1px;
    width: 100%;
    text-align: left;
    padding: var(--sp-2) var(--sp-3);
    border: 1px solid transparent;
    background: transparent;
    border-radius: var(--r-sm);
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .newchat__recent:hover {
    background: var(--state-hover);
  }
  .newchat__recent:focus-visible {
    outline: none;
    box-shadow: var(--shadow-focus);
  }
  .newchat__recent--on {
    border-color: var(--border-brand-faint);
    background: var(--state-selected);
  }
  .newchat__recent-name {
    font: var(--fw-semibold) var(--fs-body-sm) / 1.2 var(--font-sans);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .newchat__recent-dir {
    font-size: var(--fs-micro);
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .newchat__actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-3);
  }

  @media (prefers-reduced-motion: reduce) {
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
    .chat__voicebar-dot--live {
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
