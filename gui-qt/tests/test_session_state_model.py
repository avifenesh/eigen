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


class DeferredRpcClient(QObject):
    def __init__(self):
        super().__init__()
        self.calls = []

    def call(self, method, *args, callback=None):
        self.calls.append({"method": method, "args": args, "callback": callback})


def state_payload(
    *,
    model="gpt-5",
    effort="high",
    perm="gated",
    search="auto",
    fast=False,
    levels=None,
):
    if levels is None:
        levels = ["low", "high"]
    return {
        "model": model,
        "effort": effort,
        "perm": perm,
        "title": "Qt shell",
        "goal": "Tighten provider controls",
        "search": search,
        "fast": fast,
        "fastOk": True,
        "tools": [{"name": "read_file", "read_only": True}],
        "running": False,
        "roots": ["/repo/eigen"],
        "catalog": {"models": [{"id": model, "effortLevels": levels}]},
    }


def reply(call, payload):
    callback = call["callback"]
    assert callback is not None
    callback({"result": payload})


def fail(call, message):
    callback = call["callback"]
    assert callback is not None
    callback({"error": message})


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


def test_session_state_ignores_stale_refresh_after_newer_model_change():
    client = DeferredRpcClient()
    model = SessionStateModel(client, "s-chat")
    model.seed(state_payload(model="gpt-5", levels=["low", "high"]))

    model.refresh()
    stale_refresh = client.calls[-1]

    model.setModel("local-qwen")
    model_change = client.calls[-1]
    reply(
        model_change,
        state_payload(model="local-qwen", effort="medium", levels=["low", "medium"]),
    )

    assert model.model == "local-qwen"
    assert model.effort == "medium"
    assert model.effortLevels == ["low", "medium"]

    reply(stale_refresh, state_payload(model="gpt-5", effort="high", levels=["low", "high"]))

    assert model.model == "local-qwen"
    assert model.effort == "medium"
    assert model.effortLevels == ["low", "medium"]


def test_session_state_ignores_stale_external_initial_state():
    client = DeferredRpcClient()
    model = SessionStateModel(client, "s-chat")

    initial_state_seq = model.beginStateRequest()
    model.setSearch("on")
    search_change = client.calls[-1]
    reply(search_change, state_payload(search="on"))

    model.applyState(state_payload(search="off"), initial_state_seq)

    assert model.search == "on"


def test_session_state_surfaces_action_errors_and_clears_on_retry():
    client = DeferredRpcClient()
    model = SessionStateModel(client, "s-chat")
    model.seed(state_payload(model="gpt-5"))

    model.setModel("local-qwen")
    model_change = client.calls[-1]
    fail(model_change, "daemon offline")

    assert model.model == "gpt-5"
    assert model.actionError == "Could not set model: daemon offline"

    model.setSearch("on")
    search_change = client.calls[-1]

    assert model.actionError == ""

    reply(search_change, state_payload(model="gpt-5", search="on"))

    assert model.search == "on"
    assert model.actionError == ""


def test_session_state_ignores_stale_action_errors():
    client = DeferredRpcClient()
    model = SessionStateModel(client, "s-chat")
    model.seed(state_payload(search="auto", fast=False))

    model.refresh()
    stale_refresh = client.calls[-1]

    model.setFast(True)
    fast_change = client.calls[-1]
    reply(fast_change, state_payload(search="auto", fast=True))

    fail(stale_refresh, "late daemon offline")

    assert model.fast is True
    assert model.actionError == ""
