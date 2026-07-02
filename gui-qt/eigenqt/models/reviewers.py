"""
reviewers.py — Reviewers view model (revuto status + per-repo reviewers).

ReviewersModel: loads RevutoStatus + RevutoReviewers RPCs, exposes reviewer list,
mutation via RevutoSetPaused + RevutoTrigger RPCs.
Polls every 60s and reloads on window visibility (revuto state can change externally).
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

from eigenqt.rpc.client import RpcClient


class ReviewersModel(QAbstractListModel):
    """
    Revuto reviewers model — per-repo AI PR reviewer list.

    Roles: repo, paused (bool).
    Exposes status: available (bool), count (int), paused (int).
    Mutations: RevutoSetPaused (repo, paused), RevutoTrigger (repo, job).
    """

    # Qt roles
    RepoRole = Qt.UserRole + 1
    PausedRole = Qt.UserRole + 2

    # Signals
    status_changed = Signal()  # Fired when status (available/count/paused) changes
    trigger_done = Signal(str, str, bool, str)  # (repo, job, success, error_msg)
    set_paused_done = Signal(str, bool, str)  # (repo, success, error_msg)

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._reviewers: list[dict] = []
        self._available = False
        self._count = 0
        self._paused = 0

        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)  # 60s
        self._poll_timer.timeout.connect(self._fetch_data)

        self._client.connected.connect(self._on_connected)

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.RepoRole: b"repo",
            self.PausedRole: b"paused",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count."""
        if parent.isValid():
            return 0
        return len(self._reviewers)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._reviewers):
            return None

        reviewer = self._reviewers[index.row()]
        if role == self.RepoRole:
            return reviewer.get("repo", "")
        if role == self.PausedRole:
            return reviewer.get("paused", False)
        return None

    # Properties for QML (status)
    @Property(bool, notify=status_changed)
    def available(self) -> bool:
        """Is revuto CLI available?"""
        return self._available

    @Property(int, notify=status_changed)
    def count(self) -> int:
        """Total reviewer count."""
        return self._count

    @Property(int, notify=status_changed)
    def paused_count(self) -> int:
        """Paused reviewer count."""
        return self._paused

    @Slot()
    def _on_connected(self):
        """Fetch data on connect and start polling."""
        self._fetch_data()
        self._poll_timer.start()

    def _fetch_data(self):
        """Async fetch RevutoStatus + RevutoReviewers RPCs (sequential)."""
        self._client.call("RevutoStatus", callback=self._on_status_result)

    @Slot(dict)
    def _on_status_result(self, result: dict):
        """Handle RevutoStatus RPC result."""
        if "error" in result:
            self._available = False
            self._count = 0
            self._paused = 0
            self.status_changed.emit()
            return

        data = result.get("result") or {}
        self._available = data.get("available", False)
        self._count = data.get("count", 0)
        self._paused = data.get("paused", 0)
        self.status_changed.emit()

        # If available, fetch reviewers
        if self._available:
            self._client.call("RevutoReviewers", callback=self._on_reviewers_result)

    @Slot(dict)
    def _on_reviewers_result(self, result: dict):
        """Handle RevutoReviewers RPC result."""
        if "error" in result:
            return

        reviewers = result.get("result") or []

        self.beginResetModel()
        self._reviewers = reviewers
        self.endResetModel()

    @Slot(str, str)
    def trigger(self, repo: str, job: str):
        """
        Trigger a revuto job (review/learn) via RevutoTrigger RPC.
        Emits trigger_done(repo, job, success, error_msg) on completion.
        """
        self._client.call(
            "RevutoTrigger",
            repo, job,
            callback=lambda r: self._on_trigger_result(repo, job, r),
        )

    def _on_trigger_result(self, repo: str, job: str, result: dict):
        """Handle RevutoTrigger RPC result."""
        if "error" in result:
            error_msg = result.get("error", {}).get("message", "Unknown error")
            self.trigger_done.emit(repo, job, False, error_msg)
            return

        self.trigger_done.emit(repo, job, True, "")

    @Slot(str, bool)
    def set_paused(self, repo: str, paused: bool):
        """
        Toggle pause state via RevutoSetPaused RPC.
        Emits set_paused_done(repo, success, error_msg) on completion.
        """
        self._client.call(
            "RevutoSetPaused",
            repo, paused,
            callback=lambda r: self._on_set_paused_result(repo, r),
        )

    def _on_set_paused_result(self, repo: str, result: dict):
        """Handle RevutoSetPaused RPC result."""
        if "error" in result:
            error_msg = result.get("error", {}).get("message", "Unknown error")
            self.set_paused_done.emit(repo, False, error_msg)
            return

        self.set_paused_done.emit(repo, True, "")
        # Refresh data (status + reviewers)
        self._fetch_data()

    @Slot()
    def refresh(self):
        """Manually refresh data (called by QML on window visibility change)."""
        self._fetch_data()

    def stop_polling(self):
        """Stop polling when view is inactive."""
        self._poll_timer.stop()

    def start_polling(self):
        """Resume polling when view becomes active."""
        if not self._poll_timer.isActive():
            self._fetch_data()
            self._poll_timer.start()
