from PySide6.QtCore import QObject

from eigenqt.models.session_state import SessionStateModel


class FakeRpcClient(QObject):
    def __init__(self):
        super().__init__()
        self.calls = []
        self.state = {
            "model": "local-qwen",
            "effort": "high",
            "perm": "auto",
            "title": "Qt shell",
            "goal": "Tighten provider controls",
            "search": "auto",
            "fast": True,
            "fastOk": True,
            "tools": [
                {"name": "read_file", "read_only": True},
                {"name": "run_shell", "readOnly": False},
            ],
            "running": False,
            "roots": ["/repo/eigen", "/tmp/proof"],
            "catalog": {"models": [{"id": "local-qwen", "effortLevels": ["low", "high"]}]},
        }

    def call(self, method, *args, callback=None):
        self.calls.append((method, args))
        if method == "SetGoal":
            self.state["goal"] = args[1]
        elif method == "SetSearch":
            self.state["search"] = args[1]
        elif method == "SetFast":
            self.state["fast"] = bool(args[1])
        if callback:
            callback({"result": dict(self.state)})


def test_session_state_exposes_provider_modes_and_roots():
    client = FakeRpcClient()
    model = SessionStateModel(client, "s-chat")

    model.seed(
        {
            "model": "local-qwen",
            "effort": "high",
            "perm": "auto",
            "title": "Qt shell",
            "goal": "Tighten provider controls",
            "search": "on",
            "fast": True,
            "fastOk": True,
            "tools": [
                {"name": "read_file", "read_only": True},
                {"name": "run_shell", "readOnly": False},
            ],
            "running": False,
            "roots": ["/repo/eigen", "/tmp/proof"],
            "catalog": {"models": [{"id": "local-qwen", "effortLevels": ["low", "high"]}]},
        }
    )

    assert model.search == "on"
    assert model.fast is True
    assert model.fastOk is True
    assert model.tools == [
        {"name": "read_file", "read_only": True},
        {"name": "run_shell", "read_only": False},
    ]
    assert model.roots == ["/repo/eigen", "/tmp/proof"]
    assert model.dir == "/repo/eigen"
    assert model.effortLevels == ["low", "high"]

    model.setGoal("Ship the Qt shell")
    model.setSearch("auto")
    model.setFast(False)

    assert ("SetGoal", ("s-chat", "Ship the Qt shell")) in client.calls
    assert ("SetSearch", ("s-chat", "auto")) in client.calls
    assert ("SetFast", ("s-chat", False)) in client.calls
    assert model.goal == "Ship the Qt shell"
    assert model.search == "auto"
    assert model.fast is False
