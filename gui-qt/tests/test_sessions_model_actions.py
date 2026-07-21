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


class DeferredRpcClient(QObject):
    connected = Signal()
    event = Signal(str, dict)

    def __init__(self):
        super().__init__()
        self.calls = []

    def call(self, method, *args, callback=None):
        self.calls.append({"method": method, "args": args, "callback": callback})
        if method == "RemoveSession" and callback:
            callback({"result": None})

    def subscribe(self, channels):
        self.calls.append({"method": "subscribe", "args": tuple(channels or []), "callback": None})


def ensure_qt_app():
    return QCoreApplication.instance() or QCoreApplication([])


def _reply(call, result):
    callback = call["callback"]
    assert callback is not None
    callback({"result": result})


def _session(session_id, *, updated):
    return {
        "id": session_id,
        "title": session_id,
        "dir": "/repo/eigen",
        "model": "gpt-5",
        "status": "idle",
        "turns": 1,
        "updated": updated,
    }


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


def test_sessions_model_prefers_gpt_55_or_56_for_chat_resume():
    ensure_qt_app()
    client = FakeRpcClient()
    model = SessionsModel(client)
    model._on_sessions_result(
        {
            "result": [
                {**_session("s-newest", updated=30), "model": "local-qwen"},
                {**_session("s-gpt", updated=20), "model": "openai.gpt-5.5"},
                {**_session("s-other", updated=10), "model": "gpt-5"},
            ]
        }
    )

    assert model.preferredChatSessionId() == "s-gpt"

    model._on_sessions_result({"result": [{**_session("s-newest", updated=30), "model": "local-qwen"}]})
    assert model.preferredChatSessionId() == "s-newest"


def test_sessions_model_actions_surface_rpc_errors_without_dropping_rows():
    ensure_qt_app()
    client = FakeRpcClient()
    model = SessionsModel(client)
    model._on_sessions_result({"result": [dict(session) for session in client.sessions]})
    client.errors["RemoveSession"] = {"message": "daemon offline"}

    model.removeSession("s-run")

    assert model.rowCount() == 2
    assert model.actionError == "daemon offline"
    assert model.removing == []

    model.clearActionError()
    assert model.actionError == ""

    client.errors.clear()
    client.errors["ExportSession"] = {"error": {"message": "export denied"}}
    model.exportSession("s-run")
    assert model.actionError == "export denied"
    assert model.exporting == []

    model.clearActionError()
    client.errors.clear()
    client.errors["PruneSessions"] = {"message": "prune denied"}
    model.pruneSessions()
    assert model.actionError == "prune denied"
    assert model.pruning is False


def test_sessions_model_ignores_stale_refresh_callbacks():
    ensure_qt_app()
    client = DeferredRpcClient()
    model = SessionsModel(client)

    model.refresh()
    first = client.calls[-1]
    assert model.loading is True
    assert model.loaded is False
    model.refresh()
    second = client.calls[-1]

    _reply(second, [_session("s-fresh", updated=20)])
    assert model.loading is False
    assert model.loaded is True
    assert model.loadError == ""
    assert model.rowCount() == 1
    assert model.data(model.index(0, 0), model.IdRole) == "s-fresh"

    _reply(first, [_session("s-stale", updated=10)])
    assert model.rowCount() == 1
    assert model.data(model.index(0, 0), model.IdRole) == "s-fresh"


def test_sessions_model_surfaces_initial_load_error_and_recovers():
    ensure_qt_app()
    client = DeferredRpcClient()
    model = SessionsModel(client)

    model.refresh()
    failed = client.calls[-1]
    failed["callback"]({"error": {"message": "daemon offline"}})

    assert model.loading is False
    assert model.loaded is False
    assert model.loadError == "daemon offline"
    assert model.rowCount() == 0

    model.refresh()
    recovered = client.calls[-1]
    assert model.loading is True
    assert model.loadError == ""
    _reply(recovered, [_session("s-recovered", updated=20)])

    assert model.loading is False
    assert model.loaded is True
    assert model.loadError == ""
    assert model.data(model.index(0, 0), model.IdRole) == "s-recovered"


def test_sessions_model_keeps_loaded_rows_when_refresh_fails():
    ensure_qt_app()
    client = DeferredRpcClient()
    model = SessionsModel(client)
    model._on_sessions_result({"result": [_session("s-existing", updated=20)]})

    model.refresh()
    refresh = client.calls[-1]
    refresh["callback"]({"error": "temporary failure"})

    assert model.loaded is True
    assert model.loading is False
    assert model.loadError == "temporary failure"
    assert model.rowCount() == 1
    assert model.data(model.index(0, 0), model.IdRole) == "s-existing"

def test_sessions_model_remove_ignores_older_list_snapshot():
    ensure_qt_app()
    client = DeferredRpcClient()
    model = SessionsModel(client)
    model._on_sessions_result({"result": [_session("s-run", updated=20), _session("s-empty", updated=10)]})

    model.refresh()
    stale_list_call = client.calls[-1]

    model.removeSession("s-run")
    current_list_call = client.calls[-1]

    assert model.rowCount() == 1
    assert model.data(model.index(0, 0), model.IdRole) == "s-empty"
    assert model.actionMessage == "Removed s-run"

    _reply(stale_list_call, [_session("s-run", updated=20), _session("s-empty", updated=10)])
    assert model.rowCount() == 1
    assert model.data(model.index(0, 0), model.IdRole) == "s-empty"

    _reply(current_list_call, [_session("s-empty", updated=10)])
    assert model.rowCount() == 1
    assert model.data(model.index(0, 0), model.IdRole) == "s-empty"
