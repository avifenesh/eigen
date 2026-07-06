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
        self._token = 0

    def call(self, method, *args, callback=None, error_callback=None):
        self.calls.append((method, args))
        payload = {"result": self._result(method, args)}
        if callback:
            QTimer.singleShot(0, lambda: callback(payload))

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
            return {"items": [], "fresh": False}
        if method == "Board":
            return {"lanes": []}
        if method == "Kanban":
            return {"columns": []}
        if method == "Skills":
            return {"skills": []}
        if method == "ProposedSkills":
            return {"proposals": []}
        if method == "ListMemoryScopes":
            return []
        if method == "MemoryForScope":
            return {"summary": "", "notes": [], "adHoc": [], "profile": "", "banList": []}
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
    live_model = LiveSessionsModel(client)
    tasks_model = TasksModel(client)
    dashboard_model = DashboardModel(client)
    feed_model = FeedModel(client)
    board_model = BoardModel(client)
    kanban_model = KanbanModel(client)
    skills_model = SkillsModel(client)
    proposals_model = ProposalsModel(client)
    memory_model = MemoryModel(client)
    notes_controller = NotesController(client)
    connectors_model = ConnectorsModel(client)
    config_model = ConfigModel(client)
    rule_chains_model = RuleChainsModel(client)
    reviewers_model = ReviewersModel(client)
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
    ctx.setContextProperty("skillsModel", skills_model)
    ctx.setContextProperty("proposalsModel", proposals_model)
    ctx.setContextProperty("memoryModel", memory_model)
    ctx.setContextProperty("notesController", notes_controller)
    ctx.setContextProperty("connectorsModel", connectors_model)
    ctx.setContextProperty("configModel", config_model)
    ctx.setContextProperty("ruleChainsModel", rule_chains_model)
    ctx.setContextProperty("reviewersModel", reviewers_model)
    ctx.setContextProperty("sessionController", controller)
    ctx.setContextProperty("transcriptModel", transcript_model)
    ctx.setContextProperty("approvalsModel", approvals_model)
    ctx.setContextProperty("daemonOnline", True)
    ctx.setContextProperty("guiserverSha", "abcdef1234567890")
    ctx.setContextProperty("statsData", {"running_turns": 2})
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
    sessions_nav = find_item_in_window(window, "navItem_sessions")
    running_row = find_item_in_window(window, "navRunningSession_s_work")
    approval_row = find_item_in_window(window, "navRunningSession_s_approval")
    if chat_nav is None or sessions_nav is None or running_row is None or approval_row is None:
        raise AssertionError(
            "Rail did not render chat, sessions, and live sub-rows: "
            f"chat={chat_nav is not None} sessions={sessions_nav is not None} "
            f"running={running_row is not None} approval={approval_row is not None}"
            f" count={chat_nav.property('qaRunningSessionCount') if chat_nav is not None else None}"
            f" delegates={chat_nav.property('qaRunningDelegateCount') if chat_nav is not None else None}"
            f" names={object_names_with_prefix(window.contentItem(), 'navRunning')}"
        )
    if float(chat_nav.property("height") or 0) <= 30:
        raise AssertionError("Chat nav item did not expand for running-session rows")
    if scene_top(running_row) < scene_top(chat_nav) + 30 - 0.5:
        raise AssertionError("Running session row overlaps the chat nav main row")
    if scene_top(sessions_nav) < scene_bottom(chat_nav) - 0.5:
        raise AssertionError("Sessions nav item overlaps the expanded chat running list")
    if chat_nav.property("qaTextFits") is not True:
        raise AssertionError("Chat nav label does not fit")

    route_expectations = [
        ("navItem_sessions", "sessions", 1),
        ("navItem_live", "live", 2),
        ("navItem_board", "board", 4),
        ("navItem_tasks", "tasks", 5),
        ("navItem_memory", "memory", 7),
        ("navItem_notes", "notes", 8),
        ("navItem_connectors", "connectors", 9),
        ("navItem_config", "config", 10),
        ("navItem_reviewers", "reviewers", 11),
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
