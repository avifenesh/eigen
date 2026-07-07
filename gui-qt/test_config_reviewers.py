#!/usr/bin/env python3
"""
test_config_reviewers.py — Pytest for ConfigModel, RuleChainsModel, and ReviewersModel.

Tests model instantiation and basic role/data structure.
"""

import pytest
from unittest.mock import Mock
from PySide6.QtCore import Qt, QModelIndex
from PySide6.QtQml import QQmlApplicationEngine
from PySide6.QtGui import QGuiApplication

from eigenqt.rpc.client import RpcClient
from eigenqt.models.config import ConfigModel, RuleChainsModel
from eigenqt.models.reviewers import ReviewersModel


class DeferredRpcClient:
    """RPC client that lets tests resolve callbacks in arbitrary order."""

    def __init__(self):
        self.connected = Mock()
        self.connected.connect = Mock()
        self.calls = []

    def call(self, method, *args, callback=None, error_callback=None):
        self.calls.append(
            {
                "method": method,
                "args": args,
                "callback": callback,
                "error_callback": error_callback,
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


def _config_payload(path="/home/user/.eigen/config.json", model_value="gpt-5"):
    return {
        "path": path,
        "fields": [
            {
                "key": "model",
                "desc": "Default model",
                "value": model_value,
                "options": ["gpt-5", "local-qwen"],
                "multi": False,
                "allowEmpty": False,
            }
        ],
    }


def _rule_chains_payload(chain):
    return {
        "models": ["gpt-5", "local-qwen", "grok-4"],
        "roles": [
            {
                "role": "primary",
                "desc": "Primary chain",
                "chain": list(chain),
                "custom": len(chain) > 0,
            }
        ],
    }


def _first_config_value(model):
    assert model.rowCount() == 1
    return model.data(model.index(0, 0), ConfigModel.ValueRole)


def _first_chain(model):
    assert model.rowCount() == 1
    return model.data(model.index(0, 0), RuleChainsModel.ChainRole)


def _first_reviewer_repo(model):
    assert model.rowCount() == 1
    return model.data(model.index(0, 0), ReviewersModel.RepoRole)


@pytest.fixture
def app():
    """Create QGuiApplication."""
    import sys
    app = QGuiApplication.instance() or QGuiApplication(sys.argv)
    yield app


@pytest.fixture
def client(app, monkeypatch):
    """Create RpcClient (won't connect, but models can instantiate)."""
    monkeypatch.setattr(RpcClient, "_start_workers", lambda self: None)
    client = RpcClient()
    yield client
    client.shutdown()


def test_config_model_instantiation(client):
    """ConfigModel instantiates and exposes expected roles."""
    model = ConfigModel(client)
    assert model is not None
    roles = model.roleNames()
    assert b"key" in roles.values()
    assert b"desc" in roles.values()
    assert b"value" in roles.values()
    assert b"options" in roles.values()
    assert b"multi" in roles.values()
    assert b"allowEmpty" in roles.values()


def test_config_model_ignores_stale_refresh_callbacks(app):
    """Older Config replies must not overwrite the latest refresh."""
    client = DeferredRpcClient()
    model = ConfigModel(client)

    model.refresh()
    first = list(client.calls)
    model.refresh()
    second = client.calls[len(first):]

    _reply(
        _call_by_method(second, "Config"),
        result=_config_payload("/fresh/config.json", "local-qwen"),
    )
    assert model.config_path == "/fresh/config.json"
    assert _first_config_value(model) == "local-qwen"

    _reply(
        _call_by_method(first, "Config"),
        result=_config_payload("/stale/config.json", "gpt-5"),
    )
    assert model.config_path == "/fresh/config.json"
    assert _first_config_value(model) == "local-qwen"


def test_config_set_invalidates_in_flight_refresh(app):
    """A late poll from before a user edit must not repaint the old field value."""
    client = DeferredRpcClient()
    model = ConfigModel(client)
    model._on_config_result({"result": _config_payload("/current/config.json", "gpt-5")})

    model.refresh()
    stale_poll = _call_by_method(client.calls, "Config")
    model.set_config("model", "local-qwen")
    set_call = _call_by_method(client.calls, "SetConfig")

    _reply(set_call, result="local-qwen")
    assert _first_config_value(model) == "local-qwen"

    _reply(stale_poll, result=_config_payload("/stale/config.json", "gpt-5"))
    assert model.config_path == "/current/config.json"
    assert _first_config_value(model) == "local-qwen"


def test_rule_chains_model_instantiation(client):
    """RuleChainsModel instantiates and exposes expected roles."""
    model = RuleChainsModel(client)
    assert model is not None
    roles = model.roleNames()
    assert b"roleName" in roles.values()
    assert b"desc" in roles.values()
    assert b"chain" in roles.values()
    assert b"custom" in roles.values()
    assert b"models" in roles.values()


def test_rule_chains_model_ignores_stale_refresh_callbacks(app):
    """Older RuleChains replies must not overwrite the latest refresh."""
    client = DeferredRpcClient()
    model = RuleChainsModel(client)

    model.refresh()
    first = list(client.calls)
    model.refresh()
    second = client.calls[len(first):]

    _reply(_call_by_method(second, "RuleChains"), result=_rule_chains_payload(["local-qwen", "gpt-5"]))
    assert _first_chain(model) == ["local-qwen", "gpt-5"]

    _reply(_call_by_method(first, "RuleChains"), result=_rule_chains_payload(["gpt-5"]))
    assert _first_chain(model) == ["local-qwen", "gpt-5"]


def test_rule_chain_set_invalidates_in_flight_refresh(app):
    """A late poll from before a chain edit must not repaint the old chain."""
    client = DeferredRpcClient()
    model = RuleChainsModel(client)
    model._on_rule_chains_result({"result": _rule_chains_payload(["gpt-5", "local-qwen"])})

    model.refresh()
    stale_poll = _call_by_method(client.calls, "RuleChains")
    model.set_rule_chain("primary", ["local-qwen", "gpt-5"])
    set_call = _call_by_method(client.calls, "SetRuleChain")

    _reply(set_call, result=["local-qwen", "gpt-5"])
    assert _first_chain(model) == ["local-qwen", "gpt-5"]

    _reply(stale_poll, result=_rule_chains_payload(["gpt-5", "local-qwen"]))
    assert _first_chain(model) == ["local-qwen", "gpt-5"]


def test_reviewers_model_instantiation(client):
    """ReviewersModel instantiates and exposes expected roles."""
    model = ReviewersModel(client)
    assert model is not None
    roles = model.roleNames()
    assert b"repo" in roles.values()
    assert b"paused" in roles.values()
    # Check properties
    assert model.available == False  # No connection yet
    assert model.count == 0
    assert model.paused_count == 0
    assert model.loading == False
    assert model.load_error == ""


def test_reviewers_model_loading_lifecycle(app):
    """ReviewersModel keeps loading true until status + reviewer rows resolve."""
    client = Mock()
    client.connected = Mock()
    client.connected.connect = Mock()
    calls = []

    def call(method, *args, callback=None):
        calls.append((method, args, callback))

    client.call.side_effect = call

    model = ReviewersModel(client)

    model.refresh()
    assert model.loading is True
    assert calls[-1][0] == "RevutoStatus"

    calls[-1][2]({"result": {"available": True, "count": 2, "paused": 1}})

    assert model.loading is True
    assert calls[-1][0] == "RevutoReviewers"

    calls[-1][2](
        {
            "result": [
                {"repo": "avifenesh/eigen", "paused": False},
                {"repo": "avifenesh/revuto", "paused": True},
            ]
        }
    )

    assert model.loading is False
    assert model.count == 2
    assert model.paused_count == 1


def test_reviewers_model_surfaces_status_load_error(app):
    """Failed RevutoStatus loads expose a retryable load_error."""
    client = DeferredRpcClient()
    model = ReviewersModel(client)

    model.refresh()
    _reply(_call_by_method(client.calls, "RevutoStatus"), error={"message": "daemon offline"})

    assert model.loading is False
    assert model.available is False
    assert model.count == 0
    assert model.load_error == "daemon offline"

    model.refresh()
    assert model.load_error == ""


def test_reviewers_model_surfaces_reviewers_load_error(app):
    """A failed RevutoReviewers call is visible even after status reports rows."""
    client = DeferredRpcClient()
    model = ReviewersModel(client)

    model.refresh()
    _reply(_call_by_method(client.calls, "RevutoStatus"), result={"available": True, "count": 1, "paused": 0})
    _reply(_call_by_method(client.calls, "RevutoReviewers"), error="reviewer socket offline")

    assert model.loading is False
    assert model.available is True
    assert model.count == 1
    assert model.rowCount() == 0
    assert model.load_error == "reviewer socket offline"


def test_reviewers_model_ignores_stale_status_callbacks(app):
    """Older RevutoStatus replies must not repaint the reviewer list."""
    client = DeferredRpcClient()
    model = ReviewersModel(client)

    model.refresh()
    first = list(client.calls)
    model.refresh()
    second = client.calls[len(first):]

    _reply(_call_by_method(second, "RevutoStatus"), result={"available": True, "count": 1, "paused": 0})
    _reply(
        _call_by_method(client.calls, "RevutoReviewers"),
        result=[{"repo": "avifenesh/eigen", "paused": False}],
    )
    assert model.available is True
    assert model.count == 1
    assert _first_reviewer_repo(model) == "avifenesh/eigen"

    reviewer_call_count = len([call for call in client.calls if call["method"] == "RevutoReviewers"])
    _reply(_call_by_method(first, "RevutoStatus"), result={"available": False, "count": 0, "paused": 0})

    assert len([call for call in client.calls if call["method"] == "RevutoReviewers"]) == reviewer_call_count
    assert model.available is True
    assert model.count == 1
    assert _first_reviewer_repo(model) == "avifenesh/eigen"


def test_reviewers_model_ignores_stale_reviewers_callbacks(app):
    """Older RevutoReviewers replies must not overwrite a newer refresh."""
    client = DeferredRpcClient()
    model = ReviewersModel(client)

    model.refresh()
    first_status = _call_by_method(client.calls, "RevutoStatus")
    _reply(first_status, result={"available": True, "count": 1, "paused": 0})
    first_reviewers = _call_by_method(client.calls, "RevutoReviewers")

    model.refresh()
    second_status = _call_by_method(client.calls, "RevutoStatus")
    _reply(second_status, result={"available": True, "count": 1, "paused": 0})
    second_reviewers = _call_by_method(client.calls, "RevutoReviewers")

    _reply(second_reviewers, result=[{"repo": "avifenesh/eigen", "paused": False}])
    assert model.count == 1
    assert _first_reviewer_repo(model) == "avifenesh/eigen"

    _reply(first_reviewers, result=[{"repo": "avifenesh/old-reviewer", "paused": True}])
    assert model.count == 1
    assert model.paused_count == 0
    assert _first_reviewer_repo(model) == "avifenesh/eigen"


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
