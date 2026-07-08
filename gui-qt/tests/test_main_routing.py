import os
import subprocess
import sys
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_main_shell_routes_and_running_session_rail_are_clickable():
    script = r"""
from pathlib import Path

from PySide6.QtCore import QObject, QPoint, QPointF, QSize, QTimer, Qt, QtMsgType, Property, Signal, Slot, qInstallMessageHandler
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine, qmlRegisterType
from PySide6.QtQuickControls2 import QQuickStyle
from PySide6.QtTest import QTest

from eigenqt.clipboard_helper import ClipboardHelper
from eigenqt.highlighter_helper import HighlighterHelper
from eigenqt.markdown_helper import MarkdownHelper
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
    MachinesModel,
    MemoryModel,
    ObserveModel,
    PluginsModel,
    ProfileModel,
    ProposalsModel,
    RoutingModel,
    SessionStateModel,
    SessionsModel,
    SkillsModel,
    TasksModel,
    TranscriptModel,
)
from eigenqt.models.config import ConfigModel, RuleChainsModel
from eigenqt.models.connectors import ConnectorsModel
from eigenqt.models.notes import NotesController
from eigenqt.models.reviewers import ReviewersModel
from eigenqt.models.worktree import DiffModel, FileTreeModel


ROOT = Path.cwd()
SIZE = QSize(1280, 800)
ISSUE_MARKERS = (
    "ReferenceError",
    "TypeError",
    "Unable to assign",
    "Cannot assign",
    "Cannot read property",
)


class FakeRpcClient(QObject):
    connected = Signal()
    callDone = Signal(int, "QVariantMap")
    event = Signal(str, dict)
    dropped = Signal(str)

    def __init__(self):
        super().__init__()
        self.calls = []
        self.failures = {}
        self._token = 0

    def call(self, method, *args, callback=None, error_callback=None):
        self.calls.append((method, args))
        payload = (
            {"error": self.failures[method]}
            if method in self.failures
            else {"result": self._result(method, args)}
        )
        if callback:
            QTimer.singleShot(0, lambda: callback(payload))

    @Slot(str, "QVariantList", result=int)
    def callToken(self, method, args):
        self._token += 1
        token = self._token
        call_args = tuple(args or [])
        self.calls.append((method, call_args))
        payload = (
            {"error": self.failures[method]}
            if method in self.failures
            else {"result": self._result(method, call_args)}
        )
        QTimer.singleShot(0, lambda: self.callDone.emit(token, payload))
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
            return "s-new"
        if method == "State":
            return {
                "id": args[0] if args else "s-work",
                "model": "gpt-5",
                "effort": "medium",
                "perm": "gated",
                "title": "Qt shell routing",
                "goal": "Prove rail navigation",
                "running": False,
                "roots": ["/repo/eigen"],
                "catalog": {"models": [{"id": "gpt-5", "effortLevels": ["low", "medium", "high"]}]},
                "history": [],
                "pending": [],
            }
        if method == "Sessions":
            return seeded_sessions()
        if method == "Tasks":
            return {"tasks": []}
        if method == "Dashboard":
            return {"googleConnected": False, "events": [], "unreadCount": 0, "unread": [], "health": {"gpus": []}}
        if method == "Feed":
            return {"items": seeded_feed(), "fresh": True}
        if method == "Board":
            return {"lanes": []}
        if method == "Kanban":
            return {"columns": []}
        if method == "Skills":
            return {"skills": []}
        if method == "ProposedSkills":
            return {"proposals": []}
        if method == "ListMemoryScopes":
            return [
                {"key": "global", "name": "Global", "dir": "", "noteCount": 3},
                {
                    "key": "project:/repo/eigen",
                    "name": "eigen",
                    "dir": "/repo/eigen",
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
                "profile": "Qt profile proof" if scope == "global" else "",
                "profileLearned": "Prefers direct Qt checks." if scope == "global" else "",
                "banList": [],
            }
        if method == "DreamingForScope":
            scope = args[0] if args else "project:/repo/eigen"
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
                        "path": "/repo/eigen/.eigen/memory/MEMORY.md.20260707-120000.bak",
                        "label": "Jul 7, 12:00",
                        "whenMs": 1783152000000,
                        "bytes": 2048,
                    }
                ],
            }
        if method == "ObsidianStatus":
            return {"available": False, "vault": ""}
        if method == "ObsidianNotes":
            return []
        if method == "Connectors":
            return {"connectors": [], "directory": []}
        if method == "MCPServers":
            return {"servers": []}
        if method == "GoogleStatus":
            return {"configured": False, "connected": False, "clientPath": "", "setupUrl": "", "setupHint": ""}
        if method == "MCPSecretsAvailable":
            return False
        if method == "Config":
            return {"path": "/home/user/.eigen/config.json", "config": {"model": "gpt-5", "perm": "gated"}}
        if method == "RuleChains":
            return {"chains": {}}
        if method == "RevutoStatus":
            return {"available": False, "count": 0, "paused": 0}
        if method == "RevutoReviewers":
            return []
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
        return {}


class FakeSessionController(QObject):
    sessionIdChanged = Signal()
    sessionStateModelChanged = Signal()
    commandsModelChanged = Signal()

    def __init__(self, client):
        super().__init__()
        self.opened = []
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
        self.opened.append(session_id)
        self._session_id = session_id
        self.sessionIdChanged.emit()

    @Slot()
    def detach(self):
        self._session_id = ""
        self.sessionIdChanged.emit()


def seeded_sessions():
    return [
        {
            "id": "s-idle",
            "title": "Idle archive",
            "dir": "/repo/eigen",
            "model": "gpt-5",
            "status": "idle",
            "turns": 1,
            "updated": 10,
        },
        {
            "id": "s-work",
            "title": "Qt shell routing",
            "dir": "/repo/eigen/gui-qt",
            "model": "local-qwen",
            "status": "working",
            "turns": 3,
            "updated": 20,
        },
        {
            "id": "s-approval",
            "title": "Needs approval",
            "dir": "/repo/eigen",
            "model": "grok-4",
            "status": "approval",
            "turns": 2,
            "updated": 15,
        },
    ]


def seeded_feed():
    return [
        {
            "key": "feed-git",
            "kind": "git",
            "title": "Dirty checkout",
            "detail": "Review focused diff",
            "dir": "/repo/eigen",
            "dirName": "eigen",
            "task": "Review and commit the focused diff.",
        },
        {
            "key": "feed-pr",
            "kind": "github",
            "title": "PR needs polish",
            "detail": "Qt follow-up",
            "dir": "/repo/eigen",
            "dirName": "eigen",
            "task": "Tighten the Qt shell.",
        },
    ]


def pump(app, rounds=12):
    for _ in range(rounds):
        app.processEvents()


def item_visibility_score(item):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    score = width * height
    if width <= 0 or height <= 0:
        score -= 1_000_000
    probe = item
    while probe is not None:
        if probe.property("visible") is False:
            score -= 1_000_000
        opacity = probe.property("opacity")
        if opacity is not None and float(opacity) <= 0.01:
            score -= 1_000_000
        probe = probe.parentItem()
    return score


def find_item(item, object_name):
    matches = []

    def collect(candidate):
        if candidate is None:
            return
        if candidate.objectName() == object_name:
            matches.append(candidate)
        for child in candidate.childItems():
            collect(child)

    collect(item)
    if not matches:
        return None
    return max(matches, key=item_visibility_score)


def find_item_in_window(window, object_name):
    return find_item(window.contentItem(), object_name)


def object_names_with_prefix(item, prefix):
    names = []

    def collect(candidate):
        if candidate is None:
            return
        name = candidate.objectName()
        if name.startswith(prefix):
            names.append(name)
        for child in candidate.childItems():
            collect(child)

    collect(item)
    return names


def assert_item_inside_window(item, label):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    if width <= 0 or height <= 0:
        raise AssertionError(f"{label} has invalid size {width}x{height}")
    window = item.window()
    window_width = float(window.width()) if window is not None else float(SIZE.width())
    window_height = float(window.height()) if window is not None else float(SIZE.height())
    top_left = item.mapToScene(QPointF(0, 0))
    bottom_right = item.mapToScene(QPointF(width, height))
    if (
        top_left.x() < -0.5
        or top_left.y() < -0.5
        or bottom_right.x() > window_width + 0.5
        or bottom_right.y() > window_height + 0.5
    ):
        raise AssertionError(
            f"{label} is outside the rendered window: "
            f"({top_left.x():.1f}, {top_left.y():.1f}) -> "
            f"({bottom_right.x():.1f}, {bottom_right.y():.1f})"
            f" within {window_width:.0f}x{window_height:.0f}"
        )


def item_center(item):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    if width <= 0 or height <= 0:
        raise AssertionError(f"{item.objectName()} has invalid size {width}x{height}")
    window = item.window()
    window_width = float(window.width()) if window is not None else float(SIZE.width())
    window_height = float(window.height()) if window is not None else float(SIZE.height())
    point = item.mapToScene(QPointF(width / 2, height / 2))
    return QPoint(max(0, min(int(window_width) - 1, int(point.x()))), max(0, min(int(window_height) - 1, int(point.y()))))


def click_item(app, window, object_name):
    pump(app, 8)
    item = find_item_in_window(window, object_name)
    if item is None:
        raise AssertionError(f"missing item {object_name}")
    if object_name.startswith("navItem_"):
        flick = find_item_in_window(window, "railNavFlick")
        if flick is not None:
            top = item.mapToItem(flick, QPointF(0, 0)).y()
            bottom = item.mapToItem(flick, QPointF(0, float(item.property("height") or 0))).y()
            view_height = float(flick.property("height") or 0)
            content_y = float(flick.property("contentY") or 0)
            next_y = content_y
            if top < 0:
                next_y = max(0, content_y + top - 4)
            elif bottom > view_height:
                max_y = max(0, float(flick.property("contentHeight") or 0) - view_height)
                next_y = min(max_y, content_y + (bottom - view_height) + 4)
            if abs(next_y - content_y) > 0.5:
                flick.setProperty("contentY", next_y)
                pump(app, 12)
                item = find_item_in_window(window, object_name)
                if item is None:
                    raise AssertionError(f"missing item {object_name} after rail scroll")
    assert_item_inside_window(item, object_name)
    QTest.mouseClick(window, Qt.LeftButton, Qt.NoModifier, item_center(item))
    QTest.qWait(20)
    pump(app, 18)
    return item


def scene_top(item):
    return item.mapToScene(QPointF(0, 0)).y()


def scene_bottom(item):
    return item.mapToScene(QPointF(0, float(item.property("height") or 0))).y()


def assert_no_qml_issues(messages):
    issues = [
        record for record in messages
        if record["type"] in (QtMsgType.QtCriticalMsg, QtMsgType.QtFatalMsg)
        or (
            record["type"] == QtMsgType.QtWarningMsg
            and (record["file"].endswith(".qml") or any(marker in record["message"] for marker in ISSUE_MARKERS))
        )
    ]
    if issues:
        raise AssertionError(f"QML issues: {issues[:8]}")


QQuickStyle.setStyle("Basic")
app = QGuiApplication([])
messages = []


def capture_qt_message(mode, context, message):
    messages.append({
        "type": mode,
        "file": context.file or "",
        "line": context.line or 0,
        "message": message,
    })


previous_handler = qInstallMessageHandler(capture_qt_message)
try:
    qmlRegisterType(DiffModel, "Eigen", 1, 0, "DiffModel")
    qmlRegisterType(FileTreeModel, "Eigen", 1, 0, "FileTreeModel")
    client = FakeRpcClient()
    sessions_model = SessionsModel(client)
    sessions_model._on_sessions_result({"result": seeded_sessions()})
    sessions_model.mark_unread("s-work")
    live_model = LiveSessionsModel(client)
    tasks_model = TasksModel(client)
    dashboard_model = DashboardModel(client)
    feed_model = FeedModel(client)
    feed_model._on_feed_result({"result": {"items": seeded_feed(), "fresh": True}})
    board_model = BoardModel(client)
    kanban_model = KanbanModel(client)
    dreaming_model = DreamingModel(client)
    skills_model = SkillsModel(client)
    proposals_model = ProposalsModel(client)
    memory_model = MemoryModel(client)
    machines_model = MachinesModel(client)
    crons_model = CronsModel(client)
    notes_controller = NotesController(client)
    connectors_model = ConnectorsModel(client)
    observe_model = ObserveModel(client)
    routing_model = RoutingModel(client)
    config_model = ConfigModel(client)
    rule_chains_model = RuleChainsModel(client)
    reviewers_model = ReviewersModel(client)
    plugins_model = PluginsModel(client)
    profile_model = ProfileModel(client)
    controller = FakeSessionController(client)
    transcript_model = TranscriptModel(client, "")
    approvals_model = ApprovalsModel(client, "")
    clipboard = ClipboardHelper(app)
    highlighter = HighlighterHelper(app)
    markdown = MarkdownHelper(app)

    engine = QQmlApplicationEngine()
    engine.addImportPath(str(ROOT / "eigenqt"))
    ctx = engine.rootContext()
    ctx.setContextProperty("rpcClient", client)
    ctx.setContextProperty("client", client)
    ctx.setContextProperty("sessionsModel", sessions_model)
    ctx.setContextProperty("liveSessionsModel", live_model)
    ctx.setContextProperty("tasksModel", tasks_model)
    ctx.setContextProperty("dashboardModel", dashboard_model)
    ctx.setContextProperty("feedModel", feed_model)
    ctx.setContextProperty("boardModel", board_model)
    ctx.setContextProperty("kanbanModel", kanban_model)
    ctx.setContextProperty("dreamingModel", dreaming_model)
    ctx.setContextProperty("skillsModel", skills_model)
    ctx.setContextProperty("proposalsModel", proposals_model)
    ctx.setContextProperty("memoryModel", memory_model)
    ctx.setContextProperty("machinesModel", machines_model)
    ctx.setContextProperty("cronsModel", crons_model)
    ctx.setContextProperty("notesController", notes_controller)
    ctx.setContextProperty("connectorsModel", connectors_model)
    ctx.setContextProperty("observeModel", observe_model)
    ctx.setContextProperty("routingModel", routing_model)
    ctx.setContextProperty("configModel", config_model)
    ctx.setContextProperty("ruleChainsModel", rule_chains_model)
    ctx.setContextProperty("reviewersModel", reviewers_model)
    ctx.setContextProperty("pluginsModel", plugins_model)
    ctx.setContextProperty("profileModel", profile_model)
    ctx.setContextProperty("sessionController", controller)
    ctx.setContextProperty("transcriptModel", transcript_model)
    ctx.setContextProperty("approvalsModel", approvals_model)
    ctx.setContextProperty("daemonOnline", True)
    ctx.setContextProperty("guiserverSha", "abcdef1234567890")
    ctx.setContextProperty("statsData", {"running_turns": 2, "sessions": 7})
    ctx.setContextProperty("clipboardHelper", clipboard)
    ctx.setContextProperty("highlighter", highlighter)
    ctx.setContextProperty("markdownParser", markdown)

    engine.load(str(ROOT / "eigenqt" / "qml" / "Main.qml"))
    if not engine.rootObjects():
        raise AssertionError("Main.qml did not load")
    window = engine.rootObjects()[0]
    initial_available_width = int(window.property("initialAvailableWidth") or 0)
    initial_available_height = int(window.property("initialAvailableHeight") or 0)
    if initial_available_width > 0 and window.width() > initial_available_width:
        raise AssertionError(
            f"Main initial width exceeds available desktop: {window.width()} > {initial_available_width}"
        )
    if initial_available_height > 0 and window.height() > initial_available_height:
        raise AssertionError(
            f"Main initial height exceeds available desktop: {window.height()} > {initial_available_height}"
        )
    window.setProperty("width", SIZE.width())
    window.setProperty("height", SIZE.height())
    window.show()
    pump(app, 30)
    assert_no_qml_issues(messages)

    if window.property("currentRoute") != "home" or window.property("activeRouteIndex") != 0:
        raise AssertionError("Main did not start on the home route")

    chat_nav = find_item_in_window(window, "navItem_chat")
    home_nav = find_item_in_window(window, "navItem_home")
    sessions_nav = find_item_in_window(window, "navItem_sessions")
    running_row = find_item_in_window(window, "navRunningSession_s_work")
    approval_row = find_item_in_window(window, "navRunningSession_s_approval")
    if chat_nav is None or home_nav is None or sessions_nav is None or running_row is None or approval_row is None:
        raise AssertionError(
            "Rail did not render home, chat, sessions, and live sub-rows: "
            f"home={home_nav is not None} chat={chat_nav is not None} sessions={sessions_nav is not None} "
            f"running={running_row is not None} approval={approval_row is not None}"
            f" count={chat_nav.property('qaRunningSessionCount') if chat_nav is not None else None}"
            f" delegates={chat_nav.property('qaRunningDelegateCount') if chat_nav is not None else None}"
            f" names={object_names_with_prefix(window.contentItem(), 'navRunning')}"
        )
    if home_nav.property("badge") != 2:
        raise AssertionError(f"Home nav did not show feed count: {home_nav.property('badge')}")
    feed_model.dismiss("feed-git")
    pump(app, 12)
    if home_nav.property("badge") != 1:
        raise AssertionError(f"Home nav did not react to feed dismiss: {home_nav.property('badge')}")
    for badge_name in ("navBadge_home", "navBadge_chat"):
        badge = find_item_in_window(window, badge_name)
        if badge is None or badge.property("visible") is not True:
            raise AssertionError(f"Rail badge {badge_name} did not render")
        if badge.property("qaIsNavBadge") is not True:
            raise AssertionError(f"Rail badge {badge_name} did not expose nav badge QA")
        if badge.property("qaTextFits") is not True:
            raise AssertionError(f"Rail badge {badge_name} text does not fit")
        if float(badge.property("qaHorizontalPadding") or 0) < 15.5:
            raise AssertionError(f"Rail badge {badge_name} horizontal padding too small: {badge.property('qaHorizontalPadding')}")
        if float(badge.property("qaVerticalPadding") or 0) < 7.5:
            raise AssertionError(f"Rail badge {badge_name} vertical padding too small: {badge.property('qaVerticalPadding')}")
    if float(chat_nav.property("height") or 0) <= 30:
        raise AssertionError("Chat nav item did not expand for running-session rows")
    if scene_top(running_row) < scene_top(chat_nav) + 30 - 0.5:
        raise AssertionError("Running session row overlaps the chat nav main row")
    if scene_top(sessions_nav) < scene_bottom(chat_nav) - 0.5:
        raise AssertionError("Sessions nav item overlaps the expanded chat running list")
    if chat_nav.property("qaTextFits") is not True:
        raise AssertionError("Chat nav label does not fit")
    if running_row.property("qaUnread") is not True:
        raise AssertionError("Unread running session did not expose qaUnread")
    if approval_row.property("qaUnread") is not False:
        raise AssertionError("Read approval session was incorrectly marked unread")
    running_unread = find_item_in_window(window, "navRunningUnread_s_work")
    approval_unread = find_item_in_window(window, "navRunningUnread_s_approval")
    if running_unread is None or running_unread.property("visible") is not True:
        raise AssertionError("Unread rail dot did not render for s-work")
    if approval_unread is None or approval_unread.property("visible") is not False:
        raise AssertionError("Unread rail dot rendered for read approval session")

    route_expectations = [
        ("navItem_sessions", "sessions", 1),
        ("navItem_live", "live", 2),
        ("navItem_board", "board", 4),
        ("navItem_tasks", "tasks", 5),
        ("navItem_memory", "memory", 7),
        ("navItem_notes", "notes", 8),
        ("navItem_dreaming", "dreaming", 9),
        ("navItem_observe", "observe", 10),
        ("navItem_routing", "routing", 11),
        ("navItem_machines", "machines", 12),
        ("navItem_crons", "crons", 13),
        ("navItem_plugins", "plugins", 14),
        ("navItem_connectors", "connectors", 15),
        ("navItem_profile", "profile", 16),
        ("navItem_config", "config", 17),
        ("navItem_reviewers", "reviewers", 18),
    ]
    for object_name, route, index in route_expectations:
        nav = click_item(app, window, object_name)
        if window.property("currentRoute") != route or window.property("activeRouteIndex") != index:
            raise AssertionError(
                f"{object_name} did not route to {route}/{index}: "
                f"{window.property('currentRoute')}/{window.property('activeRouteIndex')}"
            )
        if nav.property("qaTextFits") is not True:
            raise AssertionError(f"{object_name} label does not fit")

    click_item(app, window, "navItem_dreaming")
    pump(app, 30)
    if ("ListMemoryScopes", ()) not in client.calls:
        raise AssertionError(f"Dreaming view did not fetch memory scopes: {client.calls}")
    if not any(call[0] == "DreamingForScope" for call in client.calls):
        raise AssertionError(f"Dreaming view did not fetch the timeline: {client.calls}")
    dreaming_view = find_item_in_window(window, "dreamingView")
    dreaming_combo = find_item_in_window(window, "dreamingScopeCombo")
    rollout_row = find_item_in_window(window, "dreamingRolloutRow_1")
    if dreaming_view is None or dreaming_combo is None or rollout_row is None:
        raise AssertionError("Dreaming view did not render scope picker and rollout rows")
    if dreaming_view.property("qaRolloutCount") != 2 or dreaming_view.property("qaConsolidationCount") != 1:
        raise AssertionError(
            "Dreaming view counts were wrong: "
            f"rollouts={dreaming_view.property('qaRolloutCount')} "
            f"consolidations={dreaming_view.property('qaConsolidationCount')}"
        )
    if dreaming_combo.property("qaTextFits") is not True or rollout_row.property("qaTextFits") is not True:
        raise AssertionError("Dreaming scope or rollout text did not fit")
    click_item(app, window, "dreamingTab_consolidations")
    pump(app, 30)
    if dreaming_view.property("qaStrand") != "consolidations":
        raise AssertionError(f"Dreaming tab did not switch to consolidations: {dreaming_view.property('qaStrand')}")
    consolidation_row = find_item_in_window(window, "dreamingConsolidationRow_0")
    if consolidation_row is None or consolidation_row.property("qaTextFits") is not True:
        raise AssertionError(
            "Dreaming consolidation row did not render cleanly: "
            f"row={consolidation_row is not None} "
            f"visible={consolidation_row.property('visible') if consolidation_row else None} "
            f"width={consolidation_row.property('width') if consolidation_row else None} "
            f"labelTruncated={consolidation_row.property('qaLabelTruncated') if consolidation_row else None} "
            f"metaTruncated={consolidation_row.property('qaMetaTruncated') if consolidation_row else None} "
            f"meta={consolidation_row.property('qaMetaText') if consolidation_row else None} "
            f"names={object_names_with_prefix(window.contentItem(), 'dreaming')[:40]} "
            f"messages={messages[:6]}"
        )

    click_item(app, window, "navItem_observe")
    pump(app, 24)
    observe_view = find_item_in_window(window, "observeView")
    observe_records = find_item_in_window(window, "observeKpi_records")
    observe_route_mix = find_item_in_window(window, "observeRouteMix")
    observe_tool = find_item_in_window(window, "observeToolRow_read_file")
    observe_model_row = find_item_in_window(window, "observeModelRow_gpt_5")
    observe_error = find_item_in_window(window, "observeErrorRow_rpc_timeout")
    if observe_view is None or observe_records is None or observe_route_mix is None:
        raise AssertionError("Observe view did not render summary cards and route mix")
    if observe_view.property("qaRecordCount") != 4:
        raise AssertionError(f"Observe view record count was wrong: {observe_view.property('qaRecordCount')}")
    if observe_view.property("qaToolCount") != 2 or observe_view.property("qaModelCount") != 1:
        raise AssertionError("Observe view did not expose tool/model counts")
    if observe_tool is None or observe_tool.property("qaTextFits") is not True:
        raise AssertionError("Observe tool row did not render cleanly")
    if observe_model_row is None or observe_model_row.property("qaTextFits") is not True:
        raise AssertionError("Observe model row did not render cleanly")
    if observe_error is None or observe_error.property("qaTextFits") is not True:
        raise AssertionError("Observe error row did not render cleanly")

    click_item(app, window, "navItem_routing")
    pump(app, 24)
    if ("Routing", ()) not in client.calls:
        raise AssertionError(f"Routing view did not fetch the catalog: {client.calls}")
    if ("ObserveSummary", (5000,)) not in client.calls:
        raise AssertionError(f"Routing view did not fetch route stats: {client.calls}")
    routing_view = find_item_in_window(window, "routingView")
    health_strip = find_item_in_window(window, "routingHealthStrip")
    all_provider = find_item_in_window(window, "routingProvider_all")
    grok_provider = find_item_in_window(window, "routingProvider_grok")
    gpt_card = find_item_in_window(window, "routingModelCard_gpt_5")
    if routing_view is None or health_strip is None or all_provider is None or grok_provider is None or gpt_card is None:
        raise AssertionError("Routing view did not render providers, health, and model cards")
    if routing_view.property("qaFilteredModelCount") != 3:
        raise AssertionError(f"Routing view filtered count was wrong: {routing_view.property('qaFilteredModelCount')}")
    if all_provider.property("qaTextFits") is not True or grok_provider.property("qaTextFits") is not True:
        raise AssertionError("Routing provider row text did not fit")
    grok_provider.forceActiveFocus(Qt.TabFocusReason)
    pump(app, 12)
    if grok_provider.property("qaVisualFocus") is not True:
        raise AssertionError("Routing provider row did not expose keyboard focus")
    if grok_provider.property("qaAccessibleName") != "grok provider":
        raise AssertionError(f"Routing provider accessible name was {grok_provider.property('qaAccessibleName')!r}")
    if gpt_card.property("qaTextFits") is not True:
        raise AssertionError("Routing model card text did not fit")
    QTest.keyClick(window, Qt.Key_Return)
    pump(app, 18)
    if routing_view.property("qaFilteredModelCount") != 1:
        raise AssertionError(f"Routing provider filter did not narrow to one model: {routing_view.property('qaFilteredModelCount')}")
    grok_card = find_item_in_window(window, "routingModelCard_grok_4")
    if grok_card is None or grok_card.property("qaTextFits") is not True:
        raise AssertionError("Routing provider filter did not keep the grok model card clean")
    all_provider.forceActiveFocus(Qt.TabFocusReason)
    pump(app, 12)
    if all_provider.property("qaVisualFocus") is not True:
        raise AssertionError("Routing all-provider row did not expose keyboard focus")
    if all_provider.property("qaAccessibleName") != "All providers":
        raise AssertionError(f"Routing all-provider accessible name was {all_provider.property('qaAccessibleName')!r}")
    QTest.keyClick(window, Qt.Key_Space)
    pump(app, 18)
    if routing_view.property("qaFilteredModelCount") != 3:
        raise AssertionError("Routing provider filter did not clear before refresh-error proof")
    client.failures["Routing"] = {"message": "daemon offline"}
    click_item(app, window, "routingRefreshButton")
    QTest.qWait(40)
    pump(app, 24)
    refresh_error = find_item_in_window(window, "routingRefreshErrorBanner")
    refresh_retry = find_item_in_window(window, "routingRefreshErrorRetry")
    gpt_card = find_item_in_window(window, "routingModelCard_gpt_5")
    if refresh_error is None or refresh_error.property("visible") is not True:
        raise AssertionError("Routing refresh failure did not show an inline error banner")
    if refresh_error.property("qaTextFits") is not True or "daemon offline" not in refresh_error.property("qaErrorText"):
        raise AssertionError(
            "Routing refresh error banner did not render cleanly: "
            f"text={refresh_error.property('qaErrorText') if refresh_error else None} "
            f"fits={refresh_error.property('qaTextFits') if refresh_error else None}"
        )
    if refresh_retry is None or refresh_retry.property("qaTextFits") is not True:
        raise AssertionError("Routing refresh retry button did not render cleanly")
    assert_item_inside_window(refresh_error, "routingRefreshErrorBanner")
    if routing_view.property("qaFilteredModelCount") != 3 or gpt_card is None:
        raise AssertionError("Routing refresh failure dropped the usable catalog")
    del client.failures["Routing"]
    click_item(app, window, "routingRefreshErrorRetry")
    pump(app, 24)
    refresh_error = find_item_in_window(window, "routingRefreshErrorBanner")
    if refresh_error is not None and refresh_error.property("visible") is True:
        raise AssertionError("Routing refresh error banner did not clear after retry")

    click_item(app, window, "navItem_machines")
    pump(app, 24)
    if ("Machines", ()) not in client.calls:
        raise AssertionError(f"Machines view did not fetch hosts: {client.calls}")
    machines_view = find_item_in_window(window, "machinesView")
    codex_card = find_item_in_window(window, "machinesCard_codex_box")
    lab_card = find_item_in_window(window, "machinesCard_lab_node")
    remote_panel = find_item_in_window(window, "machinesRemotePanel")
    if machines_view is None or codex_card is None or lab_card is None or remote_panel is None:
        raise AssertionError("Machines view did not render host cards and remote panel")
    if machines_view.property("qaMachineCount") != 2:
        raise AssertionError(f"Machines view host count was wrong: {machines_view.property('qaMachineCount')}")
    if codex_card.property("qaTextFits") is not True or lab_card.property("qaTextFits") is not True:
        raise AssertionError("Machines host card text did not fit")
    click_item(app, window, "machinesCard_codex_box")
    pump(app, 24)
    if ("RemoteSessions", ("codex-box",)) not in client.calls:
        raise AssertionError(f"Machines drill-in did not fetch remote sessions: {client.calls}")
    remote_row = find_item_in_window(window, "machinesRemoteRow_remote_codex_box_s1")
    remote_open = find_item_in_window(window, "machinesRemoteOpen_remote_codex_box_s1")
    if remote_row is None or remote_open is None:
        raise AssertionError("Machines drill-in did not render remote session rows")
    if machines_view.property("qaRemoteCount") != 2:
        raise AssertionError(f"Machines remote count was wrong: {machines_view.property('qaRemoteCount')}")
    if remote_row.property("qaTextFits") is not True or remote_open.property("qaTextFits") is not True:
        raise AssertionError("Machines remote session row text did not fit")
    click_item(app, window, "machinesRemoteOpen_remote_codex_box_s1")
    if controller.opened[-1:] != ["remote:codex-box:s1"]:
        raise AssertionError(f"Remote session open did not attach chat: {controller.opened}")
    if window.property("currentRoute") != "chat" or window.property("activeRouteIndex") != 3:
        raise AssertionError("Remote session open did not switch to chat")

    click_item(app, window, "navItem_crons")
    pump(app, 24)
    if ("Crons", ()) not in client.calls:
        raise AssertionError(f"Crons view did not fetch scheduled work: {client.calls}")
    crons_view = find_item_in_window(window, "cronsView")
    timer_row = find_item_in_window(window, "cronsTimerRow_eigen_dream_timer")
    tab_row = find_item_in_window(window, "cronsTabRow_0_9_______eigen_run_daily")
    if crons_view is None or timer_row is None or tab_row is None:
        raise AssertionError("Crons view did not render timer and crontab rows")
    if crons_view.property("qaTimerCount") != 2 or crons_view.property("qaCrontabCount") != 1:
        raise AssertionError(
            "Crons view summary counts were wrong: "
            f"timers={crons_view.property('qaTimerCount')} "
            f"crontab={crons_view.property('qaCrontabCount')}"
        )
    if timer_row.property("qaTextFits") is not True or tab_row.property("qaTextFits") is not True:
        raise AssertionError("Crons schedule row text did not fit")

    click_item(app, window, "navItem_plugins")
    pump(app, 24)
    if ("Plugins", ()) not in client.calls:
        raise AssertionError(f"Plugins view did not fetch inventory: {client.calls}")
    plugins_view = find_item_in_window(window, "pluginsView")
    installed_row = find_item_in_window(window, "pluginsInstalledRow_agentsys")
    risk_row = find_item_in_window(window, "pluginsInstalledRow_local_risk")
    market_row = find_item_in_window(window, "pluginsMarketRow_core")
    if plugins_view is None or installed_row is None or risk_row is None or market_row is None:
        raise AssertionError("Plugins view did not render installed plugins and marketplaces")
    if plugins_view.property("qaPluginCount") != 2 or plugins_view.property("qaMarketplaceCount") != 2:
        raise AssertionError(
            "Plugins view summary counts were wrong: "
            f"plugins={plugins_view.property('qaPluginCount')} "
            f"markets={plugins_view.property('qaMarketplaceCount')}"
        )
    if installed_row.property("qaTextFits") is not True or risk_row.property("qaTextFits") is not True or market_row.property("qaTextFits") is not True:
        raise AssertionError("Plugins inventory row text did not fit")

    click_item(app, window, "navItem_profile")
    pump(app, 24)
    if ("ObserveSummary", (5000,)) not in client.calls:
        raise AssertionError(f"Profile view did not fetch usage: {client.calls}")
    if ("MemoryForScope", ("global",)) not in client.calls:
        raise AssertionError(f"Profile view did not fetch the global profile: {client.calls}")
    profile_view = find_item_in_window(window, "profileView")
    turns_kpi = find_item_in_window(window, "profileKpi_turns")
    sessions_kpi = find_item_in_window(window, "profileKpi_sessions")
    model_row = find_item_in_window(window, "profileModelRow_gpt_5")
    profile_card = find_item_in_window(window, "profileUserCard")
    profile_edit = find_item_in_window(window, "profileEditButton")
    if profile_view is None or turns_kpi is None or sessions_kpi is None or model_row is None or profile_card is None:
        raise AssertionError("Profile view did not render usage and profile sections")
    if profile_view.property("qaRecordCount") != 4 or profile_view.property("qaModelCount") != 1:
        raise AssertionError(
            "Profile view usage counts were wrong: "
            f"records={profile_view.property('qaRecordCount')} "
            f"models={profile_view.property('qaModelCount')}"
        )
    if turns_kpi.property("qaTextFits") is not True or sessions_kpi.property("qaTextFits") is not True:
        raise AssertionError("Profile KPI text did not fit")
    if model_row.property("qaTextFits") is not True:
        raise AssertionError("Profile model row text did not fit")
    if profile_edit is None or profile_edit.property("qaTextFits") is not True:
        raise AssertionError("Profile edit button did not render cleanly")
    click_item(app, window, "profileEditButton")
    pump(app, 12)
    profile_text = find_item_in_window(window, "profileTextArea")
    profile_save = find_item_in_window(window, "profileSaveButton")
    if profile_text is None or profile_save is None:
        raise AssertionError("Profile editor did not open")
    if profile_text.property("qaIsAppTextArea") is not True:
        raise AssertionError("Profile editor did not use shared AppTextArea")
    if profile_text.property("qaTextFits") is not True:
        raise AssertionError("Profile editor text did not fit")
    if float(profile_text.property("qaHorizontalPadding") or 0) < 15.5:
        raise AssertionError(f"Profile editor horizontal padding too small: {profile_text.property('qaHorizontalPadding')}")
    if float(profile_text.property("qaVerticalPadding") or 0) < 7.5:
        raise AssertionError(f"Profile editor vertical padding too small: {profile_text.property('qaVerticalPadding')}")
    profile_text.setProperty("text", "Updated Qt profile proof")
    pump(app, 12)
    if profile_save.property("qaTextFits") is not True:
        raise AssertionError("Profile save button text did not fit")
    click_item(app, window, "profileSaveButton")
    pump(app, 18)
    if ("WriteUserProfile", ("Updated Qt profile proof",)) not in client.calls:
        raise AssertionError(f"Profile save did not call WriteUserProfile: {client.calls}")

    client.failures["WriteUserProfile"] = "profile daemon offline"
    click_item(app, window, "profileEditButton")
    pump(app, 12)
    profile_text = find_item_in_window(window, "profileTextArea")
    if profile_text is None:
        raise AssertionError("Profile editor did not reopen")
    if profile_text.property("qaIsAppTextArea") is not True or profile_text.property("qaTextFits") is not True:
        raise AssertionError("Reopened profile editor did not keep shared text-area geometry")
    profile_text.setProperty("text", "Retryable Qt profile proof")
    pump(app, 12)
    start = len(client.calls)
    click_item(app, window, "profileSaveButton")
    pump(app, 18)
    if ("WriteUserProfile", ("Retryable Qt profile proof",)) not in client.calls[start:]:
        raise AssertionError(f"Failed profile save did not call WriteUserProfile: {client.calls[start:]}")
    profile_error = find_item_in_window(window, "profileActionError")
    profile_error_text = find_item_in_window(window, "profileActionErrorText")
    profile_dismiss = find_item_in_window(window, "profileDismissActionError")
    if profile_error is None or profile_error.property("visible") is not True:
        raise AssertionError("Failed profile save did not render an action error")
    if profile_error_text is None or "profile daemon offline" not in profile_error_text.property("text"):
        raise AssertionError(f"Profile action error text was wrong: {profile_error_text.property('text') if profile_error_text else None}")
    if profile_dismiss is None or profile_dismiss.property("qaTextFits") is not True:
        raise AssertionError("Profile dismiss error button did not fit")
    click_item(app, window, "profileDismissActionError")
    pump(app, 12)
    if profile_view.property("profileModel").property("action_error") != "":
        raise AssertionError("Profile action error did not dismiss")
    del client.failures["WriteUserProfile"]

    client.failures["NewSession"] = "daemon offline"
    click_item(app, window, "navItem_live")
    start_calls = sum(1 for call in client.calls if call[0] == "NewSession")
    click_item(app, window, "liveNewSessionButton")
    QTest.qWait(40)
    pump(app, 18)
    if sum(1 for call in client.calls if call[0] == "NewSession") != start_calls + 1:
        raise AssertionError(f"Live new session did not call NewSession: {client.calls}")
    if window.property("pendingNewSessionToken") != 0:
        raise AssertionError("Failed shell NewSession left the pending token set")
    main_error = find_item_in_window(window, "mainActionError")
    main_error_text = find_item_in_window(window, "mainActionErrorText")
    main_error_dismiss = find_item_in_window(window, "mainDismissActionError")
    if main_error is None or main_error.property("visible") is not True:
        raise AssertionError("Main shell did not render failed NewSession error")
    if main_error_text is None or main_error_text.property("text") != "Could not start session: daemon offline":
        raise AssertionError(f"Main shell error text was wrong: {main_error_text.property('text') if main_error_text else None}")
    if main_error_dismiss is None or main_error_dismiss.property("qaTextFits") is not True:
        raise AssertionError("Main shell error dismiss button did not fit")
    assert_item_inside_window(main_error, "mainActionError")
    click_item(app, window, "mainDismissActionError")
    if window.property("actionError") != "":
        raise AssertionError("Main shell error did not dismiss")
    del client.failures["NewSession"]

    click_item(app, window, "navRunningSession_s_work")
    if controller.opened[-1:] != ["s-work"]:
        raise AssertionError(f"Running session click did not open s-work: {controller.opened}")
    if window.property("currentRoute") != "chat" or window.property("activeRouteIndex") != 3:
        raise AssertionError("Running session click did not switch to chat")
    session_status = find_item_in_window(window, "mainSessionStatus")
    if session_status is None or "s-work" not in session_status.property("text"):
        raise AssertionError("Main status strip did not show the active session")

    window.setProperty("width", max(900, int(window.minimumWidth())))
    window.setProperty("height", max(420, int(window.minimumHeight())))
    pump(app, 18)

    composer = find_item_in_window(window, "chatComposerTextArea")
    send_button = find_item_in_window(window, "chatSendButton")
    status_strip = find_item_in_window(window, "mainStatusStrip")
    if composer is None or send_button is None or status_strip is None:
        raise AssertionError("Main chat route did not render composer, send button, and status strip")
    if composer.property("qaIsAppTextArea") is not True:
        raise AssertionError("Main chat composer did not use shared AppTextArea")
    if composer.property("qaTextFits") is not True:
        raise AssertionError("Main chat composer text did not fit")
    if float(composer.property("qaHorizontalPadding") or 0) < 15.5:
        raise AssertionError(f"Main chat composer horizontal padding too small: {composer.property('qaHorizontalPadding')}")
    if float(composer.property("qaVerticalPadding") or 0) < 7.5:
        raise AssertionError(f"Main chat composer vertical padding too small: {composer.property('qaVerticalPadding')}")
    composer.setProperty("text", "prove main chat send")
    pump(app, 12)
    assert_item_inside_window(send_button, "chatSendButton")
    assert_item_inside_window(status_strip, "mainStatusStrip")
    if scene_bottom(send_button) > scene_top(status_strip) + 0.5:
        raise AssertionError(
            "Chat send button overlaps the bottom status strip: "
            f"send bottom={scene_bottom(send_button):.1f}, "
            f"status top={scene_top(status_strip):.1f}"
        )
    safe_bottom_inset = 40
    if not window.setProperty("bottomPadding", safe_bottom_inset):
        raise AssertionError("Main window does not expose bottomPadding for safe-area QA")
    pump(app, 18)
    safe_bottom = float(window.property("height") or 0) - safe_bottom_inset
    if scene_bottom(status_strip) > safe_bottom + 0.5:
        raise AssertionError(
            "Main status strip ignored bottom safe-area padding: "
            f"status bottom={scene_bottom(status_strip):.1f}, "
            f"safe bottom={safe_bottom:.1f}"
        )
    if scene_bottom(send_button) > scene_top(status_strip) + 0.5:
        raise AssertionError(
            "Chat send button overlaps the padded status strip: "
            f"send bottom={scene_bottom(send_button):.1f}, "
            f"status top={scene_top(status_strip):.1f}"
        )
    click_item(app, window, "chatSendButton")
    if ("SendInput", ("s-work", "prove main chat send", [], [])) not in client.calls:
        raise AssertionError(f"Main chat send button did not call SendInput: {client.calls}")

    send_count = sum(1 for call in client.calls if call[0] == "SendInput")
    composer.setProperty("text", "/rail")
    pump(app, 12)
    click_item(app, window, "chatSendButton")
    if window.property("railVisible") is not False:
        raise AssertionError("/rail did not hide the main navigation rail")
    rail = find_item_in_window(window, "mainRail")
    if rail is None or rail.property("visible") is not False:
        raise AssertionError("Main rail item did not become invisible after /rail")
    composer.setProperty("text", "/rail")
    pump(app, 12)
    click_item(app, window, "chatSendButton")
    if window.property("railVisible") is not True:
        raise AssertionError("Second /rail did not restore the main navigation rail")
    if sum(1 for call in client.calls if call[0] == "SendInput") != send_count:
        raise AssertionError(f"/rail leaked into SendInput: {client.calls}")

    assert_no_qml_issues(messages)
finally:
    qInstallMessageHandler(previous_handler)
"""
    env = os.environ.copy()
    env.setdefault("QT_QPA_PLATFORM", "offscreen")
    env.setdefault("QML_DISABLE_DISK_CACHE", "1")
    env.setdefault("PYTHONFAULTHANDLER", "1")

    result = subprocess.run(
        [sys.executable, "-c", textwrap.dedent(script)],
        cwd=ROOT,
        env=env,
        text=True,
        capture_output=True,
        timeout=20,
    )

    assert result.returncode == 0, result.stdout + result.stderr
