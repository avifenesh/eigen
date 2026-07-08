"""
commands.py — CommandsModel wrapping Commands RPC for slash-command popup.

Returns: list of {name, description, scope} for filterable slash-command dropdown.
"""

from typing import Optional

from PySide6.QtCore import QAbstractListModel, QModelIndex, QObject, Property, Qt, Signal, Slot

from eigenqt.rpc import RpcClient


def _err_text(value) -> str:
    if isinstance(value, dict):
        nested = value.get("message") or value.get("error")
        if nested:
            return str(nested)
    if value is None:
        return "Unknown error"
    return str(value)


BUILTIN_COMMANDS: tuple[dict[str, str], ...] = (
    {"name": "help", "description": "Show slash commands and GUI shortcuts", "scope": "builtin"},
    {"name": "home", "description": "Return to the home dashboard", "scope": "builtin"},
    {"name": "background", "description": "Leave this chat while the daemon keeps running", "scope": "builtin"},
    {"name": "bg", "description": "Alias for /background", "scope": "builtin"},
    {"name": "sessions", "description": "Open the session switcher", "scope": "builtin"},
    {"name": "rail", "description": "Toggle the left navigation rail", "scope": "builtin"},
    {"name": "changes", "description": "Toggle the worktree dock", "scope": "builtin"},
    {"name": "term", "description": "Open the terminal tool panel", "scope": "builtin"},
    {"name": "tasks", "description": "Open background tasks", "scope": "builtin"},
    {"name": "shells", "description": "Show background shells in the info dock", "scope": "builtin"},
    {"name": "tray", "description": "Open live sessions that need attention", "scope": "builtin"},
    {"name": "workflow", "description": "Run an authored workflow", "scope": "builtin"},
    {"name": "resume", "description": "Open the session list", "scope": "builtin"},
    {"name": "save", "description": "Export this session transcript", "scope": "builtin"},
    {"name": "clear", "description": "Clear the conversation", "scope": "builtin"},
    {"name": "rename", "description": "Rename this session", "scope": "builtin"},
    {"name": "compact", "description": "Compact older context", "scope": "builtin"},
    {"name": "model", "description": "Show or switch the model", "scope": "builtin"},
    {"name": "effort", "description": "Show or set reasoning effort", "scope": "builtin"},
    {"name": "search", "description": "Show or set live search", "scope": "builtin"},
    {"name": "fast", "description": "Toggle fast tier", "scope": "builtin"},
    {"name": "perm", "description": "Show or set permission posture", "scope": "builtin"},
    {"name": "goal", "description": "Show, set, or clear the session goal", "scope": "builtin"},
    {"name": "loop", "description": "Explain loop support in the GUI", "scope": "builtin"},
    {"name": "config", "description": "Open settings or set a config value", "scope": "builtin"},
    {"name": "route", "description": "Show or set model-assessed routing", "scope": "builtin"},
    {"name": "review", "description": "Ask for a cross-vendor review", "scope": "builtin"},
    {"name": "voice", "description": "Toggle hands-free voice mode or show setup", "scope": "builtin"},
    {"name": "mute", "description": "Stop hands-free voice mode", "scope": "builtin"},
    {"name": "dictate", "description": "Dictate one message", "scope": "builtin"},
    {"name": "talk", "description": "Alias for /dictate", "scope": "builtin"},
    {"name": "speak", "description": "Read the last assistant answer aloud once", "scope": "builtin"},
    {"name": "skills", "description": "List skills or preview one", "scope": "builtin"},
    {"name": "tools", "description": "List tools available to this session", "scope": "builtin"},
    {"name": "plugins", "description": "Open plugins", "scope": "builtin"},
    {"name": "hooks", "description": "Open hook and plugin wiring", "scope": "builtin"},
    {"name": "plugin", "description": "Open plugins", "scope": "builtin"},
    {"name": "marketplace", "description": "Open plugin marketplaces", "scope": "builtin"},
    {"name": "add-dir", "description": "List or grant a working directory", "scope": "builtin"},
    {"name": "find", "description": "Find text in this page", "scope": "builtin"},
    {"name": "copy", "description": "Copy the last assistant answer", "scope": "builtin"},
    {"name": "mouse", "description": "Explain terminal mouse capture", "scope": "builtin"},
    {"name": "ban", "description": "Record a hard prohibition in project memory", "scope": "builtin"},
    {"name": "unban", "description": "Remove a project-memory ban", "scope": "builtin"},
    {"name": "steer", "description": "Make Enter steer running turns", "scope": "builtin"},
    {"name": "queue", "description": "Make Enter queue while a turn runs", "scope": "builtin"},
    {"name": "export", "description": "Export this session transcript", "scope": "builtin"},
    {"name": "read", "description": "Use /speak for one-shot read-aloud", "scope": "builtin"},
    {"name": "observe", "description": "Open telemetry", "scope": "builtin"},
    {"name": "obs", "description": "Alias for /observe", "scope": "builtin"},
    {"name": "rebuild", "description": "Explain the terminal rebuild flow", "scope": "builtin"},
    {"name": "quit", "description": "Close the window from the desktop shell", "scope": "builtin"},
    {"name": "exit", "description": "Alias for /quit", "scope": "builtin"},
)


class CommandsModel(QAbstractListModel):
    """Commands list for slash-command popup (filterable)."""

    loadErrorChanged = Signal()

    # Qt roles
    NameRole = Qt.UserRole + 1
    DescriptionRole = Qt.UserRole + 2
    ScopeRole = Qt.UserRole + 3

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._commands: list[dict] = [dict(command) for command in BUILTIN_COMMANDS]
        self._filtered: list[dict] = self._commands[:]
        self._filter = ""
        self._load_error = ""

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

    @Property(str, notify=loadErrorChanged)
    def loadError(self) -> str:
        return self._load_error

    @Slot(str)
    def setFilter(self, filter_text: str) -> None:
        """Filter commands by name (prefix match)."""
        self._filter = filter_text.lower().strip()
        self._replace_filtered(self._filtered_commands())

    @Slot(str, result="QVariantList")
    def filteredCommands(self, filter_text: str) -> list[dict]:
        """Return command dictionaries matching a prefix filter for QML views."""
        needle = str(filter_text or "").lower().strip()
        if not needle:
            return [dict(command) for command in self._commands]
        return [
            dict(command)
            for command in self._commands
            if command.get("name", "").lower().startswith(needle)
        ]

    @Slot(str, result=bool)
    def hasCommand(self, name: str) -> bool:
        """Whether the merged command list contains name."""
        needle = str(name or "").lower()
        return any(command.get("name", "").lower() == needle for command in self._commands)

    @Slot(str, result=str)
    def commandScope(self, name: str) -> str:
        """Return the command scope, such as builtin, user, or project."""
        needle = str(name or "").lower()
        for command in self._commands:
            if command.get("name", "").lower() == needle:
                return str(command.get("scope", ""))
        return ""

    @Slot()
    def clearLoadError(self) -> None:
        self._set_load_error("")

    def _filtered_commands(self) -> list[dict]:
        """Return commands matching the current filter."""
        if not self._filter:
            return self._commands[:]
        return [c for c in self._commands if c.get("name", "").lower().startswith(self._filter)]

    def _replace_filtered(self, filtered: list[dict]) -> None:
        """Replace visible rows without resetting an active QML ListView."""
        old_count = len(self._filtered)
        if old_count > 0:
            self.beginRemoveRows(QModelIndex(), 0, old_count - 1)
            self._filtered = []
            self.endRemoveRows()
        if filtered:
            self.beginInsertRows(QModelIndex(), 0, len(filtered) - 1)
            self._filtered = filtered
            self.endInsertRows()

    def _fetch_commands(self) -> None:
        """Fetch commands from RPC Commands method."""

        def on_result(result: dict) -> None:
            if "error" in result:
                self._set_load_error(f"Could not load custom slash commands: {_err_text(result.get('error'))}")
                return
            commands = result.get("result") or []
            if not isinstance(commands, list):
                return
            self._set_load_error("")
            merged = [dict(command) for command in BUILTIN_COMMANDS]
            seen = {command["name"].lower() for command in merged}
            for command in commands:
                if not isinstance(command, dict):
                    continue
                name = str(command.get("name", "")).strip()
                if not name:
                    continue
                lower_name = name.lower()
                if lower_name in seen:
                    continue
                merged.append(
                    {
                        "name": name,
                        "description": str(command.get("description", "") or "custom command"),
                        "scope": str(command.get("scope", "") or "custom"),
                    }
                )
                seen.add(lower_name)
            self._commands = merged
            self._replace_filtered(self._filtered_commands())

        self._client.call("Commands", callback=on_result)

    def _set_load_error(self, value: str) -> None:
        if value == self._load_error:
            return
        self._load_error = value
        self.loadErrorChanged.emit()
