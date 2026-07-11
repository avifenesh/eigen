"""Tests for the Qt remote machines model."""

from unittest.mock import Mock

from PySide6.QtCore import QCoreApplication

from eigenqt.models.machines import MachinesModel


def ensure_app():
    return QCoreApplication.instance() or QCoreApplication([])


def fake_client():
    client = Mock()
    client.connected = Mock()
    client.connected.connect = Mock()
    client.call = Mock()
    return client


def machines_payload():
    return {
        "machines": [
            {
                "name": "codex-box",
                "ssh": "codex-box",
                "addr": "10.0.0.5",
                "dir": "/home/user/eigen",
                "model": "gpt-5",
                "perm": "gated",
                "saved": True,
                "detected": False,
            },
            {
                "name": "lab-node",
                "ssh": "lab-node",
                "dir": "/srv/eigen",
                "model": "local-qwen",
                "perm": "manual",
                "saved": False,
                "detected": True,
            },
        ]
    }


def remote_sessions_payload():
    return [
        {
            "id": "remote:codex-box:s1",
            "title": "Remote Qt polish",
            "dir": "/home/user/eigen/gui-qt",
            "model": "gpt-5",
            "status": "working",
            "turns": 4,
            "views": 1,
            "updated": 1783155600000,
        },
        {
            "id": "remote:codex-box:s2",
            "title": "Remote notes",
            "dir": "/home/user/eigen",
            "model": "local-qwen",
            "status": "idle",
            "turns": 1,
            "views": 1,
            "updated": 1783144800000,
        },
    ]


def test_machines_model_stores_host_summary():
    model = MachinesModel(fake_client())

    model._on_machines_result({"result": machines_payload()})

    assert model.machine_count == 2
    assert model.saved_count == 1
    assert model.detected_count == 1
    assert model.machines[0]["ssh"] == "codex-box"
    assert model.load_error == ""


def test_machines_model_activation_fetches_local_hosts():
    ensure_app()
    client = fake_client()
    model = MachinesModel(client)

    model.set_active(True)

    assert client.call.call_count == 1
    assert client.call.call_args.args[:1] == ("Machines",)
    assert model.loading is True


def test_machines_model_selects_machine_and_loads_remote_sessions():
    client = fake_client()
    model = MachinesModel(client)
    model._on_machines_result({"result": machines_payload()})

    model.select_machine("codex-box")

    assert client.call.call_args.args[:2] == ("RemoteSessions", "codex-box")
    assert model.selected_machine["ssh"] == "codex-box"
    assert model.remote_loading is True

    model._on_remote_result({"result": remote_sessions_payload()}, seq=model._remote_seq)

    assert model.remote_loading is False
    assert model.remote_count == 2
    assert model.remote_sessions[0]["id"] == "remote:codex-box:s1"
    assert model.remote_error == ""


def test_machines_model_clear_selection_ignores_late_remote_result():
    model = MachinesModel(fake_client())
    model._on_machines_result({"result": machines_payload()})
    model.select_machine("codex-box")
    seq = model._remote_seq

    model.clear_selection()
    model._on_remote_result({"result": remote_sessions_payload()}, seq=seq)

    assert model.selected_machine == {}
    assert model.remote_count == 0
    assert model.remote_loading is False


def test_machines_model_installs_host_and_refreshes_selected_sessions():
    client = fake_client()
    model = MachinesModel(client)
    model._on_machines_result({"result": machines_payload()})
    model.select_machine("codex-box")
    client.call.reset_mock()

    model.install_machine(" codex-box ", False)

    assert client.call.call_args.args[:3] == ("InstallRemote", "codex-box", False)
    assert model.installing is True
    assert model.install_message == ""
    assert model.install_error == ""

    callback = client.call.call_args.kwargs["callback"]
    callback({"result": "Eigen installed on codex-box"})

    assert model.installing is False
    assert model.install_error == ""
    assert model.install_message == "Eigen installed on codex-box"
    assert client.call.call_args.args[:2] == ("RemoteSessions", "codex-box")


def test_machines_model_rejects_empty_or_concurrent_install_and_surfaces_errors():
    client = fake_client()
    model = MachinesModel(client)

    model.install_machine("", True)
    assert client.call.call_count == 0

    model.install_machine("lab-node", True)
    callback = client.call.call_args.kwargs["callback"]
    model.install_machine("codex-box", False)

    assert client.call.call_count == 1
    assert model.installing is True

    callback({"error": {"message": "ssh denied"}})

    assert model.installing is False
    assert model.install_message == ""
    assert model.install_error == "ssh denied"


def test_machines_model_saves_host_then_installs_when_requested():
    client = fake_client()
    model = MachinesModel(client)
    saved = []
    model.machineSaved.connect(lambda: saved.append(True))

    model.save_machine("gpu-box", "avi@gpu-box", "/srv/eigen", True, False)

    assert client.call.call_args.args[:4] == (
        "SaveRemoteMachine",
        "gpu-box",
        "avi@gpu-box",
        "/srv/eigen",
    )
    assert model.saving_machine is True

    callback = client.call.call_args.kwargs["callback"]
    callback({"result": {"name": "gpu-box", "ssh": "avi@gpu-box", "dir": "/srv/eigen", "saved": True}})

    assert model.saving_machine is False
    assert model.save_error == ""
    assert saved == [True]
    assert model.selected_machine["ssh"] == "avi@gpu-box"
    assert model.machines == [{"name": "gpu-box", "ssh": "avi@gpu-box", "dir": "/srv/eigen", "saved": True}]
    assert client.call.call_args.args[:3] == ("InstallRemote", "avi@gpu-box", False)


def test_machines_model_keeps_add_form_open_on_save_error():
    client = fake_client()
    model = MachinesModel(client)

    model.save_machine("gpu-box", "avi@gpu-box", "", False, True)
    callback = client.call.call_args.kwargs["callback"]
    callback({"error": {"message": "invalid SSH target"}})

    assert model.saving_machine is False
    assert model.save_error == "invalid SSH target"
    assert model.machines == []


def test_machines_model_probes_saved_host_without_installing():
    client = fake_client()
    model = MachinesModel(client)

    model.save_machine("gpu-box", "avi@gpu-box", "", False, True)
    callback = client.call.call_args.kwargs["callback"]
    callback({"result": {"name": "gpu-box", "ssh": "avi@gpu-box", "saved": True}})

    assert model.installing is False
    assert client.call.call_args.args[:2] == ("RemoteSessions", "avi@gpu-box")


def test_machines_model_refresh_updates_selected_machine_details():
    model = MachinesModel(fake_client())
    model._on_machines_result({"result": machines_payload()})
    model.select_machine("codex-box")

    updated = machines_payload()
    updated["machines"][0]["dir"] = "/work/eigen"
    model._on_machines_result({"result": updated})

    assert model.selected_machine["dir"] == "/work/eigen"
    assert model.remote_loading is True


def test_machines_model_ignores_stale_host_refresh():
    model = MachinesModel(fake_client())
    model._load_seq = 2

    model._on_machines_result({"result": machines_payload()}, seq=1)

    assert model.machine_count == 0


def test_machines_model_surfaces_host_and_remote_errors():
    model = MachinesModel(fake_client())

    model._on_machines_result({"error": {"message": "daemon offline"}})
    assert model.load_error == "daemon offline"
    assert model.loading is False

    model._on_machines_result({"result": machines_payload()})
    model.select_machine("codex-box")
    model._on_remote_result({"error": "ssh timeout"}, seq=model._remote_seq)

    assert model.remote_error == "ssh timeout"
    assert model.remote_loading is False
    assert model.remote_count == 0
