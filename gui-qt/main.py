#!/usr/bin/env python3
"""
main.py — Eigen Qt/QML GUI entry point.

Launches guiserver supervisor, exposes RpcClient + models to QML, loads Main.qml.
Accepts --session <id> flag to open a session directly.
"""
import json
import sys
from pathlib import Path

from PySide6.QtCore import Property, QObject, QTimer, Signal, Slot
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine, qmlRegisterType
from PySide6.QtQuickControls2 import QQuickStyle

from eigenqt.models.worktree import DiffModel, FileTreeModel
from eigenqt.models import (
    ApprovalsModel,
    BoardModel,
    CommandsModel,
    CronsModel,
    DashboardModel,
    DreamingModel,
    FeedModel,
    KanbanModel,
    LiveSessionsModel,
    MemoryModel,
    MachinesModel,
    ObserveModel,
    PluginsModel,
    ProfileModel,
    ProposalsModel,
    ReplyWatcher,
    RoutingModel,
    SessionsModel,
    SessionStateModel,
    SkillsModel,
    TasksModel,
    TranscriptModel,
)
from eigenqt.models.connectors import ConnectorsModel
from eigenqt.models.notes import NotesController
from eigenqt.models.config import ConfigModel, RuleChainsModel
from eigenqt.models.reviewers import ReviewersModel
from eigenqt.rpc import GuiserverSupervisor, RpcClient
from eigenqt.clipboard_helper import ClipboardHelper
from eigenqt.highlighter_helper import HighlighterHelper
from eigenqt.markdown_helper import MarkdownHelper
from eigenqt.terminal_helper import TerminalHelper
from eigenqt.webengine import initialize_webengine

ROOT = Path(__file__).resolve().parent
QT_THEME_NAMES = {"deepteal", "nord", "gruvbox"}


def resolve_qt_theme(config_path: Path | None = None) -> str:
    """Read the startup-only Qt palette without requiring the daemon first."""
    path = config_path or (Path.home() / ".eigen" / "config.json")
    try:
        config = json.loads(path.read_text())
        value = config.get("theme", "") if isinstance(config, dict) else ""
    except (OSError, ValueError, TypeError):
        return "deepteal"
    value = str(value).strip().lower()
    return value if value in QT_THEME_NAMES else "deepteal"


def qt_theme_argument(theme: str) -> str:
    return f"--eigen-qt-theme={theme if theme in QT_THEME_NAMES else 'deepteal'}"


def with_qt_theme_argument(argv: list[str], config_path: Path | None = None) -> list[str]:
    """Add the resolved palette argument unless a caller already supplied one."""
    if any(arg.startswith("--eigen-qt-theme=") for arg in argv):
        return list(argv)
    return [*argv, qt_theme_argument(resolve_qt_theme(config_path))]


class AppContext(QObject):
    """Main application context exposed to QML (RpcClient + status signals)."""

    daemonOnlineChanged = Signal()
    guiserverShaChanged = Signal()
    statsChanged = Signal()

    def __init__(self, parent=None):
        super().__init__(parent)
        self._daemon_online = False
        self._guiserver_sha = "starting"
        self._stats = {}

        self.guiserver = GuiserverSupervisor(parent=self)
        self._ensure_guiserver()

        # RPC client (auto-connects on init)
        self.rpc_client = RpcClient(parent=self)

        # Models
        self.sessions_model = SessionsModel(self.rpc_client, self)
        self.live_sessions_model = LiveSessionsModel(self.rpc_client, self)
        self.tasks_model = TasksModel(self.rpc_client, self)
        self.dashboard_model = DashboardModel(self.rpc_client, self)
        self.feed_model = FeedModel(self.rpc_client, self)
        self.board_model = BoardModel(self.rpc_client, self)
        self.kanban_model = KanbanModel(self.rpc_client, self)
        self.skills_model = SkillsModel(self.rpc_client, self)
        self.proposals_model = ProposalsModel(self.rpc_client, self)
        self.memory_model = MemoryModel(self.rpc_client, self)
        self.dreaming_model = DreamingModel(self.rpc_client, self)
        self.notes_controller = NotesController(self.rpc_client, self)
        self.connectors_model = ConnectorsModel(self.rpc_client, self)
        self.observe_model = ObserveModel(self.rpc_client, self)
        self.routing_model = RoutingModel(self.rpc_client, self)
        self.machines_model = MachinesModel(self.rpc_client, self)
        self.crons_model = CronsModel(self.rpc_client, self)
        self.plugins_model = PluginsModel(self.rpc_client, self)
        self.profile_model = ProfileModel(self.rpc_client, self)
        self.config_model = ConfigModel(self.rpc_client, self)
        self.rule_chains_model = RuleChainsModel(self.rpc_client, self)
        self.reviewers_model = ReviewersModel(self.rpc_client, self)

        # ReplyWatcher for background session notifications
        self.reply_watcher = ReplyWatcher(
            self.rpc_client, self.sessions_model, self
        )

        # Connect reply watcher unread/read signals to sessions model
        self.reply_watcher.unread.connect(self.sessions_model.mark_unread)
        self.reply_watcher.read.connect(self.sessions_model.mark_read)

        # Connect signals
        self.rpc_client.connected.connect(self._on_connected)
        self.rpc_client.event.connect(self._on_event)

        # Fetch hello once connected
        QTimer.singleShot(500, self._fetch_hello)

        # Poll Stats every 5s for the stats strip
        self._stats_timer = QTimer(self)
        self._stats_timer.setInterval(5000)
        self._stats_timer.timeout.connect(self._fetch_stats)
        self._stats_timer.start()

    def _on_connected(self):
        """Handle RPC connected signal."""
        print("RPC connected")
        self.rpc_client.subscribe(
            ["eigen:daemon:stats", "eigen:daemon:health"]
        )
        self._fetch_hello()
        self._fetch_stats()

    def _on_event(self, channel: str, data: dict):
        """Handle RPC event signal."""
        if channel == "eigen:daemon:stats":
            if isinstance(data, dict):
                self._stats = data
                self.statsChanged.emit()
            self._set_daemon_online(True)
        elif channel == "eigen:daemon:health":
            self._set_daemon_online(bool(data.get("ok", False)))

    def _ensure_guiserver(self):
        """Start or attach to the guiserver before the RPC workers connect."""
        try:
            hello = self.guiserver.ensure_running(timeout=10.0)
        except Exception as exc:
            print(f"guiserver supervisor failed: {exc}", file=sys.stderr)
            self._set_guiserver_sha("error")
            return

        self._set_guiserver_sha(hello.get("sha") or "unknown")

    def _fetch_hello(self):
        """Fetch hello from guiserver."""
        self.rpc_client.call("hello", callback=self._on_hello)

    def _on_hello(self, result):
        """Handle hello response."""
        if result.get("error") == "not connected":
            QTimer.singleShot(500, self._fetch_hello)
            return

        payload = result.get("result")
        if isinstance(payload, dict):
            self._set_guiserver_sha(payload.get("sha") or "unknown")
            print(f"guiserver ready: sha={self._guiserver_sha[:8]}")
            return

        self._set_guiserver_sha("error")

    def _fetch_stats(self):
        """Fetch daemon Stats."""
        self.rpc_client.call("Stats", callback=self._on_stats)

    def _on_stats(self, result):
        """Handle Stats RPC result. Guard None: Stats returns null while the
        daemon connection is coming up — assigning None to the dict-typed
        context property spams _pythonToCppCopy warnings."""
        payload = result.get("result")
        if isinstance(payload, dict):
            self._stats = payload
            self.statsChanged.emit()
            self._set_daemon_online(True)

    def _set_daemon_online(self, online: bool):
        if self._daemon_online == online:
            return
        self._daemon_online = online
        self.daemonOnlineChanged.emit()

    def _set_guiserver_sha(self, sha: str):
        if self._guiserver_sha == sha:
            return
        self._guiserver_sha = sha
        self.guiserverShaChanged.emit()

    @Property(bool, notify=daemonOnlineChanged)
    def daemonOnline(self):
        """Daemon online status."""
        return self._daemon_online

    @Property(str, notify=guiserverShaChanged)
    def guiserverSha(self):
        """Guiserver SHA."""
        return self._guiserver_sha

    @Property("QVariantMap", notify=statsChanged)
    def stats(self):
        """Daemon stats (for home stats strip)."""
        return self._stats


class SessionController(QObject):
    """Controller for managing session state + models."""

    sessionIdChanged = Signal()
    sessionStateModelChanged = Signal()
    commandsModelChanged = Signal()

    def __init__(
        self, client: RpcClient, reply_watcher: ReplyWatcher, parent=None
    ):
        super().__init__(parent)
        self._client = client
        self._reply_watcher = reply_watcher
        self._session_id = ""
        self.transcript_model = TranscriptModel(client, "", parent)
        self.approvals_model = ApprovalsModel(client, "", parent)
        self._session_state_model = SessionStateModel(client, "", parent)
        self._commands_model = CommandsModel(client, parent)
        self._state_seq = 0
        connected = getattr(self._client, "connected", None)
        if connected is not None:
            connected.connect(self._on_connected)

    @Property(str, notify=sessionIdChanged)
    def session_id(self):
        """Get current session ID."""
        return self._session_id

    @Property(QObject, notify=sessionStateModelChanged)
    def session_state_model(self):
        """Get session state model."""
        return self._session_state_model

    @Property(QObject, notify=commandsModelChanged)
    def commands_model(self):
        """Get commands model."""
        return self._commands_model

    @Slot(str)
    def open_session(self, session_id: str):
        """Open a session (detach previous, attach new)."""
        if self._session_id:
            self.detach()

        self._session_id = session_id
        self.sessionIdChanged.emit()

        # Notify reply watcher (clears unread for this session)
        self._reply_watcher.set_current_session(session_id)

        # Update models
        self.transcript_model._session_id = session_id
        self.approvals_model._session_id = session_id
        self._session_state_model._session_id = session_id

        # Subscribe to events
        channel = f"session:{session_id}"
        self.transcript_model._event_channel = channel
        self.approvals_model._event_channel = channel
        self._client.subscribe([channel])

        # Fetch initial state
        self._fetch_state()

    @Slot()
    def _on_connected(self):
        """Refetch the open chat after a startup/reconnect socket race."""
        if not self._session_id:
            return
        self._fetch_state()

    def _fetch_state(self):
        """Fetch the current session snapshot if a chat is open."""
        if not self._session_id:
            return
        self._state_seq += 1
        controller_seq = self._state_seq
        state_seq = self._session_state_model.beginStateRequest()
        self._client.call(
            "State",
            self._session_id,
            callback=lambda result, seq=controller_seq, state_seq=state_seq: self._on_state(
                result,
                seq,
                state_seq,
            ),
        )

    def _on_state(self, result, controller_seq=None, state_seq=None):
        """Handle State RPC result."""
        if controller_seq is not None and controller_seq != self._state_seq:
            return
        if "result" in result:
            self.transcript_model.seed(result["result"])
            self.approvals_model.seed(result["result"])
            self._session_state_model.applyState(result["result"], state_seq)

    @Slot()
    def detach(self):
        """Detach from current session."""
        if self._session_id:
            self._state_seq += 1
            channel = f"session:{self._session_id}"
            self._client.unsubscribe([channel])

            # Clear the old snapshot before attaching the next session.  Using
            # the model APIs also resets transient activity/approval state, so
            # a newly-created chat never inherits a stale running tool card.
            self.transcript_model.clearRows()
            self.approvals_model.clearRows()
            self._session_state_model.seed({})

            self.transcript_model._session_id = ""
            self.transcript_model._event_channel = ""
            self.approvals_model._session_id = ""
            self.approvals_model._event_channel = ""
            self._session_state_model._session_id = ""

            self._session_id = ""
            self.sessionIdChanged.emit()


def main():
    initialize_webengine()
    QQuickStyle.setStyle("Basic")  # Unstyled base for custom theme

    # Theme.js reads this before QML is constructed. The config contract is
    # startup-only, matching the TUI and legacy GUI behavior.
    sys.argv = with_qt_theme_argument(sys.argv)

    app = QGuiApplication(sys.argv)
    app.setOrganizationName("eigen")
    app.setApplicationName("eigen")

    # QML-instantiated model types (DiffTab/FilesTab declare DiffModel{} /
    # FileTreeModel{} inline) — registered under the Eigen module.
    qmlRegisterType(DiffModel, "Eigen", 1, 0, "DiffModel")
    qmlRegisterType(FileTreeModel, "Eigen", 1, 0, "FileTreeModel")

    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()

    # Create app context
    app_context = AppContext()

    # Create session controller (pass reply_watcher)
    session_controller = SessionController(
        app_context.rpc_client, app_context.reply_watcher, app
    )

    # Create helpers
    clipboard_helper = ClipboardHelper(app)
    highlighter_helper = HighlighterHelper(app)
    markdown_helper = MarkdownHelper(app)
    terminal_helper = TerminalHelper(app)

    # Expose to QML
    ctx.setContextProperty("rpcClient", app_context.rpc_client)
    ctx.setContextProperty("client", app_context.rpc_client)  # alias for SkillsView
    ctx.setContextProperty("sessionsModel", app_context.sessions_model)
    ctx.setContextProperty("liveSessionsModel", app_context.live_sessions_model)
    ctx.setContextProperty("tasksModel", app_context.tasks_model)
    ctx.setContextProperty("dashboardModel", app_context.dashboard_model)
    ctx.setContextProperty("feedModel", app_context.feed_model)
    ctx.setContextProperty("boardModel", app_context.board_model)
    ctx.setContextProperty("kanbanModel", app_context.kanban_model)
    ctx.setContextProperty("skillsModel", app_context.skills_model)
    ctx.setContextProperty("proposalsModel", app_context.proposals_model)
    ctx.setContextProperty("memoryModel", app_context.memory_model)
    ctx.setContextProperty("dreamingModel", app_context.dreaming_model)
    ctx.setContextProperty("notesController", app_context.notes_controller)
    ctx.setContextProperty("connectorsModel", app_context.connectors_model)
    ctx.setContextProperty("observeModel", app_context.observe_model)
    ctx.setContextProperty("routingModel", app_context.routing_model)
    ctx.setContextProperty("machinesModel", app_context.machines_model)
    ctx.setContextProperty("cronsModel", app_context.crons_model)
    ctx.setContextProperty("pluginsModel", app_context.plugins_model)
    ctx.setContextProperty("profileModel", app_context.profile_model)
    ctx.setContextProperty("configModel", app_context.config_model)
    ctx.setContextProperty("ruleChainsModel", app_context.rule_chains_model)
    ctx.setContextProperty("reviewersModel", app_context.reviewers_model)
    ctx.setContextProperty("sessionController", session_controller)
    ctx.setContextProperty("transcriptModel", session_controller.transcript_model)
    ctx.setContextProperty("approvalsModel", session_controller.approvals_model)
    ctx.setContextProperty("daemonOnline", app_context.daemonOnline)
    ctx.setContextProperty("guiserverSha", app_context.guiserverSha)
    ctx.setContextProperty("statsData", app_context.stats)
    ctx.setContextProperty("clipboardHelper", clipboard_helper)
    ctx.setContextProperty("highlighter", highlighter_helper)
    ctx.setContextProperty("markdownParser", markdown_helper)
    ctx.setContextProperty("terminalHelper", terminal_helper)

    # Bind property changes
    app_context.daemonOnlineChanged.connect(lambda: ctx.setContextProperty("daemonOnline", app_context.daemonOnline))
    app_context.guiserverShaChanged.connect(lambda: ctx.setContextProperty("guiserverSha", app_context.guiserverSha))
    app_context.statsChanged.connect(lambda: ctx.setContextProperty("statsData", app_context.stats))

    # Parse CLI args for --session <id>
    session_arg = None
    for i, arg in enumerate(sys.argv[1:], start=1):
        if arg == "--session" and i < len(sys.argv) - 1:
            session_arg = sys.argv[i + 1]
            break

    if session_arg:
        # Open session directly
        print(f"Opening session: {session_arg}")
        # (Would need to defer until QML loads and expose a slot to trigger)
        # For now, manual navigation in QML after load

    # Load QML
    engine.addImportPath(str(ROOT / "eigenqt"))
    engine.load(str(ROOT / "eigenqt" / "qml" / "Main.qml"))

    if not engine.rootObjects():
        sys.exit(-1)

    sys.exit(app.exec())


if __name__ == "__main__":
    main()
