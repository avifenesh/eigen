"""
routing.py — Routing catalog view model.

Loads the read-only Routing RPC plus the optional ObserveSummary route stats.
The catalog is small, so it is exposed as QVariant lists for simple QML-side
filtering instead of a role-heavy list model.
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


class RoutingModel(QObject):
    """Read-only model/provider routing catalog plus observed route stats."""

    models_changed = Signal()
    providers_changed = Signal()
    routes_changed = Signal()
    loading_changed = Signal()
    load_error_changed = Signal()
    summary_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._models: list[dict] = []
        self._providers: list[dict] = []
        self._routes: dict = {}
        self._loading = False
        self._load_error = ""
        self._active = False
        self._load_seq = 0

        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)
        self._poll_timer.timeout.connect(self._fetch)

        self._client.connected.connect(self._on_connected)

    @Property(list, notify=models_changed)
    def models(self) -> list[dict]:
        return self._models

    @Property(list, notify=providers_changed)
    def providers(self) -> list[dict]:
        return self._providers

    @Property("QVariantMap", notify=routes_changed)
    def routes(self) -> dict:
        return self._routes

    @Property(bool, notify=loading_changed)
    def loading(self) -> bool:
        return self._loading

    @Property(str, notify=load_error_changed)
    def load_error(self) -> str:
        return self._load_error

    @Property(int, notify=summary_changed)
    def model_count(self) -> int:
        return len(self._models)

    @Property(int, notify=summary_changed)
    def available_count(self) -> int:
        return sum(1 for model in self._models if bool(model.get("available")))

    @Property(int, notify=summary_changed)
    def provider_count(self) -> int:
        return sum(1 for provider in self._providers if int(provider.get("modelCount") or 0) > 0)

    @Property(int, notify=summary_changed)
    def route_total(self) -> int:
        routes = self._routes or {}
        return (
            int(routes.get("routed") or 0)
            + int(routes.get("skipped") or 0)
            + int(routes.get("assessed") or 0)
            + int(routes.get("orchestrator") or 0)
        )

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

    def _set_routes(self, value: dict):
        if self._routes == value:
            return
        self._routes = value
        self.routes_changed.emit()
        self.summary_changed.emit()

    def _fetch(self):
        self._load_seq += 1
        seq = self._load_seq
        self._set_loading(True)
        self._set_load_error("")
        self._client.call("Routing", callback=lambda result: self._on_routing_result(result, seq))
        self._client.call("ObserveSummary", 5000, callback=lambda result: self._on_observe_result(result, seq))

    def _on_routing_result(self, result: dict, seq: Optional[int] = None):
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            self._set_loading(False)
            self._set_load_error(_err_text(result))
            return

        data = result.get("result") or {}
        models = data.get("models") if isinstance(data, dict) else []
        providers = data.get("providers") if isinstance(data, dict) else []
        self._models = list(models or [])
        self._providers = list(providers or [])
        self.models_changed.emit()
        self.providers_changed.emit()
        self.summary_changed.emit()
        self._set_loading(False)

    def _on_observe_result(self, result: dict, seq: Optional[int] = None):
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            return

        data = result.get("result") or {}
        if not isinstance(data, dict) or not data.get("available") or int(data.get("records") or 0) <= 0:
            self._set_routes({})
            return

        routes = data.get("routes") or {}
        if not isinstance(routes, dict):
            self._set_routes({})
            return
        self._set_routes(dict(routes))
