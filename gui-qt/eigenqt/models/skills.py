"""
skills.py — Skills view models (SkillsModel for active skills, ProposalsModel for dream proposals).

SkillsModel exposes all active skills (user/project/extra) via QAbstractListModel.
ProposalsModel exposes dream-proposed skills awaiting review (accept/reject).
Both poll Skills() RPC every 60s while the Skills route is active.
"""

from typing import Optional

from PySide6.QtCore import (
    QAbstractListModel,
    QModelIndex,
    QObject,
    QTimer,
    Qt,
    Property,
    Signal,
    Slot,
)

from eigenqt.rpc import RpcClient


def _err_text(result: dict) -> str:
    """Extract a displayable RPC error from either string or envelope-shaped errors."""
    error = result.get("error")
    if isinstance(error, str):
        return error or "Unknown error"
    if isinstance(error, dict):
        return error.get("message", "Unknown error")
    return str(error) if error else "Unknown error"


class SkillsModel(QAbstractListModel):
    """
    Active skills model — user/project/extra grouped skills.

    Roles: name, description, source, path (for preview slide-over).
    """

    # Qt roles
    NameRole = Qt.UserRole + 1
    DescriptionRole = Qt.UserRole + 2
    SourceRole = Qt.UserRole + 3
    PathRole = Qt.UserRole + 4
    load_error_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._skills: list[dict] = []
        self._active = False
        self._load_seq = 0
        self._load_error = ""
        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)  # 60s
        self._poll_timer.timeout.connect(self._fetch_skills)

        self._client.connected.connect(self._on_connected)

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.NameRole: b"name",
            self.DescriptionRole: b"description",
            self.SourceRole: b"source",
            self.PathRole: b"path",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count."""
        if parent.isValid():
            return 0
        return len(self._skills)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._skills):
            return ""

        skill = self._skills[index.row()]
        if role == self.NameRole:
            return skill.get("name", "")
        if role == self.DescriptionRole:
            return skill.get("description", "")
        if role == self.SourceRole:
            return skill.get("source", "")
        if role == self.PathRole:
            return skill.get("path", "")
        return ""

    @Property(str, notify=load_error_changed)
    def load_error(self) -> str:
        return self._load_error

    @Slot()
    def _on_connected(self):
        """Fetch skills on connect only while the route is active."""
        if self._active:
            self.start_polling()

    def _fetch_skills(self):
        """Async fetch Skills RPC."""
        self._load_seq += 1
        seq = self._load_seq
        self._set_load_error("")
        self._client.call("Skills", callback=lambda result: self._on_skills_result(result, seq))

    @Slot(dict)
    def _on_skills_result(self, result: dict, seq: Optional[int] = None):
        """Handle Skills RPC result."""
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            self._set_load_error(_err_text(result))
            return

        data = result.get("result") or {}
        skills = data.get("skills") or []

        self.beginResetModel()
        self._skills = skills
        self.endResetModel()

    @Slot()
    def refresh(self):
        """Manually refresh skills after a user action."""
        self._fetch_skills()

    @Slot(bool)
    def set_active(self, active: bool):
        """Start/stop route-scoped polling."""
        if self._active == active:
            return
        self._active = active
        if active:
            self.start_polling()
        else:
            self.stop_polling()

    def stop_polling(self):
        """Stop polling when view is inactive."""
        self._poll_timer.stop()
        self._load_seq += 1

    def start_polling(self):
        """Resume polling when view becomes active."""
        if not self._poll_timer.isActive():
            self._fetch_skills()
            self._poll_timer.start()

    def _set_load_error(self, value: str):
        if self._load_error == value:
            return
        self._load_error = value
        self.load_error_changed.emit()


class ProposalsModel(QAbstractListModel):
    """
    Dream-proposed skills model — proposals awaiting accept/reject.

    Roles: name, description, path.
    """

    # Qt roles
    NameRole = Qt.UserRole + 1
    DescriptionRole = Qt.UserRole + 2
    PathRole = Qt.UserRole + 3
    proposal_done = Signal(str, str, bool, str)  # (name, action, success, error_msg)
    load_error_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._proposals: list[dict] = []
        self._active = False
        self._load_seq = 0
        self._load_error = ""
        self._pending_actions: dict[str, str] = {}
        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)  # 60s
        self._poll_timer.timeout.connect(self._fetch_proposals)

        self._client.connected.connect(self._on_connected)

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.NameRole: b"name",
            self.DescriptionRole: b"description",
            self.PathRole: b"path",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count."""
        if parent.isValid():
            return 0
        return len(self._proposals)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._proposals):
            return ""

        proposal = self._proposals[index.row()]
        if role == self.NameRole:
            return proposal.get("name", "")
        if role == self.DescriptionRole:
            return proposal.get("description", "")
        if role == self.PathRole:
            return proposal.get("path", "")
        return ""

    @Property(str, notify=load_error_changed)
    def load_error(self) -> str:
        return self._load_error

    @Slot()
    def _on_connected(self):
        """Fetch proposals on connect only while the route is active."""
        if self._active:
            self.start_polling()

    def _fetch_proposals(self):
        """Async fetch Skills RPC (extract proposals)."""
        self._load_seq += 1
        seq = self._load_seq
        self._set_load_error("")
        self._client.call("Skills", callback=lambda result: self._on_skills_result(result, seq))

    @Slot(dict)
    def _on_skills_result(self, result: dict, seq: Optional[int] = None):
        """Handle Skills RPC result (extract proposals)."""
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            self._set_load_error(_err_text(result))
            return

        data = result.get("result") or {}
        proposals = data.get("proposals") or []

        self.beginResetModel()
        self._proposals = proposals
        self.endResetModel()

    @Slot(str)
    def accept(self, name: str):
        """Accept a proposal and remove it only after the RPC succeeds."""
        if not self._mark_pending(name, "accept"):
            return
        self._client.call(
            "AcceptSkill",
            name,
            callback=lambda r: self._on_action_result(name, "accept", r),
        )

    @Slot(str)
    def reject(self, name: str):
        """Reject a proposal and remove it only after the RPC succeeds."""
        if not self._mark_pending(name, "reject"):
            return
        self._client.call(
            "RejectSkill",
            name,
            callback=lambda r: self._on_action_result(name, "reject", r),
        )

    def _on_action_result(self, name: str, action: str, result: dict):
        """Handle proposal accept/reject completion."""
        self._clear_pending(name, action)
        if "error" in result:
            self.proposal_done.emit(name, action, False, _err_text(result))
            return

        self._remove_proposal(name)
        self.proposal_done.emit(name, action, True, "")

    def _remove_proposal(self, name: str) -> None:
        """Remove a proposal by name if it is still present."""
        for i, proposal in enumerate(self._proposals):
            if proposal.get("name") == name:
                self.beginRemoveRows(QModelIndex(), i, i)
                del self._proposals[i]
                self.endRemoveRows()
                break

    def _mark_pending(self, name: str, action: str) -> bool:
        name = (name or "").strip()
        if not name or name in self._pending_actions:
            return False
        self._pending_actions[name] = action
        return True

    def _clear_pending(self, name: str, action: str) -> None:
        if self._pending_actions.get(name) == action:
            del self._pending_actions[name]

    @Slot()
    def refresh(self):
        """Manually refresh proposals after a user action."""
        self._fetch_proposals()

    @Slot(bool)
    def set_active(self, active: bool):
        """Start/stop route-scoped polling."""
        if self._active == active:
            return
        self._active = active
        if active:
            self.start_polling()
        else:
            self.stop_polling()

    def stop_polling(self):
        """Stop polling when view is inactive."""
        self._poll_timer.stop()
        self._load_seq += 1

    def start_polling(self):
        """Resume polling when view becomes active."""
        if not self._poll_timer.isActive():
            self._fetch_proposals()
            self._poll_timer.start()

    def _set_load_error(self, value: str):
        if self._load_error == value:
            return
        self._load_error = value
        self.load_error_changed.emit()
