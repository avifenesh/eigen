#!/usr/bin/env python3
"""
test_client_mock.py — Test RpcClient against a mock guiserver (in-process).

Spawns a mock guiserver in a background thread, tests RpcClient against it.

Usage:
    cd gui-qt && .venv/bin/python3 test_client_mock.py

Exit 0 on success, 1 on failure.
"""

import json
import os
import socket
import sys
import threading
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from PySide6.QtCore import QCoreApplication, QTimer, Slot

from eigenqt.rpc.client import RpcClient


class MockGuiserver:
    """Mock guiserver for testing (runs in background thread)."""

    def __init__(self, sock_path: Path):
        self.sock_path = sock_path
        self.server_sock: socket.socket | None = None
        self.running = False
        self.thread: threading.Thread | None = None

    def start(self):
        """Start mock server in background thread."""
        self.sock_path.unlink(missing_ok=True)
        self.server_sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.server_sock.bind(str(self.sock_path))
        self.server_sock.listen(5)
        self.server_sock.settimeout(1.0)

        self.running = True
        self.thread = threading.Thread(target=self._run, daemon=True)
        self.thread.start()

    def stop(self):
        """Stop mock server."""
        self.running = False
        if self.thread:
            self.thread.join(timeout=2.0)
        if self.server_sock:
            self.server_sock.close()
        self.sock_path.unlink(missing_ok=True)

    def _run(self):
        """Server loop: accept connections and handle them."""
        while self.running:
            try:
                client_sock, _ = self.server_sock.accept()
                threading.Thread(target=self._handle_conn, args=(client_sock,), daemon=True).start()
            except socket.timeout:
                continue
            except Exception as e:
                if self.running:
                    print(f"Mock server error: {e}", file=sys.stderr)
                break

    def _handle_conn(self, sock: socket.socket):
        """Handle one client connection."""
        buf_holder = [b""]  # mutable container for buffer

        try:
            # Read role declaration
            msg = self._recv_json(sock, buf_holder)
            role = msg.get("role")

            if role == "rpc":
                self._handle_rpc(sock, buf_holder)
            elif role == "events":
                self._handle_events(sock, buf_holder)
            else:
                print(f"Unknown role: {role}", file=sys.stderr)

        except Exception as e:
            pass  # connection closed or error
        finally:
            sock.close()

    def _handle_rpc(self, sock: socket.socket, buf_holder: list):
        """Handle RPC connection: respond to hello and Sessions."""
        while self.running:
            try:
                msg = self._recv_json(sock, buf_holder)
                req_id = msg.get("id")
                call = msg.get("call")

                if call == "hello":
                    resp = {
                        "id": req_id,
                        "result": {
                            "sha": "abc123deadbeef",
                            "manifest": "manifest123",
                        },
                    }
                    self._send_json(sock, resp)

                elif call == "Sessions":
                    resp = {
                        "id": req_id,
                        "result": [
                            {"id": "sess1", "title": "Test Session 1"},
                            {"id": "sess2", "title": "Test Session 2"},
                        ],
                    }
                    self._send_json(sock, resp)

                else:
                    resp = {"id": req_id, "error": f"unknown method: {call}"}
                    self._send_json(sock, resp)

            except Exception as e:
                break

    def _handle_events(self, sock: socket.socket, buf_holder: list):
        """Handle events connection: send periodic stats events."""
        subscribed_channels = []
        event_lock = threading.Lock()

        # Spawn background thread to send periodic events
        def send_events():
            event_count = 0
            time.sleep(0.5)  # wait for subscription

            while self.running and event_count < 10:
                with event_lock:
                    channels = list(subscribed_channels)

                if "eigen:daemon:stats" in channels:
                    event = {
                        "event": "data",
                        "channel": "eigen:daemon:stats",
                        "data": {
                            "uptime_sec": event_count * 5,
                            "sessions": 2,
                            "goroutines": 42,
                        },
                    }
                    try:
                        self._send_json(sock, event)
                        event_count += 1
                    except Exception as e:
                        print(f"Event send error: {e}", file=sys.stderr)
                        break

                time.sleep(0.2)

        threading.Thread(target=send_events, daemon=True).start()

        # Read subscription messages
        sock.settimeout(5.0)
        while self.running:
            try:
                msg = self._recv_json(sock, buf_holder)

                if "sub" in msg:
                    channels = msg["sub"]
                    with event_lock:
                        subscribed_channels.extend(channels)
                    print(f"  [mock] Subscribed to: {channels}")

                elif "unsub" in msg:
                    channels = msg["unsub"]
                    with event_lock:
                        for ch in channels:
                            if ch in subscribed_channels:
                                subscribed_channels.remove(ch)

            except socket.timeout:
                continue
            except Exception as e:
                break

    def _recv_json(self, sock: socket.socket, buf_holder: list) -> dict:
        """Receive one JSON line (buf_holder is a mutable container)."""
        buf = buf_holder[0]

        while b"\n" not in buf:
            chunk = sock.recv(4096)
            if not chunk:
                raise ConnectionError("socket closed")
            buf += chunk

        line, buf = buf.split(b"\n", 1)
        buf_holder[0] = buf
        return json.loads(line.decode("utf-8"))

    def _send_json(self, sock: socket.socket, obj: dict):
        """Send JSON line."""
        line = json.dumps(obj).encode("utf-8") + b"\n"
        sock.sendall(line)


def main():
    app = QCoreApplication(sys.argv)

    print("=" * 60)
    print("RpcClient Mock Test")
    print("=" * 60)

    sock_path = Path("/tmp/test_guiserver.sock")

    # Start mock server
    print("\n[1/4] Starting mock guiserver...")
    server = MockGuiserver(sock_path)
    server.start()
    time.sleep(0.2)  # let server start
    print("  ✓ Mock server running")

    # Create client
    print("\n[2/4] RpcClient connecting...")
    client = RpcClient(sock_path=sock_path)

    test_state = {
        "connected": False,
        "sessions_ok": False,
        "events": [],
        "failed": False,
    }

    @Slot()
    def on_connected():
        print("  ✓ Connected (both channels ready)")
        test_state["connected"] = True
        QTimer.singleShot(100, step3_rpc)

    @Slot(str)
    def on_disconnected(reason):
        if not test_state.get("clean_shutdown"):
            print(f"  ✗ Disconnected: {reason}", file=sys.stderr)
            test_state["failed"] = True
            app.quit()

    @Slot(str, dict)
    def on_event(channel, data):
        test_state["events"].append((channel, data))
        print(f"  [event {len(test_state['events'])}] {channel}: uptime={data.get('uptime_sec', '?')}")

        if len(test_state["events"]) >= 5:
            QTimer.singleShot(200, step4_finish)

    client.connected.connect(on_connected)
    client.disconnected.connect(on_disconnected)
    client.event.connect(on_event)

    def step3_rpc():
        print("\n[3/4] RPC: Sessions...")

        def on_sessions(result):
            if "error" in result:
                print(f"  ✗ Sessions error: {result['error']}", file=sys.stderr)
                test_state["failed"] = True
                app.quit()
                return

            sessions = result.get("result", [])
            print(f"  ✓ Sessions: {len(sessions)} session(s)")
            test_state["sessions_ok"] = True

            # Subscribe to stats
            client.subscribe(["eigen:daemon:stats"])
            print("  ✓ Subscribed to stats")
            print("\n→ Listening for 5 events...")

        client.call("Sessions", callback=on_sessions)

    def step4_finish():
        print(f"\n[4/4] Finish...")
        print(f"  ✓ Received {len(test_state['events'])} event(s)")

        # Verify results
        if not test_state["connected"]:
            print("  ✗ Never connected", file=sys.stderr)
            test_state["failed"] = True

        if not test_state["sessions_ok"]:
            print("  ✗ Sessions RPC failed", file=sys.stderr)
            test_state["failed"] = True

        if len(test_state["events"]) < 5:
            print(f"  ✗ Only {len(test_state['events'])} events", file=sys.stderr)
            test_state["failed"] = True

        if test_state["failed"]:
            print("\n✗ Test FAILED")
        else:
            print("\n✓ Test PASSED")

        # Clean shutdown
        test_state["clean_shutdown"] = True
        client.shutdown()
        server.stop()
        app.quit()

    # Timeout after 5s
    QTimer.singleShot(5000, step4_finish)

    app.exec()

    sys.exit(1 if test_state["failed"] else 0)


if __name__ == "__main__":
    main()
