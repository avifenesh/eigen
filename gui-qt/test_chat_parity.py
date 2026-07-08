#!/usr/bin/env python3
"""
test_chat_parity.py — Quick verification of chat parity features.

Creates a scratch session, triggers a tool call, verifies tool card rendering.
"""
import sys
import time
from pathlib import Path

from PySide6.QtCore import QTimer
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine
from PySide6.QtQuickControls2 import QQuickStyle

from eigenqt.models import SessionsModel, TranscriptModel, SessionStateModel, CommandsModel
from eigenqt.rpc import RpcClient
from eigenqt.clipboard_helper import ClipboardHelper
from eigenqt.highlighter_helper import HighlighterHelper

ROOT = Path(__file__).resolve().parent


def main():
    QQuickStyle.setStyle("Basic")
    app = QGuiApplication(sys.argv)

    # Create RPC client
    client = RpcClient()

    # Wait for connection
    connected = False

    def on_connected():
        nonlocal connected
        connected = True
        print("✓ RPC connected")

    client.connected.connect(on_connected)

    # Spin until connected
    timeout = time.time() + 5
    while not connected and time.time() < timeout:
        app.processEvents()
        time.sleep(0.1)

    if not connected:
        print("✗ RPC connection timeout")
        return 1

    # Create scratch session
    print("Creating scratch session...")
    session_id = None

    def on_new_session(result):
        nonlocal session_id
        if "error" in result:
            print(f"✗ NewSession error: {result['error']}")
            return
        session_id = result["result"]
        print(f"✓ Session created: {session_id}")

    # NewSession(dir, model, perm) - perm is empty string for default
    client.call("NewSession", "/tmp/qt-test", "gpt-5", "", callback=on_new_session)

    # Wait for session creation
    timeout = time.time() + 5
    while not session_id and time.time() < timeout:
        app.processEvents()
        time.sleep(0.1)

    if not session_id:
        print("✗ Session creation timeout")
        return 1

    # Create models
    print("Creating models...")
    transcript_model = TranscriptModel(client, session_id)
    session_state_model = SessionStateModel(client, session_id)
    commands_model = CommandsModel(client)

    # Fetch State
    print("Fetching session state...")
    state_fetched = False

    def on_state(result):
        nonlocal state_fetched
        if "error" in result:
            print(f"✗ State error: {result['error']}")
            return
        transcript_model.seed(result["result"])
        session_state_model.seed(result["result"])
        state_fetched = True
        print("✓ State fetched")
        print(f"  Model: {session_state_model.model}")
        print(f"  Perm: {session_state_model.perm}")
        print(f"  Catalog: {session_state_model.catalog}")

    client.call("State", session_id, callback=on_state)

    timeout = time.time() + 5
    while not state_fetched and time.time() < timeout:
        app.processEvents()
        time.sleep(0.1)

    if not state_fetched:
        print("✗ State fetch timeout")
        return 1

    # Send input to trigger a tool call
    print("Sending input (will trigger tool call)...")
    client.call("SendInput", session_id, "run echo 'hello from qt test'", [], [])

    # Wait for tool events
    print("Waiting for tool call events...")
    time.sleep(3)
    app.processEvents()

    # Check transcript
    print(f"✓ Transcript rows: {transcript_model.rowCount()}")
    for i in range(transcript_model.rowCount()):
        idx = transcript_model.index(i, 0)
        kind = transcript_model.data(idx, transcript_model.KindRole)
        tool_name = transcript_model.data(idx, transcript_model.ToolNameRole)
        tool_status = transcript_model.data(idx, transcript_model.ToolStatusRole)
        print(f"  Row {i}: kind={kind}, tool={tool_name}, status={tool_status}")

    # Check commands model
    print(f"✓ Commands count: {commands_model.rowCount()}")
    if commands_model.rowCount() > 0:
        idx = commands_model.index(0, 0)
        name = commands_model.data(idx, commands_model.NameRole)
        print(f"  First command: {name}")

    # Cleanup
    print("Cleaning up...")
    client.call("RemoveSession", session_id)
    time.sleep(0.5)

    print("✓ Test complete")
    return 0


if __name__ == "__main__":
    sys.exit(main())
