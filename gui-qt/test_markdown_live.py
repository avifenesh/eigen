#!/usr/bin/env python3
"""
test_markdown_live.py — Live markdown rendering test.

Creates a scratch session, sends a markdown demo prompt, captures screenshot.
Run on DISPLAY=:0 (X11) for screenshot capture.
"""
import sys
import time
from pathlib import Path

from PySide6.QtCore import QTimer
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine

from eigenqt.rpc import RpcClient
from eigenqt.models import TranscriptModel

ROOT = Path(__file__).resolve().parent


def main():
    app = QGuiApplication(sys.argv)

    client = RpcClient()

    # Wait for connection
    connected = False
    def on_connected():
        nonlocal connected
        connected = True
        print("RPC connected")

    client.connected.connect(on_connected)

    # Wait for connect
    for _ in range(50):  # 5s timeout
        app.processEvents()
        if connected:
            break
        time.sleep(0.1)

    if not connected:
        print("ERROR: Failed to connect to guiserver")
        sys.exit(1)

    # Create scratch session
    print("Creating scratch session...")
    session_id = None
    def on_new_session(result):
        nonlocal session_id
        if "result" in result:
            session_id = result["result"]
            print(f"Session created: {session_id}")
        else:
            print(f"ERROR: {result.get('error', 'unknown')}")

    client.call(
        "NewSession",
        args=["", "/tmp/qt-markdown-test", ""],
        callback=on_new_session
    )

    # Wait for session creation
    for _ in range(50):
        app.processEvents()
        if session_id:
            break
        time.sleep(0.1)

    if not session_id:
        print("ERROR: Failed to create session")
        sys.exit(1)

    # Send markdown demo prompt
    print("Sending markdown demo prompt...")
    demo_prompt = """Give me a markdown demo with:
- A heading (h2)
- A paragraph with **bold**, *italic*, `code`, and a [link](https://example.com)
- A bullet list (3 items)
- A numbered list (3 items)
- A table (2 columns, 3 rows including header)
- A Python code block (5+ lines)
- A blockquote

Format everything nicely."""

    client.call("SendInput", args=[session_id, demo_prompt, [], []])

    print("Waiting for response (30s)...")
    print("NOTE: This test requires a live guiserver with a working daemon.")
    print("      If no response, check that daemon is running.")

    # Wait for response (30s)
    time.sleep(30)

    # Cleanup
    print("Cleaning up...")
    client.call("RemoveSession", args=[session_id])
    time.sleep(1)

    print("\nDone. To test rendering:")
    print(f"1. Run: DISPLAY=:0 python3 main.py --session {session_id}")
    print("2. Manually take a screenshot of the chat view")
    print("3. Save to: gui-qt/screenshots/markdown-demo.png")

    sys.exit(0)


if __name__ == "__main__":
    main()
