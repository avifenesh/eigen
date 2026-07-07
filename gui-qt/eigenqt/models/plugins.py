"""
plugins.py - Plugin registry view model.

Loads the read-only Plugins RPC while the route is visible. Management actions
remain in the legacy surface for now; this Qt slice is an inventory/scan-status
overview only.
"""

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
    """Read-only installed plugin and marketplace inventory."""

    plugins_changed = Signal()
    marketplaces_changed = Signal()
    loading_changed = Signal()
    load_error_changed = Signal()
    summary_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._plugins: list[dict] = []
        self._marketplaces: list[dict] = []
        self._loading = False
        self._load_error = ""
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

    def _fetch(self):
        self._load_seq += 1
        seq = self._load_seq
        self._set_loading(True)
        self._set_load_error("")
        self._client.call("Plugins", callback=lambda result: self._on_plugins_result(result, seq))

    def _on_plugins_result(self, result: dict, seq: Optional[int] = None):
        if seq is not None and seq != self._load_seq:
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
