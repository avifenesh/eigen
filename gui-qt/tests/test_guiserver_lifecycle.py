from pathlib import Path

import pytest
from PySide6.QtCore import QCoreApplication, QObject, QTimer, Signal


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

    class ImmediateThread:
        def __init__(self, *, target, **_kwargs):
            self._target = target

        def start(self):
            self._target()

    class FakeSupervisor(QObject):
        instances = []
        next_error = None

        def __init__(self, parent=None):
            super().__init__(parent)
            self.ensure_calls = []
            self.error = FakeSupervisor.next_error
            FakeSupervisor.next_error = None
            FakeSupervisor.instances.append(self)

        def ensure_running(self, timeout=10.0):
            self.ensure_calls.append(timeout)
            if self.error is not None:
                raise self.error
            return {"sha": "abcdef1234567890", "manifest": "manifest-ok"}

    class FakeRpcClient(QObject):
        connected = Signal()
        disconnected = Signal(str)
        event = Signal(str, dict)
        dropped = Signal(str)
        callDone = Signal(int, "QVariantMap")

        def __init__(self, *args, **kwargs):
            super().__init__(kwargs.get("parent"))
            self.calls = []
            self.subscriptions = []
            self.shutdown_calls = 0

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
            self.shutdown_calls += 1

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
    monkeypatch.setattr(main.threading, "Thread", ImmediateThread)
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
    assert ctx.daemonStatus == "connecting"
    assert ctx.daemonOnline is False
    assert ctx._stats_timer.isActive() is False

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
    assert ctx.daemonStatus == "online"
    assert ctx.daemonOnline is True
    assert ctx.stats["sessions"] == 4
    assert ctx._stats_timer.isActive() is True

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
    assert ctx.daemonStatus == "offline"
    assert ctx.daemonOnline is False

    ctx.rpc_client.event.emit(
        "eigen:daemon:stats", {"sessions": 9, "running_turns": 0}
    )
    app.processEvents()
    assert ctx.daemonStatus == "online"
    assert ctx.daemonOnline is True
    assert ctx.stats["sessions"] == 9

    ctx.rpc_client.disconnected.emit("rpc: socket closed")
    app.processEvents()
    assert ctx.daemonStatus == "reconnecting"
    assert ctx.daemonOnline is False
    assert ctx._stats_timer.isActive() is False
    assert len(FakeSupervisor.instances) == 2
    assert FakeSupervisor.instances[1].ensure_calls == [10.0]
    assert ctx._recovering_guiserver is False

    FakeSupervisor.next_error = RuntimeError("binary unavailable")
    ctx.rpc_client.disconnected.emit("events: socket closed")
    app.processEvents()
    assert len(FakeSupervisor.instances) == 3
    assert FakeSupervisor.instances[2].ensure_calls == [10.0]
    assert ctx.guiserverSha == "error"
    assert ctx.daemonStatus == "offline"

    ctx.shutdown()
    assert ctx.rpc_client.shutdown_calls == 1


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


def test_rpc_client_reconnect_timer_is_qt_owned_and_shutdown_latched(monkeypatch):
    from eigenqt.rpc.client import RpcClient

    QCoreApplication.instance() or QCoreApplication([])
    monkeypatch.setattr(RpcClient, "_start_workers", lambda self: None)
    client = RpcClient(sock_path=Path("/tmp/eigen-missing-guiserver.sock"))

    assert isinstance(client._reconnect_timer, QTimer)
    assert client._reconnect_timer.parent() is client
    assert client._reconnect_timer.thread() == client.thread()
    assert client._reconnect_timer.isSingleShot() is True

    restarts = []
    client._stop_workers = lambda: None
    client._start_workers = lambda: restarts.append(True)
    client._schedule_reconnect()

    assert client._reconnect_timer.isActive() is True
    assert client._reconnect_timer.interval() == 1000

    client._reconnect_timer.stop()
    client._reconnect()
    assert restarts == [True]

    client.shutdown()
    client._schedule_reconnect()
    client._reconnect()
    assert client._reconnect_timer.isActive() is False
    assert restarts == [True]


def test_rpc_client_connects_when_guiserver_appears_during_backoff(tmp_path):
    from test_client_mock import MockGuiserver

    from eigenqt.rpc.client import RpcClient

    app = QCoreApplication.instance() or QCoreApplication([])
    socket_path = tmp_path / "guiserver.sock"
    server = MockGuiserver(socket_path)
    client = RpcClient(sock_path=socket_path)
    connected = []
    events = []
    timed_out = []

    def on_connected():
        connected.append(True)
        client.subscribe(["eigen:daemon:stats"])

    def on_event(channel, data):
        events.append((channel, data))
        app.quit()

    client.connected.connect(on_connected)
    client.event.connect(on_event)
    QTimer.singleShot(150, server.start)
    watchdog = QTimer(client)
    watchdog.setSingleShot(True)
    watchdog.timeout.connect(lambda: (timed_out.append(True), app.quit()))
    watchdog.start(5000)

    app.exec()
    watchdog.stop()
    client.shutdown()
    server.stop()

    assert timed_out == []
    assert connected == [True]
    assert events[0][0] == "eigen:daemon:stats"
    assert client._shutting_down is True
    assert client._reconnect_timer.isActive() is False


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


def test_rpc_worker_closed_socket_rejects_call_and_clears_callback():
    from eigenqt.rpc.client import _RpcWorker

    worker = _RpcWorker(Path("/tmp/eigen-missing-guiserver.sock"))
    payloads = []

    ok, error = worker.enqueue_call("Stats", (), payloads.append)

    assert ok is False
    assert error == "send failed: socket unavailable"
    assert worker._pending == {}
    assert payloads == []


def test_rpc_sync_timeout_releases_pending_callback(monkeypatch):
    from eigenqt.rpc.client import RpcClient, _RpcWorker

    class SinkSocket:
        def sendall(self, _payload):
            pass

    monkeypatch.setattr(RpcClient, "_start_workers", lambda self: None)
    client = RpcClient(sock_path=Path("/tmp/eigen-missing-guiserver.sock"))
    worker = _RpcWorker(client.sock_path)
    worker._sock = SinkSocket()
    client._rpc_worker = worker
    client._rpc_ready = True

    with pytest.raises(TimeoutError, match=r"call_sync\(Stats\) timed out"):
        client.call_sync("Stats", timeout=0.01)

    assert worker._pending == {}
    client.shutdown()


def test_rpc_worker_disconnect_fails_pending_callbacks_once():
    from eigenqt.rpc.client import _RpcWorker

    worker = _RpcWorker(Path("/tmp/eigen-missing-guiserver.sock"))
    payloads = []
    worker._pending[7] = payloads.append

    worker.fail_pending("rpc: recv error: socket closed")
    worker.fail_pending("rpc: reconnecting")

    assert payloads == [{"error": "rpc: recv error: socket closed"}]
    assert worker._pending == {}
