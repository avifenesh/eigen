"""
machines.py - Remote machines view model.

Loads the fast local Machines RPC and dials RemoteSessions only after the user
selects a host. The slow drill-in has its own sequence token so closing or
switching hosts drops late ssh results.
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


class MachinesModel(QObject):
    """Remote host list plus on-demand remote session drill-in."""

    machines_changed = Signal()
    loading_changed = Signal()
    load_error_changed = Signal()
    selected_machine_changed = Signal()
    remote_sessions_changed = Signal()
    remote_loading_changed = Signal()
    remote_error_changed = Signal()
    summary_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._machines: list[dict] = []
        self._loading = False
        self._load_error = ""
        self._selected_machine: dict = {}
        self._remote_sessions: list[dict] = []
        self._remote_loading = False
        self._remote_error = ""
        self._active = False
        self._load_seq = 0
        self._remote_seq = 0

        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)
        self._poll_timer.timeout.connect(self._fetch)

        self._client.connected.connect(self._on_connected)

    @Property(list, notify=machines_changed)
    def machines(self) -> list[dict]:
        return self._machines

    @Property(bool, notify=loading_changed)
    def loading(self) -> bool:
        return self._loading

    @Property(str, notify=load_error_changed)
    def load_error(self) -> str:
        return self._load_error

    @Property("QVariantMap", notify=selected_machine_changed)
    def selected_machine(self) -> dict:
        return self._selected_machine

    @Property(list, notify=remote_sessions_changed)
    def remote_sessions(self) -> list[dict]:
        return self._remote_sessions

    @Property(bool, notify=remote_loading_changed)
    def remote_loading(self) -> bool:
        return self._remote_loading

    @Property(str, notify=remote_error_changed)
    def remote_error(self) -> str:
        return self._remote_error

    @Property(int, notify=summary_changed)
    def machine_count(self) -> int:
        return len(self._machines)

    @Property(int, notify=summary_changed)
    def saved_count(self) -> int:
        return sum(1 for machine in self._machines if bool(machine.get("saved")))

    @Property(int, notify=summary_changed)
    def detected_count(self) -> int:
        return sum(1 for machine in self._machines if bool(machine.get("detected")))

    @Property(int, notify=summary_changed)
    def remote_count(self) -> int:
        return len(self._remote_sessions)

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
            self.clear_selection()

    @Slot(str)
    def select_machine(self, ssh: str):
        ssh = str(ssh or "")
        machine = next((m for m in self._machines if str(m.get("ssh") or "") == ssh), None)
        if machine is None:
            self.clear_selection()
            return

        self._remote_seq += 1
        seq = self._remote_seq
        self._set_selected_machine(dict(machine))
        self._set_remote_sessions([])
        self._set_remote_error("")
        self._set_remote_loading(True)
        self._client.call("RemoteSessions", ssh, callback=lambda result: self._on_remote_result(result, seq))

    @Slot()
    def clear_selection(self):
        self._remote_seq += 1
        self._set_selected_machine({})
        self._set_remote_sessions([])
        self._set_remote_error("")
        self._set_remote_loading(False)

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

    def _set_selected_machine(self, value: dict):
        if self._selected_machine == value:
            return
        self._selected_machine = value
        self.selected_machine_changed.emit()

    def _set_remote_sessions(self, value: list[dict]):
        if self._remote_sessions == value:
            return
        self._remote_sessions = value
        self.remote_sessions_changed.emit()
        self.summary_changed.emit()

    def _set_remote_loading(self, value: bool):
        if self._remote_loading == value:
            return
        self._remote_loading = value
        self.remote_loading_changed.emit()

    def _set_remote_error(self, value: str):
        if self._remote_error == value:
            return
        self._remote_error = value
        self.remote_error_changed.emit()

    def _fetch(self):
        self._load_seq += 1
        seq = self._load_seq
        self._set_loading(True)
        self._set_load_error("")
        self._client.call("Machines", callback=lambda result: self._on_machines_result(result, seq))

    def _on_machines_result(self, result: dict, seq: Optional[int] = None):
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            self._set_loading(False)
            self._set_load_error(_err_text(result))
            return

        data = result.get("result") or {}
        machines = data.get("machines") if isinstance(data, dict) else []
        self._machines = [dict(machine) for machine in list(machines or []) if isinstance(machine, dict)]
        self.machines_changed.emit()
        self.summary_changed.emit()
        self._set_loading(False)

        selected_ssh = str(self._selected_machine.get("ssh") or "")
        if selected_ssh:
            selected = next(
                (machine for machine in self._machines if str(machine.get("ssh") or "") == selected_ssh),
                None,
            )
            if selected is None:
                self.clear_selection()
            else:
                self._set_selected_machine(dict(selected))

    def _on_remote_result(self, result: dict, seq: Optional[int] = None):
        if seq is not None and seq != self._remote_seq:
            return
        if "error" in result:
            self._set_remote_loading(False)
            self._set_remote_sessions([])
            self._set_remote_error(_err_text(result))
            return

        sessions = result.get("result") or []
        self._set_remote_sessions(
            [dict(session) for session in list(sessions or []) if isinstance(session, dict)]
        )
        self._set_remote_error("")
        self._set_remote_loading(False)
