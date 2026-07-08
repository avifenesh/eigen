import os
import subprocess
import sys
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_tasks_view_filters_and_actions_are_keyboard_operable():
    script = r"""
from pathlib import Path

from PySide6.QtCore import Q_ARG, QMetaObject, QObject, QPoint, QPointF, QSize, QTimer, QUrl, Qt, Signal, Slot
from PySide6.QtGui import QGuiApplication
from PySide6.QtQuick import QQuickView
from PySide6.QtQuickControls2 import QQuickStyle
from PySide6.QtTest import QTest

from eigenqt.models.tasks import TasksModel


ROOT = Path.cwd()
SIZE = QSize(1200, 820)


class FakeRpcClient(QObject):
    connected = Signal()
    callDone = Signal(int, "QVariantMap")
    event = Signal(str, dict)
    dropped = Signal(str)

    def __init__(self):
        super().__init__()
        self.calls = []
        self.failures = {}
        self.delays = {}
        self.canceling = set()
        self.transcript_text = (
            '{"Role":"assistant","Text":"Task filter proof ready",'
            '"ToolCalls":[{"name":"apply_patch","args":"{}"}]}\n'
        )
        self._token = 0

    def call(self, method, *args, callback=None, error_callback=None):
        self.calls.append((method, args))
        if callback:
            payload = (
                {"error": self.failures[method]}
                if method in self.failures
                else {"result": self._result(method, args)}
            )
            QTimer.singleShot(self.delays.get(method, 0), lambda: callback(payload))

    @Slot(str, "QVariantList", result=int)
    def callToken(self, method, args):
        self._token += 1
        token = self._token
        call_args = tuple(args or [])
        self.calls.append((method, call_args))
        delay = self.delays.get(method, 0)
        if method in self.failures:
            QTimer.singleShot(delay, lambda: self.callDone.emit(token, {"error": self.failures[method]}))
        else:
            QTimer.singleShot(delay, lambda: self.callDone.emit(token, {"result": self._result(method, call_args)}))
        return token

    def subscribe(self, channels):
        pass

    def unsubscribe(self, channels):
        pass

    def shutdown(self):
        pass

    def _result(self, method, args):
        now = 1_800_000_000_000
        if method == "Agents":
            return {
                "tasks": [
                    {
                        "id": "task-run",
                        "status": "running",
                        "task": "Replace task filter buttons",
                        "model": "gpt-5",
                        "role": "frontend",
                        "kind": "fix",
                        "difficulty": "medium",
                        "where": "/repo/eigen",
                        "startedMs": now - 120_000,
                        "lastTool": "apply_patch",
                        "steps": 4,
                        "lastNote": "Wiring keyboard chips",
                        "inTokens": 1200,
                        "outTokens": 320,
                        "canceling": "task-run" in self.canceling,
                    },
                    {
                        "id": "task-done",
                        "status": "done",
                        "task": "Review task transcript layout",
                        "model": "local-qwen",
                        "role": "qa",
                        "kind": "review",
                        "difficulty": "easy",
                        "where": "/repo/eigen",
                        "startedMs": now - 300_000,
                        "finishedMs": now - 60_000,
                        "lastTool": "pytest",
                        "steps": 3,
                        "result": "done",
                    },
                    {
                        "id": "task-lost",
                        "status": "lost",
                        "task": "Recover stale worker",
                        "model": "grok-4",
                        "role": "ops",
                        "kind": "repair",
                        "difficulty": "hard",
                        "where": "/repo/eigen",
                        "startedMs": now - 900_000,
                        "error": "worker disappeared",
                    },
                ]
            }
        if method == "AgentTranscript":
            return {"transcript": self.transcript_text}
        if method == "CancelAgent":
            if args:
                self.canceling.add(str(args[0]))
            return {}
        return {}


def pump(app, count=14):
    for _ in range(count):
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
    point = item.mapToScene(QPointF(width / 2, height / 2))
    return QPoint(max(0, min(SIZE.width() - 1, int(point.x()))), max(0, min(SIZE.height() - 1, int(point.y()))))


def key_item(app, window, root, object_name, key):
    pump(app, 8)
    item = find_item(root, object_name)
    if item is None:
        raise AssertionError(f"missing item {object_name}")
    item.forceActiveFocus(Qt.TabFocusReason)
    pump(app, 8)
    QTest.keyClick(window, key)
    QTest.qWait(20)
    pump(app, 18)
    return item


def assert_task_chip_padding(chip, name):
    if chip.property("qaIsTaskChip") is not True:
        raise AssertionError(f"{name} did not expose task chip QA marker")
    if chip.property("qaTextFits") is not True:
        raise AssertionError(f"{name} text did not fit")
    if float(chip.property("qaHorizontalPadding") or 0) < 43.5:
        raise AssertionError(f"{name} horizontal padding too small: {chip.property('qaHorizontalPadding')}")
    if float(chip.property("qaVerticalPadding") or 0) < 11.5:
        raise AssertionError(f"{name} vertical padding too small: {chip.property('qaVerticalPadding')}")


QQuickStyle.setStyle("Basic")
app = QGuiApplication([])
client = FakeRpcClient()
tasks = TasksModel(client)

view = QQuickView()
view.setResizeMode(QQuickView.SizeRootObjectToView)
view.setWidth(SIZE.width())
view.setHeight(SIZE.height())
view.engine().addImportPath(str(ROOT / "eigenqt"))
view.setInitialProperties({"tasksModel": tasks, "rpcClient": client})
view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / "TasksView.qml")))
if view.status() == QQuickView.Error or view.rootObject() is None:
    raise AssertionError([error.toString() for error in view.errors()])
root = view.rootObject()
view.show()
client.connected.emit()
QTest.qWait(30)
pump(app, 30)

if tasks.rowCount() != 3:
    raise AssertionError(f"tasks did not load: {tasks.rowCount()}")

for chip_name in ("all", "running", "done", "error"):
    chip = find_item(root, f"taskFilterChip_{chip_name}")
    if chip is None:
        raise AssertionError(f"missing task filter chip {chip_name}")
    assert_task_chip_padding(chip, chip_name)

running_chip = key_item(app, view, root, "taskFilterChip_running", Qt.Key_Space)
if root.property("currentFilter") != "running" or tasks.property("filter") != "running":
    raise AssertionError(
        f"running filter did not apply: root={root.property('currentFilter')} model={tasks.property('filter')}"
    )
if tasks.rowCount() != 1:
    raise AssertionError(f"running filter row count was {tasks.rowCount()}")
if not running_chip.property("qaVisualFocus"):
    raise AssertionError("running chip did not expose keyboard focus")
if running_chip.property("qaAccessibleName") != "Running task filter":
    raise AssertionError(f"running chip accessible name was {running_chip.property('qaAccessibleName')}")
assert_task_chip_padding(running_chip, "running")

error_chip = key_item(app, view, root, "taskFilterChip_error", Qt.Key_Return)
if root.property("currentFilter") != "error" or tasks.rowCount() != 1:
    raise AssertionError(f"error filter did not isolate lost task: {root.property('currentFilter')} rows={tasks.rowCount()}")
if not error_chip.property("qaVisualFocus"):
    raise AssertionError("error chip did not expose keyboard focus")
assert_task_chip_padding(error_chip, "error")

key_item(app, view, root, "taskFilterChip_all", Qt.Key_Return)
if root.property("currentFilter") != "all" or tasks.rowCount() != 3:
    raise AssertionError(f"all filter did not restore tasks: {root.property('currentFilter')} rows={tasks.rowCount()}")

start = len(client.calls)
transcript_button = key_item(app, view, root, "taskTranscriptButton_task_run", Qt.Key_Return)
if not transcript_button.property("qaVisualFocus"):
    raise AssertionError("transcript button did not expose keyboard focus")
if ("AgentTranscript", ("task-run",)) not in client.calls[start:]:
    raise AssertionError(f"transcript button did not call AgentTranscript: {client.calls[start:]}")
if root.property("transcriptLoading"):
    raise AssertionError("transcript did not finish loading")
if "Task filter proof ready" not in root.property("transcriptText"):
    raise AssertionError(f"transcript text was not populated: {root.property('transcriptText')}")
if root.property("transcriptError") != "":
    raise AssertionError(f"successful transcript showed an error: {root.property('transcriptError')}")

key_item(app, view, root, "taskTranscriptCloseButton", Qt.Key_Space)
if root.property("transcriptText") != "":
    raise AssertionError("transcript close did not clear transcript text")
if root.property("transcriptError") != "":
    raise AssertionError("transcript close did not clear transcript error")
if root.property("qaTranscriptEntryCount") != 0:
    raise AssertionError("transcript close did not clear parsed entries")

client.delays["AgentTranscript"] = 140
client.transcript_text = '{"Role":"assistant","Text":"late transcript should stay closed"}\n'
key_item(app, view, root, "taskTranscriptButton_task_run", Qt.Key_Return)
if root.property("transcriptLoading") is not True:
    raise AssertionError("delayed transcript did not enter loading state")
key_item(app, view, root, "taskTranscriptCloseButton", Qt.Key_Space)
QTest.qWait(180)
pump(app, 30)
if root.property("transcriptText") != "":
    raise AssertionError(f"late transcript reply repopulated closed sheet: {root.property('transcriptText')}")
if root.property("qaTranscriptEntryCount") != 0:
    raise AssertionError("late transcript reply repopulated parsed entries")
if root.property("qaTranscriptPendingCount") != 0:
    raise AssertionError("closing transcript did not drop pending tokens")
del client.delays["AgentTranscript"]

client.transcript_text = ""
key_item(app, view, root, "taskTranscriptButton_task_run", Qt.Key_Return)
if root.property("transcriptLoading"):
    raise AssertionError("empty transcript stayed loading")
if root.property("transcriptLoaded") is not True:
    raise AssertionError("empty transcript did not mark load complete")
empty = find_item(root, "taskTranscriptEmpty")
if empty is None or empty.property("visible") is not True:
    raise AssertionError("empty transcript did not render the empty-state message")
key_item(app, view, root, "taskTranscriptCloseButton", Qt.Key_Space)

long_transcript = "\n".join(
    '{"Role":"assistant","Text":"entry %03d"}' % i for i in range(205)
)
root.setProperty("transcriptLoading", True)
QMetaObject.invokeMethod(root, "parseTranscript", Qt.DirectConnection, Q_ARG("QVariant", long_transcript))
if root.property("qaTranscriptEntryCount") != 200:
    raise AssertionError(f"long transcript did not cap to 200 entries: {root.property('qaTranscriptEntryCount')}")
if root.property("transcriptElidedCount") != 5:
    raise AssertionError(f"long transcript elided count was {root.property('transcriptElidedCount')}")
client.transcript_text = (
    '{"Role":"assistant","Text":"Task filter proof ready",'
    '"ToolCalls":[{"name":"apply_patch","args":"{}"}]}\n'
)

client.failures["AgentTranscript"] = "daemon offline"
start = len(client.calls)
transcript_button = key_item(app, view, root, "taskTranscriptButton_task_run", Qt.Key_Return)
if ("AgentTranscript", ("task-run",)) not in client.calls[start:]:
    raise AssertionError(f"failed transcript button did not call AgentTranscript: {client.calls[start:]}")
if root.property("transcriptLoading"):
    raise AssertionError("failed transcript stayed loading")
if "daemon offline" not in root.property("transcriptError"):
    raise AssertionError(f"failed transcript did not expose error: {root.property('transcriptError')}")
error_box = find_item(root, "taskTranscriptError")
error_text = find_item(root, "taskTranscriptErrorText")
retry_button = find_item(root, "taskTranscriptErrorRetryButton")
if error_box is None or error_box.property("visible") is not True:
    raise AssertionError("failed transcript did not render the error box")
if error_text is None or "daemon offline" not in error_text.property("text"):
    raise AssertionError(f"failed transcript error text was wrong: {error_text.property('text') if error_text else None}")
if retry_button is None or retry_button.property("qaTextFits") is not True:
    raise AssertionError("failed transcript did not render a clean retry button")
del client.failures["AgentTranscript"]
client.transcript_text = '{"Role":"assistant","Text":"retried transcript loaded"}\n'
start = len(client.calls)
key_item(app, view, root, "taskTranscriptErrorRetryButton", Qt.Key_Return)
if ("AgentTranscript", ("task-run",)) not in client.calls[start:]:
    raise AssertionError(f"transcript retry did not call AgentTranscript: {client.calls[start:]}")
if root.property("transcriptError") != "":
    raise AssertionError(f"successful transcript retry left an error: {root.property('transcriptError')}")
if "retried transcript loaded" not in root.property("transcriptText"):
    raise AssertionError(f"transcript retry did not populate text: {root.property('transcriptText')}")
key_item(app, view, root, "taskTranscriptCloseButton", Qt.Key_Space)

client.failures["CancelAgent"] = "daemon offline"
start = len(client.calls)
cancel_button = key_item(app, view, root, "taskCancelButton_task_run", Qt.Key_Space)
if ("CancelAgent", ("task-run",)) not in client.calls[start:]:
    raise AssertionError(f"cancel button did not call CancelAgent: {client.calls[start:]}")
if cancel_button.property("enabled") is not True:
    raise AssertionError("failed cancel did not re-enable the cancel button")
if cancel_button.property("qaTextFits") is not True:
    raise AssertionError("cancel button text did not fit after failed cancel")
if "daemon offline" not in tasks.property("actionError"):
    raise AssertionError(f"failed cancel did not expose an action error: {tasks.property('actionError')}")
error_banner = find_item(root, "taskActionErrorBanner")
error_text = find_item(root, "taskActionErrorText")
if error_banner is None or error_banner.property("visible") is not True:
    raise AssertionError("failed cancel did not render the action error banner")
if error_text is None or "daemon offline" not in error_text.property("text"):
    raise AssertionError(f"failed cancel error text was wrong: {error_text.property('text') if error_text else None}")
key_item(app, view, root, "taskActionErrorDismissButton", Qt.Key_Return)
if tasks.property("actionError") != "":
    raise AssertionError("task action error dismiss did not clear the model error")
del client.failures["CancelAgent"]

start = len(client.calls)
key_item(app, view, root, "taskCancelButton_task_run", Qt.Key_Space)
if ("CancelAgent", ("task-run",)) not in client.calls[start:]:
    raise AssertionError(f"cancel button did not call CancelAgent: {client.calls[start:]}")
cancel_button = find_item(root, "taskCancelButton_task_run")
if cancel_button is None:
    raise AssertionError("cancel button disappeared after successful cancel refresh")
if cancel_button.property("enabled") is not False:
    raise AssertionError("cancel button did not disable while canceling")
if cancel_button.property("text") != "Stopping…":
    raise AssertionError(f"cancel button pending label was {cancel_button.property('text')!r}")
if cancel_button.property("qaTextFits") is not True:
    raise AssertionError("cancel button pending label did not fit")
start = len(client.calls)
key_item(app, view, root, "taskCancelButton_task_run", Qt.Key_Space)
if any(call == ("CancelAgent", ("task-run",)) for call in client.calls[start:]):
    raise AssertionError(f"disabled cancel button issued a duplicate call: {client.calls[start:]}")
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
