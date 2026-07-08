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


def _err_text(result: dict) -> str:
    """Extract error message from RPC result, handling string or dict errors."""
    e = result.get("error")
    if isinstance(e, str):
        return e or "Unknown error"
    if isinstance(e, dict):
        return e.get("message", "Unknown error")
    return str(e) if e else "Unknown error"


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
    loading_changed = Signal()
    load_error_changed = Signal()
    trigger_done = Signal(str, str, bool, str)  # (repo, job, success, error_msg)
    set_paused_done = Signal(str, bool, str)  # (repo, success, error_msg)

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._reviewers: list[dict] = []
        self._available = False
        self._count = 0
        self._paused = 0
        self._loading = False
        self._load_error = ""
        self._active = False
        self._load_seq = 0
        self._pending_repo_actions: dict[str, str] = {}

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

    @Property(bool, notify=loading_changed)
    def loading(self) -> bool:
        """Is a reviewers refresh in flight?"""
        return self._loading

    @Property(str, notify=load_error_changed)
    def load_error(self) -> str:
        """Last RevutoStatus/RevutoReviewers load error."""
        return self._load_error

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

    @Slot()
    def _on_connected(self):
        """Fetch data on connect only while the route is active."""
        if self._active:
            self.start_polling()

    def _fetch_data(self):
        """Async fetch RevutoStatus + RevutoReviewers RPCs (sequential)."""
        self._load_seq += 1
        seq = self._load_seq
        self._set_loading(True)
        self._set_load_error("")
        self._client.call("RevutoStatus", callback=lambda result: self._on_status_result(result, seq))

    @Slot(dict)
    def _on_status_result(self, result: dict, seq: Optional[int] = None):
        """Handle RevutoStatus RPC result."""
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            self._set_loading(False)
            self._set_load_error(_err_text(result))
            if self._reviewers:
                self.beginResetModel()
                self._reviewers = []
                self.endResetModel()
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
            self._client.call("RevutoReviewers", callback=lambda result: self._on_reviewers_result(result, seq))
        elif self._reviewers:
            self.beginResetModel()
            self._reviewers = []
            self.endResetModel()
            self._set_loading(False)
        else:
            self._set_loading(False)

    @Slot(dict)
    def _on_reviewers_result(self, result: dict, seq: Optional[int] = None):
        """Handle RevutoReviewers RPC result."""
        if seq is not None and seq != self._load_seq:
            return
        if "error" in result:
            self._set_loading(False)
            self._set_load_error(_err_text(result))
            return

        reviewers = result.get("result") or []

        self.beginResetModel()
        self._reviewers = reviewers
        self.endResetModel()
        self._count = len(reviewers)
        self._paused = sum(1 for reviewer in reviewers if reviewer.get("paused", False))
        self._set_loading(False)
        self.status_changed.emit()

    @Slot(str, str)
    def trigger(self, repo: str, job: str):
        """
        Trigger a revuto job (review/learn) via RevutoTrigger RPC.
        Emits trigger_done(repo, job, success, error_msg) on completion.
        """
        if not self._mark_repo_pending(repo, job):
            return
        self._client.call(
            "RevutoTrigger",
            repo, job,
            callback=lambda r: self._on_trigger_result(repo, job, r),
        )

    def _on_trigger_result(self, repo: str, job: str, result: dict):
        """Handle RevutoTrigger RPC result."""
        self._clear_repo_pending(repo, job)
        if "error" in result:
            error_msg = _err_text(result)
            self.trigger_done.emit(repo, job, False, error_msg)
            return

        self.trigger_done.emit(repo, job, True, "")

    @Slot(str, bool)
    def set_paused(self, repo: str, paused: bool):
        """
        Toggle pause state via RevutoSetPaused RPC.
        Emits set_paused_done(repo, success, error_msg) on completion.
        """
        action = "pause" if paused else "resume"
        if not self._mark_repo_pending(repo, action):
            return
        self._client.call(
            "RevutoSetPaused",
            repo, paused,
            callback=lambda r: self._on_set_paused_result(repo, paused, r),
        )

    def _on_set_paused_result(self, repo: str, paused: bool, result: dict):
        """Handle RevutoSetPaused RPC result."""
        self._clear_repo_pending(repo, "pause" if paused else "resume")
        if "error" in result:
            error_msg = _err_text(result)
            self.set_paused_done.emit(repo, False, error_msg)
            return

        self._set_reviewer_paused(repo, paused)
        self.set_paused_done.emit(repo, True, "")

    def _set_reviewer_paused(self, repo: str, paused: bool) -> None:
        """Update one reviewer row after a successful pause/resume mutation."""
        for row, reviewer in enumerate(self._reviewers):
            if reviewer.get("repo", "") != repo:
                continue
            next_reviewer = dict(reviewer)
            next_reviewer["paused"] = bool(paused)
            self._reviewers[row] = next_reviewer
            index = self.index(row, 0)
            self.dataChanged.emit(index, index, [self.PausedRole])
            self._count = len(self._reviewers)
            self._paused = sum(1 for item in self._reviewers if item.get("paused", False))
            self.status_changed.emit()
            return

    def _mark_repo_pending(self, repo: str, action: str) -> bool:
        """Reserve a repo while a reviewer mutation is in flight."""
        if not repo or repo in self._pending_repo_actions:
            return False
        self._pending_repo_actions[repo] = action
        return True

    def _clear_repo_pending(self, repo: str, action: str) -> None:
        if self._pending_repo_actions.get(repo) == action:
            del self._pending_repo_actions[repo]

    @Slot()
    def refresh(self):
        """Manually refresh data (called by QML on window visibility change)."""
        self._fetch_data()

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
            self._fetch_data()
            self._poll_timer.start()
