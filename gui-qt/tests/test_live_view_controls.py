import os
import subprocess
import sys
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_live_view_actions_use_shared_buttons_and_call_rpc():
    script = r"""
from pathlib import Path

from PySide6.QtCore import QObject, QPoint, QPointF, QSize, QTimer, QUrl, Qt, Signal, Slot
from PySide6.QtGui import QGuiApplication
from PySide6.QtQuick import QQuickView
from PySide6.QtQuickControls2 import QQuickStyle
from PySide6.QtTest import QTest

from eigenqt.models import LiveSessionsModel, SessionsModel


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
        self.failures = {}
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
        if method in self.failures:
            QTimer.singleShot(0, lambda: self.callDone.emit(token, {"error": self.failures[method]}))
        else:
            QTimer.singleShot(0, lambda: self.callDone.emit(token, {"result": self._result(method, call_args)}))
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
        if method == "Sessions":
            return seeded_sessions()
        if method == "State":
            return {
                "id": args[0] if args else "s-approval",
                "pending": [{"id": "approval-1", "tool": "shell", "args": "make test"}],
            }
        return {}


def seeded_sessions():
    return [
        {
            "id": "s-idle",
            "title": "Idle scratch",
            "dir": "/repo/eigen",
            "model": "gpt-5",
            "status": "idle",
            "turns": 1,
            "updated": 1783150000000,
        },
        {
            "id": "s-work",
            "title": "Qt live work",
            "dir": "/repo/eigen/gui-qt",
            "model": "local-qwen",
            "status": "working",
            "turns": 4,
            "updated": 1783155600000,
        },
        {
            "id": "s-approval",
            "title": "Approval check",
            "dir": "/repo/eigen",
            "model": "grok-4",
            "status": "approval",
            "turns": 2,
            "updated": 1783152000000,
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
            f"({bottom_right.x():.1f}, {bottom_right.y():.1f})"
        )


def item_center(item):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    point = item.mapToScene(QPointF(width / 2, height / 2))
    return QPoint(max(0, min(SIZE.width() - 1, int(point.x()))), max(0, min(SIZE.height() - 1, int(point.y()))))


def click_item(app, window, root, object_name):
    pump(app, 8)
    item = find_item(root, object_name)
    if item is None:
        raise AssertionError(f"missing item {object_name}")
    assert_item_inside_window(item, object_name)
    if not item.property("qaTextFits"):
        raise AssertionError(f"{object_name} text does not fit")
    QTest.mouseClick(window, Qt.LeftButton, Qt.NoModifier, item_center(item))
    QTest.qWait(20)
    pump(app, 18)
    return item


QQuickStyle.setStyle("Basic")
app = QGuiApplication([])
client = FakeRpcClient()
sessions = SessionsModel(client)
sessions._on_sessions_result({"result": seeded_sessions()})
live = LiveSessionsModel(client)
live._on_sessions_result({"result": seeded_sessions()})

view = QQuickView()
view.setResizeMode(QQuickView.SizeRootObjectToView)
view.setWidth(SIZE.width())
view.setHeight(SIZE.height())
view.engine().addImportPath(str(ROOT / "eigenqt"))
view.setInitialProperties({
    "rpcClient": client,
    "sessionsModel": sessions,
    "liveSessionsModel": live,
})
view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / "LiveView.qml")))
if view.status() == QQuickView.Error or view.rootObject() is None:
    raise AssertionError([error.toString() for error in view.errors()])
view.show()
pump(app, 30)
root = view.rootObject()

new_sessions = []
opened = []
root.newSessionRequested.connect(lambda: new_sessions.append(True))
root.openSession.connect(lambda session_id: opened.append(session_id))

updated_label = find_item(root, "liveUpdatedLabel_s_work")
if updated_label is None:
    raise AssertionError("Live updated timestamp label did not render")
updated_text = updated_label.property("qaText") or ""
if "20638" in updated_text or (updated_text != "just now" and "ago" not in updated_text):
    raise AssertionError(f"Live updated timestamp was not normalized: {updated_text!r}")

click_item(app, view, root, "liveNewSessionButton")
if new_sessions != [True]:
    raise AssertionError(f"New session button did not emit: {new_sessions}")

click_item(app, view, root, "liveOpenButton_s_work")
if opened[-1:] != ["s-work"]:
    raise AssertionError(f"Open button did not emit s-work: {opened}")

start = len(client.calls)
click_item(app, view, root, "liveInterruptButton_s_work")
if ("Interrupt", ("s-work",)) not in client.calls[start:]:
    raise AssertionError(f"Interrupt button did not call RPC: {client.calls[start:]}")
error_box = find_item(root, "liveActionError")
if error_box is not None and error_box.property("visible"):
    raise AssertionError("Successful interrupt showed an action error")

client.failures["Interrupt"] = "daemon offline"
start = len(client.calls)
click_item(app, view, root, "liveInterruptButton_s_work")
if ("Interrupt", ("s-work",)) not in client.calls[start:]:
    raise AssertionError(f"Failed interrupt did not call RPC: {client.calls[start:]}")
error_box = find_item(root, "liveActionError")
error_text = find_item(root, "liveActionErrorText")
if error_box is None or error_box.property("visible") is not True:
    raise AssertionError("Failed interrupt did not show the live action error row")
if error_text is None or "daemon offline" not in error_text.property("text"):
    raise AssertionError(f"Live action error did not include daemon failure: {error_text.property('text') if error_text else None}")
clear_error = click_item(app, view, root, "liveActionErrorClearButton")
if root.property("actionError") != "":
    raise AssertionError("Live action error clear button did not clear the message")
if not clear_error.property("qaTextFits"):
    raise AssertionError("Live action error clear text does not fit")
del client.failures["Interrupt"]

click_item(app, view, root, "liveRemoveButton_s_work")
confirm = find_item(root, "liveConfirmRemoveButton_s_work")
cancel = find_item(root, "liveCancelRemoveButton_s_work")
if confirm is None or cancel is None:
    raise AssertionError("Remove did not reveal confirm/cancel buttons")
if not confirm.property("qaTextFits") or not cancel.property("qaTextFits"):
    raise AssertionError("Confirm/cancel text does not fit")
click_item(app, view, root, "liveCancelRemoveButton_s_work")
confirm = find_item(root, "liveConfirmRemoveButton_s_work")
if confirm is not None and confirm.property("visible"):
    raise AssertionError("Cancel did not close the remove confirmation")

click_item(app, view, root, "liveRemoveButton_s_work")
start = len(client.calls)
click_item(app, view, root, "liveConfirmRemoveButton_s_work")
if ("RemoveSession", ("s-work",)) not in client.calls[start:]:
    raise AssertionError(f"Confirm remove did not call RPC: {client.calls[start:]}")

start = len(client.calls)
click_item(app, view, root, "liveApproveButton_s_approval")
if ("State", ("s-approval",)) not in client.calls[start:]:
    raise AssertionError(f"Approve button did not fetch State: {client.calls[start:]}")
allow = find_item(root, "liveAllowApprovalButton_s_approval_approval_1")
deny = find_item(root, "liveDenyApprovalButton_s_approval_approval_1")
if allow is None or deny is None:
    raise AssertionError("Approval gate did not render Allow/Deny buttons")
if not allow.property("qaTextFits") or not deny.property("qaTextFits"):
    raise AssertionError("Approval gate button text does not fit")
start = len(client.calls)
click_item(app, view, root, "liveAllowApprovalButton_s_approval_approval_1")
if ("Approve", ("s-approval", "approval-1", True)) not in client.calls[start:]:
    raise AssertionError(f"Allow approval did not call RPC: {client.calls[start:]}")

root.setProperty("rpcClient", None)
pump(app, 12)

click_item(app, view, root, "liveInterruptButton_s_work")
error_text = find_item(root, "liveActionErrorText")
if error_text is None or "RPC client is unavailable" not in error_text.property("text"):
    raise AssertionError(f"Missing offline interrupt error: {error_text.property('text') if error_text else None}")
click_item(app, view, root, "liveActionErrorClearButton")

click_item(app, view, root, "liveRemoveButton_s_work")
click_item(app, view, root, "liveConfirmRemoveButton_s_work")
error_text = find_item(root, "liveActionErrorText")
if error_text is None or "Remove failed: RPC client is unavailable" not in error_text.property("text"):
    raise AssertionError(f"Missing offline remove error: {error_text.property('text') if error_text else None}")
click_item(app, view, root, "liveActionErrorClearButton")

click_item(app, view, root, "liveApproveButton_s_approval")
gate_error = find_item(root, "liveApprovalGateError_s_approval")
if gate_error is None or gate_error.property("visible") is not True:
    raise AssertionError("Missing approval gate offline error")
if "RPC client is unavailable" not in gate_error.property("qaText"):
    raise AssertionError(f"Unexpected approval gate offline error: {gate_error.property('qaText')!r}")
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
