#!/usr/bin/env python3
"""
test_views_screenshot.py — Capture screenshots of all main views with mock data.

Usage:
    cd gui-qt && QT_QPA_PLATFORM=offscreen .venv/bin/python3 test_views_screenshot.py

Screenshots saved to screenshots/qa-fix-*.png
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from PySide6.QtCore import QTimer
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine

from eigenqt.models.board import BoardModel, KanbanModel
from eigenqt.models.memory import MemoryModel
from eigenqt.models.notes import NotesController
from eigenqt.models.connectors import ConnectorsModel
from eigenqt.rpc import RpcClient
from eigenqt.highlighter_helper import HighlighterHelper
from eigenqt.markdown_helper import MarkdownHelper

ROOT = Path(__file__).resolve().parent
SCREENSHOTS = ROOT / "screenshots"
SCREENSHOTS.mkdir(exist_ok=True)


def capture_view(view_name: str, qml_file: str, setup_context):
    """Capture a single view screenshot."""
    print(f"Capturing {view_name}...")

    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()
    setup_context(ctx)

    qml_path = ROOT / "eigenqt" / "qml" / qml_file
    engine.load(str(qml_path))

    if not engine.rootObjects():
        print(f"Failed to load {qml_file}")
        return False

    root = engine.rootObjects()[0]

    # Wait for rendering
    for _ in range(10):
        app.processEvents()

    # Grab screenshot synchronously
    from PySide6.QtCore import QSize, QEventLoop
    size = QSize(1200, 800)

    output = SCREENSHOTS / f"qa-fix-{view_name}.png"
    saved = [False]
    loop = QEventLoop()

    def on_ready(result):
        success = result.saveToFile(str(output))
        saved[0] = success
        if success:
            print(f"✓ Saved {output}")
        else:
            print(f"✗ Failed to save {output}")
        loop.quit()

    root.grabToImage(on_ready, size)
    loop.exec()

    del engine
    return saved[0]


def main():
    global app

    # Create one app instance for all views
    app = QGuiApplication(sys.argv)

    client = RpcClient()
    highlighter = HighlighterHelper(app)
    markdown_parser = MarkdownHelper(app)

    # 1. BoardView
    def setup_board(ctx):
        board_model = BoardModel(client)
        kanban_model = KanbanModel(client)

        # Mock data
        board_model._lanes = [
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
                "behind": 0,
                "todos": 5,
                "openPrs": 2,
                "openIss": 1,
                "items": [
                    {
                        "key": "pr-123",
                        "kind": "github",
                        "title": "feat: Qt GUI board view",
                        "detail": "PR #123",
                        "url": "https://github.com/avifenesh/eigen/pull/123",
                    }
                ]
            }
        ]
        board_model.layoutChanged.emit()

        ctx.setContextProperty("boardModel", board_model)
        ctx.setContextProperty("kanbanModel", kanban_model)

    capture_view("board", "BoardView.qml", setup_board)

    # 2. ConnectorsView
    def setup_connectors(ctx):
        connectors_model = ConnectorsModel(client)
        connectors_model._loading = False
        connectors_model._connectors = {
            "connectors": [
                {
                    "name": "notion",
                    "display": "Notion",
                    "glyph": "◷",
                    "description": "Notion workspace",
                    "url": "https://mcp.notion.com/mcp",
                    "connected": True,
                }
            ],
            "directory": [
                {
                    "name": "slack",
                    "display": "Slack",
                    "glyph": "⟐",
                    "category": "Communication",
                    "added": False,
                }
            ]
        }
        connectors_model.connectorsChanged.emit()
        ctx.setContextProperty("connectorsModel", connectors_model)

    capture_view("connectors", "ConnectorsView.qml", setup_connectors)

    # 3. NotesView
    def setup_notes(ctx):
        notes_controller = NotesController(client)
        notes_controller._available = True
        notes_controller._vault = "/home/user/notes"

        # Mock notes
        from eigenqt.models.notes import NotesListModel
        notes_controller._notes_model = NotesListModel()
        notes_controller._notes_model._notes = [
            {"path": "Inbox/Ideas.md", "title": "Project ideas"},
            {"path": "Daily/2026-07-02.md", "title": "2026-07-02"},
        ]
        notes_controller._notes_model.layoutChanged.emit()

        ctx.setContextProperty("notesController", notes_controller)

    capture_view("notes", "NotesView.qml", setup_notes)

    # 4. MemoryView
    def setup_memory(ctx):
        memory_model = MemoryModel(client)
        memory_model._scopes = [
            {"key": "global", "name": "Global", "dir": "", "noteCount": 3},
            {"key": "project:/home/user/eigen", "name": "eigen", "dir": "/home/user/eigen", "noteCount": 5},
        ]
        memory_model._scope_key = "global"
        memory_model._scope_label = "Global"
        memory_model._loading = False
        memory_model._current = {
            "summary": "Eigen is a personal AI operating system.",
            "hasSummary": True,
            "notes": [
                {"index": 0, "text": "User prefers dark mode UIs"},
                {"index": 1, "text": "Uses Python 3.14 for dev work"},
            ],
            "adHoc": [
                {"index": 0, "text": "Remember to test Qt views thoroughly"},
            ],
            "noteCount": 2,
            "profile": "# User profile\n\nDeveloper working on eigen GUI.",
            "profileLearned": "Works on Qt/QML interfaces",
            "banList": [],
            "backups": 3,
            "bytes": 2048,
        }
        memory_model._is_empty = False
        memory_model._has_backup_history = True
        memory_model._is_global = True

        memory_model.scopesChanged.emit()
        memory_model.currentChanged.emit()

        ctx.setContextProperty("memoryModel", memory_model)
        ctx.setContextProperty("markdownParser", markdown_parser)

    capture_view("memory", "MemoryView.qml", setup_memory)

    print("\n✓ All screenshots captured")
    return 0


if __name__ == "__main__":
    sys.exit(main())
