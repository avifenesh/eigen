#!/bin/bash
# Quick script to take a screenshot of Live view with a working session

set -e

SCREENSHOT_DIR="screenshots"
mkdir -p "$SCREENSHOT_DIR"

echo "Creating a scratch session for screenshot..."

# Create a session that will run briefly
SESSION_ID=$(python3 << 'PYEOF'
import json
import socket
from pathlib import Path

sock_path = Path.home() / ".eigen" / "guiserver.sock"
sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.connect(str(sock_path))

# NewSession RPC
req = {"id": 1, "method": "NewSession", "params": ["", "", ""]}
sock.sendall((json.dumps(req) + "\n").encode())

resp = sock.recv(65536).decode().strip()
result = json.loads(resp)
session_id = result.get("result", "")

if session_id:
    print(session_id)
else:
    print("ERROR: No session ID", file=sys.stderr)
    sys.exit(1)

sock.close()
PYEOF
)

if [ -z "$SESSION_ID" ]; then
    echo "Failed to create session"
    exit 1
fi

echo "Created session: $SESSION_ID"

# Send a slow prompt to make it "working"
echo "Sending a prompt to make the session working..."
python3 << PYEOF
import json
import socket
from pathlib import Path
import sys

session_id = "$SESSION_ID"
sock_path = Path.home() / ".eigen" / "guiserver.sock"
sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.connect(str(sock_path))

# SendInput RPC with a slow task
req = {
    "id": 2,
    "method": "SendInput",
    "params": [session_id, "Count to 10 slowly, one number per second. Use bash sleep 1 between each number."]
}
sock.sendall((json.dumps(req) + "\n").encode())

resp = sock.recv(4096).decode().strip()
print("Sent input:", resp[:100])
sock.close()
PYEOF

echo "Waiting 2 seconds for session to start working..."
sleep 2

# TODO: Actually launch GUI and screenshot
# For now, just document that we created a working session
echo "Working session created: $SESSION_ID"
echo "To screenshot manually:"
echo "  1. Launch: cd gui-qt && source .venv/bin/activate && python main.py"
echo "  2. Click 'Live' button in left rail"
echo "  3. You should see the working session in the list"
echo "  4. Take a screenshot"

echo ""
echo "Cleaning up session in 10 seconds..."
sleep 10

python3 << PYEOF
import json
import socket
from pathlib import Path

session_id = "$SESSION_ID"
sock_path = Path.home() / ".eigen" / "guiserver.sock"
sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.connect(str(sock_path))

req = {"id": 3, "method": "RemoveSession", "params": [session_id]}
sock.sendall((json.dumps(req) + "\n").encode())
sock.close()
print("Session removed")
PYEOF
