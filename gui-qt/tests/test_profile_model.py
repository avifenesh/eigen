"""Tests for the Qt profile usage/profile model."""

from unittest.mock import Mock

from PySide6.QtCore import QCoreApplication

from eigenqt.models.profile import ProfileModel


def ensure_app():
    return QCoreApplication.instance() or QCoreApplication([])


def fake_client():
    client = Mock()
    client.connected = Mock()
    client.connected.connect = Mock()
    client.call = Mock()
    return client


def summary_payload():
    return {
        "available": True,
        "records": 4,
        "models": [
            {
                "name": "gpt-5",
                "turns": 3,
                "inTokens": 12000,
                "outTokens": 2100,
                "cacheReadTokens": 6000,
                "cacheWriteTokens": 200,
            },
            {
                "name": "local-qwen",
                "turns": 1,
                "inTokens": 1000,
                "outTokens": 400,
                "cacheReadTokens": 0,
                "cacheWriteTokens": 0,
            },
        ],
        "errors": [{"name": "rpc timeout", "count": 2}],
    }


def memory_payload():
    return {
        "scope": "global",
        "profile": "Focused profile",
        "profileLearned": "Prefers direct Qt proof.",
    }


def callback_from(call):
    return call.kwargs["callback"]


def test_profile_model_stores_usage_and_profile_summary():
    model = ProfileModel(fake_client())

    model._on_summary_result({"result": summary_payload()})
    model._on_memory_result({"result": memory_payload()})

    assert model.summary_available is True
    assert model.records == 4
    assert model.in_tokens == 13000
    assert model.out_tokens == 2500
    assert model.cache_read_tokens == 6000
    assert model.cache_write_tokens == 200
    assert model.cache_hit == 31
    assert model.error_count == 2
    assert model.models[0]["name"] == "gpt-5"
    assert model.profile == "Focused profile"
    assert model.profile_learned == "Prefers direct Qt proof."


def test_profile_model_activation_fetches_usage_and_global_profile():
    ensure_app()
    client = fake_client()
    model = ProfileModel(client)

    model.set_active(True)

    assert client.call.call_count == 2
    assert client.call.call_args_list[0].args[:2] == ("ObserveSummary", 5000)
    assert client.call.call_args_list[1].args[:2] == ("MemoryForScope", "global")
    assert model.summary_loading is True
    assert model.memory_loading is True


def test_profile_model_ignores_stale_usage_and_memory():
    model = ProfileModel(fake_client())
    model._summary_seq = 2
    model._memory_seq = 2

    model._on_summary_result({"result": summary_payload()}, seq=1)
    model._on_memory_result({"result": memory_payload()}, seq=1)

    assert model.records == 0
    assert model.profile == ""


def test_profile_model_stop_ignores_late_results():
    model = ProfileModel(fake_client())
    model._summary_seq = 1
    model._memory_seq = 1

    model.stop_polling()
    model._on_summary_result({"result": summary_payload()}, seq=1)
    model._on_memory_result({"result": memory_payload()}, seq=1)

    assert model.records == 0
    assert model.profile == ""


def test_profile_model_save_updates_profile_and_reloads_global_scope():
    client = fake_client()
    model = ProfileModel(client)
    model._on_memory_result({"result": memory_payload()})
    model.start_edit()
    model.profile_draft = "Updated profile"

    model.save_profile()
    save_callback = callback_from(client.call.call_args)
    save_callback({"result": None})

    assert client.call.call_args_list[-2].args[:2] == ("WriteUserProfile", "Updated profile")
    assert client.call.call_args_list[-1].args[:2] == ("MemoryForScope", "global")
    assert model.saving_profile is False
    assert model.editing_profile is False
    assert model.profile == "Updated profile"
    assert model.action_error == ""


def test_profile_model_save_error_keeps_draft_retryable():
    client = fake_client()
    model = ProfileModel(client)
    model._on_memory_result({"result": memory_payload()})
    model.start_edit()
    model.profile_draft = "Retry me"

    model.save_profile()
    save_callback = callback_from(client.call.call_args)
    save_callback({"error": {"message": "write denied"}})

    assert model.saving_profile is False
    assert model.editing_profile is True
    assert model.profile_draft == "Retry me"
    assert model.action_error == "Could not save profile: write denied"

    model.clear_action_error()
    assert model.action_error == ""


def test_profile_model_guards_duplicate_profile_save():
    client = fake_client()
    model = ProfileModel(client)
    model.profile_draft = "Pending profile"

    model.save_profile()
    model.save_profile()

    assert [call.args[0] for call in client.call.call_args_list].count("WriteUserProfile") == 1
