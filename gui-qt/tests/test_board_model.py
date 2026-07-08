from PySide6.QtCore import QObject

from eigenqt.models.board import BoardModel


class FakeRpcClient(QObject):
    def __init__(self, responses=None):
        super().__init__()
        self.calls = []
        self.responses = responses or {}

    def call(self, method, *args, callback=None):
        self.calls.append((method, args))
        if callback:
            callback(self.responses.get(method, {"result": "s-new"}))


def seed_board(model: BoardModel, pinned: bool = False):
    model._on_board_result(
        {
            "result": {
                "lanes": [
                    {
                        "name": "eigen",
                        "dir": "/repo/eigen",
                        "repo": "",
                        "remote": False,
                        "pinned": pinned,
                        "items": [],
                    }
                ]
            }
        }
    )


def test_board_model_surfaces_pin_error_and_clears_pending_state():
    client = FakeRpcClient({"PinLane": {"error": {"message": "daemon offline"}}})
    model = BoardModel(client)
    seed_board(model)

    model.toggle_pin("/repo/eigen")

    assert ("PinLane", ("/repo/eigen",)) in client.calls
    assert model.isPinning("/repo/eigen") is False
    assert model.actionError == "Could not pin lane: daemon offline"

    model.clearActionError()
    assert model.actionError == ""


def test_board_model_surfaces_lane_session_error_without_emitting_session():
    client = FakeRpcClient({"NewSession": {"error": {"message": "daemon offline"}}})
    model = BoardModel(client)
    started = []
    model.sessionStarted.connect(started.append)

    model.open_lane_chat("/repo/eigen")

    assert ("NewSession", ("/repo/eigen", "", "")) in client.calls
    assert started == []
    assert model.actionError == "Could not start session: daemon offline"

