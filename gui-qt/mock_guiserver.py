#!/usr/bin/env python3
"""
mock_guiserver.py — Minimal mock guiserver for Qt shell verification.

Implements the wire protocol with fake sessions + streaming events.
Run this before launching main.py to enable live testing.
"""
import asyncio
import json
import os
import socket
from datetime import datetime, timedelta
from pathlib import Path


class MockGuiserver:
    """Mock guiserver with fake sessions + events."""

    def __init__(self, sock_path: Path):
        self.sock_path = sock_path
        self.sessions = self._build_fake_sessions()

    def _build_fake_sessions(self):
        """Generate fake sessions."""
        now = datetime.now()
        return [
            {
                "id": "sess-001",
                "title": "refactor-router",
                "dir": "/home/avifenesh/projects/eigen",
                "model": "sonnet-5",
                "status": "working",
                "turns": 8,
                "updated": (now - timedelta(minutes=2)).isoformat(),
            },
            {
                "id": "sess-002",
                "title": "fix/feed-suggest-dedup",
                "dir": "/home/avifenesh/projects/eigen",
                "model": "sonnet-5",
                "status": "idle",
                "turns": 5,
                "updated": (now - timedelta(minutes=14)).isoformat(),
            },
            {
                "id": "sess-003",
                "title": "gui-icon-flow",
                "dir": "/home/avifenesh/projects/eigen-blueprints",
                "model": "opus-4.6",
                "status": "error",
                "turns": 12,
                "updated": (now - timedelta(minutes=38)).isoformat(),
            },
        ]

    async def serve(self):
        """Serve guiserver socket."""
        if self.sock_path.exists():
            self.sock_path.unlink()

        server = await asyncio.start_unix_server(self.handle_client, str(self.sock_path))
        print(f"Mock guiserver listening on {self.sock_path}")

        async with server:
            await server.serve_forever()

    async def handle_client(self, reader, writer):
        """Handle a single client connection."""
        print("Client connected")

        # Wait for role declaration
        line = await reader.readline()
        if not line:
            writer.close()
            await writer.wait_closed()
            return

        msg = json.loads(line.decode())
        role = msg.get("role")

        if role == "rpc":
            await self.handle_rpc(reader, writer)
        elif role == "events":
            await self.handle_events(reader, writer)
        else:
            writer.close()
            await writer.wait_closed()

    async def handle_rpc(self, reader, writer):
        """Handle RPC connection."""
        print("RPC connection established")

        try:
            while True:
                line = await reader.readline()
                if not line:
                    break

                req = json.loads(line.decode())
                call = req.get("call")
                args = req.get("args", [])
                req_id = req.get("id")

                result = await self.handle_rpc_call(call, args)
                resp = {"id": req_id, "result": result}
                writer.write((json.dumps(resp) + "\n").encode())
                await writer.drain()
        except Exception as e:
            print(f"RPC error: {e}")
        finally:
            writer.close()
            await writer.wait_closed()

    async def handle_rpc_call(self, call: str, args: list):
        """Handle a single RPC call."""
        if call == "hello":
            return {"sha": "abcd1234", "manifest": "mock"}

        elif call == "Sessions":
            return self.sessions

        elif call == "State":
            session_id = args[0] if args else "sess-001"
            return self._fake_state(session_id)

        elif call == "SendInput":
            print(f"SendInput: {args}")
            return None

        elif call == "Interrupt":
            print(f"Interrupt: {args}")
            return None

        elif call == "Approve":
            print(f"Approve: {args}")
            return None

        else:
            print(f"Unknown RPC call: {call}")
            return None

    def _fake_state(self, session_id: str):
        """Generate fake session state."""
        return {
            "id": session_id,
            "transcript": [
                {
                    "role": "user",
                    "text": "Can you refactor the session-cache module to use atomic pointer swap?",
                },
                {
                    "role": "assistant",
                    "text": "Sure, I'll swap the mutex for a lock-free atomic pointer. Here's the core:\n\n```go\nfunc (c *Cache) Swap(next *Snapshot) {\n    old := atomic.SwapPointer(&c.ptr, unsafe.Pointer(next))\n    _ = (*Snapshot)(old)\n}\n```",
                },
                {
                    "role": "tool",
                    "tool_name": "run_tests",
                    "tool_status": "success",
                    "text": "$ go test ./internal/sessioncache/... -race\nok\t0.412s\n42 passed",
                },
                {
                    "role": "assistant",
                    "text": "Tests pass — 42/42, race detector clean.",
                },
            ],
            "pending": [],
        }

    async def handle_events(self, reader, writer):
        """Handle events connection."""
        print("Events connection established")

        subscriptions = set()

        try:
            while True:
                line = await reader.readline()
                if not line:
                    break

                msg = json.loads(line.decode())
                if "sub" in msg:
                    subscriptions.update(msg["sub"])
                    print(f"Subscribed: {subscriptions}")

                    # Send initial daemon stats
                    if "eigen:daemon:stats" in subscriptions:
                        event = {
                            "event": "data",
                            "channel": "eigen:daemon:stats",
                            "data": {"online": True},
                        }
                        writer.write((json.dumps(event) + "\n").encode())
                        await writer.drain()

                    # Send fake streaming events for session channels
                    for sub in subscriptions:
                        if sub.startswith("session:"):
                            await self.send_fake_stream(writer, sub)

                elif "unsub" in msg:
                    subscriptions -= set(msg["unsub"])
                    print(f"Unsubscribed: {msg['unsub']}")

        except Exception as e:
            print(f"Events error: {e}")
        finally:
            writer.close()
            await writer.wait_closed()

    async def send_fake_stream(self, writer, channel: str):
        """Send fake streaming events to a session channel."""
        # Simulate a short assistant response
        chunks = [
            "I'll ",
            "check ",
            "the ",
            "code ",
            "now.",
        ]

        for i, chunk in enumerate(chunks):
            event = {
                "event": "data",
                "channel": channel,
                "data": {
                    "event": {"text": {"delta": chunk}},
                    "replay": False,
                    "seq": i + 1,
                },
            }
            writer.write((json.dumps(event) + "\n").encode())
            await writer.drain()
            await asyncio.sleep(0.2)

        # Send done event
        event = {
            "event": "data",
            "channel": channel,
            "data": {
                "event": {"done": {}},
                "replay": False,
                "seq": len(chunks) + 1,
            },
        }
        writer.write((json.dumps(event) + "\n").encode())
        await writer.drain()


async def main():
    sock_path = Path.home() / ".eigen" / "guiserver.sock"
    sock_path.parent.mkdir(parents=True, exist_ok=True)

    server = MockGuiserver(sock_path)
    await server.serve()


if __name__ == "__main__":
    asyncio.run(main())
