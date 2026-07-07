"""Tests for the Qt routing catalog model."""

from unittest.mock import Mock

from eigenqt.models.routing import RoutingModel


def fake_client():
    client = Mock()
    client.connected = Mock()
    client.connected.connect = Mock()
    client.call = Mock()
    return client


def routing_payload():
    return {
        "models": [
            {
                "id": "gpt-5",
                "provider": "codex",
                "contextWindow": 400000,
                "cache": True,
                "context1m": False,
                "reasoning": True,
                "effortLevels": ["low", "medium", "high"],
                "available": True,
            },
            {
                "id": "grok-4",
                "provider": "grok",
                "contextWindow": 256000,
                "cache": False,
                "context1m": False,
                "reasoning": True,
                "effortLevels": ["low", "high"],
                "available": False,
            },
        ],
        "providers": [
            {"name": "codex", "credentialed": True, "modelCount": 1},
            {"name": "grok", "credentialed": False, "modelCount": 1},
            {"name": "unused", "credentialed": False, "modelCount": 0},
        ],
    }


def test_routing_model_stores_catalog_summary():
    model = RoutingModel(fake_client())

    model._on_routing_result({"result": routing_payload()})

    assert model.models[0]["id"] == "gpt-5"
    assert model.model_count == 2
    assert model.available_count == 1
    assert model.provider_count == 2
    assert model.load_error == ""


def test_routing_model_stores_observed_route_stats():
    model = RoutingModel(fake_client())

    model._on_observe_result(
        {
            "result": {
                "available": True,
                "records": 5,
                "routes": {
                    "routed": 2,
                    "assessed": 1,
                    "skipped": 1,
                    "orchestrator": 1,
                    "byModel": [{"name": "gpt-5", "count": 2}],
                },
            }
        }
    )

    assert model.route_total == 5
    assert model.routes["byModel"][0]["name"] == "gpt-5"


def test_routing_model_ignores_stale_load_result():
    model = RoutingModel(fake_client())
    model._load_seq = 2

    model._on_routing_result({"result": routing_payload()}, seq=1)

    assert model.model_count == 0


def test_routing_model_surfaces_catalog_load_error():
    model = RoutingModel(fake_client())

    model._on_routing_result({"error": {"message": "daemon offline"}})

    assert model.load_error == "daemon offline"
    assert model.loading is False
