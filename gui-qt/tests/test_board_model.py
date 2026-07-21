from PySide6.QtCore import QObject, Signal

from eigenqt.models.board import BoardModel, KanbanModel


class FakeRpcClient(QObject):
    connected = Signal()

    def __init__(self, responses=None):
        super().__init__()
        self.calls = []
        self.responses = responses or {}

    def call(self, method, *args, callback=None):
        self.calls.append((method, args))
        if callback:
            callback(self.responses.get(method, {"result": "s-new"}))


class StartupRpcClient(QObject):
    connected = Signal()

    def __init__(self):
        super().__init__()
        self.ready = False
        self.defer = False
        self.calls = []

    def call(self, method, *args, callback=None):
        self.calls.append((method, args))
        if not callback:
            return
        if self.defer:
            return
        if not self.ready:
            callback({"error": "not connected"})
        elif method == "Board":
            callback({"result": {"lanes": [{"name": "eigen", "items": []}]}})
        elif method == "Kanban":
            callback({"result": {"columns": [{"id": "todo", "cards": []}]}})


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


def test_board_models_retry_startup_load_when_rpc_connects():
    client = StartupRpcClient()
    board = BoardModel(client)
    kanban = KanbanModel(client)

    board.load()
    kanban.load()

    assert board.error == "not connected"
    assert kanban.error == "not connected"
    assert client.calls == [("Board", ()), ("Kanban", ())]

    client.ready = True
    client.connected.emit()

    assert board.rowCount() == 1
    assert board.error == ""
    assert len(kanban.columns) == 1
    assert kanban.error == ""
    assert client.calls == [
        ("Board", ()),
        ("Kanban", ()),
        ("Board", ()),
        ("Kanban", ()),
    ]


def test_board_models_do_not_duplicate_in_flight_load_on_connect():
    client = StartupRpcClient()
    client.defer = True
    board = BoardModel(client)
    kanban = KanbanModel(client)

    board.load()
    kanban.load()
    client.connected.emit()

    assert client.calls == [("Board", ()), ("Kanban", ())]
