#!/usr/bin/env python3
"""Debug script to check Sessions RPC response."""
import socket
import json
import os

def read_json_line(sock):
    """Read a single JSON line from socket."""
    buf = b""
    while b"\n" not in buf:
        chunk = sock.recv(1024)
        if not chunk:
            raise EOFError("Socket closed")
        buf += chunk
    line = buf.split(b"\n", 1)[0]
    return json.loads(line.decode())

sock_path = os.path.expanduser("~/.eigen/guiserver.sock")
sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.connect(sock_path)

# Send hello
hello_req = {"method": "hello", "params": [], "id": 1}
sock.sendall((json.dumps(hello_req) + "\n").encode())
hello_resp = read_json_line(sock)
print("Hello response:", hello_resp.get("result", {}).get("manifest_hash", "")[:16])

# Send Sessions request
sessions_req = {"method": "Sessions", "params": [], "id": 2}
sock.sendall((json.dumps(sessions_req) + "\n").encode())
sessions_resp = read_json_line(sock)

print("\nSessions response:")
if "result" in sessions_resp and len(sessions_resp["result"]) > 0:
    print(f"Found {len(sessions_resp['result'])} sessions")
    print(f"\nFirst session keys: {list(sessions_resp['result'][0].keys())}")
    print(f"\nFirst session data:")
    print(json.dumps(sessions_resp["result"][0], indent=2))
else:
    print("No sessions or error:", sessions_resp)

sock.close()
