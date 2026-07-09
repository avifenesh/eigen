from PySide6.QtCore import QObject, Signal

from eigenqt.models.commands import CommandsModel, filter_command_rows


class FakeRpcClient(QObject):
    def __init__(self, error=None):
        super().__init__()
        self.calls = []
        self.error = error

    def call(self, method, *args, callback=None):
        self.calls.append((method, args))
        if self.error is not None:
            if callback:
                callback({"error": self.error})
            return
        if callback:
            callback(
                {
                    "result": [
                        {
                            "name": "ship-it",
                            "description": "Turn the current diff into a PR",
                            "scope": "user",
                        },
                        {
                            "name": "review",
                            "description": "custom review should not shadow the built-in",
                            "scope": "user",
                        },
                    ]
                }
            )


class StartupRpcClient(QObject):
    connected = Signal()

    def __init__(self):
        super().__init__()
        self.calls = []
        self.ready = False

    def call(self, method, *args, callback=None):
        self.calls.append((method, args))
        if callback is None:
            return
        if not self.ready:
            callback({"error": "not connected"})
            return
        callback(
            {
                "result": [
                    {
                        "name": "ship-it",
                        "description": "Turn the current diff into a PR",
                        "scope": "user",
                    },
                ]
            }
        )


def model_rows(model):
    rows = []
    for row in range(model.rowCount()):
        index = model.index(row, 0)
        rows.append(
            {
                "name": model.data(index, CommandsModel.NameRole),
                "description": model.data(index, CommandsModel.DescriptionRole),
                "scope": model.data(index, CommandsModel.ScopeRole),
            }
        )
    return rows


def test_commands_model_merges_builtins_with_custom_commands():
    model = CommandsModel(FakeRpcClient())
    rows = model_rows(model)
    names = [row["name"] for row in rows]

    assert ("Commands", ()) in model._client.calls
    assert "help" in names
    assert "review" in names
    assert "ship-it" in names
    assert names.count("review") == 1
    assert model.hasCommand("help") is True
    assert model.hasCommand("ship-it") is True
    assert model.hasCommand("missing") is False
    assert model.commandScope("review") == "builtin"
    assert model.commandScope("ship-it") == "user"

    model.setFilter("sh")
    filtered = model_rows(model)
    assert filtered == [
        {
            "name": "shells",
            "description": "Show background shells in the info dock",
            "scope": "builtin",
        },
        {
            "name": "ship-it",
            "description": "Turn the current diff into a PR",
            "scope": "user",
        },
    ]
    assert model.loadError == ""


def test_commands_model_ranks_fuzzy_slash_matches():
    model = CommandsModel(FakeRpcClient())

    model.setFilter("cnfg")
    assert model_rows(model)[0]["name"] == "config"

    rvw = model.filteredCommands("rvw")
    assert rvw[0]["name"] == "review"

    custom = model.filteredCommands("shp")
    assert custom[0]["name"] == "ship-it"

    description = model.filteredCommands("current diff")
    assert description == [
        {
            "name": "ship-it",
            "description": "Turn the current diff into a PR",
            "scope": "user",
        }
    ]

    assert model.filteredCommands("zz-no-command") == []


def test_filter_command_rows_preserves_builtin_order_when_unfiltered():
    rows = filter_command_rows([{"name": "b"}, {"name": "a"}], "")
    assert [row["name"] for row in rows] == ["b", "a"]


def test_commands_model_surfaces_custom_command_load_error_and_keeps_builtins():
    model = CommandsModel(FakeRpcClient({"message": "daemon offline"}))
    names = [row["name"] for row in model_rows(model)]

    assert "help" in names
    assert "ship-it" not in names
    assert model.hasCommand("help") is True
    assert model.hasCommand("ship-it") is False
    assert model.loadError == "Could not load custom slash commands: daemon offline"

    model.clearLoadError()
    assert model.loadError == ""


def test_commands_model_retries_transient_startup_disconnect_without_banner():
    client = StartupRpcClient()
    model = CommandsModel(client)

    assert ("Commands", ()) in client.calls
    assert model.loadError == ""
    assert "ship-it" not in [row["name"] for row in model_rows(model)]

    client.ready = True
    client.connected.emit()

    names = [row["name"] for row in model_rows(model)]
    assert client.calls.count(("Commands", ())) == 2
    assert "help" in names
    assert "ship-it" in names
    assert model.loadError == ""
