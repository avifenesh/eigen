"""
live.py — LiveSessionsModel (filtered sessions: working + approval only).

Reuses SessionsModel infrastructure but filters to active sessions only
(working/approval), sorted by urgency (running first, then approval).
"""

import sys
from typing import Optional

from PySide6.QtCore import QAbstractListModel, QModelIndex, QObject, Qt, Slot

from eigenqt.rpc.client import RpcClient


class LiveSessionsModel(QAbstractListModel):
    """Live sessions model (filtered to working+approval, urgency sorted)."""

    # Qt roles (same as SessionsModel for consistency)
    IdRole = Qt.UserRole + 1
    TitleRole = Qt.UserRole + 2
    DirRole = Qt.UserRole + 3
    ModelRole = Qt.UserRole + 4
    StatusRole = Qt.UserRole + 5
    TurnsRole = Qt.UserRole + 6
    UpdatedRole = Qt.UserRole + 7

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._sessions: list[dict] = []

        # Connect to RPC + events
        self._client.connected.connect(self._on_connected)
        self._client.event.connect(self._on_event)

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.IdRole: b"sessionId",
            self.TitleRole: b"title",
            self.DirRole: b"dir",
            self.ModelRole: b"modelName",
            self.StatusRole: b"status",
            self.TurnsRole: b"turns",
            self.UpdatedRole: b"updated",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count (filtered sessions list length)."""
        if parent.isValid():
            return 0
        return len(self._sessions)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._sessions):
            return ""

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
            return session.get("status", "idle")
        if role == self.TurnsRole:
            return session.get("turns") or 0
        if role == self.UpdatedRole:
            return session.get("updated") or 0
        return ""

    @Slot()
    def _on_connected(self):
        """Fetch sessions on connect (async)."""
        self._client.call("Sessions", callback=self._on_sessions_result)

        # Subscribe to daemon stats + session events for live updates
        self._client.subscribe(["eigen:daemon:stats"])

    @Slot(dict)
    def _on_sessions_result(self, result: dict):
        """Handle Sessions RPC result, filter and sort."""
        if "error" in result:
            return

        sessions = result.get("result") or []

        # Filter to live sessions only (working or approval)
        filtered = [s for s in sessions if _is_live(s.get("status"))]

        # Sort by urgency: working=0, approval=1, then newest within each bucket
        rank = {"working": 0, "approval": 1}
        filtered.sort(key=lambda s: (rank.get(s.get("status"), 2), -(s.get("updated") or 0)))

        self.beginResetModel()
        self._sessions = filtered
        self.endResetModel()

    @Slot(str, dict)
    def _on_event(self, channel: str, data: dict):
        """
        Handle events that signal session-list changes.

        On any session-affecting event, refetch and refilter the list.
        """
        # Session events are "eigen:session:<id>:event"
        if channel.startswith("eigen:session:") and channel.endswith(":event"):
            event = data.get("event", {})
            if event.get("kind") == "done":
                # Turn finished → refetch to update turns count + status
                self._client.call("Sessions", callback=self._on_sessions_result)
        # Also refresh on daemon stats changes (session lifecycle)
        elif channel == "eigen:daemon:stats":
            self._client.call("Sessions", callback=self._on_sessions_result)

    def refresh(self):
        """Manually trigger a refresh (e.g., after RemoveSession, Interrupt)."""
        self._client.call("Sessions", callback=self._on_sessions_result)


def _is_live(status: str) -> bool:
    """Filter predicate: live = working or approval."""
    return status in ("working", "approval")


def filter_and_sort_live(sessions: list[dict]) -> list[dict]:
    """
    Pure function for testing: filter to live sessions, sort by urgency.

    Returns: filtered+sorted list (working first, then approval, newest within each).
    """
    filtered = [s for s in sessions if _is_live(s.get("status"))]
    rank = {"working": 0, "approval": 1}
    filtered.sort(key=lambda s: (rank.get(s.get("status"), 2), -(s.get("updated") or 0)))
    return filtered
