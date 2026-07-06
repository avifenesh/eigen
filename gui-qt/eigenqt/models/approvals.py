"""
approvals.py — ApprovalsModel for pending approvals in a session.

Driven from session:<id> event stream (approval events) + State.pending seed.
Expose approve(id, allow) → RPC Approve.
"""

from typing import Optional

from PySide6.QtCore import QAbstractListModel, QModelIndex, QObject, Qt, Signal, Slot

from eigenqt.rpc import RpcClient


class ApprovalsModel(QAbstractListModel):
    """Pending approvals model for a session (id, tool, args summary)."""

    actionError = Signal(str)

    # Qt roles
    IdRole = Qt.UserRole + 1
    ToolRole = Qt.UserRole + 2
    ArgsRole = Qt.UserRole + 3
    ApprovingRole = Qt.UserRole + 4
    ErrorRole = Qt.UserRole + 5

    def __init__(self, client: RpcClient, session_id: str, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._session_id = session_id
        self._approvals: list[dict] = []
        self._approving_ids: set[str] = set()
        self._errors: dict[str, str] = {}

        # Event channel for this session
        self._event_channel = f"session:{session_id}" if session_id else ""

        # Connect to events
        self._client.event.connect(self._on_event)

        # Subscribe to session events once a real session is attached.
        if self._event_channel:
            self._client.subscribe([self._event_channel])

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.IdRole: b"id",
            self.ToolRole: b"tool",
            self.ArgsRole: b"args",
            self.ApprovingRole: b"approving",
            self.ErrorRole: b"error",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count (approvals list length)."""
        if parent.isValid():
            return 0
        return len(self._approvals)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._approvals):
            return None

        approval = self._approvals[index.row()]
        if role == self.IdRole:
            return approval.get("id", "")
        if role == self.ToolRole:
            return approval.get("tool", "")
        if role == self.ArgsRole:
            return approval.get("args", "")
        if role == self.ApprovingRole:
            return approval.get("id", "") in self._approving_ids
        if role == self.ErrorRole:
            return self._errors.get(approval.get("id", ""), "")
        return None

    def seed(self, state: dict):
        """
        Seed approvals from State RPC result (initial load).

        State.pending is []ApprovalInfo {id, tool, args}.
        """
        pending = state.get("pending") or []
        pending_ids = {str(approval.get("id", "")) for approval in pending}
        self.beginResetModel()
        self._approvals = [self._normalize_approval(approval) for approval in pending]
        self._approving_ids.intersection_update(pending_ids)
        self._errors = {
            approval_id: error
            for approval_id, error in self._errors.items()
            if approval_id in pending_ids
        }
        self.endResetModel()

    @Slot()
    def clearRows(self) -> None:
        """Clear visible approval rows when the active chat detaches."""
        self.beginResetModel()
        self._approvals.clear()
        self._approving_ids.clear()
        self._errors.clear()
        self.endResetModel()

    @Slot(str, dict)
    def _on_event(self, channel: str, data: dict):
        """
        Handle session:<id> events (StreamEventDTO).

        Approval event: {kind: "approval", text: "tool args", result: approval_id}
        Add to pending list.
        """
        if channel != self._event_channel:
            return

        event = data.get("event", {})
        if event.get("kind") != "approval":
            return

        approval = self._normalize_approval(
            {
                "id": event.get("result", ""),  # result field = approval ID
                "tool": event.get("tool", ""),
                "args": event.get("text", ""),  # text field = "tool args"
            }
        )

        existing = self._index_for_id(approval["id"])
        if existing >= 0:
            self._approvals[existing] = approval
            self.dataChanged.emit(
                self.index(existing, 0),
                self.index(existing, 0),
                [self.ToolRole, self.ArgsRole, self.ErrorRole],
            )
            return

        # Insert at end
        row = len(self._approvals)
        self.beginInsertRows(QModelIndex(), row, row)
        self._approvals.append(approval)
        self.endInsertRows()

    @Slot(str, bool)
    def approve(self, approval_id: str, allow: bool):
        """
        Approve or deny an approval (RPC Approve).

        Args: session_id, approval_id, allow (bool).
        On success, remove from pending list.
        """
        if not approval_id or approval_id in self._approving_ids:
            return

        self._approving_ids.add(approval_id)
        self._errors.pop(approval_id, None)
        self._emit_row_changed(approval_id, [self.ApprovingRole, self.ErrorRole])

        def on_result(result: dict):
            self._approving_ids.discard(approval_id)
            error = result.get("error") if isinstance(result, dict) else None
            if error:
                message = str(error)
                self._errors[approval_id] = message
                self.actionError.emit(message)
                self._emit_row_changed(approval_id, [self.ApprovingRole, self.ErrorRole])
                return

            row = self._index_for_id(approval_id)
            if row >= 0:
                self.beginRemoveRows(QModelIndex(), row, row)
                del self._approvals[row]
                self._errors.pop(approval_id, None)
                self.endRemoveRows()

        self._client.call(
            "Approve", self._session_id, approval_id, allow, callback=on_result
        )

    def detach(self):
        """Detach from session (unsubscribe)."""
        if self._event_channel:
            self._client.unsubscribe([self._event_channel])
        try:
            self._client.event.disconnect(self._on_event)
        except (RuntimeError, TypeError):
            pass

    def _normalize_approval(self, approval: dict) -> dict:
        return {
            "id": str(approval.get("id", "")),
            "tool": str(approval.get("tool", "")),
            "args": str(approval.get("args", "")),
        }

    def _index_for_id(self, approval_id: str) -> int:
        for i, approval in enumerate(self._approvals):
            if approval.get("id") == approval_id:
                return i
        return -1

    def _emit_row_changed(self, approval_id: str, roles: list[int]) -> None:
        row = self._index_for_id(approval_id)
        if row < 0:
            return
        self.dataChanged.emit(self.index(row, 0), self.index(row, 0), roles)
