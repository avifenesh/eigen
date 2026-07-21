"""
crons.py - Scheduled work view model.

Loads the Crons RPC while the route is visible and owns guarded scheduler
mutations. Timer controls and crontab writes stay in the Go bridge; this model
only coordinates pending state, feedback, and refreshes for the native view.
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
    """Scheduled-work snapshot plus guarded timer and crontab actions."""

    crons_changed = Signal()
    loading_changed = Signal()
    load_error_changed = Signal()
    summary_changed = Signal()
    pending_actions_changed = Signal()
    adding_job_changed = Signal()
    action_error_changed = Signal()
    action_message_changed = Signal()
    jobAdded = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._crons: list[dict] = []
        self._timers = 0
        self._crontab = 0
        self._systemd_available = False
        self._loading = False
        self._load_error = ""
        self._pending_actions: set[str] = set()
        self._adding_job = False
        self._action_error = ""
        self._action_message = ""
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

    @Property(list, notify=pending_actions_changed)
    def pending_actions(self) -> list[str]:
        return sorted(self._pending_actions)

    @Property(bool, notify=adding_job_changed)
    def adding_job(self) -> bool:
        return self._adding_job

    @Property(str, notify=action_error_changed)
    def action_error(self) -> str:
        return self._action_error

    @Property(str, notify=action_message_changed)
    def action_message(self) -> str:
        return self._action_message

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

    @Slot()
    def clear_action_error(self):
        self._set_action_error("")

    @Slot()
    def clear_action_message(self):
        self._set_action_message("")

    @Slot(str, str)
    def set_timer(self, unit: str, verb: str):
        unit = str(unit or "").strip()
        verb = str(verb or "").strip().lower()
        if not unit or verb not in {"start", "stop", "enable", "disable"}:
            self._set_action_error("Choose a valid timer action")
            return

        key = self._timer_key(unit)
        if not self._mark_pending(key):
            return
        self._begin_action()
        success = {
            "start": f"Started {unit}",
            "stop": f"Stopped {unit}",
            "enable": f"Enabled {unit}",
            "disable": f"Disabled {unit}",
        }[verb]
        self._client.call(
            "SetTimer",
            unit,
            verb,
            callback=lambda result: self._on_resource_action_result(
                key, result, success
            ),
        )

    @Slot(str, str)
    def add_crontab(self, spec: str, command: str):
        spec = str(spec or "").strip()
        command = str(command or "").strip()
        if not spec or not command:
            self._set_action_error("Schedule and command are required")
            return
        if self._adding_job:
            return

        self._begin_action()
        self._set_adding_job(True)
        self._client.call(
            "AddCrontab",
            spec,
            command,
            callback=lambda result: self._on_add_crontab_result(result),
        )

    @Slot(str, str)
    def remove_crontab(self, spec: str, command: str):
        spec = str(spec or "").strip()
        command = str(command or "").strip()
        if not spec or not command:
            self._set_action_error("Schedule and command are required")
            return

        key = self._crontab_key(spec, command)
        if not self._mark_pending(key):
            return
        self._begin_action()
        self._client.call(
            "RemoveCrontab",
            spec,
            command,
            callback=lambda result: self._on_resource_action_result(
                key, result, "Removed scheduled job"
            ),
        )

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

    def _set_adding_job(self, value: bool):
        if self._adding_job == value:
            return
        self._adding_job = value
        self.adding_job_changed.emit()

    def _begin_action(self):
        self._set_action_error("")
        self._set_action_message("")

    @staticmethod
    def _timer_key(unit: str) -> str:
        return f"timer:{unit}"

    @staticmethod
    def _crontab_key(spec: str, command: str) -> str:
        return f"crontab:{spec}\n{command}"

    def _mark_pending(self, key: str) -> bool:
        if not key or key in self._pending_actions:
            return False
        self._pending_actions.add(key)
        self.pending_actions_changed.emit()
        return True

    def _clear_pending(self, key: str):
        if key not in self._pending_actions:
            return
        self._pending_actions.remove(key)
        self.pending_actions_changed.emit()

    def _on_add_crontab_result(self, result: object):
        if not self._adding_job:
            return
        self._set_adding_job(False)
        if not isinstance(result, dict):
            self._set_action_error("Invalid daemon response")
            return
        if "error" in result:
            self._set_action_error(_err_text(result))
            return
        self._set_action_message("Scheduled job added")
        self.jobAdded.emit()
        self._fetch()

    def _on_resource_action_result(
        self, key: str, result: object, success_message: str
    ):
        if key not in self._pending_actions:
            return
        self._clear_pending(key)
        if not isinstance(result, dict):
            self._set_action_error("Invalid daemon response")
            return
        if "error" in result:
            self._set_action_error(_err_text(result))
            return
        self._set_action_message(success_message)
        self._fetch()

    def _fetch(self):
        self._load_seq += 1
        seq = self._load_seq
        self._set_loading(True)
        self._set_load_error("")
        self._client.call("Crons", callback=lambda result: self._on_crons_result(result, seq))

    def _on_crons_result(self, result: object, seq: Optional[int] = None):
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
        crons = data.get("crons") if isinstance(data, dict) else []
        self._crons = [dict(cron) for cron in list(crons or []) if isinstance(cron, dict)]
        self._timers = int(data.get("timers") or 0) if isinstance(data, dict) else 0
        self._crontab = int(data.get("crontab") or 0) if isinstance(data, dict) else 0
        self._systemd_available = bool(data.get("systemdAvail")) if isinstance(data, dict) else False
        self.crons_changed.emit()
        self.summary_changed.emit()
        self._set_loading(False)
