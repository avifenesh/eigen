"""
session_state.py — SessionStateModel wrapping State RPC for session control strip.

Exposes: model name, provider, token usage, effort, perm, title, goal,
search/fast modes, tools, roots, shells, pending approvals, catalog (available
models + effort levels).
Methods: setModel, setEffort, setPerm, setTitle, setGoal, setSearch, setFast
(invoke RPC, update on success).
"""

from typing import Any, Optional

from PySide6.QtCore import QObject, Property, Signal, Slot

from eigenqt.rpc import RpcClient


def _is_preferred_model(model_id: str) -> bool:
    """Return whether an id belongs in the focused GPT-5.5/5.6 picker."""
    normalized = str(model_id or "").lower()
    return normalized.startswith("gpt-5.5") or normalized.startswith("gpt-5.6") or normalized.startswith(
        "openai.gpt-5.5"
    ) or normalized.startswith("openai.gpt-5.6")


def _selectable_model_ids(models: list[dict]) -> list[str]:
    """Return only the GPT-5.5/5.6 options requested for the picker."""
    all_ids: list[str] = []
    for model in models:
        if not isinstance(model, dict):
            continue
        model_id = str(model.get("id", "") or "")
        if model_id and model_id not in all_ids:
            all_ids.append(model_id)

    preferred = [model_id for model_id in all_ids if _is_preferred_model(model_id)]
    if not preferred:
        return all_ids
    return preferred


def _err_text(value: Any) -> str:
    """Extract a readable RPC error string."""
    if isinstance(value, dict):
        nested = value.get("message") or value.get("error")
        if nested is not None and nested is not value:
            return _err_text(nested)
        return "Unknown error"
    return str(value) if value else "Unknown error"


def _int_value(value: Any, default: int = 0) -> int:
    try:
        return int(value)
    except (TypeError, ValueError):
        return default


class SessionStateModel(QObject):
    """Session state for control strip (model, effort, perm, title, goal, catalog)."""

    modelChanged = Signal()
    providerChanged = Signal()
    tokensChanged = Signal()
    maxTokensChanged = Signal()
    effortChanged = Signal()
    permChanged = Signal()
    titleChanged = Signal()
    goalChanged = Signal()
    searchChanged = Signal()
    fastChanged = Signal()
    fastOkChanged = Signal()
    toolsChanged = Signal()
    rootsChanged = Signal()
    shellsChanged = Signal()
    pendingChanged = Signal()
    catalogChanged = Signal()
    effortLevelsChanged = Signal()
    statusChanged = Signal()
    dirChanged = Signal()
    actionErrorChanged = Signal()

    def __init__(self, client: RpcClient, session_id: str, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._session_id = session_id
        self._model = ""
        self._provider = ""
        self._tokens = 0
        self._max_tokens = 0
        self._effort = ""
        self._perm = ""
        self._title = ""
        self._goal = ""
        self._search = ""
        self._fast = False
        self._fast_ok = False
        self._tools: list[dict] = []
        self._roots: list[str] = []
        self._shells: list[dict] = []
        self._pending: list[dict] = []
        self._catalog = []  # list of model names
        self._effort_levels = []  # list of effort levels for current model
        self._status = "idle"  # Computed from State RPC "running" field
        self._load_seq = 0
        self._action_error = ""
        # The session's primary working directory (first Roots entry — the
        # State DTO carries roots, not a single dir). The diff/files dock
        # scopes to this.
        self._dir = ""

    @Property(str, notify=modelChanged)
    def model(self) -> str:
        return self._model

    @Property(str, notify=providerChanged)
    def provider(self) -> str:
        return self._provider

    @Property(int, notify=tokensChanged)
    def tokens(self) -> int:
        return self._tokens

    @Property(int, notify=maxTokensChanged)
    def maxTokens(self) -> int:
        return self._max_tokens

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

    @Property(list, notify=shellsChanged)
    def shells(self) -> list[dict]:
        return self._shells

    @Property(list, notify=pendingChanged)
    def pending(self) -> list[dict]:
        return self._pending

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

    @Property(str, notify=actionErrorChanged)
    def actionError(self) -> str:
        return self._action_error

    @Slot(dict)
    def seed(self, state: dict) -> None:
        """Seed from State RPC result."""
        self._apply_state(state)

    def beginStateRequest(self) -> int:
        """Start an external State request and return its freshness token."""
        return self._next_seq()

    def applyState(self, state: dict, seq: Optional[int] = None) -> None:
        """Apply a State response if it belongs to the newest request."""
        if seq is not None and seq != self._load_seq:
            return
        self._apply_state(state)

    def _apply_state(self, state: dict) -> None:
        self._model = state.get("model", "")
        self._provider = str(state.get("provider", "") or "")
        self._tokens = max(0, _int_value(state.get("tokens", 0)))
        self._max_tokens = max(0, _int_value(state.get("maxTokens", state.get("max_tokens", 0))))
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

        self._shells = [
            {
                "id": str(shell.get("id", "")),
                "command": str(shell.get("command", "")),
                "status": str(shell.get("status", "")),
                "exit_code": _int_value(shell.get("exit_code", shell.get("exitCode", 0))),
                "last_line": str(shell.get("last_line", shell.get("lastLine", "")) or ""),
            }
            for shell in (state.get("shells") or [])
            if isinstance(shell, dict) and shell.get("id")
        ]
        self._pending = [
            {
                "id": str(item.get("id", "")),
                "tool": str(item.get("tool", "")),
                "args": str(item.get("args", "")),
            }
            for item in (state.get("pending") or [])
            if isinstance(item, dict) and item.get("id")
        ]

        # Extract catalog (from routing.catalog)
        catalog_data = state.get("catalog") or {}
        if isinstance(catalog_data, dict):
            models = catalog_data.get("models") or []
            self._catalog = _selectable_model_ids(models)
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
        self.providerChanged.emit()
        self.tokensChanged.emit()
        self.maxTokensChanged.emit()
        self.effortChanged.emit()
        self.permChanged.emit()
        self.titleChanged.emit()
        self.goalChanged.emit()
        self.searchChanged.emit()
        self.fastChanged.emit()
        self.fastOkChanged.emit()
        self.toolsChanged.emit()
        self.rootsChanged.emit()
        self.shellsChanged.emit()
        self.pendingChanged.emit()
        self.catalogChanged.emit()
        self.effortLevelsChanged.emit()
        self.statusChanged.emit()
        self.dirChanged.emit()

    @Slot(str)
    def setModel(self, model: str) -> None:
        """Set model via RPC SetModel, update on success."""
        seq = self._begin_action()

        def on_result(result: dict) -> None:
            self._on_state_result(result, seq, "set model")

        self._client.call("SetModel", self._session_id, model, callback=on_result)

    @Slot(str)
    def setEffort(self, effort: str) -> None:
        """Set effort via RPC SetEffort, update on success."""
        seq = self._begin_action()

        def on_result(result: dict) -> None:
            self._on_state_result(result, seq, "set reasoning effort")

        self._client.call("SetEffort", self._session_id, effort, callback=on_result)

    @Slot(str)
    def setPerm(self, perm: str) -> None:
        """Set perm via RPC SetPerm, update on success."""
        seq = self._begin_action()

        def on_result(result: dict) -> None:
            self._on_state_result(result, seq, "set permission mode")

        self._client.call("SetPerm", self._session_id, perm, callback=on_result)

    @Slot(str)
    def setTitle(self, title: str) -> None:
        """Set title via RPC SetTitle, update on success."""
        seq = self._begin_action()

        def on_result(result: dict) -> None:
            self._on_state_result(result, seq, "rename session")

        self._client.call("SetTitle", self._session_id, title, callback=on_result)

    @Slot(str)
    def setGoal(self, goal: str) -> None:
        """Set persistent goal via RPC SetGoal, update on success."""
        seq = self._begin_action()

        def on_result(result: dict) -> None:
            self._on_state_result(result, seq, "set goal")

        self._client.call("SetGoal", self._session_id, goal, callback=on_result)

    @Slot(str)
    def setSearch(self, search: str) -> None:
        """Set live-search mode via RPC SetSearch, update on success."""
        seq = self._begin_action()

        def on_result(result: dict) -> None:
            self._on_state_result(result, seq, "set live search")

        self._client.call("SetSearch", self._session_id, search, callback=on_result)

    @Slot(bool)
    def setFast(self, fast: bool) -> None:
        """Toggle fast/priority tier via RPC SetFast, update on success."""
        seq = self._begin_action()

        def on_result(result: dict) -> None:
            self._on_state_result(result, seq, "set fast tier")

        self._client.call("SetFast", self._session_id, bool(fast), callback=on_result)

    @Slot()
    def refresh(self) -> None:
        """Refresh session state from RPC State."""
        seq = self._begin_action()

        def on_result(result: dict) -> None:
            self._on_state_result(result, seq, "refresh session state")

        self._client.call("State", self._session_id, callback=on_result)

    @Slot()
    def clearActionError(self) -> None:
        self._set_action_error("")

    @Slot(str)
    def setActionError(self, message: str) -> None:
        self._set_action_error(message)

    def _begin_action(self) -> int:
        seq = self._next_seq()
        self._set_action_error("")
        return seq

    def _next_seq(self) -> int:
        self._load_seq += 1
        return self._load_seq

    def _on_state_result(self, result: dict, seq: int, label: str) -> None:
        if seq != self._load_seq:
            return
        if "error" in result:
            self._set_action_error(f"Could not {label}: {_err_text(result.get('error'))}")
            return
        self._set_action_error("")
        self._apply_state(result["result"])

    def _set_action_error(self, message: str) -> None:
        if message == self._action_error:
            return
        self._action_error = message
        self.actionErrorChanged.emit()
