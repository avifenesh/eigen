"""Plugin registry inventory and management model."""

from typing import Optional

from PySide6.QtCore import QObject, Property, QTimer, Signal, Slot

from eigenqt.rpc.client import RpcClient


def _err_text(result: dict) -> str:
    """Extract error message from RPC result, handling string or dict errors."""
    e = result.get("error")
    if isinstance(e, str):
        return e or "Unknown error"
    if isinstance(e, dict):
        return e.get("message", "Unknown error")
    return str(e) if e else "Unknown error"


class PluginsModel(QObject):
    """Installed plugins, marketplaces, previews, and guarded mutations."""

    plugins_changed = Signal()
    marketplaces_changed = Signal()
    loading_changed = Signal()
    load_error_changed = Signal()
    summary_changed = Signal()
    previews_changed = Signal()
    marketplace_source_changed = Signal()
    flow_busy_changed = Signal()
    registry_busy_changed = Signal()
    pending_actions_changed = Signal()
    action_error_changed = Signal()
    action_message_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._plugins: list[dict] = []
        self._marketplaces: list[dict] = []
        self._loading = False
        self._load_error = ""
        self._previews: list[dict] = []
        self._preview_marketplace = ""
        self._marketplace_source = ""
        self._adding_marketplace = False
        self._browsing_marketplace = ""
        self._installing_plugin = ""
        self._pending_resources: set[str] = set()
        self._action_error = ""
        self._action_message = ""
        self._active = False
        self._load_seq = 0

        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)
        self._poll_timer.timeout.connect(self._fetch)

        self._client.connected.connect(self._on_connected)

    @Property(list, notify=plugins_changed)
    def plugins(self) -> list[dict]:
        return self._plugins

    @Property(list, notify=marketplaces_changed)
    def marketplaces(self) -> list[dict]:
        return self._marketplaces

    @Property(bool, notify=loading_changed)
    def loading(self) -> bool:
        return self._loading

    @Property(str, notify=load_error_changed)
    def load_error(self) -> str:
        return self._load_error

    @Property(list, notify=previews_changed)
    def previews(self) -> list[dict]:
        return self._previews

    @Property(str, notify=previews_changed)
    def preview_marketplace(self) -> str:
        return self._preview_marketplace

    @Property(str, notify=marketplace_source_changed)
    def marketplace_source(self) -> str:
        return self._marketplace_source

    @marketplace_source.setter
    def marketplace_source(self, value: str):
        value = value or ""
        if value == self._marketplace_source:
            return
        self._marketplace_source = value
        self.marketplace_source_changed.emit()

    @Property(bool, notify=flow_busy_changed)
    def adding_marketplace(self) -> bool:
        return self._adding_marketplace

    @Property(str, notify=flow_busy_changed)
    def browsing_marketplace(self) -> str:
        return self._browsing_marketplace

    @Property(str, notify=flow_busy_changed)
    def installing_plugin(self) -> str:
        return self._installing_plugin

    @Property(bool, notify=flow_busy_changed)
    def flow_busy(self) -> bool:
        return bool(
            self._adding_marketplace
            or self._browsing_marketplace
            or self._installing_plugin
        )

    @Property(bool, notify=registry_busy_changed)
    def registry_busy(self) -> bool:
        return self.flow_busy or bool(self._pending_resources)

    @Property(list, notify=pending_actions_changed)
    def pending_actions(self) -> list[str]:
        return sorted(self._pending_resources)

    @Property(str, notify=action_error_changed)
    def action_error(self) -> str:
        return self._action_error

    @Property(str, notify=action_message_changed)
    def action_message(self) -> str:
        return self._action_message

    @Property(int, notify=summary_changed)
    def plugin_count(self) -> int:
        return len(self._plugins)

    @Property(int, notify=summary_changed)
    def enabled_count(self) -> int:
        return sum(1 for plugin in self._plugins if bool(plugin.get("enabled")))

    @Property(int, notify=summary_changed)
    def marketplace_count(self) -> int:
        return len(self._marketplaces)

    @Property(int, notify=summary_changed)
    def disabled_market_count(self) -> int:
        return sum(1 for market in self._marketplaces if bool(market.get("disabled")))

    @Property(int, notify=summary_changed)
    def scan_flag_count(self) -> int:
        return sum(int(plugin.get("scanCount") or 0) for plugin in self._plugins)

    @Slot()
    def _on_connected(self):
        if self._active:
            self.start_polling()

    @Slot()
    def refresh(self):
        self._fetch()

    @Slot()
    def load(self):
        self._fetch()

    @Slot()
    def clear_action_error(self):
        self._set_action_error("")

    @Slot()
    def clear_action_message(self):
        self._set_action_message("")

    @Slot(str, str, result=bool)
    def is_pending(self, kind: str, name: str) -> bool:
        return self._resource_key(kind, name) in self._pending_resources

    @Slot()
    def add_marketplace(self):
        source = self._marketplace_source.strip()
        if not source or self.registry_busy:
            return
        self._begin_action()
        self._set_adding_marketplace(True)
        self._client.call(
            "AddMarketplace",
            source,
            callback=lambda result: self._on_add_marketplace_result(result),
        )

    def _on_add_marketplace_result(self, result: object):
        self._set_adding_marketplace(False)
        if not isinstance(result, dict):
            self._set_action_error("Invalid daemon response")
            return
        if "error" in result:
            self._set_action_error(_err_text(result))
            return
        market = result.get("result")
        if not isinstance(market, dict) or not str(market.get("name") or "").strip():
            self._set_action_message("No marketplace was added")
            return
        name = str(market["name"]).strip()
        self.marketplace_source = ""
        self._set_action_message(f"Added marketplace {name}")
        self._fetch()
        self._load_previews(name, preserve_feedback=True)

    @Slot(str)
    def browse_marketplace(self, name: str):
        name = (name or "").strip()
        if not name or self.registry_busy:
            return
        self._begin_action()
        self._load_previews(name)

    def _load_previews(self, name: str, preserve_feedback: bool = False):
        if not preserve_feedback:
            self._begin_action()
        self._set_browsing_marketplace(name)
        self._client.call(
            "MarketplacePlugins",
            name,
            callback=lambda result: self._on_previews_result(name, result),
        )

    def _on_previews_result(self, name: str, result: object):
        if self._browsing_marketplace != name:
            return
        self._set_browsing_marketplace("")
        if not isinstance(result, dict):
            self._set_previews([], "")
            self._set_action_error("Invalid daemon response")
            return
        if "error" in result:
            self._set_previews([], "")
            self._set_action_error(_err_text(result))
            return
        previews = result.get("result") or []
        rows = [dict(row) for row in list(previews) if isinstance(row, dict)]
        self._set_previews(rows, name)
        if not rows:
            self._set_action_message(f"{name} has no installable plugins")

    @Slot(str, str)
    def install_plugin(self, name: str, marketplace: str):
        name = (name or "").strip()
        marketplace = (marketplace or "").strip()
        if not name or self.registry_busy:
            return
        self._begin_action()
        self._set_installing_plugin(name)
        self._client.call(
            "InstallPlugin",
            name,
            marketplace,
            callback=lambda result: self._on_install_plugin_result(name, result),
        )

    def _on_install_plugin_result(self, name: str, result: object):
        if self._installing_plugin != name:
            return
        self._set_installing_plugin("")
        if not isinstance(result, dict):
            self._set_action_error("Invalid daemon response")
            return
        if "error" in result:
            message = _err_text(result)
            if "already installed" in message.lower():
                self._remove_preview(name)
                self._set_action_message(message)
                self._fetch()
            else:
                self._set_action_error(message)
            return
        plugin = result.get("result")
        installed_name = (
            str(plugin.get("name") or name) if isinstance(plugin, dict) else name
        )
        self._remove_preview(name)
        self._set_action_message(f"Installed {installed_name}")
        self._fetch()

    @Slot(str, bool)
    def set_plugin_enabled(self, name: str, enabled: bool):
        self._run_resource_action(
            "plugin",
            name,
            "SetPluginEnabled",
            [name, bool(enabled)],
            f"{'Enabled' if enabled else 'Disabled'} {name}",
            f"Plugin {name} was not found",
        )

    @Slot(str)
    def remove_plugin(self, name: str):
        self._run_resource_action(
            "plugin",
            name,
            "RemovePlugin",
            [name],
            f"Uninstalled {name}",
            f"{name} was not installed",
            false_is_error=False,
        )

    @Slot(str, bool)
    def set_market_enabled(self, name: str, enabled: bool):
        self._run_resource_action(
            "market",
            name,
            "SetMarketEnabled",
            [name, bool(enabled)],
            f"{'Enabled' if enabled else 'Disabled'} {name}",
            f"Marketplace {name} was not found",
            clear_preview=not enabled,
        )

    @Slot(str)
    def remove_marketplace(self, name: str):
        self._run_resource_action(
            "market",
            name,
            "RemoveMarketplace",
            [name],
            f"Removed marketplace {name}",
            f"Marketplace {name} was not found",
            false_is_error=False,
            clear_preview=True,
        )

    def _run_resource_action(
        self,
        kind: str,
        name: str,
        method: str,
        args: list,
        success_message: str,
        false_message: str,
        *,
        false_is_error: bool = True,
        clear_preview: bool = False,
    ):
        name = (name or "").strip()
        key = self._resource_key(kind, name)
        if not name or self.registry_busy or not self._mark_pending(key):
            return
        self._begin_action()
        self._client.call(
            method,
            *args,
            callback=lambda result: self._on_resource_action_result(
                key,
                result,
                success_message,
                false_message,
                false_is_error,
                clear_preview,
            ),
        )

    def _on_resource_action_result(
        self,
        key: str,
        result: object,
        success_message: str,
        false_message: str,
        false_is_error: bool,
        clear_preview: bool,
    ):
        if key not in self._pending_resources:
            return
        self._clear_pending(key)
        if not isinstance(result, dict):
            self._set_action_error("Invalid daemon response")
            self._fetch()
            return
        if "error" in result:
            self._set_action_error(_err_text(result))
            self._fetch()
            return
        if result.get("result") is False:
            if false_is_error:
                self._set_action_error(false_message)
            else:
                self._set_action_message(false_message)
            self._fetch()
            return
        if clear_preview and key == self._resource_key("market", self._preview_marketplace):
            self._set_previews([], "")
        self._set_action_message(success_message)
        self._fetch()

    @Slot(bool)
    def set_active(self, active: bool):
        if self._active == active:
            return
        self._active = active
        if active:
            self.start_polling()
        else:
            self.stop_polling()

    def start_polling(self):
        if not self._poll_timer.isActive():
            self._fetch()
            self._poll_timer.start()

    def stop_polling(self):
        self._poll_timer.stop()
        self._load_seq += 1

    def _set_loading(self, value: bool):
        if self._loading == value:
            return
        self._loading = value
        self.loading_changed.emit()

    def _set_load_error(self, value: str):
        if self._load_error == value:
            return
        self._load_error = value
        self.load_error_changed.emit()

    def _set_action_error(self, value: str):
        if self._action_error == value:
            return
        self._action_error = value
        self.action_error_changed.emit()

    def _set_action_message(self, value: str):
        if self._action_message == value:
            return
        self._action_message = value
        self.action_message_changed.emit()

    def _begin_action(self):
        self._set_action_error("")
        self._set_action_message("")

    def _set_adding_marketplace(self, value: bool):
        if self._adding_marketplace == value:
            return
        self._adding_marketplace = value
        self.flow_busy_changed.emit()
        self.registry_busy_changed.emit()

    def _set_browsing_marketplace(self, value: str):
        if self._browsing_marketplace == value:
            return
        self._browsing_marketplace = value
        self.flow_busy_changed.emit()
        self.registry_busy_changed.emit()

    def _set_installing_plugin(self, value: str):
        if self._installing_plugin == value:
            return
        self._installing_plugin = value
        self.flow_busy_changed.emit()
        self.registry_busy_changed.emit()

    def _set_previews(self, rows: list[dict], marketplace: str):
        self._previews = rows
        self._preview_marketplace = marketplace
        self.previews_changed.emit()

    def _remove_preview(self, name: str):
        rows = [row for row in self._previews if row.get("name") != name]
        if len(rows) == len(self._previews):
            return
        self._previews = rows
        self.previews_changed.emit()

    @staticmethod
    def _resource_key(kind: str, name: str) -> str:
        return f"{(kind or '').strip()}:{(name or '').strip()}"

    def _mark_pending(self, key: str) -> bool:
        if not key or key.endswith(":") or key in self._pending_resources:
            return False
        self._pending_resources.add(key)
        self.pending_actions_changed.emit()
        self.registry_busy_changed.emit()
        return True

    def _clear_pending(self, key: str):
        if key not in self._pending_resources:
            return
        self._pending_resources.remove(key)
        self.pending_actions_changed.emit()
        self.registry_busy_changed.emit()

    def _fetch(self):
        self._load_seq += 1
        seq = self._load_seq
        self._set_loading(True)
        self._set_load_error("")
        self._client.call("Plugins", callback=lambda result: self._on_plugins_result(result, seq))

    def _on_plugins_result(self, result: object, seq: Optional[int] = None):
        if seq is not None and seq != self._load_seq:
            return
        if not isinstance(result, dict):
            self._set_loading(False)
            self._set_load_error("Invalid daemon response")
            return
        if "error" in result:
            self._set_loading(False)
            self._set_load_error(_err_text(result))
            return

        data = result.get("result") or {}
        plugins = data.get("plugins") if isinstance(data, dict) else []
        marketplaces = data.get("marketplaces") if isinstance(data, dict) else []
        self._plugins = [dict(plugin) for plugin in list(plugins or []) if isinstance(plugin, dict)]
        self._marketplaces = [
            dict(market) for market in list(marketplaces or []) if isinstance(market, dict)
        ]
        self.plugins_changed.emit()
        self.marketplaces_changed.emit()
        self.summary_changed.emit()
        self._set_loading(False)
