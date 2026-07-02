"""
home.py — Home dashboard models (DashboardModel, FeedModel).

DashboardModel exposes Today/Inbox/Machine/GPU cards via RPC Dashboard; polls every 60s.
FeedModel exposes Act-On feed items; dismiss removes row + RPC call.
Working-now and resume use slices from SessionsModel (see sessions.py).
"""

import sys
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

from eigenqt.rpc import RpcClient


class DashboardModel(QObject):
    """
    Dashboard data model for Home view.

    Exposes: google_connected, events, unread_count, unread, health (cpu/mem/disk/gpus).
    Polls Dashboard() every 60s while attached.
    """

    dataChanged = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(60_000)  # 60s
        self._poll_timer.timeout.connect(self._fetch_dashboard)

        # Dashboard fields (flat properties for QML)
        self._google_connected = False
        self._events: list[dict] = []
        self._unread_count = 0
        self._unread: list[dict] = []
        self._health: dict = {}
        self._gpus: list[dict] = []

        self._client.connected.connect(self._on_connected)

    @Slot()
    def _on_connected(self):
        """Fetch dashboard on connect and start polling."""
        self._fetch_dashboard()
        self._poll_timer.start()

    def _fetch_dashboard(self):
        """Async fetch Dashboard RPC."""
        self._client.call("Dashboard", callback=self._on_dashboard_result)

    @Slot(dict)
    def _on_dashboard_result(self, result: dict):
        """Handle Dashboard RPC result."""
        if "error" in result:
            return

        data = result.get("result") or {}
        self._google_connected = data.get("googleConnected", False)
        self._events = data.get("events") or []
        self._unread_count = data.get("unreadCount", 0)
        self._unread = data.get("unread") or []
        self._health = data.get("health") or {}
        self._gpus = self._health.get("gpus") or []

        self.dataChanged.emit()

    # Properties (read-only for QML)
    @Property(bool, notify=dataChanged)
    def google_connected(self) -> bool:
        return self._google_connected

    @Property("QVariantList", notify=dataChanged)
    def events(self) -> list[dict]:
        return self._events

    @Property(int, notify=dataChanged)
    def unread_count(self) -> int:
        return self._unread_count

    @Property("QVariantList", notify=dataChanged)
    def unread(self) -> list[dict]:
        return self._unread

    @Property("QVariantMap", notify=dataChanged)
    def health(self) -> dict:
        return self._health

    @Property("QVariantList", notify=dataChanged)
    def gpus(self) -> list[dict]:
        return self._gpus

    def stop_polling(self):
        """Stop polling when view is inactive."""
        self._poll_timer.stop()

    def start_polling(self):
        """Resume polling when view becomes active."""
        if not self._poll_timer.isActive():
            self._fetch_dashboard()
            self._poll_timer.start()


class FeedModel(QAbstractListModel):
    """
    Act-On feed items model.

    Populated by RPC Feed; dismiss removes row + RPC DismissFeed.
    """

    # Qt roles
    KeyRole = Qt.UserRole + 1
    KindRole = Qt.UserRole + 2
    TitleRole = Qt.UserRole + 3
    DetailRole = Qt.UserRole + 4
    DirRole = Qt.UserRole + 5
    DirNameRole = Qt.UserRole + 6
    TaskRole = Qt.UserRole + 7
    URLRole = Qt.UserRole + 8

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._items: list[dict] = []
        self._fresh = False

        self._client.connected.connect(self._on_connected)
        # Subscribe to feed updates
        self._client.event.connect(self._on_event)

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.KeyRole: b"key",
            self.KindRole: b"kind",
            self.TitleRole: b"title",
            self.DetailRole: b"detail",
            self.DirRole: b"dir",
            self.DirNameRole: b"dirName",
            self.TaskRole: b"task",
            self.URLRole: b"url",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count."""
        if parent.isValid():
            return 0
        return len(self._items)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._items):
            return ""

        item = self._items[index.row()]
        if role == self.KeyRole:
            return item.get("key", "")
        if role == self.KindRole:
            return item.get("kind", "")
        if role == self.TitleRole:
            return item.get("title", "")
        if role == self.DetailRole:
            return item.get("detail", "")
        if role == self.DirRole:
            return item.get("dir", "")
        if role == self.DirNameRole:
            return item.get("dirName", "")
        if role == self.TaskRole:
            return item.get("task", "")
        if role == self.URLRole:
            return item.get("url", "")
        return ""

    @Slot()
    def _on_connected(self):
        """Fetch feed on connect."""
        self._fetch_feed()
        # Subscribe to feed updates
        self._client.subscribe(["eigen:feed"])

    def _fetch_feed(self):
        """Async fetch Feed RPC."""
        self._client.call("Feed", callback=self._on_feed_result)

    @Slot(dict)
    def _on_feed_result(self, result: dict):
        """Handle Feed RPC result."""
        if "error" in result:
            return

        data = result.get("result") or {}
        items = data.get("items") or []
        self._fresh = data.get("fresh", False)

        self.beginResetModel()
        self._items = items
        self.endResetModel()

    @Slot(str, dict)
    def _on_event(self, channel: str, data: dict):
        """Handle eigen:feed events (feed rescanned)."""
        if channel == "eigen:feed":
            # Feed rescanned, refetch
            self._fetch_feed()

    @Slot(str)
    def dismiss(self, key: str):
        """Dismiss a feed item (optimistic remove + RPC)."""
        # Find and remove row
        for i, item in enumerate(self._items):
            if item.get("key") == key:
                self.beginRemoveRows(QModelIndex(), i, i)
                del self._items[i]
                self.endRemoveRows()
                break

        # Fire RPC (errors silently fail; feed will rescan eventually)
        self._client.call("DismissFeed", params=[key], callback=lambda r: None)

    @Slot(str, str, result=str)
    def start_from_feed(self, dir_path: str, task: str) -> str:
        """
        Start session from feed (async RPC, returns empty; actual session ID via callback).
        For QML: use callToken pattern.
        """
        # This is a placeholder; QML should use client.callToken("StartFromFeed", [dir, task])
        return ""

    @Slot()
    def refresh(self):
        """Manually refresh feed (RescanFeed RPC)."""
        self._client.call("RescanFeed", callback=lambda r: None)
