#!/usr/bin/env python3
"""End-to-end verification of Qt chat parity after review fixes + dock."""
import sys
import time
import socket
import json
from pathlib import Path

def send_rpc(method: str, params) -> dict:
    """Send JSON-RPC to guiserver.sock."""
    sock_path = Path.home() / ".eigen" / "guiserver.sock"
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.connect(str(sock_path))
    req = {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
    msg = json.dumps(req).encode() + b"\n"
    sock.sendall(msg)

    # Read until we get a complete line
    resp = b""
    while b"\n" not in resp:
        chunk = sock.recv(4096)
        if not chunk:
            break
        resp += chunk

    sock.close()

    # Parse response
    resp_str = resp.decode().strip()
    if not resp_str:
        return {"error": "Empty response"}

    try:
        return json.loads(resp_str)
    except json.JSONDecodeError as e:
        return {"error": f"JSON decode error: {e}, response: {resp_str[:200]}"}

def main():
    print("=== End-to-End Verification ===\n")

    # 1. Create scratch session for testing
    print("1. Creating scratch session (dir=/home/avifenesh/projects/eigen)...")
    try:
        # NewSession signature: (dir, model, perm string)
        result = send_rpc("NewSession", [
            "/home/avifenesh/projects/eigen",  # dir
            "gpt-5",                            # model
            ""                                  # perm
        ])

        if "error" in result:
            print(f"   FAIL: {result['error']}")
            return 1

        session_id = result.get("result", "")
        print(f"   ✓ Created session: {session_id}")
    except Exception as e:
        print(f"   FAIL: {e}")
        return 1

    # 2. Send markdown message with table + code fence
    print("\n2. Sending markdown message (table + code fence)...")
    try:
        msg = """Here's a test message:

| Feature | Status |
|---------|--------|
| Tables  | ✓      |
| Code    | ✓      |

```python
def hello():
    return "world"
```
"""
        result = send_rpc("SendInput", [
            session_id,  # sessionID
            msg,         # text
            [],          # images
            []           # attachments
        ])
        if "error" in result:
            print(f"   FAIL: {result['error']}")
            return 1
        print("   ✓ Message sent")
    except Exception as e:
        print(f"   FAIL: {e}")
        return 1

    # 3. Create second scratch session for unread dot test
    print("\n3. Creating second scratch session for unread dot test...")
    try:
        result = send_rpc("NewSession", [
            "/home/avifenesh/projects/eigen",  # dir
            "gpt-5",                            # model
            ""                                  # perm
        ])

        if "error" in result:
            print(f"   FAIL: {result['error']}")
            return 1

        unread_session_id = result.get("result", "")
        print(f"   ✓ Created unread session: {unread_session_id}")
    except Exception as e:
        print(f"   FAIL: {e}")
        return 1

    # 4. Send message to unread session (should show blue dot)
    print("\n4. Sending message to unread session...")
    try:
        result = send_rpc("SendInput", [
            unread_session_id,
            "This should trigger unread dot",
            [],
            []
        ])
        if "error" in result:
            print(f"   FAIL: {result['error']}")
            return 1
        print("   ✓ Message sent to unread session")
        print("   → Blue dot should appear in sidebar")
    except Exception as e:
        print(f"   FAIL: {e}")
        return 1

    # 5. Check if git diff is available
    print("\n5. Checking git diff availability...")
    try:
        import subprocess
        result = subprocess.run(
            ["git", "diff", "HEAD"],
            cwd="/home/avifenesh/projects/eigen",
            capture_output=True,
            text=True
        )
        has_diff = len(result.stdout) > 0
        if has_diff:
            print(f"   ✓ Git diff available ({len(result.stdout)} chars)")
            print("   → Dock Diff tab should show changes")
        else:
            print("   ⚠ No git diff (working tree clean)")
    except Exception as e:
        print(f"   FAIL: {e}")
        return 1

    print("\n=== Manual Verification Checklist ===")
    print("□ App launched without warnings")
    print("□ Markdown table renders correctly")
    print("□ Code fence syntax highlighting works")
    print("□ Tool card expands/collapses")
    print("□ Slash popup shows and Enter selects")
    print("□ Model switch dropdown works")
    print("□ Session rename works")
    print("□ Dock toggle button works")
    print("□ Dock Diff tab shows real diff")
    print("□ Dock Files tab shows tree")
    print("□ File open in Files tab works")
    print("□ Blue dot appears on unread session")
    print("□ Blue dot clears when session opened")
    print(f"\nTest sessions created: {session_id}, {unread_session_id}")
    print("Remember to clean up with RemoveSession after testing!")

    return 0

if __name__ == "__main__":
    sys.exit(main())
