#!/usr/bin/env python3
"""
proof.py — guiserver protocol proof-of-concept and permanent debug tool

Usage:
    python3 proof.py                    # hello + sessions + subscribe + events
    python3 proof.py --send "text"      # also send input to first session

Connects to ~/.eigen/guiserver.sock with two connections (RPC + events),
runs hello (prints SHA + manifest hash), lists sessions (prints count + first
title), subscribes to stats + first live session if any, prints up to 10
events with 15s timeout. With --send, also sends input to the first session.

This script is a permanent debug tool for guiserver protocol development and
troubleshooting. Uses only Python 3 stdlib (no external deps).

Wire protocol (newline-delimited JSON, 32MB max line):
  - First message declares role: {"role":"rpc"} or {"role":"events"}
  - RPC conn: request/reply with id correlation
      → {"id":N,"call":"MethodName","args":[...]}
      ← {"id":N,"result":...} | {"id":N,"error":"..."}
  - Events conn: subscription + push-only stream
      → {"sub":["chan1","chan2",...]} | {"unsub":["chan1",...]}
      ← {"event":"data","channel":"<name>","data":...}
      ← {"event":"dropped","channel":"<name>"}

Exit codes:
    0 = success
    1 = connection/protocol error
    2 = no sessions found (when trying to subscribe)
"""

import json
import os
import socket
import sys
import time
from pathlib import Path


def main():
    import argparse
    parser = argparse.ArgumentParser(description='guiserver protocol proof-of-concept')
    parser.add_argument('--send', metavar='TEXT', help='send input to first session')
    args = parser.parse_args()

    sock_path = Path.home() / '.eigen' / 'guiserver.sock'

    print(f"Connecting to guiserver at {sock_path}...")

    # Open RPC connection
    rpc_sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        rpc_sock.connect(str(sock_path))
    except (FileNotFoundError, ConnectionRefusedError) as e:
        print(f"✗ Cannot connect: {e}", file=sys.stderr)
        print(f"  Is guiserver running? Start it with: go run . guiserver", file=sys.stderr)
        sys.exit(1)

    rpc_sock.settimeout(5.0)
    rpc = JSONLineConn(rpc_sock)

    # Open events connection
    events_sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    events_sock.connect(str(sock_path))
    events_sock.settimeout(15.0)  # longer timeout for events (may be idle)
    events = JSONLineConn(events_sock)

    try:
        # Declare roles
        rpc.send({"role": "rpc"})
        events.send({"role": "events"})

        # RPC: hello
        rpc.send({"id": 1, "call": "hello", "args": []})
        hello_resp = rpc.recv()
        if "error" in hello_resp:
            print(f"✗ hello error: {hello_resp['error']}", file=sys.stderr)
            sys.exit(1)

        result = hello_resp.get("result", {})
        sha = result.get("sha", "unknown")
        manifest = result.get("manifest", "unknown")
        print(f"✓ hello: sha={sha[:8]}... manifest={manifest}")

        # RPC: Sessions
        rpc.send({"id": 2, "call": "Sessions", "args": []})
        sessions_resp = rpc.recv()
        if "error" in sessions_resp:
            print(f"✗ Sessions error: {sessions_resp['error']}", file=sys.stderr)
            sys.exit(1)

        sessions = sessions_resp.get("result", [])
        print(f"✓ Sessions: {len(sessions)} session(s)")

        if not sessions:
            print("  (no sessions to subscribe to)")
            sys.exit(0)

        first_session = sessions[0]
        session_id = first_session.get("id")
        session_title = first_session.get("title", "(untitled)")
        print(f"  First session: id={session_id} title={session_title:.40}")

        # Subscribe to stats + first session
        channels = ["eigen:daemon:stats"]
        if session_id:
            # Session events map to "session:<id>"
            channels.append(f"session:{session_id}")

        events.send({"sub": channels})
        print(f"✓ Subscribed to: {', '.join(channels)}")

        # Optional: send input
        if args.send and session_id:
            print(f"\n→ SendInput: {args.send:.40}")
            rpc.send({"id": 3, "call": "SendInput", "args": [session_id, args.send, [], None]})
            send_resp = rpc.recv()
            if "error" in send_resp:
                print(f"✗ SendInput error: {send_resp['error']}", file=sys.stderr)
            else:
                print("✓ SendInput: OK")

        # Read events (up to 10, 15s timeout)
        print("\n→ Listening for events (up to 10, 15s timeout)...\n")
        event_count = 0
        max_events = 10

        try:
            while event_count < max_events:
                msg = events.recv()

                event_type = msg.get("event")
                channel = msg.get("channel")

                if event_type == "data":
                    data = msg.get("data", {})
                    print(f"  [{event_count+1}] {channel}: {format_event_data(data)}")
                    event_count += 1
                elif event_type == "dropped":
                    print(f"  [!] {channel}: (dropped — queue overflow)")
                else:
                    print(f"  [?] Unknown event: {msg}")

        except socket.timeout:
            print(f"\n  (timeout after {event_count} event(s))")

        print(f"\n✓ Proof test PASSED ({event_count} event(s) received)")

    finally:
        rpc_sock.close()
        events_sock.close()


class JSONLineConn:
    """Newline-delimited JSON over a socket."""

    def __init__(self, sock):
        self.sock = sock
        self.buf = b""

    def send(self, obj):
        line = json.dumps(obj).encode('utf-8') + b'\n'
        self.sock.sendall(line)

    def recv(self):
        while b'\n' not in self.buf:
            chunk = self.sock.recv(4096)
            if not chunk:
                raise ConnectionError("socket closed")
            self.buf += chunk

        line, self.buf = self.buf.split(b'\n', 1)
        return json.loads(line.decode('utf-8'))


def format_event_data(data):
    """Format event data for compact display."""
    if isinstance(data, dict):
        # Session events
        if "event" in data:
            event = data["event"]
            kind = event.get("kind", "?")
            text = event.get("text", "")
            if text:
                return f"{kind}: {text:.50}"
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
