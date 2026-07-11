import os
import subprocess
import sys
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_requested_surface_qml_mouse_actions_do_not_crash():
    script = r"""
from pathlib import Path

from PySide6.QtCore import QObject, QPoint, QPointF, QMetaObject, QSize, QTimer, QUrl, Qt, Signal, Slot
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlContext
from PySide6.QtQuick import QQuickView
from PySide6.QtQuickControls2 import QQuickStyle
from PySide6.QtTest import QTest

from eigenqt.clipboard_helper import ClipboardHelper
from eigenqt.highlighter_helper import HighlighterHelper
from eigenqt.markdown_helper import MarkdownHelper
from eigenqt.models.config import ConfigModel, RuleChainsModel
from eigenqt.models.connectors import ConnectorsModel
from eigenqt.models.crons import CronsModel
from eigenqt.models.dreaming import DreamingModel
from eigenqt.models.machines import MachinesModel
from eigenqt.models.memory import MemoryModel
from eigenqt.models.notes import NotesController
from eigenqt.models.observe import ObserveModel
from eigenqt.models.plugins import PluginsModel
from eigenqt.models.profile import ProfileModel
from eigenqt.models.reviewers import ReviewersModel
from eigenqt.models.skills import ProposalsModel, SkillsModel


ROOT = Path.cwd()
SIZE = QSize(1200, 800)
EXPECTED_PLACEHOLDER_COLOR = "#52605e"


def color_name(value):
    if hasattr(value, "name"):
        return value.name().lower()
    return str(value).lower()


def assert_placeholder_color(item, name):
    color = color_name(item.property("placeholderTextColor")) if item is not None else ""
    if color != EXPECTED_PLACEHOLDER_COLOR:
        raise AssertionError(f"{name} placeholder color regressed: {color}")


def assert_app_text_field(item, name):
    if item is None:
        raise AssertionError(f"missing {name} input")
    if item.property("qaIsAppTextField") is not True:
        raise AssertionError(f"{name} input did not use AppTextField")
    if item.property("qaTextFits") is not True:
        raise AssertionError(f"{name} input text did not fit")
    if float(item.property("qaHorizontalPadding") or 0) < 11.5:
        raise AssertionError(f"{name} input horizontal padding too small: {item.property('qaHorizontalPadding')}")
    if float(item.property("qaVerticalPadding") or 0) < 5.5:
        raise AssertionError(f"{name} input vertical padding too small: {item.property('qaVerticalPadding')}")


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
        self.delays = {}
        self.revuto_paused = False

    def call(self, method, *args, callback=None, error_callback=None):
        self.calls.append((method, args))
        payload = self._payload(method, args)
        if "error" in payload and error_callback:
            error_callback(payload["error"])
        if callback:
            if method in self.delays:
                QTimer.singleShot(int(self.delays.get(method, 0)), lambda: callback(payload))
            else:
                callback(payload)

    @Slot(str, "QVariantList", result=int)
    def callToken(self, method, args):
        self._token += 1
        token = self._token
        call_args = tuple(args or [])
        self.calls.append((method, call_args))
        QTimer.singleShot(
            int(self.delays.get(method, 0)),
            lambda: self.callDone.emit(token, self._payload(method, call_args)),
        )
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

    def _payload(self, method, args):
        if method in self.failures:
            return {"error": self.failures[method]}
        return {"result": self._result(method, args)}

    def _result(self, method, args):
        if method == "Config":
            return seeded_config_payload()
        if method == "RuleChains":
            return seeded_rule_chains_payload()
        if method == "SetConfig":
            return args[1]
        if method == "SetRuleChain":
            return list(args[1])
        if method == "Connectors":
            return seeded_connectors_payload()
        if method == "MCPServers":
            return {"servers": []}
        if method == "GoogleStatus":
            return {"configured": False, "connected": False, "clientPath": "", "setupUrl": "", "setupHint": ""}
        if method == "MCPSecretsAvailable":
            return True
        if method == "AddCatalogConnector":
            return args[0]
        if method == "AddConnector":
            return {"name": args[0], "url": args[1], "description": args[2]}
        if method == "SaveMCPServer":
            return args[0]
        if method == "ListMemoryScopes":
            return [
                {"key": "global", "name": "Global", "dir": "", "noteCount": 2, "current": True},
                {"key": "project:/repo/eigen", "name": "eigen", "dir": "/repo/eigen", "noteCount": 0},
            ]
        if method == "MemoryForScope":
            return seeded_global_memory()
        if method == "ObserveSummary":
            return {}
        if method == "DreamingForScope":
            return {"scope": args[0] if args else "project", "rollouts": [], "consolidations": [], "currentBytes": 0}
        if method == "Plugins":
            return {"plugins": [], "marketplaces": []}
        if method == "Machines":
            return {"machines": []}
        if method == "Crons":
            return {"crons": [], "timers": 0, "crontab": 0, "systemdAvail": False}
        if method in ("AppendMemory", "WriteUserProfile", "AddBan", "RemoveMemoryNote", "RemoveAdHocMemoryNote", "MoveMemoryNote"):
            return {}
        if method == "ObsidianStatus":
            return {"available": True, "vault": "/home/user/notes"}
        if method == "ObsidianNotes":
            return [{"path": "Inbox/Existing.md", "title": "Existing"}]
        if method == "ObsidianRead":
            return "# Existing\n\nBody"
        if method == "ObsidianWrite":
            return args[0]
        if method == "RevutoStatus":
            return {"available": True, "count": 1, "paused": 1 if self.revuto_paused else 0}
        if method == "RevutoReviewers":
            return [{"repo": "avifenesh/eigen", "paused": self.revuto_paused}]
        if method == "RevutoTrigger":
            return {}
        if method == "RevutoSetPaused":
            self.revuto_paused = bool(args[1])
            return {}
        if method == "Skills":
            return seeded_skill_snapshot()
        if method == "SkillBody":
            return f"# {args[0]}\n\nProject-local skill body for visual QA."
        if method in ("AcceptSkill", "RejectSkill"):
            return True
        if method in ("InstallSkillFromPath", "InstallSkillFromGitHub"):
            return {"name": args[0], "source": "user"}
        return {}


def pump(app, count=12):
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
        opacity = probe.property("opacity")
        if opacity is not None and float(opacity) <= 0.01:
            score -= 1_000_000
        probe = probe.parentItem()

    if item.property("enabled") is False:
        score -= 100_000

    try:
        point = item.mapToScene(QPointF(width / 2, height / 2))
        if point.x() < -0.5 or point.y() < -0.5 or point.x() > SIZE.width() + 0.5 or point.y() > SIZE.height() + 0.5:
            score -= 100_000
    except RuntimeError:
        score -= 1_000_000
    return score


def find_visual_item(item, object_name):
    matches = []

    def collect(candidate):
        if candidate is None:
            return
        if candidate.objectName() == object_name:
            matches.append(candidate)
        for child in candidate.childItems():
            collect(child)

    collect(item)
    if matches:
        return max(matches, key=item_visibility_score)
    return None


def item_center(item):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    if width <= 0 or height <= 0:
        raise AssertionError(f"{item.objectName()} has invalid size {width}x{height}")
    point = item.mapToScene(QPointF(width / 2, height / 2))
    if point.x() < -0.5 or point.y() < -0.5 or point.x() > SIZE.width() + 0.5 or point.y() > SIZE.height() + 0.5:
        raise AssertionError(f"{item.objectName()} center is offscreen at {point.x():.1f},{point.y():.1f}")
    x = max(0, min(SIZE.width() - 1, int(point.x())))
    y = max(0, min(SIZE.height() - 1, int(point.y())))
    return QPoint(x, y)


def scroll_to_item(app, root, item, flick_name):
    flick = find_visual_item(root, flick_name)
    if flick is None:
        raise AssertionError(f"missing flickable {flick_name}")
    point = item.mapToItem(flick, QPointF(float(item.property("width") or 0) / 2, float(item.property("height") or 0) / 2))
    content_y = float(flick.property("contentY") or 0)
    viewport_height = float(flick.property("height") or SIZE.height())
    content_height = float(flick.property("contentHeight") or viewport_height)
    target = content_y
    if point.y() < 48:
        target = content_y + point.y() - 96
    elif point.y() > viewport_height - 48:
        target = content_y + point.y() - viewport_height / 2
    target = max(0, min(target, max(0, content_height - viewport_height)))
    flick.setProperty("contentY", target)
    pump(app, 8)


def click_item(app, window, root, object_name, *, flick_name=None):
    pump(app, 8)
    item = find_visual_item(root, object_name)
    if item is None:
        item = find_visual_item(window.contentItem(), object_name)
    if item is None:
        raise AssertionError(f"missing item {object_name}")
    if flick_name:
        scroll_to_item(app, root, item, flick_name)
    QTest.mouseClick(window, Qt.LeftButton, Qt.NoModifier, item_center(item))
    QTest.qWait(20)
    pump(app)
    return item


def invoke_click(item):
    QMetaObject.invokeMethod(item, "click")


def set_text(app, root, object_name, value):
    item = find_visual_item(root, object_name)
    if item is None:
        raise AssertionError(f"missing text item {object_name}")
    item.setProperty("text", value)
    pump(app)
    return item


def focus_item(app, root, object_name, *, flick_name=None):
    pump(app, 8)
    item = find_visual_item(root, object_name)
    if item is None:
        raise AssertionError(f"missing item {object_name}")
    if flick_name:
        scroll_to_item(app, root, item, flick_name)
    try:
        item.forceActiveFocus(Qt.TabFocusReason)
    except AttributeError:
        item.setProperty("focus", True)
    pump(app, 4)
    return item


def press_key_on_item(app, window, root, object_name, key, modifiers=Qt.NoModifier, *, flick_name=None):
    item = focus_item(app, root, object_name, flick_name=flick_name)
    QTest.keyClick(window, key, modifiers)
    pump(app)
    return item


def load_view(app, client, qml_file, *, context=None, root_props=None):
    view = QQuickView()
    view.setResizeMode(QQuickView.SizeRootObjectToView)
    view.setWidth(SIZE.width())
    view.setHeight(SIZE.height())
    view.engine().addImportPath(str(ROOT / "eigenqt"))

    ctx: QQmlContext = view.rootContext()
    ctx.setContextProperty("rpcClient", client)
    ctx.setContextProperty("clipboardHelper", ClipboardHelper(view))
    ctx.setContextProperty("highlighter", HighlighterHelper(view))
    ctx.setContextProperty("markdownParser", MarkdownHelper(view))
    for name, value in (context or {}).items():
        ctx.setContextProperty(name, value)
    if root_props:
        view.setInitialProperties(root_props)

    view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / qml_file)))
    if view.status() == QQuickView.Error or view.rootObject() is None:
        errors = [error.toString() for error in view.errors()]
        raise AssertionError(f"failed to load {qml_file}: {errors}")

    view.show()
    pump(app, 24)
    return view, view.rootObject()


def close_view(app, view):
    view.hide()
    view.setSource(QUrl())
    pump(app, 6)
    view.destroy()


def assert_call(client, start, method, args):
    expected = (method, args)
    actual = client.calls[start:]
    if expected not in actual:
        raise AssertionError(f"missing call {expected}; new calls={actual}")


def click_item_until_call(app, window, root, object_name, client, start, method, args, *, flick_name=None):
    item = click_item(app, window, root, object_name, flick_name=flick_name)
    if (method, args) not in client.calls[start:]:
        invoke_click(item)
        pump(app)
    assert_call(client, start, method, args)
    return item


def qml_map_value(value, key):
    if hasattr(value, "toVariant"):
        value = value.toVariant()
    if isinstance(value, dict):
        return value.get(key)
    return None


def seeded_config_model(client):
    model = ConfigModel(client)
    model._on_config_result({"result": seeded_config_payload()})
    return model


def seeded_rule_chains_model(client):
    model = RuleChainsModel(client)
    model._on_rule_chains_result({"result": seeded_rule_chains_payload()})
    return model


def seeded_config_payload():
    return {
        "path": "/home/user/.eigen/config.json",
        "fields": [
            {"key": "model", "desc": "Default model", "value": "gpt-5", "options": ["gpt-5", "local-qwen"], "multi": False, "allowEmpty": False},
            {"key": "route", "desc": "Enable router", "value": "true", "options": ["true", "false"], "multi": False, "allowEmpty": False},
            {"key": "route_providers", "desc": "Providers", "value": "openai,xai", "options": ["openai", "xai", "local"], "multi": True, "allowEmpty": True},
            {"key": "notify_cmd", "desc": "Attention command", "value": "", "options": [], "multi": False, "allowEmpty": True},
        ],
    }


def seeded_rule_chains_payload():
    return {
        "models": ["gpt-5", "local-qwen", "grok-4"],
        "roles": [{"role": "primary", "desc": "Primary chain", "chain": ["gpt-5", "local-qwen"], "custom": True}],
    }


def seeded_connectors_payload():
    return {
        "connectors": [{"name": "notion", "display": "Notion", "url": "https://mcp.notion.com/mcp", "connected": True}],
        "directory": [{"name": "slack", "display": "Slack", "added": False}],
    }


def seeded_connectors_model(client):
    model = ConnectorsModel(client)
    model.connectors = seeded_connectors_payload()
    model.servers = {
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
    model.google_status = {"configured": False, "connected": False, "clientPath": "", "setupUrl": "", "setupHint": ""}
    model.obsidian_status = {"available": True, "vault": "/home/user/notes"}
    model.revuto_status = {"available": True, "count": 1, "paused": 0}
    model.reviewers = [{"repo": "avifenesh/eigen", "paused": False}]
    model.secrets_ok = True
    model.loading = False
    return model


def seeded_global_memory():
    return {
        "scope": "global",
        "summary": "Eigen memory summary.",
        "hasSummary": True,
        "notes": [{"index": 0, "text": "Keep Qt proof focused."}],
        "adHoc": [{"index": 0, "text": "Manual note proof."}],
        "noteCount": 1,
        "profile": "Existing profile",
        "profileLearned": "Prefers focused Qt proof.",
        "banList": [],
        "backups": 0,
        "bytes": 100,
    }


def seeded_memory_model(client):
    model = MemoryModel(client)
    model.scopes = [
        {"key": "global", "name": "Global", "dir": "", "noteCount": 2, "current": True},
        {"key": "project:/repo/eigen", "name": "eigen", "dir": "/repo/eigen", "noteCount": 0},
    ]
    model.scope_key = "global"
    model.current = seeded_global_memory()
    model.loading = False
    return model


def seeded_notes_controller(client):
    controller = NotesController(client)
    controller._on_status_result({"result": {"available": True, "vault": "/home/user/notes"}})
    return controller


def seeded_reviewers_model(client):
    model = ReviewersModel(client)
    model._on_status_result({"result": {"available": True, "count": 1, "paused": 0}})
    return model


def seeded_skill_snapshot():
    return {
        "skills": [
            {"name": "frontend-design", "description": "Design checks", "source": "user", "path": "/tmp/SKILL.md"},
            {"name": "project-commands", "description": "Project command palette helpers", "source": "project", "path": "/repo/.eigen/skills/project-commands/SKILL.md"},
        ],
        "proposals": [{"name": "qt-qa", "description": "Visual QML proof", "path": "/tmp/qt-qa/SKILL.md"}],
    }


def seeded_skill_models(client):
    skills = SkillsModel(client)
    proposals = ProposalsModel(client)
    result = {"result": seeded_skill_snapshot()}
    skills._on_skills_result(result)
    proposals._on_skills_result(result)
    return skills, proposals


def check_config(app, client):
    load_error_config = ConfigModel(client)
    load_error_chains = RuleChainsModel(client)
    view, root = load_view(
        app,
        client,
        "ConfigView.qml",
        context={"configModel": load_error_config, "ruleChainsModel": load_error_chains},
        root_props={"configModel": load_error_config, "ruleChainsModel": load_error_chains},
    )
    try:
        if load_error_config.rowCount() > 0:
            load_error_config.beginResetModel()
            load_error_config._fields = []
            load_error_config.endResetModel()
        if load_error_chains.rowCount() > 0:
            load_error_chains.beginResetModel()
            load_error_chains._roles = []
            load_error_chains._models = []
            load_error_chains.endResetModel()
        load_error_config._set_load_error("daemon offline")
        load_error_chains._set_load_error("daemon offline")
        assert_load_error_retry(app, view, root, "configLoadError", "configLoadErrorText", "configLoadErrorRetry")
        current_tabs = root.property("currentTabs")
        if hasattr(current_tabs, "toVariant"):
            current_tabs = current_tabs.toVariant()
        if current_tabs:
            raise AssertionError(f"config load error kept stale tabs: {current_tabs}")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "configLoadErrorRetry",
            client,
            start,
            "Config",
            (),
            flick_name="configFlick",
        )
        assert_call(client, start, "Config", ())
        assert_call(client, start, "RuleChains", ())
    finally:
        close_view(app, view)

    config = seeded_config_model(client)
    chains = seeded_rule_chains_model(client)
    view, root = load_view(app, client, "ConfigView.qml", context={"configModel": config, "ruleChainsModel": chains}, root_props={"configModel": config, "ruleChainsModel": chains})
    try:
        if not config._poll_timer.isActive() or not chains._poll_timer.isActive():
            raise AssertionError("config view did not activate route-scoped polling while visible")

        combo = press_key_on_item(app, view, root, "configSelect_model", Qt.Key_Space, flick_name="configFlick")
        if not combo.property("qaPopupActuallyOpen"):
            raise AssertionError("config model dropdown did not open from Space")
        press_key_on_item(app, view, root, "configSelect_model", Qt.Key_Escape, flick_name="configFlick")
        if combo.property("qaPopupActuallyOpen"):
            raise AssertionError("config model dropdown did not close from Escape")

        start = len(client.calls)
        combo = press_key_on_item(app, view, root, "configSelect_model", Qt.Key_Space, flick_name="configFlick")
        current_index = int(combo.property("qaCurrentIndex"))
        press_key_on_item(app, view, root, "configSelect_model", Qt.Key_Down, flick_name="configFlick")
        if int(combo.property("qaKeyboardIndex")) != current_index + 1:
            raise AssertionError("config model dropdown did not move keyboard highlight down")
        if any(call[0] == "SetConfig" for call in client.calls[start:]):
            raise AssertionError("config model dropdown committed while only moving the keyboard highlight")
        press_key_on_item(app, view, root, "configSelect_model", Qt.Key_Return, flick_name="configFlick")
        assert_call(client, start, "SetConfig", ("model", "local-qwen"))

        general_tab = find_visual_item(root, "configTab_General")
        models_tab = find_visual_item(root, "configTab_Models")
        integrations_tab = find_visual_item(root, "configTab_Integrations")
        if general_tab is None or models_tab is None:
            raise AssertionError("config tabs did not render all expected tabs")
        for tab, name in ((general_tab, "General"), (models_tab, "Models"), (integrations_tab, "Integrations")):
            if tab is not None and tab.property("qaTextFits") is not True:
                raise AssertionError(f"config {name} tab text did not fit")
        general_right = general_tab.mapToScene(QPointF(float(general_tab.property("width") or 0), 0)).x()
        models_left = models_tab.mapToScene(QPointF(0, 0)).x()
        models_right = models_tab.mapToScene(QPointF(float(models_tab.property("width") or 0), 0)).x()
        integrations_gap = 0
        if integrations_tab is not None:
            integrations_left = integrations_tab.mapToScene(QPointF(0, 0)).x()
            integrations_gap = integrations_left - models_right
        if models_left - general_right > 12 or integrations_gap > 12:
            raise AssertionError(
                "config tabs are spread across the toolbar instead of forming a compact row: "
                f"gaps={models_left - general_right:.1f},{integrations_gap:.1f}"
            )

        config._set_load_error("daemon offline")
        retry = assert_load_error_retry(app, view, root, "configRefreshErrorBanner", "configRefreshErrorText", "configRefreshErrorRetry")
        start = len(client.calls)
        invoke_click(retry)
        pump(app)
        assert_call(client, start, "Config", ())
        assert_call(client, start, "RuleChains", ())

        click_item(app, view, root, "configTab_Models")
        if root.property("activeTab") != "Models":
            raise AssertionError("config Models tab did not activate")

        client.delays["SetConfig"] = 45
        start = len(client.calls)
        button = focus_item(app, root, "configBoolToggle_route", flick_name="configFlick")
        if not button.property("qaVisualFocus"):
            raise AssertionError("config route toggle did not expose keyboard focus")
        if button.property("qaAccessibleName") != "route setting":
            raise AssertionError(f"config route toggle accessible name was {button.property('qaAccessibleName')}")
        if not button.property("qaTextFits") or not button.property("qaChecked"):
            raise AssertionError("config route toggle did not expose healthy QA state")
        QTest.keyClick(view, Qt.Key_Space)
        pump(app)
        assert_call(client, start, "SetConfig", ("route", "false"))
        if int(root.property("qaConfigSavingCount")) != 1 or button.property("visible") is not False:
            raise AssertionError("config route toggle did not enter a saving-only state")
        invoke_click(button)
        pump(app, 2)
        if client.calls[start:].count(("SetConfig", ("route", "false"))) != 1:
            raise AssertionError("config route toggle allowed a duplicate save while pending")
        QTest.qWait(70)
        pump(app, 12)
        if int(root.property("qaConfigSavingCount")) != 0:
            raise AssertionError("config field saving state did not clear after success")
        del client.delays["SetConfig"]

        start = len(client.calls)
        chip = focus_item(app, root, "configMultiChip_route_providers_local", flick_name="configFlick")
        if not chip.property("qaVisualFocus"):
            raise AssertionError("config route_providers chip did not expose keyboard focus")
        if float(chip.property("leftPadding") or 0) < 11.5 or float(chip.property("rightPadding") or 0) < 11.5:
            raise AssertionError(
                "config route_providers chip side padding too small: "
                f"{chip.property('leftPadding')}x{chip.property('rightPadding')}"
            )
        if float(chip.property("topPadding") or 0) < 3.5 or float(chip.property("bottomPadding") or 0) < 3.5:
            raise AssertionError(
                "config route_providers chip vertical padding too small: "
                f"{chip.property('topPadding')}x{chip.property('bottomPadding')}"
            )
        if float(chip.property("height") or 0) < 23.5:
            raise AssertionError(f"config route_providers chip height too small: {chip.property('height')}")
        QTest.keyClick(view, Qt.Key_Space)
        pump(app)
        assert_call(client, start, "SetConfig", ("route_providers", "openai xai local"))

        click_item(app, view, root, "configTab_Integrations")
        if root.property("activeTab") != "Integrations":
            raise AssertionError("config Integrations tab did not activate")
        start = len(client.calls)
        notify_input = focus_item(app, root, "configText_notify_cmd", flick_name="configFlick")
        if notify_input.property("qaIsAppTextField") is not True:
            raise AssertionError("config notify_cmd did not use shared AppTextField")
        if notify_input.property("qaTextFits") is not True:
            raise AssertionError(f"config notify_cmd text does not fit: {notify_input.property('qaText')!r}")
        notify_input.setProperty("text", "notify-send Eigen")
        pump(app)
        QTest.keyClick(view, Qt.Key_Return)
        pump(app)
        assert_call(client, start, "SetConfig", ("notify_cmd", "notify-send Eigen"))

        click_item(app, view, root, "configTab_Models")
        if root.property("activeTab") != "Models":
            raise AssertionError("config Models tab did not reactivate after Integrations")
        client.delays["SetRuleChain"] = 45
        start = len(client.calls)
        down = click_item(app, view, root, "configChainMoveDown_primary_0", flick_name="configFlick")
        if ("SetRuleChain", ("primary", ["local-qwen", "gpt-5"])) not in client.calls[start:]:
            invoke_click(down)
            pump(app)
        assert_call(client, start, "SetRuleChain", ("primary", ["local-qwen", "gpt-5"]))
        if int(root.property("qaRuleChainSavingCount")) != 1 or down.property("enabled"):
            raise AssertionError("config rule-chain move did not enter a disabled saving state")
        click_item(app, view, root, "configChainMoveDown_primary_0", flick_name="configFlick")
        if client.calls[start:].count(("SetRuleChain", ("primary", ["local-qwen", "gpt-5"]))) != 1:
            raise AssertionError("config rule-chain move allowed a duplicate save while pending")
        QTest.qWait(60)
        pump(app, 12)
        if int(root.property("qaRuleChainSavingCount")) != 0:
            raise AssertionError("config rule-chain saving state did not clear after success")
        del client.delays["SetRuleChain"]

        client.failures["SetRuleChain"] = "chain denied"
        start = len(client.calls)
        reset = click_item(app, view, root, "configResetChain_primary", flick_name="configFlick")
        if ("SetRuleChain", ("primary", [])) not in client.calls[start:]:
            invoke_click(reset)
            pump(app)
        assert_call(client, start, "SetRuleChain", ("primary", []))
        if int(root.property("qaRuleChainSavingCount")) != 0:
            raise AssertionError("failed rule-chain save left the row in saving state")
        error_box = find_visual_item(root, "configActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed rule-chain save did not render an error")
        if "chain denied" not in root.property("actionError"):
            raise AssertionError(f"unexpected rule-chain error: {root.property('actionError')}")
        del client.failures["SetRuleChain"]
    finally:
        close_view(app, view)
        if config._poll_timer.isActive() or chains._poll_timer.isActive():
            raise AssertionError("config view did not stop route-scoped polling after close")


def check_connectors(app, client):
    load_error_model = ConnectorsModel(client)
    load_error_model.loading = False
    load_error_model.load_error = "daemon offline"
    view, root = load_view(app, client, "ConnectorsView.qml", context={"connectorsModel": load_error_model}, root_props={"connectorsModel": load_error_model})
    try:
        load_error = find_visual_item(root, "connectorsLoadError")
        load_error_text = find_visual_item(root, "connectorsLoadErrorText")
        retry = find_visual_item(root, "connectorsLoadErrorRetry")
        if load_error is None or not load_error.property("visible"):
            raise AssertionError("connectors load error did not render")
        if load_error_text is None or "daemon offline" not in load_error_text.property("text"):
            raise AssertionError(f"connectors load error text was wrong: {load_error_text.property('text') if load_error_text else None}")
        if retry is None or not retry.property("qaTextFits"):
            raise AssertionError("connectors load error retry did not render cleanly")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "connectorsLoadErrorRetry",
            client,
            start,
            "Connectors",
            (),
            flick_name="connectorsFlick",
        )
        assert_call(client, start, "Connectors", ())
        assert_call(client, start, "MCPServers", ())
        assert_call(client, start, "GoogleStatus", ())
    finally:
        close_view(app, view)

    connectors = seeded_connectors_model(client)
    view, root = load_view(app, client, "ConnectorsView.qml", context={"connectorsModel": connectors}, root_props={"connectorsModel": connectors})
    try:
        connector_card = find_visual_item(root, "connectorCard_connector_notion")
        if connector_card is None or connector_card.property("qaIconText") != "N":
            raise AssertionError(f"connector card did not derive a useful icon initial: {connector_card.property('qaIconText') if connector_card is not None else None!r}")

        google_setup = click_item(app, view, root, "connectorPrimaryButton_google", flick_name="connectorsFlick")
        if root.property("qaGoogleClientDialogOpen") is not True:
            invoke_click(google_setup)
            pump(app)
        if root.property("qaGoogleClientDialogOpen") is not True:
            raise AssertionError("Google setup did not open the native client JSON picker")
        google_dialog = root.findChild(QObject, "connectorsGoogleClientDialog")
        if google_dialog is None or not QMetaObject.invokeMethod(google_dialog, "close"):
            raise AssertionError("could not close the native Google client JSON picker")
        pump(app)
        if root.property("qaGoogleClientDialogOpen") is not False:
            raise AssertionError("Google client JSON picker stayed open after close")
        close_view(app, view)

        connectors = seeded_connectors_model(client)
        view, root = load_view(app, client, "ConnectorsView.qml", context={"connectorsModel": connectors}, root_props={"connectorsModel": connectors})
        connector_card = find_visual_item(root, "connectorCard_connector_notion")
        if connector_card is None:
            raise AssertionError("connector card did not re-render after the Google picker closed")

        connectors.load_error = "daemon offline"
        pump(app, 12)
        initial_error = find_visual_item(root, "connectorsLoadError")
        refresh_error = find_visual_item(root, "connectorsRefreshErrorBanner")
        refresh_text = find_visual_item(root, "connectorsRefreshErrorText")
        refresh_retry = find_visual_item(root, "connectorsRefreshErrorRetry")
        if initial_error is not None and initial_error.property("visible"):
            raise AssertionError("connectors refresh error replaced stale content with the initial error state")
        if refresh_error is None or not refresh_error.property("visible"):
            raise AssertionError("connectors refresh error banner did not render")
        if refresh_text is None or "daemon offline" not in refresh_text.property("text"):
            raise AssertionError(f"connectors refresh error text was wrong: {refresh_text.property('text') if refresh_text else None}")
        if refresh_retry is None or not refresh_retry.property("qaTextFits"):
            raise AssertionError("connectors refresh retry did not render cleanly")
        if connector_card.property("visible") is not True:
            raise AssertionError("connectors refresh error hid the stale connector card")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "connectorsRefreshErrorRetry",
            client,
            start,
            "Connectors",
            (),
            flick_name="connectorsFlick",
        )
        assert_call(client, start, "Connectors", ())
        assert_call(client, start, "MCPServers", ())
        assert_call(client, start, "GoogleStatus", ())

        remove = click_item(app, view, root, "connectorRemoveButton_connector_notion", flick_name="connectorsFlick")
        if not connectors.confirm_remove_connector.get("notion"):
            invoke_click(remove)
            pump(app)
        if not connectors.confirm_remove_connector.get("notion"):
            raise AssertionError("connector remove did not reveal confirmation")
        confirm = find_visual_item(root, "connectorConfirmRemoveButton_connector_notion")
        cancel = find_visual_item(root, "connectorCancelRemoveButton_connector_notion")
        if confirm is None or cancel is None:
            raise AssertionError("connector remove confirm/cancel buttons did not render")
        if not confirm.property("qaTextFits") or not cancel.property("qaTextFits"):
            raise AssertionError("connector remove confirm/cancel text does not fit")
        invoke_click(cancel)
        pump(app)
        if connectors.confirm_remove_connector.get("notion"):
            raise AssertionError("connector remove cancel did not reset confirmation")

        remove = click_item(app, view, root, "connectorRemoveButton_connector_notion", flick_name="connectorsFlick")
        if not connectors.confirm_remove_connector.get("notion"):
            invoke_click(remove)
            pump(app)
        confirm = find_visual_item(root, "connectorConfirmRemoveButton_connector_notion")
        if confirm is None:
            raise AssertionError("connector remove confirm did not return after cancel")
        client.delays["RemoveConnector"] = 45
        start = len(client.calls)
        invoke_click(confirm)
        pump(app, 2)
        remove_calls = [call for call in client.calls[start:] if call[0] == "RemoveConnector" and call[1] == ("notion",)]
        if len(remove_calls) != 1:
            raise AssertionError(f"expected one pending RemoveConnector call, got {remove_calls}")
        pending_remove = find_visual_item(root, "connectorRemoveButton_connector_notion")
        pending_primary = find_visual_item(root, "connectorPrimaryButton_connector_notion")
        if pending_remove is None or pending_remove.property("enabled") is not False:
            raise AssertionError("pending connector remove did not disable the remove button")
        if pending_primary is None or pending_primary.property("enabled") is not False:
            raise AssertionError("pending connector remove did not disable the primary action")
        invoke_click(pending_remove)
        invoke_click(pending_primary)
        pump(app, 2)
        remove_calls = [call for call in client.calls[start:] if call[0] == "RemoveConnector" and call[1] == ("notion",)]
        if len(remove_calls) != 1:
            raise AssertionError(f"pending connector remove duplicated RPCs: {remove_calls}")
        QTest.qWait(70)
        pump(app)
        del client.delays["RemoveConnector"]

        client.failures["AddCatalogConnector"] = "catalog denied"
        start = len(client.calls)
        tile = focus_item(app, root, "catalogTile_slack", flick_name="connectorsFlick")
        if not tile.property("qaVisualFocus"):
            raise AssertionError("connector catalog tile did not expose keyboard focus")
        if tile.property("qaAccessibleName") != "Connect Slack":
            raise AssertionError(f"connector catalog tile accessible name was wrong: {tile.property('qaAccessibleName')}")
        if tile.property("qaIconText") != "S":
            raise AssertionError(f"connector catalog tile did not derive a useful icon initial: {tile.property('qaIconText')!r}")
        QTest.keyClick(view, Qt.Key_Space)
        pump(app)
        if ("AddCatalogConnector", ("slack",)) not in client.calls[start:]:
            invoke_click(tile)
            pump(app)
        assert_call(client, start, "AddCatalogConnector", ("slack",))
        error_box = find_visual_item(root, "connectorsActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed catalog connector add did not render a global action error")
        if connectors.connecting:
            raise AssertionError(f"failed catalog connector add left connecting state: {connectors.connecting}")
        if "catalog denied" not in connectors.action_error:
            raise AssertionError(f"unexpected catalog connector error: {connectors.action_error}")
        dismiss = click_item(app, view, root, "connectorsDismissActionError", flick_name="connectorsFlick")
        if connectors.action_error:
            invoke_click(dismiss)
            pump(app)
        if connectors.action_error:
            raise AssertionError("connector action error did not dismiss")
        del client.failures["AddCatalogConnector"]

        button = click_item(app, view, root, "connectorsAddButton", flick_name="connectorsFlick")
        if not connectors.add_open:
            invoke_click(button)
            pump(app)
        add_button = find_visual_item(root, "connectorsAddAuthorizeButton")
        if add_button is None:
            raise AssertionError("missing connector add authorize button")
        if add_button.property("enabled"):
            raise AssertionError("empty connector form should not be submittable")
        add_name = find_visual_item(root, "connectorsAddNameInput")
        add_url = find_visual_item(root, "connectorsAddUrlInput")
        add_desc = find_visual_item(root, "connectorsAddDescInput")
        assert_app_text_field(add_name, "connector name")
        assert_app_text_field(add_url, "connector URL")
        assert_app_text_field(add_desc, "connector description")
        assert_placeholder_color(add_name, "connector name")
        set_text(app, root, "connectorsAddNameInput", "linear")
        if add_button.property("enabled"):
            raise AssertionError("connector form without URL should not be submittable")
        set_text(app, root, "connectorsAddUrlInput", "https://mcp.linear.app/mcp")
        set_text(app, root, "connectorsAddDescInput", "Linear issues")
        if not add_button.property("enabled"):
            raise AssertionError("valid connector form should be submittable")
        client.failures["AddConnector"] = "add denied"
        start = len(client.calls)
        add_button = click_item(app, view, root, "connectorsAddAuthorizeButton", flick_name="connectorsFlick")
        if ("AddConnector", ("linear", "https://mcp.linear.app/mcp", "Linear issues")) not in client.calls[start:]:
            invoke_click(add_button)
            pump(app)
        assert_call(client, start, "AddConnector", ("linear", "https://mcp.linear.app/mcp", "Linear issues"))
        error_box = find_visual_item(root, "connectorsActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed connector add did not render an action error")
        if not connectors.add_open or connectors.add_name != "linear" or connectors.add_url != "https://mcp.linear.app/mcp":
            raise AssertionError("failed connector add did not preserve the form")
        del client.failures["AddConnector"]

        start = len(client.calls)
        add_button = click_item(app, view, root, "connectorsAddAuthorizeButton", flick_name="connectorsFlick")
        if ("AddConnector", ("linear", "https://mcp.linear.app/mcp", "Linear issues")) not in client.calls[start:]:
            invoke_click(add_button)
            pump(app)
        assert_call(client, start, "AddConnector", ("linear", "https://mcp.linear.app/mcp", "Linear issues"))
    finally:
        close_view(app, view)

    connectors = seeded_connectors_model(client)
    view, root = load_view(app, client, "ConnectorsView.qml", context={"connectorsModel": connectors}, root_props={"connectorsModel": connectors})
    try:
        server_card = find_visual_item(root, "connectorCard_server_github-local")
        if server_card is None or server_card.property("qaIconText") != "GL":
            raise AssertionError(f"server card did not derive a useful icon initial: {server_card.property('qaIconText') if server_card is not None else None!r}")

        server_remove = click_item(app, view, root, "connectorRemoveButton_server_github-local", flick_name="connectorsFlick")
        if not connectors.confirm_remove_server.get("github-local"):
            invoke_click(server_remove)
            pump(app)
        if not connectors.confirm_remove_server.get("github-local"):
            raise AssertionError("server remove did not reveal confirmation")
        server_confirm = find_visual_item(root, "connectorConfirmRemoveButton_server_github-local")
        server_cancel = find_visual_item(root, "connectorCancelRemoveButton_server_github-local")
        if server_confirm is None or server_cancel is None:
            raise AssertionError("server remove confirm/cancel buttons did not render")
        if not server_confirm.property("qaTextFits") or not server_cancel.property("qaTextFits"):
            raise AssertionError("server remove confirm/cancel text does not fit")
        invoke_click(server_cancel)
        pump(app)
        if connectors.confirm_remove_server.get("github-local"):
            raise AssertionError("server remove cancel did not reset confirmation")

        button = click_item(app, view, root, "connectorsAddServerButton", flick_name="connectorsFlick")
        if not connectors.srv_open:
            invoke_click(button)
            pump(app)
        save_button = find_visual_item(root, "connectorsSaveServerButton")
        if save_button is None:
            raise AssertionError("missing local server save button")
        if save_button.property("enabled"):
            raise AssertionError("empty local server form should not be submittable")
        server_name = find_visual_item(root, "connectorsServerNameInput")
        server_command = find_visual_item(root, "connectorsServerCommandInput")
        server_desc = find_visual_item(root, "connectorsServerDescInput")
        assert_app_text_field(server_name, "local server name")
        assert_app_text_field(server_command, "local server command")
        assert_app_text_field(server_desc, "local server description")
        assert_placeholder_color(server_name, "local server name")
        set_text(app, root, "connectorsServerNameInput", "github-local")
        if save_button.property("enabled"):
            raise AssertionError("local server form without command should not be submittable")
        set_text(app, root, "connectorsServerCommandInput", "uvx github-mcp-server")
        set_text(app, root, "connectorsServerDescInput", "GitHub MCP")
        env_value = "LOG_LEVEL=" + ("info" * 18)
        secret_value = "GITHUB_TOKEN=" + ("tok" * 24)
        set_text(app, root, "connectorsServerEnvInput", env_value)
        set_text(app, root, "connectorsServerSecretInput", secret_value)
        env_input = find_visual_item(root, "connectorsServerEnvInput")
        secret_input = find_visual_item(root, "connectorsServerSecretInput")
        if env_input is None or env_input.property("qaTextFits") is not True:
            raise AssertionError(
                "connector env input did not wrap long values cleanly: "
                f"content={env_input.property('qaContentWidth') if env_input else None} "
                f"available={env_input.property('qaTextAvailableWidth') if env_input else None}"
            )
        if secret_input is None or secret_input.property("qaTextFits") is not True:
            raise AssertionError(
                "connector secret input did not wrap long values cleanly: "
                f"content={secret_input.property('qaContentWidth') if secret_input else None} "
                f"available={secret_input.property('qaTextAvailableWidth') if secret_input else None}"
            )
        if not save_button.property("enabled"):
            raise AssertionError("valid local server form should be submittable")
        client.failures["SaveMCPServer"] = "save denied"
        start = len(client.calls)
        invoke_click(save_button)
        pump(app)
        save_calls = [call for call in client.calls[start:] if call[0] == "SaveMCPServer"]
        if not save_calls:
            raise AssertionError("missing SaveMCPServer call")
        server = save_calls[-1][1][0]
        if server.get("name") != "github-local" or server.get("command") != ["uvx", "github-mcp-server"]:
            raise AssertionError(f"unexpected server payload: {server}")
        if server.get("envPairs") != [env_value]:
            raise AssertionError(f"env payload lost: {server}")
        if server.get("secretEnvPairs") != [secret_value]:
            raise AssertionError(f"secret payload lost: {server}")
        error_box = find_visual_item(root, "connectorsServerActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed local server save did not render an action error")
        for field, name in (
            (find_visual_item(root, "connectorsServerNameInput"), "local server name after error"),
            (find_visual_item(root, "connectorsServerCommandInput"), "local server command after error"),
            (find_visual_item(root, "connectorsServerDescInput"), "local server description after error"),
        ):
            assert_app_text_field(field, name)
        if not connectors.srv_open or connectors.srv_name != "github-local" or connectors.srv_command != "uvx github-mcp-server":
            raise AssertionError("failed local server save did not preserve the form")
        del client.failures["SaveMCPServer"]

        start = len(client.calls)
        invoke_click(save_button)
        pump(app)
        if not [call for call in client.calls[start:] if call[0] == "SaveMCPServer"]:
            raise AssertionError("missing retry SaveMCPServer call")
    finally:
        close_view(app, view)


def check_memory(app, client):
    load_error_memory = seeded_memory_model(client)
    load_error_memory.current = None
    load_error_memory.load_error = "daemon offline"
    view, root = load_view(app, client, "MemoryView.qml", context={"memoryModel": load_error_memory}, root_props={"memoryModel": load_error_memory})
    try:
        load_error = find_visual_item(root, "memoryLoadError")
        load_error_text = find_visual_item(root, "memoryLoadErrorText")
        retry = find_visual_item(root, "memoryLoadErrorRetry")
        if load_error is None or not load_error.property("visible"):
            raise AssertionError("memory load error did not render")
        if load_error_text is None or "daemon offline" not in load_error_text.property("text"):
            raise AssertionError(f"memory load error text was wrong: {load_error_text.property('text') if load_error_text else None}")
        if retry is None or not retry.property("qaTextFits"):
            raise AssertionError("memory load error retry did not render cleanly")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "memoryLoadErrorRetry",
            client,
            start,
            "MemoryForScope",
            ("global",),
        )
    finally:
        close_view(app, view)

    stale_error_memory = seeded_memory_model(client)
    stale_error_memory.load_error = "daemon offline"
    view, root = load_view(app, client, "MemoryView.qml", context={"memoryModel": stale_error_memory}, root_props={"memoryModel": stale_error_memory})
    try:
        retry = assert_load_error_retry(app, view, root, "memoryRefreshErrorBanner", "memoryRefreshErrorText", "memoryRefreshErrorRetry")
        start = len(client.calls)
        invoke_click(retry)
        pump(app)
        assert_call(client, start, "MemoryForScope", ("global",))
    finally:
        close_view(app, view)

    memory = seeded_memory_model(client)
    view, root = load_view(app, client, "MemoryView.qml", context={"memoryModel": memory}, root_props={"memoryModel": memory})
    try:
        button = click_item(app, view, root, "memoryAddNoteButton")
        if not memory.composing:
            invoke_click(button)
            pump(app)
        compose = find_visual_item(root, "memoryComposeTextArea")
        if compose is None:
            raise AssertionError("missing memory compose input")
        assert_placeholder_color(compose, "memory compose")
        memory_text = "Mouse action memory proof " + ("longmemorytoken" * 16)
        set_text(app, root, "memoryComposeTextArea", memory_text)
        if compose.property("qaTextFits") is not True:
            raise AssertionError(
                "memory compose input did not wrap long text cleanly: "
                f"content={compose.property('qaContentWidth')} "
                f"available={compose.property('qaTextAvailableWidth')}"
            )
        client.failures["AppendMemory"] = "save denied"
        client.delays["AppendMemory"] = 45
        start = len(client.calls)
        save = click_item(app, view, root, "memorySaveNoteButton")
        if ("AppendMemory", ("global", memory_text)) not in client.calls[start:]:
            invoke_click(save)
            pump(app)
        assert_call(client, start, "AppendMemory", ("global", memory_text))
        if not memory.saving:
            raise AssertionError("memory note save did not expose a pending state")
        discard = find_visual_item(root, "memoryDiscardNoteButton")
        cancel_add = find_visual_item(root, "memoryAddNoteButton")
        if discard is None or cancel_add is None:
            raise AssertionError("memory pending save controls did not render")
        if discard.property("enabled") is not False or cancel_add.property("enabled") is not False:
            raise AssertionError("memory pending save allowed the composer to close")
        click_item(app, view, root, "memorySaveNoteButton")
        pump(app, 4)
        if client.calls[start:].count(("AppendMemory", ("global", memory_text))) != 1:
            raise AssertionError("memory pending save submitted a duplicate AppendMemory")
        QTest.qWait(70)
        pump(app, 12)
        error_box = find_visual_item(root, "memoryActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed memory note save did not render an action error")
        if "save denied" not in memory.action_error:
            raise AssertionError(f"unexpected memory action error: {memory.action_error}")
        if not memory.composing or memory.draft != memory_text:
            raise AssertionError("failed memory note save did not preserve the draft")
        del client.failures["AppendMemory"]
        del client.delays["AppendMemory"]

        start = len(client.calls)
        save = click_item(app, view, root, "memorySaveNoteButton")
        if ("AppendMemory", ("global", memory_text)) not in client.calls[start:]:
            invoke_click(save)
            pump(app)
        assert_call(client, start, "AppendMemory", ("global", memory_text))

        start = len(client.calls)
        move_ad_hoc = click_item(app, view, root, "memoryAdHocMoveButton_0", flick_name="memoryFlick")
        if not move_ad_hoc.property("qaTextFits"):
            raise AssertionError("manual memory move button text does not fit")
        if not memory.move_open or not memory.move_pending or memory.move_pending.get("idx") != 0:
            raise AssertionError(f"manual memory move did not open picker: {memory.move_pending}")
        move_target = click_item(app, view, root, "memoryMoveTargetButton_project_repo_eigen")
        if not move_target.property("qaTextFits"):
            raise AssertionError("memory move target button text does not fit")
        assert_call(client, start, "MoveMemoryNote", ("global", "project:/repo/eigen", "Manual note proof."))

        start = len(client.calls)
        move_note = click_item(app, view, root, "memoryNoteMoveButton_0", flick_name="memoryFlick")
        if not move_note.property("qaTextFits"):
            raise AssertionError("distilled memory move button text does not fit")
        if not memory.move_open or not memory.move_pending or memory.move_pending.get("text") != "Keep Qt proof focused.":
            raise AssertionError(f"distilled memory move did not open picker: {memory.move_pending}")
        click_item(app, view, root, "memoryMoveTargetButton_project_repo_eigen")
        assert_call(client, start, "MoveMemoryNote", ("global", "project:/repo/eigen", "Keep Qt proof focused."))

        remove_ad_hoc = click_item(app, view, root, "memoryAdHocRemoveButton_0", flick_name="memoryFlick")
        if not remove_ad_hoc.property("qaTextFits"):
            raise AssertionError("manual memory remove button text does not fit")
        if memory.confirm_remove_ad_hoc != 0:
            raise AssertionError("manual memory remove did not reveal confirmation")
        cancel_ad_hoc = find_visual_item(root, "memoryAdHocRemoveCancelButton_0")
        confirm_ad_hoc = find_visual_item(root, "memoryAdHocRemoveConfirmButton_0")
        if cancel_ad_hoc is None or confirm_ad_hoc is None:
            raise AssertionError("manual memory remove confirm/cancel buttons did not render")
        if not cancel_ad_hoc.property("qaTextFits") or not confirm_ad_hoc.property("qaTextFits"):
            raise AssertionError("manual memory remove confirm/cancel text does not fit")
        click_item(app, view, root, "memoryAdHocRemoveCancelButton_0", flick_name="memoryFlick")
        if memory.confirm_remove_ad_hoc != -1:
            raise AssertionError("manual memory remove cancel did not reset confirmation")
        click_item(app, view, root, "memoryAdHocRemoveButton_0", flick_name="memoryFlick")
        start = len(client.calls)
        click_item(app, view, root, "memoryAdHocRemoveConfirmButton_0", flick_name="memoryFlick")
        assert_call(client, start, "RemoveAdHocMemoryNote", ("global", 0))

        remove_note = click_item(app, view, root, "memoryNoteRemoveButton_0", flick_name="memoryFlick")
        if not remove_note.property("qaTextFits"):
            raise AssertionError("distilled memory remove button text does not fit")
        if memory.confirm_remove_note != 0:
            raise AssertionError("distilled memory remove did not reveal confirmation")
        cancel_note = find_visual_item(root, "memoryNoteRemoveCancelButton_0")
        confirm_note = find_visual_item(root, "memoryNoteRemoveConfirmButton_0")
        if cancel_note is None or confirm_note is None:
            raise AssertionError("distilled memory remove confirm/cancel buttons did not render")
        if not cancel_note.property("qaTextFits") or not confirm_note.property("qaTextFits"):
            raise AssertionError("distilled memory remove confirm/cancel text does not fit")
        click_item(app, view, root, "memoryNoteRemoveCancelButton_0", flick_name="memoryFlick")
        if memory.confirm_remove_note != -1:
            raise AssertionError("distilled memory remove cancel did not reset confirmation")
        click_item(app, view, root, "memoryNoteRemoveButton_0", flick_name="memoryFlick")
        client.delays["RemoveMemoryNote"] = 45
        start = len(client.calls)
        click_item(app, view, root, "memoryNoteRemoveConfirmButton_0", flick_name="memoryFlick")
        assert_call(client, start, "RemoveMemoryNote", ("global", 0))
        if memory.destructive_action_pending is not True:
            raise AssertionError("distilled memory remove did not expose a global destructive pending state")
        pending_remove = find_visual_item(root, "memoryNoteRemoveButton_0")
        if pending_remove is None:
            raise AssertionError("pending distilled memory remove did not keep a row control visible")
        if pending_remove.property("qaText") != "Removing…" or pending_remove.property("enabled") is not False:
            raise AssertionError(
                "pending distilled memory remove did not render as disabled Removing: "
                f"text={pending_remove.property('qaText')!r} enabled={pending_remove.property('enabled')}"
            )
        sibling_move = find_visual_item(root, "memoryAdHocMoveButton_0")
        sibling_remove = find_visual_item(root, "memoryAdHocRemoveButton_0")
        if sibling_move is None or sibling_remove is None:
            raise AssertionError("memory destructive sibling controls did not render")
        if sibling_move.property("enabled") is not False or sibling_remove.property("enabled") is not False:
            raise AssertionError("memory destructive sibling controls stayed enabled while remove was pending")
        click_item(app, view, root, "memoryAdHocMoveButton_0", flick_name="memoryFlick")
        if [call for call in client.calls[start:] if call[0] == "MoveMemoryNote"]:
            raise AssertionError(f"pending memory remove still allowed a move RPC: {client.calls[start:]}")
        QTest.qWait(70)
        pump(app, 12)
        if memory.destructive_action_pending is not False:
            raise AssertionError("destructive pending state did not clear after memory remove finished")
        del client.delays["RemoveMemoryNote"]

        edit_profile = click_item(app, view, root, "memoryEditProfileButton", flick_name="memoryFlick")
        if not memory.editing_profile:
            invoke_click(edit_profile)
            pump(app)
        profile_text = "Keyboard profile proof " + ("longprofiletoken" * 16)
        set_text(app, root, "memoryProfileTextArea", profile_text)
        profile_editor = find_visual_item(root, "memoryProfileTextArea")
        if profile_editor is None or profile_editor.property("qaTextFits") is not True:
            raise AssertionError(
                "memory profile editor did not wrap long text cleanly: "
                f"content={profile_editor.property('qaContentWidth') if profile_editor else None} "
                f"available={profile_editor.property('qaTextAvailableWidth') if profile_editor else None}"
            )
        start = len(client.calls)
        press_key_on_item(app, view, root, "memoryProfileTextArea", Qt.Key_Return, Qt.ControlModifier, flick_name="memoryFlick")
        assert_call(client, start, "WriteUserProfile", (profile_text,))

        memory.editing_profile = True
        memory.profile_draft = "cancel me"
        pump(app)
        press_key_on_item(app, view, root, "memoryProfileTextArea", Qt.Key_Escape, flick_name="memoryFlick")
        if memory.editing_profile or memory.profile_draft != "Existing profile":
            raise AssertionError("profile Escape did not cancel and restore the current profile")

        add_ban = click_item(app, view, root, "memoryAddBanButton", flick_name="memoryFlick")
        if not memory.adding_ban:
            invoke_click(add_ban)
            pump(app)
        ban_title_input = set_text(app, root, "memoryBanTitleInput", "No broad rewrites")
        if ban_title_input.property("qaIsAppTextField") is not True:
            raise AssertionError("memory ban title did not use shared AppTextField")
        if ban_title_input.property("qaTextFits") is not True:
            raise AssertionError(f"memory ban title text does not fit: {ban_title_input.property('qaText')!r}")
        ban_rule = "Do not expand a Qt follow-up without evidence " + ("longbanrule" * 18)
        set_text(app, root, "memoryBanRuleTextArea", ban_rule)
        ban_editor = find_visual_item(root, "memoryBanRuleTextArea")
        if ban_editor is None or ban_editor.property("qaTextFits") is not True:
            raise AssertionError(
                "memory ban editor did not wrap long text cleanly: "
                f"content={ban_editor.property('qaContentWidth') if ban_editor else None} "
                f"available={ban_editor.property('qaTextAvailableWidth') if ban_editor else None}"
            )
        start = len(client.calls)
        press_key_on_item(app, view, root, "memoryBanRuleTextArea", Qt.Key_Return, Qt.ControlModifier, flick_name="memoryFlick")
        assert_call(client, start, "AddBan", ("global", "No broad rewrites", ban_rule))

        memory.adding_ban = True
        memory.ban_title = "discard"
        memory.ban_rule = "discard"
        pump(app)
        press_key_on_item(app, view, root, "memoryBanTitleInput", Qt.Key_Escape, flick_name="memoryFlick")
        if memory.adding_ban or memory.ban_title or memory.ban_rule:
            raise AssertionError("ban Escape did not cancel and clear the form")
    finally:
        close_view(app, view)


def assert_load_error_retry(app, view, root, banner_name, text_name, retry_name, expected_text="daemon offline"):
    pump(app, 12)
    banner = find_visual_item(root, banner_name)
    error_text = find_visual_item(root, text_name)
    retry = find_visual_item(root, retry_name)
    if banner is None or banner.property("visible") is not True:
        raise AssertionError(f"{banner_name} did not render")
    if error_text is None or expected_text not in str(error_text.property("text")):
        raise AssertionError(f"{text_name} rendered wrong text: {error_text.property('text') if error_text else None!r}")
    if retry is None or retry.property("qaTextFits") is not True:
        raise AssertionError(f"{retry_name} did not render cleanly")
    return retry


def assert_refresh_error_preserves_item(app, view, root, banner_name, text_name, retry_name, initial_name, preserved_name, expected_text="daemon offline"):
    retry = assert_load_error_retry(app, view, root, banner_name, text_name, retry_name, expected_text)
    initial = find_visual_item(root, initial_name) if initial_name else None
    if initial is not None and initial.property("visible") is True:
        raise AssertionError(f"{banner_name} rendered by replacing stale content with {initial_name}")
    preserved = find_visual_item(root, preserved_name)
    if preserved is None or preserved.property("visible") is not True:
        raise AssertionError(f"{banner_name} hid preserved item {preserved_name}")
    if preserved.property("qaTextFits") is False:
        raise AssertionError(f"{banner_name} left preserved item {preserved_name} clipped")
    return retry


def seed_observe_summary(model):
    model._summary = {
        "available": True,
        "records": 4,
        "routes": {"routed": 2, "assessed": 1, "skipped": 1, "orchestrator": 0, "byModel": [], "byKind": [], "byDifficulty": [], "skipReasons": []},
        "tools": [{"name": "read_file", "calls": 4, "errors": 0, "durationMs": 80}],
        "models": [{"name": "gpt-5", "turns": 3, "inTokens": 1200, "outTokens": 320, "cacheReadTokens": 200, "cacheWriteTokens": 0, "durationMs": 1200}],
        "hooks": [],
        "errors": [],
        "byKind": [],
    }
    model.summary_changed.emit()


def seed_dreaming_current(model):
    model._scopes = [{"key": "global", "name": "Global", "dir": "", "noteCount": 2, "current": True}]
    model._scope_key = "global"
    model._current = {
        "scope": "global",
        "currentBytes": 1024,
        "rollouts": [{"index": 1, "text": "# Outcome: success\n\nFocused Qt proof.", "outcome": "success", "whenMs": 1783155600000}],
        "consolidations": [],
    }
    model.scopes_changed.emit()
    model.scope_key_changed.emit()
    model.current_changed.emit()
    model.summary_changed.emit()


def seed_plugins_inventory(model):
    model._plugins = [{"name": "agentsys", "marketplace": "core", "version": "5.1.0", "description": "Agent workflow tools", "enabled": True, "skills": ["audit-project"], "scanStatus": "clean", "scanCount": 0}]
    model._marketplaces = [{"name": "core", "source": "github.com/avifenesh/eigen-plugins", "owner": "Avi", "disabled": False}]
    model.plugins_changed.emit()
    model.marketplaces_changed.emit()
    model.summary_changed.emit()


def seed_machines_inventory(model):
    model._machines = [{"name": "codex-box", "ssh": "codex-box", "addr": "10.0.0.5", "dir": "/home/user/eigen", "model": "gpt-5", "perm": "gated", "saved": True, "detected": False}]
    model.machines_changed.emit()
    model.summary_changed.emit()


def seed_crons_inventory(model):
    model._crons = [{"name": "eigen-dream", "kind": "timer", "next": "today 19:30", "last": "today 17:00", "active": True, "enabled": True, "command": "eigen-dream.service", "unit": "eigen-dream.timer"}]
    model._timers = 1
    model._crontab = 0
    model._systemd_available = True
    model.crons_changed.emit()
    model.summary_changed.emit()


def seed_profile_summary(model):
    model._summary = {
        "available": True,
        "records": 4,
        "models": [{"name": "gpt-5", "turns": 3, "inTokens": 1200, "outTokens": 320, "cacheReadTokens": 200, "cacheWriteTokens": 0, "durationMs": 1200}],
        "errors": [],
    }
    model._memory = {"profile": "Existing profile", "profileLearned": "Prefers focused Qt proof."}
    model.summary_changed.emit()
    model.memory_changed.emit()


def seed_model_load_error(app, model, clear_state):
    clear_state()
    if hasattr(model, "_set_loading"):
        model._set_loading(False)
    elif hasattr(model, "loading"):
        model.loading = False
    if hasattr(model, "_set_load_error"):
        model._set_load_error("daemon offline")
    else:
        model.load_error = "daemon offline"
    pump(app, 12)


def check_utility_load_errors(app, client):
    observe = ObserveModel(client)
    view, root = load_view(app, client, "ObserveView.qml", context={"observeModel": observe}, root_props={"observeModel": observe})
    try:
        def clear_observe():
            observe._summary = {}
            observe.summary_changed.emit()

        seed_model_load_error(app, observe, clear_observe)
        assert_load_error_retry(app, view, root, "observeLoadError", "observeLoadErrorText", "observeLoadErrorRetry")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "observeLoadErrorRetry",
            client,
            start,
            "ObserveSummary",
            (5000,),
            flick_name="observeFlick",
        )

        seed_observe_summary(observe)
        observe._set_loading(False)
        observe._set_load_error("daemon offline")
        assert_refresh_error_preserves_item(
            app,
            view,
            root,
            "observeRefreshErrorBanner",
            "observeRefreshErrorText",
            "observeRefreshErrorRetry",
            "observeLoadError",
            "observeToolRow_read_file",
        )
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "observeRefreshErrorRetry",
            client,
            start,
            "ObserveSummary",
            (5000,),
            flick_name="observeFlick",
        )
    finally:
        close_view(app, view)

    dreaming = DreamingModel(client)
    view, root = load_view(app, client, "DreamingView.qml", context={"dreamingModel": dreaming}, root_props={"dreamingModel": dreaming})
    try:
        def clear_dreaming():
            dreaming._scope_key = "global"
            dreaming.scope_key_changed.emit()
            dreaming._current = {}
            dreaming.current_changed.emit()
            dreaming.summary_changed.emit()

        seed_model_load_error(app, dreaming, clear_dreaming)
        assert_load_error_retry(app, view, root, "dreamingLoadError", "dreamingLoadErrorText", "dreamingLoadErrorRetry")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "dreamingLoadErrorRetry",
            client,
            start,
            "ListMemoryScopes",
            (),
            flick_name="dreamingFlick",
        )
        assert_call(client, start, "DreamingForScope", ("global",))

        seed_dreaming_current(dreaming)
        dreaming._set_loading(False)
        dreaming._set_load_error("daemon offline")
        assert_refresh_error_preserves_item(
            app,
            view,
            root,
            "dreamingRefreshErrorBanner",
            "dreamingRefreshErrorText",
            "dreamingRefreshErrorRetry",
            "dreamingLoadError",
            "dreamingRolloutRow_1",
        )
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "dreamingRefreshErrorRetry",
            client,
            start,
            "ListMemoryScopes",
            (),
            flick_name="dreamingFlick",
        )
        assert_call(client, start, "DreamingForScope", ("global",))
    finally:
        close_view(app, view)

    plugins = PluginsModel(client)
    view, root = load_view(app, client, "PluginsView.qml", context={"pluginsModel": plugins}, root_props={"pluginsModel": plugins})
    try:
        def clear_plugins():
            plugins._plugins = []
            plugins._marketplaces = []
            plugins.plugins_changed.emit()
            plugins.marketplaces_changed.emit()
            plugins.summary_changed.emit()

        seed_model_load_error(app, plugins, clear_plugins)
        assert_load_error_retry(app, view, root, "pluginsLoadError", "pluginsLoadErrorText", "pluginsLoadErrorRetry")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "pluginsLoadErrorRetry",
            client,
            start,
            "Plugins",
            (),
            flick_name="pluginsFlick",
        )

        seed_plugins_inventory(plugins)
        plugins._set_loading(False)
        plugins._set_load_error("daemon offline")
        assert_refresh_error_preserves_item(
            app,
            view,
            root,
            "pluginsRefreshErrorBanner",
            "pluginsRefreshErrorText",
            "pluginsRefreshErrorRetry",
            "pluginsLoadError",
            "pluginsInstalledRow_agentsys",
        )
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "pluginsRefreshErrorRetry",
            client,
            start,
            "Plugins",
            (),
            flick_name="pluginsFlick",
        )
    finally:
        close_view(app, view)

    machines = MachinesModel(client)
    view, root = load_view(app, client, "MachinesView.qml", context={"machinesModel": machines}, root_props={"machinesModel": machines})
    try:
        def clear_machines():
            machines._machines = []
            machines.machines_changed.emit()
            machines.summary_changed.emit()

        seed_model_load_error(app, machines, clear_machines)
        assert_load_error_retry(app, view, root, "machinesLoadError", "machinesLoadErrorText", "machinesLoadErrorRetry")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "machinesLoadErrorRetry",
            client,
            start,
            "Machines",
            (),
            flick_name="machinesFlick",
        )

        seed_machines_inventory(machines)
        machines._set_loading(False)
        machines._set_load_error("daemon offline")
        assert_refresh_error_preserves_item(
            app,
            view,
            root,
            "machinesRefreshErrorBanner",
            "machinesRefreshErrorText",
            "machinesRefreshErrorRetry",
            "machinesLoadError",
            "machinesCard_codex_box",
        )
        start = len(client.calls)
        quick_install = click_item_until_call(
            app,
            view,
            root,
            "machinesQuickInstallButton_codex_box",
            client,
            start,
            "InstallRemote",
            ("codex-box", True),
            flick_name="machinesFlick",
        )
        if quick_install.property("qaTextFits") is not True:
            raise AssertionError("machines quick install button text was clipped")
        if machines.selected_machine.get("ssh") != "codex-box":
            raise AssertionError("machines quick install did not select its host")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "machinesRefreshErrorRetry",
            client,
            start,
            "Machines",
            (),
            flick_name="machinesFlick",
        )

        seed_machines_inventory(machines)
        machines._set_loading(False)
        machines._set_load_error("")
        machines.select_machine("codex-box")
        install_button = find_visual_item(root, "machinesInstallButton")
        credentials = find_visual_item(root, "machinesCredentialsSwitch")
        if install_button is None or credentials is None:
            raise AssertionError("machines view did not render install controls for the selected host")
        if install_button.property("qaTextFits") is not True:
            raise AssertionError("machines install button text was clipped")
        if credentials.property("qaTextFits") is not True or credentials.property("qaAccessibleName") != "Copy local daemon credentials":
            raise AssertionError("machines credential switch did not expose a usable accessible control")

        machines._set_remote_error("no eigen on codex-box")
        remote_install = find_visual_item(root, "machinesRemoteInstallButton")
        if remote_install is None or remote_install.property("qaTextFits") is not True:
            raise AssertionError("machines remote error did not expose a clean install action")
    finally:
        close_view(app, view)

    crons = CronsModel(client)
    view, root = load_view(app, client, "CronsView.qml", context={"cronsModel": crons}, root_props={"cronsModel": crons})
    try:
        def clear_crons():
            crons._crons = []
            crons._timers = 0
            crons._crontab = 0
            crons.crons_changed.emit()
            crons.summary_changed.emit()

        seed_model_load_error(app, crons, clear_crons)
        assert_load_error_retry(app, view, root, "cronsLoadError", "cronsLoadErrorText", "cronsLoadErrorRetry")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "cronsLoadErrorRetry",
            client,
            start,
            "Crons",
            (),
            flick_name="cronsFlick",
        )

        seed_crons_inventory(crons)
        crons._set_loading(False)
        crons._set_load_error("daemon offline")
        assert_refresh_error_preserves_item(
            app,
            view,
            root,
            "cronsRefreshErrorBanner",
            "cronsRefreshErrorText",
            "cronsRefreshErrorRetry",
            "cronsLoadError",
            "cronsTimerRow_eigen_dream_timer",
        )
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "cronsRefreshErrorRetry",
            client,
            start,
            "Crons",
            (),
            flick_name="cronsFlick",
        )
    finally:
        close_view(app, view)

    profile = ProfileModel(client)
    view, root = load_view(app, client, "ProfileView.qml", context={"profileModel": profile, "statsData": {"sessions": 7}}, root_props={"profileModel": profile, "statsData": {"sessions": 7}})
    try:
        seed_profile_summary(profile)
        profile._set_summary_loading(False)
        profile._set_summary_error("daemon offline")
        assert_refresh_error_preserves_item(
            app,
            view,
            root,
            "profileSummaryRefreshErrorBanner",
            "profileSummaryRefreshErrorText",
            "profileSummaryRefreshErrorRetry",
            "",
            "profileModelRow_gpt_5",
        )
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "profileSummaryRefreshErrorRetry",
            client,
            start,
            "ObserveSummary",
            (5000,),
            flick_name="profileFlick",
        )
        assert_call(client, start, "ObserveSummary", (5000,))
        assert_call(client, start, "MemoryForScope", ("global",))
    finally:
        close_view(app, view)

    memory = MemoryModel(client)
    memory.scopes = [{"key": "global", "name": "Global", "dir": "", "noteCount": 0, "current": True}]
    memory.scope_key = "global"
    view, root = load_view(app, client, "MemoryView.qml", context={"memoryModel": memory}, root_props={"memoryModel": memory})
    try:
        def clear_memory():
            memory.current = None

        seed_model_load_error(app, memory, clear_memory)
        assert_load_error_retry(app, view, root, "memoryLoadError", "memoryLoadErrorText", "memoryLoadErrorRetry")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "memoryLoadErrorRetry",
            client,
            start,
            "MemoryForScope",
            ("global",),
        )
    finally:
        close_view(app, view)


def check_notes(app, client):
    status_error_notes = NotesController(client)
    status_error_notes.status_error = "daemon offline"
    view, root = load_view(app, client, "NotesView.qml", context={"notesController": status_error_notes}, root_props={"notesController": status_error_notes})
    try:
        assert_load_error_retry(app, view, root, "notesStatusLoadError", "notesStatusLoadErrorText", "notesStatusLoadErrorRetry")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "notesStatusLoadErrorRetry",
            client,
            start,
            "ObsidianStatus",
            (),
        )
    finally:
        close_view(app, view)

    status_refresh_notes = NotesController(client)
    status_refresh_notes.status = {"available": True, "vault": "/home/user/notes"}
    status_refresh_notes.status_error = "daemon offline"
    view, root = load_view(app, client, "NotesView.qml", context={"notesController": status_refresh_notes}, root_props={"notesController": status_refresh_notes})
    try:
        initial_error = find_visual_item(root, "notesStatusLoadError")
        refresh_error = find_visual_item(root, "notesStatusRefreshErrorBanner")
        refresh_text = find_visual_item(root, "notesStatusRefreshErrorText")
        refresh_retry = find_visual_item(root, "notesStatusRefreshErrorRetry")
        new_button = find_visual_item(root, "notesNewButton")
        if initial_error is not None and initial_error.property("visible"):
            raise AssertionError("notes status refresh error replaced stale notes with the initial error state")
        if refresh_error is None or not refresh_error.property("visible"):
            raise AssertionError("notes status refresh error banner did not render")
        if refresh_text is None or "daemon offline" not in refresh_text.property("text"):
            raise AssertionError(f"notes status refresh error text was wrong: {refresh_text.property('text') if refresh_text else None}")
        if refresh_retry is None or not refresh_retry.property("qaTextFits"):
            raise AssertionError("notes status refresh retry did not render cleanly")
        if new_button is None or new_button.property("visible") is not True:
            raise AssertionError("notes status refresh error hid the usable notes controls")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "notesStatusRefreshErrorRetry",
            client,
            start,
            "ObsidianStatus",
            (),
        )
    finally:
        close_view(app, view)

    list_error_notes = NotesController(client)
    list_error_notes.status = {"available": True, "vault": "/home/user/notes"}
    list_error_notes.notes_model._set_error("daemon offline")
    view, root = load_view(app, client, "NotesView.qml", context={"notesController": list_error_notes}, root_props={"notesController": list_error_notes})
    try:
        assert_load_error_retry(app, view, root, "notesLoadError", "notesLoadErrorText", "notesLoadErrorRetry")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "notesLoadErrorRetry",
            client,
            start,
            "ObsidianNotes",
            ("",),
        )
    finally:
        close_view(app, view)

    stale_error_notes = seeded_notes_controller(client)
    stale_error_notes.notes_model._set_error("daemon offline")
    view, root = load_view(app, client, "NotesView.qml", context={"notesController": stale_error_notes}, root_props={"notesController": stale_error_notes})
    try:
        retry = assert_load_error_retry(app, view, root, "notesRefreshErrorBanner", "notesRefreshErrorText", "notesRefreshErrorRetry")
        start = len(client.calls)
        invoke_click(retry)
        pump(app)
        assert_call(client, start, "ObsidianNotes", ("",))
    finally:
        close_view(app, view)

    notes = seeded_notes_controller(client)
    view, root = load_view(app, client, "NotesView.qml", context={"notesController": notes}, root_props={"notesController": notes})
    try:
        start = len(client.calls)
        row = click_item(app, view, root, "notesRow_0")
        if ("ObsidianRead", ("Inbox/Existing.md",)) not in client.calls[start:]:
            invoke_click(row)
            pump(app)
        assert_call(client, start, "ObsidianRead", ("Inbox/Existing.md",))
        markdown_body = find_visual_item(root, "notesMarkdownBody")
        if markdown_body is None or markdown_body.property("visible") is not True:
            raise AssertionError("note read mode did not render the markdown body")
        markdown_blocks = markdown_body.property("blocks") or []
        if len(markdown_blocks) < 2:
            raise AssertionError(f"note markdown body did not parse into blocks: {markdown_blocks}")

        actions = find_visual_item(root, "notesHeaderActions")
        if actions is None or float(actions.property("width") or 0) <= 0:
            raise AssertionError("notes header action row did not expose stable geometry")

        edit = click_item(app, view, root, "notesEditButton")
        if not edit.property("qaTextFits"):
            raise AssertionError("notes edit button text does not fit")
        if not notes.editing:
            invoke_click(edit)
            pump(app)
        note_text = "# Existing\n\nEdited body " + ("longnotetoken" * 18)
        set_text(app, root, "notesEditorTextArea", note_text)
        editor = find_visual_item(root, "notesEditorTextArea")
        if editor is None or editor.property("qaTextFits") is not True:
            raise AssertionError(
                "notes editor did not wrap long text cleanly: "
                f"content={editor.property('qaContentWidth') if editor else None} "
                f"available={editor.property('qaTextAvailableWidth') if editor else None}"
            )

        client.failures["ObsidianWrite"] = "save denied"
        client.delays["ObsidianWrite"] = 45
        start = len(client.calls)
        save = click_item(app, view, root, "notesSaveEditButton")
        if ("ObsidianWrite", ("Inbox/Existing.md", note_text, False)) not in client.calls[start:]:
            invoke_click(save)
            pump(app)
        if not notes.saving:
            raise AssertionError("note save did not expose a pending state")
        pending_save = find_visual_item(root, "notesSaveEditButton")
        cancel = find_visual_item(root, "notesCancelEditButton")
        if pending_save is None or pending_save.property("qaText") != "Saving…" or not pending_save.property("qaTextFits"):
            raise AssertionError(
                "notes pending save button did not render cleanly: "
                f"text={pending_save.property('qaText') if pending_save else None!r}, "
                f"fits={pending_save.property('qaTextFits') if pending_save else None}, "
                f"width={pending_save.property('width') if pending_save else None}"
            )
        if cancel is None or not cancel.property("qaTextFits"):
            raise AssertionError("notes cancel edit button text does not fit")
        if cancel.property("enabled") is not False:
            raise AssertionError("notes cancel edit button stayed enabled while save was pending")
        QTest.qWait(70)
        pump(app, 12)
        assert_call(client, start, "ObsidianWrite", ("Inbox/Existing.md", note_text, False))
        error_box = find_visual_item(root, "notesActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed note save did not render an action error")
        if not notes.editing or notes.draft != note_text:
            raise AssertionError("failed note save did not keep the editor draft alive")
        del client.failures["ObsidianWrite"]
        del client.delays["ObsidianWrite"]

        start = len(client.calls)
        save = click_item(app, view, root, "notesSaveEditButton")
        if ("ObsidianWrite", ("Inbox/Existing.md", note_text, False)) not in client.calls[start:]:
            invoke_click(save)
            pump(app)
        assert_call(client, start, "ObsidianWrite", ("Inbox/Existing.md", note_text, False))
        if notes.editing or notes.content != note_text:
            raise AssertionError("successful note save did not leave read mode with updated content")

        button = click_item(app, view, root, "notesNewButton")
        if not notes.creating:
            invoke_click(button)
            pump(app)
        create_name = find_visual_item(root, "notesCreateNameInput")
        if create_name is None:
            raise AssertionError("missing notes create input")
        assert_placeholder_color(create_name, "notes create")
        set_text(app, root, "notesCreateNameInput", "Inbox/Mouse.md")

        client.failures["ObsidianWrite"] = "create denied"
        start = len(client.calls)
        create = click_item(app, view, root, "notesCreateButton")
        if ("ObsidianWrite", ("Inbox/Mouse.md", "# Inbox/Mouse\n\n", False)) not in client.calls[start:]:
            invoke_click(create)
            pump(app)
        assert_call(client, start, "ObsidianWrite", ("Inbox/Mouse.md", "# Inbox/Mouse\n\n", False))
        error_box = find_visual_item(root, "notesActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed note create did not render an action error")
        if not notes.creating or notes.new_name != "Inbox/Mouse.md":
            raise AssertionError("failed note create did not preserve the create form")
        del client.failures["ObsidianWrite"]

        start = len(client.calls)
        create = click_item(app, view, root, "notesCreateButton")
        if ("ObsidianWrite", ("Inbox/Mouse.md", "# Inbox/Mouse\n\n", False)) not in client.calls[start:]:
            invoke_click(create)
            pump(app)
        assert_call(client, start, "ObsidianWrite", ("Inbox/Mouse.md", "# Inbox/Mouse\n\n", False))
    finally:
        close_view(app, view)


def check_reviewers(app, client):
    load_error_reviewers = ReviewersModel(client)
    view, root = load_view(app, client, "ReviewersView.qml", context={"reviewersModel": load_error_reviewers}, root_props={"reviewersModel": load_error_reviewers})
    try:
        if load_error_reviewers.rowCount() > 0:
            load_error_reviewers.beginResetModel()
            load_error_reviewers._reviewers = []
            load_error_reviewers.endResetModel()
        load_error_reviewers._available = False
        load_error_reviewers._count = 0
        load_error_reviewers._paused = 0
        load_error_reviewers.status_changed.emit()
        load_error_reviewers._set_loading(False)
        load_error_reviewers._set_load_error("daemon offline")
        assert_load_error_retry(app, view, root, "reviewersLoadError", "reviewersLoadErrorText", "reviewersLoadErrorRetry")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "reviewersLoadErrorRetry",
            client,
            start,
            "RevutoStatus",
            (),
        )
    finally:
        close_view(app, view)

    reviewers = seeded_reviewers_model(client)
    view, root = load_view(app, client, "ReviewersView.qml", context={"reviewersModel": reviewers}, root_props={"reviewersModel": reviewers})
    try:
        if not reviewers._poll_timer.isActive():
            raise AssertionError("reviewers view did not activate route-scoped polling while visible")

        reviewers_list = find_visual_item(root, "reviewersList")
        row = find_visual_item(root, "reviewerRow_avifenesh_eigen")
        if reviewers_list is None or row is None:
            raise AssertionError("reviewers list did not expose its seeded row")
        row_width = float(row.property("width") or 0)
        row_height = float(row.property("height") or 0)
        list_width = float(reviewers_list.property("width") or 0)
        if row_width <= 0 or row_height < 55:
            raise AssertionError(f"reviewers row has unstable geometry: {row_width}x{row_height}")
        if abs(row_width - list_width) > 1.0:
            raise AssertionError(f"reviewers row no longer fills list width: row={row_width}, list={list_width}")

        refresh = find_visual_item(root, "reviewersRefreshButton")
        if refresh is None or refresh.property("qaTextFits") is not True:
            raise AssertionError("reviewers refresh button text does not fit")

        reviewers._set_load_error("daemon offline")
        retry = assert_load_error_retry(app, view, root, "reviewersRefreshErrorBanner", "reviewersRefreshErrorText", "reviewersRefreshErrorRetry")
        start = len(client.calls)
        invoke_click(retry)
        pump(app)
        assert_call(client, start, "RevutoStatus", ())

        start = len(client.calls)
        button = click_item(app, view, root, "reviewerReviewButton_avifenesh_eigen")
        if button.property("qaTextFits") is not True:
            raise AssertionError("reviewers review button text does not fit")
        if ("RevutoTrigger", ("avifenesh/eigen", "review")) not in client.calls[start:]:
            invoke_click(button)
            pump(app)
        assert_call(client, start, "RevutoTrigger", ("avifenesh/eigen", "review"))

        client.failures["RevutoTrigger"] = "revuto unavailable"
        start = len(client.calls)
        learn = click_item(app, view, root, "reviewerLearnButton_avifenesh_eigen")
        if learn.property("qaTextFits") is not True:
            raise AssertionError("reviewers learn button text does not fit")
        if ("RevutoTrigger", ("avifenesh/eigen", "learn")) not in client.calls[start:]:
            invoke_click(learn)
            pump(app)
        assert_call(client, start, "RevutoTrigger", ("avifenesh/eigen", "learn"))
        error_box = find_visual_item(root, "reviewersActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("reviewer trigger failure did not render an error")
        if "revuto unavailable" not in root.property("actionError"):
            raise AssertionError(f"unexpected reviewers error: {root.property('actionError')}")
        dismiss = find_visual_item(root, "reviewersDismissErrorButton")
        if dismiss is None or dismiss.property("qaTextFits") is not True:
            raise AssertionError("reviewers dismiss error button text does not fit")
        del client.failures["RevutoTrigger"]

        client.revuto_paused = False
        reviewers._set_reviewer_paused("avifenesh/eigen", False)
        pump(app, 4)

        client.delays["RevutoSetPaused"] = 45
        start = len(client.calls)
        pause = click_item(app, view, root, "reviewerPauseButton_avifenesh_eigen")
        if pause.property("qaTextFits") is not True:
            raise AssertionError("reviewers pause button text does not fit")
        if ("RevutoSetPaused", ("avifenesh/eigen", True)) not in client.calls[start:]:
            invoke_click(pause)
            pump(app)
        assert_call(client, start, "RevutoSetPaused", ("avifenesh/eigen", True))
        if qml_map_value(root.property("busyAction"), "avifenesh/eigen") != "pause":
            raise AssertionError(f"pause did not expose pending action: {root.property('busyAction')}")
        for name in ("reviewerReviewButton_avifenesh_eigen", "reviewerLearnButton_avifenesh_eigen", "reviewerPauseButton_avifenesh_eigen"):
            item = find_visual_item(root, name)
            if item is None or item.property("enabled") is not False:
                raise AssertionError(f"{name} stayed enabled while pause was pending")
        click_item(app, view, root, "reviewerPauseButton_avifenesh_eigen")
        pump(app, 4)
        if client.calls[start:].count(("RevutoSetPaused", ("avifenesh/eigen", True))) != 1:
            raise AssertionError("disabled pause button submitted a duplicate RevutoSetPaused")
        QTest.qWait(70)
        pump(app, 12)
        resume = find_visual_item(root, "reviewerPauseButton_avifenesh_eigen")
        if resume is None or resume.property("text") != "Resume" or resume.property("enabled") is not True:
            raise AssertionError("pause success did not update the row to Resume")

        client.failures["RevutoSetPaused"] = "pause denied"
        start = len(client.calls)
        resume = click_item(app, view, root, "reviewerPauseButton_avifenesh_eigen")
        if resume.property("qaTextFits") is not True:
            raise AssertionError("reviewers resume button text does not fit")
        if ("RevutoSetPaused", ("avifenesh/eigen", False)) not in client.calls[start:]:
            invoke_click(resume)
            pump(app)
        assert_call(client, start, "RevutoSetPaused", ("avifenesh/eigen", False))
        if qml_map_value(root.property("busyAction"), "avifenesh/eigen") != "resume":
            raise AssertionError(f"resume did not expose pending action: {root.property('busyAction')}")
        QTest.qWait(70)
        pump(app, 12)
        error_box = find_visual_item(root, "reviewersActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("reviewer pause failure did not render an error")
        if "pause denied" not in root.property("actionError"):
            raise AssertionError(f"unexpected reviewers pause error: {root.property('actionError')}")
        resume = find_visual_item(root, "reviewerPauseButton_avifenesh_eigen")
        if resume is None or resume.property("text") != "Resume" or resume.property("enabled") is not True:
            raise AssertionError("pause failure did not preserve the previous paused state")
        del client.failures["RevutoSetPaused"]
        del client.delays["RevutoSetPaused"]
    finally:
        close_view(app, view)
        if reviewers._poll_timer.isActive():
            raise AssertionError("reviewers view did not stop route-scoped polling after close")


def check_skills(app, client):
    load_error_skills = SkillsModel(client)
    load_error_proposals = ProposalsModel(client)
    view, root = load_view(app, client, "SkillsView.qml", context={"skillsModel": load_error_skills, "proposalsModel": load_error_proposals}, root_props={"skillsModel": load_error_skills, "proposalsModel": load_error_proposals})
    try:
        if load_error_skills.rowCount() > 0:
            load_error_skills.beginResetModel()
            load_error_skills._skills = []
            load_error_skills.endResetModel()
        if load_error_proposals.rowCount() > 0:
            load_error_proposals.beginResetModel()
            load_error_proposals._proposals = []
            load_error_proposals.endResetModel()
        load_error_skills._set_load_error("daemon offline")
        load_error_proposals._set_load_error("daemon offline")
        assert_load_error_retry(app, view, root, "skillsLoadError", "skillsLoadErrorText", "skillsLoadErrorRetry")
        start = len(client.calls)
        click_item_until_call(
            app,
            view,
            root,
            "skillsLoadErrorRetry",
            client,
            start,
            "Skills",
            (),
            flick_name="skillsFlick",
        )
        if client.calls[start:].count(("Skills", ())) < 2:
            raise AssertionError(f"skills load retry did not refresh both models: {client.calls[start:]}")
    finally:
        close_view(app, view)

    skills, proposals = seeded_skill_models(client)
    offline_view, offline_root = load_view(app, client, "SkillsView.qml", context={"skillsModel": skills, "proposalsModel": proposals}, root_props={"skillsModel": skills, "proposalsModel": proposals})
    try:
        start = len(client.calls)
        project_card = click_item(app, offline_view, offline_root, "skillCard_project-commands", flick_name="skillsFlick")
        if [call for call in client.calls[start:] if call[0] == "SkillBody"]:
            raise AssertionError(f"offline skill preview still called SkillBody: {client.calls[start:]}")
        if not offline_root.property("qaPreviewOpen"):
            invoke_click(project_card)
            pump(app)
        if not offline_root.property("qaPreviewOpen") or offline_root.property("bodyLoading") or offline_root.property("bodyToken") != 0:
            raise AssertionError("offline skill preview did not settle into an error state")
        error_box = find_visual_item(offline_root, "skillsActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("offline skill preview did not render a visible error")
        if "RPC client is unavailable" not in offline_root.property("actionError"):
            raise AssertionError(f"offline skill preview error was wrong: {offline_root.property('actionError')}")
        close = click_item(app, offline_view, offline_root, "skillPreviewCloseButton")
        if offline_root.property("qaPreviewOpen"):
            invoke_click(close)
            pump(app)
        dismiss = click_item(app, offline_view, offline_root, "skillsDismissErrorButton")
        if offline_root.property("actionError"):
            invoke_click(dismiss)
            pump(app)
        if offline_root.property("actionError"):
            raise AssertionError("offline skill preview error did not dismiss")
    finally:
        close_view(app, offline_view)

    skills, proposals = seeded_skill_models(client)
    view, root = load_view(app, client, "SkillsView.qml", context={"skillsModel": skills, "proposalsModel": proposals}, root_props={"skillsModel": skills, "proposalsModel": proposals, "rpcClient": client})
    try:
        if not skills._poll_timer.isActive() or not proposals._poll_timer.isActive():
            raise AssertionError("skills view did not activate route-scoped polling while visible")
        proposal_strip = find_visual_item(root, "skillsProposalReviewStrip")
        proposal_scroller = find_visual_item(root, "skillsProposalScroller")
        if proposal_strip is None or proposal_scroller is None:
            raise AssertionError("skills proposal review strip was not exposed")
        if root.property("qaProposalCount") != 1:
            raise AssertionError(f"unexpected seeded proposal count: {root.property('qaProposalCount')}")
        if float(root.property("qaProposalStripHeight") or 0) > 180:
            raise AssertionError(f"single proposal strip is too tall: {root.property('qaProposalStripHeight')}")
        if float(root.property("qaProposalScrollerHeight") or 0) > 130:
            raise AssertionError(f"single proposal scroller is too tall: {root.property('qaProposalScrollerHeight')}")

        skills._set_load_error("daemon offline")
        retry = assert_load_error_retry(app, view, root, "skillsRefreshErrorBanner", "skillsRefreshErrorText", "skillsRefreshErrorRetry")
        start = len(client.calls)
        invoke_click(retry)
        pump(app)
        if client.calls[start:].count(("Skills", ())) < 2:
            raise AssertionError(f"skills refresh retry did not refresh both models: {client.calls[start:]}")

        client.delays["SkillBody"] = 70
        start = len(client.calls)
        project_card = click_item(app, view, root, "skillCard_project-commands", flick_name="skillsFlick")
        if ("SkillBody", ("project-commands",)) not in client.calls[start:]:
            invoke_click(project_card)
            pump(app)
        assert_call(client, start, "SkillBody", ("project-commands",))
        if not root.property("qaPreviewOpen") or not root.property("bodyLoading"):
            raise AssertionError("project skill preview did not open in loading state")
        close = click_item(app, view, root, "skillPreviewCloseButton")
        if root.property("qaPreviewOpen"):
            invoke_click(close)
            pump(app)
        if root.property("qaPreviewOpen") or root.property("bodyLoading") or root.property("bodyToken") != 0:
            raise AssertionError("skill preview close did not clear pending body state")
        QTest.qWait(90)
        pump(app, 12)
        if root.property("body") != "":
            raise AssertionError("late SkillBody reply mutated a closed preview")
        del client.delays["SkillBody"]

        start = len(client.calls)
        project_card = click_item(app, view, root, "skillCard_project-commands", flick_name="skillsFlick")
        if ("SkillBody", ("project-commands",)) not in client.calls[start:]:
            invoke_click(project_card)
            pump(app)
        assert_call(client, start, "SkillBody", ("project-commands",))
        QTest.qWait(20)
        pump(app, 8)
        if not root.property("qaPreviewOpen") or "Project-local skill body" not in root.property("body"):
            raise AssertionError("project skill preview did not open with loaded body")
        markdown_body = find_visual_item(root, "skillPreviewMarkdownBody")
        if markdown_body is None or not markdown_body.property("visible"):
            raise AssertionError("project skill preview did not render markdown blocks")
        if root.property("qaBodyBlockCount") < 2:
            raise AssertionError(f"project skill preview did not parse markdown body: {root.property('qaBodyBlockCount')}")
        close = click_item(app, view, root, "skillPreviewCloseButton")
        if root.property("qaPreviewOpen"):
            invoke_click(close)
            pump(app)
        if root.property("qaPreviewOpen"):
            raise AssertionError("skill preview close button did not close the sheet")

        client.delays["RemoveSkill"] = 70
        start = len(client.calls)
        user_card = click_item(app, view, root, "skillCard_frontend-design", flick_name="skillsFlick")
        if ("SkillBody", ("frontend-design",)) not in client.calls[start:]:
            invoke_click(user_card)
            pump(app)
        assert_call(client, start, "SkillBody", ("frontend-design",))
        QTest.qWait(20)
        pump(app, 8)
        remove = click_item(app, view, root, "skillRemoveButton_frontend-design")
        if not remove.property("qaTextFits"):
            raise AssertionError("skill remove button text does not fit")
        if not root.property("confirmRemove"):
            invoke_click(remove)
            pump(app)
        confirm = find_visual_item(root, "skillRemoveConfirmButton_frontend-design")
        if confirm is None:
            raise AssertionError("missing skill remove confirm button")
        cancel = find_visual_item(root, "skillRemoveButton_frontend-design")
        if cancel is None:
            raise AssertionError("missing skill remove cancel button")
        if not confirm.property("qaTextFits") or not cancel.property("qaTextFits"):
            raise AssertionError("skill remove confirm/cancel text does not fit")
        start = len(client.calls)
        invoke_click(confirm)
        invoke_click(confirm)
        pump(app, 4)
        if client.calls[start:].count(("RemoveSkill", ("frontend-design",))) != 1:
            raise AssertionError("skill remove submitted duplicate calls while pending")
        if not root.property("removing"):
            raise AssertionError("skill remove did not expose pending state")
        pending_confirm = find_visual_item(root, "skillRemoveConfirmButton_frontend-design")
        if pending_confirm is None:
            raise AssertionError("missing pending skill remove button")
        if pending_confirm.property("qaText") != "Removing…":
            raise AssertionError(f"skill remove pending button had wrong text: {pending_confirm.property('qaText')!r}")
        if pending_confirm.property("qaTextFits") is not True:
            raise AssertionError("skill remove pending button text does not fit")
        close = find_visual_item(root, "skillPreviewCloseButton")
        if close is None:
            raise AssertionError("missing skill preview close button while removing")
        if close.property("enabled") is not False:
            raise AssertionError("skill preview close button stayed enabled while remove was pending")
        if not close.property("qaTextFits"):
            raise AssertionError("skill preview close button text does not fit")
        invoke_click(close)
        pump(app, 4)
        if not root.property("qaPreviewOpen"):
            raise AssertionError("skill preview closed while remove was pending")
        QTest.qWait(90)
        pump(app, 12)
        if root.property("qaPreviewOpen") or root.property("removing"):
            raise AssertionError("successful skill remove did not close the preview and clear pending state")
        del client.delays["RemoveSkill"]

        start = len(client.calls)
        accept = click_item(app, view, root, "proposalAcceptButton_qt-qa")
        if ("AcceptSkill", ("qt-qa",)) not in client.calls[start:]:
            invoke_click(accept)
            pump(app)
        assert_call(client, start, "AcceptSkill", ("qt-qa",))
        if proposals.rowCount() != 0:
            raise AssertionError("successful proposal accept did not remove the proposal")

        proposals._on_skills_result({"result": seeded_skill_snapshot()})
        pump(app, 12)
        client.failures["RejectSkill"] = "reject denied"
        start = len(client.calls)
        reject = click_item(app, view, root, "proposalRejectButton_qt-qa")
        if ("RejectSkill", ("qt-qa",)) not in client.calls[start:]:
            invoke_click(reject)
            pump(app)
        assert_call(client, start, "RejectSkill", ("qt-qa",))
        error_box = find_visual_item(root, "skillsActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed proposal reject did not render an error")
        if "reject denied" not in root.property("actionError"):
            raise AssertionError(f"unexpected skills proposal error: {root.property('actionError')}")
        if proposals.rowCount() != 1:
            raise AssertionError("failed proposal reject removed the proposal")
        if not reject.property("enabled"):
            raise AssertionError("proposal reject button stayed disabled after failure")
        del client.failures["RejectSkill"]

        path_mode = find_visual_item(root, "skillsAddMode_path")
        github_mode = find_visual_item(root, "skillsAddMode_github")
        if path_mode is None or github_mode is None:
            raise AssertionError("missing skills add-mode segmented buttons")
        if root.property("addMode") != "path" or not path_mode.property("selected") or github_mode.property("selected"):
            raise AssertionError("skills add-mode did not start on the path segment")
        input_item = find_visual_item(root, "skillsAddInput")
        if input_item is None:
            raise AssertionError("missing skills add input")
        assert_placeholder_color(input_item, "skills add")
        set_text(app, root, "skillsAddInput", "/tmp/missing-skill")
        client.failures["InstallSkillFromPath"] = "install denied"
        start = len(client.calls)
        add = click_item(app, view, root, "skillsAddButton")
        if ("InstallSkillFromPath", ("/tmp/missing-skill",)) not in client.calls[start:]:
            invoke_click(add)
            pump(app)
        assert_call(client, start, "InstallSkillFromPath", ("/tmp/missing-skill",))
        error_box = find_visual_item(root, "skillsActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed skill install did not render an error")
        if "install denied" not in root.property("actionError"):
            raise AssertionError(f"unexpected skills install error: {root.property('actionError')}")
        input_item = find_visual_item(root, "skillsAddInput")
        if input_item is None or input_item.property("text") != "/tmp/missing-skill":
            raise AssertionError("failed skill install did not preserve the add input")
        del client.failures["InstallSkillFromPath"]

        clear = click_item(app, view, root, "skillsAddClearButton")
        if root.property("addInput") != "" or root.property("actionError") != "":
            invoke_click(clear)
            pump(app)
        if root.property("addInput") != "" or root.property("actionError") != "":
            raise AssertionError("skills clear button did not reset add input and error")

        github_mode = click_item(app, view, root, "skillsAddMode_github")
        if root.property("addMode") != "github" or not github_mode.property("selected") or path_mode.property("selected"):
            raise AssertionError("skills GitHub segment did not activate")
        github_mode.setProperty("qaForceKeyboardFocus", True)
        pump(app)
        if not github_mode.property("qaVisualFocus"):
            raise AssertionError("skills GitHub segment did not expose keyboard focus")
        set_text(app, root, "skillsAddInput", "owner/repo")
        github_add = find_visual_item(root, "skillsAddButton")
        github_input = find_visual_item(root, "skillsAddInput")
        if github_add is None or github_add.property("enabled") is not True:
            raise AssertionError(
                "skills GitHub add button was disabled: "
                f"addMode={root.property('addMode')!r}, "
                f"addInput={root.property('addInput')!r}, "
                f"installing={root.property('installing')!r}, "
                f"inputText={github_input.property('text') if github_input else None!r}"
            )
        start = len(client.calls)
        press_key_on_item(app, view, root, "skillsAddInput", Qt.Key_Return)
        add = find_visual_item(root, "skillsAddButton")
        if ("InstallSkillFromGitHub", ("owner/repo",)) not in client.calls[start:]:
            add = click_item(app, view, root, "skillsAddButton")
        if ("InstallSkillFromGitHub", ("owner/repo",)) not in client.calls[start:] and add is not None:
            invoke_click(add)
            pump(app)
        assert_call(client, start, "InstallSkillFromGitHub", ("owner/repo",))
    finally:
        close_view(app, view)
        if skills._poll_timer.isActive() or proposals._poll_timer.isActive():
            raise AssertionError("skills view did not stop route-scoped polling after close")


def main():
    QQuickStyle.setStyle("Basic")
    app = QGuiApplication([])
    client = FakeRpcClient()
    for check in (check_config, check_connectors, check_memory, check_utility_load_errors, check_notes, check_reviewers, check_skills):
        check(app, client)
    return 0


raise SystemExit(main())
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
