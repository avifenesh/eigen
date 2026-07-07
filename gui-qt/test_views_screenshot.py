#!/usr/bin/env python3
"""
test_views_screenshot.py — Capture screenshots of all main views with mock data.

Usage:
    cd gui-qt && QT_QPA_PLATFORM=offscreen .venv/bin/python3 test_views_screenshot.py

Screenshots saved to screenshots/qa-fix-*.png
"""

import sys
import atexit
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from PySide6.QtCore import QObject, Property, QPointF, QTimer, QUrl, Qt, Signal, Slot
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine
from PySide6.QtQuick import QQuickView
from PySide6.QtTest import QTest

from eigenqt.models.board import BoardModel, KanbanModel
from eigenqt.models.config import ConfigModel, RuleChainsModel
from eigenqt.models.memory import MemoryModel
from eigenqt.models.notes import NotesController
from eigenqt.models.connectors import ConnectorsModel
from eigenqt.models.reviewers import ReviewersModel
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

ROOT = Path(__file__).resolve().parent
SCREENSHOTS = ROOT / "screenshots"
SCREENSHOTS.mkdir(exist_ok=True)


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
        if method == "WorkingDiff":
            return {"isRepo": True, "clean": True, "branch": "fix/qt", "truncated": False, "patch": "", "files": []}
        if method == "FileTree":
            return {"truncated": False, "entries": [{"name": "README.md", "path": "/home/user/eigen/README.md", "isDir": False}]}
        if method == "ReadFileForView":
            return "# Eigen\n"
        if method == "Commands":
            return [
                {"name": "ship-it", "description": "Turn the current diff into a PR", "scope": "user"},
                {"name": "review", "description": "Custom review should not shadow the built-in", "scope": "user"},
            ]
        if method == "State":
            return {
                "id": args[0] if args else "s-qa-chat",
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
            return []
        if method == "MemoryForScope":
            return {}
        if method == "Config":
            return self.config_payload
        if method == "RuleChains":
            return self.rule_chains_payload
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


def capture_view(view_name: str, qml_file: str, setup_context, after_render=None):
    """Capture a single view screenshot."""
    print(f"Capturing {view_name}...")

    view = QQuickView()
    view.setResizeMode(QQuickView.SizeRootObjectToView)
    view.setWidth(1200)
    view.setHeight(800)

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


def capture_main_shell(client, clipboard_helper, highlighter, markdown_parser):
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
        "notesController": NotesController(client),
        "connectorsModel": ConnectorsModel(client),
        "configModel": ConfigModel(client),
        "ruleChainsModel": RuleChainsModel(client),
        "reviewersModel": ReviewersModel(client),
        "sessionController": controller,
        "transcriptModel": transcript_model,
        "approvalsModel": ApprovalsModel(client, ""),
        "daemonOnline": True,
        "guiserverSha": "qa1234567890",
        "statsData": {"running_turns": 2},
        "clipboardHelper": clipboard_helper,
        "highlighter": highlighter,
        "markdownParser": markdown_parser,
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

    window.setProperty("width", 900)
    window.setProperty("height", 420)
    QTest.qWait(80)
    for _ in range(14):
        app.processEvents()

    send_button = find_item(window.contentItem(), "chatSendButton")
    status_strip = find_item(window.contentItem(), "mainStatusStrip")
    if send_button is None or status_strip is None:
        print("✗ Main minimum chat proof could not find send button/status strip")
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

    output_compact = SCREENSHOTS / "qa-fix-main-chat-compact.png"
    image_compact = window.grabWindow()
    success_compact = image_compact.save(str(output_compact))
    if success_compact:
        print(f"✓ Saved {output_compact}")
    else:
        print(f"✗ Failed to save {output_compact}")

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
    return success and success_min and success_compact and success_safe


def main():
    global app

    # Create one app instance for all views
    app = QGuiApplication(sys.argv)

    client = ScreenshotRpcClient()
    clipboard_helper = ClipboardHelper(app)
    highlighter = HighlighterHelper(app)
    markdown_parser = MarkdownHelper(app)
    atexit.register(client.shutdown)
    ok = True

    # 1. SessionsView
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

    # 2. ChatView
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
        return {
            "sessionId": "s-qa-chat",
            "sessionStateModel": session_state,
            "commandsModel": commands_model,
            "transcriptModel": transcript_model,
            "approvalsModel": approvals_model,
            "rpcClient": client,
            "clipboardHelper": clipboard_helper,
            "highlighter": highlighter,
        }

    ok = capture_view("chat", "ChatView.qml", setup_chat) and ok

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
            composer.setProperty("text", "draft preserved while RPC is unavailable")
        root.setProperty("actionError", "Could not send message: RPC client is unavailable.")

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
            model.appendNote("opened worktree dock; no background shells reported")
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

    def open_config_model_dropdown(_view, root):
        root.setProperty("qaOpenCombo", "configSelect_model")

    ok = capture_view("config-model-dropdown", "ConfigView.qml", setup_config, open_config_model_dropdown) and ok

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
                        }
                    ],
                },
                {"id": "done", "title": "Done", "cards": []},
            ]
        }

        ctx.setContextProperty("boardModel", board_model)
        ctx.setContextProperty("kanbanModel", kanban_model)
        return {"boardModel": board_model, "kanbanModel": kanban_model, "rpcClient": client, "sessionsModel": None}

    ok = capture_view("board", "BoardView.qml", setup_board) and ok

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

    ok = capture_view("tasks-transcript-error", "TasksView.qml", setup_tasks, show_tasks_transcript_error) and ok

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
        proposals_model = ProposalsModel(client)
        proposals_model._on_skills_result({"result": {"proposals": []}})
        proposals_model._poll_timer.stop()
        ctx.setContextProperty("skillsModel", skills_model)
        ctx.setContextProperty("proposalsModel", proposals_model)
        ctx.setContextProperty("markdownParser", markdown_parser)
        ctx.setContextProperty("highlighter", highlighter)
        ctx.setContextProperty("clipboardHelper", clipboard_helper)
        return {"skillsModel": skills_model, "proposalsModel": proposals_model}

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

    # 9. ConnectorsView
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

    def show_connectors_remove_confirm(_view, root):
        model = root.property("connectorsModel")
        if model is not None:
            model.confirm_remove_connector_set("notion")
            model.confirm_remove_server_set("github-local")

    ok = capture_view("connectors-remove-confirm", "ConnectorsView.qml", setup_connectors, show_connectors_remove_confirm) and ok

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

    # 11. MemoryView
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

    def show_memory_remove_confirm(_view, root):
        model = root.property("memoryModel")
        if model is not None:
            model.setProperty("confirm_remove_ad_hoc", 0)
            model.setProperty("confirm_remove_note", 0)

    ok = capture_view("memory-remove-confirm", "MemoryView.qml", setup_memory, show_memory_remove_confirm) and ok

    def show_memory_move_dialog(_view, root):
        model = root.property("memoryModel")
        if model is not None:
            model.setProperty("move_pending", {"text": "Remember to test Qt views thoroughly", "idx": 0})
            model.setProperty("move_open", True)

    ok = capture_view("memory-move-dialog", "MemoryView.qml", setup_memory, show_memory_move_dialog) and ok

    # 12. Main shell with Chat route, proving the send button is not clipped by the status strip.
    ok = capture_main_shell(client, clipboard_helper, highlighter, markdown_parser) and ok

    if ok:
        print("\n✓ All screenshots captured")
        return 0
    print("\n✗ Screenshot capture failed")
    return 1


if __name__ == "__main__":
    sys.exit(main())
