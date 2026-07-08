"""
dreaming.py - Dreaming timeline view model.

Loads memory scopes plus the read-only DreamingForScope timeline while the route
is visible. This Qt slice intentionally avoids DreamNow and consolidation diff
writes; it is a safe inventory of rollouts and memory snapshots.
"""

from typing import Optional

from PySide6.QtCore import QObject, Property, QTimer, Signal, Slot

from eigenqt.rpc.client import RpcClient


def _err_text(result: dict | None) -> str:
    """Extract error message from RPC result, handling string or dict errors."""
    if not result or "error" not in result:
        return "Unknown error"
    error = result.get("error")
    if isinstance(error, str):
        return error or "Unknown error"
    if isinstance(error, dict):
        return error.get("message") or "Unknown error"
    return str(error) if error else "Unknown error"


def _as_int(value) -> int:
    try:
        return int(value or 0)
    except (TypeError, ValueError):
        return 0


class DreamingModel(QObject):
    """Read-only dreaming timeline for one memory scope."""

    scopes_changed = Signal()
    scope_key_changed = Signal()
    current_changed = Signal()
    loading_changed = Signal()
    load_error_changed = Signal()
    summary_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._scopes: list[dict] = []
        self._scope_key = "project"
        self._current: dict = {}
        self._loading = False
        self._load_error = ""
        self._active = False
        self._scope_seq = 0
        self._load_seq = 0

        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)
        self._poll_timer.timeout.connect(self._fetch_current)

        self._client.connected.connect(self._on_connected)

    @Property(list, notify=scopes_changed)
    def scopes(self) -> list[dict]:
        return self._scopes

    @Property(str, notify=scope_key_changed)
    def scope_key(self) -> str:
        return self._scope_key

    @Property("QVariantMap", notify=current_changed)
    def current(self) -> dict:
        return self._current

    @Property(bool, notify=loading_changed)
    def loading(self) -> bool:
        return self._loading

    @Property(str, notify=load_error_changed)
    def load_error(self) -> str:
        return self._load_error

    @Property(str, notify=summary_changed)
    def scope_label(self) -> str:
        for scope in self._scopes:
            if scope.get("key") == self._scope_key:
                return str(scope.get("name") or self._scope_key)
        return str(self._current.get("scope") or self._scope_key)

    @Property(list, notify=current_changed)
    def rollouts(self) -> list[dict]:
        return [dict(row) for row in self._current.get("rollouts") or [] if isinstance(row, dict)]

    @Property(list, notify=current_changed)
    def consolidations(self) -> list[dict]:
        return [dict(row) for row in self._current.get("consolidations") or [] if isinstance(row, dict)]

    @Property(int, notify=summary_changed)
    def rollout_count(self) -> int:
        return len(self.rollouts)

    @Property(int, notify=summary_changed)
    def consolidation_count(self) -> int:
        return len(self.consolidations)

    @Property(int, notify=summary_changed)
    def current_bytes(self) -> int:
        return _as_int(self._current.get("currentBytes"))

    @Slot()
    def _on_connected(self):
        if self._active:
            self.start_polling()

    @Slot()
    def refresh(self):
        self._fetch_scopes()
        self._fetch_current()

    @Slot()
    def load(self):
        self.refresh()

    @Slot(bool)
    def set_active(self, active: bool):
        if self._active == active:
            return
        self._active = active
        if active:
            self.start_polling()
        else:
            self.stop_polling()

    @Slot(str)
    def select_scope(self, key: str):
        if not key or key == self._scope_key:
            return
        self._scope_key = key
        self.scope_key_changed.emit()
        self.summary_changed.emit()
        self._fetch_current()

    def start_polling(self):
        if not self._poll_timer.isActive():
            self.refresh()
            self._poll_timer.start()

    def stop_polling(self):
        self._poll_timer.stop()
        self._scope_seq += 1
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

    def _fetch_scopes(self):
        self._scope_seq += 1
        seq = self._scope_seq
        self._client.call("ListMemoryScopes", callback=lambda result: self._on_scopes_result(result, seq))

    def _fetch_current(self):
        self._load_seq += 1
        seq = self._load_seq
        self._set_loading(True)
        self._set_load_error("")
        self._client.call(
            "DreamingForScope",
            self._scope_key,
            callback=lambda result: self._on_current_result(result, seq),
        )

    def _on_scopes_result(self, result: dict, seq: Optional[int] = None):
        if seq is not None and seq != self._scope_seq:
            return
        if "error" in result:
            if not self._current:
                self._set_load_error(_err_text(result))
            return

        refs = result.get("result") or []
        self._scopes = [dict(scope) for scope in list(refs or []) if isinstance(scope, dict)]
        self.scopes_changed.emit()
        self.summary_changed.emit()

        if self._scope_key == "project":
            for scope in self._scopes:
                if bool(scope.get("current")) and scope.get("key"):
                    self._scope_key = str(scope.get("key"))
                    self.scope_key_changed.emit()
                    self.summary_changed.emit()
                    self._fetch_current()
                    break

    def _on_current_result(self, result: dict, seq: Optional[int] = None):
        if seq is not None and seq != self._load_seq:
            return
        self._set_loading(False)
        if "error" in result:
            self._set_load_error(_err_text(result))
            return

        data = result.get("result") or {}
        self._current = dict(data) if isinstance(data, dict) else {}
        self.current_changed.emit()
        self.summary_changed.emit()
