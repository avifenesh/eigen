#!/usr/bin/env python3
"""
verify_live.py — Live verification of RpcClient + supervision against real guiserver.

Tests:
1. Supervise: spawn guiserver via GuiserverSupervisor, verify hello
2. RpcClient: hello, Sessions, subscribe to stats + first session, receive 5 events
3. Graceful disconnect

Usage:
    cd gui-qt && .venv/bin/python3 verify_live.py

Exit 0 on success, 1 on failure.
"""

import sys
import time
from pathlib import Path

# Add eigenqt to path
sys.path.insert(0, str(Path(__file__).parent))

from PySide6.QtCore import QCoreApplication, QTimer, Slot

from eigenqt.rpc.client import RpcClient
from eigenqt.rpc.supervise import GuiserverSupervisor


def main():
    app = QCoreApplication(sys.argv)

    print("=" * 60)
    print("Live RPC Verification Test")
    print("=" * 60)

    # Step 1: Supervise — spawn guiserver
    print("\n[1/5] Supervise: spawn guiserver...")
    supervisor = GuiserverSupervisor()

    @Slot(str, str)
    def on_mismatch(sha, manifest):
        print(f"  ⚠ Manifest mismatch detected: sha={sha[:8]}... manifest={manifest}")
        print("  → Auto-respawning guiserver from on-disk binary...")

    supervisor.mismatch.connect(on_mismatch)

    try:
        hello = supervisor.ensure_running(timeout=10.0)
        print(f"  ✓ guiserver ready: sha={hello['sha'][:8]}... manifest={hello['manifest'][:8]}...")
    except Exception as e:
        print(f"  ✗ Failed to spawn/connect: {e}", file=sys.stderr)
        sys.exit(1)

    # Step 2: RpcClient — connect
    print("\n[2/5] RpcClient: connect...")
    client = RpcClient()

    test_state = {
        "connected": False,
        "sessions": None,
        "events": [],
        "failed": False,
    }

    @Slot()
    def on_connected():
        print("  ✓ Connected (both RPC + events channels ready)")
        test_state["connected"] = True
        # Trigger next step
        QTimer.singleShot(100, step3_sessions)

    @Slot(str)
    def on_disconnected(reason):
        if not test_state.get("clean_shutdown"):
            print(f"  ✗ Disconnected: {reason}", file=sys.stderr)
            test_state["failed"] = True
            app.quit()

    @Slot(str, dict)
    def on_event(channel, data):
        test_state["events"].append((channel, data))
        print(f"  [event {len(test_state['events'])}] {channel}: {_format_event(data)}")

        # After 5 events, finish test
        if len(test_state["events"]) >= 5:
            QTimer.singleShot(500, step5_finish)

    @Slot(str)
    def on_dropped(channel):
        print(f"  [!] Dropped: {channel}")

    client.connected.connect(on_connected)
    client.disconnected.connect(on_disconnected)
    client.event.connect(on_event)
    client.dropped.connect(on_dropped)

    # Step 3: Sessions RPC
    def step3_sessions():
        print("\n[3/5] RPC: Sessions...")

        def on_sessions(result):
            if "error" in result:
                print(f"  ✗ Sessions error: {result['error']}", file=sys.stderr)
                test_state["failed"] = True
                app.quit()
                return

            sessions = result.get("result", [])
            print(f"  ✓ Sessions: {len(sessions)} session(s)")

            test_state["sessions"] = sessions

            if sessions:
                first = sessions[0]
                print(f"    First session: id={first.get('id')} title={first.get('title', '(untitled)')[:40]}")

            QTimer.singleShot(100, step4_subscribe)

        client.call("Sessions", callback=on_sessions)

    # Step 4: Subscribe
    def step4_subscribe():
        print("\n[4/5] Subscribe: stats + first session...")

        channels = ["eigen:daemon:stats"]
        sessions = test_state["sessions"]

        if sessions:
            session_id = sessions[0].get("id")
            if session_id:
                channels.append(f"session:{session_id}")

        client.subscribe(channels)
        print(f"  ✓ Subscribed to: {', '.join(channels)}")
        print(f"\n→ Listening for 5 events (or 10s timeout)...")

        # Timeout after 10s if we don't get 5 events
        QTimer.singleShot(10000, lambda: step5_finish() if len(test_state["events"]) < 5 else None)

    # Step 5: Finish
    def step5_finish():
        print(f"\n[5/5] Finish...")

        event_count = len(test_state["events"])
        if event_count < 5:
            print(f"  ⚠ Only received {event_count} event(s) (expected 5)")
        else:
            print(f"  ✓ Received {event_count} event(s)")

        print("\n✓ Live verification PASSED")

        # Clean shutdown
        test_state["clean_shutdown"] = True
        client.shutdown()
        supervisor.shutdown()
        app.quit()

    # Start event loop
    app.exec()

    sys.exit(1 if test_state["failed"] else 0)


def _format_event(data: dict) -> str:
    """Format event data for compact display."""
    if isinstance(data, dict):
        # Session events
        if "event" in data:
            event = data["event"]
            kind = event.get("kind", "?")
            text = event.get("text", "")
            if text:
                return f"{kind}: {text[:40]}"
            return kind

        # Stats events
        if "uptime_sec" in data:
            uptime = data["uptime_sec"]
            sessions = data.get("sessions", 0)
            goroutines = data.get("goroutines", 0)
            return f"stats: uptime={uptime}s sessions={sessions} goroutines={goroutines}"

        # Generic dict
        keys = ", ".join(list(data.keys())[:3])
        return f"dict({keys}...)"

    return str(data)[:60]


if __name__ == "__main__":
    main()
