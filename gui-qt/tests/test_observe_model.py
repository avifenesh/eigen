"""Tests for the Qt observe summary model."""

from unittest.mock import Mock

from PySide6.QtCore import QCoreApplication

from eigenqt.models.observe import ObserveModel


def ensure_app():
    return QCoreApplication.instance() or QCoreApplication([])


def fake_client():
    client = Mock()
    client.connected = Mock()
    client.connected.connect = Mock()
    client.call = Mock()
    return client


def observe_payload():
    return {
        "available": True,
        "records": 8,
        "routes": {
            "routed": 3,
            "assessed": 2,
            "skipped": 1,
            "orchestrator": 1,
            "byModel": [{"name": "gpt-5", "count": 3}],
            "byKind": [],
            "byDifficulty": [],
            "skipReasons": [],
        },
        "tools": [
            {"name": "read_file", "calls": 6, "errors": 0, "durationMs": 120},
            {"name": "run_shell", "calls": 2, "errors": 1, "durationMs": 400},
        ],
        "models": [
            {
                "name": "gpt-5",
                "turns": 4,
                "inTokens": 12000,
                "outTokens": 2200,
                "cacheReadTokens": 8000,
                "cacheWriteTokens": 500,
                "durationMs": 2400,
            }
        ],
        "hooks": [],
        "errors": [{"name": "rpc timeout", "count": 2}],
        "byKind": [],
        "subagents": {
            "taskCalls": 3,
            "taskErrors": 1,
            "groupCalls": 1,
            "groupErrors": 0,
            "mutatingCalls": 2,
            "mutatingErrors": 1,
            "statusChecks": 5,
            "promotes": 1,
            "promoteErrors": 0,
            "backgroundDone": 1,
            "backgroundNotes": 2,
            "routeNotes": 1,
        },
    }


def test_observe_model_stores_summary_counters():
    model = ObserveModel(fake_client())

    model._on_summary_result({"result": observe_payload()})

    assert model.available is True
    assert model.records == 8
    assert model.route_total == 7
    assert model.tool_calls == 8
    assert model.tool_errors == 1
    assert model.model_turns == 4
    assert model.error_count == 2
    assert model.subagent_errors == 2


def test_observe_model_unavailable_log_is_not_error():
    model = ObserveModel(fake_client())

    model._on_summary_result({"result": {"available": False}})

    assert model.available is False
    assert model.records == 0
    assert model.load_error == ""


def test_observe_model_activation_fetches_summary():
    ensure_app()
    client = fake_client()
    model = ObserveModel(client)

    model.set_active(True)

    assert client.call.call_count == 1
    assert client.call.call_args.args[:2] == ("ObserveSummary", 5000)
    assert model.loading is True


def test_observe_model_ignores_stale_result():
    model = ObserveModel(fake_client())
    model._load_seq = 2

    model._on_summary_result({"result": observe_payload()}, seq=1)

    assert model.records == 0


def test_observe_model_surfaces_load_error():
    model = ObserveModel(fake_client())

    model._on_summary_result({"error": {"message": "daemon offline"}})

    assert model.load_error == "daemon offline"
    assert model.loading is False
