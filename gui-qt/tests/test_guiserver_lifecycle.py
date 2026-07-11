from pathlib import Path

from PySide6.QtCore import QCoreApplication, QObject, Signal


ROOT = Path(__file__).resolve().parents[2]


def _fnv1a_64(data: bytes) -> str:
    value = 14695981039346656037
    for byte in data:
        value ^= byte
        value = (value * 1099511628211) & 0xFFFFFFFFFFFFFFFF
    return f"{value:016x}"


def test_supervisor_finds_checkout_binary_and_manifest():
    from eigenqt.rpc.supervise import GuiserverSupervisor

    supervisor = GuiserverSupervisor()
    assert supervisor.binary_path == ROOT / "bin" / "eigen"
    assert supervisor._compute_expected_manifest() == _fnv1a_64(
        (ROOT / "internal" / "gui" / "bridge.manifest.json").read_bytes()
    )


def test_app_context_starts_guiserver_and_tracks_daemon_health(monkeypatch):
    app = QCoreApplication.instance() or QCoreApplication([])

    import main

    class FakeSupervisor(QObject):
        instances = []

        def __init__(self, parent=None):
            super().__init__(parent)
            self.ensure_calls = []
            FakeSupervisor.instances.append(self)

        def ensure_running(self, timeout=10.0):
            self.ensure_calls.append(timeout)
            return {"sha": "abcdef1234567890", "manifest": "manifest-ok"}

    class FakeRpcClient(QObject):
        connected = Signal()
        event = Signal(str, dict)
        dropped = Signal(str)
        callDone = Signal(int, "QVariantMap")

        def __init__(self, *args, **kwargs):
            super().__init__(kwargs.get("parent"))
            self.calls = []
            self.subscriptions = []

        def call(self, method, *args, callback=None):
            self.calls.append((method, args))
            if callback is None:
                return
            if method == "hello":
                callback(
                    {
                        "result": {
                            "sha": "feedfacecafebeef",
                            "manifest": "manifest-ok",
                        }
                    }
                )
            elif method == "Stats":
                callback({"result": {"sessions": 4, "running_turns": 1}})
            else:
                callback({"result": {}})

        def subscribe(self, channels):
            self.subscriptions.append(list(channels))

        def unsubscribe(self, channels):
            self.subscriptions.append([f"unsub:{ch}" for ch in channels])

        def shutdown(self):
            pass

    class DummyModel(QObject):
        def __init__(self, *args, **kwargs):
            parent = kwargs.get("parent")
            if parent is None and args and isinstance(args[-1], QObject):
                parent = args[-1]
            super().__init__(parent)

        def mark_unread(self, session_id):
            pass

        def mark_read(self, session_id):
            pass

    class DummyReplyWatcher(QObject):
        unread = Signal(str)
        read = Signal(str)

        def __init__(self, *args, **kwargs):
            parent = kwargs.get("parent")
            if parent is None and args and isinstance(args[-1], QObject):
                parent = args[-1]
            super().__init__(parent)

    monkeypatch.setattr(main, "GuiserverSupervisor", FakeSupervisor)
    monkeypatch.setattr(main, "RpcClient", FakeRpcClient)
    for attr in (
        "ApprovalsModel",
        "BoardModel",
        "CommandsModel",
        "ConfigModel",
        "ConnectorsModel",
        "DashboardModel",
        "FeedModel",
        "KanbanModel",
        "LiveSessionsModel",
        "MemoryModel",
        "NotesController",
        "ProposalsModel",
        "ReviewersModel",
        "RuleChainsModel",
        "SessionsModel",
        "SessionStateModel",
        "SkillsModel",
        "TasksModel",
        "TranscriptModel",
    ):
        monkeypatch.setattr(main, attr, DummyModel)
    monkeypatch.setattr(main, "ReplyWatcher", DummyReplyWatcher)

    ctx = main.AppContext()
    ctx._stats_timer.stop()

    assert FakeSupervisor.instances[0].ensure_calls == [10.0]
    assert ctx.guiserverSha == "abcdef1234567890"

    ctx._on_hello({"error": "not connected"})
    assert ctx.guiserverSha == "abcdef1234567890"

    ctx.rpc_client.connected.emit()
    app.processEvents()

    assert ctx.rpc_client.subscriptions[-1] == [
        "eigen:daemon:stats",
        "eigen:daemon:health",
    ]
    assert ("hello", ()) in ctx.rpc_client.calls
    assert ("Stats", ()) in ctx.rpc_client.calls
    assert ctx.guiserverSha == "feedfacecafebeef"
    assert ctx.daemonOnline is True
    assert ctx.stats["sessions"] == 4

    ctx.rpc_client.event.emit(
        "eigen:daemon:stats", {"sessions": 8, "running_turns": 2}
    )
    app.processEvents()
    assert ctx.daemonOnline is True
    assert ctx.stats["sessions"] == 8

    ctx.rpc_client.event.emit(
        "eigen:daemon:health", {"ok": False, "error": "daemon down"}
    )
    app.processEvents()
    assert ctx.daemonOnline is False

    ctx.rpc_client.event.emit(
        "eigen:daemon:stats", {"sessions": 9, "running_turns": 0}
    )
    app.processEvents()
    assert ctx.daemonOnline is True
    assert ctx.stats["sessions"] == 9


def test_session_controller_ignores_stale_initial_state_reply():
    app = QCoreApplication.instance() or QCoreApplication([])

    import main

    class DeferredClient(QObject):
        event = Signal(str, dict)
        dropped = Signal(str)

        def __init__(self):
            super().__init__()
            self.calls = []
            self.subscriptions = []

        def call(self, method, *args, callback=None):
            self.calls.append({"method": method, "args": args, "callback": callback})

        def subscribe(self, channels):
            self.subscriptions.append(list(channels))

        def unsubscribe(self, channels):
            self.subscriptions.append([f"unsub:{channel}" for channel in channels])

    class Watcher:
        def __init__(self):
            self.current = []

        def set_current_session(self, session_id):
            self.current.append(session_id)

    def state(model, text, approval_id):
        return {
            "model": model,
            "effort": "high",
            "perm": "gated",
            "title": "Qt chat",
            "goal": "",
            "search": "auto",
            "fast": False,
            "fastOk": True,
            "tools": [],
            "running": False,
            "roots": ["/repo/eigen"],
            "catalog": {"models": [{"id": model, "effortLevels": ["low", "high"]}]},
            "messages": [{"role": "user", "text": text}],
            "pending": [{"id": approval_id, "tool": "shell", "args": "ls"}],
        }

    client = DeferredClient()
    controller = main.SessionController(client, Watcher())

    controller.open_session("s-old")
    old_state = client.calls[-1]

    hydrated_old = state("gpt-5", "old transcript", "p-old")
    hydrated_old["running"] = True
    hydrated_old["messages"] = [
        {
            "role": "assistant",
            "toolCalls": [
                {"id": "tool-old", "name": "bash", "args": '{"command":"pytest"}'}
            ],
        }
    ]
    old_state["callback"]({"result": hydrated_old})
    app.processEvents()

    assert controller.session_state_model.model == "gpt-5"
    assert controller.transcript_model.hasActivity is True
    assert controller.transcript_model.rowCount() == 1
    assert controller.approvals_model.rowCount() == 1

    controller.open_session("s-new")
    new_state = client.calls[-1]

    assert controller.session_id == "s-new"
    assert controller.session_state_model.model == ""
    assert controller.transcript_model.hasActivity is False
    assert controller.transcript_model.rowCount() == 0
    assert controller.approvals_model.rowCount() == 0

    new_state["callback"]({"result": state("local-qwen", "new transcript", "p-new")})
    app.processEvents()

    assert controller.session_id == "s-new"
    assert controller.session_state_model.model == "local-qwen"
    assert controller.transcript_model.rowCount() == 1
    assert (
        controller.transcript_model.data(
            controller.transcript_model.index(0, 0),
            controller.transcript_model.TextRole,
        )
        == "new transcript"
    )
    assert controller.approvals_model.rowCount() == 1

    old_state["callback"]({"result": hydrated_old})
    app.processEvents()

    assert controller.session_state_model.model == "local-qwen"
    assert controller.transcript_model.rowCount() == 1
    assert (
        controller.transcript_model.data(
            controller.transcript_model.index(0, 0),
            controller.transcript_model.TextRole,
        )
        == "new transcript"
    )
    assert controller.approvals_model.rowCount() == 1
    assert (
        controller.approvals_model.data(
            controller.approvals_model.index(0, 0),
            controller.approvals_model.IdRole,
        )
        == "p-new"
    )


def test_session_controller_refetches_open_chat_after_startup_connect():
    app = QCoreApplication.instance() or QCoreApplication([])

    import main

    class StartupClient(QObject):
        connected = Signal()
        event = Signal(str, dict)
        dropped = Signal(str)

        def __init__(self):
            super().__init__()
            self.ready = False
            self.calls = []
            self.subscriptions = []

        def call(self, method, *args, callback=None):
            self.calls.append({"method": method, "args": args})
            if callback is None:
                return
            if not self.ready:
                callback({"error": "not connected"})
                return
            if method == "Commands":
                callback({"result": []})
                return
            if method == "State":
                callback({"result": state()})
                return
            callback({"result": {}})

        def subscribe(self, channels):
            self.subscriptions.append(list(channels))

        def unsubscribe(self, channels):
            self.subscriptions.append([f"unsub:{channel}" for channel in channels])

    class Watcher:
        def __init__(self):
            self.current = []

        def set_current_session(self, session_id):
            self.current.append(session_id)

    def state():
        return {
            "model": "local-qwen",
            "effort": "high",
            "perm": "gated",
            "title": "Qt chat",
            "goal": "",
            "search": "auto",
            "fast": False,
            "fastOk": True,
            "tools": [],
            "running": False,
            "roots": ["/repo/eigen"],
            "catalog": {
                "models": [
                    {"id": "gpt-5", "effortLevels": ["low", "high"]},
                    {"id": "local-qwen", "effortLevels": ["low", "medium", "high"]},
                ],
                "providers": [],
            },
            "messages": [{"role": "user", "text": "state after connect"}],
            "pending": [],
        }

    client = StartupClient()
    controller = main.SessionController(client, Watcher())

    controller.open_session("s-chat")
    app.processEvents()

    assert controller.session_state_model.model == ""
    assert controller.session_state_model.catalog == []
    assert [call["method"] for call in client.calls].count("State") == 1

    client.ready = True
    client.connected.emit()
    app.processEvents()

    assert [call["method"] for call in client.calls].count("State") == 2
    assert controller.session_state_model.model == "local-qwen"
    assert controller.session_state_model.catalog == ["gpt-5", "local-qwen"]
    assert controller.session_state_model.effortLevels == ["low", "medium", "high"]
    assert controller.transcript_model.rowCount() == 1


def test_rpc_call_send_failure_reports_error_without_raising(monkeypatch):
    from eigenqt.rpc.client import RpcClient

    monkeypatch.setattr(RpcClient, "_start_workers", lambda self: None)

    client = RpcClient(sock_path=Path("/tmp/eigen-missing-guiserver.sock"))

    class BrokenWorker:
        def enqueue_call(self, method, args, callback):
            raise BrokenPipeError("dead guiserver socket")

        def stop(self):
            pass

    client._rpc_worker = BrokenWorker()
    client._rpc_ready = True
    reconnects = []
    disconnected = []
    payloads = []
    client._schedule_reconnect = lambda: reconnects.append(True)
    client.disconnected.connect(disconnected.append)

    client.call("Stats", callback=payloads.append)

    assert payloads == [{"error": "send failed: dead guiserver socket"}]
    assert disconnected == ["rpc: send failed: dead guiserver socket"]
    assert reconnects == [True]
    assert client._rpc_ready is False
    client.shutdown()


def test_rpc_client_replays_event_subscriptions_after_worker_ready(monkeypatch):
    from eigenqt.rpc.client import RpcClient

    monkeypatch.setattr(RpcClient, "_start_workers", lambda self: None)
    client = RpcClient(sock_path=Path("/tmp/eigen-missing-guiserver.sock"))

    class EventsWorker:
        def __init__(self):
            self.subscribed = []
            self.unsubscribed = []

        def subscribe(self, channels):
            self.subscribed.append(tuple(channels or []))

        def unsubscribe(self, channels):
            self.unsubscribed.append(tuple(channels or []))

        def stop(self):
            pass

    worker = EventsWorker()
    client._events_worker = worker

    client.subscribe(["session:s1"])
    assert worker.subscribed == []

    client._on_events_ready()
    assert worker.subscribed == [("session:s1",)]

    client.unsubscribe(["session:s1"])
    assert worker.unsubscribed == [("session:s1",)]

    worker2 = EventsWorker()
    client._events_worker = worker2
    client._events_ready = False
    client.subscribe(["session:s2"])
    client._on_events_ready()
    assert worker2.subscribed == [("session:s2",)]

    client._rpc_ready = True
    client._events_ready = True
    client._stop_workers()
    assert client._rpc_ready is False
    assert client._events_ready is False
    client.shutdown()


def test_rpc_worker_send_failure_clears_pending_callback():
    from eigenqt.rpc.client import _RpcWorker

    worker = _RpcWorker(Path("/tmp/eigen-missing-guiserver.sock"))

    def fail_send(_payload):
        raise BrokenPipeError("dead guiserver socket")

    worker._send = fail_send
    payloads = []

    ok, error = worker.enqueue_call("Stats", (), payloads.append)

    assert ok is False
    assert error == "send failed: dead guiserver socket"
    assert worker._pending == {}
    assert payloads == []


def test_rpc_worker_disconnect_fails_pending_callbacks_once():
    from eigenqt.rpc.client import _RpcWorker

    worker = _RpcWorker(Path("/tmp/eigen-missing-guiserver.sock"))
    payloads = []
    worker._pending[7] = payloads.append

    worker.fail_pending("rpc: recv error: socket closed")
    worker.fail_pending("rpc: reconnecting")

    assert payloads == [{"error": "rpc: recv error: socket closed"}]
    assert worker._pending == {}
