#!/usr/bin/env python3
"""
Test script to launch GUI, navigate to Live view, and take a screenshot.
"""
import sys
import time
from pathlib import Path

from PySide6.QtCore import QTimer
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine, qmlRegisterType
from PySide6.QtQuickControls2 import QQuickStyle

from eigenqt.models.worktree import DiffModel, FileTreeModel
from eigenqt.models import (
    ApprovalsModel,
    CommandsModel,
    LiveSessionsModel,
    ReplyWatcher,
    SessionsModel,
    SessionStateModel,
    TasksModel,
    TranscriptModel,
)
from eigenqt.rpc import RpcClient
from eigenqt.clipboard_helper import ClipboardHelper
from eigenqt.highlighter_helper import HighlighterHelper

ROOT = Path(__file__).resolve().parent


def main():
    import os
    os.environ["QT_QPA_PLATFORM"] = "offscreen"

    QQuickStyle.setStyle("Basic")
    app = QGuiApplication(sys.argv)
    app.setOrganizationName("eigen")
    app.setApplicationName("eigen-test")

    qmlRegisterType(DiffModel, "Eigen", 1, 0, "DiffModel")
    qmlRegisterType(FileTreeModel, "Eigen", 1, 0, "FileTreeModel")

    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()

    # Create minimal context (no AppContext wrapper for simplicity)
    rpc_client = RpcClient(parent=app)
    sessions_model = SessionsModel(rpc_client, app)
    live_sessions_model = LiveSessionsModel(rpc_client, app)

    # Minimal session controller stub
    class SessionController:
        def __init__(self):
            self.session_id = ""
            self.transcript_model = TranscriptModel(rpc_client, "", app)
            self.approvals_model = ApprovalsModel(rpc_client, "", app)
            self.session_state_model = SessionStateModel(rpc_client, "", app)
            self.commands_model = CommandsModel(rpc_client, app)

    session_controller = SessionController()
    clipboard_helper = ClipboardHelper(app)
    highlighter_helper = HighlighterHelper(app)

    ctx.setContextProperty("rpcClient", rpc_client)
    ctx.setContextProperty("sessionsModel", sessions_model)
    ctx.setContextProperty("liveSessionsModel", live_sessions_model)
    ctx.setContextProperty("sessionController", session_controller)
    ctx.setContextProperty("transcriptModel", session_controller.transcript_model)
    ctx.setContextProperty("approvalsModel", session_controller.approvals_model)
    ctx.setContextProperty("daemonOnline", True)
    ctx.setContextProperty("guiserverSha", "test")
    ctx.setContextProperty("clipboardHelper", clipboard_helper)
    ctx.setContextProperty("highlighter", highlighter_helper)

    engine.addImportPath(str(ROOT / "eigenqt"))
    engine.load(str(ROOT / "eigenqt" / "qml" / "Main.qml"))

    if not engine.rootObjects():
        print("ERROR: Failed to load QML")
        return 1

    root = engine.rootObjects()[0]

    def take_screenshot_and_quit():
        try:
            # Navigate to Live view (index 1)
            root.setProperty("currentIndex", 1)

            # Wait a moment for render
            time.sleep(0.5)

            # Take screenshot
            from PySide6.QtQuick import QQuickWindow
            window = root
            if isinstance(window, QQuickWindow):
                pixmap = window.grabWindow()
                screenshot_path = ROOT / "screenshots" / "live-view.png"
                screenshot_path.parent.mkdir(exist_ok=True)
                pixmap.save(str(screenshot_path))
                print(f"Screenshot saved to {screenshot_path}")
            else:
                print("ERROR: Root object is not a QQuickWindow")
        except Exception as e:
            print(f"ERROR taking screenshot: {e}")
            import traceback
            traceback.print_exc()
        finally:
            app.quit()

    # Take screenshot after a delay
    QTimer.singleShot(2000, take_screenshot_and_quit)

    return app.exec()


if __name__ == "__main__":
    sys.exit(main())
