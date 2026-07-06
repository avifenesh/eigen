#!/usr/bin/env python3
"""
test_connectors_model.py — pytest for ConnectorsModel logic.
"""
import pytest

from eigenqt.models.connectors import ConnectorsModel


class MockSignal:
    def connect(self, _):
        pass


class MockRpcClient:
    """Mock RPC client."""

    def __init__(self):
        self.connected = MockSignal()
        self.calls = []
        self.failures = {}

    def call(self, method, *args, callback=None):
        self.calls.append((method, args))
        if method in self.failures:
            if callback:
                callback({"error": self.failures[method]})
            return
        if callback:
            callback({"result": self._mock_result(method, *args)})

    def _mock_result(self, method, *args):
        if method == "Connectors":
            return {"connectors": [], "directory": []}
        elif method == "MCPServers":
            return {"servers": []}
        elif method == "GoogleStatus":
            return {"configured": False, "connected": False, "clientPath": "", "setupUrl": "", "setupHint": ""}
        elif method == "ObsidianStatus":
            return {"available": False, "vault": ""}
        elif method == "RevutoStatus":
            return {"available": False, "count": 0, "paused": 0}
        elif method == "MCPSecretsAvailable":
            return True
        return {}


class DeferredRpcClient:
    """RPC client that lets tests resolve callbacks in arbitrary order."""

    def __init__(self):
        self.connected = MockSignal()
        self.calls = []

    def call(self, method, *args, callback=None):
        self.calls.append(
            {
                "method": method,
                "args": args,
                "callback": callback,
            }
        )


def _call_by_method(calls, method):
    matches = [call for call in calls if call["method"] == method]
    assert matches, f"missing deferred call for {method}: {calls}"
    return matches[-1]


def _reply(call, *, result=None, error=None):
    callback = call["callback"]
    assert callback is not None, f"{call['method']} did not register a callback"
    callback({"error": error} if error is not None else {"result": result})


@pytest.fixture
def client():
    return MockRpcClient()


@pytest.fixture
def model(client):
    m = ConnectorsModel(client)
    return m


def test_init(model):
    """Test model initialization."""
    assert model.connectors is None
    assert model.servers is None
    assert model.google_status is None
    assert model.loading is True
    assert model.load_error == ""
    assert model.secrets_ok is True


def test_load(model, client):
    """Test load() fans out the current RPC envelope and sets properties."""
    model.load()
    assert [method for method, _ in client.calls] == [
        "Connectors",
        "MCPServers",
        "GoogleStatus",
        "ObsidianStatus",
        "RevutoStatus",
        "MCPSecretsAvailable",
    ]
    assert model.connectors == {"connectors": [], "directory": []}
    assert model.servers == {"servers": []}
    assert model.google_status == {
        "configured": False,
        "connected": False,
        "clientPath": "",
        "setupUrl": "",
        "setupHint": "",
    }
    assert model.obsidian_status == {"available": False, "vault": ""}
    assert model.revuto_status == {"available": False, "count": 0, "paused": 0}
    assert model.secrets_ok is True
    assert model.loading is False


def test_load_ignores_stale_callbacks_from_older_refresh():
    """Late replies from a superseded refresh must not overwrite fresh state."""
    client = DeferredRpcClient()
    model = ConnectorsModel(client)

    model.load()
    first = list(client.calls)
    model.load()
    second = client.calls[len(first):]

    assert [call["method"] for call in first] == [
        "Connectors",
        "MCPServers",
        "GoogleStatus",
        "ObsidianStatus",
        "RevutoStatus",
        "MCPSecretsAvailable",
    ]
    assert [call["method"] for call in second] == [call["method"] for call in first]

    _reply(
        _call_by_method(second, "Connectors"),
        result={"connectors": [{"name": "fresh"}], "directory": []},
    )
    _reply(_call_by_method(second, "MCPServers"), result={"servers": [{"name": "fresh-server"}]})
    _reply(_call_by_method(second, "GoogleStatus"), result={"configured": True, "connected": True})
    assert model.loading is False

    _reply(_call_by_method(second, "ObsidianStatus"), result={"available": True, "vault": "/fresh"})
    _reply(_call_by_method(second, "RevutoStatus"), result={"available": True, "count": 2, "paused": 0})
    _reply(_call_by_method(second, "MCPSecretsAvailable"), result=False)

    _reply(_call_by_method(first, "Connectors"), error="stale connectors exploded")
    _reply(_call_by_method(first, "MCPServers"), result={"servers": [{"name": "stale-server"}]})
    _reply(_call_by_method(first, "GoogleStatus"), result={"configured": False, "connected": False})
    _reply(_call_by_method(first, "ObsidianStatus"), result={"available": False, "vault": "/stale"})
    _reply(_call_by_method(first, "RevutoStatus"), result={"available": False, "count": 0, "paused": 0})
    _reply(_call_by_method(first, "MCPSecretsAvailable"), result=True)

    assert model.connectors == {"connectors": [{"name": "fresh"}], "directory": []}
    assert model.servers == {"servers": [{"name": "fresh-server"}]}
    assert model.google_status == {"configured": True, "connected": True}
    assert model.obsidian_status == {"available": True, "vault": "/fresh"}
    assert model.revuto_status == {"available": True, "count": 2, "paused": 0}
    assert model.secrets_ok is False
    assert model.load_error == ""
    assert model.loading is False


def test_add_connector(model, client):
    """Test add_connector() calls AddConnector RPC."""
    model.add_name = "notion"
    model.add_url = "https://mcp.notion.com/mcp"
    model.add_desc = "Notion workspace"
    model.add_connector("notion")
    assert ("AddConnector", ("notion", "https://mcp.notion.com/mcp", "Notion workspace")) in client.calls


def test_add_connector_error_keeps_form_for_retry(model, client):
    """Failed AddConnector should surface an error and preserve typed fields."""
    client.failures["AddConnector"] = {"message": "add denied"}
    model.add_open = True
    model.add_name = "notion"
    model.add_url = "https://mcp.notion.com/mcp"
    model.add_desc = "Notion workspace"

    model.add_connector("notion")

    assert model.adding is False
    assert model.add_open is True
    assert model.add_name == "notion"
    assert model.add_url == "https://mcp.notion.com/mcp"
    assert model.add_desc == "Notion workspace"
    assert model.action_error == "Could not add connector notion: add denied"


def test_cancel_add_connector_clears_form(model):
    """Canceling a custom connector add should not leave stale typed values."""
    model.add_open = True
    model.add_name = "linear"
    model.add_url = "https://mcp.linear.app/mcp"
    model.add_desc = "Linear issues"
    model.action_error = "Could not add connector linear: denied"

    model.cancel_add_connector()

    assert model.add_open is False
    assert model.add_name == ""
    assert model.add_url == ""
    assert model.add_desc == ""
    assert model.action_error == ""


def test_connect_connector(model, client):
    """Test connect_connector() calls ConnectConnector RPC."""
    model.connect_connector("slack")
    assert ("ConnectConnector", ("slack",)) in client.calls


def test_catalog_and_connect_errors_clear_connecting_state(model, client):
    """Failed catalog/OAuth starts should not leave an infinite authorizing chip."""
    client.failures["AddCatalogConnector"] = {"message": "catalog denied"}
    model.add_from_catalog("slack")

    assert model.connecting == {}
    assert model.action_error == "Could not add slack: catalog denied"

    del client.failures["AddCatalogConnector"]
    client.failures["ConnectConnector"] = "oauth denied"
    model.connect_connector("slack")

    assert model.connecting == {}
    assert model.action_error == "Could not connect slack: oauth denied"


def test_disconnect_connector(model, client):
    """Test disconnect_connector() calls DisconnectConnector RPC."""
    model.disconnect_connector("notion")
    assert ("DisconnectConnector", ("notion",)) in client.calls


@pytest.mark.parametrize(
    "method,args,expected_error",
    [
        ("disconnect_connector", ("notion",), "Could not disconnect notion: disconnect denied"),
        ("remove_connector", ("notion",), "Could not remove notion: remove denied"),
        ("toggle_server", ("github", True), "Could not update github: toggle denied"),
        ("remove_server", ("github",), "Could not remove MCP server github: remove denied"),
    ],
)
def test_busy_connector_actions_clear_busy_on_error(model, client, method, args, expected_error):
    """Failed connector/server actions clear busy state and remain retryable."""
    rpc_method = {
        "disconnect_connector": "DisconnectConnector",
        "remove_connector": "RemoveConnector",
        "toggle_server": "SetMCPServerDisabled",
        "remove_server": "RemoveMCPServer",
    }[method]
    client.failures[rpc_method] = expected_error.rsplit(": ", 1)[1]

    getattr(model, method)(*args)

    assert model.busy == {}
    assert model.action_error == expected_error


def test_remove_connector(model, client):
    """Test remove_connector() calls RemoveConnector RPC."""
    model.remove_connector("slack")
    assert ("RemoveConnector", ("slack",)) in client.calls


def test_toggle_server(model, client):
    """Test toggle_server() calls SetMCPServerDisabled RPC."""
    model.toggle_server("github", True)
    assert ("SetMCPServerDisabled", ("github", True)) in client.calls


def test_remove_server(model, client):
    """Test remove_server() calls RemoveMCPServer RPC."""
    model.remove_server("filesystem")
    assert ("RemoveMCPServer", ("filesystem",)) in client.calls


def test_save_local_server(model, client):
    """Test save_local_server() calls SaveMCPServer RPC."""
    model.srv_name = "test"
    model.srv_command = "npx test-server"
    model.srv_desc = "Test server"
    model.srv_env = "KEY1=value1\nKEY2=value2"
    model.srv_secret = "SECRET=xyz"
    model.save_local_server()
    # Find the SaveMCPServer call
    save_calls = [c for c in client.calls if c[0] == "SaveMCPServer"]
    assert len(save_calls) == 1
    _, (server_dto,) = save_calls[0]
    assert server_dto["name"] == "test"
    assert server_dto["command"] == ["npx", "test-server"]
    assert server_dto["description"] == "Test server"
    assert server_dto["envPairs"] == ["KEY1=value1", "KEY2=value2"]
    assert server_dto["secretEnvPairs"] == ["SECRET=xyz"]


def test_save_local_server_error_keeps_form_for_retry(model, client):
    """Failed local server save should keep all typed server fields."""
    client.failures["SaveMCPServer"] = "save denied"
    model.srv_open = True
    model.srv_name = "test"
    model.srv_command = "npx test-server"
    model.srv_desc = "Test server"
    model.srv_env = "KEY1=value1"
    model.srv_secret = "SECRET=xyz"

    model.save_local_server()

    assert model.srv_saving is False
    assert model.srv_open is True
    assert model.srv_name == "test"
    assert model.srv_command == "npx test-server"
    assert model.srv_desc == "Test server"
    assert model.srv_env == "KEY1=value1"
    assert model.srv_secret == "SECRET=xyz"
    assert model.action_error == "Could not save MCP server test: save denied"


def test_cancel_local_server_clears_form(model):
    """Canceling a local MCP server add should clear all pending fields."""
    model.srv_open = True
    model.srv_name = "github-local"
    model.srv_command = "uvx github-mcp-server"
    model.srv_desc = "GitHub MCP"
    model.srv_env = "LOG_LEVEL=info"
    model.srv_secret = "GITHUB_TOKEN=secret"
    model.action_error = "Could not save MCP server github-local: denied"

    model.cancel_local_server()

    assert model.srv_open is False
    assert model.srv_name == ""
    assert model.srv_command == ""
    assert model.srv_desc == ""
    assert model.srv_env == ""
    assert model.srv_secret == ""
    assert model.action_error == ""


def test_choose_vault(model, client):
    """Test choose_vault() calls ChooseObsidianVault RPC."""
    model.choose_vault("/home/user/vault")
    assert ("ChooseObsidianVault", ("/home/user/vault",)) in client.calls


def test_choose_vault_error_clears_busy_and_surfaces_error(model, client):
    client.failures["ChooseObsidianVault"] = {"message": "vault denied"}

    model.choose_vault("/home/user/vault")

    assert model.obsidian_busy is False
    assert model.action_error == "Could not choose Obsidian vault: vault denied"


def test_toggle_revuto(model, client):
    """Test toggle_revuto() toggles revuto_open."""
    assert model.revuto_open is False
    model.toggle_revuto()
    assert model.revuto_open is True
    model.toggle_revuto()
    assert model.revuto_open is False


def test_revuto_pause(model, client):
    """Test revuto_pause() calls RevutoSetPaused RPC."""
    model.revuto_pause("owner/repo", True)
    assert ("RevutoSetPaused", ("owner/repo", True)) in client.calls


def test_revuto_errors_clear_busy_and_surface_error(model, client):
    client.failures["RevutoSetPaused"] = "pause denied"
    model.revuto_pause("owner/repo", True)

    assert model.revuto_busy == {}
    assert model.action_error == "Could not update owner/repo: pause denied"

    del client.failures["RevutoSetPaused"]
    client.failures["RevutoTrigger"] = {"message": "trigger denied"}
    model.revuto_trigger("owner/repo")

    assert model.revuto_busy == {}
    assert model.action_error == "Could not run review for owner/repo: trigger denied"


def test_revuto_trigger(model, client):
    """Test revuto_trigger() calls RevutoTrigger RPC."""
    model.revuto_trigger("owner/repo")
    assert ("RevutoTrigger", ("owner/repo", "review")) in client.calls


def test_setup_google(model, client):
    """Test setup_google() calls ImportGoogleClient RPC."""
    model.setup_google()
    assert ("ImportGoogleClient", ()) in client.calls


def test_connect_google(model, client):
    """Test connect_google() calls ConnectGoogle RPC."""
    model.connect_google()
    assert ("ConnectGoogle", ()) in client.calls


def test_disconnect_google(model, client):
    """Test disconnect_google() calls DisconnectGoogle RPC."""
    model.disconnect_google()
    assert ("DisconnectGoogle", ()) in client.calls


@pytest.mark.parametrize(
    "method,rpc_method,expected_error",
    [
        ("setup_google", "ImportGoogleClient", "Could not import Google client: import denied"),
        ("connect_google", "ConnectGoogle", "Could not connect Google: connect denied"),
        ("disconnect_google", "DisconnectGoogle", "Could not disconnect Google: disconnect denied"),
    ],
)
def test_google_action_errors_clear_busy(model, client, method, rpc_method, expected_error):
    client.failures[rpc_method] = expected_error.rsplit(": ", 1)[1]

    getattr(model, method)()

    assert model.google_busy is False
    assert model.action_error == expected_error


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
