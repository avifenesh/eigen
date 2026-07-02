#!/usr/bin/env python3
"""
test_connectors_model.py — pytest for ConnectorsModel logic.
"""
import pytest
from unittest.mock import Mock, MagicMock
from PySide6.QtCore import QObject

from eigenqt.models.connectors import ConnectorsModel


class MockRpcClient(QObject):
    """Mock RPC client."""

    connected = MagicMock()
    event = MagicMock()

    def __init__(self):
        super().__init__()
        self.calls = []
        self.parallel_calls = []

    def call(self, method, *args, callback=None, error_callback=None):
        self.calls.append((method, args))
        if callback:
            callback(self._mock_result(method, *args))

    def call_parallel(self, calls, callback=None, error_callback=None):
        self.parallel_calls.append(calls)
        results = []
        for method, args in calls:
            results.append(self._mock_result(method, *args))
        if callback:
            callback(results)

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
    """Test load() calls parallel RPC and sets properties."""
    model.load()
    assert len(client.parallel_calls) == 1
    assert client.parallel_calls[0] == [
        ("Connectors", []),
        ("MCPServers", []),
        ("GoogleStatus", []),
    ]
    assert model.connectors is not None
    assert model.servers is not None
    assert model.google_status is not None
    assert model.loading is False


def test_add_connector(model, client):
    """Test add_connector() calls AddConnector RPC."""
    model.add_name = "notion"
    model.add_url = "https://mcp.notion.com/mcp"
    model.add_desc = "Notion workspace"
    model.add_connector("notion")
    assert ("AddConnector", ("notion", "https://mcp.notion.com/mcp", "Notion workspace")) in client.calls


def test_connect_connector(model, client):
    """Test connect_connector() calls ConnectConnector RPC."""
    model.connect_connector("slack")
    assert ("ConnectConnector", ("slack",)) in client.calls


def test_disconnect_connector(model, client):
    """Test disconnect_connector() calls DisconnectConnector RPC."""
    model.disconnect_connector("notion")
    assert ("DisconnectConnector", ("notion",)) in client.calls


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


def test_choose_vault(model, client):
    """Test choose_vault() calls ChooseObsidianVault RPC."""
    model.choose_vault("/home/user/vault")
    assert ("ChooseObsidianVault", ("/home/user/vault",)) in client.calls


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


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
