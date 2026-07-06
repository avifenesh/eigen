from PySide6.QtCore import QObject, QCoreApplication, Signal

from eigenqt.models.sessions import SessionsModel


class FakeRpcClient(QObject):
    connected = Signal()
    event = Signal(str, dict)

    def __init__(self):
        super().__init__()
        self.calls = []
        self.errors = {}
        self.sessions = [
            {
                "id": "s-empty",
                "title": "Empty scratch",
                "dir": "/repo/eigen",
                "model": "gpt-5",
                "status": "idle",
                "turns": 0,
                "updated": 10,
            },
            {
                "id": "s-run",
                "title": "Running QA",
                "dir": "/repo/eigen/gui-qt",
                "model": "local-qwen",
                "status": "idle",
                "turns": 2,
                "updated": 20,
            },
        ]

    def call(self, method, *args, callback=None):
        self.calls.append((method, args))
        if method in self.errors:
            payload = {"error": self.errors[method]}
        elif method == "Sessions":
            payload = {"result": [dict(session) for session in self.sessions]}
        elif method == "RemoveSession":
            session_id = args[0]
            self.sessions = [session for session in self.sessions if session["id"] != session_id]
            payload = {"result": None}
        elif method == "PruneSessions":
            removed = [session["id"] for session in self.sessions if session["turns"] == 0]
            self.sessions = [session for session in self.sessions if session["id"] not in removed]
            payload = {"result": removed}
        elif method == "ExportSession":
            payload = {"result": f"/home/user/eigen-exports/{args[0]}.jsonl"}
        else:
            payload = {"result": None}
        if callback:
            callback(payload)

    def subscribe(self, channels):
        self.calls.append(("subscribe", tuple(channels or [])))


def ensure_qt_app():
    return QCoreApplication.instance() or QCoreApplication([])


def test_sessions_model_remove_and_prune_refresh_rows():
    ensure_qt_app()
    client = FakeRpcClient()
    model = SessionsModel(client)
    model._on_sessions_result({"result": [dict(session) for session in client.sessions]})

    assert model.rowCount() == 2

    model.removeSession("s-run")
    assert ("RemoveSession", ("s-run",)) in client.calls
    assert ("Sessions", ()) in client.calls
    assert model.rowCount() == 1
    assert model.data(model.index(0, 0), model.IdRole) == "s-empty"
    assert model.actionError == ""
    assert model.removing == []

    model.pruneSessions()
    assert ("PruneSessions", ()) in client.calls
    assert model.rowCount() == 0
    assert model.actionError == ""
    assert model.pruning is False
    assert model.actionMessage == "Pruned 1 empty session"


def test_sessions_model_filter_and_export_session():
    ensure_qt_app()
    client = FakeRpcClient()
    model = SessionsModel(client)
    model._on_sessions_result({"result": [dict(session) for session in client.sessions]})

    assert model.totalCount == 2
    assert model.filteredCount == 2

    model.query = "gui-qt"
    assert model.filteredCount == 1
    assert model.rowCount() == 1
    assert model.data(model.index(0, 0), model.IdRole) == "s-run"

    model.exportSession("s-run")

    assert ("ExportSession", ("s-run",)) in client.calls
    assert model.exporting == []
    assert model.actionError == ""
    assert model.actionMessage == "Exported s-run to /home/user/eigen-exports/s-run.jsonl"


def test_sessions_model_actions_surface_rpc_errors_without_dropping_rows():
    ensure_qt_app()
    client = FakeRpcClient()
    model = SessionsModel(client)
    model._on_sessions_result({"result": [dict(session) for session in client.sessions]})
    client.errors["RemoveSession"] = "daemon offline"

    model.removeSession("s-run")

    assert model.rowCount() == 2
    assert model.actionError == "daemon offline"
    assert model.removing == []
