"""
tasks.py — TasksModel for background agents/tasks view.

Polls Agents RPC every 1.5s while running tasks exist, else 4s.
Pause polling when app is hidden (QGuiApplication::applicationStateChanged).
Roles: id, status, task, model, elapsed, lastTool, steps.
cancel(id) slot → CancelAgent RPC.
"""

import time
from typing import Optional

from PySide6.QtCore import (
    Property,
    QAbstractListModel,
    QModelIndex,
    QObject,
    Qt,
    QTimer,
    Signal,
    Slot,
)
from PySide6.QtGui import QGuiApplication

from eigenqt.rpc import RpcClient


class TasksModel(QAbstractListModel):
    countsChanged = Signal()  # running/done/error counts changed (rail badges bind these)
    """Background agents/tasks model (polled, status-filtered)."""

    # Qt roles
    IdRole = Qt.UserRole + 1
    StatusRole = Qt.UserRole + 2
    TaskRole = Qt.UserRole + 3
    ModelRole = Qt.UserRole + 4
    ElapsedRole = Qt.UserRole + 5
    LastToolRole = Qt.UserRole + 6
    StepsRole = Qt.UserRole + 7
    StartedMsRole = Qt.UserRole + 8
    FinishedMsRole = Qt.UserRole + 9
    ErrorRole = Qt.UserRole + 10
    CancelingRole = Qt.UserRole + 11
    RoleNameRole = Qt.UserRole + 12
    KindRole = Qt.UserRole + 13
    DifficultyRole = Qt.UserRole + 14
    WhereRole = Qt.UserRole + 15
    LastNoteRole = Qt.UserRole + 16
    InTokensRole = Qt.UserRole + 17
    OutTokensRole = Qt.UserRole + 18
    AttemptsRole = Qt.UserRole + 19
    EscalatedRole = Qt.UserRole + 20
    ResultRole = Qt.UserRole + 21

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._tasks: list[dict] = []
        self._all_tasks: list[dict] = []  # unfiltered snapshot (filter applies on top)
        self._filter = "all"  # all | running | done | error
        self._running_count = 0
        self._done_count = 0
        self._error_count = 0

        # Polling timer (adaptive cadence)
        self._poll_timer = QTimer(self)
        self._poll_timer.timeout.connect(self._poll)

        # Track app state (pause polling when hidden)
        self._app_visible = True
        # TODO: pause polling when app hidden (needs QGuiApplication from QtGui, not QtCore)

        # Connect to RPC
        self._client.connected.connect(self._on_connected)

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.IdRole: b"taskId",
            self.StatusRole: b"status",
            self.TaskRole: b"task",
            self.ModelRole: b"modelName",
            self.ElapsedRole: b"elapsed",
            self.LastToolRole: b"lastTool",
            self.StepsRole: b"steps",
            self.StartedMsRole: b"startedMs",
            self.FinishedMsRole: b"finishedMs",
            self.ErrorRole: b"error",
            self.CancelingRole: b"canceling",
            self.RoleNameRole: b"roleName",
            self.KindRole: b"kind",
            self.DifficultyRole: b"difficulty",
            self.WhereRole: b"where",
            self.LastNoteRole: b"lastNote",
            self.InTokensRole: b"inTokens",
            self.OutTokensRole: b"outTokens",
            self.AttemptsRole: b"attempts",
            self.EscalatedRole: b"escalated",
            self.ResultRole: b"result",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count (tasks list length)."""
        if parent.isValid():
            return 0
        return len(self._tasks)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._tasks):
            return None

        task = self._tasks[index.row()]
        if role == self.IdRole:
            return task.get("id", "")
        if role == self.StatusRole:
            return task.get("status", "")
        if role == self.TaskRole:
            return task.get("task", "")
        if role == self.ModelRole:
            return task.get("model", "")
        if role == self.ElapsedRole:
            # Compute elapsed live (for ticking display)
            return self._compute_elapsed(task)
        if role == self.LastToolRole:
            return task.get("lastTool", "")
        if role == self.StepsRole:
            return task.get("steps", 0)
        if role == self.StartedMsRole:
            return task.get("startedMs", 0)
        if role == self.FinishedMsRole:
            return task.get("finishedMs", 0)
        if role == self.ErrorRole:
            return task.get("error", "")
        if role == self.CancelingRole:
            return task.get("canceling", False)
        if role == self.RoleNameRole:
            return task.get("role", "")
        if role == self.KindRole:
            return task.get("kind", "")
        if role == self.DifficultyRole:
            return task.get("difficulty", "")
        if role == self.WhereRole:
            return task.get("where", "")
        if role == self.LastNoteRole:
            return task.get("lastNote", "")
        if role == self.InTokensRole:
            return task.get("inTokens", 0)
        if role == self.OutTokensRole:
            return task.get("outTokens", 0)
        if role == self.AttemptsRole:
            return task.get("attempts", 0)
        if role == self.EscalatedRole:
            return task.get("escalated", False)
        if role == self.ResultRole:
            return task.get("result", "")
        return None

    def _compute_elapsed(self, task: dict) -> str:
        """Compute elapsed time string (live for running tasks)."""
        started = task.get("startedMs", 0)
        finished = task.get("finishedMs", 0)
        if not started:
            return ""

        end_ms = finished if finished > 0 else int(time.time() * 1000)
        secs = max(0, (end_ms - started) // 1000)

        if secs < 60:
            return f"{secs}s"
        mins = secs // 60
        if mins < 60:
            return f"{mins}m {secs % 60}s"
        hours = mins // 60
        return f"{hours}h {mins % 60}m"

    @Slot()
    def _on_connected(self):
        """Start polling on connect."""
        self._poll()

    @Slot()
    def _poll(self):
        """Fetch Agents RPC (async)."""
        if self._app_visible:
            self._client.call("Agents", callback=self._on_agents_result)
        else:
            # Still reschedule even when hidden (resume when shown)
            self._schedule_next_poll()

    @Slot(dict)
    def _on_agents_result(self, result: dict):
        """Handle Agents RPC result."""
        if "error" in result:
            self._schedule_next_poll()
            return

        data = result.get("result", {})
        tasks = data.get("tasks", [])

        # Update counts
        self._running_count = sum(1 for t in tasks if t.get("status") == "running")
        self._done_count = sum(1 for t in tasks if t.get("status") == "done")
        self._error_count = sum(
            1 for t in tasks if t.get("status") in ("error", "lost")
        )

        # Update model (simple reset for now; incremental updates possible but overkill)
        self._all_tasks = tasks
        self.beginResetModel()
        self._tasks = self._apply_filter(tasks)
        self.endResetModel()
        self.countsChanged.emit()

        # Schedule next poll (adaptive cadence)
        self._schedule_next_poll()

    def _schedule_next_poll(self):
        """Schedule next poll (1.5s if running tasks, 4s if idle)."""
        interval = 1500 if self._running_count > 0 else 4000
        self._poll_timer.start(interval)

    # @Slot(Qt.ApplicationState)
    # def _on_app_state_changed(self, state):
    #     """Pause polling when app is hidden (minimize/background)."""
    #     # TODO: re-enable when QGuiApplication import is fixed
    #     self._app_visible = state == Qt.ApplicationActive

    @Slot(str)
    def cancel(self, task_id: str):
        """Cancel a running task (fire-and-forget RPC)."""
        if not task_id:
            return
        self._client.call("CancelAgent", task_id)
        # Optimistically mark as canceling (will be confirmed on next poll)
        for task in self._tasks:
            if task.get("id") == task_id:
                task["canceling"] = True
                # Notify QML
                row = self._tasks.index(task)
                idx = self.index(row, 0)
                self.dataChanged.emit(idx, idx, [self.CancelingRole])
                break

    def refresh(self):
        """Manually trigger a refresh."""
        self._poll()

    def _apply_filter(self, tasks: list[dict]) -> list[dict]:
        f = self._filter
        if f == "all":
            return list(tasks)
        if f == "error":
            return [t for t in tasks if t.get("status") in ("error", "lost")]
        return [t for t in tasks if t.get("status") == f]

    filterChanged = Signal()

    @Property(str, notify=filterChanged)
    def filter(self) -> str:
        return self._filter

    @filter.setter
    def filter(self, value: str) -> None:
        if value == self._filter:
            return
        self._filter = value
        self.beginResetModel()
        self._tasks = self._apply_filter(self._all_tasks)
        self.endResetModel()
        self.filterChanged.emit()

    # Qt Properties (plain python @property is INVISIBLE to QML — the rail
    # badges bound to running_count silently read undefined until this).
    @Property(int, notify=countsChanged)
    def running_count(self) -> int:
        return self._running_count

    @Property(int, notify=countsChanged)
    def done_count(self) -> int:
        return self._done_count

    @Property(int, notify=countsChanged)
    def error_count(self) -> int:
        return self._error_count
