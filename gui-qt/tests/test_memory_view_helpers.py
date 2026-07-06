import os
import subprocess
import sys
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_memory_view_falls_back_when_markdown_helper_is_missing():
    script = r"""
from pathlib import Path

from PySide6.QtCore import QObject, QSize, QTimer, QUrl, QtMsgType, Signal, qInstallMessageHandler
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlContext
from PySide6.QtQuick import QQuickView
from PySide6.QtQuickControls2 import QQuickStyle

from eigenqt.models.memory import MemoryModel


ROOT = Path.cwd()
SIZE = QSize(900, 700)
ISSUE_MARKERS = ("ReferenceError", "TypeError", "Cannot read property", "Unable to assign")


class FakeSignal:
    def connect(self, _):
        pass


class FakeRpcClient(QObject):
    connected = Signal()

    def __init__(self):
        super().__init__()
        self.calls = []

    def call(self, method, *args, callback=None, error_callback=None):
        self.calls.append((method, args))
        if callback:
            QTimer.singleShot(0, lambda: callback({"result": self._result(method, args)}))

    def _result(self, method, args):
        if method == "MemoryForScope":
            return seeded_memory()
        return {}


def seeded_memory():
    return {
        "scope": "global",
        "summary": "# Memory summary\n\nQt should render without the optional helper.",
        "hasSummary": True,
        "notes": [{"index": 0, "text": "Keep helper fallbacks local."}],
        "adHoc": [],
        "noteCount": 1,
        "profile": "Focused profile",
        "profileLearned": "",
        "banList": [],
        "backups": 0,
        "bytes": 128,
    }


def pump(app, rounds=16):
    for _ in range(rounds):
        app.processEvents()


def qml_issues(messages):
    return [
        record for record in messages
        if record["type"] in (QtMsgType.QtCriticalMsg, QtMsgType.QtFatalMsg)
        or (
            record["type"] == QtMsgType.QtWarningMsg
            and (record["file"].endswith(".qml") or any(marker in record["message"] for marker in ISSUE_MARKERS))
        )
    ]


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


previous = qInstallMessageHandler(capture_qt_message)
view = None
try:
    client = FakeRpcClient()
    model = MemoryModel(client)
    model.scopes = [{"key": "global", "name": "Global", "dir": "", "noteCount": 1}]
    model.scope_key = "global"
    model.current = seeded_memory()
    model.loading = False

    view = QQuickView()
    view.setResizeMode(QQuickView.SizeRootObjectToView)
    view.setWidth(SIZE.width())
    view.setHeight(SIZE.height())
    view.engine().addImportPath(str(ROOT / "eigenqt"))
    ctx: QQmlContext = view.rootContext()
    ctx.setContextProperty("memoryModel", model)
    view.setInitialProperties({"memoryModel": model})
    view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / "MemoryView.qml")))
    if view.status() == QQuickView.Error or view.rootObject() is None:
        raise AssertionError([error.toString() for error in view.errors()])
    view.show()
    pump(app, 40)
    issues = qml_issues(messages)
    if issues:
        raise AssertionError(f"QML issues: {issues[:8]}")
finally:
    if view is not None:
        view.hide()
        view.setSource(QUrl())
        pump(app, 8)
    qInstallMessageHandler(previous)
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
        timeout=12,
    )

    assert result.returncode == 0, result.stdout + result.stderr
