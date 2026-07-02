#!/usr/bin/env python3
"""
main.py — Eigen Qt/QML GUI entry point.

Launches guiserver supervisor, exposes RpcClient + models to QML, loads Main.qml.
Accepts --session <id> flag to open a session directly.
"""
import sys
from pathlib import Path

from PySide6.QtCore import Property, QObject, QTimer, Signal, Slot
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine
from PySide6.QtQuickControls2 import QQuickStyle

from eigenqt.models import (
    ApprovalsModel,
    CommandsModel,
    ReplyWatcher,
    SessionsModel,
    SessionStateModel,
    TranscriptModel,
)
from eigenqt.rpc import GuiserverSupervisor, RpcClient
from eigenqt.clipboard_helper import ClipboardHelper
from eigenqt.highlighter_helper import HighlighterHelper

ROOT = Path(__file__).resolve().parent


class AppContext(QObject):
    """Main application context exposed to QML (RpcClient + status signals)."""

    daemonOnlineChanged = Signal()
    guiserverShaChanged = Signal()

    def __init__(self, parent=None):
        super().__init__(parent)
        self._daemon_online = False
        self._guiserver_sha = ""

        # RPC client (auto-connects on init)
        self.rpc_client = RpcClient(parent=self)

        # Models
        self.sessions_model = SessionsModel(self.rpc_client, self)

        # ReplyWatcher for background session notifications
        self.reply_watcher = ReplyWatcher(
            self.rpc_client, self.sessions_model, self
        )

        # Connect reply watcher unread signal to sessions model
        self.reply_watcher.unread.connect(self.sessions_model.mark_unread)

        # Connect signals
        self.rpc_client.connected.connect(self._on_connected)
        self.rpc_client.event.connect(self._on_event)

        # Fetch hello once connected
        QTimer.singleShot(500, self._fetch_hello)

    def _on_connected(self):
        """Handle RPC connected signal."""
        print("RPC connected")
        # Subscribe to daemon stats for online/offline tracking
        self.rpc_client.subscribe(["eigen:daemon:stats"])

    def _on_event(self, channel: str, data: dict):
        """Handle RPC event signal."""
        if channel == "eigen:daemon:stats":
            # Update daemon online status
            was_online = self._daemon_online
            self._daemon_online = data.get("online", False)
            if was_online != self._daemon_online:
                self.daemonOnlineChanged.emit()

    def _fetch_hello(self):
        """Fetch hello from guiserver."""
        self.rpc_client.call("hello", callback=self._on_hello)

    def _on_hello(self, result):
        """Handle hello response."""
        if "result" in result:
            self._guiserver_sha = result["result"].get("sha", "unknown")
            self.guiserverShaChanged.emit()
            print(f"guiserver ready: sha={self._guiserver_sha[:8]}")
        else:
            self._guiserver_sha = "error"
            self.guiserverShaChanged.emit()

    @Property(bool, notify=daemonOnlineChanged)
    def daemonOnline(self):
        """Daemon online status."""
        return self._daemon_online

    @Property(str, notify=guiserverShaChanged)
    def guiserverSha(self):
        """Guiserver SHA."""
        return self._guiserver_sha


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
        self._client.call("State", args=[session_id], callback=self._on_state)

    def _on_state(self, result):
        """Handle State RPC result."""
        if "result" in result:
            self.transcript_model.seed(result["result"])
            self.approvals_model.seed(result["result"])
            self._session_state_model.seed(result["result"])

    @Slot()
    def detach(self):
        """Detach from current session."""
        if self._session_id:
            channel = f"session:{self._session_id}"
            self._client.unsubscribe([channel])

            # Clear models
            self.transcript_model._rows.clear()
            self.transcript_model.beginResetModel()
            self.transcript_model.endResetModel()

            self.approvals_model._approvals.clear()
            self.approvals_model.beginResetModel()
            self.approvals_model.endResetModel()

            self._session_id = ""
            self.sessionIdChanged.emit()


def main():
    QQuickStyle.setStyle("Basic")  # Unstyled base for custom theme

    app = QGuiApplication(sys.argv)
    app.setOrganizationName("eigen")
    app.setApplicationName("eigen")

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

    # Expose to QML
    ctx.setContextProperty("rpcClient", app_context.rpc_client)
    ctx.setContextProperty("sessionsModel", app_context.sessions_model)
    ctx.setContextProperty("sessionController", session_controller)
    ctx.setContextProperty("transcriptModel", session_controller.transcript_model)
    ctx.setContextProperty("approvalsModel", session_controller.approvals_model)
    ctx.setContextProperty("daemonOnline", app_context.daemonOnline)
    ctx.setContextProperty("guiserverSha", app_context.guiserverSha)
    ctx.setContextProperty("clipboardHelper", clipboard_helper)
    ctx.setContextProperty("highlighter", highlighter_helper)

    # Bind property changes
    app_context.daemonOnlineChanged.connect(lambda: ctx.setContextProperty("daemonOnline", app_context.daemonOnline))
    app_context.guiserverShaChanged.connect(lambda: ctx.setContextProperty("guiserverSha", app_context.guiserverSha))

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
