"""
approvals.py — ApprovalsModel for pending approvals in a session.

Driven from session:<id> event stream (approval events) + State.pending seed.
Expose approve(id, allow) → RPC Approve.
"""

from typing import Optional

from PySide6.QtCore import QAbstractListModel, QModelIndex, QObject, Qt, Slot

from eigenqt.rpc import RpcClient


class ApprovalsModel(QAbstractListModel):
    """Pending approvals model for a session (id, tool, args summary)."""

    # Qt roles
    IdRole = Qt.UserRole + 1
    ToolRole = Qt.UserRole + 2
    ArgsRole = Qt.UserRole + 3

    def __init__(self, client: RpcClient, session_id: str, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._session_id = session_id
        self._approvals: list[dict] = []

        # Event channel for this session
        self._event_channel = f"session:{session_id}"

        # Connect to events
        self._client.event.connect(self._on_event)

        # Subscribe to session events
        self._client.subscribe([self._event_channel])

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.IdRole: b"id",
            self.ToolRole: b"tool",
            self.ArgsRole: b"args",
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
        return None

    def seed(self, state: dict):
        """
        Seed approvals from State RPC result (initial load).

        State.pending is []ApprovalInfo {id, tool, args}.
        """
        pending = state.get("pending", [])
        self.beginResetModel()
        self._approvals = pending
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

        # New approval
        approval = {
            "id": event.get("result", ""),  # result field = approval ID
            "tool": event.get("tool", ""),
            "args": event.get("text", ""),  # text field = "tool args"
        }

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

        def on_result(result: dict):
            if "error" not in result:
                # Remove from list
                for i, appr in enumerate(self._approvals):
                    if appr.get("id") == approval_id:
                        self.beginRemoveRows(QModelIndex(), i, i)
                        del self._approvals[i]
                        self.endRemoveRows()
                        break

        self._client.call(
            "Approve", self._session_id, approval_id, allow, callback=on_result
        )

    def detach(self):
        """Detach from session (unsubscribe)."""
        self._client.unsubscribe([self._event_channel])
