"""
connectors.py — ConnectorsModel for the Connectors view.

Loads Connectors, MCPServers, GoogleStatus, ObsidianStatus, RevutoStatus.
Handles mutations: AddConnector, AddCatalogConnector, ConnectConnector,
DisconnectConnector, RemoveConnector (inline confirm), SetMCPServerDisabled,
RemoveMCPServer (inline confirm), SaveMCPServer, Google connect/disconnect/import,
ChooseObsidianVault, Revuto pause/trigger actions.
"""

from typing import Optional

from PySide6.QtCore import QObject, Property, Signal, Slot

from eigenqt.rpc import RpcClient


def _err_text(err) -> str:
    """Extract a readable RPC error string."""
    if isinstance(err, dict):
        return err.get("message", "Unknown error")
    return str(err) if err else "Unknown error"


class ConnectorsModel(QObject):
    """Connectors model — integrations surface."""

    # Signals for reactive properties
    connectors_changed = Signal()
    servers_changed = Signal()
    google_status_changed = Signal()
    obsidian_status_changed = Signal()
    revuto_status_changed = Signal()
    loading_changed = Signal()
    load_error_changed = Signal()
    busy_changed = Signal()
    connecting_changed = Signal()
    confirm_remove_connector_changed = Signal()
    confirm_remove_server_changed = Signal()
    google_busy_changed = Signal()
    obsidian_busy_changed = Signal()
    action_error_changed = Signal()
    revuto_open_changed = Signal()
    reviewers_changed = Signal()
    revuto_busy_changed = Signal()
    secrets_ok_changed = Signal()
    # Add-connector form
    add_open_changed = Signal()
    add_name_changed = Signal()
    add_url_changed = Signal()
    add_desc_changed = Signal()
    adding_changed = Signal()
    # Add-local-MCP-server form
    srv_open_changed = Signal()
    srv_name_changed = Signal()
    srv_command_changed = Signal()
    srv_desc_changed = Signal()
    srv_env_changed = Signal()
    srv_secret_changed = Signal()
    srv_saving_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._connectors: Optional[dict] = None
        self._servers: Optional[dict] = None
        self._google_status: Optional[dict] = None
        self._obsidian_status: Optional[dict] = None
        self._revuto_status: Optional[dict] = None
        self._loading: bool = True
        self._load_error: str = ""
        self._busy: dict[str, bool] = {}
        self._connecting: dict[str, bool] = {}
        self._confirm_remove_connector: dict[str, bool] = {}
        self._confirm_remove_server: dict[str, bool] = {}
        self._google_busy: bool = False
        self._obsidian_busy: bool = False
        self._action_error: str = ""
        self._revuto_open: bool = False
        self._reviewers: list[dict] = []
        self._revuto_busy: dict[str, bool] = {}
        self._secrets_ok: bool = True
        # Add-connector form
        self._add_open: bool = False
        self._add_name: str = ""
        self._add_url: str = ""
        self._add_desc: str = ""
        self._adding: bool = False
        # Add-local-MCP-server form
        self._srv_open: bool = False
        self._srv_name: str = ""
        self._srv_command: str = ""
        self._srv_desc: str = ""
        self._srv_env: str = ""
        self._srv_secret: str = ""
        self._srv_saving: bool = False
        self._load_seq: int = 0
        self._connector_event_channel = "eigen:connector"

        # Load on connected
        self._client.connected.connect(self._on_connected)
        self._client.event.connect(self._on_event)

    # Properties
    @Property("QVariant", notify=connectors_changed)
    def connectors(self):
        return self._connectors

    @connectors.setter
    def connectors(self, value):
        if self._connectors != value:
            self._connectors = value
            self.connectors_changed.emit()

    @Property("QVariant", notify=servers_changed)
    def servers(self):
        return self._servers

    @servers.setter
    def servers(self, value):
        if self._servers != value:
            self._servers = value
            self.servers_changed.emit()

    @Property("QVariant", notify=google_status_changed)
    def google_status(self):
        return self._google_status

    @google_status.setter
    def google_status(self, value):
        if self._google_status != value:
            self._google_status = value
            self.google_status_changed.emit()

    @Property("QVariant", notify=obsidian_status_changed)
    def obsidian_status(self):
        return self._obsidian_status

    @obsidian_status.setter
    def obsidian_status(self, value):
        if self._obsidian_status != value:
            self._obsidian_status = value
            self.obsidian_status_changed.emit()

    @Property("QVariant", notify=revuto_status_changed)
    def revuto_status(self):
        return self._revuto_status

    @revuto_status.setter
    def revuto_status(self, value):
        if self._revuto_status != value:
            self._revuto_status = value
            self.revuto_status_changed.emit()

    @Property(bool, notify=loading_changed)
    def loading(self) -> bool:
        return self._loading

    @loading.setter
    def loading(self, value: bool):
        if self._loading != value:
            self._loading = value
            self.loading_changed.emit()

    @Property(str, notify=load_error_changed)
    def load_error(self) -> str:
        return self._load_error

    @load_error.setter
    def load_error(self, value: str):
        if self._load_error != value:
            self._load_error = value
            self.load_error_changed.emit()

    @Property("QVariant", notify=busy_changed)
    def busy(self):
        return self._busy

    @Property("QVariant", notify=connecting_changed)
    def connecting(self):
        return self._connecting

    @Property("QVariant", notify=confirm_remove_connector_changed)
    def confirm_remove_connector(self):
        return self._confirm_remove_connector

    @Property("QVariant", notify=confirm_remove_server_changed)
    def confirm_remove_server(self):
        return self._confirm_remove_server

    @Property(bool, notify=google_busy_changed)
    def google_busy(self) -> bool:
        return self._google_busy

    @google_busy.setter
    def google_busy(self, value: bool):
        if self._google_busy != value:
            self._google_busy = value
            self.google_busy_changed.emit()

    @Property(bool, notify=obsidian_busy_changed)
    def obsidian_busy(self) -> bool:
        return self._obsidian_busy

    @obsidian_busy.setter
    def obsidian_busy(self, value: bool):
        if self._obsidian_busy != value:
            self._obsidian_busy = value
            self.obsidian_busy_changed.emit()

    @Property(str, notify=action_error_changed)
    def action_error(self) -> str:
        return self._action_error

    @action_error.setter
    def action_error(self, value: str):
        if self._action_error != value:
            self._action_error = value
            self.action_error_changed.emit()

    @Property(bool, notify=revuto_open_changed)
    def revuto_open(self) -> bool:
        return self._revuto_open

    @revuto_open.setter
    def revuto_open(self, value: bool):
        if self._revuto_open != value:
            self._revuto_open = value
            self.revuto_open_changed.emit()

    @Property(list, notify=reviewers_changed)
    def reviewers(self) -> list[dict]:
        return self._reviewers

    @reviewers.setter
    def reviewers(self, value: list[dict]):
        if self._reviewers != value:
            self._reviewers = value
            self.reviewers_changed.emit()

    @Property("QVariant", notify=revuto_busy_changed)
    def revuto_busy(self):
        return self._revuto_busy

    @Property(bool, notify=secrets_ok_changed)
    def secrets_ok(self) -> bool:
        return self._secrets_ok

    @secrets_ok.setter
    def secrets_ok(self, value: bool):
        if self._secrets_ok != value:
            self._secrets_ok = value
            self.secrets_ok_changed.emit()

    # Add-connector form properties
    @Property(bool, notify=add_open_changed)
    def add_open(self) -> bool:
        return self._add_open

    @add_open.setter
    def add_open(self, value: bool):
        if self._add_open != value:
            self._add_open = value
            self.add_open_changed.emit()

    @Property(str, notify=add_name_changed)
    def add_name(self) -> str:
        return self._add_name

    @add_name.setter
    def add_name(self, value: str):
        if self._add_name != value:
            self._add_name = value
            self.add_name_changed.emit()

    @Property(str, notify=add_url_changed)
    def add_url(self) -> str:
        return self._add_url

    @add_url.setter
    def add_url(self, value: str):
        if self._add_url != value:
            self._add_url = value
            self.add_url_changed.emit()

    @Property(str, notify=add_desc_changed)
    def add_desc(self) -> str:
        return self._add_desc

    @add_desc.setter
    def add_desc(self, value: str):
        if self._add_desc != value:
            self._add_desc = value
            self.add_desc_changed.emit()

    @Property(bool, notify=adding_changed)
    def adding(self) -> bool:
        return self._adding

    @adding.setter
    def adding(self, value: bool):
        if self._adding != value:
            self._adding = value
            self.adding_changed.emit()

    # Add-local-MCP-server form properties
    @Property(bool, notify=srv_open_changed)
    def srv_open(self) -> bool:
        return self._srv_open

    @srv_open.setter
    def srv_open(self, value: bool):
        if self._srv_open != value:
            self._srv_open = value
            self.srv_open_changed.emit()

    @Property(str, notify=srv_name_changed)
    def srv_name(self) -> str:
        return self._srv_name

    @srv_name.setter
    def srv_name(self, value: str):
        if self._srv_name != value:
            self._srv_name = value
            self.srv_name_changed.emit()

    @Property(str, notify=srv_command_changed)
    def srv_command(self) -> str:
        return self._srv_command

    @srv_command.setter
    def srv_command(self, value: str):
        if self._srv_command != value:
            self._srv_command = value
            self.srv_command_changed.emit()

    @Property(str, notify=srv_desc_changed)
    def srv_desc(self) -> str:
        return self._srv_desc

    @srv_desc.setter
    def srv_desc(self, value: str):
        if self._srv_desc != value:
            self._srv_desc = value
            self.srv_desc_changed.emit()

    @Property(str, notify=srv_env_changed)
    def srv_env(self) -> str:
        return self._srv_env

    @srv_env.setter
    def srv_env(self, value: str):
        if self._srv_env != value:
            self._srv_env = value
            self.srv_env_changed.emit()

    @Property(str, notify=srv_secret_changed)
    def srv_secret(self) -> str:
        return self._srv_secret

    @srv_secret.setter
    def srv_secret(self, value: str):
        if self._srv_secret != value:
            self._srv_secret = value
            self.srv_secret_changed.emit()

    @Property(bool, notify=srv_saving_changed)
    def srv_saving(self) -> bool:
        return self._srv_saving

    @srv_saving.setter
    def srv_saving(self, value: bool):
        if self._srv_saving != value:
            self._srv_saving = value
            self.srv_saving_changed.emit()

    # Methods
    def _on_connected(self):
        """Load data on daemon connection."""
        self._client.subscribe([self._connector_event_channel])
        self.load()

    @Slot(str, dict)
    def _on_event(self, channel: str, data: dict):
        """Handle connector OAuth completion events from guiserver."""
        if channel != self._connector_event_channel:
            return
        name = str((data or {}).get("name") or "").strip()
        if name:
            self._clear_connecting(name)
        if not (data or {}).get("ok", False):
            message = (data or {}).get("error") or "authorization failed"
            self.action_error = (
                f"Could not authorize {name}: {message}"
                if name
                else f"Connector authorization failed: {message}"
            )
        self.load()

    @Slot()
    def load(self):
        """Load connectors, servers, and status via three independent RPCs.

        RpcClient has no batch primitive — calls are id-multiplexed and
        already run concurrently on the wire; loading clears when the last
        of the three current-load calls lands (order-independent countdown).
        """
        self._load_seq += 1
        seq = self._load_seq
        self.loading = True
        self.load_error = ""
        pending = {"n": 3}

        def is_current() -> bool:
            return seq == self._load_seq

        def done_one():
            if not is_current():
                return
            pending["n"] -= 1
            if pending["n"] == 0:
                self.loading = False

        def on_connectors(r):
            if not is_current():
                return
            if "error" in r:
                self.load_error = _err_text(r["error"])
            else:
                self.connectors = r.get("result") or {}
            done_one()

        def on_servers(r):
            if not is_current():
                return
            if "error" in r:
                self.load_error = _err_text(r["error"])
            else:
                self.servers = r.get("result") or {}
            done_one()

        def on_google(r):
            if not is_current():
                return
            if "error" in r:
                self.load_error = _err_text(r["error"])
            else:
                self.google_status = r.get("result") or {}
            done_one()

        def on_obsidian(r):
            if is_current():
                self.obsidian_status = r.get("result") or {}

        def on_revuto(r):
            if is_current():
                self.revuto_status = r.get("result") or {}

        def on_secrets(r):
            if is_current():
                self.secrets_ok = bool(r.get("result", True))

        self._client.call("Connectors", callback=on_connectors)
        self._client.call("MCPServers", callback=on_servers)
        self._client.call("GoogleStatus", callback=on_google)

        # Best-effort loads for Obsidian/Revuto
        self._client.call("ObsidianStatus", callback=on_obsidian)
        self._client.call("RevutoStatus", callback=on_revuto)
        self._client.call("MCPSecretsAvailable", callback=on_secrets)

    def _clear_busy(self, name: str):
        self._busy.pop(name, None)
        self.busy_changed.emit()

    def _clear_connecting(self, name: str):
        self._connecting.pop(name, None)
        self.connecting_changed.emit()

    def _clear_revuto_busy(self, repo: str):
        self._revuto_busy.pop(repo, None)
        self.revuto_busy_changed.emit()

    def _fail_action(self, label: str, err):
        self.action_error = f"{label}: {_err_text(err)}"

    @Slot(str)
    def add_connector(self, name: str):
        """Add a custom connector by name, URL, description."""
        if self._adding:
            return
        if not name.strip() or not self._add_url.strip():
            self.action_error = "Connector name and URL are required."
            return
        name = name.strip()
        url = self._add_url.strip()
        desc = self._add_desc.strip()
        self.adding = True
        self.action_error = ""

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self._connecting[name] = True
            self.connecting_changed.emit()
            self.add_open = False
            self.add_name = ""
            self.add_url = ""
            self.add_desc = ""
            self.adding = False
            self.load()

        def on_error(err):
            self.adding = False
            self.action_error = f"Could not add connector {name}: {_err_text(err)}"

        self._client.call(
            "AddConnector",
            name,
            url,
            desc,
            callback=on_done,
        )

    @Slot()
    def cancel_add_connector(self):
        """Close and clear the custom connector form."""
        self.add_open = False
        self.add_name = ""
        self.add_url = ""
        self.add_desc = ""
        self.action_error = ""

    @Slot(str)
    def add_from_catalog(self, name: str):
        """Add a catalog connector by name."""
        name = name.strip()
        if not name or self._connecting.get(name):
            return
        self._connecting[name] = True
        self.connecting_changed.emit()
        self.action_error = ""

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self.load()

        def on_error(err):
            self._clear_connecting(name)
            self._fail_action(f"Could not add {name}", err)

        self._client.call("AddCatalogConnector", name, callback=on_done)

    @Slot(str)
    def connect_connector(self, name: str):
        """Initiate OAuth flow for a connector."""
        name = name.strip()
        if not name or self._busy.get(name) or self._connecting.get(name):
            return
        self._connecting[name] = True
        self.connecting_changed.emit()
        self.action_error = ""

        def on_error(err):
            self._clear_connecting(name)
            self._fail_action(f"Could not connect {name}", err)

        def on_done(result):
            if "error" in result:
                on_error(result["error"])

        self._client.call("ConnectConnector", name, callback=on_done)

    @Slot(str)
    def disconnect_connector(self, name: str):
        """Disconnect a connector."""
        if self._busy.get(name) or self._connecting.get(name):
            return
        self._busy[name] = True
        self.busy_changed.emit()
        self.action_error = ""

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self._clear_busy(name)
            self.load()

        def on_error(err):
            self._clear_busy(name)
            self._fail_action(f"Could not disconnect {name}", err)

        self._client.call("DisconnectConnector", name, callback=on_done)

    @Slot(str)
    def remove_connector(self, name: str):
        """Remove a connector (after inline confirm)."""
        if self._busy.get(name) or self._connecting.get(name):
            return
        self._busy[name] = True
        self.busy_changed.emit()
        self.action_error = ""
        if name in self._confirm_remove_connector:
            del self._confirm_remove_connector[name]
            self.confirm_remove_connector_changed.emit()

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self._clear_busy(name)
            self.load()

        def on_error(err):
            self._clear_busy(name)
            self._fail_action(f"Could not remove {name}", err)

        self._client.call("RemoveConnector", name, callback=on_done)

    @Slot(str)
    def confirm_remove_connector_set(self, name: str):
        """Set confirm state for connector removal."""
        if self._busy.get(name) or self._connecting.get(name):
            return
        self._confirm_remove_connector[name] = True
        self.confirm_remove_connector_changed.emit()

    @Slot(str)
    def confirm_remove_connector_cancel(self, name: str):
        """Cancel confirm state for connector removal."""
        if name in self._confirm_remove_connector:
            del self._confirm_remove_connector[name]
            self.confirm_remove_connector_changed.emit()

    @Slot(str, bool)
    def toggle_server(self, name: str, disabled: bool):
        """Enable/disable an MCP server."""
        if self._busy.get(name):
            return
        self._busy[name] = True
        self.busy_changed.emit()
        self.action_error = ""

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self._clear_busy(name)
            self.load()

        def on_error(err):
            self._clear_busy(name)
            self._fail_action(f"Could not update {name}", err)

        self._client.call("SetMCPServerDisabled", name, disabled, callback=on_done)

    @Slot(str)
    def remove_server(self, name: str):
        """Remove an MCP server (after inline confirm)."""
        if self._busy.get(name):
            return
        self._busy[name] = True
        self.busy_changed.emit()
        self.action_error = ""
        if name in self._confirm_remove_server:
            del self._confirm_remove_server[name]
            self.confirm_remove_server_changed.emit()

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self._clear_busy(name)
            self.load()

        def on_error(err):
            self._clear_busy(name)
            self._fail_action(f"Could not remove MCP server {name}", err)

        self._client.call("RemoveMCPServer", name, callback=on_done)

    @Slot(str)
    def confirm_remove_server_set(self, name: str):
        """Set confirm state for server removal."""
        if self._busy.get(name):
            return
        self._confirm_remove_server[name] = True
        self.confirm_remove_server_changed.emit()

    @Slot(str)
    def confirm_remove_server_cancel(self, name: str):
        """Cancel confirm state for server removal."""
        if name in self._confirm_remove_server:
            del self._confirm_remove_server[name]
            self.confirm_remove_server_changed.emit()

    @Slot()
    def save_local_server(self):
        """Save a local MCP server (stdio)."""
        if self._srv_saving:
            return
        name = (self._srv_name or "").strip()
        cmd = [c for c in (self._srv_command or "").strip().split() if c]
        if not name or not cmd:
            self.action_error = "MCP server name and command are required."
            return

        def split_lines(s: str) -> list[str]:
            return [line.strip() for line in (s or "").split("\n") if line.strip()]

        env_pairs = split_lines(self._srv_env)
        secret_pairs = split_lines(self._srv_secret) if self._secrets_ok else []
        if not self._secrets_ok:
            env_pairs.extend(split_lines(self._srv_secret))

        server_dto = {
            "name": name,
            "command": cmd,
            "description": (self._srv_desc or "").strip(),
            "disabled": False,
            "remote": False,
            "envPairs": env_pairs,
            "secretEnvPairs": secret_pairs,
        }

        self.srv_saving = True
        self.action_error = ""

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self.srv_open = False
            self.srv_name = ""
            self.srv_command = ""
            self.srv_desc = ""
            self.srv_env = ""
            self.srv_secret = ""
            self.srv_saving = False
            self.load()

        def on_error(err):
            self.srv_saving = False
            self.action_error = f"Could not save MCP server {name}: {_err_text(err)}"

        self._client.call("SaveMCPServer", server_dto, callback=on_done)

    @Slot()
    def cancel_local_server(self):
        """Close and clear the local MCP server form."""
        self.srv_open = False
        self.srv_name = ""
        self.srv_command = ""
        self.srv_desc = ""
        self.srv_env = ""
        self.srv_secret = ""
        self.action_error = ""

    @Slot()
    def clear_action_error(self):
        """Dismiss the current connector action error."""
        self.action_error = ""

    @Slot(str)
    def choose_vault(self, path: str):
        """Choose Obsidian vault."""
        if self._obsidian_busy:
            return
        self.obsidian_busy = True
        self.action_error = ""

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self.obsidian_busy = False
            if result.get("result"):
                # Refresh Obsidian status
                self._client.call("ObsidianStatus", callback=lambda r: setattr(self, "obsidian_status", r.get("result") or {}))

        def on_error(err):
            self.obsidian_busy = False
            self._fail_action("Could not choose Obsidian vault", err)

        self._client.call("ChooseObsidianVault", path, callback=on_done)

    @Slot()
    def toggle_revuto(self):
        """Toggle revuto reviewer panel."""
        self.revuto_open = not self._revuto_open
        if self._revuto_open and not self._reviewers:
            self._client.call("RevutoReviewers", callback=lambda r: setattr(self, "reviewers", r.get("result") or []))

    @Slot(str, bool)
    def revuto_pause(self, repo: str, paused: bool):
        """Pause/resume a revuto reviewer."""
        if self._revuto_busy.get(repo):
            return
        self._revuto_busy[repo] = True
        self.revuto_busy_changed.emit()
        self.action_error = ""

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self._clear_revuto_busy(repo)
            # Refresh reviewers + status
            self._client.call("RevutoReviewers", callback=lambda r: setattr(self, "reviewers", r.get("result") or []))
            self._client.call("RevutoStatus", callback=lambda r: setattr(self, "revuto_status", r.get("result") or {}))

        def on_error(err):
            self._clear_revuto_busy(repo)
            self._fail_action(f"Could not update {repo}", err)

        self._client.call("RevutoSetPaused", repo, paused, callback=on_done)

    @Slot(str)
    def revuto_trigger(self, repo: str):
        """Trigger a revuto review."""
        if self._revuto_busy.get(repo):
            return
        self._revuto_busy[repo] = True
        self.revuto_busy_changed.emit()
        self.action_error = ""

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self._clear_revuto_busy(repo)

        def on_error(err):
            self._clear_revuto_busy(repo)
            self._fail_action(f"Could not run review for {repo}", err)

        self._client.call("RevutoTrigger", repo, "review", callback=on_done)

    @Slot()
    def setup_google(self):
        """Setup Google (open Cloud Console + import client JSON)."""
        if self._google_busy:
            return
        self.google_busy = True
        self.action_error = ""

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self.google_busy = False
            if result.get("result"):
                self.load()

        def on_error(err):
            self.google_busy = False
            self._fail_action("Could not import Google client", err)

        self._client.call("ImportGoogleClient", callback=on_done)

    @Slot()
    def connect_google(self):
        """Connect Google (OAuth flow)."""
        if self._google_busy:
            return
        self.google_busy = True
        self.action_error = ""

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self.google_busy = False
            self.load()

        def on_error(err):
            self.google_busy = False
            self._fail_action("Could not connect Google", err)

        self._client.call("ConnectGoogle", callback=on_done)

    @Slot()
    def disconnect_google(self):
        """Disconnect Google."""
        if self._google_busy:
            return
        self.google_busy = True
        self.action_error = ""

        def on_done(result):
            if "error" in result:
                on_error(result["error"])
                return
            self.google_busy = False
            self.load()

        def on_error(err):
            self.google_busy = False
            self._fail_action("Could not disconnect Google", err)

        self._client.call("DisconnectGoogle", callback=on_done)
