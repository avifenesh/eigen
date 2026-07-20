import os
import subprocess
import sys
import textwrap
from pathlib import Path

import pytest


ROOT = Path(__file__).resolve().parents[1]


@pytest.mark.parametrize("theme_name", ["deepteal", "nord", "gruvbox"])
def test_plugins_view_management_controls_are_responsive_and_clickable(theme_name):
    script = r"""
import os
from pathlib import Path

from PySide6.QtCore import QObject, QPoint, QPointF, Qt, QtMsgType, QTimer, QUrl, Signal, qInstallMessageHandler
from PySide6.QtGui import QGuiApplication
from PySide6.QtQuick import QQuickView
from PySide6.QtQuickControls2 import QQuickStyle
from PySide6.QtTest import QTest

from eigenqt.models.plugins import PluginsModel


ROOT = Path.cwd()
ISSUE_MARKERS = (
    "ReferenceError",
    "TypeError",
    "Unable to assign",
    "Cannot assign",
    "Cannot read property",
)


class FakeRpcClient(QObject):
    connected = Signal()

    def __init__(self):
        super().__init__()
        self.calls = []
        self.failures = {}
        self.delays = {}
        self.plugins = [
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
                "marketplace": "core",
                "version": "0.1.0",
                "description": "Long local plugin description that should wrap without pushing actions outside the card.",
                "installedMs": 1783144800000,
                "enabled": False,
                "skills": ["scratch"],
                "scanStatus": "forced",
                "scanCount": 1,
                "scans": [
                    {
                        "component": "skills/scratch/SKILL.md",
                        "reasons": ["network shell", "reads process environment"],
                    }
                ],
                "warnings": ["installed with scan flags"],
            },
        ]
        self.markets = [
            {
                "name": "core",
                "source": "github.com/avifenesh/eigen-plugins",
                "owner": "Avi",
                "disabled": False,
                "addedMs": 1783152000000,
                "pluginCount": 1,
            }
        ]

    def call(self, method, *args, callback=None, error_callback=None):
        self.calls.append((method, args))
        if method in self.failures:
            payload = {"error": {"message": self.failures.pop(method)}}
        else:
            payload = {"result": self._result(method, args)}
        if callback is not None:
            QTimer.singleShot(
                int(self.delays.get(method, 0)),
                lambda payload=payload: callback(payload),
            )

    def _result(self, method, args):
        if method == "Plugins":
            return {"plugins": list(self.plugins), "marketplaces": list(self.markets)}
        if method == "AddMarketplace":
            market = {
                "name": "lab",
                "source": args[0],
                "owner": "Qt QA",
                "disabled": False,
                "addedMs": 1783159200000,
                "pluginCount": 1,
            }
            self.markets = [row for row in self.markets if row["name"] != "lab"] + [market]
            return market
        if method == "MarketplacePlugins":
            return [
                {
                    "name": "qt-tool",
                    "marketplace": args[0],
                    "version": "1.0.0",
                    "description": "Focused Qt plugin proof",
                    "skills": 1,
                    "agents": 1,
                    "commands": 1,
                }
            ]
        if method == "InstallPlugin":
            plugin = {
                "name": args[0],
                "marketplace": args[1],
                "version": "1.0.0",
                "description": "Focused Qt plugin proof",
                "installedMs": 1783159200000,
                "enabled": True,
                "skills": ["qt-proof"],
                "agents": ["qt-reviewer"],
                "commands": ["qt-check"],
                "hooks": 0,
                "scanStatus": "clean",
                "scanCount": 0,
            }
            self.plugins = [row for row in self.plugins if row["name"] != args[0]] + [plugin]
            return plugin
        if method == "SetPluginEnabled":
            for plugin in self.plugins:
                if plugin["name"] == args[0]:
                    plugin["enabled"] = bool(args[1])
                    return True
            return False
        if method == "RemovePlugin":
            before = len(self.plugins)
            self.plugins = [row for row in self.plugins if row["name"] != args[0]]
            return len(self.plugins) != before
        if method == "SetMarketEnabled":
            for market in self.markets:
                if market["name"] == args[0]:
                    market["disabled"] = not bool(args[1])
                    return True
            return False
        if method == "RemoveMarketplace":
            before = len(self.markets)
            self.markets = [row for row in self.markets if row["name"] != args[0]]
            return len(self.markets) != before
        return None


def pump(app, rounds=16):
    for _ in range(rounds):
        app.processEvents()


def visibility_score(item):
    score = float(item.property("width") or 0) * float(item.property("height") or 0)
    probe = item
    while probe is not None:
        if probe.property("visible") is False:
            score -= 1_000_000
        probe = probe.parentItem()
    return score


def find_item(root, object_name):
    matches = []

    def collect(item):
        if item is None:
            return
        if item.objectName() == object_name:
            matches.append(item)
        for child in item.childItems():
            collect(child)

    collect(root)
    return max(matches, key=visibility_score, default=None)


def reveal(app, root, item):
    flick = find_item(root, "pluginsFlick")
    if flick is None:
        raise AssertionError("plugins flickable missing")
    point = item.mapToItem(flick, QPointF(0, 0))
    current = float(flick.property("contentY") or 0)
    maximum = max(0.0, float(flick.property("contentHeight") or 0) - float(flick.property("height") or 0))
    target = max(0.0, min(maximum, current + point.y() - 120.0))
    flick.setProperty("contentY", target)
    pump(app, 20)


def item_center(item):
    point = item.mapToScene(QPointF(float(item.property("width")) / 2, float(item.property("height")) / 2))
    return QPoint(int(point.x()), int(point.y()))


def click_item(app, view, root, object_name):
    item = find_item(root, object_name)
    if item is None:
        raise AssertionError(f"missing {object_name}")
    reveal(app, root, item)
    point = item_center(item)
    if point.x() < 0 or point.x() >= view.width() or point.y() < 0 or point.y() >= view.height():
        raise AssertionError(f"{object_name} is outside the window at {point.x()},{point.y()}")
    QTest.mouseClick(view, Qt.LeftButton, Qt.NoModifier, point)
    QTest.qWait(25)
    pump(app, 24)
    return item


def assert_horizontal_bounds(item, width, label):
    left = item.mapToScene(QPointF(0, 0)).x()
    right = item.mapToScene(QPointF(float(item.property("width") or 0), 0)).x()
    if left < -0.5 or right > width + 0.5:
        raise AssertionError(f"{label} overflows horizontally: {left:.1f} -> {right:.1f} in {width}px")


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
        raise AssertionError(f"QML issues: {issues[:10]}")


QQuickStyle.setStyle("Basic")
theme_name = os.environ["EIGEN_QT_TEST_THEME"]
app = QGuiApplication(["plugins-view-test", f"--eigen-qt-theme={theme_name}"])
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
    client = FakeRpcClient()
    model = PluginsModel(client)
    view = QQuickView()
    view.setResizeMode(QQuickView.SizeRootObjectToView)
    view.setInitialProperties({"pluginsModel": model})
    view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / "PluginsView.qml")))
    if view.status() == QQuickView.Error or view.rootObject() is None:
        raise AssertionError([error.toString() for error in view.errors()])
    view.setWidth(1100)
    view.setHeight(820)
    view.show()
    pump(app, 36)
    root = view.rootObject()
    assert_no_qml_issues(messages)

    screenshots = os.environ.get("EIGEN_QT_SCREENSHOT_DIR", "")
    if screenshots:
        output = Path(screenshots)
        output.mkdir(parents=True, exist_ok=True)
        view.grabWindow().save(str(output / f"plugins-{theme_name}-wide.png"))

    view.setWidth(420)
    view.setHeight(820)
    pump(app, 36)
    add_panel = find_item(root, "pluginsAddPanel")
    source = find_item(root, "pluginsAddSourceField")
    add = find_item(root, "pluginsAddSourceButton")
    if add_panel is None or source is None or add is None:
        raise AssertionError("plugin source controls did not render")
    for item, label in ((add_panel, "add panel"), (source, "source field"), (add, "add button")):
        assert_horizontal_bounds(item, 420, label)
    if source.property("qaTextFits") is not True or add.property("qaTextFits") is not True:
        raise AssertionError("plugin source controls clip their text")

    source.forceActiveFocus(Qt.TabFocusReason)
    app.clipboard().setText("avifenesh/qt-plugins")
    QTest.keyClick(view, Qt.Key_V, Qt.ControlModifier)
    pump(app, 12)
    click_item(app, view, root, "pluginsAddSourceButton")
    if ("AddMarketplace", ("avifenesh/qt-plugins",)) not in client.calls:
        raise AssertionError(f"add source did not call the bridge: {client.calls}")
    if ("MarketplacePlugins", ("lab",)) not in client.calls:
        raise AssertionError(f"add source did not load previews: {client.calls}")
    preview = find_item(root, "pluginsPreviewRow_qt_tool")
    install = find_item(root, "pluginsInstallButton_qt_tool")
    if preview is None or install is None:
        raise AssertionError("marketplace preview did not render")
    assert_horizontal_bounds(preview, 420, "preview row")
    if install.property("qaTextFits") is not True:
        raise AssertionError("install button text is clipped")

    click_item(app, view, root, "pluginsInstallButton_qt_tool")
    if ("InstallPlugin", ("qt-tool", "lab")) not in client.calls:
        raise AssertionError(f"install did not call the bridge: {client.calls}")
    if root.property("qaPluginCount") != 3:
        raise AssertionError(f"installed plugin did not refresh inventory: {root.property('qaPluginCount')}")

    click_item(app, view, root, "pluginsScanButton_local_risk")
    scan_details = find_item(root, "pluginsScanDetails_local_risk")
    if scan_details is None or scan_details.property("visible") is not True:
        raise AssertionError("scan detail toggle did not expand")
    assert_horizontal_bounds(scan_details, 420, "scan details")

    click_item(app, view, root, "pluginsEnableSwitch_local_risk")
    if ("SetPluginEnabled", ("local-risk", True)) not in client.calls:
        raise AssertionError(f"plugin toggle did not call the bridge: {client.calls}")
    enable_risk = find_item(root, "pluginsEnableSwitch_local_risk")
    if enable_risk is None or enable_risk.property("qaChecked") is not True:
        raise AssertionError("successful plugin toggle did not stay enabled")

    client.failures["SetPluginEnabled"] = "daemon offline"
    click_item(app, view, root, "pluginsEnableSwitch_agentsys")
    error = find_item(root, "pluginsActionError")
    error_text = find_item(root, "pluginsActionErrorText")
    if error is None or error.property("visible") is not True:
        raise AssertionError("failed plugin toggle did not show an error")
    if error_text is None or "daemon offline" not in error_text.property("text"):
        raise AssertionError("plugin action error text was wrong")
    failed_enable = find_item(root, "pluginsEnableSwitch_agentsys")
    if failed_enable is None or failed_enable.property("qaChecked") is not True:
        raise AssertionError("failed plugin toggle did not reconcile to daemon state")
    click_item(app, view, root, "pluginsActionErrorDismissButton")
    if model.action_error != "":
        raise AssertionError("plugin action error did not dismiss")

    click_item(app, view, root, "pluginsUninstallButton_qt_tool")
    confirm = find_item(root, "pluginsUninstallConfirm_qt_tool")
    if confirm is None or confirm.property("visible") is not True or confirm.property("qaTextFits") is not True:
        raise AssertionError("plugin uninstall confirmation did not render cleanly")
    if screenshots:
        view.grabWindow().save(str(Path(screenshots) / f"plugins-{theme_name}-narrow-confirm.png"))
    client.delays["RemovePlugin"] = 120
    click_item(app, view, root, "pluginsUninstallConfirm_qt_tool")
    if ("RemovePlugin", ("qt-tool",)) not in client.calls:
        raise AssertionError(f"plugin uninstall did not call the bridge: {client.calls}")
    pending_remove = find_item(root, "pluginsUninstallButton_qt_tool")
    if pending_remove is None or pending_remove.property("text") != "Removing..." or pending_remove.property("enabled") is not False:
        raise AssertionError("slow plugin removal did not expose a locked pending state")
    QTest.qWait(140)
    pump(app, 24)
    if root.property("qaPluginCount") != 2:
        raise AssertionError("plugin uninstall did not refresh inventory")

    click_item(app, view, root, "pluginsMarketEnableSwitch_lab")
    if ("SetMarketEnabled", ("lab", False)) not in client.calls:
        raise AssertionError(f"market toggle did not call the bridge: {client.calls}")
    market_toggle = find_item(root, "pluginsMarketEnableSwitch_lab")
    if market_toggle is None or market_toggle.property("qaChecked") is not False:
        raise AssertionError("successful marketplace toggle did not follow daemon state")
    click_item(app, view, root, "pluginsBrowseButton_lab")
    if client.calls.count(("MarketplacePlugins", ("lab",))) < 2:
        raise AssertionError(f"market browse did not reload previews: {client.calls}")
    click_item(app, view, root, "pluginsMarketRemoveButton_lab")
    market_confirm = find_item(root, "pluginsMarketRemoveConfirm_lab")
    if market_confirm is None or market_confirm.property("visible") is not True:
        raise AssertionError("market removal confirmation did not render")
    click_item(app, view, root, "pluginsMarketRemoveConfirm_lab")
    if ("RemoveMarketplace", ("lab",)) not in client.calls:
        raise AssertionError(f"market removal did not call the bridge: {client.calls}")
    if root.property("qaMarketplaceCount") != 1:
        raise AssertionError("market removal did not refresh inventory")
    if root.property("qaPreviewCount") != 0:
        raise AssertionError("removed marketplace left stale install previews")

    for name in ("pluginsInstalledRow_agentsys", "pluginsInstalledRow_local_risk", "pluginsMarketRow_core"):
        item = find_item(root, name)
        if item is None or item.property("qaTextFits") is not True:
            raise AssertionError(f"{name} text does not fit")
        assert_horizontal_bounds(item, 420, name)
    assert_no_qml_issues(messages)
    model.set_active(False)
    view.close()
finally:
    qInstallMessageHandler(previous_handler)
"""
    env = os.environ.copy()
    env.setdefault("QT_QPA_PLATFORM", "offscreen")
    env.setdefault("QML_DISABLE_DISK_CACHE", "1")
    env["PYTHONPATH"] = str(ROOT) + os.pathsep + env.get("PYTHONPATH", "")
    env["EIGEN_QT_TEST_THEME"] = theme_name
    result = subprocess.run(
        [sys.executable, "-c", textwrap.dedent(script)],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, result.stdout + result.stderr
