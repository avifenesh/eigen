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
