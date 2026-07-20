"""Tests for the Qt plugin inventory and management model."""

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


def callback_for(client, method, occurrence=0):
    calls = [call for call in client.call.call_args_list if call.args[0] == method]
    return calls[occurrence].kwargs["callback"]


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


def test_plugins_model_rejects_malformed_inventory_response():
    model = PluginsModel(fake_client())
    model._set_loading(True)

    model._on_plugins_result(None)

    assert model.load_error == "Invalid daemon response"
    assert model.loading is False


def test_add_marketplace_chains_inventory_and_preview_refresh():
    client = fake_client()
    model = PluginsModel(client)
    model.marketplace_source = " avifenesh/eigen-plugins "

    model.add_marketplace()

    assert model.adding_marketplace is True
    assert model.flow_busy is True
    assert model.registry_busy is True
    assert client.call.call_args.args[:2] == (
        "AddMarketplace",
        "avifenesh/eigen-plugins",
    )

    callback_for(client, "AddMarketplace")(
        {"result": {"name": "core", "source": "avifenesh/eigen-plugins"}}
    )

    assert model.marketplace_source == ""
    assert model.action_message == "Added marketplace core"
    assert model.browsing_marketplace == "core"
    assert [call.args[0] for call in client.call.call_args_list] == [
        "AddMarketplace",
        "Plugins",
        "MarketplacePlugins",
    ]

    callback_for(client, "MarketplacePlugins")(
        {
            "result": [
                {
                    "name": "agentsys",
                    "marketplace": "core",
                    "skills": 2,
                    "commands": 1,
                }
            ]
        }
    )

    assert model.flow_busy is False
    assert model.registry_busy is False
    assert model.preview_marketplace == "core"
    assert model.previews[0]["name"] == "agentsys"


def test_marketplace_flow_blocks_duplicate_slow_requests_and_surfaces_error():
    client = fake_client()
    model = PluginsModel(client)
    model.marketplace_source = "source"

    model.add_marketplace()
    model.add_marketplace()
    model.browse_marketplace("core")

    assert client.call.call_count == 1
    callback_for(client, "AddMarketplace")(
        {"error": {"message": "catalog unavailable"}}
    )

    assert model.flow_busy is False
    assert model.marketplace_source == "source"
    assert model.action_error == "catalog unavailable"


def test_marketplace_flows_unlock_after_malformed_daemon_responses():
    client = fake_client()
    model = PluginsModel(client)
    model.marketplace_source = "source"

    model.add_marketplace()
    callback_for(client, "AddMarketplace")(None)

    assert model.action_error == "Invalid daemon response"
    assert model.marketplace_source == "source"
    assert model.registry_busy is False

    model._set_previews([{"name": "old", "marketplace": "core"}], "core")
    model.browse_marketplace("core")
    callback_for(client, "MarketplacePlugins")(None)

    assert model.action_error == "Invalid daemon response"
    assert model.previews == []
    assert model.preview_marketplace == ""
    assert model.registry_busy is False


def test_install_plugin_updates_only_after_success_and_blocks_other_flow_actions():
    client = fake_client()
    model = PluginsModel(client)
    model._set_previews(
        [{"name": "agentsys", "marketplace": "core"}],
        "core",
    )

    model.install_plugin("agentsys", "core")
    model.install_plugin("agentsys", "core")
    model.browse_marketplace("core")

    assert client.call.call_count == 1
    assert model.previews[0]["name"] == "agentsys"
    assert model.installing_plugin == "agentsys"

    callback_for(client, "InstallPlugin")(
        {"result": {"name": "agentsys", "enabled": True}}
    )

    assert model.installing_plugin == ""
    assert model.previews == []
    assert model.action_message == "Installed agentsys"
    assert [call.args[0] for call in client.call.call_args_list] == [
        "InstallPlugin",
        "Plugins",
    ]


def test_install_plugin_keeps_preview_on_scan_failure():
    client = fake_client()
    model = PluginsModel(client)
    model._set_previews(
        [{"name": "unsafe", "marketplace": "lab"}],
        "lab",
    )

    model.install_plugin("unsafe", "lab")
    callback_for(client, "InstallPlugin")(
        {"error": {"message": "RISKY: network shell"}}
    )

    assert model.previews[0]["name"] == "unsafe"
    assert model.action_error == "RISKY: network shell"
    assert model.flow_busy is False


def test_install_and_resource_actions_unlock_after_malformed_daemon_responses():
    client = fake_client()
    model = PluginsModel(client)
    model._set_previews([{"name": "agentsys", "marketplace": "core"}], "core")

    model.install_plugin("agentsys", "core")
    callback_for(client, "InstallPlugin")(None)

    assert model.action_error == "Invalid daemon response"
    assert model.previews[0]["name"] == "agentsys"
    assert model.registry_busy is False

    model.set_plugin_enabled("agentsys", False)
    callback_for(client, "SetPluginEnabled")(None)

    assert model.action_error == "Invalid daemon response"
    assert model.is_pending("plugin", "agentsys") is False
    assert model.registry_busy is False
    assert [call.args[0] for call in client.call.call_args_list][-1] == "Plugins"


def test_resource_mutations_are_serialized_and_refresh_after_completion():
    client = fake_client()
    model = PluginsModel(client)

    model.set_plugin_enabled("agentsys", False)
    model.remove_plugin("agentsys")
    model.set_market_enabled("core", False)

    assert client.call.call_count == 1
    assert model.registry_busy is True
    assert model.is_pending("plugin", "agentsys") is True
    assert client.call.call_args.args[:3] == (
        "SetPluginEnabled",
        "agentsys",
        False,
    )

    callback_for(client, "SetPluginEnabled")({"result": True})

    assert model.is_pending("plugin", "agentsys") is False
    assert model.registry_busy is False
    assert model.action_message == "Disabled agentsys"
    assert [call.args[0] for call in client.call.call_args_list] == [
        "SetPluginEnabled",
        "Plugins",
    ]


def test_marketplace_remove_false_result_is_non_destructive_feedback():
    client = fake_client()
    model = PluginsModel(client)

    model.remove_marketplace("missing")
    callback_for(client, "RemoveMarketplace")({"result": False})

    assert model.action_error == ""
    assert model.action_message == "Marketplace missing was not found"
    assert model.is_pending("market", "missing") is False


def test_disabling_preview_marketplace_clears_stale_install_choices():
    client = fake_client()
    model = PluginsModel(client)
    model._set_previews(
        [{"name": "qt-tool", "marketplace": "lab"}],
        "lab",
    )

    model.set_market_enabled("lab", False)
    callback_for(client, "SetMarketEnabled")({"result": True})

    assert model.previews == []
    assert model.preview_marketplace == ""
    assert model.action_message == "Disabled lab"
