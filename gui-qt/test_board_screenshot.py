#!/usr/bin/env python3
"""
test_board_screenshot.py — Launch BoardView offscreen and capture screenshot.

Usage:
    cd gui-qt && QT_QPA_PLATFORM=offscreen .venv/bin/python3 test_board_screenshot.py

Screenshot saved to screenshots/board_view.png
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from PySide6.QtCore import QTimer
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine

from eigenqt.models.board import BoardModel, KanbanModel
from eigenqt.rpc import RpcClient

ROOT = Path(__file__).resolve().parent


def main():
    app = QGuiApplication(sys.argv)

    # Create mock client and models
    client = RpcClient()
    board_model = BoardModel(client)
    kanban_model = KanbanModel(client)

    # Inject mock data (simulating Board() RPC result)
    mock_lanes = [
        {
            "dir": "/home/user/eigen",
            "name": "eigen",
            "repo": "avifenesh/eigen",
            "branch": "main",
            "url": "https://github.com/avifenesh/eigen",
            "remote": True,
            "pinned": True,
            "dirty": 3,
            "unpushed": 2,
            "behind": 1,
            "todos": 12,
            "openPrs": 2,
            "openIss": 4,
            "items": [
                {
                    "key": "pr-123",
                    "kind": "github",
                    "title": "feat: add board view",
                    "detail": "PR #123 · ready for review",
                    "url": "https://github.com/avifenesh/eigen/pull/123",
                    "task": "",
                    "dir": "/home/user/eigen"
                },
                {
                    "key": "issue-456",
                    "kind": "github",
                    "title": "bug: kanban card overflow",
                    "detail": "issue #456",
                    "url": "https://github.com/avifenesh/eigen/issues/456",
                    "task": "",
                    "dir": "/home/user/eigen"
                }
            ]
        },
        {
            "dir": "/home/user/myproject",
            "name": "myproject",
            "repo": "",
            "branch": "feat/new-feature",
            "url": "",
            "remote": False,
            "pinned": False,
            "dirty": 5,
            "unpushed": 3,
            "behind": 0,
            "todos": 7,
            "openPrs": 0,
            "openIss": 0,
            "items": [
                {
                    "key": "git-uncommitted",
                    "kind": "git",
                    "title": "Uncommitted changes in feat/new-feature",
                    "detail": "5 modified files",
                    "url": "",
                    "task": "commit-and-push",
                    "dir": "/home/user/myproject"
                }
            ]
        },
        {
            "dir": "/home/user/clean-repo",
            "name": "clean-repo",
            "repo": "user/clean-repo",
            "branch": "main",
            "url": "https://github.com/user/clean-repo",
            "remote": True,
            "pinned": False,
            "dirty": 0,
            "unpushed": 0,
            "behind": 0,
            "todos": 0,
            "openPrs": 0,
            "openIss": 0,
            "items": []
        }
    ]

    board_model.beginResetModel()
    board_model._lanes = mock_lanes
    board_model.endResetModel()

    # Inject mock kanban data
    mock_kanban_columns = [
        {
            "id": "needs-you",
            "title": "Needs You",
            "cards": [
                {
                    "key": "card-pr-789",
                    "kind": "pr",
                    "title": "Review requested: Update dependencies",
                    "repo": "avifenesh/eigen",
                    "number": 789,
                    "ageHours": 48,
                    "needsYou": True,
                    "draft": False,
                    "review": "changes",
                    "session": False,
                    "url": "https://github.com/avifenesh/eigen/pull/789",
                    "task": "",
                    "dir": "/home/user/eigen"
                }
            ]
        },
        {
            "id": "in-review",
            "title": "In Review",
            "cards": [
                {
                    "key": "card-pr-123",
                    "kind": "pr",
                    "title": "feat: add board view",
                    "repo": "avifenesh/eigen",
                    "number": 123,
                    "ageHours": 24,
                    "needsYou": False,
                    "draft": False,
                    "review": "approved",
                    "session": True,
                    "url": "https://github.com/avifenesh/eigen/pull/123",
                    "task": "",
                    "dir": "/home/user/eigen"
                }
            ]
        },
        {
            "id": "done",
            "title": "Done",
            "cards": []
        }
    ]

    kanban_model._columns = mock_kanban_columns
    kanban_model.columnsChanged.emit()

    # Create QML engine
    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()

    # Expose models to QML
    ctx.setContextProperty("boardModel", board_model)
    ctx.setContextProperty("kanbanModel", kanban_model)

    # Load BoardView directly
    engine.load(str(ROOT / "eigenqt" / "qml" / "BoardView.qml"))

    if not engine.rootObjects():
        print("Failed to load BoardView.qml", file=sys.stderr)
        sys.exit(1)

    # Grab screenshot after short delay (allow rendering)
    def capture_screenshot():
        if not engine.rootObjects():
            print("No root objects", file=sys.stderr)
            sys.exit(1)

        root_obj = engine.rootObjects()[0]

        # Use QQuickWindow::grabWindow for offscreen screenshot
        from PySide6.QtQuick import QQuickWindow
        if isinstance(root_obj, QQuickWindow):
            pixmap = root_obj.grabWindow()
        else:
            # Try accessing window property
            window = root_obj.window()
            if window:
                pixmap = window.grabWindow()
            else:
                print("Cannot grab window (no QQuickWindow)", file=sys.stderr)
                app.quit()
                return

        screenshot_path = ROOT / "screenshots" / "board_view.png"
        screenshot_path.parent.mkdir(parents=True, exist_ok=True)
        pixmap.save(str(screenshot_path))
        print(f"Screenshot saved: {screenshot_path}")
        app.quit()

    QTimer.singleShot(1200, capture_screenshot)

    sys.exit(app.exec())


if __name__ == "__main__":
    main()
