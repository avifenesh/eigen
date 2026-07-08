"""Tests for the Qt dreaming timeline model."""

from unittest.mock import Mock

from PySide6.QtCore import QCoreApplication

from eigenqt.models.dreaming import DreamingModel


def ensure_app():
    return QCoreApplication.instance() or QCoreApplication([])


def fake_client():
    client = Mock()
    client.connected = Mock()
    client.connected.connect = Mock()
    client.call = Mock()
    return client


def scopes_payload():
    return [
        {"key": "global", "name": "Global", "dir": "", "noteCount": 3},
        {
            "key": "project:/repo/eigen",
            "name": "eigen",
            "dir": "/repo/eigen",
            "noteCount": 5,
            "current": True,
        },
    ]


def dreaming_payload(scope="project:/repo/eigen"):
    return {
        "scope": scope,
        "currentBytes": 4096,
        "rollouts": [
            {
                "index": 1,
                "text": "# Outcome: success\n\nCaptured focused Qt proof.",
                "outcome": "success",
                "whenMs": 1783155600000,
            },
            {
                "index": 0,
                "text": "# Outcome: partial\n\nNeeds visual pass.",
                "outcome": "partial",
                "whenMs": 1783144800000,
            },
        ],
        "consolidations": [
            {
                "path": "/repo/eigen/.eigen/memory/MEMORY.md.20260707-120000.bak",
                "label": "Jul 7, 12:00",
                "whenMs": 1783152000000,
                "bytes": 2048,
            }
        ],
    }


def test_dreaming_model_stores_timeline_summary():
    model = DreamingModel(fake_client())

    model._on_current_result({"result": dreaming_payload()})

    assert model.rollout_count == 2
    assert model.consolidation_count == 1
    assert model.current_bytes == 4096
    assert model.rollouts[0]["outcome"] == "success"
    assert model.consolidations[0]["label"] == "Jul 7, 12:00"


def test_dreaming_model_activation_fetches_scopes_and_project_timeline():
    ensure_app()
    client = fake_client()
    model = DreamingModel(client)

    model.set_active(True)

    assert client.call.call_count == 2
    assert client.call.call_args_list[0].args[:1] == ("ListMemoryScopes",)
    assert client.call.call_args_list[1].args[:2] == ("DreamingForScope", "project")
    assert model.loading is True


def test_dreaming_model_scope_result_reopens_current_project_key():
    client = fake_client()
    model = DreamingModel(client)

    model._on_scopes_result({"result": scopes_payload()})

    assert model.scope_key == "project:/repo/eigen"
    assert client.call.call_args.args[:2] == ("DreamingForScope", "project:/repo/eigen")


def test_dreaming_model_select_scope_fetches_that_scope():
    client = fake_client()
    model = DreamingModel(client)
    model._on_scopes_result({"result": scopes_payload()})
    client.call.reset_mock()

    model.select_scope("global")

    assert model.scope_key == "global"
    assert client.call.call_args.args[:2] == ("DreamingForScope", "global")


def test_dreaming_model_ignores_stale_results():
    model = DreamingModel(fake_client())
    model._load_seq = 2

    model._on_current_result({"result": dreaming_payload()}, seq=1)

    assert model.rollout_count == 0
    assert model.consolidation_count == 0


def test_dreaming_model_stop_ignores_late_results():
    model = DreamingModel(fake_client())
    model._scope_seq = 1
    model._load_seq = 1

    model.stop_polling()
    model._on_scopes_result({"result": scopes_payload()}, seq=1)
    model._on_current_result({"result": dreaming_payload()}, seq=1)

    assert model.scopes == []
    assert model.rollout_count == 0


def test_dreaming_model_surfaces_load_error():
    model = DreamingModel(fake_client())

    model._on_current_result({"error": {"message": "dreaming unavailable"}})

    assert model.load_error == "dreaming unavailable"
    assert model.loading is False
