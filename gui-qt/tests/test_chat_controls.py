import os
import subprocess
import sys
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_chat_controls_use_shared_actions_and_keep_rpc_contracts():
    script = r"""
import base64
from pathlib import Path

from PySide6.QtCore import (
    QAbstractListModel,
    QModelIndex,
    QObject,
    QPoint,
    QPointF,
    QSize,
    QTimer,
    QUrl,
    Qt,
    Signal,
    Slot,
    Property,
)
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlContext
from PySide6.QtQuick import QQuickView
from PySide6.QtQuickControls2 import QQuickStyle
from PySide6.QtTest import QTest

import eigenqt.models.worktree  # registers DiffModel/FileTreeModel for DockPanel
from eigenqt.terminal_helper import TerminalHelper
from eigenqt.webengine import initialize_webengine


ROOT = Path.cwd()
SIZE = QSize(1180, 820)
VALID_PNG_BASE64 = (
    "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAACXBIWXMAAA7EAAAOxAGVKw4bAAAAG0lEQVQImWPUb3j7/4deMQPjp5M+/3/2cTMAAFRICM+3aAs3AAAAAElFTkSuQmCC"
)
EXPECTED_PLACEHOLDER_COLOR = "#52605e"


class FakeRpcClient(QObject):
    connected = Signal()
    callDone = Signal(int, "QVariantMap")
    event = Signal(str, dict)
    dropped = Signal(str)

    def __init__(self):
        super().__init__()
        self.calls = []
        self.failures = {}
        self.defer_methods = set()
        self.deferred = []
        self._token = 0

    def call(self, method, *args, callback=None, error_callback=None):
        self.calls.append((method, args))
        payload = {"result": self._result(method, args)}
        if method in self.defer_methods:
            if callback:
                self.deferred.append((callback, payload))
            return
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

    @Slot("QVariantList")
    def subscribe(self, channels):
        self.calls.append(("subscribe", tuple(channels or [])))

    @Slot("QVariantList")
    def unsubscribe(self, channels):
        self.calls.append(("unsubscribe", tuple(channels or [])))

    def shutdown(self):
        pass

    def flush_deferred(self):
        pending = list(self.deferred)
        self.deferred.clear()
        for callback, payload in pending:
            QTimer.singleShot(0, lambda cb=callback, p=payload: cb(p))

    def _result(self, method, args):
        if method == "WorkingDiff":
            return {"isRepo": True, "clean": True, "branch": "fix/qt", "truncated": False, "patch": "", "files": []}
        if method == "FileTree":
            return {"truncated": False, "entries": [{"name": "README.md", "path": "/repo/eigen/README.md", "isDir": False}]}
        if method == "ReadFileForView":
            return "# Eigen\n"
        if method == "TerminalStart":
            return "term-chat"
        if method == "Config":
            return {
                "fields": [
                    {
                        "key": "route",
                        "value": "true",
                        "desc": "Enable model-assessed routing.",
                        "options": ["true", "false"],
                    },
                    {"key": "route_providers", "value": "openai local"},
                ]
            }
        if method == "SetConfig":
            return args[1] if len(args) > 1 else ""
        if method == "SetTitle":
            title = args[1] if len(args) > 1 else ""
            return {
                "model": "local-qwen",
                "effort": "high",
                "perm": "auto",
                "title": title,
                "goal": "Tighten the GUI",
                "search": "auto",
                "fast": True,
                "fastOk": True,
                "tools": [
                    {"name": "read_file", "read_only": True},
                    {"name": "run_shell", "read_only": False},
                ],
                "running": False,
                "roots": ["/repo/eigen"],
                "catalog": {"models": [{"id": "local-qwen", "effortLevels": ["low", "medium", "high"]}]},
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
            return "/home/user/eigen-exports/s-chat.jsonl"
        if method == "SkillBody":
            name = args[0] if args else ""
            if name == "reviewer":
                return "Use the review tool before publishing risky changes."
            return ""
        if method == "RunCommand":
            name = args[1] if len(args) > 1 else ""
            command_args = args[2] if len(args) > 2 else ""
            if name == "ship-it":
                return "Ship the custom command target: " + command_args
            return ""
        if method == "Workflows":
            return [
                {"name": "ship", "description": "Prepare a careful release", "steps": 2},
                {"name": "audit", "description": "Review the current diff", "steps": 1},
            ]
        if method == "RunWorkflow":
            return {"completed": ["build", "test"], "failedAt": "", "outputs": {"test": "ok"}}
        if method == "AddBan":
            return False
        if method == "RemoveBan":
            return args[1] == "No broad rewrites" if len(args) > 1 else False
        if method == "VoiceStatus":
            return {"stt": True, "tts": False}
        if method == "VoiceModeStart":
            return None
        if method == "VoiceModeStop":
            return None
        if method == "VoiceListen":
            return "Dictated follow up"
        if method == "VoiceSpeak":
            return None
        if method == "Plugins":
            return {
                "plugins": [
                    {"name": "agent-tools", "hooks": 2},
                    {"name": "local-review"},
                ],
                "marketplaces": [{"name": "agent-sh"}],
            }
        if method == "ObserveSummary":
            return {
                "available": True,
                "records": 42,
                "tools": [{"name": "read_file", "calls": 7}],
                "models": [{"name": "gpt-5", "turns": 3}],
                "hooks": [{"name": "post_tool", "starts": 2}],
                "errors": [{"name": "tool_error", "count": 1}],
            }
        return {}

    def _state(self, **overrides):
        state = {
            "model": "local-qwen",
            "effort": "high",
            "perm": "auto",
            "title": "Qt chat controls",
            "goal": "Tighten the GUI",
            "search": "auto",
            "fast": True,
            "fastOk": True,
            "tools": [
                {"name": "read_file", "read_only": True},
                {"name": "run_shell", "read_only": False},
            ],
            "running": False,
            "roots": ["/repo/eigen", "/tmp/qt-proof"],
            "catalog": {"models": [{"id": "local-qwen", "effortLevels": ["low", "medium", "high"]}]},
            "messages": [],
            "pending": [],
        }
        state.update(overrides)
        return state


class FakeSessionState(QObject):
    modelChanged = Signal()
    providerChanged = Signal()
    tokensChanged = Signal()
    maxTokensChanged = Signal()
    effortChanged = Signal()
    permChanged = Signal()
    titleChanged = Signal()
    goalChanged = Signal()
    catalogChanged = Signal()
    effortLevelsChanged = Signal()
    dirChanged = Signal()
    actionErrorChanged = Signal()

    def __init__(self):
        super().__init__()
        self.calls = []
        self._model = "gpt-5"
        self._provider = "codex"
        self._tokens = 32000
        self._max_tokens = 128000
        self._effort = "medium"
        self._perm = "gated"
        self._title = "Qt chat controls"
        self._goal = "Tighten the GUI"
        self._search = "off"
        self._fast = False
        self._fast_ok = True
        self._tools = [
            {"name": "read_file", "read_only": True},
            {"name": "run_shell", "read_only": False},
        ]
        self._roots = ["/repo/eigen"]
        self._shells = [
            {"id": "sh-1", "command": "pytest -q gui-qt", "status": "running", "exit_code": 0, "last_line": "collecting"}
        ]
        self._pending = [
            {"id": "approval-1", "tool": "shell", "args": "{\"cmd\":\"make test\"}"}
        ]
        self._catalog = ["gpt-5", "local-qwen", "grok-4"]
        self._effort_levels = ["low", "medium", "high"]
        self._dir = "/repo/eigen"
        self._action_error = ""

    @Property(str, notify=modelChanged)
    def model(self):
        return self._model

    @Property(str, notify=providerChanged)
    def provider(self):
        return self._provider

    @Property(int, notify=tokensChanged)
    def tokens(self):
        return self._tokens

    @Property(int, notify=maxTokensChanged)
    def maxTokens(self):
        return self._max_tokens

    @Property(str, notify=effortChanged)
    def effort(self):
        return self._effort

    @Property(str, notify=permChanged)
    def perm(self):
        return self._perm

    @Property(str, notify=titleChanged)
    def title(self):
        return self._title

    @Property(str, notify=goalChanged)
    def goal(self):
        return self._goal

    @Property(str, notify=goalChanged)
    def search(self):
        return self._search

    @Property(bool, notify=goalChanged)
    def fast(self):
        return self._fast

    @Property(bool, notify=goalChanged)
    def fastOk(self):
        return self._fast_ok

    @Property(list, notify=goalChanged)
    def roots(self):
        return self._roots

    @Property(list, notify=goalChanged)
    def tools(self):
        return self._tools

    @Property(list, notify=goalChanged)
    def shells(self):
        return self._shells

    @Property(list, notify=goalChanged)
    def pending(self):
        return self._pending

    @Property(list, notify=catalogChanged)
    def catalog(self):
        return self._catalog

    @Property(list, notify=effortLevelsChanged)
    def effortLevels(self):
        return self._effort_levels

    @Property(str, notify=dirChanged)
    def dir(self):
        return self._dir

    @Property(str, notify=actionErrorChanged)
    def actionError(self):
        return self._action_error

    @Slot(str)
    def setActionError(self, message):
        self._action_error = message
        self.actionErrorChanged.emit()

    @Slot()
    def clearActionError(self):
        self.setActionError("")

    @Slot(str)
    def setModel(self, model):
        self.calls.append(("SetModel", model))
        self._model = model
        self.modelChanged.emit()

    @Slot(str)
    def setEffort(self, effort):
        self.calls.append(("SetEffort", effort))
        self._effort = effort
        self.effortChanged.emit()

    @Slot(str)
    def setPerm(self, perm):
        self.calls.append(("SetPerm", perm))
        self._perm = perm
        self.permChanged.emit()

    @Slot(str)
    def setTitle(self, title):
        self.calls.append(("SetTitle", title))
        self._title = title
        self.titleChanged.emit()

    @Slot(str)
    def setGoal(self, goal):
        self.calls.append(("SetGoal", goal))
        self._goal = goal
        self.goalChanged.emit()

    @Slot(str)
    def setSearch(self, search):
        self.calls.append(("SetSearch", search))
        self._search = search
        self.goalChanged.emit()

    @Slot(bool)
    def setFast(self, fast):
        self.calls.append(("SetFast", fast))
        self._fast = bool(fast)
        self.goalChanged.emit()

    @Slot(dict)
    def seed(self, state):
        title = state.get("title", "")
        if title != self._title:
            self._title = title
            self.titleChanged.emit()
        self._provider = state.get("provider", self._provider)
        self._tokens = state.get("tokens", self._tokens)
        self._max_tokens = state.get("maxTokens", self._max_tokens)
        self._goal = state.get("goal", self._goal)
        self._search = state.get("search", self._search)
        self._fast = bool(state.get("fast", self._fast))
        self._fast_ok = bool(state.get("fastOk", self._fast_ok))
        self._tools = state.get("tools", self._tools)
        self._roots = state.get("roots", self._roots)
        self._shells = state.get("shells", self._shells)
        self._pending = state.get("pending", self._pending)
        self.providerChanged.emit()
        self.tokensChanged.emit()
        self.maxTokensChanged.emit()
        self.goalChanged.emit()

    @Slot()
    def refresh(self):
        self.calls.append(("Refresh",))


class StaticTranscript(QAbstractListModel):
    streamingChanged = Signal()

    KindRole = Qt.UserRole + 1
    TextRole = Qt.UserRole + 2
    ToolNameRole = Qt.UserRole + 3
    ToolIdRole = Qt.UserRole + 4
    ToolArgsRole = Qt.UserRole + 5
    ToolStatusRole = Qt.UserRole + 6
    StreamingRole = Qt.UserRole + 7
    ReasoningRole = Qt.UserRole + 8
    BlocksRole = Qt.UserRole + 10

    def __init__(self, rows=None):
        super().__init__()
        self.rows = rows or []
        self._has_streaming = any(row.get("streaming", False) for row in self.rows)

    def roleNames(self):
        return {
            self.KindRole: b"kind",
            self.TextRole: b"text",
            self.ToolNameRole: b"toolName",
            self.ToolIdRole: b"toolId",
            self.ToolArgsRole: b"toolArgs",
            self.ToolStatusRole: b"toolStatus",
            self.StreamingRole: b"streaming",
            self.ReasoningRole: b"reasoning",
            self.BlocksRole: b"blocks",
        }

    def rowCount(self, parent=QModelIndex()):
        return 0 if parent.isValid() else len(self.rows)

    @Property(bool, notify=streamingChanged)
    def hasStreaming(self):
        return self._has_streaming

    def data(self, index, role=Qt.DisplayRole):
        if not index.isValid() or index.row() >= len(self.rows):
            return None
        row = self.rows[index.row()]
        if role == self.KindRole:
            return row.get("kind", "")
        if role == self.TextRole:
            return row.get("text", "")
        if role == self.ToolNameRole:
            return row.get("toolName", "")
        if role == self.ToolIdRole:
            return row.get("toolId", "")
        if role == self.ToolArgsRole:
            return row.get("toolArgs", "")
        if role == self.ToolStatusRole:
            return row.get("toolStatus", "")
        if role == self.StreamingRole:
            return row.get("streaming", False)
        if role == self.ReasoningRole:
            return row.get("reasoning", "")
        if role == self.BlocksRole:
            return row.get("blocks", [])
        return None

    @Slot(str)
    def appendNote(self, text):
        self.beginInsertRows(QModelIndex(), len(self.rows), len(self.rows))
        self.rows.append({"kind": "note", "text": text, "blocks": []})
        self.endInsertRows()

    @Slot(str)
    def appendUserMessage(self, text):
        self.beginInsertRows(QModelIndex(), len(self.rows), len(self.rows))
        self.rows.append({"kind": "user", "text": text, "blocks": []})
        self.endInsertRows()

    @Slot()
    def clearRows(self):
        self.beginResetModel()
        self.rows.clear()
        self.endResetModel()
        self.setStreaming(False)

    @Slot(bool)
    def setStreaming(self, streaming):
        streaming = bool(streaming)
        if self._has_streaming == streaming:
            return
        self._has_streaming = streaming
        self.streamingChanged.emit()

    @Slot(result=str)
    def lastAssistantText(self):
        for row in reversed(self.rows):
            if row.get("kind") == "assistant" and row.get("text"):
                return row.get("text", "")
        return ""


class ApprovalModel(QAbstractListModel):
    IdRole = Qt.UserRole + 1
    ToolRole = Qt.UserRole + 2
    ArgsRole = Qt.UserRole + 3
    ApprovingRole = Qt.UserRole + 4
    ErrorRole = Qt.UserRole + 5

    def __init__(self, rows=None):
        super().__init__()
        self.rows = rows or []

    def roleNames(self):
        return {
            self.IdRole: b"id",
            self.ToolRole: b"tool",
            self.ArgsRole: b"args",
            self.ApprovingRole: b"approving",
            self.ErrorRole: b"error",
        }

    def rowCount(self, parent=QModelIndex()):
        return 0 if parent.isValid() else len(self.rows)

    def data(self, index, role=Qt.DisplayRole):
        if not index.isValid() or index.row() >= len(self.rows):
            return None
        row = self.rows[index.row()]
        if role == self.IdRole:
            return row["id"]
        if role == self.ToolRole:
            return row["tool"]
        if role == self.ArgsRole:
            return row["args"]
        if role == self.ApprovingRole:
            return row.get("approving", False)
        if role == self.ErrorRole:
            return row.get("error", "")
        return None

    @Slot()
    def clearRows(self):
        self.beginResetModel()
        self.rows.clear()
        self.endResetModel()


class CommandModel(QAbstractListModel):
    loadErrorChanged = Signal()

    NameRole = Qt.UserRole + 1
    DescriptionRole = Qt.UserRole + 2
    ScopeRole = Qt.UserRole + 3

    def __init__(self):
        super().__init__()
        self._commands = [
            {"name": "compact", "description": "Compact older context", "scope": "builtin"},
            {"name": "config", "description": "Open or edit config", "scope": "builtin"},
            {"name": "clear", "description": "Clear the conversation", "scope": "builtin"},
            {"name": "goal", "description": "Show or set the session goal", "scope": "builtin"},
            {"name": "search", "description": "Show or set live search", "scope": "builtin"},
            {"name": "fast", "description": "Toggle fast tier", "scope": "builtin"},
            {"name": "add-dir", "description": "Grant a working directory", "scope": "builtin"},
            {"name": "tools", "description": "List tools available to this session", "scope": "builtin"},
            {"name": "copy", "description": "Copy the last assistant answer", "scope": "builtin"},
            {"name": "review", "description": "Ask for a cross-vendor review", "scope": "builtin"},
            {"name": "rename", "description": "Rename this session", "scope": "builtin"},
            {"name": "save", "description": "Export this session transcript", "scope": "builtin"},
            {"name": "export", "description": "Export this session transcript", "scope": "builtin"},
            {"name": "help", "description": "Show slash command help", "scope": "builtin"},
            {"name": "ship-it", "description": "Turn the current diff into a PR", "scope": "user"},
        ]
        self._filtered = list(self._commands)
        self.filters = []
        self._load_error = ""

    def roleNames(self):
        return {
            self.NameRole: b"name",
            self.DescriptionRole: b"description",
            self.ScopeRole: b"scope",
        }

    def rowCount(self, parent=QModelIndex()):
        return 0 if parent.isValid() else len(self._filtered)

    def data(self, index, role=Qt.DisplayRole):
        if not index.isValid() or index.row() >= len(self._filtered):
            return None
        row = self._filtered[index.row()]
        if role == self.NameRole:
            return row["name"]
        if role == self.DescriptionRole:
            return row["description"]
        if role == self.ScopeRole:
            return row["scope"]
        return None

    @Slot(str)
    def setFilter(self, filter_text):
        self.filters.append(filter_text)
        needle = (filter_text or "").lower().strip()
        next_filtered = [
            row for row in self._commands
            if not needle or row["name"].lower().startswith(needle)
        ]
        old_count = len(self._filtered)
        if old_count:
            self.beginRemoveRows(QModelIndex(), 0, old_count - 1)
            self._filtered = []
            self.endRemoveRows()
        if next_filtered:
            self.beginInsertRows(QModelIndex(), 0, len(next_filtered) - 1)
            self._filtered = next_filtered
            self.endInsertRows()

    @Slot(str, result="QVariantList")
    def filteredCommands(self, filter_text):
        self.filters.append(filter_text)
        needle = (filter_text or "").lower().strip()
        return [
            dict(row) for row in self._commands
            if not needle or row["name"].lower().startswith(needle)
        ]

    @Slot(str, result=str)
    def commandScope(self, name):
        needle = (name or "").lower()
        for row in self._commands:
            if row["name"].lower() == needle:
                return row.get("scope", "")
        return ""

    @Property(str, notify=loadErrorChanged)
    def loadError(self):
        return self._load_error

    def setLoadError(self, message):
        self._load_error = message
        self.loadErrorChanged.emit()

    @Slot()
    def clearLoadError(self):
        self.setLoadError("")


class FakeClipboard(QObject):
    def __init__(self):
        super().__init__()
        self.copied = []

    @Slot(result=str)
    def pasteImage(self):
        return ""

    @Slot(str)
    def copyText(self, text):
        self.copied.append(text)


class FakeHighlighter(QObject):
    @Slot(str, str, result=str)
    def highlight(self, lang, source):
        return source


def pump(app, rounds=12):
    for _ in range(rounds):
        app.processEvents()


def color_name(value):
    if hasattr(value, "name"):
        return value.name().lower()
    return str(value).lower()


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


def find_item_in(window, root, object_name):
    item = find_item(root, object_name)
    if item is None:
        item = find_item(window.contentItem(), object_name)
    return item


def assert_item_inside_window(item, label):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    if width <= 0 or height <= 0:
        raise AssertionError(f"{label} has invalid size {width}x{height}")
    top_left = item.mapToScene(QPointF(0, 0))
    bottom_right = item.mapToScene(QPointF(width, height))
    if (
        top_left.x() < -0.5
        or top_left.y() < -0.5
        or bottom_right.x() > SIZE.width() + 0.5
        or bottom_right.y() > SIZE.height() + 0.5
    ):
        raise AssertionError(
            f"{label} is outside the rendered window: "
            f"({top_left.x():.1f}, {top_left.y():.1f}) -> "
            f"({bottom_right.x():.1f}, {bottom_right.y():.1f}) "
            f"within {SIZE.width()}x{SIZE.height()}"
        )


def assert_combo_popup_clean(window, root, combo, label, option_index=0):
    if combo.property("qaTextFits") is not True:
        raise AssertionError(f"{label} text does not fit: {combo.property('qaText')!r}")
    assert_item_inside_window(combo, label)
    combo.setProperty("qaPopupOpen", True)
    pump(app, 12)
    if combo.property("qaPopupActuallyOpen") is not True:
        raise AssertionError(f"{label} popup did not open")
    if combo.property("qaPopupInsideWindow") is not True:
        raise AssertionError(
            f"{label} popup escaped the window: "
            f"above={combo.property('qaPopupAvailableAbove')} "
            f"below={combo.property('qaPopupAvailableBelow')} "
            f"height={combo.property('qaPopupEffectiveHeight')}"
        )
    option = find_item_in(window, root, f"{combo.objectName()}_option_{option_index}")
    if option is None:
        raise AssertionError(f"{label} popup did not expose option {option_index}")
    if option.property("qaTextFits") is not True:
        raise AssertionError(f"{label} popup option text does not fit: {option.property('qaText')!r}")
    combo.setProperty("qaPopupOpen", False)
    pump(app, 8)
    if combo.property("qaPopupActuallyOpen") is True:
        raise AssertionError(f"{label} popup did not close")


def item_center(window, item):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    if width <= 0 or height <= 0:
        raise AssertionError(f"{item.objectName()} has invalid size {width}x{height}")
    point = item.mapToItem(window.contentItem(), QPointF(width / 2, height / 2))
    return QPoint(max(0, min(SIZE.width() - 1, int(point.x()))), max(0, min(SIZE.height() - 1, int(point.y()))))


def click_item(app, window, root, object_name):
    pump(app, 8)
    item = find_item_in(window, root, object_name)
    if item is None:
        raise AssertionError(f"missing item {object_name}")
    assert_item_inside_window(item, object_name)
    QTest.mouseClick(window, Qt.LeftButton, Qt.NoModifier, item_center(window, item))
    QTest.qWait(20)
    pump(app, 18)
    return item


def call_count(client, method):
    return sum(1 for call in client.calls if call[0] == method)


def make_view(source, context_props=None, initial=None):
    view = QQuickView()
    view.setResizeMode(QQuickView.SizeRootObjectToView)
    view.setWidth(SIZE.width())
    view.setHeight(SIZE.height())
    view.engine().addImportPath(str(ROOT / "eigenqt"))
    ctx: QQmlContext = view.rootContext()
    for name, value in (context_props or {}).items():
        ctx.setContextProperty(name, value)
    if initial:
        view.setInitialProperties(initial)
    view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / source)))
    if view.status() == QQuickView.Error or view.rootObject() is None:
        raise AssertionError([error.toString() for error in view.errors()])
    view.show()
    pump(QGuiApplication.instance(), 30)
    return view, view.rootObject()


QQuickStyle.setStyle("Basic")
initialize_webengine()
app = QGuiApplication([])
client = FakeRpcClient()
state = FakeSessionState()
commands = CommandModel()
clipboard = FakeClipboard()
highlighter = FakeHighlighter()
terminal_helper = TerminalHelper(app)
transcript = StaticTranscript(
    [
        {
            "kind": "assistant",
            "text": "Ready for the next instruction.",
            "toolStatus": "running",
            "streaming": False,
            "blocks": [],
        }
    ]
)
approvals = ApprovalModel()

offline_client = FakeRpcClient()
offline_view, offline_chat = make_view(
    "ChatView.qml",
    {},
    {
        "sessionId": "s-offline",
        "sessionStateModel": FakeSessionState(),
        "commandsModel": CommandModel(),
        "transcriptModel": StaticTranscript([]),
        "approvalsModel": ApprovalModel(),
        "clipboardHelper": clipboard,
        "highlighter": highlighter,
        "terminalHelper": terminal_helper,
    },
)
offline_composer = find_item(offline_chat, "chatComposerTextArea")
if offline_composer is None:
    raise AssertionError("Offline composer did not render")
if color_name(offline_composer.property("placeholderTextColor")) != EXPECTED_PLACEHOLDER_COLOR:
    raise AssertionError(
        f"Chat composer placeholder color regressed: {color_name(offline_composer.property('placeholderTextColor'))}"
    )
click_item(app, offline_view, offline_chat, "chatDockToggleButton")
if offline_chat.property("dockOpen") is not False:
    raise AssertionError("Offline dock toggle opened the dock without RPC")
offline_error = find_item(offline_chat, "chatActionError")
offline_error_text = find_item(offline_chat, "chatActionErrorText")
if offline_error is None or offline_error.property("visible") is not True:
    raise AssertionError("Offline dock toggle did not show a visible chat action error")
if offline_error_text is None or "RPC client is unavailable" not in offline_error_text.property("text"):
    raise AssertionError(f"Offline dock error text was wrong: {offline_error_text.property('text') if offline_error_text else None}")
click_item(app, offline_view, offline_chat, "chatDismissActionError")
if offline_chat.property("actionError") != "":
    raise AssertionError("Offline dock action error did not dismiss")
offline_composer.setProperty("text", "keep this draft")
pump(app, 8)
offline_start = len(offline_client.calls)
click_item(app, offline_view, offline_chat, "chatSendButton")
if offline_client.calls[offline_start:]:
    raise AssertionError(f"Offline send unexpectedly reached RPC: {offline_client.calls}")
if offline_composer.property("text") != "keep this draft":
    raise AssertionError("Offline send cleared the draft")
offline_error = find_item(offline_chat, "chatActionError")
offline_error_text = find_item(offline_chat, "chatActionErrorText")
if offline_error is None or offline_error.property("visible") is not True:
    raise AssertionError("Offline send did not show a visible chat action error")
if offline_error_text is None or "RPC client is unavailable" not in offline_error_text.property("text"):
    raise AssertionError(f"Offline send error text was wrong: {offline_error_text.property('text') if offline_error_text else None}")
click_item(app, offline_view, offline_chat, "chatDismissActionError")
if offline_chat.property("actionError") != "":
    raise AssertionError("Offline chat action error did not dismiss")
offline_view.hide()
offline_view.setSource(QUrl())

chat_view, chat = make_view(
    "ChatView.qml",
    {},
    {
        "sessionId": "s-chat",
        "sessionStateModel": state,
        "commandsModel": commands,
        "transcriptModel": transcript,
        "approvalsModel": approvals,
        "rpcClient": client,
        "clipboardHelper": clipboard,
        "highlighter": highlighter,
        "terminalHelper": terminal_helper,
    },
)
back_count = []
route_events = []
rail_events = []
chat.backClicked.connect(lambda: back_count.append(1))
chat.routeRequested.connect(lambda route: route_events.append(route))
chat.railToggleRequested.connect(lambda: rail_events.append(1))
if chat.property("qaTranscriptRows") != 1:
    raise AssertionError(f"Seeded transcript did not reach ChatView: {chat.property('qaTranscriptRows')}")
if float(chat.property("qaTranscriptContentHeight") or 0) <= 0:
    raise AssertionError("Seeded transcript rendered with zero height")

click_item(app, chat_view, chat, "chatBackButton")
if len(back_count) != 1:
    raise AssertionError("Back button did not emit backClicked")

dock = click_item(app, chat_view, chat, "chatDockToggleButton")
if chat.property("dockOpen") is not True or not dock.property("selected"):
    raise AssertionError("Dock toggle did not open/select the dock")
if ("WorkingDiff", ("/repo/eigen",)) not in client.calls or ("FileTree", ("/repo/eigen",)) not in client.calls:
    raise AssertionError(f"Dock did not request worktree data: {client.calls}")

diff_count = call_count(client, "WorkingDiff")
diff_refresh = click_item(app, chat_view, chat, "diffRefreshButton")
if call_count(client, "WorkingDiff") <= diff_count:
    raise AssertionError(f"Diff refresh did not request WorkingDiff again: {client.calls}")
if not diff_refresh.property("qaTextFits"):
    raise AssertionError("Diff refresh button text does not fit")

files_tab = click_item(app, chat_view, chat, "dockTab_Files")
if files_tab.property("selected") is not True:
    raise AssertionError("Files dock tab did not become selected")
files_count = call_count(client, "FileTree")
files_refresh = click_item(app, chat_view, chat, "filesRefreshButton")
if call_count(client, "FileTree") <= files_count:
    raise AssertionError(f"Files refresh did not request FileTree again: {client.calls}")
if not files_refresh.property("qaTextFits"):
    raise AssertionError("Files refresh button text does not fit")

files_root = find_item(chat, "filesTabRoot")
if files_root is None:
    raise AssertionError("Files tab root did not render")
files_root.setProperty("viewPath", "/repo/eigen/README.md")
files_root.setProperty("viewText", "# Eigen")
pump(app, 12)
viewer_close = find_item(chat, "filesViewerCloseButton")
if viewer_close is None:
    raise AssertionError("Files viewer close button did not render")
if not viewer_close.property("qaTextFits"):
    raise AssertionError("Files viewer close button text does not fit")
assert_item_inside_window(viewer_close, "filesViewerCloseButton")
click_item(app, chat_view, chat, "filesViewerCloseButton")
if files_root.property("viewPath") != "":
    raise AssertionError("Files viewer close button did not clear the viewer")

info_tab = click_item(app, chat_view, chat, "dockTab_Info")
if info_tab.property("selected") is not True:
    raise AssertionError("Info dock tab did not become selected")
info_title = find_item(chat, "dockInfoTitle")
info_model = find_item(chat, "dockInfoModel")
info_provider = find_item(chat, "dockInfoProvider")
info_context = find_item(chat, "dockInfoContextSummary")
info_goal = find_item(chat, "dockInfoGoal")
info_root = find_item(chat, "dockInfoRoot_0")
info_shell = find_item(chat, "dockInfoShell_0")
info_pending = find_item(chat, "dockInfoPending_0")
info_shells = find_item(chat, "dockInfoShellsSummary")
info_pending_summary = find_item(chat, "dockInfoPendingSummary")
info_tools = find_item(chat, "dockInfoToolsSummary")
if None in (
    info_title,
    info_model,
    info_provider,
    info_context,
    info_goal,
    info_root,
    info_shell,
    info_pending,
    info_shells,
    info_pending_summary,
    info_tools,
):
    raise AssertionError("Info dock tab did not render its session metadata")
if info_title.property("text") != "Qt chat controls":
    raise AssertionError(f"Info dock title was wrong: {info_title.property('text')}")
if "gpt-5 / medium / gated" not in info_model.property("text"):
    raise AssertionError(f"Info dock model summary was wrong: {info_model.property('text')}")
if info_provider.property("text") != "codex":
    raise AssertionError(f"Info dock provider was wrong: {info_provider.property('text')}")
if info_context.property("text") != "32,000 / 128,000 (25%)":
    raise AssertionError(f"Info dock context summary was wrong: {info_context.property('text')}")
if info_goal.property("text") != "Tighten the GUI":
    raise AssertionError(f"Info dock goal was wrong: {info_goal.property('text')}")
if info_shells.property("text") != "1 shell":
    raise AssertionError(f"Info dock shell summary was wrong: {info_shells.property('text')}")
if info_pending_summary.property("text") != "1 approval":
    raise AssertionError(f"Info dock approval summary was wrong: {info_pending_summary.property('text')}")
if info_tools.property("text") != "2 tools (1 read, 1 write)":
    raise AssertionError(f"Info dock tool summary was wrong: {info_tools.property('text')}")

if QGuiApplication.platformName().lower() != "offscreen":
    browser_tab = click_item(app, chat_view, chat, "dockTab_Browser")
    if browser_tab.property("selected") is not True:
        raise AssertionError("Browser dock tab did not become selected")
    browser_root = find_item(chat, "browserTab")
    browser_address = find_item(chat, "browserAddressField")
    browser_go = find_item(chat, "browserGoButton")
    browser_external = find_item(chat, "browserOpenExternalButton")
    browser_empty = find_item(chat, "browserEmptyState")
    browser_view = find_item(chat, "browserWebView")
    if None in (browser_root, browser_address, browser_go, browser_external, browser_empty):
        raise AssertionError("Browser dock tab did not render its controls")
    if browser_view is not None or browser_root.property("qaBrowserLoaded") is not False:
        raise AssertionError("Browser dock eagerly loaded WebEngine before navigation")
    if browser_root.property("qaEmptyStateVisible") is not True:
        raise AssertionError("Browser dock did not show its blank state")
    browser_address.setProperty("text", "localhost:4321/docs")
    pump(app, 8)
    click_item(app, chat_view, chat, "browserGoButton")
    pump(app, 20)
    browser_view = find_item(chat, "browserWebView")
    if browser_view is None or browser_root.property("qaBrowserLoaded") is not True:
        raise AssertionError("Browser dock did not lazy-load WebEngine after navigation")
    if browser_root.property("qaEmptyStateVisible") is not False:
        raise AssertionError("Browser dock kept the empty state after navigation")
    if browser_address.property("text") != "http://localhost:4321/docs":
        raise AssertionError(f"Browser dock did not normalize localhost URL: {browser_address.property('text')}")
    if not browser_go.property("qaTextFits"):
        raise AssertionError("Browser Go button text does not fit")
    if not browser_external.property("qaTextFits"):
        raise AssertionError("Browser external button text does not fit")

diff_tab = click_item(app, chat_view, chat, "dockTab_Diff")
if diff_tab.property("selected") is not True:
    raise AssertionError("Diff dock tab did not become selected")
dock_close = find_item(chat, "dockCloseButton")
if dock_close is None:
    raise AssertionError("Dock close button did not render")
if not dock_close.property("qaTextFits"):
    raise AssertionError("Dock close button text does not fit")
click_item(app, chat_view, chat, "dockCloseButton")
if chat.property("dockOpen") is not False:
    raise AssertionError("Dock close button did not close the dock")

model_combo = find_item(chat, "sessionModelCombo")
perm_combo = find_item(chat, "sessionPermCombo")
effort_combo = find_item(chat, "sessionEffortCombo")
search_combo = find_item(chat, "sessionSearchCombo")
fast_switch = find_item(chat, "sessionFastSwitch")
title_field = find_item(chat, "sessionTitleField")
if (
    model_combo is None
    or perm_combo is None
    or effort_combo is None
    or search_combo is None
    or fast_switch is None
    or title_field is None
):
    raise AssertionError("Session settings controls did not render")
if model_combo.property("qaText") != "gpt-5":
    raise AssertionError(f"Model combo did not show current model: {model_combo.property('qaText')}")
if search_combo.property("qaText") != "off":
    raise AssertionError(f"Search combo did not show current search mode: {search_combo.property('qaText')}")
if title_field.property("qaText") != "Qt chat controls":
    raise AssertionError(f"Title field did not show current title: {title_field.property('qaText')}")
if title_field.property("qaTextFits") is not True:
    raise AssertionError(f"Title field text does not fit: {title_field.property('qaText')!r}")
assert_item_inside_window(title_field, "sessionTitleField")
QTest.mouseDClick(chat_view, Qt.LeftButton, Qt.NoModifier, item_center(chat_view, title_field))
pump(app, 18)
if title_field.property("qaEditing") is not True:
    raise AssertionError("Double-click did not put the session title field into edit mode")
title_field.setProperty("text", "Qt title edited from field")
QTest.keyClick(chat_view, Qt.Key_Return)
pump(app, 18)
if ("SetTitle", "Qt title edited from field") not in state.calls:
    raise AssertionError(f"Title field did not call setTitle: {state.calls}")
if title_field.property("qaEditing") is not False:
    raise AssertionError("Return did not leave the title field read-only after editing")
if title_field.property("qaTextFits") is not True:
    raise AssertionError(f"Edited title field text does not fit: {title_field.property('qaText')!r}")
title_field.forceActiveFocus()
QTest.keyClick(chat_view, Qt.Key_F2)
pump(app, 18)
if title_field.property("qaEditing") is not True:
    raise AssertionError("F2 did not put the session title field into edit mode")
title_field.setProperty("text", "Qt title edited with keyboard")
QTest.keyClick(chat_view, Qt.Key_Return)
pump(app, 18)
if ("SetTitle", "Qt title edited with keyboard") not in state.calls:
    raise AssertionError(f"Title field F2 edit did not call setTitle: {state.calls}")
if title_field.property("qaEditing") is not False:
    raise AssertionError("Return did not leave the F2 title edit read-only")
assert_combo_popup_clean(chat_view, chat, model_combo, "session model combo", 0)
assert_combo_popup_clean(chat_view, chat, perm_combo, "session permission combo", 0)
assert_combo_popup_clean(chat_view, chat, effort_combo, "session effort combo", 0)
assert_combo_popup_clean(chat_view, chat, search_combo, "session search combo", 0)
model_combo.setProperty("qaForceKeyboardFocus", True)
pump(app, 8)
QTest.keyClick(chat_view, Qt.Key_Down)
pump(app, 12)
if model_combo.property("qaPopupActuallyOpen") is not True:
    raise AssertionError("Model combo Down key did not open the popup")
if model_combo.property("qaKeyboardIndex") != 1:
    raise AssertionError(f"Model combo Down key did not highlight the next option: {model_combo.property('qaKeyboardIndex')}")
QTest.keyClick(chat_view, Qt.Key_Escape)
pump(app, 12)
if model_combo.property("qaPopupActuallyOpen") is True:
    raise AssertionError("Model combo Escape did not close the popup")
QTest.keyClick(chat_view, Qt.Key_Down)
pump(app, 8)
QTest.keyClick(chat_view, Qt.Key_Return)
pump(app, 18)
if ("SetModel", "local-qwen") not in state.calls:
    raise AssertionError(f"Model combo keyboard activation did not call setModel: {state.calls}")
if model_combo.property("qaPopupActuallyOpen") is True:
    raise AssertionError("Model combo keyboard activation did not close the popup")
click_item(app, chat_view, chat, "sessionModelCombo")
click_item(app, chat_view, chat, "sessionModelCombo_option_1")
click_item(app, chat_view, chat, "sessionPermCombo")
click_item(app, chat_view, chat, "sessionPermCombo_option_1")
click_item(app, chat_view, chat, "sessionEffortCombo")
click_item(app, chat_view, chat, "sessionEffortCombo_option_2")
click_item(app, chat_view, chat, "sessionSearchCombo")
click_item(app, chat_view, chat, "sessionSearchCombo_option_2")
click_item(app, chat_view, chat, "sessionFastSwitch")
pump(app, 12)
if ("SetModel", "local-qwen") not in state.calls:
    raise AssertionError(f"Model combo did not call setModel: {state.calls}")
if ("SetPerm", "auto") not in state.calls:
    raise AssertionError(f"Perm combo did not call setPerm: {state.calls}")
if ("SetEffort", "high") not in state.calls:
    raise AssertionError(f"Effort combo did not call setEffort: {state.calls}")
if ("SetSearch", "on") not in state.calls:
    raise AssertionError(f"Search combo did not call setSearch: {state.calls}")
if ("SetFast", True) not in state.calls:
    raise AssertionError(f"Fast switch did not call setFast: {state.calls}")

QTest.keyClick(chat_view, Qt.Key_Escape)
pump(app, 18)
state.setActionError("Could not set model: daemon offline")
pump(app, 18)
error_banner = find_item(chat, "chatActionError")
error_text = find_item(chat, "chatActionErrorText")
if error_banner is None or error_banner.property("visible") is not True:
    raise AssertionError("Session setting error did not show a visible chat action error")
if error_text is None or "daemon offline" not in error_text.property("text"):
    raise AssertionError(f"Session setting error text was wrong: {error_text.property('text') if error_text else None}")
dismiss = find_item(chat, "chatDismissActionError")
if dismiss is None or not dismiss.property("qaTextFits"):
    raise AssertionError("Session setting error dismiss button did not render cleanly")
assert_item_inside_window(dismiss, "chatDismissActionError")
chat.setProperty("dismissedSessionActionError", chat.property("sessionActionError"))
pump(app, 18)
if chat.property("visibleActionError") != "":
    raise AssertionError("Session setting action error did not dismiss")

commands.setLoadError("Could not load custom slash commands: daemon offline")
pump(app, 18)
if "custom slash commands" not in chat.property("visibleActionError"):
    raise AssertionError(f"Commands load error did not surface in chat banner: {chat.property('visibleActionError')!r}")
error_banner = find_item(chat, "chatActionError")
error_text = find_item(chat, "chatActionErrorText")
if error_banner is None or error_banner.property("visible") is not True:
    raise AssertionError("Commands load error did not show a visible chat action error")
if error_text is None or "daemon offline" not in error_text.property("text"):
    raise AssertionError(f"Commands load error text was wrong: {error_text.property('text') if error_text else None}")
dismiss = find_item(chat, "chatDismissActionError")
if dismiss is None or not dismiss.property("qaTextFits"):
    raise AssertionError("Commands load error dismiss button did not render cleanly")
assert_item_inside_window(dismiss, "chatDismissActionError commands")
chat.setProperty("dismissedCommandsLoadError", chat.property("commandsLoadError"))
pump(app, 18)
if chat.property("visibleActionError") != "":
    raise AssertionError("Commands load error did not dismiss")

composer = find_item(chat, "chatComposerTextArea")
if composer is None:
    raise AssertionError("Composer text area did not expose an objectName")
composer.forceActiveFocus()
pump(app, 8)
QTest.keyClick(chat_view, Qt.Key_Slash)
pump(app, 18)
if chat.property("qaSlashPopupOpen") is not True:
    raise AssertionError("Slash command popup did not open after typing /")
if chat.property("qaSlashPopupInsideWindow") is not True:
    raise AssertionError("Slash command popup escaped the ChatView bounds")
if composer.property("activeFocus") is not True:
    raise AssertionError("Slash command popup stole focus from the composer")
QTest.keyClick(chat_view, Qt.Key_C)
pump(app, 18)
if composer.property("text") != "/c" or commands.filters[-1:] != ["c"]:
    raise AssertionError(f"Slash command filter did not track composer text: text={composer.property('text')!r} filters={commands.filters}")
QTest.keyClick(chat_view, Qt.Key_Return)
pump(app, 18)
if composer.property("text") != "/compact ":
    raise AssertionError(f"Return did not complete highlighted slash command: {composer.property('text')!r}")
if chat.property("qaSlashPopupOpen") is True:
    raise AssertionError("Slash popup stayed open after keyboard completion")
if any(call[0] in {"SendInput", "SteerInput"} for call in client.calls):
    raise AssertionError(f"Completing slash command submitted chat input: {client.calls}")

composer.setProperty("text", "/h")
composer.setProperty("cursorPosition", 2)
composer.forceActiveFocus()
pump(app, 18)
help_option = find_item_in(chat_view, chat, "slashCommandOption_help")
if help_option is None:
    raise AssertionError("Slash command option did not expose objectName for mouse QA")
if not help_option.property("qaTextFits"):
    raise AssertionError("Slash command option text does not fit")
click_item(app, chat_view, chat, "slashCommandOption_help")
if composer.property("text") != "/help ":
    raise AssertionError(f"Clicking slash command option did not complete composer: {composer.property('text')!r}")

send_start = call_count(client, "SendInput") + call_count(client, "SteerInput")
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"/help was sent to the model instead of handled locally: {client.calls}")
if composer.property("text") != "":
    raise AssertionError("/help did not clear the composer after local handling")
if not transcript.rows or transcript.rows[-1]["kind"] != "note" or "Qt slash commands" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/help did not append local slash help: {transcript.rows[-1:]}")

composer.setProperty("text", "/model local-qwen")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if ("SetModel", "local-qwen") not in state.calls:
    raise AssertionError(f"/model did not call session state setModel: {state.calls}")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"/model was sent as chat input: {client.calls}")
if "model -> local-qwen" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/model did not append a local note: {transcript.rows[-1:]}")

composer.setProperty("text", "/perm auto")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if ("SetPerm", "auto") not in state.calls:
    raise AssertionError(f"/perm did not call session state setPerm: {state.calls}")
if "permission posture -> auto" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/perm did not append a local note: {transcript.rows[-1:]}")

composer.setProperty("text", "/effort high")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if ("SetEffort", "high") not in state.calls:
    raise AssertionError(f"/effort did not call session state setEffort: {state.calls}")
if "reasoning effort -> high" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/effort did not append a local note: {transcript.rows[-1:]}")

composer.setProperty("text", "/route on")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("SetConfig", ("route", "true")) not in client.calls:
    raise AssertionError(f"/route on did not call SetConfig: {client.calls}")
if "model-assessed routing ON" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/route on did not append a local note: {transcript.rows[-1:]}")

composer.setProperty("text", "/route")
pump(app, 8)
config_start = call_count(client, "Config")
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if call_count(client, "Config") <= config_start:
    raise AssertionError(f"/route did not query Config: {client.calls}")
if "routing: on" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/route did not append routing status: {transcript.rows[-1:]}")

composer.setProperty("text", "/config route")
pump(app, 8)
config_start = call_count(client, "Config")
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if call_count(client, "Config") <= config_start:
    raise AssertionError(f"/config key did not query Config: {client.calls}")
config_note = transcript.rows[-1]["text"]
if "route = true" not in config_note or "Enable model-assessed routing." not in config_note or "values: true|false" not in config_note:
    raise AssertionError(f"/config key did not append field details: {transcript.rows[-1:]}")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"/config key was sent as chat input: {client.calls}")

composer.setProperty("text", "/goal")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if "goal: Tighten the GUI" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/goal did not append goal status: {transcript.rows[-1:]}")

composer.setProperty("text", "/goal clear")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("SetGoal", ("s-chat", "")) not in client.calls:
    raise AssertionError(f"/goal clear did not call SetGoal: {client.calls}")
if "goal cleared" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/goal clear did not append feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/goal Ship the Qt shell")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("SetGoal", ("s-chat", "Ship the Qt shell")) not in client.calls:
    raise AssertionError(f"/goal text did not call SetGoal: {client.calls}")
if "goal -> Ship the Qt shell" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/goal text did not append feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/search")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if "live search:" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/search did not append search status: {transcript.rows[-1:]}")

composer.setProperty("text", "/search auto")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("SetSearch", ("s-chat", "auto")) not in client.calls:
    raise AssertionError(f"/search auto did not call SetSearch: {client.calls}")
if "live search -> auto" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/search auto did not append feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/fast off")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("SetFast", ("s-chat", False)) not in client.calls:
    raise AssertionError(f"/fast off did not call SetFast: {client.calls}")
if "fast mode -> off" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/fast off did not append feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/add-dir")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if "working directories:" not in transcript.rows[-1]["text"] or "/repo/eigen" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/add-dir did not list working directories: {transcript.rows[-1:]}")

composer.setProperty("text", "/add-dir /tmp/qt-proof")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("AddDir", ("s-chat", "/tmp/qt-proof")) not in client.calls:
    raise AssertionError(f"/add-dir path did not call AddDir: {client.calls}")
if ("Refresh",) not in state.calls:
    raise AssertionError(f"/add-dir path did not refresh session state: {state.calls}")
if "added working directory -> /tmp/qt-proof" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/add-dir path did not append feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/workflow")
pump(app, 8)
workflow_start = call_count(client, "Workflows")
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if call_count(client, "Workflows") <= workflow_start:
    raise AssertionError(f"/workflow did not call Workflows: {client.calls}")
workflow_note = transcript.rows[-1]["text"]
if "workflows:" not in workflow_note or "ship (2 steps)" not in workflow_note or "Prepare a careful release" not in workflow_note:
    raise AssertionError(f"/workflow did not list authored workflows: {transcript.rows[-1:]}")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"/workflow list leaked into SendInput/SteerInput: {client.calls}")

composer.setProperty("text", "/workflow ship target=qt mode=careful ignored")
pump(app, 8)
run_workflow_start = call_count(client, "RunWorkflow")
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if call_count(client, "RunWorkflow") <= run_workflow_start:
    raise AssertionError(f"/workflow name did not call RunWorkflow: {client.calls}")
workflow_calls = [call for call in client.calls if call[0] == "RunWorkflow"]
workflow_args = workflow_calls[-1][1]
if workflow_args[0] != "s-chat" or workflow_args[1] != "ship" or workflow_args[2] != {"target": "qt", "mode": "careful"}:
    raise AssertionError(f"/workflow did not pass parsed vars: {workflow_args!r}")
if ("Refresh",) not in state.calls:
    raise AssertionError(f"/workflow did not refresh session state: {state.calls}")
if not any(row["kind"] == "note" and row["text"] == "workflow ship started" for row in transcript.rows):
    raise AssertionError(f"/workflow did not append a started note: {transcript.rows[-4:]}")
if "workflow ship: 2 steps complete" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/workflow did not append completion feedback: {transcript.rows[-1:]}")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"/workflow leaked into SendInput/SteerInput: {client.calls}")

composer.setProperty("text", "/ban")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if route_events[-1:] != ["memory"]:
    raise AssertionError(f"Bare /ban did not route to memory: {route_events}")
if "/ban <title>: <rule>" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"Bare /ban did not append usage guidance: {transcript.rows[-1:]}")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"Bare /ban leaked into SendInput/SteerInput: {client.calls}")

composer.setProperty("text", "/ban No broad rewrites: Stay scoped to the Qt surface.")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("AddBan", ("project", "No broad rewrites", "Stay scoped to the Qt surface.")) not in client.calls:
    raise AssertionError(f"/ban did not call AddBan with the parsed title and rule: {client.calls}")
if "banned: No broad rewrites" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/ban did not append ban feedback: {transcript.rows[-1:]}")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"/ban leaked into SendInput/SteerInput: {client.calls}")

composer.setProperty("text", "/ban Missing rule")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if "usage: /ban <title>: <rule>" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/ban without a rule did not append usage feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/unban No broad rewrites")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("RemoveBan", ("project", "No broad rewrites")) not in client.calls:
    raise AssertionError(f"/unban did not call RemoveBan with the title: {client.calls}")
if "removed ban: No broad rewrites" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/unban did not append removal feedback: {transcript.rows[-1:]}")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"/unban leaked into SendInput/SteerInput: {client.calls}")

composer.setProperty("text", "/background")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if "nothing running to background" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/background idle did not append feedback: {transcript.rows[-1:]}")

transcript.setStreaming(True)
pump(app, 18)
composer.setProperty("text", "/bg")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if route_events[-1:] != ["home"]:
    raise AssertionError(f"/bg while streaming did not route home: {route_events}")
if "moved to background" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/bg while streaming did not append feedback: {transcript.rows[-1:]}")
transcript.setStreaming(False)
pump(app, 18)

composer.setProperty("text", "/rail")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if len(rail_events) != 1:
    raise AssertionError(f"/rail did not emit railToggleRequested: {rail_events}")
if "toggled navigation rail" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/rail did not append feedback: {transcript.rows[-1:]}")

term_start_count = call_count(client, "TerminalStart")
composer.setProperty("text", "/term")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if chat.property("dockOpen") is not True:
    raise AssertionError("/term did not open the worktree dock")
terminal_tab = find_item(chat, "dockTab_Terminal")
if terminal_tab is None or terminal_tab.property("selected") is not True:
    raise AssertionError("/term did not select the Terminal dock tab")
terminal_root = find_item(chat, "terminalTab")
terminal_output = find_item(chat, "terminalOutputArea")
terminal_command = find_item(chat, "terminalCommandField")
terminal_send = find_item(chat, "terminalSendButton")
terminal_start = find_item(chat, "terminalStartButton")
terminal_stop = find_item(chat, "terminalStopButton")
terminal_clear = find_item(chat, "terminalClearButton")
if None in (terminal_root, terminal_output, terminal_command, terminal_send, terminal_start, terminal_stop, terminal_clear):
    raise AssertionError("Terminal dock did not render its controls")
if call_count(client, "TerminalStart") <= term_start_count:
    raise AssertionError(f"/term did not start a terminal: {client.calls}")
if ("subscribe", ("eigen:terminal",)) not in client.calls:
    raise AssertionError(f"Terminal dock did not subscribe to terminal events: {client.calls}")
encoded_output = base64.b64encode(b"\x1b[32mready\n\x1b[0m").decode("ascii")
client.event.emit("eigen:terminal", {"id": "term-chat", "data": encoded_output})
pump(app, 18)
if "ready\n" not in terminal_output.property("text"):
    raise AssertionError(f"Terminal output did not decode event bytes: {terminal_output.property('text')!r}")
terminal_command.setProperty("text", "pwd")
pump(app, 8)
click_item(app, chat_view, chat, "terminalSendButton")
if ("TerminalWrite", ("term-chat", "pwd\r")) not in client.calls:
    raise AssertionError(f"Terminal command did not write to PTY: {client.calls}")
if terminal_command.property("text") != "":
    raise AssertionError("Terminal command field did not clear after send")
composer.forceActiveFocus()
pump(app, 8)
click_item(app, chat_view, chat, "terminalOutputArea")
if terminal_output.property("activeFocus") is not True:
    raise AssertionError("Terminal output did not take focus after click")
direct_key_start = len(client.calls)
QTest.keyClick(chat_view, Qt.Key_W)
QTest.keyClick(chat_view, Qt.Key_H)
QTest.keyClick(chat_view, Qt.Key_O)
QTest.keyClick(chat_view, Qt.Key_Return)
QTest.keyClick(chat_view, Qt.Key_Backspace)
QTest.keyClick(chat_view, Qt.Key_Tab)
QTest.keyClick(chat_view, Qt.Key_Up)
QTest.keyClick(chat_view, Qt.Key_Down)
QTest.keyClick(chat_view, Qt.Key_Right)
QTest.keyClick(chat_view, Qt.Key_Left)
QTest.keyClick(chat_view, Qt.Key_C, Qt.ControlModifier)
QTest.keyClick(chat_view, Qt.Key_D, Qt.ControlModifier)
pump(app, 18)
direct_writes = [call for call in client.calls[direct_key_start:] if call[0] == "TerminalWrite"]
for expected in (
    ("TerminalWrite", ("term-chat", "w")),
    ("TerminalWrite", ("term-chat", "h")),
    ("TerminalWrite", ("term-chat", "o")),
    ("TerminalWrite", ("term-chat", "\r")),
    ("TerminalWrite", ("term-chat", "\x7f")),
    ("TerminalWrite", ("term-chat", "\t")),
    ("TerminalWrite", ("term-chat", "\x1b[A")),
    ("TerminalWrite", ("term-chat", "\x1b[B")),
    ("TerminalWrite", ("term-chat", "\x1b[C")),
    ("TerminalWrite", ("term-chat", "\x1b[D")),
    ("TerminalWrite", ("term-chat", "\x03")),
    ("TerminalWrite", ("term-chat", "\x04")),
):
    if expected not in direct_writes:
        raise AssertionError(f"Terminal output did not send direct key {expected!r}: {direct_writes!r}")
for button, name in (
    (terminal_send, "terminalSendButton"),
    (terminal_start, "terminalStartButton"),
    (terminal_stop, "terminalStopButton"),
    (terminal_clear, "terminalClearButton"),
):
    if not button.property("qaTextFits"):
        raise AssertionError(f"{name} text does not fit")
terminal_start_after_send = call_count(client, "TerminalStart")
click_item(app, chat_view, chat, "dockTab_Diff")
pump(app, 8)
click_item(app, chat_view, chat, "dockTab_Terminal")
pump(app, 18)
terminal_output = find_item(chat, "terminalOutputArea")
if call_count(client, "TerminalStart") != terminal_start_after_send:
    raise AssertionError(f"Switching back to Terminal restarted the PTY: {client.calls}")
if terminal_output is None or "ready\n" not in terminal_output.property("text"):
    raise AssertionError("Terminal output was not preserved across dock tab switches")
if "opened Terminal dock" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/term did not append expected feedback: {transcript.rows[-1:]}")
click_item(app, chat_view, chat, "dockCloseButton")
pump(app, 18)
if ("TerminalKill", ("term-chat",)) not in client.calls:
    raise AssertionError(f"Closing the dock did not kill the terminal: {client.calls}")

pending_client = FakeRpcClient()
pending_client.defer_methods.add("TerminalStart")
pending_state = FakeSessionState()
pending_commands = CommandModel()
pending_transcript = StaticTranscript([])
pending_approvals = ApprovalModel()
pending_view, pending_chat = make_view(
    "ChatView.qml",
    {},
    {
        "sessionId": "s-pending-terminal",
        "sessionStateModel": pending_state,
        "commandsModel": pending_commands,
        "transcriptModel": pending_transcript,
        "approvalsModel": pending_approvals,
        "rpcClient": pending_client,
        "clipboardHelper": clipboard,
        "highlighter": highlighter,
        "terminalHelper": terminal_helper,
    },
)
pending_composer = find_item(pending_chat, "chatComposerTextArea")
if pending_composer is None:
    raise AssertionError("Pending terminal composer did not render")
pending_composer.setProperty("text", "/term")
pump(app, 8)
click_item(app, pending_view, pending_chat, "chatSendButton")
pump(app, 18)
if call_count(pending_client, "TerminalStart") != 1:
    raise AssertionError(f"Pending terminal did not request TerminalStart: {pending_client.calls}")
if pending_client.deferred == []:
    raise AssertionError("Pending terminal fake did not defer TerminalStart")
click_item(app, pending_view, pending_chat, "dockCloseButton")
pump(app, 18)
pending_client.flush_deferred()
pump(app, 18)
if ("TerminalKill", ("term-chat",)) not in pending_client.calls:
    raise AssertionError(f"Late TerminalStart result was not killed after dock close: {pending_client.calls}")
pending_view.hide()
pending_view.setSource(QUrl())
chat_view.show()
chat_view.requestActivate()
composer.forceActiveFocus()
pump(app, 8)

composer.setProperty("text", "/shells")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if chat.property("dockOpen") is not True:
    raise AssertionError("/shells did not open the worktree dock")
info_tab = find_item(chat, "dockTab_Info")
if info_tab is None or info_tab.property("selected") is not True:
    raise AssertionError("/shells did not select the Info dock tab")
if "background shells are shown in the Info dock" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/shells did not append expected feedback: {transcript.rows[-1:]}")

legacy_notes = [
    ("/loop", "TUI-local"),
    ("/read", "read-aloud"),
    ("/mouse", "terminal-only"),
    ("/rebuild", "terminal"),
    ("/quit", "Close this window"),
]
for command_text, expected_note in legacy_notes:
    composer.setProperty("text", command_text)
    pump(app, 8)
    click_item(app, chat_view, chat, "chatSendButton")
    if expected_note not in transcript.rows[-1]["text"]:
        raise AssertionError(f"{command_text} did not append expected feedback: {transcript.rows[-1:]}")

if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"Legacy local commands leaked into SendInput/SteerInput: {client.calls}")

composer.setProperty("text", "/find")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if "usage: /find <text>" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"Bare /find did not append usage feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/find Ready")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if "find: Ready" not in transcript.rows[-1]["text"] or "match" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/find hit did not append match feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/find definitely absent text")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if "no matches for definitely absent text" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/find miss did not append miss feedback: {transcript.rows[-1:]}")

if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"/find leaked into SendInput/SteerInput: {client.calls}")

composer.setProperty("text", "/voice doctor")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("VoiceStatus", ()) not in client.calls:
    raise AssertionError(f"/voice doctor did not call VoiceStatus: {client.calls}")
if "voice: STT available, TTS missing" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/voice doctor did not append voice status: {transcript.rows[-1:]}")

composer.setProperty("text", "/voice")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("VoiceModeStart", ("s-chat",)) not in client.calls:
    raise AssertionError(f"/voice did not call VoiceModeStart: {client.calls}")
if "voice mode on" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/voice did not append mode feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/mute")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("VoiceModeStop", ()) not in client.calls:
    raise AssertionError(f"/mute did not call VoiceModeStop: {client.calls}")
if "voice mode off" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/mute did not append feedback: {transcript.rows[-1:]}")

voice_send_start = call_count(client, "SendInput") + call_count(client, "SteerInput")
composer.setProperty("text", "/dictate")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("VoiceListen", ()) not in client.calls:
    raise AssertionError(f"/dictate did not call VoiceListen: {client.calls}")
if ("SendInput", ("s-chat", "Dictated follow up", [], [])) not in client.calls:
    raise AssertionError(f"/dictate did not submit the heard transcript: {client.calls}")
if transcript.rows[-1]["kind"] != "user" or transcript.rows[-1]["text"] != "Dictated follow up":
    raise AssertionError(f"/dictate did not append the dictated user turn: {transcript.rows[-1:]}")
send_start += 1
if call_count(client, "SendInput") + call_count(client, "SteerInput") != voice_send_start + 1:
    raise AssertionError(f"/dictate made the wrong number of sends: {client.calls}")

composer.setProperty("text", "/speak")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if not any(call[0] == "VoiceSpeak" and call[1] == ("Ready for the next instruction.",) for call in client.calls):
    raise AssertionError(f"/speak did not call VoiceSpeak with the last assistant answer: {client.calls}")
if "speaking last assistant answer" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/speak did not append feedback: {transcript.rows[-1:]}")

if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"Voice commands leaked unexpected SendInput/SteerInput calls: {client.calls}")

composer.setProperty("text", "/plugins")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("Plugins", ()) not in client.calls:
    raise AssertionError(f"/plugins did not call Plugins: {client.calls}")
plugin_note = transcript.rows[-1]["text"]
if "plugins: 2 installed, 1 marketplaces, 2 hooks" not in plugin_note or "agent-tools" not in plugin_note:
    raise AssertionError(f"/plugins did not append plugin summary: {transcript.rows[-1:]}")

composer.setProperty("text", "/hooks")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if call_count(client, "Plugins") < 2:
    raise AssertionError(f"/hooks did not reuse Plugins summary: {client.calls}")
if "plugins: 2 installed" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/hooks did not append plugin summary: {transcript.rows[-1:]}")

composer.setProperty("text", "/observe")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("ObserveSummary", (5000,)) not in client.calls:
    raise AssertionError(f"/observe did not call ObserveSummary: {client.calls}")
observe_note = transcript.rows[-1]["text"]
if "observe: 42 records" not in observe_note or "1 tools" not in observe_note or "1 error groups" not in observe_note:
    raise AssertionError(f"/observe did not append telemetry summary: {transcript.rows[-1:]}")

if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"Plugin/observe commands leaked unexpected SendInput/SteerInput calls: {client.calls}")

input_mode = find_item(chat, "chatInputModeButton")
if input_mode is None:
    raise AssertionError("Input mode button did not render")
if input_mode.property("qaText") != "Steer" or input_mode.property("selected") is not False:
    raise AssertionError(f"Input mode did not start in steer mode: {input_mode.property('qaText')!r}")
if not input_mode.property("qaTextFits"):
    raise AssertionError("Input mode button text does not fit")

transcript.setStreaming(True)
pump(app, 18)
send = find_item(chat, "chatSendButton")
if send.property("qaText") != "Steer":
    raise AssertionError(f"Streaming steer mode did not label send button as Steer: {send.property('qaText')!r}")

composer.setProperty("text", "/queue")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if chat.property("qaInputMode") != "queue":
    raise AssertionError(f"/queue did not switch input mode: {chat.property('qaInputMode')!r}")
if "input mode -> queue" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/queue did not append queue feedback: {transcript.rows[-1:]}")
input_mode = find_item(chat, "chatInputModeButton")
if input_mode.property("qaText") != "Queue" or input_mode.property("selected") is not True:
    raise AssertionError(f"Input mode button did not show queue mode: {input_mode.property('qaText')!r}")
send = find_item(chat, "chatSendButton")
if send.property("qaText") != "Queue":
    raise AssertionError(f"Streaming queue mode did not label send button as Queue: {send.property('qaText')!r}")

composer.setProperty("text", "hold this until the turn finishes")
pump(app, 8)
queued_send_start = call_count(client, "SendInput") + call_count(client, "SteerInput")
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if call_count(client, "SendInput") + call_count(client, "SteerInput") != queued_send_start:
    raise AssertionError(f"Queue mode sent immediately instead of holding: {client.calls}")
if chat.property("qaQueuedInputCount") != 1:
    raise AssertionError(f"Queue mode did not retain the message: {chat.property('qaQueuedInputCount')}")
if "queued -> will send when the turn finishes (1)" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"Queue mode did not append queued feedback: {transcript.rows[-1:]}")
input_mode = find_item(chat, "chatInputModeButton")
if input_mode.property("badgeText") != "1":
    raise AssertionError(f"Input mode button did not show queued badge: {input_mode.property('badgeText')!r}")
if composer.property("text") != "":
    raise AssertionError("Queue mode did not clear the composer after accepting the hold")

transcript.setStreaming(False)
pump(app, 24)
if ("SendInput", ("s-chat", "hold this until the turn finishes", [], [])) not in client.calls:
    raise AssertionError(f"Queued message did not drain as a fresh SendInput: {client.calls}")
if chat.property("qaQueuedInputCount") != 0:
    raise AssertionError(f"Queued message did not clear after drain: {chat.property('qaQueuedInputCount')}")
send_start += 1

transcript.setStreaming(True)
pump(app, 18)
composer.setProperty("text", "/steer")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if chat.property("qaInputMode") != "steer":
    raise AssertionError(f"/steer did not switch input mode: {chat.property('qaInputMode')!r}")
if "input mode -> steer" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/steer did not append steer feedback: {transcript.rows[-1:]}")
composer.setProperty("text", "inject this into the running turn")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("SteerInput", ("s-chat", "inject this into the running turn", [])) not in client.calls:
    raise AssertionError(f"Steer mode did not call SteerInput while streaming: {client.calls}")
send_start += 1
transcript.setStreaming(False)
pump(app, 18)

composer.setProperty("text", "/tools")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if "tools:" not in transcript.rows[-1]["text"] or "read_file" not in transcript.rows[-1]["text"] or "run_shell" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/tools did not append the session tool list: {transcript.rows[-1:]}")

composer.setProperty("text", "/skills reviewer")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("SkillBody", ("reviewer",)) not in client.calls:
    raise AssertionError(f"/skills name did not call SkillBody: {client.calls}")
if "skill: reviewer" not in transcript.rows[-1]["text"] or "Use the review tool" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/skills name did not append a skill preview: {transcript.rows[-1:]}")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"/skills name leaked into SendInput/SteerInput: {client.calls}")

composer.setProperty("text", "/copy")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if not clipboard.copied or clipboard.copied[-1] != "Ready for the next instruction.":
    raise AssertionError(f"/copy did not copy the last assistant answer: {clipboard.copied}")
if "copied 31 chars" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/copy did not append copy feedback: {transcript.rows[-1:]}")

review_prompt = (
    "Use the review tool to get a cross-vendor critique of the current Qt diff. "
    "Package the relevant artifact (the plan, diff, or code) into the tool's `artifact` "
    "argument with enough context to judge it, set an appropriate `focus`, then act on "
    "the critique: fix real issues it raises and note anything you disagree with and why."
)
composer.setProperty("text", "/review the current Qt diff")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("SendInput", ("s-chat", review_prompt, [], [])) not in client.calls:
    raise AssertionError(f"/review did not send the synthetic review turn: {client.calls}")
if transcript.rows[-1]["kind"] != "user" or transcript.rows[-1]["text"] != review_prompt:
    raise AssertionError(f"/review did not append the synthetic user turn: {transcript.rows[-1:]}")
if composer.property("text") != "":
    raise AssertionError("/review did not clear the composer after handling")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start + 1:
    raise AssertionError(f"/review made the wrong number of chat sends: {client.calls}")
send_start += 1

custom_prompt = "Ship the custom command target: current diff"
composer.setProperty("text", "/ship-it current diff")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("RunCommand", ("s-chat", "ship-it", "current diff")) not in client.calls:
    raise AssertionError(f"Custom slash command did not call RunCommand: {client.calls}")
if transcript.rows[-1]["kind"] != "user" or transcript.rows[-1]["text"] != custom_prompt:
    raise AssertionError(f"Custom slash command did not append the expanded prompt: {transcript.rows[-1:]}")
if composer.property("text") != "":
    raise AssertionError("Custom slash command did not clear the composer after handling")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"Custom slash command leaked into SendInput/SteerInput: {client.calls}")

normal_send_start = call_count(client, "SendInput") + call_count(client, "SteerInput")
composer.setProperty("text", "Plain Enter follow up")
composer.forceActiveFocus()
pump(app, 8)
QTest.keyClick(chat_view, Qt.Key_Return)
pump(app, 18)
if ("SendInput", ("s-chat", "Plain Enter follow up", [], [])) not in client.calls:
    raise AssertionError(f"Plain Return did not send composer text: {client.calls}")
if composer.property("text") != "":
    raise AssertionError("Plain Return did not clear the composer after sending")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != normal_send_start + 1:
    raise AssertionError(f"Plain Return made the wrong number of sends: {client.calls}")
send_start += 1

composer.setProperty("text", "/rename")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if "usage: /rename <title>" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"Bare /rename did not append usage feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/rename Night run")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("SetTitle", ("s-chat", "Night run")) not in client.calls:
    raise AssertionError(f"/rename did not call SetTitle: {client.calls}")
if state.property("title") != "Night run":
    raise AssertionError(f"/rename did not reseed the session title: {state.property('title')!r}")
if "renamed -> Night run" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/rename did not append a local note: {transcript.rows[-1:]}")

composer.setProperty("text", "/compact")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("Compact", ("s-chat", 0)) not in client.calls:
    raise AssertionError(f"/compact did not call Compact: {client.calls}")
if "compacted 42 -> 7" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/compact did not append compaction feedback: {transcript.rows[-1:]}")

export_start = call_count(client, "ExportSession")
composer.setProperty("text", "/save")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if call_count(client, "ExportSession") <= export_start:
    raise AssertionError(f"/save did not call ExportSession: {client.calls}")
if "exported -> /home/user/eigen-exports/s-chat.jsonl" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/save did not append export feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/export")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if call_count(client, "ExportSession") <= export_start + 1:
    raise AssertionError(f"/export did not call ExportSession: {client.calls}")
if "exported -> /home/user/eigen-exports/s-chat.jsonl" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/export did not append export feedback: {transcript.rows[-1:]}")

composer.setProperty("text", "/clear")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
pump(app, 18)
if ("Clear", ("s-chat",)) not in client.calls:
    raise AssertionError(f"/clear did not call Clear: {client.calls}")
if len(transcript.rows) != 1 or transcript.rows[-1]["kind"] != "note" or "-- cleared --" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"/clear did not reset the transcript and append feedback: {transcript.rows}")

composer.setProperty("text", "/sessions")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if route_events[-1:] != ["sessions"]:
    raise AssertionError(f"/sessions did not emit routeRequested: {route_events}")

composer.setProperty("text", "/wat")
pump(app, 8)
click_item(app, chat_view, chat, "chatSendButton")
if "unknown command /wat" not in transcript.rows[-1]["text"]:
    raise AssertionError(f"Unknown slash command did not append feedback: {transcript.rows[-1:]}")
if call_count(client, "SendInput") + call_count(client, "SteerInput") != send_start:
    raise AssertionError(f"Slash commands leaked into SendInput/SteerInput: {client.calls}")

composer.setProperty("text", "ship the next Qt slice")
pump(app, 8)
send = find_item_in(chat_view, chat, "chatSendButton")
if send is None:
    raise AssertionError("Send button did not render")
if send.property("qaText") != "Send":
    raise AssertionError(f"Non-streaming transcript showed {send.property('qaText')} instead of Send")
interrupt = find_item(chat, "chatInterruptButton")
if interrupt is None or interrupt.property("visible") is not False:
    raise AssertionError("Non-streaming transcript showed the interrupt control")
assert_item_inside_window(send, "chatSendButton")
send = click_item(app, chat_view, chat, "chatSendButton")
if ("SendInput", ("s-chat", "ship the next Qt slice", [], [])) not in client.calls:
    raise AssertionError(f"Send button did not call SendInput: {client.calls}")
if composer.property("text") != "":
    raise AssertionError("Composer did not clear after send")
if not send.property("qaTextFits"):
    raise AssertionError("Send button text does not fit")

client.failures["SendInput"] = "daemon offline"
composer.setProperty("text", "keep this when daemon fails")
pump(app, 8)
failed_start = call_count(client, "SendInput")
click_item(app, chat_view, chat, "chatSendButton")
QTest.qWait(40)
pump(app, 20)
if call_count(client, "SendInput") != failed_start + 1:
    raise AssertionError(f"Failed send did not call SendInput: {client.calls}")
if "daemon offline" not in chat.property("actionError"):
    raise AssertionError(f"Failed send did not surface daemon error: {chat.property('actionError')!r}")
error_banner = find_item(chat, "chatActionError")
error_text = find_item(chat, "chatActionErrorText")
if error_banner is None or error_banner.property("visible") is not True:
    raise AssertionError("Failed send did not show a visible chat action error")
if error_text is None or "daemon offline" not in error_text.property("text"):
    raise AssertionError(f"Failed send error text was wrong: {error_text.property('text') if error_text else None}")
if composer.property("text") != "keep this when daemon fails":
    raise AssertionError("Failed send did not restore the composer draft")
click_item(app, chat_view, chat, "chatDismissActionError")
if chat.property("actionError") != "":
    raise AssertionError("Failed send action error did not dismiss")
del client.failures["SendInput"]

chat.setProperty("attachedImage", VALID_PNG_BASE64)
QTest.qWait(40)
pump(app, 20)
preview = find_item(chat, "chatAttachmentPreviewImage")
if preview is None:
    raise AssertionError("Attachment preview image did not render")
for _ in range(20):
    if preview.property("qaImageReady") is True:
        break
    QTest.qWait(10)
    pump(app, 1)
source = preview.property("source")
source_text = source.toString() if hasattr(source, "toString") else str(source)
if "data:image/png;base64," not in source_text:
    raise AssertionError(f"Attachment preview did not use an image data URL: {source_text!r}")
if preview.property("qaImageError") is True or preview.property("qaImageReady") is not True:
    raise AssertionError("Attachment preview image did not load")
if preview.property("qaSourceSizeWidth") != 96 or preview.property("qaSourceSizeHeight") != 96:
    raise AssertionError(
        f"Attachment preview sourceSize was not bounded: "
        f"{preview.property('qaSourceSizeWidth')}x{preview.property('qaSourceSizeHeight')}"
    )
painted_width = float(preview.property("qaPaintedWidth") or 0)
painted_height = float(preview.property("qaPaintedHeight") or 0)
if not (0 < painted_width <= 42 and 0 < painted_height <= 42):
    raise AssertionError(f"Attachment preview painted outside its thumbnail: {painted_width}x{painted_height}")
clear = find_item(chat, "chatClearAttachmentButton")
if clear is None:
    raise AssertionError("Attachment clear button did not render")
if not clear.property("qaTextFits"):
    raise AssertionError("Attachment clear button text does not fit")
click_item(app, chat_view, chat, "chatClearAttachmentButton")
if chat.property("attachedImage") != "":
    raise AssertionError("Attachment clear button did not clear the image")

approval_args = (
    '{"command":"pytest -q gui-qt/tests/test_chat_controls.py",'
    '"cwd":"/home/user/eigen",'
    '"reason":"approval args should be readable, selectable, and bounded inside the sheet"}'
)
approval_view, approval = make_view(
    "ApprovalOverlay.qml",
    {},
    {
        "model": ApprovalModel(
            [
                {"id": "approval-1", "tool": "shell", "args": approval_args},
                {
                    "id": "approval-2",
                    "tool": "edit",
                    "args": "write file",
                    "approving": True,
                    "error": "daemon offline",
                },
            ]
        )
    },
)
approvals = []
approval.approve.connect(lambda approval_id, allow: approvals.append((approval_id, allow)))
overlay = find_item(approval, "approvalOverlay")
if overlay is None:
    raise AssertionError("Approval overlay did not expose a root objectName")
assert_item_inside_window(overlay, "approvalOverlay")
args_box = find_item(approval, "approvalArgs_approval_1")
args_text = find_item(approval, "approvalArgsText_approval_1")
if args_box is None or args_text is None:
    raise AssertionError("Approval args well did not render")
if '"command": "pytest -q gui-qt/tests/test_chat_controls.py"' not in args_text.property("text"):
    raise AssertionError(f"Approval args were not pretty-printed: {args_text.property('text')!r}")
assert_item_inside_window(args_box, "approvalArgs_approval_1")
toggle = click_item(app, approval_view, approval, "approvalArgsToggle_approval_1")
if toggle.property("qaText") != "Show less":
    raise AssertionError("Approval args toggle did not expand")
assert_item_inside_window(args_box, "approvalArgs_approval_1 expanded")
error = find_item(approval, "approvalError_approval_2")
if error is None or error.property("visible") is not True:
    raise AssertionError("Approval row error did not render")
busy_allow = find_item(approval, "approvalAllow_approval_2")
busy_deny = find_item(approval, "approvalDeny_approval_2")
if busy_allow is None or busy_deny is None or busy_allow.property("enabled") is not False or busy_deny.property("enabled") is not False:
    raise AssertionError("Approving approval row did not disable both actions")
allow = click_item(app, approval_view, approval, "approvalAllow_approval_1")
deny = click_item(app, approval_view, approval, "approvalDeny_approval_1")
if approvals != [("approval-1", True), ("approval-1", False)]:
    raise AssertionError(f"Approval overlay emitted wrong results: {approvals}")
if not allow.property("qaTextFits") or not deny.property("qaTextFits"):
    raise AssertionError("Approval button text does not fit")

markdown_view, markdown = make_view(
    "MarkdownBlocks.qml",
    {"clipboardHelper": clipboard, "highlighter": highlighter},
    {"blocks": [{"type": "code", "lang": "sh", "source": "echo qt"}]},
)
copy = click_item(app, markdown_view, markdown, "markdownCopyCodeButton")
if not clipboard.copied or clipboard.copied[-1] != "echo qt":
    raise AssertionError(f"Code copy did not reach clipboard helper: {clipboard.copied}")
if not copy.property("qaTextFits"):
    raise AssertionError("Markdown copy button text does not fit")
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
        timeout=15,
    )

    assert result.returncode == 0, result.stdout + result.stderr
