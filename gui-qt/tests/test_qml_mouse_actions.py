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
from eigenqt.models.memory import MemoryModel
from eigenqt.models.notes import NotesController
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

        click_item(app, view, root, "configTab_Models")
        if root.property("activeTab") != "Models":
            raise AssertionError("config Models tab did not activate")

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

        start = len(client.calls)
        chip = focus_item(app, root, "configMultiChip_route_providers_local", flick_name="configFlick")
        if not chip.property("qaVisualFocus"):
            raise AssertionError("config route_providers chip did not expose keyboard focus")
        QTest.keyClick(view, Qt.Key_Space)
        pump(app)
        assert_call(client, start, "SetConfig", ("route_providers", "openai xai local"))

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
        click_item(app, view, root, "connectorsLoadErrorRetry", flick_name="connectorsFlick")
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
        if add_name is None:
            raise AssertionError("missing connector name input")
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
        if server_name is None:
            raise AssertionError("missing local server name input")
        assert_placeholder_color(server_name, "local server name")
        set_text(app, root, "connectorsServerNameInput", "github-local")
        if save_button.property("enabled"):
            raise AssertionError("local server form without command should not be submittable")
        set_text(app, root, "connectorsServerCommandInput", "uvx github-mcp-server")
        set_text(app, root, "connectorsServerDescInput", "GitHub MCP")
        set_text(app, root, "connectorsServerEnvInput", "LOG_LEVEL=info")
        set_text(app, root, "connectorsServerSecretInput", "GITHUB_TOKEN=secret")
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
        if server.get("envPairs") != ["LOG_LEVEL=info"]:
            raise AssertionError(f"env payload lost: {server}")
        if server.get("secretEnvPairs") != ["GITHUB_TOKEN=secret"]:
            raise AssertionError(f"secret payload lost: {server}")
        error_box = find_visual_item(root, "connectorsServerActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed local server save did not render an action error")
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
        set_text(app, root, "memoryComposeTextArea", "Mouse action memory proof")
        client.failures["AppendMemory"] = "save denied"
        client.delays["AppendMemory"] = 45
        start = len(client.calls)
        save = click_item(app, view, root, "memorySaveNoteButton")
        if ("AppendMemory", ("global", "Mouse action memory proof")) not in client.calls[start:]:
            invoke_click(save)
            pump(app)
        assert_call(client, start, "AppendMemory", ("global", "Mouse action memory proof"))
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
        if client.calls[start:].count(("AppendMemory", ("global", "Mouse action memory proof"))) != 1:
            raise AssertionError("memory pending save submitted a duplicate AppendMemory")
        QTest.qWait(70)
        pump(app, 12)
        error_box = find_visual_item(root, "memoryActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed memory note save did not render an action error")
        if "save denied" not in memory.action_error:
            raise AssertionError(f"unexpected memory action error: {memory.action_error}")
        if not memory.composing or memory.draft != "Mouse action memory proof":
            raise AssertionError("failed memory note save did not preserve the draft")
        del client.failures["AppendMemory"]
        del client.delays["AppendMemory"]

        start = len(client.calls)
        save = click_item(app, view, root, "memorySaveNoteButton")
        if ("AppendMemory", ("global", "Mouse action memory proof")) not in client.calls[start:]:
            invoke_click(save)
            pump(app)
        assert_call(client, start, "AppendMemory", ("global", "Mouse action memory proof"))

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
        start = len(client.calls)
        click_item(app, view, root, "memoryNoteRemoveConfirmButton_0", flick_name="memoryFlick")
        assert_call(client, start, "RemoveMemoryNote", ("global", 0))

        edit_profile = click_item(app, view, root, "memoryEditProfileButton", flick_name="memoryFlick")
        if not memory.editing_profile:
            invoke_click(edit_profile)
            pump(app)
        set_text(app, root, "memoryProfileTextArea", "Keyboard profile proof")
        start = len(client.calls)
        press_key_on_item(app, view, root, "memoryProfileTextArea", Qt.Key_Return, Qt.ControlModifier, flick_name="memoryFlick")
        assert_call(client, start, "WriteUserProfile", ("Keyboard profile proof",))

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
        set_text(app, root, "memoryBanTitleInput", "No broad rewrites")
        set_text(app, root, "memoryBanRuleTextArea", "Do not expand a Qt follow-up without evidence.")
        start = len(client.calls)
        press_key_on_item(app, view, root, "memoryBanRuleTextArea", Qt.Key_Return, Qt.ControlModifier, flick_name="memoryFlick")
        assert_call(client, start, "AddBan", ("global", "No broad rewrites", "Do not expand a Qt follow-up without evidence."))

        memory.adding_ban = True
        memory.ban_title = "discard"
        memory.ban_rule = "discard"
        pump(app)
        press_key_on_item(app, view, root, "memoryBanTitleInput", Qt.Key_Escape, flick_name="memoryFlick")
        if memory.adding_ban or memory.ban_title or memory.ban_rule:
            raise AssertionError("ban Escape did not cancel and clear the form")
    finally:
        close_view(app, view)


def check_notes(app, client):
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
        set_text(app, root, "notesEditorTextArea", "# Existing\n\nEdited body")

        client.failures["ObsidianWrite"] = "save denied"
        client.delays["ObsidianWrite"] = 45
        start = len(client.calls)
        save = click_item(app, view, root, "notesSaveEditButton")
        if ("ObsidianWrite", ("Inbox/Existing.md", "# Existing\n\nEdited body", False)) not in client.calls[start:]:
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
        assert_call(client, start, "ObsidianWrite", ("Inbox/Existing.md", "# Existing\n\nEdited body", False))
        error_box = find_visual_item(root, "notesActionError")
        if error_box is None or not error_box.property("visible"):
            raise AssertionError("failed note save did not render an action error")
        if not notes.editing or notes.draft != "# Existing\n\nEdited body":
            raise AssertionError("failed note save did not keep the editor draft alive")
        del client.failures["ObsidianWrite"]
        del client.delays["ObsidianWrite"]

        start = len(client.calls)
        save = click_item(app, view, root, "notesSaveEditButton")
        if ("ObsidianWrite", ("Inbox/Existing.md", "# Existing\n\nEdited body", False)) not in client.calls[start:]:
            invoke_click(save)
            pump(app)
        assert_call(client, start, "ObsidianWrite", ("Inbox/Existing.md", "# Existing\n\nEdited body", False))
        if notes.editing or notes.content != "# Existing\n\nEdited body":
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
        start = len(client.calls)
        add = click_item(app, view, root, "skillsAddButton")
        if ("InstallSkillFromGitHub", ("owner/repo",)) not in client.calls[start:]:
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
    for check in (check_config, check_connectors, check_memory, check_notes, check_reviewers, check_skills):
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
