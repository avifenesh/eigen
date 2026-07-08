import os
import re
import subprocess
import sys
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_qml_theme_tokens_are_not_read_as_color_components():
    pattern = re.compile(r"Theme\.colors\.[A-Za-z0-9_]+\.(?:r|g|b|a)\b")
    offenders = []
    for path in (ROOT / "eigenqt" / "qml").glob("*.qml"):
        for line_no, line in enumerate(path.read_text().splitlines(), start=1):
            if pattern.search(line):
                offenders.append(f"{path.relative_to(ROOT)}:{line_no}: {line.strip()}")

    assert not offenders, (
        "Theme.js color tokens are strings; use explicit rgba tokens instead:\n"
        + "\n".join(offenders)
    )


def test_infinite_qml_animations_are_motion_gated():
    theme = (ROOT / "eigenqt" / "qml" / "Theme.js").read_text()
    assert re.search(r"\bvar\s+continuousMotion\s*=\s*false\b", theme)

    offenders = []
    for path in (ROOT / "eigenqt" / "qml").glob("*.qml"):
        lines = path.read_text().splitlines()
        for index, line in enumerate(lines):
            if "loops: Animation.Infinite" not in line:
                continue
            context = lines[max(0, index - 8) : min(len(lines), index + 9)]
            if not any("running:" in candidate and "Theme.continuousMotion" in candidate for candidate in context):
                offenders.append(f"{path.relative_to(ROOT)}:{index + 1}: {line.strip()}")

    assert not offenders, (
        "Infinite QML animations must be gated by Theme.continuousMotion:\n"
        + "\n".join(offenders)
    )


def test_app_button_and_combo_keyboard_contracts():
    script = r"""
from pathlib import Path

from PySide6.QtCore import QPoint, QPointF, Qt, QtMsgType, QUrl, qInstallMessageHandler
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlComponent, QQmlEngine
from PySide6.QtQuickControls2 import QQuickStyle
from PySide6.QtTest import QTest


ROOT = Path.cwd()
ISSUE_MARKERS = (
    "ReferenceError",
    "TypeError",
    "Unable to assign",
    "Cannot assign",
    "Cannot read property",
)


def find_item(item, object_name):
    if item is None:
        return None
    if item.objectName() == object_name:
        return item
    if hasattr(item, "childItems"):
        for child in item.childItems():
            found = find_item(child, object_name)
            if found is not None:
                return found
    return None


def pump(app, rounds=8):
    for _ in range(rounds):
        app.processEvents()


def item_center(item):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    if width <= 0 or height <= 0:
        raise AssertionError(f"{item.objectName()} has invalid size {width}x{height}")
    point = item.mapToScene(QPointF(width / 2, height / 2))
    return QPoint(int(point.x()), int(point.y()))


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
    engine = QQmlEngine()
    engine.addImportPath(str(ROOT / "eigenqt" / "qml"))
    component = QQmlComponent(engine)
    component.setData(
        b'''
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

    Window {
        id: root
        width: 420
        height: 260
        visible: true
        property int buttonClicks: 0
        property int customButtonClicks: 0
        property int disabledButtonClicks: 0
        property int switchClicks: 0
        property bool lastSwitchChecked: false
        property int lastComboActivation: -1
        property string missingOptionText: objectCombo.optionText({})
        property string nullOptionText: objectCombo.optionText(null)
        property string numberOptionText: objectCombo.optionText(42)

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 16
        spacing: 12

        AppButton {
            objectName: "primaryAction"
            text: "Run focused QA"
            toolTipText: "Run focused QA"
            variant: "primary"
            onClicked: root.buttonClicks += 1
        }

        AppButton {
            objectName: "customContentAction"
            text: "Open details"
            onClicked: root.customButtonClicks += 1

            contentItem: Label {
                text: "Open details"
                elide: Text.ElideRight
            }
        }

        AppButton {
            objectName: "disabledPrimaryAction"
            text: "Send"
            variant: "primary"
            enabled: false
            onClicked: root.disabledButtonClicks += 1
        }

        AppTag {
            objectName: "sampleTag"
            text: "forced:1"
            backgroundColor: "#1a2428"
            borderColor: "#2b3b40"
            textColor: "#dde4e3"
        }

        AppComboBox {
            objectName: "modelCombo"
            Layout.preferredWidth: 220
            model: ["gpt-5", "local-qwen", "grok-4"]
            onActivated: function(index) {
                root.lastComboActivation = index
            }
        }

            AppComboBox {
                id: objectCombo
                objectName: "objectCombo"
                Layout.preferredWidth: 220
                textRole: "label"
                model: [{"label": "Alpha"}, {}, null, {"label": "Omega"}]
            }

            AppSwitch {
                objectName: "routeSwitch"
                accessibleName: "route setting"
                toolTipText: "Toggle route setting"
                checked: false
                onClicked: {
                    root.switchClicks += 1
                    root.lastSwitchChecked = checked
                }
            }
        }

        AppComboBox {
            id: bottomCombo
            objectName: "bottomCombo"
            x: 184
            y: root.height - height - 12
            width: 220
            height: 32
            popupMaxHeight: 180
            model: ["first", "second", "third", "fourth", "fifth", "sixth"]
        }
    }
''',
        QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / "AppControlsHarness.qml")),
    )
    root = component.create()
    if root is None:
        raise AssertionError([error.toString() for error in component.errors()])
    pump(app)
    assert_no_qml_issues(messages)

    root_item = root.contentItem()
    button = find_item(root_item, "primaryAction")
    custom_button = find_item(root_item, "customContentAction")
    disabled_button = find_item(root_item, "disabledPrimaryAction")
    sample_tag = find_item(root_item, "sampleTag")
    combo = find_item(root_item, "modelCombo")
    object_combo = find_item(root_item, "objectCombo")
    bottom_combo = find_item(root_item, "bottomCombo")
    route_switch = find_item(root_item, "routeSwitch")
    if button is None or custom_button is None or disabled_button is None or sample_tag is None or combo is None or object_combo is None or bottom_combo is None or route_switch is None:
        raise AssertionError("control harness did not render all controls")

    button.setProperty("qaForceKeyboardFocus", True)
    pump(app)
    if not button.property("qaVisualFocus"):
        raise AssertionError("AppButton did not expose keyboard focus")
    if not button.property("qaTextFits"):
        raise AssertionError("AppButton text does not fit")
    QTest.keyClick(root, Qt.Key_Return)
    QTest.keyClick(root, Qt.Key_Space)
    pump(app)
    if root.property("buttonClicks") != 2:
        raise AssertionError(f"AppButton keyboard activation failed: {root.property('buttonClicks')}")
    if not custom_button.property("qaTextFits"):
        raise AssertionError("AppButton custom content reported a false text overflow")
    custom_button.setProperty("qaForceKeyboardFocus", True)
    pump(app)
    if not custom_button.property("qaVisualFocus"):
        raise AssertionError("AppButton custom content did not expose keyboard focus")
    QTest.keyClick(root, Qt.Key_Return)
    pump(app)
    if root.property("customButtonClicks") != 1:
        raise AssertionError("AppButton custom content keyboard activation failed")
    if not disabled_button.property("qaTextFits"):
        raise AssertionError("Disabled primary AppButton text does not fit")
    disabled_button.setProperty("qaForceKeyboardFocus", True)
    pump(app)
    QTest.keyClick(root, Qt.Key_Return)
    QTest.mouseClick(root, Qt.LeftButton, Qt.NoModifier, item_center(disabled_button))
    pump(app)
    if root.property("disabledButtonClicks") != 0:
        raise AssertionError("Disabled primary AppButton activated")

    if sample_tag.property("qaIsAppTag") is not True:
        raise AssertionError("AppTag did not expose its QA marker")
    if sample_tag.property("qaTextFits") is not True:
        raise AssertionError("AppTag text does not fit")
    if float(sample_tag.property("qaHorizontalPadding") or 0) < 11.5:
        raise AssertionError(f"AppTag horizontal padding too small: {sample_tag.property('qaHorizontalPadding')}")
    if float(sample_tag.property("qaVerticalPadding") or 0) < 3.5:
        raise AssertionError(f"AppTag vertical padding too small: {sample_tag.property('qaVerticalPadding')}")

    combo.setProperty("qaForceKeyboardFocus", True)
    pump(app)
    if not combo.property("qaVisualFocus"):
        raise AssertionError("AppComboBox did not expose keyboard focus")
    QTest.keyClick(root, Qt.Key_Down)
    pump(app)
    if not combo.property("qaPopupActuallyOpen") or combo.property("qaKeyboardIndex") != 1:
        raise AssertionError("AppComboBox Down did not open and advance")
    selected_option = find_item(root_item, "modelCombo_option_0")
    highlighted_option = find_item(root_item, "modelCombo_option_1")
    if selected_option is None or highlighted_option is None:
        raise AssertionError("AppComboBox popup options were not exposed for QA")
    if selected_option.property("qaSelected") is not True:
        raise AssertionError("AppComboBox current option was not visually marked")
    if highlighted_option.property("qaSelected") is True or highlighted_option.property("qaKeyboardHighlighted") is not True:
        raise AssertionError("AppComboBox keyboard highlight was conflated with the selected option")
    QTest.keyClick(root, Qt.Key_Home)
    pump(app)
    if combo.property("qaKeyboardIndex") != 0:
        raise AssertionError("AppComboBox Home did not clamp to first option")
    QTest.keyClick(root, Qt.Key_End)
    pump(app)
    if combo.property("qaKeyboardIndex") != 2:
        raise AssertionError("AppComboBox End did not clamp to last option")
    QTest.keyClick(root, Qt.Key_Return)
    pump(app)
    if combo.property("qaPopupActuallyOpen") or combo.property("qaCurrentIndex") != 2 or root.property("lastComboActivation") != 2:
        raise AssertionError("AppComboBox Return did not activate selected option")
    QTest.keyClick(root, Qt.Key_Space)
    pump(app)
    QTest.keyClick(root, Qt.Key_PageUp)
    pump(app)
    if combo.property("qaKeyboardIndex") != 0:
        raise AssertionError("AppComboBox PageUp did not move by a page")
    QTest.keyClick(root, Qt.Key_Escape)
    pump(app)
    if combo.property("qaPopupActuallyOpen"):
        raise AssertionError("AppComboBox Escape did not close the popup")

    if root.property("missingOptionText") != "" or root.property("nullOptionText") != "" or root.property("numberOptionText") != "42":
        raise AssertionError("AppComboBox optionText did not normalize edge values")

    bottom_combo.setProperty("qaPopupOpen", True)
    pump(app)
    if not bottom_combo.property("qaPopupActuallyOpen"):
        raise AssertionError("Bottom AppComboBox did not open")
    if not bottom_combo.property("qaPopupOpensUp"):
        raise AssertionError(
            "Bottom AppComboBox did not flip upward: "
            f"y={bottom_combo.property('y')} "
            f"controlHeight={bottom_combo.property('height')} "
            f"above={bottom_combo.property('qaPopupAvailableAbove')} "
            f"below={bottom_combo.property('qaPopupAvailableBelow')} "
            f"height={bottom_combo.property('qaPopupEffectiveHeight')}"
        )
    if not bottom_combo.property("qaPopupInsideWindow"):
        raise AssertionError(
            "Bottom AppComboBox popup escaped the window: "
            f"y={bottom_combo.property('y')} "
            f"controlHeight={bottom_combo.property('height')} "
            f"above={bottom_combo.property('qaPopupAvailableAbove')} "
            f"below={bottom_combo.property('qaPopupAvailableBelow')} "
            f"height={bottom_combo.property('qaPopupEffectiveHeight')}"
        )
    bottom_combo.setProperty("qaPopupOpen", False)
    pump(app)

    route_switch.setProperty("qaForceKeyboardFocus", True)
    pump(app)
    if not route_switch.property("qaVisualFocus"):
        raise AssertionError("AppSwitch did not expose keyboard focus")
    if route_switch.property("qaChecked"):
        raise AssertionError("AppSwitch started checked unexpectedly")
    if route_switch.property("qaAccessibleName") != "route setting":
        raise AssertionError("AppSwitch accessible name was not exposed")
    if not route_switch.property("qaTextFits"):
        raise AssertionError("AppSwitch reported text overflow")
    QTest.keyClick(root, Qt.Key_Return)
    pump(app)
    if root.property("switchClicks") != 1 or not root.property("lastSwitchChecked") or not route_switch.property("qaChecked"):
        raise AssertionError("AppSwitch Return did not toggle on")
    QTest.keyClick(root, Qt.Key_Space)
    pump(app)
    if root.property("switchClicks") != 2 or root.property("lastSwitchChecked") or route_switch.property("qaChecked"):
        raise AssertionError("AppSwitch Space did not toggle off")
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
        timeout=10,
    )

    assert result.returncode == 0, result.stdout + result.stderr
