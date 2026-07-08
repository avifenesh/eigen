"""
crons.py - Scheduled work view model.

Loads the Crons RPC while the route is visible. This first Qt slice is
metadata-only: systemd timer and crontab status without mutating the user's
actual scheduler.
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


class CronsModel(QObject):
    """Read-only scheduled-work snapshot for timers and crontab rows."""

    crons_changed = Signal()
    loading_changed = Signal()
    load_error_changed = Signal()
    summary_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._crons: list[dict] = []
        self._timers = 0
        self._crontab = 0
        self._systemd_available = False
        self._loading = False
        self._load_error = ""
        self._active = False
        self._load_seq = 0

        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)
        self._poll_timer.timeout.connect(self._fetch)

        self._client.connected.connect(self._on_connected)

    @Property(list, notify=crons_changed)
    def crons(self) -> list[dict]:
        return self._crons

    @Property(bool, notify=loading_changed)
    def loading(self) -> bool:
        return self._loading

    @Property(str, notify=load_error_changed)
    def load_error(self) -> str:
        return self._load_error

    @Property(int, notify=summary_changed)
    def timers_count(self) -> int:
        return self._timers

    @Property(int, notify=summary_changed)
    def crontab_count(self) -> int:
        return self._crontab

    @Property(bool, notify=summary_changed)
    def systemd_available(self) -> bool:
        return self._systemd_available

    @Property(int, notify=summary_changed)
    def active_timer_count(self) -> int:
        return sum(
            1
            for cron in self._crons
            if cron.get("kind") == "timer" and bool(cron.get("active"))
        )

    @Property(int, notify=summary_changed)
    def enabled_timer_count(self) -> int:
        return sum(
            1
            for cron in self._crons
            if cron.get("kind") == "timer" and bool(cron.get("enabled"))
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
        self._client.call("Crons", callback=lambda result: self._on_crons_result(result, seq))

    def _on_crons_result(self, result: dict, seq: Optional[int] = None):
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            self._set_loading(False)
            self._set_load_error(_err_text(result))
            return

        data = result.get("result") or {}
        crons = data.get("crons") if isinstance(data, dict) else []
        self._crons = [dict(cron) for cron in list(crons or []) if isinstance(cron, dict)]
        self._timers = int(data.get("timers") or 0) if isinstance(data, dict) else 0
        self._crontab = int(data.get("crontab") or 0) if isinstance(data, dict) else 0
        self._systemd_available = bool(data.get("systemdAvail")) if isinstance(data, dict) else False
        self.crons_changed.emit()
        self.summary_changed.emit()
        self._set_loading(False)
