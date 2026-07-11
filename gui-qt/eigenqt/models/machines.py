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
    installing_changed = Signal()
    install_message_changed = Signal()
    install_error_changed = Signal()
    saving_machine_changed = Signal()
    save_error_changed = Signal()
    machineSaved = Signal()
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
        self._installing = False
        self._install_message = ""
        self._install_error = ""
        self._saving_machine = False
        self._save_error = ""
        self._active = False
        self._load_seq = 0
        self._remote_seq = 0
        self._install_seq = 0
        self._save_seq = 0

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

    @Property(bool, notify=installing_changed)
    def installing(self) -> bool:
        return self._installing

    @Property(str, notify=install_message_changed)
    def install_message(self) -> str:
        return self._install_message

    @Property(str, notify=install_error_changed)
    def install_error(self) -> str:
        return self._install_error

    @Property(bool, notify=saving_machine_changed)
    def saving_machine(self) -> bool:
        return self._saving_machine

    @Property(str, notify=save_error_changed)
    def save_error(self) -> str:
        return self._save_error

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

    @Slot(str, bool)
    def install_machine(self, ssh: str, push_creds: bool = True):
        """Bootstrap selected host through the established remote CLI flow."""
        ssh = str(ssh or "").strip()
        if not ssh or self._installing:
            return

        self._install_seq += 1
        seq = self._install_seq
        self._set_installing(True)
        self._set_install_message("")
        self._set_install_error("")
        self._client.call(
            "InstallRemote",
            ssh,
            bool(push_creds),
            callback=lambda result: self._on_install_result(result, ssh, seq),
        )

    @Slot(str, str, str, bool, bool)
    def save_machine(self, name: str, ssh: str, remote_dir: str, install: bool, push_creds: bool):
        """Save an SSH target, optionally launching the established installer."""
        if self._saving_machine:
            return
        self._save_seq += 1
        seq = self._save_seq
        self._set_saving_machine(True)
        self._set_save_error("")
        self._client.call(
            "SaveRemoteMachine",
            str(name or "").strip(),
            str(ssh or "").strip(),
            str(remote_dir or "").strip(),
            callback=lambda result: self._on_save_machine_result(result, bool(install), bool(push_creds), seq),
        )

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

    def _set_installing(self, value: bool):
        if self._installing == value:
            return
        self._installing = value
        self.installing_changed.emit()

    def _set_install_message(self, value: str):
        if self._install_message == value:
            return
        self._install_message = value
        self.install_message_changed.emit()

    def _set_install_error(self, value: str):
        if self._install_error == value:
            return
        self._install_error = value
        self.install_error_changed.emit()

    def _set_saving_machine(self, value: bool):
        if self._saving_machine == value:
            return
        self._saving_machine = value
        self.saving_machine_changed.emit()

    def _set_save_error(self, value: str):
        if self._save_error == value:
            return
        self._save_error = value
        self.save_error_changed.emit()

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

    def _on_install_result(self, result: dict, ssh: str, seq: int):
        if seq != self._install_seq:
            return
        self._set_installing(False)
        if "error" in result:
            self._set_install_error(_err_text(result))
            return

        self._set_install_error("")
        self._set_install_message(str(result.get("result") or f"Eigen installed on {ssh}"))
        if str(self._selected_machine.get("ssh") or "") == ssh:
            self.select_machine(ssh)

    def _on_save_machine_result(self, result: dict, install: bool, push_creds: bool, seq: int):
        if seq != self._save_seq:
            return
        self._set_saving_machine(False)
        if "error" in result:
            self._set_save_error(_err_text(result))
            return

        machine = result.get("result") or {}
        if not isinstance(machine, dict) or not str(machine.get("ssh") or ""):
            self._set_save_error("Saved machine response was incomplete")
            return

        saved = dict(machine)
        name = str(saved.get("name") or "")
        ssh = str(saved.get("ssh") or "")
        existing = next(
            (
                item for item in self._machines
                if str(item.get("name") or "") == name or str(item.get("ssh") or "") == ssh
            ),
            None,
        )
        if existing is not None:
            merged = dict(existing)
            merged.update(saved)
            saved = merged
        self._machines = [
            item for item in self._machines
            if str(item.get("name") or "") != name and str(item.get("ssh") or "") != ssh
        ]
        self._machines.append(saved)
        self._machines.sort(key=lambda item: str(item.get("name") or item.get("ssh") or "").lower())
        self.machines_changed.emit()
        self.summary_changed.emit()
        self._set_save_error("")
        self._set_selected_machine(saved)
        self._set_remote_sessions([])
        self._set_remote_error("")
        self.machineSaved.emit()

        if install:
            self.install_machine(ssh, push_creds)
        else:
            self.select_machine(ssh)
