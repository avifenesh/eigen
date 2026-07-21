"""Tests for the Qt scheduled-work model."""

from unittest.mock import Mock

from PySide6.QtCore import QCoreApplication

from eigenqt.models.crons import CronsModel


def ensure_app():
    return QCoreApplication.instance() or QCoreApplication([])


def fake_client():
    client = Mock()
    client.connected = Mock()
    client.connected.connect = Mock()
    client.call = Mock()
    return client


def crons_payload():
    return {
        "crons": [
            {
                "name": "eigen-dream",
                "kind": "timer",
                "next": "today 19:30",
                "last": "today 17:00",
                "active": True,
                "enabled": True,
                "command": "eigen-dream.service",
                "unit": "eigen-dream.timer",
            },
            {
                "name": "eigen-clean",
                "kind": "timer",
                "next": "2026-07-08 09:00",
                "last": "",
                "active": False,
                "enabled": True,
                "command": "eigen-clean.service",
                "unit": "eigen-clean.timer",
            },
            {
                "name": "eigen run daily",
                "kind": "crontab",
                "next": "0 9 * * *",
                "last": "",
                "active": True,
                "enabled": True,
                "command": "eigen run daily",
            },
        ],
        "timers": 2,
        "crontab": 1,
        "systemdAvail": True,
    }


def test_crons_model_stores_schedule_summary():
    model = CronsModel(fake_client())

    model._on_crons_result({"result": crons_payload()})

    assert model.timers_count == 2
    assert model.crontab_count == 1
    assert model.systemd_available is True
    assert model.active_timer_count == 1
    assert model.enabled_timer_count == 2
    assert model.crons[0]["unit"] == "eigen-dream.timer"


def test_crons_model_activation_fetches_snapshot():
    ensure_app()
    client = fake_client()
    model = CronsModel(client)

    model.set_active(True)

    assert client.call.call_count == 1
    assert client.call.call_args.args[:1] == ("Crons",)
    assert model.loading is True


def test_crons_model_ignores_stale_snapshot():
    model = CronsModel(fake_client())
    model._load_seq = 2

    model._on_crons_result({"result": crons_payload()}, seq=1)

    assert model.timers_count == 0
    assert model.crontab_count == 0


def test_crons_model_stop_ignores_late_snapshot():
    model = CronsModel(fake_client())
    model._load_seq = 1

    model.stop_polling()
    model._on_crons_result({"result": crons_payload()}, seq=1)

    assert model.timers_count == 0
    assert model.crontab_count == 0


def test_crons_model_surfaces_load_error():
    model = CronsModel(fake_client())

    model._on_crons_result({"error": {"message": "systemctl unavailable"}})

    assert model.load_error == "systemctl unavailable"
    assert model.loading is False


def test_crons_model_runs_guarded_timer_action_and_refreshes():
    client = fake_client()
    model = CronsModel(client)

    model.set_timer("eigen-dream.timer", "stop")

    action_call = client.call.call_args
    assert action_call.args == ("SetTimer", "eigen-dream.timer", "stop")
    assert model.pending_actions == ["timer:eigen-dream.timer"]

    model.set_timer("eigen-dream.timer", "disable")
    assert client.call.call_count == 1

    action_call.kwargs["callback"]({"result": None})

    assert model.pending_actions == []
    assert model.action_message == "Stopped eigen-dream.timer"
    assert client.call.call_count == 2
    assert client.call.call_args.args == ("Crons",)


def test_crons_model_timer_error_clears_pending_without_refresh():
    client = fake_client()
    model = CronsModel(client)

    model.set_timer("eigen-dream.timer", "disable")
    callback = client.call.call_args.kwargs["callback"]
    callback({"error": {"message": "systemctl denied"}})

    assert model.pending_actions == []
    assert model.action_error == "systemctl denied"
    assert model.action_message == ""
    assert client.call.call_count == 1


def test_crons_model_rejects_invalid_timer_action_locally():
    client = fake_client()
    model = CronsModel(client)

    model.set_timer("eigen-dream.service", "restart")

    client.call.assert_not_called()
    assert model.action_error == "Choose a valid timer action"


def test_crons_model_adds_job_and_emits_completion():
    ensure_app()
    client = fake_client()
    model = CronsModel(client)
    added = []
    model.jobAdded.connect(lambda: added.append(True))

    model.add_crontab(" 0 9 * * 1-5 ", " eigen run standup ")

    action_call = client.call.call_args
    assert action_call.args == ("AddCrontab", "0 9 * * 1-5", "eigen run standup")
    assert model.adding_job is True

    action_call.kwargs["callback"]({"result": None})

    assert model.adding_job is False
    assert model.action_message == "Scheduled job added"
    assert added == [True]
    assert client.call.call_args.args == ("Crons",)


def test_crons_model_add_validation_and_backend_error_preserve_feedback():
    client = fake_client()
    model = CronsModel(client)

    model.add_crontab("", "eigen run")
    client.call.assert_not_called()
    assert model.action_error == "Schedule and command are required"

    model.add_crontab("@daily", "eigen run")
    callback = client.call.call_args.kwargs["callback"]
    callback({"error": "duplicate crontab entry"})

    assert model.adding_job is False
    assert model.action_error == "duplicate crontab entry"
    assert client.call.call_count == 1


def test_crons_model_removes_exact_job_and_handles_invalid_response():
    client = fake_client()
    model = CronsModel(client)

    model.remove_crontab("@hourly", "eigen run compact")

    action_call = client.call.call_args
    key = "crontab:@hourly\neigen run compact"
    assert action_call.args == ("RemoveCrontab", "@hourly", "eigen run compact")
    assert model.pending_actions == [key]

    action_call.kwargs["callback"](None)

    assert model.pending_actions == []
    assert model.action_error == "Invalid daemon response"
    assert client.call.call_count == 1


def test_crons_model_rejects_invalid_snapshot_response():
    model = CronsModel(fake_client())
    model._set_loading(True)

    model._on_crons_result(None)

    assert model.loading is False
    assert model.load_error == "Invalid daemon response"
