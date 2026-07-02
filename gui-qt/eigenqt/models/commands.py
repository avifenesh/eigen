"""
commands.py — CommandsModel wrapping Commands RPC for slash-command popup.

Returns: list of {name, description, scope} for filterable slash-command dropdown.
"""

from typing import Optional

from PySide6.QtCore import QAbstractListModel, QModelIndex, QObject, Qt, Slot

from eigenqt.rpc import RpcClient


class CommandsModel(QAbstractListModel):
    """Commands list for slash-command popup (filterable)."""

    # Qt roles
    NameRole = Qt.UserRole + 1
    DescriptionRole = Qt.UserRole + 2
    ScopeRole = Qt.UserRole + 3

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._commands: list[dict] = []
        self._filtered: list[dict] = []
        self._filter = ""

        # Fetch commands on init
        self._fetch_commands()

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.NameRole: b"name",
            self.DescriptionRole: b"description",
            self.ScopeRole: b"scope",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count (filtered commands)."""
        if parent.isValid():
            return 0
        return len(self._filtered)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._filtered):
            return None

        cmd = self._filtered[index.row()]
        if role == self.NameRole:
            return cmd.get("name", "")
        if role == self.DescriptionRole:
            return cmd.get("description", "")
        if role == self.ScopeRole:
            return cmd.get("scope", "")
        return None

    @Slot(str)
    def setFilter(self, filter_text: str) -> None:
        """Filter commands by name (prefix match)."""
        self._filter = filter_text.lower().strip()
        self._apply_filter()

    def _apply_filter(self) -> None:
        """Apply filter, rebuild filtered list, reset model."""
        self.beginResetModel()
        if not self._filter:
            self._filtered = self._commands[:]
        else:
            self._filtered = [
                c for c in self._commands if c.get("name", "").lower().startswith(self._filter)
            ]
        self.endResetModel()

    def _fetch_commands(self) -> None:
        """Fetch commands from RPC Commands method."""

        def on_result(result: dict) -> None:
            if "error" in result:
                print(f"Commands RPC error: {result['error']}")
                return
            commands = result.get("result") or []
            if not isinstance(commands, list):
                return
            self.beginResetModel()
            self._commands = commands
            self._filtered = commands[:]
            self.endResetModel()

        self._client.call("Commands", callback=on_result)
