import os
import subprocess
import sys
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_session_settings_strip_wraps_every_control_on_narrow_windows():
    script = r"""
import os
from pathlib import Path

from PySide6.QtCore import QObject, QPointF, QUrl
from PySide6.QtGui import QGuiApplication
from PySide6.QtQuick import QQuickView
from PySide6.QtQuickControls2 import QQuickStyle
from PySide6.QtTest import QTest

from eigenqt.models.session_state import SessionStateModel


ROOT = Path.cwd()
HEIGHT = 120


def pump(app, rounds=20):
    for _ in range(rounds):
        app.processEvents()


def effectively_visible(item):
    current = item
    while current is not None:
        if current.property("visible") is False:
            return False
        opacity = current.property("opacity")
        if opacity is not None and float(opacity) <= 0.01:
            return False
        current = current.parentItem()
    return True


def find_visible_item(root, object_name):
    matches = []

    def collect(item):
        if item.objectName() == object_name and effectively_visible(item):
            matches.append(item)
        for child in item.childItems():
            collect(child)

    collect(root)
    return max(matches, key=lambda item: float(item.property("width") or 0) * float(item.property("height") or 0), default=None)


def count_items(root, object_name):
    count = 1 if root.objectName() == object_name else 0
    for child in root.childItems():
        count += count_items(child, object_name)
    return count


def assert_inside_window(item, window_width, window_height):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    if width <= 0 or height <= 0:
        raise AssertionError(f"{item.objectName()} has invalid size {width}x{height}")
    top_left = item.mapToScene(QPointF(0, 0))
    bottom_right = item.mapToScene(QPointF(width, height))
    if (
        top_left.x() < -0.5
        or top_left.y() < -0.5
        or bottom_right.x() > window_width + 0.5
        or bottom_right.y() > window_height + 0.5
    ):
        raise AssertionError(
            f"{item.objectName()} escaped the strip: "
            f"({top_left.x():.1f}, {top_left.y():.1f}) -> "
            f"({bottom_right.x():.1f}, {bottom_right.y():.1f})"
        )


QQuickStyle.setStyle("Basic")
app = QGuiApplication([])
state = SessionStateModel(QObject(), "s-layout")
state.seed(
    {
        "model": "local-qwen",
        "perm": "auto",
        "effort": "medium",
        "search": "off",
        "fast": True,
        "fastOk": True,
        "title": "Narrow controls",
        "catalog": {
            "models": [
                {"id": "openai.gpt-5.5", "effortLevels": ["low", "medium", "high"]},
                {"id": "gpt-5.6-sol", "effortLevels": ["low", "high"]},
                {"id": "local-qwen", "effortLevels": ["low", "high"]},
            ]
        },
    }
)

view = QQuickView()
view.setResizeMode(QQuickView.SizeRootObjectToView)
view.setInitialProperties({"sessionState": state})
view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / "SessionSettingsStrip.qml")))
if view.status() == QQuickView.Error or view.rootObject() is None:
    raise AssertionError([error.toString() for error in view.errors()])
view.setWidth(1180)
view.setHeight(HEIGHT)
view.show()
root = view.rootObject()
names = (
    "sessionModelCombo",
    "sessionPermCombo",
    "sessionEffortCombo",
    "sessionSearchCombo",
    "sessionFastSwitch",
)

for window_width in (1180, 420):
    view.setWidth(window_width)
    view.setHeight(HEIGHT)
    QTest.qWait(30)
    pump(app, 32)
    controls = []
    for name in names:
        control = find_visible_item(root, name)
        if control is None:
            raise AssertionError(f"missing {name} at {window_width}px")
        assert_inside_window(control, window_width, HEIGHT)
        if control.property("qaTextFits") is not True:
            raise AssertionError(f"{name} text is clipped at {window_width}px")
        if count_items(root, name) != 1:
            raise AssertionError(f"inactive responsive layout retained duplicate {name} at {window_width}px")
        controls.append(control)
    fast_label = find_visible_item(root, "sessionFastLabel")
    if fast_label is None or fast_label.property("text") != "Fast":
        raise AssertionError(f"Fast switch lost its visible label at {window_width}px")
    assert_inside_window(fast_label, window_width, HEIGHT)
    if count_items(root, "sessionFastLabel") != 1:
        raise AssertionError(f"inactive responsive layout retained duplicate Fast label at {window_width}px")
    model_combo = find_visible_item(root, "sessionModelCombo")
    if model_combo.property("count") != 2:
        raise AssertionError(f"model picker exposed non-GPT options at {window_width}px: {model_combo.property('count')}")
    if model_combo.property("qaText") != "local-qwen":
        raise AssertionError(f"model picker hid the active legacy model label at {window_width}px: {model_combo.property('qaText')}")
    if window_width == 420:
        rows = {round(control.mapToScene(QPointF(0, 0)).y()) for control in controls}
        if len(rows) < 2:
            raise AssertionError("compact session controls did not wrap into multiple rows")
        model_combo.setProperty("qaPopupOpen", True)
        pump(app, 20)
        if model_combo.property("qaPopupActuallyOpen") is not True:
            raise AssertionError("focused model picker did not open")
        if model_combo.property("qaPopupInsideWindow") is not True:
            raise AssertionError("focused model picker escaped the narrow window")
        screenshots = os.environ.get("EIGEN_QT_SCREENSHOT_DIR", "")
        if screenshots:
            output = Path(screenshots)
            output.mkdir(parents=True, exist_ok=True)
            view.grabWindow().save(str(output / "session-model-picker-narrow.png"))
        model_combo.setProperty("qaPopupOpen", False)

view.close()
"""
    env = os.environ.copy()
    env.setdefault("QT_QPA_PLATFORM", "offscreen")
    env["PYTHONPATH"] = str(ROOT) + os.pathsep + env.get("PYTHONPATH", "")
    result = subprocess.run(
        [sys.executable, "-c", textwrap.dedent(script)],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, result.stdout + result.stderr
