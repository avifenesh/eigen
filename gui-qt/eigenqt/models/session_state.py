"""
session_state.py — SessionStateModel wrapping State RPC for session control strip.

Exposes: model name, effort, perm, title, goal, catalog (available models + effort levels).
Methods: setModel, setEffort, setPerm, setTitle (invoke RPC, update on success).
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
    catalogChanged = Signal()
    effortLevelsChanged = Signal()

    def __init__(self, client: RpcClient, session_id: str, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._session_id = session_id
        self._model = ""
        self._effort = ""
        self._perm = ""
        self._title = ""
        self._goal = ""
        self._catalog = []  # list of model names
        self._effort_levels = []  # list of effort levels for current model

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

    @Property(list, notify=catalogChanged)
    def catalog(self) -> list:
        return self._catalog

    @Property(list, notify=effortLevelsChanged)
    def effortLevels(self) -> list:
        return self._effort_levels

    @Slot(dict)
    def seed(self, state: dict) -> None:
        """Seed from State RPC result."""
        self._model = state.get("model", "")
        self._effort = state.get("effort", "")
        self._perm = state.get("perm", "")
        self._title = state.get("title", "")
        self._goal = state.get("goal", "")

        # Extract catalog (from routing.catalog)
        catalog_data = state.get("catalog", {})
        if isinstance(catalog_data, dict):
            models = catalog_data.get("models", [])
            self._catalog = [m.get("id", "") for m in models if isinstance(m, dict)]
        else:
            self._catalog = []

        # Extract effort levels for current model (from catalog.models[].effortLevels)
        self._effort_levels = []
        if isinstance(catalog_data, dict):
            models = catalog_data.get("models", [])
            for m in models:
                if isinstance(m, dict) and m.get("id") == self._model:
                    levels = m.get("effortLevels", [])
                    if isinstance(levels, list):
                        self._effort_levels = levels
                    break

        self.modelChanged.emit()
        self.effortChanged.emit()
        self.permChanged.emit()
        self.titleChanged.emit()
        self.goalChanged.emit()
        self.catalogChanged.emit()
        self.effortLevelsChanged.emit()

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
