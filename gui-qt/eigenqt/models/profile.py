"""
profile.py - Profile usage and USER.md editor model.

Loads the durable observe summary plus the global memory profile while the route
is visible. The only write in this slice is WriteUserProfile, matching the
existing USER.md editor contract.
"""

from typing import Optional

from PySide6.QtCore import QObject, Property, QTimer, Signal, Slot

from eigenqt.rpc.client import RpcClient


def _err_text(result: dict | None) -> str:
    """Extract an RPC error message from string or object payloads."""
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


class ProfileModel(QObject):
    """Route-scoped usage summary plus global USER.md profile editor."""

    summary_changed = Signal()
    memory_changed = Signal()
    summary_loading_changed = Signal()
    memory_loading_changed = Signal()
    summary_error_changed = Signal()
    memory_error_changed = Signal()
    editing_profile_changed = Signal()
    profile_draft_changed = Signal()
    saving_profile_changed = Signal()
    action_error_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._summary: dict = {}
        self._memory: dict = {}
        self._summary_loading = False
        self._memory_loading = False
        self._summary_error = ""
        self._memory_error = ""
        self._editing_profile = False
        self._profile_draft = ""
        self._saving_profile = False
        self._action_error = ""
        self._active = False
        self._summary_seq = 0
        self._memory_seq = 0

        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)
        self._poll_timer.timeout.connect(self._fetch_summary)

        self._client.connected.connect(self._on_connected)

    @Property("QVariantMap", notify=summary_changed)
    def summary(self) -> dict:
        return self._summary

    @Property("QVariantMap", notify=memory_changed)
    def memory(self) -> dict:
        return self._memory

    @Property(bool, notify=summary_loading_changed)
    def summary_loading(self) -> bool:
        return self._summary_loading

    @Property(bool, notify=memory_loading_changed)
    def memory_loading(self) -> bool:
        return self._memory_loading

    @Property(str, notify=summary_error_changed)
    def summary_error(self) -> str:
        return self._summary_error

    @Property(str, notify=memory_error_changed)
    def memory_error(self) -> str:
        return self._memory_error

    @Property(str, notify=action_error_changed)
    def action_error(self) -> str:
        return self._action_error

    @Property(bool, notify=summary_changed)
    def summary_available(self) -> bool:
        return bool(self._summary.get("available"))

    @Property(int, notify=summary_changed)
    def records(self) -> int:
        return _as_int(self._summary.get("records"))

    @Property(list, notify=summary_changed)
    def models(self) -> list[dict]:
        return [dict(model) for model in self._summary.get("models") or [] if isinstance(model, dict)]

    @Property(int, notify=summary_changed)
    def in_tokens(self) -> int:
        return sum(_as_int(model.get("inTokens")) for model in self.models)

    @Property(int, notify=summary_changed)
    def out_tokens(self) -> int:
        return sum(_as_int(model.get("outTokens")) for model in self.models)

    @Property(int, notify=summary_changed)
    def cache_read_tokens(self) -> int:
        return sum(_as_int(model.get("cacheReadTokens")) for model in self.models)

    @Property(int, notify=summary_changed)
    def cache_write_tokens(self) -> int:
        return sum(_as_int(model.get("cacheWriteTokens")) for model in self.models)

    @Property(int, notify=summary_changed)
    def cache_hit(self) -> int:
        total = self.in_tokens + self.cache_read_tokens + self.cache_write_tokens
        if total <= 0:
            return 0
        return max(0, min(100, round((self.cache_read_tokens / total) * 100)))

    @Property(int, notify=summary_changed)
    def error_count(self) -> int:
        return sum(_as_int(item.get("count")) for item in self._summary.get("errors") or [])

    @Property(str, notify=memory_changed)
    def profile(self) -> str:
        return str(self._memory.get("profile") or "")

    @Property(str, notify=memory_changed)
    def profile_learned(self) -> str:
        return str(self._memory.get("profileLearned") or "")

    @Property(bool, notify=editing_profile_changed)
    def editing_profile(self) -> bool:
        return self._editing_profile

    @editing_profile.setter
    def editing_profile(self, value: bool):
        if self._editing_profile == value:
            return
        self._editing_profile = value
        self.editing_profile_changed.emit()

    @Property(str, notify=profile_draft_changed)
    def profile_draft(self) -> str:
        return self._profile_draft

    @profile_draft.setter
    def profile_draft(self, value: str):
        if self._profile_draft == value:
            return
        self._profile_draft = value
        self.profile_draft_changed.emit()

    @Property(bool, notify=saving_profile_changed)
    def saving_profile(self) -> bool:
        return self._saving_profile

    @saving_profile.setter
    def saving_profile(self, value: bool):
        if self._saving_profile == value:
            return
        self._saving_profile = value
        self.saving_profile_changed.emit()

    @Slot()
    def _on_connected(self):
        if self._active:
            self.start_polling()

    @Slot(bool)
    def set_active(self, active: bool):
        if self._active == active:
            return
        self._active = active
        if active:
            self.start_polling()
        else:
            self.stop_polling()

    @Slot()
    def load(self):
        self.refresh()

    @Slot()
    def refresh(self):
        self._fetch_summary()
        self._fetch_memory()

    @Slot()
    def start_edit(self):
        self.profile_draft = self.profile
        self._set_action_error("")
        self.editing_profile = True

    @Slot()
    def cancel_edit(self):
        if self.saving_profile:
            return
        self.profile_draft = self.profile
        self.editing_profile = False

    @Slot()
    def save_profile(self):
        if self.saving_profile:
            return
        self._set_action_error("")
        self.saving_profile = True
        self._client.call(
            "WriteUserProfile",
            self._profile_draft,
            callback=self._on_save_profile_result,
        )

    def start_polling(self):
        if not self._poll_timer.isActive():
            self.refresh()
            self._poll_timer.start()

    def stop_polling(self):
        self._poll_timer.stop()
        self._summary_seq += 1
        self._memory_seq += 1

    def _set_summary_loading(self, value: bool):
        if self._summary_loading == value:
            return
        self._summary_loading = value
        self.summary_loading_changed.emit()

    def _set_memory_loading(self, value: bool):
        if self._memory_loading == value:
            return
        self._memory_loading = value
        self.memory_loading_changed.emit()

    def _set_summary_error(self, value: str):
        if self._summary_error == value:
            return
        self._summary_error = value
        self.summary_error_changed.emit()

    def _set_memory_error(self, value: str):
        if self._memory_error == value:
            return
        self._memory_error = value
        self.memory_error_changed.emit()

    def _set_action_error(self, value: str):
        if self._action_error == value:
            return
        self._action_error = value
        self.action_error_changed.emit()

    def _fetch_summary(self):
        self._summary_seq += 1
        seq = self._summary_seq
        self._set_summary_loading(True)
        self._set_summary_error("")
        self._client.call("ObserveSummary", 5000, callback=lambda result: self._on_summary_result(result, seq))

    def _fetch_memory(self):
        self._memory_seq += 1
        seq = self._memory_seq
        self._set_memory_loading(True)
        self._set_memory_error("")
        self._client.call("MemoryForScope", "global", callback=lambda result: self._on_memory_result(result, seq))

    def _on_summary_result(self, result: dict, seq: Optional[int] = None):
        if seq is not None and seq != self._summary_seq:
            return
        self._set_summary_loading(False)
        if "error" in result:
            self._set_summary_error(_err_text(result))
            return
        data = result.get("result") or {}
        self._summary = dict(data) if isinstance(data, dict) else {}
        self.summary_changed.emit()

    def _on_memory_result(self, result: dict, seq: Optional[int] = None):
        if seq is not None and seq != self._memory_seq:
            return
        self._set_memory_loading(False)
        if "error" in result:
            self._set_memory_error(_err_text(result))
            return
        data = result.get("result") or {}
        self._memory = dict(data) if isinstance(data, dict) else {}
        self.memory_changed.emit()

    def _on_save_profile_result(self, result: dict):
        self.saving_profile = False
        if "error" in result:
            self.editing_profile = True
            self._set_action_error(f"Could not save profile: {_err_text(result)}")
            return

        next_memory = dict(self._memory)
        next_memory["profile"] = self._profile_draft
        self._memory = next_memory
        self.memory_changed.emit()
        self.editing_profile = False
        self._fetch_memory()
