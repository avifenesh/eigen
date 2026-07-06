from PySide6.QtCore import QObject

from eigenqt.models.commands import CommandsModel


class FakeRpcClient(QObject):
    def __init__(self):
        super().__init__()
        self.calls = []

    def call(self, method, *args, callback=None):
        self.calls.append((method, args))
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
