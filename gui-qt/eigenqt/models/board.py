"""
board.py — BoardModel (QAbstractListModel) for board lanes, plus KanbanModel.

Board: cross-project view board showing git state + action cards.
Kanban: derived cross-repo columns with actionable cards.

Loaded via Board() and Kanban() bridge calls. No subscriptions — user-triggered refresh.
"""

import sys
from typing import Optional

from PySide6.QtCore import QAbstractListModel, QModelIndex, QObject, Property, Qt, Signal, Slot

from eigenqt.rpc import RpcClient


class BoardModel(QAbstractListModel):
    """Board lanes model — one lane per project with git stats + items."""

    # Qt roles for BoardLaneDTO
    DirRole = Qt.UserRole + 1
    NameRole = Qt.UserRole + 2
    RepoRole = Qt.UserRole + 3
    BranchRole = Qt.UserRole + 4
    URLRole = Qt.UserRole + 5
    RemoteRole = Qt.UserRole + 6
    PinnedRole = Qt.UserRole + 7
    DirtyRole = Qt.UserRole + 8
    UnpushedRole = Qt.UserRole + 9
    BehindRole = Qt.UserRole + 10
    TodosRole = Qt.UserRole + 11
    OpenPrsRole = Qt.UserRole + 12
    OpenIssRole = Qt.UserRole + 13
    ItemsRole = Qt.UserRole + 14

    loadingChanged = Signal()
    errorChanged = Signal()
    sessionStarted = Signal(str)

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._lanes: list[dict] = []
        self._loading = False
        self._error = ""

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.DirRole: b"dir",
            self.NameRole: b"name",
            self.RepoRole: b"repo",
            self.BranchRole: b"branch",
            self.URLRole: b"url",
            self.RemoteRole: b"remote",
            self.PinnedRole: b"pinned",
            self.DirtyRole: b"dirty",
            self.UnpushedRole: b"unpushed",
            self.BehindRole: b"behind",
            self.TodosRole: b"todos",
            self.OpenPrsRole: b"openPrs",
            self.OpenIssRole: b"openIss",
            self.ItemsRole: b"items",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count (lanes list length)."""
        if parent.isValid():
            return 0
        return len(self._lanes)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._lanes):
            return None

        lane = self._lanes[index.row()]
        if role == self.DirRole:
            return lane.get("dir", "")
        if role == self.NameRole:
            return lane.get("name", "")
        if role == self.RepoRole:
            return lane.get("repo", "")
        if role == self.BranchRole:
            return lane.get("branch", "")
        if role == self.URLRole:
            return lane.get("url", "")
        if role == self.RemoteRole:
            return lane.get("remote", False)
        if role == self.PinnedRole:
            return lane.get("pinned", False)
        if role == self.DirtyRole:
            return lane.get("dirty") or 0
        if role == self.UnpushedRole:
            return lane.get("unpushed") or 0
        if role == self.BehindRole:
            return lane.get("behind") or 0
        if role == self.TodosRole:
            return lane.get("todos") or 0
        if role == self.OpenPrsRole:
            return lane.get("openPrs") or 0
        if role == self.OpenIssRole:
            return lane.get("openIss") or 0
        if role == self.ItemsRole:
            # Return items list directly (QML can iterate over it)
            return lane.get("items", [])
        return None

    @Property(bool, notify=loadingChanged)
    def loading(self):
        """Loading state."""
        return self._loading

    @Property(str, notify=errorChanged)
    def error(self):
        """Error message (empty if none)."""
        return self._error

    @Slot()
    def load(self):
        """Fetch board data from daemon."""
        self._loading = True
        self.loadingChanged.emit()
        self._error = ""
        self.errorChanged.emit()

        self._client.call("Board", callback=self._on_board_result)

    @Slot(dict)
    def _on_board_result(self, result: dict):
        """Handle Board RPC result."""
        self._loading = False
        self.loadingChanged.emit()

        if "error" in result:
            err = result.get("error", "")
            # Error can be a string or dict
            if isinstance(err, dict):
                self._error = err.get("message", "Unknown error")
            else:
                self._error = str(err) or "Unknown error"
            self.errorChanged.emit()
            return

        board_dto = result.get("result") or {}
        lanes = board_dto.get("lanes") or []

        # Sort: pinned first, then by name
        lanes.sort(key=lambda l: (not (l.get("pinned") or False), (l.get("name") or "").lower()))

        self.beginResetModel()
        self._lanes = lanes
        self.endResetModel()

    @Slot(str)
    def toggle_pin(self, key: str):
        """Toggle lane pin state. Key is repo (remote) or dir (local)."""
        # Find the lane to determine current pinned state
        lane = next((l for l in self._lanes if (l.get("remote") and l.get("repo") == key) or (not l.get("remote") and l.get("dir") == key)), None)
        if not lane:
            return

        method = "UnpinLane" if lane.get("pinned") else "PinLane"
        self._client.call(method, key, callback=lambda r: self._on_toggle_pin_result(r, key))

    def _on_toggle_pin_result(self, result: dict, key: str):
        """Handle pin/unpin result — reload board."""
        if "error" in result:
            print(f"Toggle pin error: {result['error']}", file=sys.stderr)
            return
        self.load()

    @Slot(str)
    def open_lane_chat(self, lane_dir: str):
        """Open a new session for the given lane. Emits signal for routing."""
        self._client.call("NewSession", lane_dir, "", "", callback=lambda r: self._on_new_session_result(r))

    def _on_new_session_result(self, result: dict):
        """Handle NewSession result. Emit a signal that Main can catch."""
        if "error" in result:
            print(f"NewSession error: {result['error']}", file=sys.stderr)
            return
        session_id = result.get("result") or ""
        if session_id:
            self.sessionStarted.emit(session_id)


class KanbanModel(QObject):
    """Kanban board model — columns with cards (not a list model, just column data)."""

    columnsChanged = Signal()
    loadingChanged = Signal()
    errorChanged = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._columns: list[dict] = []
        self._loading = False
        self._error = ""

    @Property(list, notify=columnsChanged)
    def columns(self):
        """Kanban columns (list of dicts with id/title/cards)."""
        return self._columns

    @Property(bool, notify=loadingChanged)
    def loading(self):
        """Loading state."""
        return self._loading

    @Property(str, notify=errorChanged)
    def error(self):
        """Error message (empty if none)."""
        return self._error

    @Slot()
    def load(self):
        """Fetch kanban data from daemon."""
        self._loading = True
        self.loadingChanged.emit()
        self._error = ""
        self.errorChanged.emit()

        self._client.call("Kanban", callback=self._on_kanban_result)

    @Slot(dict)
    def _on_kanban_result(self, result: dict):
        """Handle Kanban RPC result."""
        self._loading = False
        self.loadingChanged.emit()

        if "error" in result:
            err = result.get("error", "")
            # Error can be a string or dict
            if isinstance(err, dict):
                self._error = err.get("message", "Unknown error")
            else:
                self._error = str(err) or "Unknown error"
            self.errorChanged.emit()
            return

        kanban_dto = result.get("result") or {}
        self._columns = kanban_dto.get("columns") or []
        self.columnsChanged.emit()
