"""
observe.py — Observe summary view model.

Loads the metadata-only ObserveSummary RPC for a read-only Qt telemetry route.
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


class ObserveModel(QObject):
    """Read-only observability summary for tools, models, routes, and errors."""

    summary_changed = Signal()
    loading_changed = Signal()
    load_error_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._summary: dict = {}
        self._loading = False
        self._load_error = ""
        self._active = False
        self._load_seq = 0

        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)
        self._poll_timer.timeout.connect(self._fetch)

        self._client.connected.connect(self._on_connected)

    @Property("QVariantMap", notify=summary_changed)
    def summary(self) -> dict:
        return self._summary

    @Property(bool, notify=loading_changed)
    def loading(self) -> bool:
        return self._loading

    @Property(str, notify=load_error_changed)
    def load_error(self) -> str:
        return self._load_error

    @Property(bool, notify=summary_changed)
    def available(self) -> bool:
        return bool(self._summary.get("available"))

    @Property(int, notify=summary_changed)
    def records(self) -> int:
        return int(self._summary.get("records") or 0)

    @Property(int, notify=summary_changed)
    def route_total(self) -> int:
        routes = self._summary.get("routes") or {}
        return (
            int(routes.get("routed") or 0)
            + int(routes.get("skipped") or 0)
            + int(routes.get("assessed") or 0)
            + int(routes.get("orchestrator") or 0)
        )

    @Property(int, notify=summary_changed)
    def tool_calls(self) -> int:
        return sum(int(tool.get("calls") or 0) for tool in self._summary.get("tools") or [])

    @Property(int, notify=summary_changed)
    def tool_errors(self) -> int:
        return sum(int(tool.get("errors") or 0) for tool in self._summary.get("tools") or [])

    @Property(int, notify=summary_changed)
    def model_turns(self) -> int:
        return sum(int(model.get("turns") or 0) for model in self._summary.get("models") or [])

    @Property(int, notify=summary_changed)
    def error_count(self) -> int:
        return sum(int(item.get("count") or 0) for item in self._summary.get("errors") or [])

    @Property(int, notify=summary_changed)
    def subagent_errors(self) -> int:
        subagents = self._summary.get("subagents") or {}
        return (
            int(subagents.get("taskErrors") or 0)
            + int(subagents.get("groupErrors") or 0)
            + int(subagents.get("mutatingErrors") or 0)
            + int(subagents.get("promoteErrors") or 0)
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

    def _fetch(self):
        self._load_seq += 1
        seq = self._load_seq
        self._set_loading(True)
        self._set_load_error("")
        self._client.call("ObserveSummary", 5000, callback=lambda result: self._on_summary_result(result, seq))

    def _on_summary_result(self, result: dict, seq: Optional[int] = None):
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            self._set_loading(False)
            self._set_load_error(_err_text(result))
            return

        data = result.get("result") or {}
        self._summary = dict(data) if isinstance(data, dict) else {}
        self.summary_changed.emit()
        self._set_loading(False)
