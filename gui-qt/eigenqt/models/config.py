"""
config.py — Config view model (ConfigModel for editable config fields + RuleChainsModel).

ConfigModel: loads Config() RPC, exposes fields as QAbstractListModel, mutation via SetConfig RPC.
RuleChainsModel: loads RuleChains() RPC, exposes per-role chains, mutations via SetRuleChain RPC.
Both poll every 60s and reload on window visibility (config can be edited externally).
"""

from typing import Optional

from PySide6.QtCore import (
    QAbstractListModel,
    QModelIndex,
    QObject,
    QTimer,
    Qt,
    Property,
    Signal,
    Slot,
)

from eigenqt.rpc.client import RpcClient


def _err_text(result: dict) -> str:
    """Extract error message from RPC result, handling string or dict errors."""
    e = result.get("error")
    if isinstance(e, str):
        return e or "Unknown error"
    if isinstance(e, dict):
        return e.get("message", "Unknown error")
    return str(e) if e else "Unknown error"


class ConfigModel(QAbstractListModel):
    """
    Config fields model — editable ~/.eigen/config.json.

    Roles: key, desc, value, options (array of strings or null), multi (bool), allowEmpty (bool).
    Mutations: SetConfig (key, value) via QML slot.
    """

    # Qt roles
    KeyRole = Qt.UserRole + 1
    DescRole = Qt.UserRole + 2
    ValueRole = Qt.UserRole + 3
    OptionsRole = Qt.UserRole + 4  # list[str] or None
    MultiRole = Qt.UserRole + 5  # bool
    AllowEmptyRole = Qt.UserRole + 6  # bool

    # Signals
    set_config_done = Signal(str, str, bool, str)  # (key, stored_value, success, error_msg)

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._config_path = ""
        self._fields: list[dict] = []
        self._active = False
        self._load_seq = 0
        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)  # 60s
        self._poll_timer.timeout.connect(self._fetch_config)

        self._client.connected.connect(self._on_connected)

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.KeyRole: b"key",
            self.DescRole: b"desc",
            self.ValueRole: b"value",
            self.OptionsRole: b"options",
            self.MultiRole: b"multi",
            self.AllowEmptyRole: b"allowEmpty",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count."""
        if parent.isValid():
            return 0
        return len(self._fields)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._fields):
            return None

        field = self._fields[index.row()]
        if role == self.KeyRole:
            return field.get("key", "")
        if role == self.DescRole:
            return field.get("desc", "")
        if role == self.ValueRole:
            return field.get("value", "")
        if role == self.OptionsRole:
            return field.get("options")  # None or list[str]
        if role == self.MultiRole:
            return field.get("multi", False)
        if role == self.AllowEmptyRole:
            return field.get("allowEmpty", False)
        return None

    configPathChanged = Signal()

    @Property(str, notify=configPathChanged)
    def config_path(self) -> str:
        """Config file path (for QML display)."""
        return self._config_path

    @Slot()
    def _on_connected(self):
        """Fetch config on connect only while the route is active."""
        if self._active:
            self.start_polling()

    def _fetch_config(self):
        """Async fetch Config RPC."""
        self._load_seq += 1
        seq = self._load_seq
        self._client.call(
            "Config",
            callback=lambda result: self._on_config_result(result, seq),
        )

    @Slot(dict)
    def _on_config_result(self, result: dict, seq: Optional[int] = None):
        """Handle Config RPC result."""
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            return

        data = result.get("result") or {}
        fields = data.get("fields") or []
        path = data.get("path") or ""

        self.beginResetModel()
        self._fields = fields
        self._config_path = path
        self.configPathChanged.emit()
        self.endResetModel()

    @Slot(str, str)
    def set_config(self, key: str, value: str):
        """
        Persist a config field mutation via SetConfig RPC.
        Emits set_config_done(key, stored_value, success, error_msg) on completion.
        """
        # A user edit is newer than any already in-flight poll result.
        self._load_seq += 1
        self._client.call(
            "SetConfig",
            key, value,
            callback=lambda r: self._on_set_config_result(key, r),
        )

    def _on_set_config_result(self, key: str, result: dict):
        """Handle SetConfig RPC result."""
        if "error" in result:
            error_msg = _err_text(result)
            self.set_config_done.emit(key, "", False, error_msg)
            return

        stored_value = result.get("result", "")
        self.set_config_done.emit(key, stored_value, True, "")

        # Update the field in the model
        for i, field in enumerate(self._fields):
            if field.get("key") == key:
                field["value"] = stored_value
                idx = self.index(i, 0)
                self.dataChanged.emit(idx, idx, [self.ValueRole])
                break

    @Slot()
    def refresh(self):
        """Manually refresh config (called by QML on window visibility change)."""
        self._fetch_config()

    @Slot(bool)
    def set_active(self, active: bool):
        """Start/stop route-scoped polling."""
        if self._active == active:
            return
        self._active = active
        if active:
            self.start_polling()
        else:
            self.stop_polling()

    def stop_polling(self):
        """Stop polling when view is inactive."""
        self._poll_timer.stop()
        self._load_seq += 1

    def start_polling(self):
        """Resume polling when view becomes active."""
        if not self._poll_timer.isActive():
            self._fetch_config()
            self._poll_timer.start()


class RuleChainsModel(QAbstractListModel):
    """
    Rule chains model — per-role model fallback chains.

    Roles: role, desc, chain (array of model strings), custom (bool), models (array of all available models).
    Mutations: SetRuleChain (role, chain) via QML slot.
    """

    # Qt roles
    RoleNameRole = Qt.UserRole + 1
    DescRole = Qt.UserRole + 2
    ChainRole = Qt.UserRole + 3  # list[str]
    CustomRole = Qt.UserRole + 4  # bool
    ModelsRole = Qt.UserRole + 5  # list[str] (all available models)

    # Signals
    set_rule_chain_done = Signal(str, list, bool, str)  # (role, stored_chain, success, error_msg)

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._roles: list[dict] = []
        self._models: list[str] = []
        self._active = False
        self._load_seq = 0
        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)  # 60s
        self._poll_timer.timeout.connect(self._fetch_rule_chains)

        self._client.connected.connect(self._on_connected)

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.RoleNameRole: b"roleName",
            self.DescRole: b"desc",
            self.ChainRole: b"chain",
            self.CustomRole: b"custom",
            self.ModelsRole: b"models",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count."""
        if parent.isValid():
            return 0
        return len(self._roles)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._roles):
            return None

        role_data = self._roles[index.row()]
        if role == self.RoleNameRole:
            return role_data.get("role", "")
        if role == self.DescRole:
            return role_data.get("desc", "")
        if role == self.ChainRole:
            return role_data.get("chain", [])
        if role == self.CustomRole:
            return role_data.get("custom", False)
        if role == self.ModelsRole:
            return self._models
        return None

    @Slot()
    def _on_connected(self):
        """Fetch rule chains on connect only while the route is active."""
        if self._active:
            self.start_polling()

    def _fetch_rule_chains(self):
        """Async fetch RuleChains RPC."""
        self._load_seq += 1
        seq = self._load_seq
        self._client.call(
            "RuleChains",
            callback=lambda result: self._on_rule_chains_result(result, seq),
        )

    @Slot(dict)
    def _on_rule_chains_result(self, result: dict, seq: Optional[int] = None):
        """Handle RuleChains RPC result."""
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            return

        data = result.get("result") or {}
        roles = data.get("roles") or []
        models = data.get("models") or []

        self.beginResetModel()
        self._roles = roles
        self._models = models
        self.endResetModel()

    @Slot(str, list)
    def set_rule_chain(self, role_name: str, chain: list):
        """
        Persist a role's chain mutation via SetRuleChain RPC.
        Emits set_rule_chain_done(role, stored_chain, success, error_msg) on completion.
        """
        # A user edit is newer than any already in-flight poll result.
        self._load_seq += 1
        self._client.call(
            "SetRuleChain",
            role_name, chain,
            callback=lambda r: self._on_set_rule_chain_result(role_name, r),
        )

    def _on_set_rule_chain_result(self, role_name: str, result: dict):
        """Handle SetRuleChain RPC result."""
        if "error" in result:
            error_msg = _err_text(result)
            self.set_rule_chain_done.emit(role_name, [], False, error_msg)
            return

        stored_chain = result.get("result", [])
        self.set_rule_chain_done.emit(role_name, stored_chain, True, "")

        # Update the role in the model
        for i, role_data in enumerate(self._roles):
            if role_data.get("role") == role_name:
                role_data["chain"] = stored_chain
                role_data["custom"] = len(stored_chain) > 0  # empty = default
                idx = self.index(i, 0)
                self.dataChanged.emit(idx, idx, [self.ChainRole, self.CustomRole])
                break

    @Slot()
    def refresh(self):
        """Manually refresh rule chains (called by QML on window visibility change)."""
        self._fetch_rule_chains()

    @Slot(bool)
    def set_active(self, active: bool):
        """Start/stop route-scoped polling."""
        if self._active == active:
            return
        self._active = active
        if active:
            self.start_polling()
        else:
            self.stop_polling()

    def stop_polling(self):
        """Stop polling when view is inactive."""
        self._poll_timer.stop()
        self._load_seq += 1

    def start_polling(self):
        """Resume polling when view becomes active."""
        if not self._poll_timer.isActive():
            self._fetch_rule_chains()
            self._poll_timer.start()
