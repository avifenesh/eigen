"""
sessions.py — SessionsModel (QAbstractListModel) for session list view.

Populated by RPC Sessions; live-updated from subscribed events. Sort: newest-updated first.
"""

import sys
from typing import Optional

from PySide6.QtCore import QAbstractListModel, QModelIndex, QObject, Qt, Slot

from eigenqt.rpc import RpcClient


class SessionsModel(QAbstractListModel):
    """Sessions list model (id, title, dir, model, status, turns, updated, unread)."""

    # Qt roles
    IdRole = Qt.UserRole + 1
    TitleRole = Qt.UserRole + 2
    DirRole = Qt.UserRole + 3
    ModelRole = Qt.UserRole + 4
    StatusRole = Qt.UserRole + 5
    TurnsRole = Qt.UserRole + 6
    UpdatedRole = Qt.UserRole + 7
    UnreadRole = Qt.UserRole + 8

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._sessions: list[dict] = []
        self._unread: set[str] = set()  # Session IDs with unread status

        # Connect to RPC + events
        self._client.connected.connect(self._on_connected)
        self._client.event.connect(self._on_event)

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.IdRole: b"sessionId",
            self.TitleRole: b"title",
            self.DirRole: b"dir",
            self.ModelRole: b"modelName",  # Avoid 'model' keyword conflict in QML
            self.StatusRole: b"status",
            self.TurnsRole: b"turns",
            self.UpdatedRole: b"updated",
            self.UnreadRole: b"unread",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count (sessions list length)."""
        if parent.isValid():
            return 0
        return len(self._sessions)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._sessions):
            return ""  # Return empty string instead of None

        session = self._sessions[index.row()]
        if role == self.IdRole:
            return session.get("id", "")
        if role == self.TitleRole:
            return session.get("title", "")
        if role == self.DirRole:
            return session.get("dir", "")
        if role == self.ModelRole:
            return session.get("model", "")
        if role == self.StatusRole:
            return session.get("status", "idle")  # Default status
        if role == self.TurnsRole:
            return session.get("turns", 0)
        if role == self.UpdatedRole:
            return session.get("updated", 0)
        if role == self.UnreadRole:
            session_id = session.get("id", "")
            return session_id in self._unread
        return ""  # Return empty string for unknown roles

    @Slot()
    def _on_connected(self):
        """Fetch sessions on connect (async)."""
        self._client.call("Sessions", callback=self._on_sessions_result)

        # Subscribe to daemon stats to get global session list updates
        self._client.subscribe(["eigen:daemon:stats"])

    @Slot(dict)
    def _on_sessions_result(self, result: dict):
        """Handle Sessions RPC result (list of SessionInfoDTO)."""
        if "error" in result:
            return

        sessions = result.get("result", [])
        # Sort by updated descending (newest first)
        sessions.sort(key=lambda s: s.get("updated", 0), reverse=True)

        self.beginResetModel()
        self._sessions = sessions
        self.endResetModel()

    @Slot(str, dict)
    def _on_event(self, channel: str, data: dict):
        """
        Handle events that signal session-list changes.

        Events that affect sessions list (from Svelte stores/sessions.svelte.ts):
        - "eigen:session:*:event" with event.kind == "done" → refetch (turn finished, turns incremented)
        - State changes from other RPCs (SetTitle, etc.) → partial update

        For simplicity: on any session-affecting event, refetch the list.
        """
        # Session events are "eigen:session:<id>:event"
        if channel.startswith("eigen:session:") and channel.endswith(":event"):
            event = data.get("event", {})
            if event.get("kind") == "done":
                # Turn finished → refetch to update turns count
                self._client.call("Sessions", callback=self._on_sessions_result)

    def refresh(self):
        """Manually trigger a refresh (e.g., after RemoveSession)."""
        self._client.call("Sessions", callback=self._on_sessions_result)

    def mark_unread(self, session_id: str):
        """Mark a session as unread."""
        if session_id and session_id not in self._unread:
            self._unread.add(session_id)
            # Notify QML of change
            self._notify_unread_changed(session_id)

    def mark_read(self, session_id: str):
        """Mark a session as read (clear unread)."""
        if session_id and session_id in self._unread:
            self._unread.discard(session_id)
            # Notify QML of change
            self._notify_unread_changed(session_id)

    def _notify_unread_changed(self, session_id: str):
        """Emit dataChanged for a session's unread role."""
        for row in range(len(self._sessions)):
            if self._sessions[row].get("id") == session_id:
                idx = self.index(row, 0)
                self.dataChanged.emit(idx, idx, [self.UnreadRole])
                break
