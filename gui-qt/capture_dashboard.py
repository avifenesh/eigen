#!/usr/bin/env python3
"""Capture a live Dashboard() response for test fixtures."""
import json
import os
import socket
import sys
import uuid

def call_rpc(method: str, params=None):
    """Call guiserver RPC and return result."""
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        sock.connect(os.path.expanduser("~/.eigen/guiserver.sock"))
    except FileNotFoundError:
        print("guiserver.sock not found — is guiserver running?", file=sys.stderr)
        sys.exit(1)

    req_id = str(uuid.uuid4())
    req = {"id": req_id, "method": method}
    if params:
        req["params"] = params

    sock.sendall((json.dumps(req) + "\n").encode("utf-8"))

    # Read response line
    buf = b""
    while b"\n" not in buf:
        chunk = sock.recv(4096)
        if not chunk:
            sock.close()
            print("Connection closed before newline", file=sys.stderr)
            sys.exit(1)
        buf += chunk

    sock.close()

    line = buf.split(b"\n", 1)[0]
    if not line:
        print("Empty response", file=sys.stderr)
        sys.exit(1)

    resp = json.loads(line.decode("utf-8"))
    if "error" in resp:
        print(f"RPC error: {resp['error']}", file=sys.stderr)
        sys.exit(1)

    return resp.get("result")

if __name__ == "__main__":
    dashboard = call_rpc("Dashboard")
    print(json.dumps(dashboard, indent=2))
