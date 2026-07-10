"""
sessions.py — SessionsModel (QAbstractListModel) for session list view.

Populated by RPC Sessions; live-updated from subscribed events. Sort: newest-updated first.
"""

import sys
from typing import Any, Optional

from PySide6.QtCore import Property, QAbstractListModel, QModelIndex, QObject, Qt, Signal, Slot

from eigenqt.rpc import RpcClient


def _err_text(value: Any) -> str:
    """Extract a readable RPC error string."""
    if isinstance(value, dict):
        nested = value.get("message") or value.get("error")
        if nested is not None and nested is not value:
            return _err_text(nested)
        return "Unknown error"
    return str(value) if value else "Unknown error"


class SessionsModel(QAbstractListModel):
    """Sessions list model (id, title, dir, model, status, turns, updated, unread)."""

    pruningChanged = Signal()
    removingChanged = Signal()
    exportingChanged = Signal()
    actionErrorChanged = Signal()
    actionMessageChanged = Signal()
    queryChanged = Signal()
    totalCountChanged = Signal()
    filteredCountChanged = Signal()
    sessionEntriesChanged = Signal()

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
        self._all_sessions: list[dict] = []
        self._sessions: list[dict] = []
        self._unread: set[str] = set()  # Session IDs with unread status
        self._removing: set[str] = set()
        self._exporting: set[str] = set()
        self._pruning = False
        self._action_error = ""
        self._action_message = ""
        self._query = ""
        self._load_seq = 0

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
            return session.get("turns") or 0
        if role == self.UpdatedRole:
            return session.get("updated") or 0
        if role == self.UnreadRole:
            session_id = session.get("id", "")
            return session_id in self._unread
        return ""  # Return empty string for unknown roles

    @Property(bool, notify=pruningChanged)
    def pruning(self) -> bool:
        """Whether PruneSessions is in flight."""
        return self._pruning

    @Property(list, notify=removingChanged)
    def removing(self) -> list[str]:
        """Session IDs currently being removed."""
        return sorted(self._removing)

    @Property(list, notify=exportingChanged)
    def exporting(self) -> list[str]:
        """Session IDs currently being exported."""
        return sorted(self._exporting)

    @Property(str, notify=actionErrorChanged)
    def actionError(self) -> str:
        """Last remove/prune error for the sessions view."""
        return self._action_error

    @Property(str, notify=actionMessageChanged)
    def actionMessage(self) -> str:
        """Last successful session action message for the sessions view."""
        return self._action_message

    @Property(str, notify=queryChanged)
    def query(self) -> str:
        """Current title/directory/model filter."""
        return self._query

    @query.setter
    def query(self, value: str):
        value = (value or "").strip()
        if value == self._query:
            return
        self._query = value
        self.queryChanged.emit()
        self._apply_filter()

    @Property(int, notify=totalCountChanged)
    def totalCount(self) -> int:
        """Total sessions before the active filter."""
        return len(self._all_sessions)

    @Property(int, notify=filteredCountChanged)
    def filteredCount(self) -> int:
        """Sessions matching the active filter."""
        return len(self._sessions)

    @Property("QVariantList", notify=sessionEntriesChanged)
    def session_entries(self) -> list[dict]:
        """All session rows for global navigation, independent of the screen filter."""
        return [
            {
                **session,
                "unread": str(session.get("id") or "") in self._unread,
            }
            for session in self._all_sessions
        ]

    @Slot()
    def _on_connected(self):
        """Fetch sessions on connect (async)."""
        self._fetch_sessions()

        # Subscribe to daemon stats to get global session list updates
        self._client.subscribe(["eigen:daemon:stats"])

    @Slot(dict)
    def _on_sessions_result(self, result: dict, seq: Optional[int] = None):
        """Handle Sessions RPC result (list of SessionInfoDTO)."""
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            return

        sessions = result.get("result") or []
        # Sort by updated descending (newest first)
        sessions.sort(key=lambda s: (s.get("updated") or 0), reverse=True)

        old_total = len(self._all_sessions)
        self.beginResetModel()
        self._all_sessions = sessions
        self._sessions = self._filtered_sessions()
        self.endResetModel()
        if old_total != len(self._all_sessions):
            self.totalCountChanged.emit()
        self.filteredCountChanged.emit()

        existing_ids = {session.get("id", "") for session in sessions}
        self._unread.intersection_update(existing_ids)
        self.sessionEntriesChanged.emit()

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
                self._fetch_sessions()

    @Slot()
    def refresh(self):
        """Manually trigger a refresh (e.g., after RemoveSession)."""
        self._fetch_sessions()

    def _fetch_sessions(self):
        """Fetch the session list, ignoring replies from older refreshes."""
        self._load_seq += 1
        seq = self._load_seq
        self._client.call("Sessions", callback=lambda result: self._on_sessions_result(result, seq))

    @Slot(str, result=bool)
    def isRemoving(self, session_id: str) -> bool:
        """Return whether a session remove call is currently in flight."""
        return session_id in self._removing

    @Slot(str, result=bool)
    def isExporting(self, session_id: str) -> bool:
        """Return whether a session export call is currently in flight."""
        return session_id in self._exporting

    @Slot(str)
    def removeSession(self, session_id: str):
        """Remove a session through the GUI bridge, then refresh the list."""
        session_id = (session_id or "").strip()
        if not session_id or session_id in self._removing:
            return

        self._set_action_error("")
        self._set_action_message("")
        self._removing.add(session_id)
        self.removingChanged.emit()

        def on_result(result: dict):
            self._removing.discard(session_id)
            self.removingChanged.emit()
            if "error" in result:
                self._set_action_error(_err_text(result.get("error")))
                return
            self._remove_local(session_id)
            self._set_action_message(f"Removed {session_id}")
            self.refresh()

        self._client.call("RemoveSession", session_id, callback=on_result)

    @Slot(str)
    def exportSession(self, session_id: str):
        """Export a session transcript through the GUI bridge."""
        session_id = (session_id or "").strip()
        if not session_id or session_id in self._exporting:
            return

        self._set_action_error("")
        self._set_action_message("")
        self._exporting.add(session_id)
        self.exportingChanged.emit()

        def on_result(result: dict):
            self._exporting.discard(session_id)
            self.exportingChanged.emit()
            if "error" in result:
                self._set_action_error(_err_text(result.get("error")))
                return
            path = str(result.get("result") or "")
            self._set_action_message(f"Exported {session_id}" + (f" to {path}" if path else ""))

        self._client.call("ExportSession", session_id, callback=on_result)

    @Slot()
    def pruneSessions(self):
        """Remove idle/empty sessions through the GUI bridge, then refresh."""
        if self._pruning:
            return

        self._set_action_error("")
        self._set_action_message("")
        self._set_pruning(True)

        def on_result(result: dict):
            self._set_pruning(False)
            if "error" in result:
                self._set_action_error(_err_text(result.get("error")))
                return
            removed = result.get("result") or []
            if isinstance(removed, list):
                for session_id in removed:
                    self._remove_local(str(session_id))
            count = len(removed) if isinstance(removed, list) else 0
            self._set_action_message(
                "No empty sessions to prune" if count == 0 else f"Pruned {count} empty session{'s' if count != 1 else ''}"
            )
            self.refresh()

        self._client.call("PruneSessions", callback=on_result)

    @Slot()
    def clearActionError(self):
        """Clear the visible session action error banner."""
        self._set_action_error("")

    @Slot()
    def clearActionMessage(self):
        """Clear the visible session action success/info banner."""
        self._set_action_message("")

    def mark_unread(self, session_id: str):
        """Mark a session as unread."""
        if session_id and session_id not in self._unread:
            self._unread.add(session_id)
            # Notify QML of change
            self._notify_unread_changed(session_id)
            self.sessionEntriesChanged.emit()

    def mark_read(self, session_id: str):
        """Mark a session as read (clear unread)."""
        if session_id and session_id in self._unread:
            self._unread.discard(session_id)
            # Notify QML of change
            self._notify_unread_changed(session_id)
            self.sessionEntriesChanged.emit()

    def _notify_unread_changed(self, session_id: str):
        """Emit dataChanged for a session's unread role."""
        for row in range(len(self._sessions)):
            if self._sessions[row].get("id") == session_id:
                idx = self.index(row, 0)
                self.dataChanged.emit(idx, idx, [self.UnreadRole])
                break

    def _remove_local(self, session_id: str):
        """Drop a session row locally while the async refresh catches up."""
        old_total = len(self._all_sessions)
        self._all_sessions = [session for session in self._all_sessions if session.get("id") != session_id]
        if len(self._all_sessions) != old_total:
            self.totalCountChanged.emit()
        for row, session in enumerate(self._sessions):
            if session.get("id") == session_id:
                self.beginRemoveRows(QModelIndex(), row, row)
                del self._sessions[row]
                self.endRemoveRows()
                self._unread.discard(session_id)
                self.filteredCountChanged.emit()
                self.sessionEntriesChanged.emit()
                return
        self.sessionEntriesChanged.emit()

    def _filtered_sessions(self) -> list[dict]:
        if not self._query:
            return list(self._all_sessions)
        query = self._query.lower()
        return [
            session
            for session in self._all_sessions
            if query in str(session.get("title") or "").lower()
            or query in str(session.get("dir") or "").lower()
            or query in str(session.get("model") or "").lower()
            or query in str(session.get("id") or "").lower()
        ]

    def _apply_filter(self):
        old_count = len(self._sessions)
        self.beginResetModel()
        self._sessions = self._filtered_sessions()
        self.endResetModel()
        if old_count != len(self._sessions):
            self.filteredCountChanged.emit()

    def _set_pruning(self, value: bool):
        if self._pruning == value:
            return
        self._pruning = value
        self.pruningChanged.emit()

    def _set_action_error(self, value: str):
        if self._action_error == value:
            return
        self._action_error = value
        self.actionErrorChanged.emit()

    def _set_action_message(self, value: str):
        if self._action_message == value:
            return
        self._action_message = value
        self.actionMessageChanged.emit()
