import os
import subprocess
import sys
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_home_and_board_actions_start_and_route_sessions():
    script = r"""
from pathlib import Path

from PySide6.QtCore import QObject, QPoint, QPointF, QSize, QTimer, QUrl, Qt, Signal, Slot
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlContext
from PySide6.QtQuick import QQuickView
from PySide6.QtQuickControls2 import QQuickStyle
from PySide6.QtTest import QTest

from eigenqt.models.board import BoardModel, KanbanModel
from eigenqt.models.home import FeedModel


ROOT = Path.cwd()
SIZE = QSize(1180, 820)


class FakeRpcClient(QObject):
    connected = Signal()
    callDone = Signal(int, "QVariantMap")
    event = Signal(str, dict)
    dropped = Signal(str)

    def __init__(self):
        super().__init__()
        self.calls = []
        self._token = 0
        self.failures = {}
        self.delays = {"NewSession": 80}
        self.board_payload = {
            "lanes": [
                {
                    "name": "eigen",
                    "dir": "/repo/eigen",
                    "branch": "fix/qt",
                    "dirty": 2,
                    "unpushed": 0,
                    "behind": 0,
                    "todos": 1,
                    "openPrs": 1,
                    "openIss": 0,
                    "items": [
                        {
                            "key": "git-eigen",
                            "kind": "git",
                            "title": "2 uncommitted files",
                            "detail": "Review the local diff",
                            "dir": "/repo/eigen",
                            "task": "Review the working tree and commit coherent chunks.",
                        },
                        {
                            "key": "pr-eigen-71",
                            "kind": "github",
                            "title": "Qt replacement follow-up",
                            "detail": "PR #71",
                            "url": "https://github.com/avifenesh/eigen/pull/71",
                        },
                    ],
                }
            ]
        }
        self.kanban_payload = {
            "columns": [
                {
                    "id": "needs-you",
                    "title": "Needs you",
                    "cards": [
                        {
                            "key": "issue-eigen-5",
                            "repo": "avifenesh/eigen",
                            "title": "Fix desktop routing",
                            "kind": "issue",
                            "url": "https://github.com/avifenesh/eigen/issues/5",
                            "needsYou": True,
                        }
                    ],
                },
                {"id": "todo", "title": "Todo", "cards": []},
            ]
        }

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
        QTimer.singleShot(
            int(self.delays.get(method, 0)),
            lambda: self.callDone.emit(token, payload),
        )
        return token

    @Slot(str, "QVariantList")
    def callFire(self, method, args):
        self.calls.append((method, tuple(args or [])))

    def subscribe(self, channels):
        pass

    def unsubscribe(self, channels):
        pass

    def shutdown(self):
        pass

    def _result(self, method, args):
        if method == "Board":
            return self.board_payload
        if method == "Kanban":
            return self.kanban_payload
        if method == "StartFromFeed":
            return "s-feed"
        if method == "NewSession":
            return "s-new"
        if method == "ReviewPR":
            return "s-pr"
        if method == "WorkIssue":
            return "s-issue"
        return {}


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


def item_center(item):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    if width <= 0 or height <= 0:
        raise AssertionError(f"{item.objectName()} has invalid size {width}x{height}")
    point = item.mapToScene(QPointF(width / 2, height / 2))
    return QPoint(
        max(0, min(SIZE.width() - 1, int(point.x()))),
        max(0, min(SIZE.height() - 1, int(point.y()))),
    )


def click_item(app, window, root, object_name, *, pump_after=True):
    pump(app, 8)
    item = find_item(root, object_name)
    if item is None:
        raise AssertionError(f"missing item {object_name}")
    QTest.mouseClick(window, Qt.LeftButton, Qt.NoModifier, item_center(item))
    if pump_after:
        QTest.qWait(20)
        pump(app, 18)
    return item


def make_view(source, client, initial):
    view = QQuickView()
    view.setResizeMode(QQuickView.SizeRootObjectToView)
    view.setWidth(SIZE.width())
    view.setHeight(SIZE.height())
    view.engine().addImportPath(str(ROOT / "eigenqt"))
    ctx: QQmlContext = view.rootContext()
    ctx.setContextProperty("rpcClient", client)
    for name, value in initial.items():
        ctx.setContextProperty(name, value)
    view.setInitialProperties(initial)
    view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / source)))
    if view.status() == QQuickView.Error or view.rootObject() is None:
        raise AssertionError([error.toString() for error in view.errors()])
    view.show()
    pump(QGuiApplication.instance(), 30)
    return view, view.rootObject()


QQuickStyle.setStyle("Basic")
app = QGuiApplication([])
client = FakeRpcClient()

feed = FeedModel(client)
feed._on_feed_result(
    {
        "result": {
            "items": [
                {
                    "key": "home-git",
                    "kind": "git",
                    "title": "Dirty Eigen checkout",
                    "detail": "Review and commit the focused diff",
                    "dir": "/repo/eigen",
                    "dirName": "eigen",
                    "task": "Review and commit the focused diff.",
                }
            ],
            "fresh": True,
        }
    }
)
home_view, home = make_view(
    "HomeView.qml",
    client,
    {"rpcClient": client, "feedModel": feed, "statsData": {}, "sessionsModel": None},
)
home_started = []
home.sessionStarted.connect(lambda session_id: home_started.append(session_id))

start = len(client.calls)
feed_start = find_item(home, "homeFeedStart_home_git")
if feed_start is None or feed_start.property("text") != "Start":
    raise AssertionError(f"home feed start button label was {feed_start.property('text') if feed_start else None}")
if feed_start.property("qaTextFits") is not True:
    raise AssertionError("home feed start button text did not fit")
click_item(app, home_view, home, "homeFeedStart_home_git")
if ("StartFromFeed", ("/repo/eigen", "Review and commit the focused diff.")) not in client.calls[start:]:
    raise AssertionError(f"home feed did not call StartFromFeed: {client.calls[start:]}")
if home_started[-1:] != ["s-feed"]:
    raise AssertionError(f"home feed did not route started session: {home_started}")

start = len(client.calls)
button = click_item(app, home_view, home, "homeStartSessionButton")
if button.property("enabled") is not False:
    raise AssertionError("home new-session button did not disable while pending")
if button.property("qaTextFits") is not True:
    raise AssertionError("home new-session pending text did not fit")
QTest.qWait(120)
pump(app, 20)
if ("NewSession", ("", "", "")) not in client.calls[start:]:
    raise AssertionError(f"home start did not call NewSession: {client.calls[start:]}")
if home_started[-1:] != ["s-new"]:
    raise AssertionError(f"home new session did not route: {home_started}")

client.failures["NewSession"] = "daemon offline"
start = len(client.calls)
button = click_item(app, home_view, home, "homeStartSessionButton")
if button.property("enabled") is not False:
    raise AssertionError("home failed new-session button did not enter pending state")
QTest.qWait(120)
pump(app, 20)
if ("NewSession", ("", "", "")) not in client.calls[start:]:
    raise AssertionError(f"home failed start did not call NewSession: {client.calls[start:]}")
button = find_item(home, "homeStartSessionButton")
if button is None or button.property("enabled") is not True:
    raise AssertionError("home failed new-session did not re-enable the button")
if "daemon offline" not in home.property("actionError"):
    raise AssertionError(f"home failed new-session did not expose action error: {home.property('actionError')}")
error_banner = find_item(home, "homeActionErrorBanner")
error_text = find_item(home, "homeActionErrorText")
if error_banner is None or error_banner.property("visible") is not True:
    raise AssertionError("home failed new-session did not render the action error banner")
if error_text is None or "daemon offline" not in error_text.property("text"):
    raise AssertionError(f"home action error text was wrong: {error_text.property('text') if error_text else None}")
dismiss = click_item(app, home_view, home, "homeActionErrorDismissButton")
if dismiss.property("qaTextFits") is not True:
    raise AssertionError("home action error dismiss text did not fit")
if home.property("actionError") != "":
    raise AssertionError("home action error dismiss did not clear the error")
del client.failures["NewSession"]
home_view.close()

board = BoardModel(client)
kanban = KanbanModel(client)
board_view, board_root = make_view(
    "BoardView.qml",
    client,
    {
        "rpcClient": client,
        "boardModel": board,
        "kanbanModel": kanban,
        "sessionsModel": None,
    },
)
board_started = []
board_root.sessionStarted.connect(lambda session_id: board_started.append(session_id))

board_root.setProperty("stateFilter", "issues")
pump(app, 20)
filtered_state = find_item(board_root, "boardProjectsState_filtered")
if filtered_state is None or filtered_state.property("visible") is not True:
    raise AssertionError("board did not show the filtered empty state")
click_item(app, board_view, board_root, "boardProjectsStateAction")
if board_root.property("stateFilter") != "all" or board_root.property("ownerFilter") != "all":
    raise AssertionError("filtered board reset did not restore all filters")
if find_item(board_root, "boardLaneName__repo_eigen") is None:
    raise AssertionError("board lane did not return after resetting filters")

client.failures["PinLane"] = "daemon offline"
start = len(client.calls)
click_item(app, board_view, board_root, "boardPinButton__repo_eigen")
if ("PinLane", ("/repo/eigen",)) not in client.calls[start:]:
    raise AssertionError(f"board pin did not call PinLane: {client.calls[start:]}")
if board.isPinning("/repo/eigen"):
    raise AssertionError("failed board pin left the lane in pinning state")
if "daemon offline" not in board.property("actionError"):
    raise AssertionError(f"failed board pin did not expose model action error: {board.property('actionError')}")
error_banner = find_item(board_root, "boardActionErrorBanner")
error_text = find_item(board_root, "boardActionErrorText")
if error_banner is None or error_banner.property("visible") is not True:
    raise AssertionError("failed board pin did not render the action error banner")
if error_text is None or "daemon offline" not in error_text.property("text"):
    raise AssertionError(f"board action error text was wrong: {error_text.property('text') if error_text else None}")
dismiss = click_item(app, board_view, board_root, "boardActionErrorDismissButton")
if dismiss.property("qaTextFits") is not True:
    raise AssertionError("board action error dismiss text did not fit")
if board.property("actionError") != "" or board_root.property("visibleActionError") != "":
    raise AssertionError("board action error dismiss did not clear the error")
del client.failures["PinLane"]

start = len(client.calls)
click_item(app, board_view, board_root, "boardLaneName__repo_eigen", pump_after=False)
click_item(app, board_view, board_root, "boardLaneName__repo_eigen", pump_after=False)
QTest.qWait(120)
pump(app, 20)
new_session_calls = [call for call in client.calls[start:] if call == ("NewSession", ("/repo/eigen", "", ""))]
if len(new_session_calls) != 1:
    raise AssertionError(f"lane double click minted duplicate sessions: {client.calls[start:]}")
if board_started[-1:] != ["s-new"]:
    raise AssertionError(f"lane session did not route: {board_started}")

start = len(client.calls)
click_item(app, board_view, board_root, "boardItemAction_git_eigen")
if ("StartFromFeed", ("/repo/eigen", "Review the working tree and commit coherent chunks.")) not in client.calls[start:]:
    raise AssertionError(f"git board card did not call StartFromFeed: {client.calls[start:]}")
if board_started[-1:] != ["s-feed"]:
    raise AssertionError(f"git card did not route: {board_started}")

start = len(client.calls)
click_item(app, board_view, board_root, "boardItemAction_pr_eigen_71")
if ("ReviewPR", ("https://github.com/avifenesh/eigen/pull/71",)) not in client.calls[start:]:
    raise AssertionError(f"PR board card did not call ReviewPR: {client.calls[start:]}")
if board_started[-1:] != ["s-pr"]:
    raise AssertionError(f"PR card did not route: {board_started}")

board_root.setProperty("viewMode", "kanban")
pump(app, 20)
start = len(client.calls)
click_item(app, board_view, board_root, "kanbanCardAction_issue_eigen_5")
if ("WorkIssue", ("https://github.com/avifenesh/eigen/issues/5",)) not in client.calls[start:]:
    raise AssertionError(f"issue kanban card did not call WorkIssue: {client.calls[start:]}")
if board_started[-1:] != ["s-issue"]:
    raise AssertionError(f"issue card did not route: {board_started}")

board_view.close()

client.board_payload = {"lanes": []}
client.kanban_payload = {"columns": []}
empty_board = BoardModel(client)
empty_kanban = KanbanModel(client)
empty_view, empty_root = make_view(
    "BoardView.qml",
    client,
    {
        "rpcClient": client,
        "boardModel": empty_board,
        "kanbanModel": empty_kanban,
        "sessionsModel": None,
    },
)
empty_state = find_item(empty_root, "boardProjectsState_empty")
if empty_state is None or empty_state.property("visible") is not True:
    raise AssertionError("empty projects board did not render an empty state")
empty_root.setProperty("viewMode", "kanban")
pump(app, 20)
empty_kanban_state = find_item(empty_root, "boardKanbanState_empty")
if empty_kanban_state is None or empty_kanban_state.property("visible") is not True:
    raise AssertionError("empty kanban board did not render an empty state")
empty_view.close()
client.shutdown()
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
