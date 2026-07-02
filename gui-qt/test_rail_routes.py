#!/usr/bin/env python3
"""
test_rail_routes.py — Navigate through all 5 rail routes and screenshot each.
Programmatically sets currentRoute via QML property binding and captures screenshots.
"""
import sys
import os
from pathlib import Path
from PySide6.QtCore import QTimer, QObject, Slot, Property, Signal
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine
from PySide6.QtQuickControls2 import QQuickStyle

# Ensure screenshots dir exists
SCREENSHOTS_DIR = Path(__file__).parent / "screenshots"
SCREENSHOTS_DIR.mkdir(exist_ok=True)

# Routes to test
ROUTES = ["home", "sessions", "live", "chat", "tasks"]

class TestController(QObject):
    """Controller to drive route navigation and screenshot capture."""

    def __init__(self, engine, parent=None):
        super().__init__(parent)
        self._engine = engine
        self._route_index = 0
        self._root_object = None

        # Timer to navigate through routes
        self._timer = QTimer(self)
        self._timer.timeout.connect(self._next_route)

    def start(self):
        """Start the test sequence."""
        self._root_object = self._engine.rootObjects()[0]
        if not self._root_object:
            print("ERROR: No root object found")
            QGuiApplication.quit()
            return

        print("Starting rail routing test...")
        # Start with a slight delay to let UI settle
        QTimer.singleShot(500, self._next_route)

    @Slot()
    def _next_route(self):
        """Navigate to the next route and screenshot."""
        if self._route_index >= len(ROUTES):
            print("Test complete. Exiting.")
            QGuiApplication.quit()
            return

        route = ROUTES[self._route_index]
        print(f"Navigating to route: {route}")

        # Set the route property on the root QML object
        self._root_object.setProperty("currentRoute", route)

        # Wait a bit for the UI to render, then screenshot
        QTimer.singleShot(300, lambda: self._screenshot(route))

    def _screenshot(self, route):
        """Capture screenshot of the current route."""
        screenshot_path = SCREENSHOTS_DIR / f"rail-{route}.png"
        window = self._root_object.window() if hasattr(self._root_object, 'window') else self._root_object

        if window and hasattr(window, 'grabWindow'):
            pixmap = window.grabWindow()
            pixmap.save(str(screenshot_path))
            print(f"  Screenshot saved: {screenshot_path}")
        else:
            print(f"  ERROR: Could not grab window for route {route}")

        # Move to next route
        self._route_index += 1
        QTimer.singleShot(200, self._next_route)


def main():
    QQuickStyle.setStyle("Basic")

    app = QGuiApplication(sys.argv)
    app.setOrganizationName("eigen")
    app.setApplicationName("eigen-test")

    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()

    # Create minimal mock models (empty models to satisfy QML dependencies)
    # In a real test we'd use the full AppContext, but for route navigation testing
    # we just need the views to load without crashing

    # Import models
    from eigenqt.models import (
        SessionsModel, LiveSessionsModel, TasksModel,
        DashboardModel, FeedModel, SessionStateModel, CommandsModel
    )
    from eigenqt.rpc import RpcClient
    from eigenqt.clipboard_helper import ClipboardHelper
    from eigenqt.highlighter_helper import HighlighterHelper

    # Create RPC client (will auto-connect to guiserver)
    rpc_client = RpcClient()

    # Create models
    sessions_model = SessionsModel(rpc_client)
    live_sessions_model = LiveSessionsModel(rpc_client)
    tasks_model = TasksModel(rpc_client)
    dashboard_model = DashboardModel(rpc_client)
    feed_model = FeedModel(rpc_client)

    # Minimal session controller (for ChatView)
    from eigenqt.models import TranscriptModel, ApprovalsModel
    class MinimalSessionController(QObject):
        sessionIdChanged = Signal()
        sessionStateModelChanged = Signal()
        commandsModelChanged = Signal()

        def __init__(self, client, parent=None):
            super().__init__(parent)
            self._session_id = ""
            self._session_state_model = SessionStateModel(client, "")
            self._commands_model = CommandsModel(client)

        @Property(str, notify=sessionIdChanged)
        def session_id(self):
            return self._session_id

        @Property(QObject, notify=sessionStateModelChanged)
        def session_state_model(self):
            return self._session_state_model

        @Property(QObject, notify=commandsModelChanged)
        def commands_model(self):
            return self._commands_model

        @Slot(str)
        def open_session(self, session_id):
            self._session_id = session_id
            self.sessionIdChanged.emit()

        @Slot()
        def detach(self):
            self._session_id = ""
            self.sessionIdChanged.emit()

    session_controller = MinimalSessionController(rpc_client)

    # Helpers
    clipboard_helper = ClipboardHelper()
    highlighter_helper = HighlighterHelper()

    # Expose to QML
    ctx.setContextProperty("rpcClient", rpc_client)
    ctx.setContextProperty("sessionsModel", sessions_model)
    ctx.setContextProperty("liveSessionsModel", live_sessions_model)
    ctx.setContextProperty("tasksModel", tasks_model)
    ctx.setContextProperty("dashboardModel", dashboard_model)
    ctx.setContextProperty("feedModel", feed_model)
    ctx.setContextProperty("sessionController", session_controller)
    ctx.setContextProperty("daemonOnline", False)
    ctx.setContextProperty("guiserverSha", "test-sha")
    ctx.setContextProperty("statsData", {})
    ctx.setContextProperty("clipboardHelper", clipboard_helper)
    ctx.setContextProperty("highlighter", highlighter_helper)

    # Mock transcript and approvals models for chat view
    transcript_model = TranscriptModel(rpc_client, "")
    approvals_model = ApprovalsModel(rpc_client, "")
    ctx.setContextProperty("transcriptModel", transcript_model)
    ctx.setContextProperty("approvalsModel", approvals_model)

    # Load Main.qml
    ROOT = Path(__file__).parent
    engine.addImportPath(str(ROOT / "eigenqt"))
    engine.load(str(ROOT / "eigenqt" / "qml" / "Main.qml"))

    if not engine.rootObjects():
        print("ERROR: Failed to load QML")
        sys.exit(-1)

    # Start test controller
    test_controller = TestController(engine)
    QTimer.singleShot(100, test_controller.start)

    sys.exit(app.exec())


if __name__ == "__main__":
    main()
