"""
reply_watch.py — ReplyWatcher for detecting background session replies.

Watches SessionsModel status transitions (working/approval → idle) for sessions that
are NOT currently open. When detected, marks session unread + fires NotifyChatReply RPC
(which triggers desktop notification on the Go side).

Mirrors Svelte's sessionReplyWatch.svelte.ts.
"""
import logging
from typing import Optional, Set

from PySide6.QtCore import QObject, QTimer, Signal, Slot

from eigenqt.rpc import RpcClient

logger = logging.getLogger(__name__)

# Statuses that indicate active work
ACTIVE_STATUSES = {"working", "approval"}


class ReplyWatcher(QObject):
    """Watches session status transitions and detects background replies."""

    # Signal emitted when a session gets a reply while user is not viewing it
    unread = Signal(str)  # session_id

    def __init__(
        self,
        client: RpcClient,
        sessions_model: "SessionsModel",  # type: ignore
        parent: Optional[QObject] = None,
    ):
        """
        Initialize ReplyWatcher.

        Args:
            client: RpcClient instance for NotifyChatReply RPC
            sessions_model: SessionsModel to watch for changes
            parent: QObject parent
        """
        super().__init__(parent)
        self._client = client
        self._sessions = sessions_model
        self._prev_status: dict[str, str] = {}  # session_id -> status
        self._current_session_id: str = ""
        self._window_focused: bool = True
        self._unread_ids: Set[str] = set()

        # Connect to sessions model changes
        self._sessions.dataChanged.connect(self._on_sessions_data_changed)
        self._sessions.modelReset.connect(self._on_sessions_reset)

        # Poll sessions list every 2s to catch status changes
        # (SessionsModel doesn't automatically refetch; we need to trigger it)
        self._poll_timer = QTimer(self)
        self._poll_timer.timeout.connect(self._poll_sessions)
        self._poll_timer.start(2000)  # 2s interval

        logger.info("ReplyWatcher initialized")

    @Slot(str)
    def set_current_session(self, session_id: str):
        """
        Set the currently-open session (clear unread for this session).

        Args:
            session_id: Session ID that is now open
        """
        if session_id and session_id in self._unread_ids:
            self._unread_ids.discard(session_id)
            logger.info(f"Cleared unread for session {session_id[:8]}")
        self._current_session_id = session_id

    @Slot(bool)
    def set_window_focused(self, focused: bool):
        """
        Set window focus state.

        Args:
            focused: True if window is focused
        """
        self._window_focused = focused

    def is_unread(self, session_id: str) -> bool:
        """Check if session is unread."""
        return session_id in self._unread_ids

    @Slot()
    def _on_sessions_reset(self):
        """Handle sessions model reset (full list refetch)."""
        self._check_all_sessions()

    @Slot()
    def _on_sessions_data_changed(self):
        """Handle sessions model data change."""
        self._check_all_sessions()

    @Slot()
    def _poll_sessions(self):
        """Poll sessions list for status updates."""
        self._sessions.refresh()

    def _check_all_sessions(self):
        """Check all sessions for working/approval → idle transitions."""
        row_count = self._sessions.rowCount()
        for row in range(row_count):
            index = self._sessions.index(row, 0)
            session_id = self._sessions.data(index, self._sessions.IdRole)
            status = self._sessions.data(index, self._sessions.StatusRole)

            if not session_id or not status:
                continue

            prev = self._prev_status.get(session_id)
            self._prev_status[session_id] = status

            # Detect transition: working/approval → idle
            if not prev or prev not in ACTIVE_STATUSES or status != "idle":
                continue

            # Skip if this is the currently-open session
            if session_id == self._current_session_id:
                logger.debug(
                    f"Session {session_id[:8]} finished, but is open — skip notify"
                )
                continue

            # Mark unread + notify
            self._mark_unread_and_notify(session_id, index)

    def _mark_unread_and_notify(self, session_id: str, index):
        """Mark session unread and fire desktop notification."""
        self._unread_ids.add(session_id)
        self.unread.emit(session_id)

        # Get session title for notification
        title = self._sessions.data(index, self._sessions.TitleRole) or ""
        if not title.strip():
            dir_path = self._sessions.data(index, self._sessions.DirRole) or ""
            dir_path = dir_path.rstrip("/")
            title = dir_path.split("/")[-1] if dir_path else "Chat"

        logger.info(
            f"New reply from background session {session_id[:8]} ('{title}')"
        )

        # Fire desktop notification via RPC (Go side handles the OS notification)
        try:
            self._client.call("NotifyChatReply", session_id, title)
        except Exception as e:
            logger.warning(f"NotifyChatReply RPC failed: {e}")

    def reconcile_sessions(self, live_ids: list[str]):
        """
        Remove unread markers for sessions that no longer exist.

        Args:
            live_ids: List of currently-existing session IDs
        """
        live_set = set(live_ids)
        to_remove = [sid for sid in self._unread_ids if sid not in live_set]
        for sid in to_remove:
            self._unread_ids.discard(sid)
            logger.debug(f"Reconciled removed session {sid[:8]} from unread")
