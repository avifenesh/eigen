from PySide6.QtCore import QCoreApplication, QObject, Qt, QTimer, Signal

from eigenqt.models.approvals import ApprovalsModel


class FakeRpcClient(QObject):
    event = Signal(str, dict)

    def __init__(self):
        super().__init__()
        self.calls = []
        self.subscribed = []
        self.unsubscribed = []
        self.deferred = {}

    def subscribe(self, channels):
        self.subscribed.extend(channels or [])

    def unsubscribe(self, channels):
        self.unsubscribed.extend(channels or [])

    def call(self, method, *args, callback=None):
        self.calls.append((method, args))
        if method in self.deferred:
            self.deferred[method].append(callback)
            return
        if callback:
            QTimer.singleShot(0, lambda: callback({"result": {}}))


def _app():
    return QCoreApplication.instance() or QCoreApplication([])


def _pump(app, rounds=8):
    for _ in range(rounds):
        app.processEvents()


def _row(model, row=0):
    idx = model.index(row, 0)
    return {
        "id": model.data(idx, ApprovalsModel.IdRole),
        "tool": model.data(idx, ApprovalsModel.ToolRole),
        "args": model.data(idx, ApprovalsModel.ArgsRole),
        "approving": model.data(idx, ApprovalsModel.ApprovingRole),
        "error": model.data(idx, ApprovalsModel.ErrorRole),
    }


def test_approvals_model_dedupes_replayed_approval_events():
    app = _app()
    client = FakeRpcClient()
    model = ApprovalsModel(client, "s-chat")

    client.event.emit(
        "session:s-chat",
        {"event": {"kind": "approval", "result": "approval-1", "tool": "shell", "text": "make test"}},
    )
    _pump(app)
    assert model.rowCount() == 1
    assert _row(model)["args"] == "make test"

    client.event.emit(
        "session:s-chat",
        {
            "event": {
                "kind": "approval",
                "result": "approval-1",
                "tool": "shell",
                "text": "pytest -q gui-qt/tests/test_approvals_model.py",
            }
        },
    )
    _pump(app)

    assert model.rowCount() == 1
    assert _row(model)["args"] == "pytest -q gui-qt/tests/test_approvals_model.py"


def test_approvals_model_blocks_duplicate_approve_until_rpc_finishes():
    client = FakeRpcClient()
    client.deferred["Approve"] = []
    model = ApprovalsModel(client, "s-chat")
    model.seed({"pending": [{"id": "approval-1", "tool": "shell", "args": "make test"}]})

    model.approve("approval-1", True)
    model.approve("approval-1", True)

    assert client.calls == [("Approve", ("s-chat", "approval-1", True))]
    assert _row(model)["approving"] is True

    client.deferred["Approve"][0]({"result": {}})

    assert model.rowCount() == 0


def test_approvals_model_keeps_row_and_surfaces_error_on_failure():
    client = FakeRpcClient()
    client.deferred["Approve"] = []
    model = ApprovalsModel(client, "s-chat")
    errors = []
    model.actionError.connect(errors.append)
    model.seed({"pending": [{"id": "approval-1", "tool": "shell", "args": "make test"}]})

    model.approve("approval-1", False)
    client.deferred["Approve"][0]({"error": "daemon offline"})

    assert model.rowCount() == 1
    assert _row(model)["approving"] is False
    assert _row(model)["error"] == "daemon offline"
    assert errors == ["daemon offline"]

    model.approve("approval-1", False)

    assert len(client.calls) == 2
    assert _row(model)["approving"] is True
    assert _row(model)["error"] == ""


def test_approvals_model_clears_action_state_and_unsubscribes_on_detach():
    client = FakeRpcClient()
    client.deferred["Approve"] = []
    model = ApprovalsModel(client, "s-chat")
    model.seed({"pending": [{"id": "approval-1", "tool": "shell", "args": "make test"}]})
    model.approve("approval-1", True)

    model.clearRows()
    assert model.rowCount() == 0

    model.detach()
    assert client.unsubscribed == ["session:s-chat"]

    client.event.emit(
        "session:s-chat",
        {"event": {"kind": "approval", "result": "approval-2", "tool": "shell", "text": "ignored"}},
    )
    _pump(_app())
    assert model.rowCount() == 0
