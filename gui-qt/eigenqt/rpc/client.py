"""
client.py — RPC client for eigenqt (thread-safe, two-connection architecture).

Owns TWO QThread-hosted socket workers (rpc + events). All socket reads + json.loads
happen on worker threads; results cross to GUI thread via queued signals.

Usage:
    client = RpcClient()
    client.connected.connect(lambda: print("connected"))
    client.event.connect(lambda ch, data: print(f"event on {ch}: {data}"))

    # async call with callback
    client.call("Sessions", callback=lambda result: print(result))

    # sync call (blocks until response or timeout)
    result = client.call_sync("Sessions", timeout=5.0)

    client.subscribe(["eigen:daemon:stats"])
    client.unsubscribe(["eigen:daemon:stats"])
"""

import json
import socket
import threading
from pathlib import Path
from typing import Any, Callable, Optional

from PySide6.QtCore import QObject, QThread, QTimer, Signal, Slot


class RpcClient(QObject):
    """Thread-safe guiserver RPC client (two-connection architecture)."""

    # Signals (all emitted on GUI thread via queued connections)
    connected = Signal()  # Both connections ready
    disconnected = Signal(str)  # Disconnect reason
    event = Signal(str, dict)  # (channel, data)
    dropped = Signal(str)  # channel
    callDone = Signal(int, "QVariantMap")  # (token, payload) — QML token-call results

    def __init__(self, sock_path: Optional[Path] = None, parent: Optional[QObject] = None):
        self._qml_token = 0
        super().__init__(parent)
        self.sock_path = sock_path or (Path.home() / ".eigen" / "guiserver.sock")

        # Workers (will be moved to threads)
        self._rpc_worker: Optional[_RpcWorker] = None
        self._events_worker: Optional[_EventsWorker] = None
        self._rpc_thread: Optional[QThread] = None
        self._events_thread: Optional[QThread] = None

        # Connection tracking
        self._rpc_ready = False
        self._events_ready = False
        # Desired event subscriptions.  Models often subscribe before the
        # events socket is ready, and the socket can reconnect underneath them;
        # replay this set whenever a worker reaches ready so live transcript
        # deltas do not require a manual chat refresh.
        self._subscriptions: set[str] = set()
        self._reconnect_timer: Optional[threading.Timer] = None
        self._backoff_sec = 1.0

        self._start_workers()

    def _start_workers(self) -> None:
        """Spawn both worker threads and attempt connection."""
        # RPC worker
        self._rpc_thread = QThread()
        self._rpc_worker = _RpcWorker(self.sock_path)
        self._rpc_worker.moveToThread(self._rpc_thread)

        self._rpc_worker.ready.connect(self._on_rpc_ready, Qt.QueuedConnection)
        self._rpc_worker.error.connect(self._on_rpc_error, Qt.QueuedConnection)
        self._rpc_worker.response.connect(self._on_rpc_response, Qt.QueuedConnection)

        self._rpc_thread.started.connect(self._rpc_worker.connect_and_run)
        self._rpc_thread.start()

        # Events worker
        self._events_thread = QThread()
        self._events_worker = _EventsWorker(self.sock_path)
        self._events_worker.moveToThread(self._events_thread)

        self._events_worker.ready.connect(self._on_events_ready, Qt.QueuedConnection)
        self._events_worker.error.connect(self._on_events_error, Qt.QueuedConnection)
        self._events_worker.event_data.connect(lambda ch, data: self.event.emit(ch, data), Qt.QueuedConnection)
        self._events_worker.event_dropped.connect(lambda ch: self.dropped.emit(ch), Qt.QueuedConnection)

        self._events_thread.started.connect(self._events_worker.connect_and_run)
        self._events_thread.start()

    @Slot()
    def _on_rpc_ready(self) -> None:
        self._rpc_ready = True
        self._check_both_ready()

    @Slot()
    def _on_events_ready(self) -> None:
        self._events_ready = True
        self._replay_subscriptions()
        self._check_both_ready()

    def _replay_subscriptions(self) -> None:
        """Send all desired event subscriptions to a ready events worker."""
        if not self._events_worker or not self._events_ready or not self._subscriptions:
            return
        try:
            self._events_worker.subscribe(sorted(self._subscriptions))
        except Exception as exc:
            self._events_ready = False
            self._handle_disconnect(f"events: resubscribe failed: {exc}")

    def _check_both_ready(self) -> None:
        """Emit connected signal once both workers are ready."""
        if self._rpc_ready and self._events_ready:
            self._backoff_sec = 1.0  # reset backoff on successful connect
            self.connected.emit()

    @Slot(str)
    def _on_rpc_error(self, reason: str) -> None:
        self._rpc_ready = False
        if self._rpc_worker:
            self._rpc_worker.fail_pending(f"rpc: {reason}")
        self._handle_disconnect(f"rpc: {reason}")

    @Slot(str)
    def _on_events_error(self, reason: str) -> None:
        self._events_ready = False
        self._handle_disconnect(f"events: {reason}")

    def _handle_disconnect(self, reason: str) -> None:
        """Handle disconnect and schedule reconnect with backoff."""
        if not self._rpc_ready or not self._events_ready:
            self.disconnected.emit(reason)
            self._schedule_reconnect()

    def _schedule_reconnect(self) -> None:
        """Schedule reconnect with exponential backoff (1s, 2s, 4s, ... max 15s)."""
        if self._reconnect_timer and self._reconnect_timer.is_alive():
            return  # already scheduled

        delay = self._backoff_sec
        self._backoff_sec = min(self._backoff_sec * 2, 15.0)

        self._reconnect_timer = threading.Timer(delay, self._reconnect)
        self._reconnect_timer.start()

    def _reconnect(self) -> None:
        """Tear down and restart workers."""
        if self._rpc_worker:
            self._rpc_worker.fail_pending("rpc: reconnecting")
        self._stop_workers()
        self._start_workers()

    def _stop_workers(self) -> None:
        """Stop both worker threads gracefully."""
        # A reconnect replaces both sockets. Clear readiness up front so the
        # first replacement worker cannot emit connected while its peer is
        # still starting.
        self._rpc_ready = False
        self._events_ready = False
        if self._rpc_worker:
            self._rpc_worker.stop()
        if self._events_worker:
            self._events_worker.stop()

        if self._rpc_thread and self._rpc_thread.isRunning():
            self._rpc_thread.quit()
            self._rpc_thread.wait(2000)

        if self._events_thread and self._events_thread.isRunning():
            self._events_thread.quit()
            self._events_thread.wait(2000)

        self._rpc_worker = None
        self._events_worker = None
        self._rpc_thread = None
        self._events_thread = None

    @Slot(int, dict)
    def _on_rpc_response(self, req_id: int, payload: dict) -> None:
        """Dispatch RPC response to waiting callback."""
        if self._rpc_worker:
            self._rpc_worker.dispatch_response(req_id, payload)

    def call(self, method: str, *args, callback: Optional[Callable[[Any], None]] = None) -> None:
        """Async RPC call (id-multiplexed). Result delivered to callback on GUI thread."""
        if not self._rpc_worker or not self._rpc_ready:
            if callback:
                callback({"error": "not connected"})
            return

        try:
            ok, error = self._rpc_worker.enqueue_call(method, args, callback)
        except Exception as exc:
            ok, error = False, f"send failed: {exc}"

        if not ok:
            self._rpc_ready = False
            if callback:
                callback({"error": error})
            self._handle_disconnect(f"rpc: {error}")

    # Token-based QML variant: QJSValue callbacks captured across event-loop
    # turns are unreliable in PySide6 (the JS function can be collected before
    # the response lands, silently dropping the callback). Instead QML calls
    # callToken() and listens for the callDone(token, payload) signal
    # (declared with the other class-level Signals above).
    @Slot(str, "QVariantList", result=int)
    def callToken(self, method: str, args: list) -> int:
        self._qml_token += 1
        token = self._qml_token
        # Always deliver on the NEXT event-loop turn: call() invokes the
        # callback synchronously when disconnected, i.e. before callToken has
        # even returned the token to QML — the QML side hasn't stored it yet
        # and would drop the response. A 0-timer makes delivery order uniform.
        self.call(
            method,
            *list(args or []),
            callback=lambda payload, t=token: QTimer.singleShot(
                0, lambda: self.callDone.emit(t, payload)
            ),
        )
        return token

    # Fire-and-forget QML variant. Python's *args form of call() carries no
    # Slot metadata, so it is INVISIBLE to the QML engine — a QML
    # `rpcClient.call(...)` throws "not a function". Mutations that don't
    # need the response (Interrupt, SendInput, ...) use this; calls that DO
    # need the result use callToken() + the callDone signal (QJSValue
    # callbacks held across event-loop turns proved unreliable in PySide6 —
    # the JS function can be collected before the response lands).
    @Slot(str, "QVariantList")
    def callFire(self, method: str, args: list) -> None:
        self.call(method, *list(args or []))

    def call_sync(self, method: str, *args, timeout: float = 5.0) -> Any:
        """Sync RPC call (blocks until response or timeout). Returns result or raises RuntimeError."""
        if not self._rpc_worker or not self._rpc_ready:
            raise RuntimeError("not connected")

        event = threading.Event()
        result_box = {}

        def cb(payload):
            result_box["payload"] = payload
            event.set()

        try:
            ok, error = self._rpc_worker.enqueue_call(method, args, cb)
        except Exception as exc:
            ok, error = False, f"send failed: {exc}"
        if not ok:
            self._rpc_ready = False
            self._handle_disconnect(f"rpc: {error}")
            raise RuntimeError(error)

        if not event.wait(timeout):
            raise TimeoutError(f"call_sync({method}) timed out after {timeout}s")

        payload = result_box["payload"]
        if "error" in payload:
            raise RuntimeError(payload["error"])
        return payload.get("result")

    @Slot("QVariantList")
    def subscribe(self, channels: list[str]) -> None:
        """Subscribe to event channels."""
        wanted = [str(channel) for channel in (channels or []) if channel]
        self._subscriptions.update(wanted)
        if not self._events_worker or not self._events_ready or not wanted:
            return
        try:
            self._events_worker.subscribe(wanted)
        except Exception as exc:
            self._events_ready = False
            self._handle_disconnect(f"events: send failed: {exc}")

    @Slot("QVariantList")
    def unsubscribe(self, channels: list[str]) -> None:
        """Unsubscribe from event channels."""
        unwanted = [str(channel) for channel in (channels or []) if channel]
        for channel in unwanted:
            self._subscriptions.discard(channel)
        if not self._events_worker or not self._events_ready or not unwanted:
            return
        try:
            self._events_worker.unsubscribe(unwanted)
        except Exception as exc:
            self._events_ready = False
            self._handle_disconnect(f"events: send failed: {exc}")

    def shutdown(self) -> None:
        """Gracefully shut down both workers and threads."""
        if self._reconnect_timer and self._reconnect_timer.is_alive():
            self._reconnect_timer.cancel()
        self._stop_workers()


# Import Qt namespace for connection type
from PySide6.QtCore import Qt


class _RpcWorker(QObject):
    """Worker for RPC connection (runs on dedicated thread)."""

    ready = Signal()
    error = Signal(str)
    response = Signal(int, dict)  # (req_id, payload)

    def __init__(self, sock_path: Path):
        super().__init__()
        self.sock_path = sock_path
        self._sock: Optional[socket.socket] = None
        self._buf = b""
        self._running = False
        self._next_id = 1
        self._pending: dict[int, Callable] = {}  # req_id -> callback
        self._lock = threading.Lock()

    @Slot()
    def connect_and_run(self) -> None:
        """Connect to guiserver and run read loop."""
        try:
            self._sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            self._sock.settimeout(10.0)
            self._sock.connect(str(self.sock_path))

            # Declare role
            self._send({"role": "rpc"})

            self._running = True
            self.ready.emit()

            # Read loop (blocking, on worker thread)
            while self._running:
                try:
                    msg = self._recv()
                    req_id = msg.get("id")
                    if req_id is not None:
                        self.response.emit(req_id, msg)
                except socket.timeout:
                    continue  # normal timeout, keep looping
                except Exception as e:
                    if self._running:
                        self.error.emit(f"recv error: {e}")
                    break

        except Exception as e:
            self.error.emit(f"connect failed: {e}")

        finally:
            if self._sock:
                self._sock.close()
            self._running = False

    def enqueue_call(self, method: str, args: tuple, callback: Optional[Callable]) -> tuple[bool, str]:
        """Thread-safe socket send for an RPC call."""
        with self._lock:
            req_id = self._next_id
            self._next_id += 1

            if callback:
                self._pending[req_id] = callback

            payload = {"id": req_id, "call": method, "args": list(args)}
            try:
                self._send(payload)
            except Exception as exc:
                self._pending.pop(req_id, None)
                self._running = False
                return False, f"send failed: {exc}"

        return True, ""

    def dispatch_response(self, req_id: int, payload: dict) -> None:
        """Dispatch response to callback (called on GUI thread via signal)."""
        with self._lock:
            cb = self._pending.pop(req_id, None)

        if cb:
            if "error" in payload:
                cb({"error": payload["error"]})
            else:
                cb(payload)

    def fail_pending(self, reason: str) -> None:
        """Complete pending callbacks with an error before dropping a dead worker."""
        with self._lock:
            pending = list(self._pending.values())
            self._pending.clear()

        for cb in pending:
            cb({"error": reason})

    def stop(self) -> None:
        """Stop the worker."""
        self._running = False
        if self._sock:
            try:
                self._sock.shutdown(socket.SHUT_RDWR)
            except:
                pass

    def _send(self, obj: dict) -> None:
        """Send JSON line (NOT thread-safe — caller must hold lock if needed)."""
        if not self._sock:
            return
        line = json.dumps(obj).encode("utf-8") + b"\n"
        self._sock.sendall(line)

    def _recv(self) -> dict:
        """Receive one JSON line (blocking, on worker thread). Handles 32MB budget."""
        MAX_LINE = 32 * 1024 * 1024

        while b"\n" not in self._buf:
            if len(self._buf) > MAX_LINE:
                raise RuntimeError(f"line exceeds 32MB budget")

            chunk = self._sock.recv(65536)
            if not chunk:
                raise ConnectionError("socket closed")
            self._buf += chunk

        line, self._buf = self._buf.split(b"\n", 1)
        return json.loads(line.decode("utf-8"))


class _EventsWorker(QObject):
    """Worker for events connection (runs on dedicated thread)."""

    ready = Signal()
    error = Signal(str)
    event_data = Signal(str, dict)  # (channel, data)
    event_dropped = Signal(str)  # channel

    def __init__(self, sock_path: Path):
        super().__init__()
        self.sock_path = sock_path
        self._sock: Optional[socket.socket] = None
        self._buf = b""
        self._running = False

    @Slot()
    def connect_and_run(self) -> None:
        """Connect to guiserver and run read loop."""
        try:
            self._sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            self._sock.settimeout(30.0)  # longer timeout for events (may be idle)
            self._sock.connect(str(self.sock_path))

            # Declare role
            self._send({"role": "events"})

            self._running = True
            self.ready.emit()

            # Read loop (blocking, on worker thread)
            while self._running:
                try:
                    msg = self._recv()
                    event_type = msg.get("event")
                    channel = msg.get("channel")

                    if event_type == "data":
                        data = msg.get("data", {})
                        self.event_data.emit(channel, data)
                    elif event_type == "dropped":
                        self.event_dropped.emit(channel)

                except socket.timeout:
                    continue  # normal idle timeout, keep looping
                except Exception as e:
                    if self._running:
                        self.error.emit(f"recv error: {e}")
                    break

        except Exception as e:
            self.error.emit(f"connect failed: {e}")

        finally:
            if self._sock:
                self._sock.close()
            self._running = False

    def subscribe(self, channels: list[str]) -> None:
        """Thread-safe: subscribe to channels."""
        if self._sock:
            self._send({"sub": channels})

    def unsubscribe(self, channels: list[str]) -> None:
        """Thread-safe: unsubscribe from channels."""
        if self._sock:
            self._send({"unsub": channels})

    def stop(self) -> None:
        """Stop the worker."""
        self._running = False
        if self._sock:
            try:
                self._sock.shutdown(socket.SHUT_RDWR)
            except:
                pass

    def _send(self, obj: dict) -> None:
        """Send JSON line."""
        if not self._sock:
            return
        line = json.dumps(obj).encode("utf-8") + b"\n"
        self._sock.sendall(line)

    def _recv(self) -> dict:
        """Receive one JSON line (blocking, on worker thread). Handles 32MB budget."""
        MAX_LINE = 32 * 1024 * 1024

        while b"\n" not in self._buf:
            if len(self._buf) > MAX_LINE:
                raise RuntimeError(f"line exceeds 32MB budget")

            chunk = self._sock.recv(65536)
            if not chunk:
                raise ConnectionError("socket closed")
            self._buf += chunk

        line, self._buf = self._buf.split(b"\n", 1)
        return json.loads(line.decode("utf-8"))
