"""Tests for the Qt plugin inventory model."""

from unittest.mock import Mock

from PySide6.QtCore import QCoreApplication

from eigenqt.models.plugins import PluginsModel


def ensure_app():
    return QCoreApplication.instance() or QCoreApplication([])


def fake_client():
    client = Mock()
    client.connected = Mock()
    client.connected.connect = Mock()
    client.call = Mock()
    return client


def plugins_payload():
    return {
        "plugins": [
            {
                "name": "agentsys",
                "marketplace": "core",
                "version": "5.1.0",
                "description": "Agent workflow tools",
                "installedMs": 1783155600000,
                "enabled": True,
                "skills": ["audit-project"],
                "agents": ["reviewer"],
                "mcpServers": ["github"],
                "commands": ["enhance"],
                "hooks": 2,
                "scanStatus": "clean",
                "scanCount": 0,
            },
            {
                "name": "local-risk",
                "marketplace": "lab",
                "version": "0.1.0",
                "description": "Local experiment",
                "installedMs": 1783144800000,
                "enabled": False,
                "skills": ["scratch"],
                "scanStatus": "forced",
                "scanCount": 1,
                "scans": [{"component": "skills/scratch", "reasons": ["network shell"]}],
                "warnings": ["installed with scan flags"],
            },
        ],
        "marketplaces": [
            {
                "name": "core",
                "source": "github.com/avifenesh/eigen-plugins",
                "owner": "Avi",
                "disabled": False,
                "addedMs": 1783152000000,
            },
            {
                "name": "lab",
                "source": "/home/user/plugins/lab",
                "disabled": True,
                "addedMs": 1783141200000,
            },
        ],
    }


def test_plugins_model_stores_inventory_summary():
    model = PluginsModel(fake_client())

    model._on_plugins_result({"result": plugins_payload()})

    assert model.plugin_count == 2
    assert model.enabled_count == 1
    assert model.marketplace_count == 2
    assert model.disabled_market_count == 1
    assert model.scan_flag_count == 1
    assert model.plugins[0]["name"] == "agentsys"
    assert model.marketplaces[1]["name"] == "lab"


def test_plugins_model_activation_fetches_inventory():
    ensure_app()
    client = fake_client()
    model = PluginsModel(client)

    model.set_active(True)

    assert client.call.call_count == 1
    assert client.call.call_args.args[:1] == ("Plugins",)
    assert model.loading is True


def test_plugins_model_ignores_stale_inventory():
    model = PluginsModel(fake_client())
    model._load_seq = 2

    model._on_plugins_result({"result": plugins_payload()}, seq=1)

    assert model.plugin_count == 0
    assert model.marketplace_count == 0


def test_plugins_model_stop_ignores_late_inventory():
    model = PluginsModel(fake_client())
    model._load_seq = 1

    model.stop_polling()
    model._on_plugins_result({"result": plugins_payload()}, seq=1)

    assert model.plugin_count == 0
    assert model.marketplace_count == 0


def test_plugins_model_surfaces_load_error():
    model = PluginsModel(fake_client())

    model._on_plugins_result({"error": {"message": "registry unavailable"}})

    assert model.load_error == "registry unavailable"
    assert model.loading is False
