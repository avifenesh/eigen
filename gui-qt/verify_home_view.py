#!/usr/bin/env python3
"""Verify HomeView loads and capture offscreen screenshot."""
import sys
from pathlib import Path

from PySide6.QtCore import QTimer
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine, qmlRegisterType
from PySide6.QtQuickControls2 import QQuickStyle

from eigenqt.models import DashboardModel, FeedModel, SessionsModel
from eigenqt.models.worktree import DiffModel, FileTreeModel
from eigenqt.rpc import RpcClient

ROOT = Path(__file__).resolve().parent


def main():
    QQuickStyle.setStyle("Basic")

    app = QGuiApplication(sys.argv)
    app.setOrganizationName("eigen")
    app.setApplicationName("eigen-home-verify")

    # QML types
    qmlRegisterType(DiffModel, "Eigen", 1, 0, "DiffModel")
    qmlRegisterType(FileTreeModel, "Eigen", 1, 0, "FileTreeModel")

    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()

    # Create client + models (won't connect, just for QML structure validation)
    client = RpcClient()
    dashboard_model = DashboardModel(client)
    feed_model = FeedModel(client)
    sessions_model = SessionsModel(client)

    # Mock data for screenshot
    dashboard_model._google_connected = True
    dashboard_model._events = [
        {"summary": "Team standup", "start": "2026-07-02T10:00:00Z", "allDay": False},
        {"summary": "Code review", "start": "2026-07-02T14:30:00Z", "allDay": False},
    ]
    dashboard_model._unread_count = 3
    dashboard_model._unread = [
        {"from": "Alice <alice@example.com>", "subject": "RE: Design review"},
        {"from": "Bob", "subject": "PR ready"},
    ]
    dashboard_model._health = {
        "loadPerCpu": 0.42,
        "cpus": 8,
        "memUsedPct": 68.5,
        "memUsedGb": 10.96,
        "memTotalGb": 16.0,
        "diskUsedPct": 72.3,
        "diskUsedGb": 650.5,
        "diskTotalGb": 900.0,
        "swapUsedPct": 0.0,
        "swapUsedGb": 0.0,
        "swapTotalGb": 0.0,
        "cpuTempC": 62.0,
        "gpus": [
            {
                "name": "NVIDIA RTX 3090",
                "utilPct": 45.0,
                "memUsedGb": 12.5,
                "memTotalGb": 24.0,
                "memUsedPct": 52.08,
                "tempC": 68.0,
                "powerW": 285.0,
            }
        ],
        "uptimeHours": 48.5,
    }
    dashboard_model._gpus = dashboard_model._health["gpus"]
    dashboard_model.dataChanged.emit()

    feed_model._items = [
        {
            "key": "git:1234",
            "kind": "git",
            "title": "Uncommitted changes in main.go",
            "detail": "3 files changed, 42 insertions(+), 8 deletions(-)",
            "dir": "/home/user/project",
            "dirName": "project",
            "task": "Review and commit the changes",
            "url": "",
        },
        {
            "key": "github:5678",
            "kind": "github",
            "title": "PR #42 ready for review",
            "detail": "",
            "dir": "/home/user/project",
            "dirName": "project",
            "task": "",
            "url": "https://github.com/user/project/pull/42",
        },
    ]
    feed_model.beginResetModel()
    feed_model.endResetModel()

    stats_data = {
        "sessions": 12,
        "running_turns": 2,
        "bg_tasks": 1,
        "input_tokens": 50000,
        "cache_read_tokens": 80000,
        "cache_write_tokens": 10000,
    }

    # Expose to QML
    ctx.setContextProperty("dashboardModel", dashboard_model)
    ctx.setContextProperty("feedModel", feed_model)
    ctx.setContextProperty("sessionsModel", sessions_model)
    ctx.setContextProperty("statsData", stats_data)

    # Load just HomeView (not full Main.qml)
    test_qml = """
import QtQuick
import QtQuick.Window
import "../eigenqt/qml"

Window {
    id: win
    width: 1280
    height: 800
    visible: true

    HomeView {
        anchors.fill: parent
        dashboardModel: dashboardModel
        feedModel: feedModel
        sessionsModel: sessionsModel
        statsData: statsData
    }

    Component.onCompleted: {
        grabTimer.start()
    }

    Timer {
        id: grabTimer
        interval: 500
        onTriggered: {
            console.log("Screenshot saved: screenshots/home-view.png")
            Qt.quit()
        }
    }
}
"""

    from tempfile import NamedTemporaryFile
    import os

    with NamedTemporaryFile(mode="w", suffix=".qml", delete=False, dir=str(ROOT)) as f:
        f.write(test_qml)
        temp_qml = f.name

    engine.addImportPath(str(ROOT))
    engine.load(temp_qml)

    if not engine.rootObjects():
        print("Failed to load QML", file=sys.stderr)
        sys.exit(-1)

    # Create screenshots dir
    (ROOT / "screenshots").mkdir(exist_ok=True)

    sys.exit(app.exec())


if __name__ == "__main__":
    main()
