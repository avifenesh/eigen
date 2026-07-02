#!/usr/bin/env bash
# verify_markdown_render.sh — Verify markdown rendering in Qt GUI (manual test)
#
# Prerequisites:
# 1. guiserver is running with a working daemon
# 2. DISPLAY=:0 (X11/Wayland with xdg-desktop-portal or similar screenshot support)
#
# Steps:
# 1. Creates a scratch session
# 2. Sends a markdown demo prompt
# 3. Launches Qt GUI to view the rendered markdown
# 4. User manually takes a screenshot (use GUI screenshot tool or scrot/gnome-screenshot)
# 5. Save screenshot to screenshots/markdown-demo.png

set -e

cd "$(dirname "$0")"

VENV=".venv/bin/python3"

# Create scratch session + send markdown demo
echo "Creating scratch session..."
SESSION_ID=$($VENV -c "
import sys
from eigenqt.rpc import RpcClient
client = RpcClient()
import time
connected = False
def on_connected():
    global connected
    connected = True
client.connected.connect(on_connected)
# Wait for connect
for _ in range(50):
    from PySide6.QtCore import QCoreApplication
    QCoreApplication.processEvents()
    if connected:
        break
    time.sleep(0.1)
if not connected:
    print('ERROR: Failed to connect', file=sys.stderr)
    sys.exit(1)
# Create session
session_id = None
def on_new_session(result):
    global session_id
    if 'result' in result:
        session_id = result['result']
client.call('NewSession', args=['', '/tmp/qt-markdown-test', ''], callback=on_new_session)
for _ in range(50):
    QCoreApplication.processEvents()
    if session_id:
        break
    time.sleep(0.1)
print(session_id)
")

if [ -z "$SESSION_ID" ]; then
    echo "ERROR: Failed to create session"
    exit 1
fi

echo "Session created: $SESSION_ID"

# Send markdown demo prompt
echo "Sending markdown demo prompt..."
$VENV -c "
from eigenqt.rpc import RpcClient
import time
client = RpcClient()
connected = False
def on_connected():
    global connected
    connected = True
client.connected.connect(on_connected)
for _ in range(50):
    from PySide6.QtCore import QCoreApplication
    QCoreApplication.processEvents()
    if connected:
        break
    time.sleep(0.1)
prompt = '''Give me a markdown demo with:
- A heading (h2)
- A paragraph with **bold**, *italic*, \`code\`, and a [link](https://example.com)
- A bullet list (3 items)
- A numbered list (3 items)
- A table (2 columns, 3 rows including header)
- A Python code block (5+ lines)
- A blockquote

Format everything nicely.'''
client.call('SendInput', args=['$SESSION_ID', prompt, [], []])
time.sleep(1)
"

echo ""
echo "Waiting 10s for model response..."
sleep 10

echo ""
echo "Now launching Qt GUI..."
echo "Once the GUI opens:"
echo "  1. Verify markdown rendering (code blocks, tables, lists, etc.)"
echo "  2. Take a screenshot (use your OS screenshot tool)"
echo "  3. Save to: gui-qt/screenshots/markdown-demo.png"
echo ""
echo "Session ID: $SESSION_ID"
echo ""

# Launch Qt GUI with the session
DISPLAY=:0 $VENV main.py --session "$SESSION_ID"
