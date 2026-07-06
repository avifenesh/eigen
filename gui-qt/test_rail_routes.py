#!/usr/bin/env python3
"""Capture deterministic screenshots for every Main.qml rail route.

This is a visual QA helper, not a pytest test. It intentionally stays offline:
all RPC calls are answered by a local QObject double so route screenshots do
not depend on a running guiserver or private local state.

Usage:
    cd gui-qt
    QT_QPA_PLATFORM=offscreen QML_DISABLE_DISK_CACHE=1 .venv/bin/python test_rail_routes.py

Outputs:
    screenshots/rail-<route>.png
    screenshots/qa-fix-main-routes-contact.png
"""

from __future__ import annotations

import sys
from pathlib import Path

from PySide6.QtCore import QObject, Property, QRect, QSize, Qt, QTimer, Signal, Slot
from PySide6.QtGui import QColor, QFont, QGuiApplication, QImage, QPainter, QPen
from PySide6.QtQml import QQmlApplicationEngine, qmlRegisterType
from PySide6.QtQuickControls2 import QQuickStyle

from eigenqt.clipboard_helper import ClipboardHelper
from eigenqt.highlighter_helper import HighlighterHelper
from eigenqt.markdown_helper import MarkdownHelper
from eigenqt.models import (
    ApprovalsModel,
    BoardModel,
    CommandsModel,
    DashboardModel,
    FeedModel,
    KanbanModel,
    LiveSessionsModel,
    MemoryModel,
    ProposalsModel,
    SessionStateModel,
    SessionsModel,
    SkillsModel,
    TasksModel,
    TranscriptModel,
)
from eigenqt.models.config import ConfigModel, RuleChainsModel
from eigenqt.models.connectors import ConnectorsModel
from eigenqt.models.notes import NotesController, NotesModel
from eigenqt.models.reviewers import ReviewersModel
from eigenqt.models.transcript_logic import TranscriptRow
from eigenqt.models.worktree import DiffModel, FileTreeModel


ROOT = Path(__file__).resolve().parent
SCREENSHOTS = ROOT / "screenshots"
SCREENSHOTS.mkdir(exist_ok=True)

WINDOW_SIZE = QSize(1280, 800)
ROUTES = [
    "home",
    "sessions",
    "live",
    "chat",
    "board",
    "tasks",
    "skills",
    "memory",
    "notes",
    "connectors",
    "config",
    "reviewers",
]


class OfflineRpcClient(QObject):
    """Small deterministic guiserver double for Main route visual QA."""

    connected = Signal()
    callDone = Signal(int, "QVariantMap")
    event = Signal(str, dict)
    dropped = Signal(str)

    def __init__(self):
        super().__init__()
        self.calls: list[tuple[str, tuple]] = []
        self._token = 0

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

    def subscribe(self, channels):
        self.calls.append(("subscribe", tuple(channels or [])))

    def unsubscribe(self, channels):
        self.calls.append(("unsubscribe", tuple(channels or [])))

    def shutdown(self):
        pass

    def _result(self, method, args):
        if method == "NewSession":
            return "s-qa-chat"
        if method == "State":
            return {
                "id": args[0] if args else "s-qa-chat",
                "model": "gpt-5",
                "effort": "medium",
                "perm": "gated",
                "title": "Qt shell route",
                "goal": "Visual QA across every route",
                "running": False,
                "roots": ["/home/user/eigen"],
                "catalog": {
                    "models": [
                        {"id": "gpt-5", "effortLevels": ["low", "medium", "high"]},
                        {"id": "local-qwen", "effortLevels": ["low", "medium"]},
                    ]
                },
                "messages": [],
                "pending": [],
            }
        if method == "Sessions":
            return seeded_sessions()
        if method == "LiveSessions":
            return {"sessions": seeded_sessions()[1:]}
        if method in {"Tasks", "Agents"}:
            return {
                "tasks": [
                    {
                        "id": "task-qa",
                        "task": "Refresh Qt route screenshots",
                        "status": "running",
                        "model": "gpt-5",
                        "role": "frontend",
                        "kind": "qa",
                        "difficulty": "medium",
                        "where": "/home/user/eigen/gui-qt",
                        "lastTool": "grabWindow",
                        "steps": 3,
                        "startedMs": 1783155600000,
                    },
                    {
                        "id": "task-proof",
                        "task": "Check dock file viewer",
                        "status": "done",
                        "model": "local-qwen",
                        "role": "qa",
                        "kind": "proof",
                        "difficulty": "easy",
                        "where": "/home/user/eigen/gui-qt",
                        "lastTool": "pytest",
                        "steps": 2,
                        "startedMs": 1783152000000,
                        "finishedMs": 1783152120000,
                    },
                ]
            }
        if method == "Dashboard":
            return {"googleConnected": False, "events": [], "unreadCount": 0, "unread": [], "health": {"gpus": []}}
        if method == "Feed":
            return {
                "fresh": True,
                "items": [
                    {"key": "dirty-qt", "title": "Qt follow-up is dirty", "detail": "Route-level visual QA pending"},
                    {"key": "sessions", "title": "Resume GUI shell", "detail": "s-qa-chat has recent route proof"},
                ],
            }
        if method == "Board":
            return {
                "lanes": [
                    {
                        "dir": "/home/user/eigen",
                        "name": "eigen",
                        "repo": "avifenesh/eigen",
                        "branch": "fix/qt-parity-hardening",
                        "dirty": 3,
                        "todos": 2,
                        "items": [],
                    }
                ]
            }
        if method == "Kanban":
            return {"columns": []}
        if method == "Skills":
            return {
                "skills": [{"name": "frontend-design", "description": "Visual polish checks", "source": "user"}],
                "proposals": [{"name": "qt-qa", "description": "Route screenshot discipline"}],
            }
        if method == "ProposedSkills":
            return {"proposals": [{"name": "qt-qa", "description": "Route screenshot discipline"}]}
        if method == "ListMemoryScopes":
            return [{"key": "global", "name": "Global", "dir": "", "noteCount": 2, "current": True}]
        if method == "MemoryForScope":
            return {
                "summary": "Eigen Qt work favors visible proof and installed launcher checks.",
                "hasSummary": True,
                "notes": [{"index": 0, "text": "Keep tested chats on GPT/local models."}],
                "adHoc": [{"index": 0, "text": "Watch route screenshots for stale shell geometry."}],
                "profile": "# Profile\n\nWorks on local-first coding agent interfaces.",
                "profileLearned": "Prefers direct UI proof over green-only tests.",
                "banList": [],
                "backups": 1,
                "bytes": 512,
            }
        if method == "ObsidianStatus":
            return {"available": True, "vault": "/home/user/notes"}
        if method == "ObsidianNotes":
            return [{"path": "Inbox/Qt.md", "title": "Qt QA notes"}]
        if method == "ObsidianRead":
            return "# Qt QA notes\n\nRefresh every route after shell fixes.\n"
        if method == "Connectors":
            return {
                "connectors": [
                    {
                        "name": "notion",
                        "display": "Notion",
                        "url": "https://mcp.notion.com/mcp",
                        "connected": True,
                    }
                ],
                "directory": [{"name": "slack", "display": "Slack", "added": False}],
            }
        if method == "MCPServers":
            return {"servers": [{"name": "filesystem", "command": ["uvx", "mcp-server-filesystem"], "enabled": True}]}
        if method == "GoogleStatus":
            return {"configured": False, "connected": False, "clientPath": "", "setupUrl": "", "setupHint": ""}
        if method == "MCPSecretsAvailable":
            return True
        if method == "Config":
            return {
                "path": "/home/user/.eigen/config.json",
                "fields": [
                    {"key": "model", "desc": "Default model", "value": "gpt-5", "options": ["gpt-5", "local-qwen"]},
                    {"key": "route", "desc": "Enable router", "value": "true", "options": ["true", "false"]},
                ],
            }
        if method == "RuleChains":
            return {"models": ["gpt-5", "local-qwen"], "roles": [{"role": "primary", "chain": ["gpt-5"]}]}
        if method == "RevutoStatus":
            return {"available": True, "count": 1, "paused": 0}
        if method == "RevutoReviewers":
            return [{"repo": "avifenesh/eigen", "paused": False}]
        if method == "WorkingDiff":
            return {"isRepo": True, "clean": False, "branch": "fix/qt", "truncated": False, "patch": "", "files": []}
        if method == "FileTree":
            return {
                "truncated": False,
                "entries": [{"name": "README.md", "path": "/home/user/eigen/README.md", "isDir": False}],
            }
        if method == "ReadFileForView":
            return "# Eigen\n"
        return {}


class RouteSessionController(QObject):
    sessionIdChanged = Signal()
    sessionStateModelChanged = Signal()
    commandsModelChanged = Signal()

    def __init__(self, client, parent=None):
        super().__init__(parent)
        self._session_id = "s-qa-chat"
        self._session_state_model = SessionStateModel(client, self._session_id)
        self._session_state_model.seed(
            {
                "model": "gpt-5",
                "effort": "medium",
                "perm": "gated",
                "title": "Qt route QA",
                "goal": "Capture every shell route",
                "running": False,
                "roots": ["/home/user/eigen"],
                "catalog": {"models": [{"id": "gpt-5", "effortLevels": ["low", "medium", "high"]}]},
            }
        )
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


def seeded_sessions():
    return [
        {
            "id": "s-empty",
            "title": "Empty scratch",
            "dir": "/home/user/eigen",
            "model": "gpt-5",
            "status": "idle",
            "turns": 0,
            "updated": 1783144800000,
        },
        {
            "id": "s-qa-chat",
            "title": "Qt route QA",
            "dir": "/home/user/eigen/gui-qt",
            "model": "local-qwen",
            "status": "working",
            "turns": 4,
            "updated": 1783155600000,
        },
        {
            "id": "s-approval",
            "title": "Approval check",
            "dir": "/home/user/eigen",
            "model": "grok-4",
            "status": "approval",
            "turns": 2,
            "updated": 1783152000000,
        },
    ]


def pump(app, rounds=12):
    for _ in range(rounds):
        app.processEvents()


def seed_models(client):
    sessions = SessionsModel(client)
    sessions._on_sessions_result({"result": seeded_sessions()})

    live = LiveSessionsModel(client)
    live._on_sessions_result({"result": seeded_sessions()})

    tasks = TasksModel(client)
    tasks._on_agents_result({"result": client._result("Agents", ())})
    tasks._poll_timer.stop()

    board = BoardModel(client)
    board._on_board_result({"result": client._result("Board", ())})

    kanban = KanbanModel(client)
    kanban._on_kanban_result({"result": client._result("Kanban", ())})

    skills = SkillsModel(client)
    proposals = ProposalsModel(client)
    skill_snapshot = client._result("Skills", ())
    skills._on_skills_result({"result": skill_snapshot})
    proposals._on_skills_result({"result": skill_snapshot})

    memory = MemoryModel(client)
    memory.scopes = client._result("ListMemoryScopes", ())
    memory.scope_key = "global"
    memory.current = client._result("MemoryForScope", ())
    memory.loading = False

    notes = NotesController(client)
    notes.status = client._result("ObsidianStatus", ())
    notes._notes_model = NotesModel(client)
    notes._notes_model._on_notes_result({"result": client._result("ObsidianNotes", ())})
    notes.selected = {"path": "Inbox/Qt.md", "title": "Qt QA notes"}
    notes.content = client._result("ObsidianRead", ("Inbox/Qt.md",))

    connectors = ConnectorsModel(client)
    connectors.connectors = client._result("Connectors", ())
    connectors.servers = client._result("MCPServers", ())
    connectors.google_status = client._result("GoogleStatus", ())
    connectors.obsidian_status = client._result("ObsidianStatus", ())
    connectors.revuto_status = client._result("RevutoStatus", ())
    connectors.reviewers = client._result("RevutoReviewers", ())
    connectors.secrets_ok = True
    connectors.loading = False

    config = ConfigModel(client)
    config._on_config_result({"result": client._result("Config", ())})

    rule_chains = RuleChainsModel(client)
    rule_chains._on_rule_chains_result({"result": client._result("RuleChains", ())})

    reviewers = ReviewersModel(client)
    reviewers._on_status_result({"result": client._result("RevutoStatus", ())})
    reviewers._on_reviewers_result({"result": client._result("RevutoReviewers", ())})

    transcript = TranscriptModel(client, "s-qa-chat")
    transcript._rows = [
        TranscriptRow(kind="user", text="Refresh every route after the shell fixes."),
        TranscriptRow(kind="assistant", text="All rail routes are captured offline for visual QA."),
    ]
    transcript.layoutChanged.emit()

    return {
        "rpcClient": client,
        "client": client,
        "sessionsModel": sessions,
        "liveSessionsModel": live,
        "tasksModel": tasks,
        "dashboardModel": DashboardModel(client),
        "feedModel": FeedModel(client),
        "boardModel": board,
        "kanbanModel": kanban,
        "skillsModel": skills,
        "proposalsModel": proposals,
        "memoryModel": memory,
        "notesController": notes,
        "connectorsModel": connectors,
        "configModel": config,
        "ruleChainsModel": rule_chains,
        "reviewersModel": reviewers,
        "sessionController": RouteSessionController(client),
        "transcriptModel": transcript,
        "approvalsModel": ApprovalsModel(client, "s-qa-chat"),
        "daemonOnline": True,
        "guiserverSha": "routeqa123456",
        "statsData": {"running_turns": 1},
    }


def save_contact_sheet(route_images):
    columns = 3
    thumb = QSize(400, 250)
    label_height = 28
    rows = (len(route_images) + columns - 1) // columns
    sheet = QImage(columns * thumb.width(), rows * (thumb.height() + label_height), QImage.Format_RGB32)
    sheet.fill(QColor("#07100f"))

    painter = QPainter(sheet)
    painter.setRenderHint(QPainter.SmoothPixmapTransform)
    painter.setFont(QFont("Sans Serif", 10, QFont.Bold))
    painter.setPen(QPen(QColor("#c8fff4")))

    for index, (route, image) in enumerate(route_images):
        col = index % columns
        row = index // columns
        x = col * thumb.width()
        y = row * (thumb.height() + label_height)
        painter.drawText(QRect(x + 8, y, thumb.width() - 16, label_height), Qt.AlignVCenter, route)
        painter.drawImage(QRect(x, y + label_height, thumb.width(), thumb.height()), image)

    painter.end()
    output = SCREENSHOTS / "qa-fix-main-routes-contact.png"
    if not sheet.save(str(output)):
        raise RuntimeError(f"failed to save {output}")
    print(f"saved {output}")


def main():
    QQuickStyle.setStyle("Basic")
    app = QGuiApplication(sys.argv)
    app.setOrganizationName("eigen")
    app.setApplicationName("eigen-rail-qa")

    qmlRegisterType(DiffModel, "Eigen", 1, 0, "DiffModel")
    qmlRegisterType(FileTreeModel, "Eigen", 1, 0, "FileTreeModel")

    client = OfflineRpcClient()
    engine = QQmlApplicationEngine()
    engine.addImportPath(str(ROOT / "eigenqt"))

    context = seed_models(client)
    context["clipboardHelper"] = ClipboardHelper(app)
    context["highlighter"] = HighlighterHelper(app)
    context["markdownParser"] = MarkdownHelper(app)
    for name, value in context.items():
        engine.rootContext().setContextProperty(name, value)

    engine.load(str(ROOT / "eigenqt" / "qml" / "Main.qml"))
    if not engine.rootObjects():
        raise RuntimeError("failed to load Main.qml")

    window = engine.rootObjects()[0]
    window.setProperty("width", WINDOW_SIZE.width())
    window.setProperty("height", WINDOW_SIZE.height())
    window.show()
    pump(app, 30)

    route_images = []
    for route in ROUTES:
        window.setProperty("currentRoute", route)
        pump(app, 24)
        image = window.grabWindow()
        if image.isNull():
            raise RuntimeError(f"grabWindow returned a null image for {route}")
        if image.size() != WINDOW_SIZE:
            raise RuntimeError(f"{route} captured at {image.size().width()}x{image.size().height()}")
        output = SCREENSHOTS / f"rail-{route}.png"
        if not image.save(str(output)):
            raise RuntimeError(f"failed to save {output}")
        print(f"saved {output}")
        route_images.append((route, image))

    save_contact_sheet(route_images)
    client.shutdown()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
