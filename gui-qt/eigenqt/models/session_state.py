"""
session_state.py — SessionStateModel wrapping State RPC for session control strip.

Exposes: model name, effort, perm, title, goal, search/fast modes, tools, roots,
catalog (available models + effort levels).
Methods: setModel, setEffort, setPerm, setTitle, setGoal, setSearch, setFast
(invoke RPC, update on success).
"""

from typing import Optional

from PySide6.QtCore import QObject, Property, Signal, Slot

from eigenqt.rpc import RpcClient


class SessionStateModel(QObject):
    """Session state for control strip (model, effort, perm, title, goal, catalog)."""

    modelChanged = Signal()
    effortChanged = Signal()
    permChanged = Signal()
    titleChanged = Signal()
    goalChanged = Signal()
    searchChanged = Signal()
    fastChanged = Signal()
    fastOkChanged = Signal()
    toolsChanged = Signal()
    rootsChanged = Signal()
    catalogChanged = Signal()
    effortLevelsChanged = Signal()
    statusChanged = Signal()
    dirChanged = Signal()

    def __init__(self, client: RpcClient, session_id: str, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._session_id = session_id
        self._model = ""
        self._effort = ""
        self._perm = ""
        self._title = ""
        self._goal = ""
        self._search = ""
        self._fast = False
        self._fast_ok = False
        self._tools: list[dict] = []
        self._roots: list[str] = []
        self._catalog = []  # list of model names
        self._effort_levels = []  # list of effort levels for current model
        self._status = "idle"  # Computed from State RPC "running" field
        # The session's primary working directory (first Roots entry — the
        # State DTO carries roots, not a single dir). The diff/files dock
        # scopes to this.
        self._dir = ""

    @Property(str, notify=modelChanged)
    def model(self) -> str:
        return self._model

    @Property(str, notify=effortChanged)
    def effort(self) -> str:
        return self._effort

    @Property(str, notify=permChanged)
    def perm(self) -> str:
        return self._perm

    @Property(str, notify=titleChanged)
    def title(self) -> str:
        return self._title

    @Property(str, notify=goalChanged)
    def goal(self) -> str:
        return self._goal

    @Property(str, notify=searchChanged)
    def search(self) -> str:
        return self._search

    @Property(bool, notify=fastChanged)
    def fast(self) -> bool:
        return self._fast

    @Property(bool, notify=fastOkChanged)
    def fastOk(self) -> bool:
        return self._fast_ok

    @Property(list, notify=toolsChanged)
    def tools(self) -> list[dict]:
        return self._tools

    @Property(list, notify=rootsChanged)
    def roots(self) -> list[str]:
        return self._roots

    @Property(list, notify=catalogChanged)
    def catalog(self) -> list:
        return self._catalog

    @Property(list, notify=effortLevelsChanged)
    def effortLevels(self) -> list:
        return self._effort_levels

    @Property(str, notify=statusChanged)
    def status(self) -> str:
        return self._status

    @Property(str, notify=dirChanged)
    def dir(self) -> str:
        return self._dir

    @Slot(dict)
    def seed(self, state: dict) -> None:
        """Seed from State RPC result."""
        self._model = state.get("model", "")
        self._effort = state.get("effort", "")
        self._perm = state.get("perm", "")
        self._title = state.get("title", "")
        self._goal = state.get("goal", "")
        self._search = state.get("search", "")
        self._fast = bool(state.get("fast", False))
        self._fast_ok = bool(state.get("fastOk", False))
        self._tools = [
            {
                "name": str(tool.get("name", "")),
                "read_only": bool(tool.get("read_only", tool.get("readOnly", False))),
            }
            for tool in (state.get("tools") or [])
            if isinstance(tool, dict) and tool.get("name")
        ]

        # Compute status from "running" field
        running = state.get("running", False)
        self._status = "working" if running else "idle"

        # Primary working dir = first sandbox root (State has roots, not dir).
        roots = state.get("roots") or []
        self._roots = [str(root) for root in roots if root]
        self._dir = self._roots[0] if self._roots else ""

        # Extract catalog (from routing.catalog)
        catalog_data = state.get("catalog") or {}
        if isinstance(catalog_data, dict):
            models = catalog_data.get("models") or []
            self._catalog = [m.get("id", "") for m in models if isinstance(m, dict)]
        else:
            self._catalog = []

        # Extract effort levels for current model (from catalog.models[].effortLevels)
        self._effort_levels = []
        if isinstance(catalog_data, dict):
            models = catalog_data.get("models") or []
            for m in models:
                if isinstance(m, dict) and m.get("id") == self._model:
                    levels = m.get("effortLevels") or []
                    if isinstance(levels, list):
                        self._effort_levels = levels
                    break

        self.modelChanged.emit()
        self.effortChanged.emit()
        self.permChanged.emit()
        self.titleChanged.emit()
        self.goalChanged.emit()
        self.searchChanged.emit()
        self.fastChanged.emit()
        self.fastOkChanged.emit()
        self.toolsChanged.emit()
        self.rootsChanged.emit()
        self.catalogChanged.emit()
        self.effortLevelsChanged.emit()
        self.statusChanged.emit()
        self.dirChanged.emit()

    @Slot(str)
    def setModel(self, model: str) -> None:
        """Set model via RPC SetModel, update on success."""

        def on_result(result: dict) -> None:
            if "error" in result:
                print(f"SetModel error: {result['error']}")
                return
            # Re-seed from the returned SessionStateDTO
            self.seed(result["result"])

        self._client.call("SetModel", self._session_id, model, callback=on_result)

    @Slot(str)
    def setEffort(self, effort: str) -> None:
        """Set effort via RPC SetEffort, update on success."""

        def on_result(result: dict) -> None:
            if "error" in result:
                print(f"SetEffort error: {result['error']}")
                return
            self.seed(result["result"])

        self._client.call("SetEffort", self._session_id, effort, callback=on_result)

    @Slot(str)
    def setPerm(self, perm: str) -> None:
        """Set perm via RPC SetPerm, update on success."""

        def on_result(result: dict) -> None:
            if "error" in result:
                print(f"SetPerm error: {result['error']}")
                return
            self.seed(result["result"])

        self._client.call("SetPerm", self._session_id, perm, callback=on_result)

    @Slot(str)
    def setTitle(self, title: str) -> None:
        """Set title via RPC SetTitle, update on success."""

        def on_result(result: dict) -> None:
            if "error" in result:
                print(f"SetTitle error: {result['error']}")
                return
            self.seed(result["result"])

        self._client.call("SetTitle", self._session_id, title, callback=on_result)

    @Slot(str)
    def setGoal(self, goal: str) -> None:
        """Set persistent goal via RPC SetGoal, update on success."""

        def on_result(result: dict) -> None:
            if "error" in result:
                print(f"SetGoal error: {result['error']}")
                return
            self.seed(result["result"])

        self._client.call("SetGoal", self._session_id, goal, callback=on_result)

    @Slot(str)
    def setSearch(self, search: str) -> None:
        """Set live-search mode via RPC SetSearch, update on success."""

        def on_result(result: dict) -> None:
            if "error" in result:
                print(f"SetSearch error: {result['error']}")
                return
            self.seed(result["result"])

        self._client.call("SetSearch", self._session_id, search, callback=on_result)

    @Slot(bool)
    def setFast(self, fast: bool) -> None:
        """Toggle fast/priority tier via RPC SetFast, update on success."""

        def on_result(result: dict) -> None:
            if "error" in result:
                print(f"SetFast error: {result['error']}")
                return
            self.seed(result["result"])

        self._client.call("SetFast", self._session_id, bool(fast), callback=on_result)

    @Slot()
    def refresh(self) -> None:
        """Refresh session state from RPC State."""

        def on_result(result: dict) -> None:
            if "error" in result:
                print(f"State refresh error: {result['error']}")
                return
            self.seed(result["result"])

        self._client.call("State", self._session_id, callback=on_result)
