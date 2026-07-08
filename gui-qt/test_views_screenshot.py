#!/usr/bin/env python3
"""
test_views_screenshot.py — Capture screenshots of all main views with mock data.

Usage:
    cd gui-qt && QT_QPA_PLATFORM=offscreen .venv/bin/python3 test_views_screenshot.py

Screenshots saved to screenshots/qa-fix-*.png
"""

import sys
import atexit
import base64
import os
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from PySide6.QtCore import QObject, Property, QPoint, QPointF, QTimer, QUrl, Qt, Signal, Slot
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine
from PySide6.QtQuick import QQuickView
from PySide6.QtTest import QTest

from eigenqt.models.board import BoardModel, KanbanModel
from eigenqt.models.config import ConfigModel, RuleChainsModel
from eigenqt.models.dreaming import DreamingModel
from eigenqt.models.memory import MemoryModel
from eigenqt.models.notes import NotesController
from eigenqt.models.connectors import ConnectorsModel
from eigenqt.models.reviewers import ReviewersModel
from eigenqt.models.observe import ObserveModel
from eigenqt.models.routing import RoutingModel
from eigenqt.models.machines import MachinesModel
from eigenqt.models.crons import CronsModel
from eigenqt.models.plugins import PluginsModel
from eigenqt.models.profile import ProfileModel
from eigenqt.models import (
    ApprovalsModel,
    CommandsModel,
    DashboardModel,
    FeedModel,
    LiveSessionsModel,
    ProposalsModel,
    SessionStateModel,
    SessionsModel,
    SkillsModel,
    TasksModel,
    TranscriptModel,
)
from eigenqt.models.transcript_logic import TranscriptRow
from eigenqt.clipboard_helper import ClipboardHelper
from eigenqt.highlighter_helper import HighlighterHelper
from eigenqt.markdown_helper import MarkdownHelper
from eigenqt.terminal_helper import TerminalHelper
from eigenqt.webengine import initialize_webengine

ROOT = Path(__file__).resolve().parent
SCREENSHOTS = ROOT / "screenshots"
SCREENSHOTS.mkdir(exist_ok=True)
VALID_PNG_BASE64 = (
    "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAACXBIWXMAAA7EAAAOxAGVKw4bAAAAG0lEQVQImWPUb3j7/4deMQPjp5M+/3/2cTMAAFRICM+3aAs3AAAAAElFTkSuQmCC"
)


class ScreenshotRpcClient(QObject):
    """Offline RPC double for deterministic screenshot QA."""

    connected = Signal()
    callDone = Signal(int, "QVariantMap")
    event = Signal(str, dict)
    dropped = Signal(str)

    def __init__(self):
        super().__init__()
        self.calls = []
        self._token = 0
        self.session_rows = []
        self.config_payload = {"path": "", "fields": []}
        self.rule_chains_payload = {"models": [], "roles": []}
        self.board_payload = {"lanes": []}
        self.kanban_payload = {"columns": []}
        self.diff_payload = {
            "isRepo": True,
            "clean": False,
            "branch": "feat/qt-dock-proof",
            "truncated": False,
            "files": [
                {"path": "gui-qt/eigenqt/qml/ChatView.qml", "adds": 12, "dels": 3},
                {"path": "gui-qt/eigenqt/qml/DiffTab.qml", "adds": 8, "dels": 1},
            ],
            "patch": (
                "diff --git a/gui-qt/eigenqt/qml/ChatView.qml b/gui-qt/eigenqt/qml/ChatView.qml\n"
                "index 1111111..2222222 100644\n"
                "--- a/gui-qt/eigenqt/qml/ChatView.qml\n"
                "+++ b/gui-qt/eigenqt/qml/ChatView.qml\n"
                "@@ -42,6 +42,9 @@ Rectangle {\n"
                "     property bool dockOpen: false\n"
                "+    readonly property bool qaDockProof: true\n"
                "+    readonly property string qaDockMode: \"visual\"\n"
                "     property int dockTabIndex: 0\n"
                "-    color: Theme.colors.bgBase\n"
                "+    color: Theme.colors.bgWell\n"
                " }\n"
                "diff --git a/gui-qt/eigenqt/qml/DiffTab.qml b/gui-qt/eigenqt/qml/DiffTab.qml\n"
                "index 3333333..4444444 100644\n"
                "--- a/gui-qt/eigenqt/qml/DiffTab.qml\n"
                "+++ b/gui-qt/eigenqt/qml/DiffTab.qml\n"
                "@@ -100,6 +100,7 @@ Item {\n"
                "     Text { text: branch }\n"
                "+    AppTag { text: branch }\n"
                " }\n"
            ),
        }
        self.file_tree_payload = {
            "truncated": False,
            "entries": [
                {
                    "name": "gui-qt",
                    "path": "/home/user/eigen/gui-qt",
                    "isDir": True,
                    "children": [
                        {
                            "name": "eigenqt",
                            "path": "/home/user/eigen/gui-qt/eigenqt",
                            "isDir": True,
                            "children": [
                                {
                                    "name": "qml",
                                    "path": "/home/user/eigen/gui-qt/eigenqt/qml",
                                    "isDir": True,
                                    "children": [
                                        {
                                            "name": "ChatView.qml",
                                            "path": "/home/user/eigen/gui-qt/eigenqt/qml/ChatView.qml",
                                            "isDir": False,
                                        },
                                        {
                                            "name": "DiffTab.qml",
                                            "path": "/home/user/eigen/gui-qt/eigenqt/qml/DiffTab.qml",
                                            "isDir": False,
                                        },
                                    ],
                                }
                            ],
                        }
                    ],
                },
                {"name": "README.md", "path": "/home/user/eigen/README.md", "isDir": False},
            ],
        }

    def call(self, method, *args, callback=None, error_callback=None):
        self.calls.append((method, args))
        if callback:
            QTimer.singleShot(0, lambda: callback({"result": self._result(method, args)}))

    @Slot(str, "QVariantList", result=int)
    def callToken(self, method, args):
        self._token += 1
        token = self._token
        call_args = tuple(args or [])
        self.calls.append((method, call_args))
        QTimer.singleShot(0, lambda: self.callDone.emit(token, {"result": self._result(method, call_args)}))
        return token

    @Slot(str, "QVariantList")
    def callFire(self, method, args):
        self.calls.append((method, tuple(args or [])))

    @Slot("QVariantList")
    def subscribe(self, channels):
        self.calls.append(("subscribe", tuple(channels or [])))

    @Slot("QVariantList")
    def unsubscribe(self, channels):
        self.calls.append(("unsubscribe", tuple(channels or [])))

    def shutdown(self):
        pass

    def _result(self, method, args):
        if method == "WorkingDiff":
            return self.diff_payload
        if method == "FileTree":
            return self.file_tree_payload
        if method == "ReadFileForView":
            return "# Eigen\n\nQt dock visual proof.\n\n- Diff\n- Files\n"
        if method == "TerminalStart":
            return "term-shot"
        if method == "Commands":
            return [
                {"name": "ship-it", "description": "Turn the current diff into a PR", "scope": "user"},
                {"name": "review", "description": "Custom review should not shadow the built-in", "scope": "user"},
            ]
        if method == "State":
            return {
                "id": args[0] if args else "s-qa-chat",
                "model": "gpt-5",
                "provider": "codex",
                "tokens": 32000,
                "maxTokens": 128000,
                "effort": "medium",
                "perm": "gated",
                "title": "Qt shell chat",
                "goal": "Verify screenshot QA",
                "search": "auto",
                "fast": True,
                "fastOk": True,
                "tools": [
                    {"name": "read_file", "read_only": True},
                    {"name": "run_shell", "read_only": False},
                ],
                "running": False,
                "roots": ["/home/user/eigen"],
                "catalog": {"models": [{"id": "gpt-5", "effortLevels": ["low", "medium", "high"]}]},
                "messages": [],
                "pending": [],
            }
        if method == "Sessions":
            return [dict(session) for session in self.session_rows]
        if method == "PruneSessions":
            removed = [session.get("id", "") for session in self.session_rows if not session.get("turns")]
            self.session_rows = [session for session in self.session_rows if session.get("id", "") not in removed]
            return removed
        if method == "Board":
            return self.board_payload
        if method == "Kanban":
            return self.kanban_payload
        if method in {"Tasks", "Skills", "ProposedSkills"}:
            return {}
        if method == "Dashboard":
            return {"googleConnected": False, "events": [], "unreadCount": 0, "unread": [], "health": {"gpus": []}}
        if method == "Feed":
            return {"items": [], "fresh": False}
        if method == "Connectors":
            return {"connectors": [], "directory": []}
        if method == "MCPServers":
            return {"servers": []}
        if method == "GoogleStatus":
            return {"configured": False, "connected": False, "clientPath": "", "setupUrl": "", "setupHint": ""}
        if method == "MCPSecretsAvailable":
            return False
        if method == "ObsidianStatus":
            return {"available": False, "vault": ""}
        if method == "ObsidianNotes":
            return []
        if method == "ListMemoryScopes":
            return [
                {"key": "global", "name": "Global", "dir": "", "noteCount": 3},
                {
                    "key": "project:/home/user/eigen",
                    "name": "eigen",
                    "dir": "/home/user/eigen",
                    "noteCount": 5,
                    "current": True,
                },
            ]
        if method == "MemoryForScope":
            scope = args[0] if args else ""
            return {
                "scope": scope,
                "summary": "",
                "notes": [],
                "adHoc": [],
                "profile": "# User profile\n\nDeveloper working on eigen GUI." if scope == "global" else "",
                "profileLearned": "Works on Qt/QML interfaces" if scope == "global" else "",
                "banList": [],
            }
        if method == "DreamingForScope":
            scope = args[0] if args else "project:/home/user/eigen"
            return {
                "scope": scope,
                "currentBytes": 4096,
                "rollouts": [
                    {
                        "index": 1,
                        "text": "# Outcome: success\n\nCaptured focused Qt proof.",
                        "outcome": "success",
                        "whenMs": 1783155600000,
                    },
                    {
                        "index": 0,
                        "text": "# Outcome: partial\n\nNeeds visual pass.",
                        "outcome": "partial",
                        "whenMs": 1783144800000,
                    },
                ],
                "consolidations": [
                    {
                        "path": "/home/user/eigen/.eigen/memory/MEMORY.md.20260707-120000.bak",
                        "label": "Jul 7, 12:00",
                        "whenMs": 1783152000000,
                        "bytes": 2048,
                    }
                ],
            }
        if method == "Config":
            return self.config_payload
        if method == "RuleChains":
            return self.rule_chains_payload
        if method == "Routing":
            return {
                "models": [
                    {
                        "id": "gpt-5",
                        "provider": "codex",
                        "contextWindow": 400000,
                        "cache": True,
                        "context1m": False,
                        "reasoning": True,
                        "effort": "medium",
                        "effortLevels": ["low", "medium", "high"],
                        "thinkingBudget": 0,
                        "search": True,
                        "vision": True,
                        "social": False,
                        "available": True,
                    },
                    {
                        "id": "grok-4",
                        "provider": "grok",
                        "contextWindow": 256000,
                        "cache": False,
                        "context1m": False,
                        "reasoning": True,
                        "effort": "high",
                        "effortLevels": ["low", "high"],
                        "thinkingBudget": 0,
                        "search": True,
                        "vision": False,
                        "social": True,
                        "available": False,
                    },
                    {
                        "id": "local-qwen",
                        "provider": "llama",
                        "contextWindow": 128000,
                        "cache": False,
                        "context1m": False,
                        "reasoning": False,
                        "search": False,
                        "vision": False,
                        "social": False,
                        "available": True,
                    },
                ],
                "providers": [
                    {"name": "codex", "credentialed": True, "modelCount": 1},
                    {"name": "grok", "credentialed": False, "modelCount": 1},
                    {"name": "llama", "credentialed": True, "modelCount": 1},
                ],
            }
        if method == "ObserveSummary":
            return {
                "available": True,
                "records": 4,
                "routes": {
                    "routed": 2,
                    "assessed": 1,
                    "skipped": 1,
                    "orchestrator": 0,
                    "byModel": [{"name": "gpt-5", "count": 2}],
                    "byKind": [],
                    "byDifficulty": [],
                    "skipReasons": [],
                },
                "tools": [
                    {"name": "read_file", "calls": 4, "errors": 0, "durationMs": 80},
                    {"name": "run_shell", "calls": 2, "errors": 1, "durationMs": 420},
                ],
                "models": [
                    {
                        "name": "gpt-5",
                        "turns": 3,
                        "inTokens": 12000,
                        "outTokens": 2100,
                        "cacheReadTokens": 6000,
                        "cacheWriteTokens": 200,
                        "durationMs": 1800,
                    }
                ],
                "hooks": [],
                "errors": [{"name": "rpc timeout", "count": 1}],
                "byKind": [],
                "subagents": {
                    "taskCalls": 2,
                    "taskErrors": 0,
                    "groupCalls": 1,
                    "groupErrors": 0,
                    "mutatingCalls": 1,
                    "mutatingErrors": 0,
                    "statusChecks": 3,
                    "promotes": 0,
                    "promoteErrors": 0,
                    "backgroundDone": 1,
                    "backgroundNotes": 1,
                    "routeNotes": 1,
                },
            }
        if method == "Machines":
            return {
                "machines": [
                    {
                        "name": "codex-box",
                        "ssh": "codex-box",
                        "addr": "10.0.0.5",
                        "dir": "/home/user/eigen",
                        "model": "gpt-5",
                        "perm": "gated",
                        "saved": True,
                        "detected": False,
                    },
                    {
                        "name": "lab-node",
                        "ssh": "lab-node",
                        "dir": "/srv/eigen",
                        "model": "local-qwen",
                        "perm": "manual",
                        "saved": False,
                        "detected": True,
                    },
                ]
            }
        if method == "RemoteSessions":
            target = args[0] if args else "codex-box"
            return [
                {
                    "id": f"remote:{target}:s1",
                    "title": "Remote Qt polish",
                    "dir": "/home/user/eigen/gui-qt",
                    "model": "gpt-5",
                    "status": "working",
                    "turns": 4,
                    "views": 1,
                    "updated": 1783155600000,
                },
                {
                    "id": f"remote:{target}:s2",
                    "title": "Remote notes",
                    "dir": "/home/user/eigen",
                    "model": "local-qwen",
                    "status": "idle",
                    "turns": 1,
                    "views": 1,
                    "updated": 1783144800000,
                },
            ]
        if method == "Crons":
            return {
                "crons": [
                    {
                        "name": "eigen-dream",
                        "kind": "timer",
                        "next": "today 19:30",
                        "last": "today 17:00",
                        "active": True,
                        "enabled": True,
                        "command": "eigen-dream.service",
                        "unit": "eigen-dream.timer",
                    },
                    {
                        "name": "eigen-clean",
                        "kind": "timer",
                        "next": "2026-07-08 09:00",
                        "last": "",
                        "active": False,
                        "enabled": True,
                        "command": "eigen-clean.service",
                        "unit": "eigen-clean.timer",
                    },
                    {
                        "name": "eigen run daily",
                        "kind": "crontab",
                        "next": "0 9 * * *",
                        "last": "",
                        "active": True,
                        "enabled": True,
                        "command": "eigen run daily",
                    },
                ],
                "timers": 2,
                "crontab": 1,
                "systemdAvail": True,
            }
        if method == "Plugins":
            return {
                "plugins": [
                    {
                        "name": "agentsys",
                        "marketplace": "core",
                        "version": "5.1.0",
                        "description": "Agent workflow tools",
                        "installedMs": 1783155600000,
                        "enabled": True,
                        "skills": ["audit-project"],
                        "agents": ["reviewer"],
                        "mcpServers": ["github"],
                        "commands": ["enhance"],
                        "hooks": 2,
                        "scanStatus": "clean",
                        "scanCount": 0,
                    },
                    {
                        "name": "local-risk",
                        "marketplace": "lab",
                        "version": "0.1.0",
                        "description": "Local experiment",
                        "installedMs": 1783144800000,
                        "enabled": False,
                        "skills": ["scratch"],
                        "scanStatus": "forced",
                        "scanCount": 1,
                        "scans": [{"component": "skills/scratch", "reasons": ["network shell"]}],
                        "warnings": ["installed with scan flags"],
                    },
                ],
                "marketplaces": [
                    {
                        "name": "core",
                        "source": "github.com/avifenesh/eigen-plugins",
                        "owner": "Avi",
                        "disabled": False,
                        "addedMs": 1783152000000,
                    },
                    {
                        "name": "lab",
                        "source": "/home/user/plugins/lab",
                        "disabled": True,
                        "addedMs": 1783141200000,
                    },
                ],
            }
        if method == "SetTitle":
            return {
                "model": "gpt-5",
                "effort": "medium",
                "perm": "gated",
                "title": args[1] if len(args) > 1 else "Qt shell chat",
                "goal": "Verify screenshot QA",
                "search": "auto",
                "fast": True,
                "fastOk": True,
                "tools": [
                    {"name": "read_file", "read_only": True},
                    {"name": "run_shell", "read_only": False},
                ],
                "running": False,
                "roots": ["/home/user/eigen"],
                "catalog": {"models": [{"id": "gpt-5", "effortLevels": ["low", "medium", "high"]}]},
                "messages": [],
                "pending": [],
            }
        if method == "SetGoal":
            return self._state(goal=args[1] if len(args) > 1 else "")
        if method == "SetSearch":
            return self._state(search=args[1] if len(args) > 1 else "off")
        if method == "SetFast":
            return self._state(fast=bool(args[1]) if len(args) > 1 else False)
        if method == "AddDir":
            return args[1] if len(args) > 1 else ""
        if method == "Clear":
            return None
        if method == "Compact":
            return {"before": 42, "after": 7}
        if method == "ExportSession":
            return "/home/user/eigen-exports/s-qa-chat.jsonl"
        if method == "RevutoStatus":
            return {"available": False, "count": 0, "paused": 0}
        if method == "RevutoReviewers":
            return []
        return {}

    def _state(self, **overrides):
        state = {
            "model": "gpt-5",
            "effort": "medium",
            "perm": "gated",
            "title": "Qt shell chat",
            "goal": "Verify screenshot QA",
            "search": "auto",
            "fast": True,
            "fastOk": True,
            "tools": [
                {"name": "read_file", "read_only": True},
                {"name": "run_shell", "read_only": False},
            ],
            "running": False,
            "roots": ["/home/user/eigen"],
            "catalog": {"models": [{"id": "gpt-5", "effortLevels": ["low", "medium", "high"]}]},
            "messages": [],
            "pending": [],
        }
        state.update(overrides)
        return state


class ScreenshotSessionController(QObject):
    sessionIdChanged = Signal()
    sessionStateModelChanged = Signal()
    commandsModelChanged = Signal()

    def __init__(self, client, parent=None):
        super().__init__(parent)
        self._session_id = "s-qa-chat"
        self._session_state_model = SessionStateModel(client, "")
        self._session_state_model.seed(
            {
                "model": "gpt-5",
                "provider": "codex",
                "tokens": 32000,
                "maxTokens": 128000,
                "effort": "medium",
                "perm": "gated",
                "title": "Qt shell chat",
                "goal": "Verify the shell does not clip the composer",
                "search": "auto",
                "fast": True,
                "fastOk": True,
                "tools": [
                    {"name": "read_file", "read_only": True},
                    {"name": "run_shell", "read_only": False},
                ],
                "running": False,
                "roots": ["/home/user/eigen"],
                "shells": [
                    {
                        "id": "sh-1",
                        "command": "pytest -q gui-qt/tests/test_chat_controls.py",
                        "status": "running",
                        "exit_code": 0,
                        "last_line": "collecting tests",
                    }
                ],
                "pending": [
                    {"id": "approval-1", "tool": "shell", "args": "{\"cmd\":\"make test\"}"}
                ],
                "catalog": {
                    "models": [
                        {"id": "gpt-5", "effortLevels": ["low", "medium", "high"]},
                        {"id": "local-qwen", "effortLevels": ["low", "medium"]},
                    ]
                },
            }
        )
        self._commands_model = None

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


def find_item(item, object_name):
    if item is None:
        return None
    if item.objectName() == object_name:
        return item
    for child in item.childItems():
        found = find_item(child, object_name)
        if found is not None:
            return found
    return None


def scene_top(item):
    return item.mapToScene(QPointF(0, 0)).y()


def scene_bottom(item):
    return item.mapToScene(QPointF(0, float(item.property("height") or 0))).y()


def scene_right(item):
    return item.mapToScene(QPointF(float(item.property("width") or 0), 0)).x()


def assert_app_tags_have_padding(view_name, root_item):
    failures = []

    def visit(item):
        if item is None:
            return
        if item.property("qaIsAppTag") is True:
            width = float(item.property("width") or 0)
            height = float(item.property("height") or 0)
            visible = item.isVisible() if hasattr(item, "isVisible") else item.property("visible")
            if visible and width > 0 and height > 0:
                padding = float(item.property("qaHorizontalPadding") or 0)
                if item.property("qaTextFits") is not True or padding < 7.5:
                    failures.append(
                        f"{item.objectName() or '<unnamed>'} "
                        f"fits={item.property('qaTextFits')} padding={padding:.1f} size={width:.1f}x{height:.1f}"
                    )
        if hasattr(item, "childItems"):
            for child in item.childItems():
                visit(child)

    visit(root_item)
    if failures:
        raise AssertionError(f"{view_name} rendered cramped AppTag text: {failures[:8]}")


def click_item(view, item):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    if width <= 0 or height <= 0:
        raise AssertionError(f"{item.objectName()} has invalid size {width}x{height}")
    point = item.mapToItem(view.contentItem(), QPointF(width / 2, height / 2))
    QTest.mouseClick(view, Qt.LeftButton, Qt.NoModifier, QPoint(int(point.x()), int(point.y())))
    QTest.mouseMove(view, QPoint(2, 2))
    QTest.qWait(80)


def capture_view(view_name: str, qml_file: str, setup_context, after_render=None, width=1200, height=800):
    """Capture a single view screenshot."""
    print(f"Capturing {view_name}...")

    view = QQuickView()
    view.setResizeMode(QQuickView.SizeRootObjectToView)
    view.setWidth(width)
    view.setHeight(height)

    ctx = view.rootContext()
    initial_properties = setup_context(ctx) or {}

    qml_path = ROOT / "eigenqt" / "qml" / qml_file
    view.engine().addImportPath(str(ROOT / "eigenqt"))
    view.setInitialProperties(initial_properties)
    view.setSource(QUrl.fromLocalFile(str(qml_path)))

    if view.status() == QQuickView.Error or view.rootObject() is None:
        print(f"Failed to load {qml_file}: {[error.toString() for error in view.errors()]}")
        return False

    view.show()

    # Wait for rendering
    for _ in range(10):
        app.processEvents()
    QTest.qWait(160)
    app.processEvents()
    if after_render:
        after_render(view, view.rootObject())
        for _ in range(12):
            app.processEvents()
    assert_app_tags_have_padding(view_name, view.rootObject())

    output = SCREENSHOTS / f"qa-fix-{view_name}.png"
    image = view.grabWindow()
    success = image.save(str(output))
    if success:
        print(f"✓ Saved {output}")
    else:
        print(f"✗ Failed to save {output}")

    view.hide()
    view.setSource(QUrl())
    return success


def capture_main_shell(client, clipboard_helper, highlighter, markdown_parser, terminal_helper):
    """Capture the real Main.qml shell on Chat so bottom composer geometry is visible."""
    print("Capturing main-chat...")

    engine = QQmlApplicationEngine()
    engine.addImportPath(str(ROOT / "eigenqt"))
    ctx = engine.rootContext()

    sessions_model = SessionsModel(client)
    sessions_model._on_sessions_result(
        {
            "result": [
                {
                    "id": "s-qa-chat",
                    "title": "Qt shell chat",
                    "dir": "/home/user/eigen/gui-qt",
                    "model": "gpt-5",
                    "status": "working",
                    "turns": 3,
                    "updated": 1783155600000,
                },
                {
                    "id": "s-qa-approval",
                    "title": "Approval check",
                    "dir": "/home/user/eigen",
                    "model": "local-qwen",
                    "status": "approval",
                    "turns": 2,
                    "updated": 1783144800000,
                },
            ]
        }
    )
    sessions_model.mark_unread("s-qa-chat")

    controller = ScreenshotSessionController(client)
    transcript_model = TranscriptModel(client, "")
    transcript_model._rows = [
        TranscriptRow(kind="user", text="The send button used to be clipped here."),
        TranscriptRow(kind="assistant", text="The status strip now has its own layout row."),
    ]
    transcript_model.layoutChanged.emit()

    feed_model = FeedModel(client)
    feed_model._on_feed_result(
        {
            "result": {
                "items": [
                    {
                        "key": "rail-feed",
                        "kind": "git",
                        "title": "Qt follow-up",
                        "detail": "Keep the desktop shell honest",
                        "dir": "/home/user/eigen",
                        "dirName": "eigen",
                        "task": "Tighten the Qt desktop shell.",
                    }
                ],
                "fresh": True,
            }
        }
    )

    context = {
        "rpcClient": client,
        "client": client,
        "sessionsModel": sessions_model,
        "liveSessionsModel": LiveSessionsModel(client),
        "tasksModel": TasksModel(client),
        "dashboardModel": DashboardModel(client),
        "feedModel": feed_model,
        "boardModel": BoardModel(client),
        "kanbanModel": KanbanModel(client),
        "skillsModel": SkillsModel(client),
        "proposalsModel": ProposalsModel(client),
        "memoryModel": MemoryModel(client),
        "dreamingModel": DreamingModel(client),
        "notesController": NotesController(client),
        "connectorsModel": ConnectorsModel(client),
        "observeModel": ObserveModel(client),
        "routingModel": RoutingModel(client),
        "machinesModel": MachinesModel(client),
        "cronsModel": CronsModel(client),
        "pluginsModel": PluginsModel(client),
        "profileModel": ProfileModel(client),
        "configModel": ConfigModel(client),
        "ruleChainsModel": RuleChainsModel(client),
        "reviewersModel": ReviewersModel(client),
        "sessionController": controller,
        "transcriptModel": transcript_model,
        "approvalsModel": ApprovalsModel(client, ""),
        "daemonOnline": True,
        "guiserverSha": "qa1234567890",
        "statsData": {"running_turns": 2, "sessions": 7},
        "clipboardHelper": clipboard_helper,
        "highlighter": highlighter,
        "markdownParser": markdown_parser,
        "terminalHelper": terminal_helper,
    }
    for name, value in context.items():
        ctx.setContextProperty(name, value)

    engine.load(str(ROOT / "eigenqt" / "qml" / "Main.qml"))
    if not engine.rootObjects():
        print("Failed to load Main.qml")
        return False

    window = engine.rootObjects()[0]
    window.setProperty("width", 1200)
    window.setProperty("height", 800)
    window.setProperty("currentRoute", "chat")
    window.show()

    for _ in range(20):
        app.processEvents()

    composer = find_item(window.contentItem(), "chatComposerTextArea")
    if composer is not None:
        composer.setProperty("text", "Send button stays visible above the status strip")
    for _ in range(10):
        app.processEvents()

    unread_dot = find_item(window.contentItem(), "navRunningUnread_s_qa_chat")
    read_dot = find_item(window.contentItem(), "navRunningUnread_s_qa_approval")
    if unread_dot is None or unread_dot.property("visible") is not True:
        print("✗ Main chat proof did not show unread rail marker")
        window.hide()
        return False
    if read_dot is None or read_dot.property("visible") is not False:
        print("✗ Main chat proof showed unread rail marker for a read session")
        window.hide()
        return False
    home_nav = find_item(window.contentItem(), "navItem_home")
    if home_nav is None or home_nav.property("badge") != 1:
        print("✗ Main chat proof did not show feed badge on the Home rail item")
        window.hide()
        return False

    output = SCREENSHOTS / "qa-fix-main-chat.png"
    image = window.grabWindow()
    success = image.save(str(output))
    if success:
        print(f"✓ Saved {output}")
    else:
        print(f"✗ Failed to save {output}")

    window.setProperty("actionError", "Could not start session: daemon offline")
    for _ in range(10):
        app.processEvents()
    status_strip = find_item(window.contentItem(), "mainStatusStrip")
    shell_error = find_item(window.contentItem(), "mainActionError")
    shell_error_text = find_item(window.contentItem(), "mainActionErrorText")
    shell_error_dismiss = find_item(window.contentItem(), "mainDismissActionError")
    if shell_error is None or shell_error.property("visible") is not True:
        print("✗ Main shell action error proof did not render the error banner")
        window.hide()
        return False
    if shell_error_text is None or "daemon offline" not in shell_error_text.property("text"):
        print("✗ Main shell action error proof rendered the wrong error text")
        window.hide()
        return False
    if shell_error_dismiss is None or shell_error_dismiss.property("qaTextFits") is not True:
        print("✗ Main shell action error proof did not render a clean dismiss button")
        window.hide()
        return False
    if status_strip is None or scene_bottom(shell_error) > scene_top(status_strip) + 0.5:
        print(
            "✗ Main shell action error proof overlapped the status strip: "
            f"error bottom={scene_bottom(shell_error):.1f}, "
            f"status top={scene_top(status_strip) if status_strip is not None else -1:.1f}"
        )
        window.hide()
        return False

    output_error = SCREENSHOTS / "qa-fix-main-shell-action-error.png"
    image_error = window.grabWindow()
    success_error = image_error.save(str(output_error))
    if success_error:
        print(f"✓ Saved {output_error}")
    else:
        print(f"✗ Failed to save {output_error}")
    window.setProperty("actionError", "")
    for _ in range(10):
        app.processEvents()

    window.setProperty("width", 900)
    window.setProperty("height", 420)
    QTest.qWait(80)
    for _ in range(14):
        app.processEvents()

    send_button = find_item(window.contentItem(), "chatSendButton")
    status_strip = find_item(window.contentItem(), "mainStatusStrip")
    fast_switch = find_item(window.contentItem(), "sessionFastSwitch")
    if send_button is None or status_strip is None or fast_switch is None:
        print("✗ Main minimum chat proof could not find send button/status strip/fast switch")
        window.hide()
        return False
    if scene_right(fast_switch) > float(window.width()) + 0.5:
        print(
            "✗ Main minimum chat proof clipped fast switch: "
            f"switch right={scene_right(fast_switch):.1f}, "
            f"window width={float(window.width()):.1f}"
        )
        window.hide()
        return False
    if scene_bottom(send_button) > scene_top(status_strip) + 0.5:
        print(
            "✗ Main minimum chat proof clipped send button: "
            f"send bottom={scene_bottom(send_button):.1f}, "
            f"status top={scene_top(status_strip):.1f}"
        )
        window.hide()
        return False
    if scene_bottom(status_strip) > float(window.height()) + 0.5:
        print(
            "✗ Main minimum chat proof clipped status strip: "
            f"status bottom={scene_bottom(status_strip):.1f}, "
            f"window height={float(window.height()):.1f}"
        )
        window.hide()
        return False

    output_min = SCREENSHOTS / "qa-fix-main-chat-minimum.png"
    image_min = window.grabWindow()
    success_min = image_min.save(str(output_min))
    if success_min:
        print(f"✓ Saved {output_min}")
    else:
        print(f"✗ Failed to save {output_min}")

    window.setProperty("minimumHeight", 320)
    window.setProperty("width", 900)
    window.setProperty("height", 320)
    QTest.qWait(80)
    for _ in range(14):
        app.processEvents()

    if scene_bottom(send_button) > scene_top(status_strip) + 0.5:
        print(
            "✗ Main compact chat proof clipped send button: "
            f"send bottom={scene_bottom(send_button):.1f}, "
            f"status top={scene_top(status_strip):.1f}"
        )
        window.hide()
        return False
    if scene_bottom(status_strip) > float(window.height()) + 0.5:
        print(
            "✗ Main compact chat proof clipped status strip: "
            f"status bottom={scene_bottom(status_strip):.1f}, "
            f"window height={float(window.height()):.1f}"
        )
        window.hide()
        return False
    if scene_right(fast_switch) > float(window.width()) + 0.5:
        print(
            "✗ Main compact chat proof clipped fast switch: "
            f"switch right={scene_right(fast_switch):.1f}, "
            f"window width={float(window.width()):.1f}"
        )
        window.hide()
        return False

    output_compact = SCREENSHOTS / "qa-fix-main-chat-compact.png"
    image_compact = window.grabWindow()
    success_compact = image_compact.save(str(output_compact))
    if success_compact:
        print(f"✓ Saved {output_compact}")
    else:
        print(f"✗ Failed to save {output_compact}")

    compact_model_combo = find_item(window.contentItem(), "sessionModelCombo")
    if compact_model_combo is None:
        print("✗ Main compact dropdown proof could not find the model combo")
        window.hide()
        return False
    compact_model_combo.setProperty("qaPopupOpen", True)
    QTest.qWait(80)
    for _ in range(14):
        app.processEvents()
    if compact_model_combo.property("qaPopupActuallyOpen") is not True:
        print("✗ Main compact dropdown proof did not open the model dropdown")
        window.hide()
        return False
    if compact_model_combo.property("qaPopupInsideWindow") is not True:
        print(
            "✗ Main compact dropdown proof clipped model dropdown: "
            f"above={compact_model_combo.property('qaPopupAvailableAbove')}, "
            f"below={compact_model_combo.property('qaPopupAvailableBelow')}, "
            f"height={compact_model_combo.property('qaPopupEffectiveHeight')}"
        )
        window.hide()
        return False

    output_compact_dropdown = SCREENSHOTS / "qa-fix-main-chat-compact-dropdown.png"
    image_compact_dropdown = window.grabWindow()
    success_compact_dropdown = image_compact_dropdown.save(str(output_compact_dropdown))
    if success_compact_dropdown:
        print(f"✓ Saved {output_compact_dropdown}")
    else:
        print(f"✗ Failed to save {output_compact_dropdown}")
    compact_model_combo.setProperty("qaPopupOpen", False)
    QTest.qWait(80)
    for _ in range(14):
        app.processEvents()

    safe_bottom_inset = 40
    if not window.setProperty("bottomPadding", safe_bottom_inset):
        print("✗ Main safe-area chat proof could not set bottomPadding")
        window.hide()
        return False
    window.setProperty("width", 900)
    window.setProperty("height", 360)
    QTest.qWait(80)
    for _ in range(14):
        app.processEvents()

    safe_bottom = float(window.height()) - safe_bottom_inset
    if scene_bottom(status_strip) > safe_bottom + 0.5:
        print(
            "✗ Main safe-area chat proof clipped status strip: "
            f"status bottom={scene_bottom(status_strip):.1f}, "
            f"safe bottom={safe_bottom:.1f}"
        )
        window.hide()
        return False
    if scene_bottom(send_button) > scene_top(status_strip) + 0.5:
        print(
            "✗ Main safe-area chat proof clipped send button: "
            f"send bottom={scene_bottom(send_button):.1f}, "
            f"status top={scene_top(status_strip):.1f}"
        )
        window.hide()
        return False

    output_safe = SCREENSHOTS / "qa-fix-main-chat-safe-area.png"
    image_safe = window.grabWindow()
    success_safe = image_safe.save(str(output_safe))
    if success_safe:
        print(f"✓ Saved {output_safe}")
    else:
        print(f"✗ Failed to save {output_safe}")

    window.hide()
    return success and success_error and success_min and success_compact and success_compact_dropdown and success_safe


def main():
    global app

    # Create one app instance for all views
    initialize_webengine()
    app = QGuiApplication(sys.argv)

    client = ScreenshotRpcClient()
    clipboard_helper = ClipboardHelper(app)
    highlighter = HighlighterHelper(app)
    markdown_parser = MarkdownHelper(app)
    terminal_helper = TerminalHelper(app)
    atexit.register(client.shutdown)
    ok = True

    # 1. HomeView
    def setup_home(ctx):
        dashboard_model = DashboardModel(client)
        dashboard_model._on_dashboard_result({
            "result": {
                "googleConnected": True,
                "events": [
                    {
                        "summary": "Qt parity review",
                        "start": "2026-07-07T17:00:00+03:00",
                        "allDay": False,
                    },
                    {
                        "summary": "Ship follow-up",
                        "start": "2026-07-07T19:30:00+03:00",
                        "allDay": False,
                    },
                ],
                "unreadCount": 2,
                "unread": [
                    {"from": "Revuto <bot@example.test>", "subject": "Review finished"},
                    {"from": "GitHub <noreply@example.test>", "subject": "PR checks passed"},
                ],
                "health": {
                    "cpuTempC": 62,
                    "loadPerCpu": 0.42,
                    "memUsedGb": 22.4,
                    "memTotalGb": 64.0,
                    "memUsedPct": 35,
                    "diskUsedPct": 58,
                    "gpus": [
                        {
                            "name": "RTX PRO 6000",
                            "utilPct": 36,
                            "memUsedGb": 18.2,
                            "memTotalGb": 96.0,
                            "memUsedPct": 19,
                            "tempC": 54,
                            "powerW": 182,
                        }
                    ],
                },
            }
        })
        feed_model = FeedModel(client)
        feed_model._on_feed_result({
            "result": {
                "items": [
                    {
                        "key": "home-qt-follow-up",
                        "kind": "git",
                        "title": "Qt follow-up",
                        "detail": "Keep the desktop shell honest",
                        "dir": "/home/user/eigen",
                        "dirName": "eigen",
                        "task": "Tighten the Qt desktop shell.",
                    },
                    {
                        "key": "home-review-ready",
                        "kind": "github",
                        "title": "PR #76 checks",
                        "detail": "Review the green follow-up before merge",
                        "dir": "/home/user/eigen",
                        "dirName": "eigen",
                        "url": "https://github.com/avifenesh/eigen/pull/76",
                    },
                ],
                "fresh": True,
            }
        })
        sessions_model = SessionsModel(client)
        sessions_model._on_sessions_result(
            {
                "result": [
                    {
                        "id": "s-home-working",
                        "title": "Qt shell polish",
                        "dir": "/home/user/eigen/gui-qt",
                        "model": "gpt-5",
                        "status": "working",
                        "turns": 4,
                        "updated": 1783155600000,
                    },
                    {
                        "id": "s-home-approval",
                        "title": "Approval check",
                        "dir": "/home/user/eigen",
                        "model": "local-qwen",
                        "status": "approval",
                        "turns": 2,
                        "updated": 1783152000000,
                    },
                    {
                        "id": "s-home-recent",
                        "title": "Recent notes cleanup",
                        "dir": "/home/user/eigen",
                        "model": "grok-4",
                        "status": "idle",
                        "turns": 5,
                        "updated": 1783148400000,
                    },
                ]
            }
        )
        ctx.setContextProperty("dashboardModel", dashboard_model)
        ctx.setContextProperty("feedModel", feed_model)
        ctx.setContextProperty("sessionsModel", sessions_model)
        ctx.setContextProperty("rpcClient", client)
        return {
            "dashboardModel": dashboard_model,
            "feedModel": feed_model,
            "sessionsModel": sessions_model,
            "rpcClient": client,
            "statsData": {"sessions": 71, "running_turns": 2, "bg_tasks": 1, "input_tokens": 12000, "cache_read_tokens": 4200},
        }

    def assert_home_tags(_view, root):
        for object_name in (
            "homePanelBadge_Inbox",
            "homeFeedDirTag_home_qt_follow_up",
            "homeLiveApprovalTag_s_home_approval",
        ):
            tag = find_item(root, object_name)
            if tag is None or tag.property("qaIsAppTag") is not True:
                raise AssertionError(f"home tag {object_name} did not use AppTag")
            if tag.property("qaTextFits") is not True or float(tag.property("qaHorizontalPadding") or 0) < 7.5:
                raise AssertionError(
                    f"home tag {object_name} is cramped: "
                    f"fits={tag.property('qaTextFits')} padding={tag.property('qaHorizontalPadding')}"
                )

    ok = capture_view("home", "HomeView.qml", setup_home, assert_home_tags) and ok

    def show_home_start_pending(_view, root):
        root.setPending("new-session", True)

    ok = capture_view("home-start-pending", "HomeView.qml", setup_home, show_home_start_pending) and ok

    def show_home_action_error(_view, root):
        root.setProperty("actionError", "Could not start session: daemon offline")

    ok = capture_view("home-action-error", "HomeView.qml", setup_home, show_home_action_error) and ok

    # 2. SessionsView
    def setup_sessions(ctx):
        sessions_model = SessionsModel(client)
        client.session_rows = [
            {
                "id": "s-qa-empty",
                "title": "Empty scratch",
                "dir": "/home/user/eigen",
                "model": "gpt-5",
                "status": "idle",
                "turns": 0,
                "updated": 1783144800000,
            },
            {
                "id": "s-qa-chat",
                "title": "Qt chat controls",
                "dir": "/home/user/eigen/gui-qt",
                "model": "local-qwen",
                "status": "working",
                "turns": 3,
                "updated": 1783155600000,
            },
        ]
        sessions_model._on_sessions_result({"result": [dict(session) for session in client.session_rows]})
        sessions_model.query = "gui"
        return {"sessionsModel": sessions_model}

    ok = capture_view("sessions", "SessionsView.qml", setup_sessions) and ok

    def show_sessions_row_focus(_view, root):
        row = find_item(root, "sessionsRow_s_qa_chat")
        if row is not None:
            row.forceActiveFocus(Qt.TabFocusReason)

    ok = capture_view("sessions-row-focus", "SessionsView.qml", setup_sessions, show_sessions_row_focus) and ok

    def show_sessions_remove_confirm(_view, root):
        root.setConfirming("s-qa-chat", True)

    ok = capture_view("sessions-remove-confirm", "SessionsView.qml", setup_sessions, show_sessions_remove_confirm) and ok

    def show_sessions_remove_pending(_view, root):
        root.setConfirming("s-qa-chat", True)
        model = root.property("sessionsModel")
        if model is not None:
            model._removing.add("s-qa-chat")
            model.removingChanged.emit()
        for _ in range(8):
            app.processEvents()
        confirm = find_item(root, "sessionsRemoveConfirmButton_s_qa_chat")
        cancel = find_item(root, "sessionsRemoveCancelButton_s_qa_chat")
        remove = find_item(root, "sessionsRemoveButton_s_qa_chat")
        if confirm is None or confirm.property("qaText") != "Removing..." or confirm.property("enabled") is not False:
            raise AssertionError("sessions pending remove screenshot did not render a disabled Removing button")
        if cancel is None or cancel.property("enabled") is not False:
            raise AssertionError("sessions pending remove screenshot did not disable cancel")
        if remove is not None and remove.property("visible") is True:
            raise AssertionError("sessions pending remove screenshot fell back to the plain Remove button")

    ok = capture_view("sessions-remove-pending", "SessionsView.qml", setup_sessions, show_sessions_remove_pending) and ok

    def show_sessions_action_message(_view, root):
        model = root.property("sessionsModel")
        if model is not None:
            model._set_action_message("Exported s-qa-chat to /home/user/eigen-exports/s-qa-chat.jsonl")

    ok = capture_view("sessions-action-message", "SessionsView.qml", setup_sessions, show_sessions_action_message) and ok

    def show_sessions_action_error(_view, root):
        model = root.property("sessionsModel")
        if model is not None:
            model._set_action_message("")
            model._set_action_error("Could not remove s-qa-chat: daemon offline")

    ok = capture_view("sessions-action-error", "SessionsView.qml", setup_sessions, show_sessions_action_error) and ok

    # 3. ChatView
    def setup_chat(ctx):
        transcript_model = TranscriptModel(client, "")
        transcript_model._rows = [
            TranscriptRow(kind="user", text="Tighten the Qt chat controls."),
            TranscriptRow(
                kind="assistant",
                text=(
                    "The chat surface now uses the shared native controls.\n\n"
                    "```sh\npytest -q tests/test_chat_controls.py\n```"
                ),
            ),
            TranscriptRow(
                kind="tool",
                tool_name="pytest",
                tool_status="success",
                tool_args="tests/test_chat_controls.py",
            ),
        ]
        transcript_model.layoutChanged.emit()

        session_state = SessionStateModel(client, "")
        session_state.seed(
            {
                "model": "gpt-5",
                "provider": "codex",
                "tokens": 32000,
                "maxTokens": 128000,
                "effort": "medium",
                "perm": "gated",
                "title": "Qt chat controls",
                "goal": "Unify buttons, dropdowns, and copy actions",
                "search": "auto",
                "fast": True,
                "fastOk": True,
                "tools": [
                    {"name": "read_file", "read_only": True},
                    {"name": "run_shell", "read_only": False},
                ],
                "running": False,
                "roots": ["/home/user/eigen"],
                "shells": [
                    {
                        "id": "sh-1",
                        "command": "pytest -q gui-qt/tests/test_chat_controls.py",
                        "status": "running",
                        "exit_code": 0,
                        "last_line": "collecting tests",
                    }
                ],
                "pending": [
                    {"id": "approval-1", "tool": "shell", "args": "{\"cmd\":\"make test\"}"}
                ],
                "catalog": {
                    "models": [
                        {"id": "gpt-5", "effortLevels": ["low", "medium", "high"]},
                        {"id": "local-qwen", "effortLevels": ["low", "medium"]},
                    ]
                },
            }
        )
        approvals_model = ApprovalsModel(client, "")
        commands_model = CommandsModel(client)

        ctx.setContextProperty("rpcClient", client)
        ctx.setContextProperty("transcriptModel", transcript_model)
        ctx.setContextProperty("approvalsModel", approvals_model)
        ctx.setContextProperty("commandsModel", commands_model)
        ctx.setContextProperty("clipboardHelper", clipboard_helper)
        ctx.setContextProperty("highlighter", highlighter)
        ctx.setContextProperty("terminalHelper", terminal_helper)
        return {
            "sessionId": "s-qa-chat",
            "sessionStateModel": session_state,
            "commandsModel": commands_model,
            "transcriptModel": transcript_model,
            "approvalsModel": approvals_model,
            "rpcClient": client,
            "clipboardHelper": clipboard_helper,
            "highlighter": highlighter,
            "terminalHelper": terminal_helper,
        }

    ok = capture_view("chat", "ChatView.qml", setup_chat) and ok

    def show_chat_stream_tail(_view, root):
        model = root.property("transcriptModel")
        if model is not None:
            for i in range(28):
                model.appendNote(f"rapid stream tail row {i + 1}")
        QTest.qWait(80)
        app.processEvents()
        if root.property("qaTranscriptAtBottom") is not True:
            raise AssertionError(
                "Chat transcript did not stay pinned at the tail during rapid inserts"
            )

    ok = capture_view("chat-stream-tail", "ChatView.qml", setup_chat, show_chat_stream_tail) and ok

    def open_chat_diff_dock(_view, root):
        root.setProperty("dockTabIndex", 0)
        root.setProperty("dockOpen", True)
        QTest.qWait(180)
        diff_root = find_item(root, "diffTabRoot")
        branch_tag = find_item(root, "diffBranchTag")
        refresh = find_item(root, "diffRefreshButton")
        if diff_root is None or branch_tag is None or refresh is None:
            raise AssertionError("chat diff dock did not render diff controls")
        if diff_root.property("qaDiffRowCount") < 8:
            raise AssertionError(f"chat diff dock did not render the seeded patch rows: {diff_root.property('qaDiffRowCount')}")
        if branch_tag.property("qaTextFits") is not True:
            raise AssertionError("chat diff dock branch tag text does not fit")
        if refresh.property("qaTextFits") is not True:
            raise AssertionError("chat diff dock refresh button text does not fit")

    ok = capture_view("chat-dock-diff", "ChatView.qml", setup_chat, open_chat_diff_dock) and ok

    def open_chat_files_dock(_view, root):
        root.setProperty("dockTabIndex", 1)
        root.setProperty("dockOpen", True)
        QTest.qWait(180)
        files_root = find_item(root, "filesTabRoot")
        refresh = find_item(root, "filesRefreshButton")
        if files_root is None or refresh is None:
            raise AssertionError("chat files dock did not render files controls")
        if files_root.property("qaTreeRowCount") < 3:
            raise AssertionError(f"chat files dock did not render the seeded tree rows: {files_root.property('qaTreeRowCount')}")
        if refresh.property("qaTextFits") is not True:
            raise AssertionError("chat files dock refresh button text does not fit")
        files_root.setProperty("viewPath", "/home/user/eigen/gui-qt/eigenqt/qml/ChatView.qml")
        files_root.setProperty("viewText", "# ChatView.qml\n\nQt dock visual proof.\n")
        QTest.qWait(120)
        viewer_close = find_item(root, "filesViewerCloseButton")
        if viewer_close is None:
            raise AssertionError("chat files dock viewer did not render close button")
        if files_root.property("qaViewerOpen") is not True or files_root.property("qaViewerCloseFits") is not True:
            raise AssertionError("chat files dock viewer did not expose clean QA geometry")

    ok = capture_view("chat-dock-files", "ChatView.qml", setup_chat, open_chat_files_dock) and ok

    def open_chat_info_dock(_view, root):
        root.setProperty("dockTabIndex", 2)
        root.setProperty("dockOpen", True)
        QTest.qWait(120)
        info_title = find_item(root, "dockInfoTitle")
        info_model = find_item(root, "dockInfoModel")
        info_provider = find_item(root, "dockInfoProvider")
        info_context = find_item(root, "dockInfoContextSummary")
        info_shells = find_item(root, "dockInfoShellsSummary")
        info_pending = find_item(root, "dockInfoPendingSummary")
        info_tools = find_item(root, "dockInfoToolsSummary")
        if (
            info_title is None
            or info_model is None
            or info_provider is None
            or info_context is None
            or info_shells is None
            or info_pending is None
            or info_tools is None
        ):
            raise AssertionError("chat info dock did not render session metadata")
        if info_title.property("text") != "Qt chat controls":
            raise AssertionError(f"chat info dock title was wrong: {info_title.property('text')}")
        if "gpt-5 / medium / gated" not in info_model.property("text"):
            raise AssertionError(f"chat info dock model summary was wrong: {info_model.property('text')}")
        if info_provider.property("text") != "codex":
            raise AssertionError(f"chat info dock provider was wrong: {info_provider.property('text')}")
        if info_context.property("text") != "32,000 / 128,000 (25%)":
            raise AssertionError(f"chat info dock context summary was wrong: {info_context.property('text')}")
        if info_shells.property("text") != "1 shell":
            raise AssertionError(f"chat info dock shell summary was wrong: {info_shells.property('text')}")
        if info_pending.property("text") != "1 approval":
            raise AssertionError(f"chat info dock approval summary was wrong: {info_pending.property('text')}")
        if info_tools.property("text") != "2 tools (1 read, 1 write)":
            raise AssertionError(f"chat info dock tool summary was wrong: {info_tools.property('text')}")

    ok = capture_view("chat-dock-info", "ChatView.qml", setup_chat, open_chat_info_dock) and ok

    def open_chat_browser_dock(_view, root):
        root.setProperty("dockTabIndex", 3)
        root.setProperty("dockOpen", True)
        QTest.qWait(240)
        browser_tab = find_item(root, "browserTab")
        address = find_item(root, "browserAddressField")
        go_button = find_item(root, "browserGoButton")
        empty = find_item(root, "browserEmptyState")
        browser = find_item(root, "browserWebView")
        if browser_tab is None or address is None or go_button is None or empty is None:
            raise AssertionError("chat browser dock did not render browser controls")
        if address.property("text") != "":
            raise AssertionError(f"chat browser dock address was wrong: {address.property('text')}")
        if browser_tab.property("qaBrowserLoaded") is not False or browser is not None:
            raise AssertionError("chat browser dock loaded WebEngine before a page was opened")
        if browser_tab.property("qaEmptyStateVisible") is not True:
            raise AssertionError("chat browser dock did not show the empty state")
        if go_button.property("qaTextFits") is not True:
            raise AssertionError("chat browser dock Go button text did not fit")

    ok = capture_view("chat-dock-browser", "ChatView.qml", setup_chat, open_chat_browser_dock) and ok

    def open_chat_browser_loaded(view, root):
        root.setProperty("dockTabIndex", 3)
        root.setProperty("dockOpen", True)
        QTest.qWait(240)
        browser_tab = find_item(root, "browserTab")
        address = find_item(root, "browserAddressField")
        go_button = find_item(root, "browserGoButton")
        if browser_tab is None or address is None or go_button is None:
            raise AssertionError("chat browser dock did not render controls before navigation")
        proof = Path("/tmp/eigen-qt-browser-proof.html")
        proof.write_text(
            "<body style='background:#091010;color:#e5f8f6;font:16px sans-serif'>Qt browser proof</body>",
            encoding="utf-8",
        )
        address.setProperty("text", proof.as_uri())
        click_item(view, go_button)
        QTest.qWait(600)
        browser = find_item(root, "browserWebView")
        if browser is None or browser_tab.property("qaBrowserLoaded") is not True:
            raise AssertionError("chat browser dock did not lazy-load WebEngine after navigation")
        if browser_tab.property("qaEmptyStateVisible") is not False:
            raise AssertionError("chat browser dock kept the empty state over a loaded page")

    if os.environ.get("EIGEN_QT_SKIP_WEBENGINE_LOADED") == "1":
        print("Skipping chat-dock-browser-loaded (EIGEN_QT_SKIP_WEBENGINE_LOADED=1)")
    else:
        ok = capture_view("chat-dock-browser-loaded", "ChatView.qml", setup_chat, open_chat_browser_loaded) and ok

    def open_chat_terminal_dock(_view, root):
        before_starts = sum(1 for call in client.calls if call[0] == "TerminalStart")
        root.setProperty("dockTabIndex", 4)
        root.setProperty("dockOpen", True)
        QTest.qWait(240)
        terminal_tab = find_item(root, "terminalTab")
        output_area = find_item(root, "terminalOutputArea")
        command_field = find_item(root, "terminalCommandField")
        send_button = find_item(root, "terminalSendButton")
        start_button = find_item(root, "terminalStartButton")
        stop_button = find_item(root, "terminalStopButton")
        clear_button = find_item(root, "terminalClearButton")
        if None in (terminal_tab, output_area, command_field, send_button, start_button, stop_button, clear_button):
            raise AssertionError("chat terminal dock did not render controls")
        if sum(1 for call in client.calls if call[0] == "TerminalStart") <= before_starts:
            raise AssertionError("chat terminal dock did not start a PTY")
        event_data = base64.b64encode(b"$ pytest -q\ncollecting tests\n").decode("ascii")
        client.event.emit("eigen:terminal", {"id": "term-shot", "data": event_data})
        QTest.qWait(160)
        if "collecting tests" not in output_area.property("text"):
            raise AssertionError(f"chat terminal dock did not render decoded output: {output_area.property('text')!r}")
        command_field.setProperty("text", "pwd")
        QTest.qWait(80)
        if send_button.property("qaTextFits") is not True:
            raise AssertionError("chat terminal Send button text did not fit")
        for button, name in (
            (start_button, "Start"),
            (stop_button, "Stop"),
            (clear_button, "Clear"),
        ):
            if button.property("qaTextFits") is not True:
                raise AssertionError(f"chat terminal {name} button text did not fit")

    ok = capture_view("chat-dock-terminal", "ChatView.qml", setup_chat, open_chat_terminal_dock) and ok

    def show_chat_attachment(_view, root):
        root.setProperty("attachedImage", VALID_PNG_BASE64)
        QTest.qWait(80)

    ok = capture_view("chat-attachment", "ChatView.qml", setup_chat, show_chat_attachment) and ok

    def open_chat_model_dropdown(_view, root):
        combo = find_item(root, "sessionModelCombo")
        if combo is not None:
            combo.setProperty("qaPopupOpen", True)

    ok = capture_view("chat-model-dropdown", "ChatView.qml", setup_chat, open_chat_model_dropdown) and ok

    def open_chat_perm_dropdown(_view, root):
        combo = find_item(root, "sessionPermCombo")
        if combo is not None:
            combo.setProperty("qaPopupOpen", True)

    ok = capture_view("chat-perm-dropdown", "ChatView.qml", setup_chat, open_chat_perm_dropdown) and ok

    def open_chat_effort_dropdown(_view, root):
        combo = find_item(root, "sessionEffortCombo")
        if combo is not None:
            combo.setProperty("qaPopupOpen", True)

    ok = capture_view("chat-effort-dropdown", "ChatView.qml", setup_chat, open_chat_effort_dropdown) and ok

    def open_chat_search_dropdown(_view, root):
        combo = find_item(root, "sessionSearchCombo")
        if combo is not None:
            combo.setProperty("qaPopupOpen", True)

    ok = capture_view("chat-search-dropdown", "ChatView.qml", setup_chat, open_chat_search_dropdown) and ok

    def show_chat_action_error(_view, root):
        composer = find_item(root, "chatComposerTextArea")
        if composer is not None:
            composer.setProperty("text", "draft preserved while daemon is offline")
        root.setProperty("actionError", "Could not send message: daemon offline")

    ok = capture_view("chat-action-error", "ChatView.qml", setup_chat, show_chat_action_error) and ok

    def open_chat_slash(_view, root):
        composer = find_item(root, "chatComposerTextArea")
        if composer is not None:
            composer.setProperty("text", "/c")
            composer.setProperty("cursorPosition", 2)
            composer.forceActiveFocus()

    ok = capture_view("chat-slash", "ChatView.qml", setup_chat, open_chat_slash) and ok

    def open_chat_custom_slash(_view, root):
        composer = find_item(root, "chatComposerTextArea")
        if composer is not None:
            composer.setProperty("text", "/sh")
            composer.setProperty("cursorPosition", 3)
            composer.forceActiveFocus()

    ok = capture_view("chat-custom-slash", "ChatView.qml", setup_chat, open_chat_custom_slash) and ok

    def open_chat_fuzzy_slash(_view, root):
        composer = find_item(root, "chatComposerTextArea")
        if composer is not None:
            composer.setProperty("text", "/rvw")
            composer.setProperty("cursorPosition", 4)
            composer.forceActiveFocus()

    ok = capture_view("chat-fuzzy-slash", "ChatView.qml", setup_chat, open_chat_fuzzy_slash) and ok

    def open_chat_empty_slash(_view, root):
        composer = find_item(root, "chatComposerTextArea")
        if composer is not None:
            composer.setProperty("text", "/zzx")
            composer.setProperty("cursorPosition", 4)
            composer.forceActiveFocus()

    ok = capture_view("chat-empty-slash", "ChatView.qml", setup_chat, open_chat_empty_slash) and ok

    def show_chat_slash_note(_view, root):
        model = root.property("transcriptModel")
        if model is not None:
            model.appendNote(
                "model-assessed routing ON\n"
                "goal -> Ship the Qt shell\n"
                "live search -> auto\n"
                "tools:\n"
                "  - read_file\n"
                "  * run_shell\n"
                "copied 47 chars"
            )

    ok = capture_view("chat-slash-note", "ChatView.qml", setup_chat, show_chat_slash_note) and ok

    def show_chat_config_lookup(_view, root):
        model = root.property("transcriptModel")
        if model is not None:
            model.appendNote(
                "route = true\n"
                "Enable model-assessed routing.\n"
                "values: true|false"
            )

    ok = capture_view("chat-config-lookup", "ChatView.qml", setup_chat, show_chat_config_lookup) and ok

    def show_chat_workflow_note(_view, root):
        model = root.property("transcriptModel")
        if model is not None:
            model.appendNote(
                "workflows:\n"
                "  - ship (2 steps) - Prepare a careful release\n"
                "  - audit (1 step) - Review the current diff\n"
                "/workflow <name> [k=v ...]"
            )
            model.appendNote("workflow ship: 2 steps complete")

    ok = capture_view("chat-workflow-note", "ChatView.qml", setup_chat, show_chat_workflow_note) and ok

    def show_chat_ban_note(_view, root):
        model = root.property("transcriptModel")
        if model is not None:
            model.appendNote("/ban <title>: <rule> records a hard prohibition in project memory")
            model.appendNote("banned: No broad rewrites")
            model.appendNote("removed ban: No broad rewrites")

    ok = capture_view("chat-ban-note", "ChatView.qml", setup_chat, show_chat_ban_note) and ok

    def show_chat_legacy_note(_view, root):
        model = root.property("transcriptModel")
        if model is not None:
            model.appendNote("nothing running to background")
            model.appendNote("toggled navigation rail")
            model.appendNote("opened worktree dock; terminal sessions remain in the TUI/web console")
            model.appendNote("background shells are shown in the Info dock")
            model.appendNote("/loop is TUI-local; in the GUI, set a Goal for persistent autonomous follow-up")
            model.appendNote("/mouse is terminal-only; the Qt GUI always allows normal text selection")

    ok = capture_view("chat-legacy-note", "ChatView.qml", setup_chat, show_chat_legacy_note) and ok

    def show_chat_find_note(_view, root):
        model = root.property("transcriptModel")
        if model is not None:
            model.appendNote("find: controls (2 matches in 2 rows)")
            model.appendNote("no matches for missing token")

    ok = capture_view("chat-find-note", "ChatView.qml", setup_chat, show_chat_find_note) and ok

    def show_chat_voice_note(_view, root):
        model = root.property("transcriptModel")
        if model is not None:
            model.appendNote("voice: STT available, TTS missing")
            model.appendNote("voice mode on")
            model.appendNote("speaking last assistant answer")

    ok = capture_view("chat-voice-note", "ChatView.qml", setup_chat, show_chat_voice_note) and ok

    def show_chat_plugin_observe_note(_view, root):
        model = root.property("transcriptModel")
        if model is not None:
            model.appendNote("plugins: 2 installed, 1 marketplaces, 2 hooks\nagent-tools, local-review")
            model.appendNote("observe: 42 records, 1 tools, 1 models, 1 hooks, 1 error groups")

    ok = capture_view("chat-plugin-observe-note", "ChatView.qml", setup_chat, show_chat_plugin_observe_note) and ok

    def show_chat_queue_mode(_view, root):
        root.setProperty("inputMode", "queue")
        root.setProperty("queuedInputs", [{"text": "Follow-up after this turn", "images": []}])
        model = root.property("transcriptModel")
        if model is not None:
            model._rows.append(TranscriptRow(kind="assistant", text="Still working through the current turn...", streaming=True))
            model._sync_streaming_state()
            model.layoutChanged.emit()
        composer = find_item(root, "chatComposerTextArea")
        if composer is not None:
            composer.setProperty("text", "Queue this after the current turn")

    ok = capture_view("chat-queue-mode", "ChatView.qml", setup_chat, show_chat_queue_mode) and ok

    def show_chat_skill_preview(_view, root):
        model = root.property("transcriptModel")
        if model is not None:
            model.appendNote(
                "skill: reviewer\n\n"
                "Use the review tool before publishing risky changes.\n\n"
                "Focus the critique on correctness, UI regressions, and missing proof."
            )

    ok = capture_view("chat-skill-preview", "ChatView.qml", setup_chat, show_chat_skill_preview) and ok

    def show_chat_review_turn(_view, root):
        model = root.property("transcriptModel")
        if model is not None:
            model.appendUserMessage(
                "Use the review tool to get a cross-vendor critique of the current Qt diff. "
                "Package the relevant artifact (the plan, diff, or code) into the tool's "
                "`artifact` argument with enough context to judge it, set an appropriate "
                "`focus`, then act on the critique: fix real issues it raises and note "
                "anything you disagree with and why."
            )

    ok = capture_view("chat-review-turn", "ChatView.qml", setup_chat, show_chat_review_turn) and ok

    def show_chat_approval(_view, root):
        model = root.property("approvalsModel")
        if model is not None:
            model.seed(
                {
                    "pending": [
                        {
                            "id": "approval-qa",
                            "tool": "shell",
                            "args": (
                                '{"command":"pytest -q gui-qt/tests/test_approvals_model.py",'
                                '"cwd":"/home/user/eigen",'
                                '"reason":"Approval arguments stay readable and bounded."}'
                            ),
                        }
                    ]
                }
            )

    ok = capture_view("chat-approval", "ChatView.qml", setup_chat, show_chat_approval) and ok

    # 3. ConfigView
    def assert_config_load_error_state(root, banner_name, text_name, retry_name, expected_text="daemon offline"):
        for _ in range(8):
            app.processEvents()
        banner = find_item(root, banner_name)
        error_text = find_item(root, text_name)
        retry = find_item(root, retry_name)
        if banner is None or banner.property("visible") is not True:
            raise AssertionError(f"{banner_name} did not render")
        if error_text is None or expected_text not in str(error_text.property("text")):
            raise AssertionError(f"{text_name} rendered wrong text: {error_text.property('text') if error_text else None!r}")
        if retry is None or retry.property("qaTextFits") is not True:
            raise AssertionError(f"{retry_name} did not render cleanly")

    def setup_config(ctx):
        client.config_payload = {
            "path": "/home/user/.eigen/config.json",
            "fields": [
                {
                    "key": "model",
                    "desc": "Default model",
                    "value": "gpt-5",
                    "options": ["gpt-5", "local-qwen", "grok-4"],
                    "multi": False,
                    "allowEmpty": False,
                },
                {
                    "key": "perm",
                    "desc": "Permission mode for tool use",
                    "value": "gated",
                    "options": ["gated", "auto", "manual"],
                    "multi": False,
                    "allowEmpty": False,
                },
                {
                    "key": "route",
                    "desc": "Enable task router",
                    "value": "true",
                    "options": ["true", "false"],
                    "multi": False,
                    "allowEmpty": False,
                },
                {
                    "key": "route_providers",
                    "desc": "Providers available to routed subtasks",
                    "value": "openai xai",
                    "options": ["openai", "xai", "local"],
                    "multi": True,
                    "allowEmpty": True,
                },
                {
                    "key": "notify_cmd",
                    "desc": "Command to run when a turn needs attention",
                    "value": "",
                    "options": [],
                    "multi": False,
                    "allowEmpty": True,
                },
            ],
        }
        client.rule_chains_payload = {
            "models": ["gpt-5", "local-qwen", "grok-4"],
            "roles": [
                {
                    "role": "primary",
                    "desc": "Main assistant fallback chain",
                    "chain": ["gpt-5", "local-qwen"],
                    "custom": True,
                },
                {
                    "role": "review",
                    "desc": "Review and critique chain",
                    "chain": ["local-qwen"],
                    "custom": True,
                },
            ],
        }

        config_model = ConfigModel(client)
        config_model._on_config_result({"result": client.config_payload})
        config_model.stop_polling()

        rule_chains_model = RuleChainsModel(client)
        rule_chains_model._on_rule_chains_result({"result": client.rule_chains_payload})
        rule_chains_model.stop_polling()

        ctx.setContextProperty("configModel", config_model)
        ctx.setContextProperty("ruleChainsModel", rule_chains_model)
        return {"configModel": config_model, "ruleChainsModel": rule_chains_model}

    ok = capture_view("config", "ConfigView.qml", setup_config) and ok

    def show_config_load_error(_view, root):
        config_model = root.property("configModel")
        rule_chains_model = root.property("ruleChainsModel")
        config_model.beginResetModel()
        config_model._fields = []
        config_model._config_path = ""
        config_model.configPathChanged.emit()
        config_model.endResetModel()
        rule_chains_model.beginResetModel()
        rule_chains_model._roles = []
        rule_chains_model._models = []
        rule_chains_model.endResetModel()
        config_model._set_load_error("daemon offline")
        rule_chains_model._set_load_error("daemon offline")
        assert_config_load_error_state(root, "configLoadError", "configLoadErrorText", "configLoadErrorRetry")

    ok = capture_view("config-load-error", "ConfigView.qml", setup_config, show_config_load_error) and ok

    def open_config_model_dropdown(_view, root):
        root.setProperty("qaOpenCombo", "configSelect_model")

    ok = capture_view("config-model-dropdown", "ConfigView.qml", setup_config, open_config_model_dropdown) and ok

    def show_config_models(_view, root):
        root.setProperty("activeTab", "Models")

    ok = capture_view("config-models", "ConfigView.qml", setup_config, show_config_models) and ok

    def open_config_chain_dropdown(_view, root):
        root.setProperty("activeTab", "Models")
        root.setProperty("qaOpenCombo", "configAddModelCombo_primary")

    ok = capture_view("config-chain-dropdown", "ConfigView.qml", setup_config, open_config_chain_dropdown) and ok

    def show_config_models_error(_view, root):
        root.setProperty("activeTab", "Models")
        root.setProperty("ruleChainSaving", {"primary": True})
        root.setProperty("actionError", "Could not save primary chain: daemon offline")

    ok = capture_view("config-models-error", "ConfigView.qml", setup_config, show_config_models_error) and ok

    # 4. BoardView
    def setup_board(ctx):
        board_model = BoardModel(client)
        kanban_model = KanbanModel(client)

        client.board_payload = {
            "lanes": [
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
                    ],
                }
            ]
        }
        client.kanban_payload = {
            "columns": [
                {
                    "id": "needs-you",
                    "title": "Needs you",
                    "cards": [
                        {
                            "key": "pr-qt-followup",
                            "kind": "pr",
                            "repo": "avifenesh/eigen",
                            "number": 76,
                            "title": "Qt parity hardening",
                            "url": "https://github.com/avifenesh/eigen/pull/76",
                            "needsYou": True,
                            "session": "s-qa-board",
                            "draft": True,
                            "review": "changes",
                        }
                    ],
                },
                {
                    "id": "done",
                    "title": "Done",
                    "cards": [
                        {
                            "key": "pr-qt-approved",
                            "kind": "pr",
                            "repo": "avifenesh/eigen",
                            "number": 75,
                            "title": "Qt compact shell polish",
                            "url": "https://github.com/avifenesh/eigen/pull/75",
                            "review": "approved",
                        }
                    ],
                },
            ]
        }

        ctx.setContextProperty("boardModel", board_model)
        ctx.setContextProperty("kanbanModel", kanban_model)
        return {"boardModel": board_model, "kanbanModel": kanban_model, "rpcClient": client, "sessionsModel": None}

    ok = capture_view("board", "BoardView.qml", setup_board) and ok

    def show_board_kanban(_view, root):
        root.setProperty("viewMode", "kanban")
        for _ in range(8):
            app.processEvents()
        for object_name in (
            "kanbanSessionTag_pr_qt_followup",
            "kanbanDraftTag_pr_qt_followup",
            "kanbanChangesTag_pr_qt_followup",
            "kanbanApprovedTag_pr_qt_approved",
        ):
            tag = find_item(root, object_name)
            if tag is None or tag.property("qaIsAppTag") is not True:
                raise AssertionError(f"kanban tag {object_name} did not use AppTag")
            if tag.property("qaTextFits") is not True or float(tag.property("qaHorizontalPadding") or 0) < 7.5:
                raise AssertionError(
                    f"kanban tag {object_name} is cramped: "
                    f"fits={tag.property('qaTextFits')} padding={tag.property('qaHorizontalPadding')}"
                )

    ok = capture_view("board-kanban", "BoardView.qml", setup_board, show_board_kanban) and ok

    def show_board_action_error(_view, root):
        for _ in range(8):
            app.processEvents()
        model = root.property("boardModel")
        model._set_action_error("Could not pin lane: daemon offline")
        for _ in range(8):
            app.processEvents()
        banner = find_item(root, "boardActionErrorBanner")
        text = find_item(root, "boardActionErrorText")
        dismiss = find_item(root, "boardActionErrorDismissButton")
        if banner is None or banner.property("visible") is not True:
            raise AssertionError("board action error screenshot did not render the banner")
        if text is None or "daemon offline" not in text.property("text"):
            raise AssertionError("board action error screenshot rendered the wrong text")
        if dismiss is None or dismiss.property("qaTextFits") is not True:
            raise AssertionError("board action error screenshot did not render a clean dismiss button")

    ok = capture_view("board-action-error", "BoardView.qml", setup_board, show_board_action_error) and ok

    # 5. LiveView with an action failure row
    def setup_live(ctx):
        live_rows = [
            {
                "id": "s-qa-live-work",
                "title": "Qt parity review",
                "dir": "/home/user/eigen",
                "model": "gpt-5",
                "status": "working",
                "turns": 18,
                "updated": 1783155600000,
            },
            {
                "id": "s-qa-live-approval",
                "title": "Confirm connector install guardrails",
                "dir": "/home/user/eigen",
                "model": "local-qwen",
                "status": "approval",
                "turns": 7,
                "updated": 1783152000000,
            },
            {
                "id": "s-qa-live-idle",
                "title": "Idle scratch",
                "dir": "/home/user/eigen",
                "model": "gpt-5",
                "status": "idle",
                "turns": 1,
                "updated": 1783144800000,
            },
        ]
        sessions_model = SessionsModel(client)
        sessions_model._on_sessions_result({"result": live_rows})
        live_model = LiveSessionsModel(client)
        live_model._on_sessions_result({"result": live_rows})
        return {"sessionsModel": sessions_model, "liveSessionsModel": live_model, "rpcClient": client}

    def show_live_error(_view, root):
        root.setProperty("actionError", "Interrupt failed: daemon offline")

    ok = capture_view("live-action-error", "LiveView.qml", setup_live, show_live_error) and ok

    def show_live_approval_rpc_unavailable(_view, root):
        root.setProperty("gateOpen", {"s-qa-live-approval": True})
        root.setProperty("gateLoading", {})
        root.setProperty("gatePending", {})
        root.setProperty("gateError", {"s-qa-live-approval": "Could not load approvals: RPC client is unavailable."})

    ok = capture_view("live-approval-rpc-unavailable", "LiveView.qml", setup_live, show_live_approval_rpc_unavailable) and ok

    # 6. TasksView with transcript sheet states
    def setup_tasks(ctx):
        tasks_model = TasksModel(client)
        now = 1_800_000_000_000
        tasks_model._on_agents_result({
            "result": {
                "tasks": [
                    {
                        "id": "task-run",
                        "status": "running",
                        "task": "Harden transcript failure handling",
                        "model": "gpt-5",
                        "role": "frontend",
                        "kind": "fix",
                        "difficulty": "medium",
                        "where": "/home/user/eigen",
                        "startedMs": now - 120_000,
                        "lastTool": "pytest",
                        "steps": 4,
                        "lastNote": "Checking visible error state",
                        "inTokens": 1200,
                        "outTokens": 320,
                    },
                    {
                        "id": "task-done",
                        "status": "done",
                        "task": "Review task transcript layout",
                        "model": "local-qwen",
                        "role": "qa",
                        "kind": "review",
                        "difficulty": "easy",
                        "where": "/home/user/eigen",
                        "startedMs": now - 300_000,
                        "finishedMs": now - 60_000,
                        "result": "done",
                    },
                ]
            }
        })
        tasks_model._poll_timer.stop()
        ctx.setContextProperty("tasksModel", tasks_model)
        ctx.setContextProperty("rpcClient", client)
        return {"tasksModel": tasks_model, "rpcClient": client}

    ok = capture_view("tasks", "TasksView.qml", setup_tasks) and ok

    def show_tasks_cancel_pending(_view, root):
        model = root.property("tasksModel")
        if model:
            model._canceling_ids.add("task-run")
            model._set_task_canceling("task-run", True)

    ok = capture_view("tasks-cancel-pending", "TasksView.qml", setup_tasks, show_tasks_cancel_pending) and ok

    def show_tasks_cancel_error(_view, root):
        model = root.property("tasksModel")
        if model:
            model._set_action_error("Could not cancel task-run: daemon offline")

    ok = capture_view("tasks-cancel-error", "TasksView.qml", setup_tasks, show_tasks_cancel_error) and ok

    def show_tasks_transcript_empty(_view, root):
        root.setProperty(
            "openTask",
            {
                "taskId": "task-run",
                "status": "running",
                "task": "Harden transcript empty-state handling",
                "result": "",
            },
        )
        root.setProperty("transcriptLoading", False)
        root.setProperty("transcriptLoaded", True)
        root.setProperty("transcriptError", "")

    ok = capture_view("tasks-transcript-empty", "TasksView.qml", setup_tasks, show_tasks_transcript_empty) and ok

    def show_tasks_transcript_error(_view, root):
        root.setProperty(
            "openTask",
            {
                "taskId": "task-run",
                "status": "running",
                "task": "Harden transcript failure handling",
                "result": "",
            },
        )
        root.setProperty("transcriptLoading", False)
        root.setProperty("transcriptError", "Could not load transcript: daemon offline")
        for _ in range(4):
            app.processEvents()
        retry = find_item(root, "taskTranscriptErrorRetryButton")
        if retry is None or retry.property("qaTextFits") is not True:
            raise AssertionError("task transcript error retry did not render cleanly")

    ok = capture_view("tasks-transcript-error", "TasksView.qml", setup_tasks, show_tasks_transcript_error) and ok

    def assert_load_error_state(root, banner_name, text_name, retry_name, expected_text="daemon offline"):
        for _ in range(8):
            app.processEvents()
        banner = find_item(root, banner_name)
        error_text = find_item(root, text_name)
        retry = find_item(root, retry_name)
        if banner is None or banner.property("visible") is not True:
            raise AssertionError(f"{banner_name} did not render")
        if error_text is None or expected_text not in str(error_text.property("text")):
            raise AssertionError(f"{text_name} rendered wrong text: {error_text.property('text') if error_text else None!r}")
        if retry is None or retry.property("qaTextFits") is not True:
            raise AssertionError(f"{retry_name} did not render cleanly")

    def assert_refresh_error_state(root, banner_name, text_name, retry_name, initial_name, preserved_name, expected_text="daemon offline"):
        assert_load_error_state(root, banner_name, text_name, retry_name, expected_text)
        initial = find_item(root, initial_name) if initial_name else None
        if initial is not None and initial.property("visible") is True:
            raise AssertionError(f"{banner_name} hid stale content behind {initial_name}")
        preserved = find_item(root, preserved_name)
        if preserved is None or preserved.property("visible") is not True:
            raise AssertionError(f"{banner_name} dropped preserved item {preserved_name}")
        if preserved.property("qaTextFits") is False:
            raise AssertionError(f"{banner_name} left preserved item {preserved_name} clipped")

    # 7. SkillsView with unavailable RPC feedback
    def setup_skills(ctx):
        skills_model = SkillsModel(client)
        skills_model._on_skills_result({
            "result": {
                "skills": [
                    {
                        "name": "frontend-design",
                        "description": "Visual polish checks for Qt surfaces",
                        "source": "user",
                        "path": "/home/user/.eigen/skills/frontend-design/SKILL.md",
                    },
                    {
                        "name": "project-commands",
                        "description": "Project command helpers",
                        "source": "project",
                        "path": "/home/user/eigen/.eigen/skills/project-commands/SKILL.md",
                    },
                ],
                "proposals": [],
            }
        })
        skills_model._poll_timer.stop()
        skills_model._active = True
        proposals_model = ProposalsModel(client)
        proposals_model._on_skills_result({"result": {"proposals": []}})
        proposals_model._poll_timer.stop()
        proposals_model._active = True
        ctx.setContextProperty("skillsModel", skills_model)
        ctx.setContextProperty("proposalsModel", proposals_model)
        ctx.setContextProperty("markdownParser", markdown_parser)
        ctx.setContextProperty("highlighter", highlighter)
        ctx.setContextProperty("clipboardHelper", clipboard_helper)
        return {"skillsModel": skills_model, "proposalsModel": proposals_model}

    def setup_skills_with_proposal(ctx):
        props = setup_skills(ctx)
        props["proposalsModel"]._on_skills_result(
            {
                "result": {
                    "proposals": [
                        {
                            "name": "qt-qa",
                            "description": "Visual QML proof for pending proposal actions",
                            "path": "/tmp/qt-qa/SKILL.md",
                        }
                    ]
                }
            }
        )
        return props

    def show_skills_markdown_preview(_view, root):
        body = (
            "# frontend-design\n\n"
            "Render **SKILL.md** as structured markdown, not a raw text blob.\n\n"
            "- Headings keep hierarchy\n"
            "- Lists stay scannable\n\n"
            "```bash\n"
            "eigen skill add ./SKILL.md\n"
            "```"
        )
        root.setProperty(
            "openSkill",
            {
                "name": "frontend-design",
                "description": "Visual polish checks for Qt surfaces",
                "source": "user",
                "path": "/home/user/.eigen/skills/frontend-design/SKILL.md",
            },
        )
        root.setProperty("bodyLoading", False)
        root.setProperty("body", body)
        root.setProperty("bodyBlocks", markdown_parser.parse(body))

    ok = capture_view("skills-markdown-preview", "SkillsView.qml", setup_skills, show_skills_markdown_preview) and ok

    def show_skills_remove_confirm(_view, root):
        show_skills_markdown_preview(_view, root)
        root.setProperty("confirmRemove", True)

    ok = capture_view("skills-remove-confirm", "SkillsView.qml", setup_skills, show_skills_remove_confirm) and ok

    def show_skills_remove_pending(_view, root):
        show_skills_markdown_preview(_view, root)
        root.setProperty("confirmRemove", True)
        root.setProperty("removing", True)

    ok = capture_view("skills-remove-pending", "SkillsView.qml", setup_skills, show_skills_remove_pending) and ok

    def show_skills_proposal_pending(_view, root):
        root.setProperty("acting", {"qt-qa": "accept"})
        accept = find_item(root, "proposalAcceptButton_qt-qa")
        reject = find_item(root, "proposalRejectButton_qt-qa")
        if accept is None or accept.property("qaText") != "Accepting…" or accept.property("enabled") is not False:
            raise AssertionError("skills pending proposal screenshot did not render a disabled Accepting button")
        if accept.property("qaTextFits") is not True:
            raise AssertionError("skills pending proposal accept button text does not fit")
        if reject is None or reject.property("enabled") is not False or reject.property("qaTextFits") is not True:
            raise AssertionError("skills pending proposal screenshot did not disable the sibling reject action")

    ok = capture_view("skills-proposal-pending", "SkillsView.qml", setup_skills_with_proposal, show_skills_proposal_pending) and ok

    def show_skills_rpc_error(_view, root):
        root.setProperty(
            "openSkill",
            {
                "name": "frontend-design",
                "description": "Visual polish checks for Qt surfaces",
                "source": "user",
                "path": "/home/user/.eigen/skills/frontend-design/SKILL.md",
            },
        )
        root.setProperty("bodyLoading", False)
        root.setProperty("actionError", "Could not load skill body: RPC client is unavailable.")

    ok = capture_view("skills-rpc-error", "SkillsView.qml", setup_skills, show_skills_rpc_error) and ok

    def show_skills_load_error(_view, root):
        skills_model = root.property("skillsModel")
        proposals_model = root.property("proposalsModel")
        skills_model.beginResetModel()
        skills_model._skills = []
        skills_model.endResetModel()
        proposals_model.beginResetModel()
        proposals_model._proposals = []
        proposals_model.endResetModel()
        skills_model._set_load_error("daemon offline")
        proposals_model._set_load_error("daemon offline")
        assert_load_error_state(root, "skillsLoadError", "skillsLoadErrorText", "skillsLoadErrorRetry")

    ok = capture_view("skills-load-error", "SkillsView.qml", setup_skills, show_skills_load_error) and ok

    # 8. ReviewersView
    reviewer_rows = [
        {"repo": "avifenesh/eigen", "paused": False},
        {"repo": "avifenesh/revuto", "paused": True},
    ]

    def seed_reviewers_state(model, rows=None, *, available=True, loading=False):
        rows = list(rows or [])
        model.stop_polling()
        model._available = available
        model._count = len(rows)
        model._paused = sum(1 for row in rows if row.get("paused", False))
        model.beginResetModel()
        model._reviewers = rows
        model.endResetModel()
        model._set_loading(loading)
        model.status_changed.emit()

    def setup_reviewers(ctx):
        reviewers_model = ReviewersModel(client)
        ctx.setContextProperty("reviewersModel", reviewers_model)
        return {"reviewersModel": reviewers_model}

    def show_reviewers(_view, root):
        seed_reviewers_state(root.property("reviewersModel"), reviewer_rows)

    ok = capture_view("reviewers", "ReviewersView.qml", setup_reviewers, show_reviewers) and ok

    def show_reviewers_loading(_view, root):
        seed_reviewers_state(root.property("reviewersModel"), [], available=False, loading=True)

    ok = capture_view("reviewers-loading", "ReviewersView.qml", setup_reviewers, show_reviewers_loading) and ok

    def show_reviewers_action_error(_view, root):
        seed_reviewers_state(root.property("reviewersModel"), reviewer_rows)
        root.setProperty("actionError", "Could not run learn for avifenesh/eigen: revuto unavailable")

    ok = capture_view("reviewers-action-error", "ReviewersView.qml", setup_reviewers, show_reviewers_action_error) and ok

    def show_reviewers_review_pending(_view, root):
        seed_reviewers_state(root.property("reviewersModel"), reviewer_rows)
        root.setProperty("busy", {"avifenesh/eigen": True})
        root.setProperty("busyAction", {"avifenesh/eigen": "review"})
        review = find_item(root, "reviewerReviewButton_avifenesh_eigen")
        learn = find_item(root, "reviewerLearnButton_avifenesh_eigen")
        pause = find_item(root, "reviewerPauseButton_avifenesh_eigen")
        if review is None or review.property("qaText") != "Reviewing…" or review.property("enabled") is not False:
            raise AssertionError("reviewers pending review screenshot did not render a disabled Reviewing button")
        if review.property("qaTextFits") is not True:
            raise AssertionError("reviewers pending review button text does not fit")
        if learn is None or learn.property("enabled") is not False or pause is None or pause.property("enabled") is not False:
            raise AssertionError("reviewers pending review screenshot did not disable sibling actions")

    ok = capture_view("reviewers-review-pending", "ReviewersView.qml", setup_reviewers, show_reviewers_review_pending) and ok

    def show_reviewers_load_error(_view, root):
        model = root.property("reviewersModel")
        seed_reviewers_state(model, [], available=False, loading=False)
        model._set_load_error("daemon offline")
        assert_load_error_state(root, "reviewersLoadError", "reviewersLoadErrorText", "reviewersLoadErrorRetry")

    ok = capture_view("reviewers-load-error", "ReviewersView.qml", setup_reviewers, show_reviewers_load_error) and ok

    # 9. ObserveView
    def setup_observe(ctx):
        observe_model = ObserveModel(client)
        ctx.setContextProperty("observeModel", observe_model)
        return {"observeModel": observe_model}

    def show_observe(_view, root):
        for _ in range(8):
            app.processEvents()
        if root.property("qaRecordCount") != 4:
            raise AssertionError(f"observe screenshot expected 4 records, saw {root.property('qaRecordCount')}")
        route_mix = find_item(root, "observeRouteMix")
        tool_row = find_item(root, "observeToolRow_read_file")
        model_row = find_item(root, "observeModelRow_gpt_5")
        error_row = find_item(root, "observeErrorRow_rpc_timeout")
        if route_mix is None or route_mix.property("visible") is not True:
            raise AssertionError("observe screenshot did not render route mix")
        if tool_row is None or tool_row.property("qaTextFits") is not True:
            raise AssertionError("observe screenshot did not render a clean tool row")
        if model_row is None or model_row.property("qaTextFits") is not True:
            raise AssertionError("observe screenshot did not render a clean model row")
        if error_row is None or error_row.property("qaTextFits") is not True:
            raise AssertionError("observe screenshot did not render a clean error row")

    ok = capture_view("observe", "ObserveView.qml", setup_observe, show_observe) and ok

    def show_observe_load_error(_view, root):
        model = root.property("observeModel")
        model._summary = {}
        model.summary_changed.emit()
        model._set_loading(False)
        model._set_load_error("daemon offline")
        assert_load_error_state(root, "observeLoadError", "observeLoadErrorText", "observeLoadErrorRetry")

    ok = capture_view("observe-load-error", "ObserveView.qml", setup_observe, show_observe_load_error) and ok

    def show_observe_refresh_error(_view, root):
        for _ in range(8):
            app.processEvents()
        model = root.property("observeModel")
        model._set_loading(False)
        model._set_load_error("daemon offline")
        assert_refresh_error_state(
            root,
            "observeRefreshErrorBanner",
            "observeRefreshErrorText",
            "observeRefreshErrorRetry",
            "observeLoadError",
            "observeToolRow_read_file",
        )

    ok = capture_view("observe-refresh-error", "ObserveView.qml", setup_observe, show_observe_refresh_error) and ok

    # 10. RoutingView
    def setup_routing(ctx):
        routing_model = RoutingModel(client)
        ctx.setContextProperty("routingModel", routing_model)
        return {"routingModel": routing_model}

    def show_routing(_view, root):
        for _ in range(8):
            app.processEvents()
        if root.property("qaFilteredModelCount") != 3:
            raise AssertionError(f"routing screenshot expected 3 models, saw {root.property('qaFilteredModelCount')}")
        health = find_item(root, "routingHealthStrip")
        provider = find_item(root, "routingProvider_grok")
        model_card = find_item(root, "routingModelCard_gpt_5")
        if health is None or health.property("visible") is not True:
            raise AssertionError("routing screenshot did not render the route-health strip")
        if provider is None or provider.property("qaTextFits") is not True:
            raise AssertionError("routing screenshot did not render a clean provider row")
        if model_card is None or model_card.property("qaTextFits") is not True:
            raise AssertionError("routing screenshot did not render a clean model card")

    ok = capture_view("routing", "RoutingView.qml", setup_routing, show_routing) and ok

    def show_routing_refresh_error(_view, root):
        for _ in range(8):
            app.processEvents()
        model = root.property("routingModel")
        model._on_routing_result({"error": {"message": "daemon offline"}})
        for _ in range(8):
            app.processEvents()
        banner = find_item(root, "routingRefreshErrorBanner")
        retry = find_item(root, "routingRefreshErrorRetry")
        model_card = find_item(root, "routingModelCard_gpt_5")
        if banner is None or banner.property("visible") is not True:
            raise AssertionError("routing refresh error screenshot did not render the banner")
        if banner.property("qaTextFits") is not True or "daemon offline" not in banner.property("qaErrorText"):
            raise AssertionError("routing refresh error screenshot rendered clipped or wrong text")
        if retry is None or retry.property("qaTextFits") is not True:
            raise AssertionError("routing refresh error screenshot did not render a clean retry button")
        if root.property("qaFilteredModelCount") != 3 or model_card is None:
            raise AssertionError("routing refresh error screenshot dropped the catalog")

    ok = capture_view("routing-refresh-error", "RoutingView.qml", setup_routing, show_routing_refresh_error) and ok

    # 11. MachinesView
    def setup_machines(ctx):
        machines_model = MachinesModel(client)
        ctx.setContextProperty("machinesModel", machines_model)
        return {"machinesModel": machines_model}

    def show_machines(_view, root):
        for _ in range(8):
            app.processEvents()
        if root.property("qaMachineCount") != 2:
            raise AssertionError(f"machines screenshot expected 2 hosts, saw {root.property('qaMachineCount')}")
        codex_card = find_item(root, "machinesCard_codex_box")
        lab_card = find_item(root, "machinesCard_lab_node")
        if codex_card is None or lab_card is None:
            raise AssertionError("machines screenshot did not render host cards")
        if codex_card.property("qaTextFits") is not True or lab_card.property("qaTextFits") is not True:
            raise AssertionError("machines screenshot rendered clipped host text")
        root.property("machinesModel").select_machine("codex-box")
        for _ in range(12):
            app.processEvents()
        if root.property("qaRemoteCount") != 2:
            raise AssertionError(f"machines screenshot expected 2 remote sessions, saw {root.property('qaRemoteCount')}")
        remote_row = find_item(root, "machinesRemoteRow_remote_codex_box_s1")
        remote_open = find_item(root, "machinesRemoteOpen_remote_codex_box_s1")
        if remote_row is None or remote_open is None:
            raise AssertionError("machines screenshot did not render remote sessions")
        if remote_row.property("qaTextFits") is not True or remote_open.property("qaTextFits") is not True:
            raise AssertionError("machines screenshot rendered clipped remote session text")

    ok = capture_view("machines", "MachinesView.qml", setup_machines, show_machines) and ok

    def show_machines_load_error(_view, root):
        model = root.property("machinesModel")
        model._machines = []
        model.machines_changed.emit()
        model.summary_changed.emit()
        model._set_loading(False)
        model._set_load_error("daemon offline")
        assert_load_error_state(root, "machinesLoadError", "machinesLoadErrorText", "machinesLoadErrorRetry")

    ok = capture_view("machines-load-error", "MachinesView.qml", setup_machines, show_machines_load_error) and ok

    def show_machines_refresh_error(_view, root):
        for _ in range(8):
            app.processEvents()
        model = root.property("machinesModel")
        model._set_loading(False)
        model._set_load_error("daemon offline")
        assert_refresh_error_state(
            root,
            "machinesRefreshErrorBanner",
            "machinesRefreshErrorText",
            "machinesRefreshErrorRetry",
            "machinesLoadError",
            "machinesCard_codex_box",
        )

    ok = capture_view("machines-refresh-error", "MachinesView.qml", setup_machines, show_machines_refresh_error) and ok

    # 12. CronsView
    def setup_crons(ctx):
        crons_model = CronsModel(client)
        ctx.setContextProperty("cronsModel", crons_model)
        return {"cronsModel": crons_model}

    def show_crons(_view, root):
        for _ in range(8):
            app.processEvents()
        if root.property("qaTimerCount") != 2 or root.property("qaCrontabCount") != 1:
            raise AssertionError(
                "crons screenshot expected 2 timers and 1 crontab row, saw "
                f"{root.property('qaTimerCount')} timers and {root.property('qaCrontabCount')} crontab"
            )
        timer_row = find_item(root, "cronsTimerRow_eigen_dream_timer")
        tab_row = find_item(root, "cronsTabRow_0_9_______eigen_run_daily")
        if timer_row is None or tab_row is None:
            raise AssertionError("crons screenshot did not render timer and crontab rows")
        if timer_row.property("qaTextFits") is not True or tab_row.property("qaTextFits") is not True:
            raise AssertionError("crons screenshot rendered clipped schedule text")

    ok = capture_view("crons", "CronsView.qml", setup_crons, show_crons) and ok

    def show_crons_load_error(_view, root):
        model = root.property("cronsModel")
        model._crons = []
        model._timers = 0
        model._crontab = 0
        model.crons_changed.emit()
        model.summary_changed.emit()
        model._set_loading(False)
        model._set_load_error("daemon offline")
        assert_load_error_state(root, "cronsLoadError", "cronsLoadErrorText", "cronsLoadErrorRetry")

    ok = capture_view("crons-load-error", "CronsView.qml", setup_crons, show_crons_load_error) and ok

    def show_crons_refresh_error(_view, root):
        for _ in range(8):
            app.processEvents()
        model = root.property("cronsModel")
        model._set_loading(False)
        model._set_load_error("daemon offline")
        assert_refresh_error_state(
            root,
            "cronsRefreshErrorBanner",
            "cronsRefreshErrorText",
            "cronsRefreshErrorRetry",
            "cronsLoadError",
            "cronsTimerRow_eigen_dream_timer",
        )

    ok = capture_view("crons-refresh-error", "CronsView.qml", setup_crons, show_crons_refresh_error) and ok

    # 13. PluginsView
    def setup_plugins(ctx):
        plugins_model = PluginsModel(client)
        ctx.setContextProperty("pluginsModel", plugins_model)
        return {"pluginsModel": plugins_model}

    def show_plugins(_view, root):
        for _ in range(8):
            app.processEvents()
        if root.property("qaPluginCount") != 2 or root.property("qaMarketplaceCount") != 2:
            raise AssertionError(
                "plugins screenshot expected 2 plugins and 2 marketplaces, saw "
                f"{root.property('qaPluginCount')} plugins and {root.property('qaMarketplaceCount')} marketplaces"
            )
        installed_row = find_item(root, "pluginsInstalledRow_agentsys")
        risk_row = find_item(root, "pluginsInstalledRow_local_risk")
        market_row = find_item(root, "pluginsMarketRow_core")
        if installed_row is None or risk_row is None or market_row is None:
            raise AssertionError("plugins screenshot did not render installed and marketplace rows")
        if installed_row.property("qaTextFits") is not True or risk_row.property("qaTextFits") is not True or market_row.property("qaTextFits") is not True:
            raise AssertionError("plugins screenshot rendered clipped inventory text")

    ok = capture_view("plugins", "PluginsView.qml", setup_plugins, show_plugins) and ok

    def show_plugins_load_error(_view, root):
        model = root.property("pluginsModel")
        model._plugins = []
        model._marketplaces = []
        model.plugins_changed.emit()
        model.marketplaces_changed.emit()
        model.summary_changed.emit()
        model._set_loading(False)
        model._set_load_error("daemon offline")
        assert_load_error_state(root, "pluginsLoadError", "pluginsLoadErrorText", "pluginsLoadErrorRetry")

    ok = capture_view("plugins-load-error", "PluginsView.qml", setup_plugins, show_plugins_load_error) and ok

    def show_plugins_refresh_error(_view, root):
        for _ in range(8):
            app.processEvents()
        model = root.property("pluginsModel")
        model._set_loading(False)
        model._set_load_error("daemon offline")
        assert_refresh_error_state(
            root,
            "pluginsRefreshErrorBanner",
            "pluginsRefreshErrorText",
            "pluginsRefreshErrorRetry",
            "pluginsLoadError",
            "pluginsInstalledRow_agentsys",
        )

    ok = capture_view("plugins-refresh-error", "PluginsView.qml", setup_plugins, show_plugins_refresh_error) and ok

    # 14. ProfileView
    def setup_profile(ctx):
        profile_model = ProfileModel(client)
        stats = {"sessions": 7}
        ctx.setContextProperty("profileModel", profile_model)
        ctx.setContextProperty("statsData", stats)
        ctx.setContextProperty("markdownParser", markdown_parser)
        ctx.setContextProperty("clipboardHelper", clipboard_helper)
        ctx.setContextProperty("highlighter", highlighter)
        return {"profileModel": profile_model, "statsData": stats}

    def show_profile(_view, root):
        for _ in range(8):
            app.processEvents()
        if root.property("qaRecordCount") != 4 or root.property("qaModelCount") != 1:
            raise AssertionError(
                "profile screenshot expected 4 records and 1 model, saw "
                f"{root.property('qaRecordCount')} records and {root.property('qaModelCount')} models"
            )
        sessions_kpi = find_item(root, "profileKpi_sessions")
        turns_kpi = find_item(root, "profileKpi_turns")
        model_row = find_item(root, "profileModelRow_gpt_5")
        profile_card = find_item(root, "profileUserCard")
        edit_button = find_item(root, "profileEditButton")
        if sessions_kpi is None or turns_kpi is None or model_row is None or profile_card is None:
            raise AssertionError("profile screenshot did not render usage/profile sections")
        if sessions_kpi.property("qaTextFits") is not True or turns_kpi.property("qaTextFits") is not True:
            raise AssertionError("profile screenshot rendered clipped KPI text")
        if model_row.property("qaTextFits") is not True:
            raise AssertionError("profile screenshot rendered clipped model row")
        if edit_button is None or edit_button.property("qaTextFits") is not True:
            raise AssertionError("profile screenshot did not render a clean edit button")

    ok = capture_view("profile", "ProfileView.qml", setup_profile, show_profile) and ok

    def show_profile_summary_refresh_error(_view, root):
        for _ in range(8):
            app.processEvents()
        model = root.property("profileModel")
        model._set_summary_loading(False)
        model._set_summary_error("daemon offline")
        assert_refresh_error_state(
            root,
            "profileSummaryRefreshErrorBanner",
            "profileSummaryRefreshErrorText",
            "profileSummaryRefreshErrorRetry",
            "",
            "profileModelRow_gpt_5",
        )

    ok = capture_view("profile-summary-refresh-error", "ProfileView.qml", setup_profile, show_profile_summary_refresh_error) and ok

    def show_profile_action_error(_view, root):
        model = root.property("profileModel")
        if model is not None:
            model.start_edit()
            model._set_action_error("Could not save profile: daemon offline")
        for _ in range(8):
            app.processEvents()
        banner = find_item(root, "profileActionError")
        text = find_item(root, "profileActionErrorText")
        dismiss = find_item(root, "profileDismissActionError")
        if banner is None or banner.property("visible") is not True:
            raise AssertionError("profile action error screenshot did not render the banner")
        if text is None or "daemon offline" not in text.property("text"):
            raise AssertionError("profile action error screenshot rendered the wrong text")
        if dismiss is None or dismiss.property("qaTextFits") is not True:
            raise AssertionError("profile action error screenshot did not render a clean dismiss button")

    ok = capture_view("profile-action-error", "ProfileView.qml", setup_profile, show_profile_action_error) and ok

    # 15. ConnectorsView
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
        connectors_model._servers = {
            "servers": [
                {
                    "name": "github-local",
                    "description": "GitHub MCP",
                    "command": ["uvx", "github-mcp-server"],
                    "remote": False,
                    "disabled": False,
                    "secretEnvKeys": ["GITHUB_TOKEN"],
                }
            ]
        }
        connectors_model.connectors_changed.emit()
        connectors_model.servers_changed.emit()
        ctx.setContextProperty("connectorsModel", connectors_model)
        return {"connectorsModel": connectors_model}

    ok = capture_view("connectors", "ConnectorsView.qml", setup_connectors) and ok

    def show_connectors_load_error(_view, root):
        model = root.property("connectorsModel")
        if model is not None:
            model.connectors = None
            model.servers = None
            model.loading = False
            model.load_error = "daemon offline"
        for _ in range(8):
            app.processEvents()
        banner = find_item(root, "connectorsLoadError")
        text = find_item(root, "connectorsLoadErrorText")
        retry = find_item(root, "connectorsLoadErrorRetry")
        if banner is None or banner.property("visible") is not True:
            raise AssertionError("connectors load error screenshot did not render the banner")
        if text is None or "daemon offline" not in text.property("text"):
            raise AssertionError("connectors load error screenshot rendered the wrong text")
        if retry is None or retry.property("qaTextFits") is not True:
            raise AssertionError("connectors load error screenshot did not render a clean retry button")

    ok = capture_view("connectors-load-error", "ConnectorsView.qml", setup_connectors, show_connectors_load_error) and ok

    def show_connectors_refresh_error(_view, root):
        model = root.property("connectorsModel")
        if model is not None:
            model.loading = False
            model.load_error = "daemon offline"
        for _ in range(8):
            app.processEvents()
        initial = find_item(root, "connectorsLoadError")
        banner = find_item(root, "connectorsRefreshErrorBanner")
        text = find_item(root, "connectorsRefreshErrorText")
        retry = find_item(root, "connectorsRefreshErrorRetry")
        card = find_item(root, "connectorCard_connector_notion")
        if initial is not None and initial.property("visible") is True:
            raise AssertionError("connectors refresh error screenshot hid stale content behind the initial error")
        if banner is None or banner.property("visible") is not True:
            raise AssertionError("connectors refresh error screenshot did not render the banner")
        if text is None or "daemon offline" not in text.property("text"):
            raise AssertionError("connectors refresh error screenshot rendered the wrong text")
        if retry is None or retry.property("qaTextFits") is not True:
            raise AssertionError("connectors refresh error screenshot did not render a clean retry button")
        if card is None or card.property("visible") is not True:
            raise AssertionError("connectors refresh error screenshot dropped stale connector content")

    ok = capture_view("connectors-refresh-error", "ConnectorsView.qml", setup_connectors, show_connectors_refresh_error) and ok

    def show_connectors_remove_confirm(_view, root):
        model = root.property("connectorsModel")
        if model is not None:
            model.confirm_remove_connector_set("notion")
            model.confirm_remove_server_set("github-local")
        for _ in range(4):
            app.processEvents()

        for button_name in (
            "connectorPrimaryButton_connector_notion",
            "connectorConfirmRemoveButton_connector_notion",
            "connectorCancelRemoveButton_connector_notion",
            "connectorPrimaryButton_server_github-local",
            "connectorConfirmRemoveButton_server_github-local",
            "connectorCancelRemoveButton_server_github-local",
        ):
            button = find_item(root, button_name)
            if button is None or button.property("visible") is not True:
                raise AssertionError(f"connectors remove confirm missing {button_name}")
            if button.property("qaTextFits") is not True:
                raise AssertionError(f"connectors remove confirm text clipped in {button_name}")
            if scene_right(button) > float(root.property("width") or 0) + 0.5:
                raise AssertionError(
                    f"connectors remove confirm overflowed {button_name}: "
                    f"right={scene_right(button):.1f}, root={float(root.property('width') or 0):.1f}"
                )

    ok = capture_view("connectors-remove-confirm", "ConnectorsView.qml", setup_connectors, show_connectors_remove_confirm) and ok
    ok = capture_view(
        "connectors-narrow-remove-confirm",
        "ConnectorsView.qml",
        setup_connectors,
        show_connectors_remove_confirm,
        width=380,
        height=760,
    ) and ok

    def show_connectors_action_error(_view, root):
        model = root.property("connectorsModel")
        if model is not None:
            model.add_open = True
            model.add_name = "linear"
            model.add_url = "https://mcp.linear.app/mcp"
            model.add_desc = "Linear issues"
            model.action_error = "Could not add connector linear: add denied"
        for _ in range(4):
            app.processEvents()
        dismiss = find_item(root, "connectorsDismissActionError")
        if dismiss is None or dismiss.property("qaTextFits") is not True:
            raise AssertionError("connector add-form error dismiss button did not render cleanly")

    ok = capture_view("connectors-action-error", "ConnectorsView.qml", setup_connectors, show_connectors_action_error) and ok

    def show_connectors_server_action_error(_view, root):
        model = root.property("connectorsModel")
        if model is not None:
            model.srv_open = True
            model.srv_name = "linear-local"
            model.srv_command = "uvx linear-mcp"
            model.srv_desc = "Local Linear MCP"
            model.action_error = "Could not save server linear-local: daemon offline"
        for _ in range(4):
            app.processEvents()
        flick = find_item(root, "connectorsFlick")
        if flick is not None:
            flick.setProperty("contentY", 520)
            for _ in range(4):
                app.processEvents()
        dismiss = find_item(root, "connectorsDismissServerActionError")
        if dismiss is None or dismiss.property("qaTextFits") is not True:
            raise AssertionError("connector server error dismiss button did not render cleanly")

    ok = capture_view("connectors-server-action-error", "ConnectorsView.qml", setup_connectors, show_connectors_server_action_error) and ok

    # 10. NotesView
    def setup_notes(ctx, editing=False):
        notes_controller = NotesController(client)
        notes_controller.status = {"available": True, "vault": "/home/user/notes"}

        # Mock notes
        from eigenqt.models.notes import NotesModel
        notes_controller._notes_model = NotesModel(client)
        notes_controller._notes_model._notes = [
            {"path": "Inbox/Ideas.md", "title": "Project ideas"},
            {"path": "Daily/2026-07-02.md", "title": "2026-07-02"},
        ]
        notes_controller._notes_model.layoutChanged.emit()
        notes_controller.selected = {"path": "Inbox/Ideas.md", "title": "Project ideas"}
        notes_controller.content = "# Project ideas\n\nUse the Qt follow-up for focused surface hardening."
        notes_controller.editing = editing
        if editing:
            notes_controller.draft = notes_controller.content + "\n\nEditing action controls stay aligned."

        ctx.setContextProperty("notesController", notes_controller)
        ctx.setContextProperty("markdownParser", markdown_parser)
        ctx.setContextProperty("highlighter", highlighter)
        ctx.setContextProperty("clipboardHelper", clipboard_helper)
        return {"notesController": notes_controller}

    ok = capture_view("notes", "NotesView.qml", setup_notes) and ok
    ok = capture_view("notes-edit", "NotesView.qml", lambda ctx: setup_notes(ctx, editing=True)) and ok

    def show_notes_save_pending(_view, root):
        notes_controller = root.property("notesController")
        if notes_controller is not None:
            notes_controller.saving = True

    ok = capture_view(
        "notes-save-pending",
        "NotesView.qml",
        lambda ctx: setup_notes(ctx, editing=True),
        show_notes_save_pending,
    ) and ok

    def show_notes_save_error(_view, root):
        notes_controller = root.property("notesController")
        if notes_controller is not None:
            notes_controller.action_error = "Could not save Inbox/Ideas.md: save denied"

    ok = capture_view(
        "notes-save-error",
        "NotesView.qml",
        lambda ctx: setup_notes(ctx, editing=True),
        show_notes_save_error,
    ) and ok

    def show_notes_status_refresh_error(_view, root):
        notes_controller = root.property("notesController")
        if notes_controller is not None:
            notes_controller.status_error = "daemon offline"
        for _ in range(6):
            app.processEvents()
        initial = find_item(root, "notesStatusLoadError")
        banner = find_item(root, "notesStatusRefreshErrorBanner")
        text = find_item(root, "notesStatusRefreshErrorText")
        retry = find_item(root, "notesStatusRefreshErrorRetry")
        new_button = find_item(root, "notesNewButton")
        if initial is not None and initial.property("visible") is True:
            raise AssertionError("notes status refresh screenshot hid stale notes behind the initial error")
        if banner is None or banner.property("visible") is not True:
            raise AssertionError("notes status refresh screenshot did not render the banner")
        if text is None or "daemon offline" not in text.property("text"):
            raise AssertionError("notes status refresh screenshot rendered the wrong text")
        if retry is None or retry.property("qaTextFits") is not True:
            raise AssertionError("notes status refresh screenshot did not render a clean retry button")
        if new_button is None or new_button.property("visible") is not True:
            raise AssertionError("notes status refresh screenshot dropped usable note controls")

    ok = capture_view(
        "notes-status-refresh-error",
        "NotesView.qml",
        setup_notes,
        show_notes_status_refresh_error,
    ) and ok

    def setup_notes_load_error(ctx):
        notes_controller = NotesController(client)
        notes_controller.status = {"available": True, "vault": "/home/user/notes"}

        from eigenqt.models.notes import NotesModel
        notes_controller._notes_model = NotesModel(client)
        notes_controller._notes_model._set_error("daemon offline")

        ctx.setContextProperty("notesController", notes_controller)
        ctx.setContextProperty("markdownParser", markdown_parser)
        ctx.setContextProperty("highlighter", highlighter)
        ctx.setContextProperty("clipboardHelper", clipboard_helper)
        return {"notesController": notes_controller}

    ok = capture_view("notes-load-error", "NotesView.qml", setup_notes_load_error) and ok

    # 11. DreamingView
    def setup_dreaming(ctx):
        dreaming_model = DreamingModel(client)
        ctx.setContextProperty("dreamingModel", dreaming_model)
        return {"dreamingModel": dreaming_model}

    def show_dreaming(_view, root):
        for _ in range(10):
            app.processEvents()
        if root.property("qaRolloutCount") != 2 or root.property("qaConsolidationCount") != 1:
            raise AssertionError(
                "dreaming screenshot expected 2 rollouts and 1 consolidation, saw "
                f"{root.property('qaRolloutCount')} rollouts and {root.property('qaConsolidationCount')} consolidations"
            )
        combo = find_item(root, "dreamingScopeCombo")
        rollout_row = find_item(root, "dreamingRolloutRow_1")
        tab = find_item(root, "dreamingTab_consolidations")
        if combo is None or rollout_row is None or tab is None:
            raise AssertionError("dreaming screenshot did not render scope, rollout row, and tab")
        if combo.property("qaTextFits") is not True or rollout_row.property("qaTextFits") is not True:
            raise AssertionError("dreaming screenshot rendered clipped rollout text")
        root.setProperty("strand", "consolidations")
        for _ in range(8):
            app.processEvents()
        cons_row = find_item(root, "dreamingConsolidationRow_0")
        if cons_row is None or cons_row.property("qaTextFits") is not True:
            raise AssertionError("dreaming screenshot did not render a clean consolidation row")

    ok = capture_view("dreaming", "DreamingView.qml", setup_dreaming, show_dreaming) and ok

    def show_dreaming_load_error(_view, root):
        model = root.property("dreamingModel")
        model._scope_key = "global"
        model.scope_key_changed.emit()
        model._current = {}
        model.current_changed.emit()
        model.summary_changed.emit()
        model._set_loading(False)
        model._set_load_error("daemon offline")
        assert_load_error_state(root, "dreamingLoadError", "dreamingLoadErrorText", "dreamingLoadErrorRetry")

    ok = capture_view("dreaming-load-error", "DreamingView.qml", setup_dreaming, show_dreaming_load_error) and ok

    def show_dreaming_refresh_error(_view, root):
        for _ in range(8):
            app.processEvents()
        model = root.property("dreamingModel")
        model._set_loading(False)
        model._set_load_error("daemon offline")
        assert_refresh_error_state(
            root,
            "dreamingRefreshErrorBanner",
            "dreamingRefreshErrorText",
            "dreamingRefreshErrorRetry",
            "dreamingLoadError",
            "dreamingRolloutRow_1",
        )

    ok = capture_view("dreaming-refresh-error", "DreamingView.qml", setup_dreaming, show_dreaming_refresh_error) and ok

    # 12. MemoryView
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

        memory_model.scopes_changed.emit()
        memory_model.current_changed.emit()

        ctx.setContextProperty("memoryModel", memory_model)
        ctx.setContextProperty("markdownParser", markdown_parser)
        return {"memoryModel": memory_model}

    ok = capture_view("memory", "MemoryView.qml", setup_memory) and ok

    def show_memory_load_error(_view, root):
        model = root.property("memoryModel")
        if model is not None:
            model.current = None
            model.loading = False
            model.load_error = "daemon offline"
        for _ in range(8):
            app.processEvents()
        banner = find_item(root, "memoryLoadError")
        text = find_item(root, "memoryLoadErrorText")
        retry = find_item(root, "memoryLoadErrorRetry")
        if banner is None or banner.property("visible") is not True:
            raise AssertionError("memory load error screenshot did not render the banner")
        if text is None or "daemon offline" not in text.property("text"):
            raise AssertionError("memory load error screenshot rendered the wrong text")
        if retry is None or retry.property("qaTextFits") is not True:
            raise AssertionError("memory load error screenshot did not render a clean retry button")

    ok = capture_view("memory-load-error", "MemoryView.qml", setup_memory, show_memory_load_error) and ok

    def show_memory_refresh_error(_view, root):
        model = root.property("memoryModel")
        if model is not None:
            model.load_error = "daemon offline"
        for _ in range(8):
            app.processEvents()
        banner = find_item(root, "memoryRefreshErrorBanner")
        text = find_item(root, "memoryRefreshErrorText")
        retry = find_item(root, "memoryRefreshErrorRetry")
        if banner is None or banner.property("visible") is not True:
            raise AssertionError("memory refresh error screenshot did not render the banner")
        if text is None or "daemon offline" not in text.property("text"):
            raise AssertionError("memory refresh error screenshot rendered the wrong text")
        if retry is None or retry.property("qaTextFits") is not True:
            raise AssertionError("memory refresh error screenshot did not render a clean retry button")

    ok = capture_view("memory-refresh-error", "MemoryView.qml", setup_memory, show_memory_refresh_error) and ok

    def show_memory_save_pending(_view, root):
        model = root.property("memoryModel")
        if model is not None:
            model.composing = True
            model.draft = "Memory pending save proof"
            model.saving = True

    ok = capture_view("memory-save-pending", "MemoryView.qml", setup_memory, show_memory_save_pending) and ok

    def show_memory_save_error(_view, root):
        model = root.property("memoryModel")
        if model is not None:
            model.composing = True
            model.draft = "Memory retry draft stays visible"
            model.action_error = "Could not save note: save denied"

    ok = capture_view("memory-save-error", "MemoryView.qml", setup_memory, show_memory_save_error) and ok

    def show_memory_remove_confirm(_view, root):
        model = root.property("memoryModel")
        if model is not None:
            model.setProperty("confirm_remove_ad_hoc", 0)
            model.setProperty("confirm_remove_note", 0)
        for _ in range(8):
            app.processEvents()
        for group_name, object_names in (
            (
                "manual memory actions",
                (
                    "memoryAdHocMoveButton_0",
                    "memoryAdHocRemoveConfirmButton_0",
                    "memoryAdHocRemoveCancelButton_0",
                ),
            ),
            (
                "distilled memory actions",
                (
                    "memoryNoteMoveButton_0",
                    "memoryNoteRemoveConfirmButton_0",
                    "memoryNoteRemoveCancelButton_0",
                ),
            ),
        ):
            buttons = [find_item(root, name) for name in object_names]
            if any(button is None for button in buttons):
                raise AssertionError(f"{group_name} did not render all confirm buttons")
            if any(button.property("qaTextFits") is not True for button in buttons):
                raise AssertionError(f"{group_name} rendered clipped action text")
            tops = [scene_top(button) for button in buttons]
            if max(tops) - min(tops) > 3:
                raise AssertionError(f"{group_name} stacked vertically instead of staying inline: {tops}")

    ok = capture_view("memory-remove-confirm", "MemoryView.qml", setup_memory, show_memory_remove_confirm) and ok

    def show_memory_move_dialog(_view, root):
        model = root.property("memoryModel")
        if model is not None:
            model.setProperty("move_pending", {"text": "Remember to test Qt views thoroughly", "idx": 0})
            model.setProperty("move_open", True)

    ok = capture_view("memory-move-dialog", "MemoryView.qml", setup_memory, show_memory_move_dialog) and ok

    # 12. Main shell with Chat route, proving the send button is not clipped by the status strip.
    ok = capture_main_shell(client, clipboard_helper, highlighter, markdown_parser, terminal_helper) and ok

    if ok:
        print("\n✓ All screenshots captured")
        return 0
    print("\n✗ Screenshot capture failed")
    return 1


if __name__ == "__main__":
    sys.exit(main())
