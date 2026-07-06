"""
supervise.py — guiserver supervision (find-or-spawn, auto-respawn on manifest mismatch).

Implements the auto-respawn discipline from plan §2: try connect; if no socket, spawn
guiserver from configured binary path; poll hello up to 10s; compare hello manifest vs
on-disk binary freshness; on mismatch, kill and respawn once; toast/log on mismatch.

Usage:
    supervisor = GuiserverSupervisor()
    supervisor.mismatch.connect(lambda sha, manifest: print(f"respawn: {sha} {manifest}"))

    # Attempt connect/spawn (blocks until hello or timeout)
    try:
        hello = supervisor.ensure_running(timeout=10.0)
        print(f"guiserver ready: sha={hello['sha']}, manifest={hello['manifest']}")
    except TimeoutError:
        print("guiserver did not respond to hello within 10s")
    except RuntimeError as e:
        print(f"guiserver spawn/connect failed: {e}")
"""

import json
import os
import signal
import shutil
import socket
import subprocess
import time
from pathlib import Path
from typing import Any, Optional

from PySide6.QtCore import QObject, Signal


class GuiserverSupervisor(QObject):
    """Find-or-spawn guiserver with auto-respawn on manifest mismatch."""

    # Signals
    mismatch = Signal(str, str)  # (sha, manifest) — emitted on mismatch before respawn

    def __init__(self, parent: Optional[QObject] = None):
        super().__init__(parent)

        # Binary path discovery
        self.binary_path = self._find_binary_path()
        self.sock_path = Path.home() / ".eigen" / "guiserver.sock"

        # Manifest expectation (computed from on-disk binary)
        self._expected_manifest: Optional[str] = None

        # Spawn tracking
        self._proc: Optional[subprocess.Popen] = None
        self._respawn_attempted = False

    def _find_binary_path(self) -> Path:
        """Discover eigen binary path (EIGEN_BIN env or ../bin/eigen sibling)."""
        if "EIGEN_BIN" in os.environ:
            return Path(os.environ["EIGEN_BIN"]).expanduser()

        # Repo checkout: gui-qt/eigenqt/rpc/supervise.py -> ../../.. -> repo.
        repo_binary = Path(__file__).resolve().parents[3] / "bin" / "eigen"
        if repo_binary.exists():
            return repo_binary

        resolved = shutil.which("eigen")
        if resolved:
            return Path(resolved)

        # Preserve the old fallback for clearer error text in _spawn_guiserver.
        return Path("eigen")

    def ensure_running(self, timeout: float = 10.0) -> dict[str, Any]:
        """
        Ensure guiserver is running and responsive. Returns hello payload.

        Lifecycle:
        1. Try connect to existing socket
        2. If no socket, spawn guiserver from binary_path
        3. Poll hello (up to timeout)
        4. Compare hello.manifest vs on-disk binary expectation
        5. On mismatch: kill, respawn once, re-check (if still mismatch, raise)

        Raises TimeoutError if guiserver doesn't respond within timeout.
        Raises RuntimeError on spawn/connect failure or persistent mismatch.
        """
        # Step 1: try connect to existing socket
        if self.sock_path.exists():
            try:
                hello = self._try_hello(timeout=2.0)
                return self._check_manifest(hello)
            except (socket.error, ConnectionError, TimeoutError):
                # Socket exists but not responsive — remove stale socket
                self.sock_path.unlink(missing_ok=True)

        # Step 2: spawn guiserver
        self._spawn_guiserver()

        # Step 3: poll hello
        hello = self._poll_hello(timeout)

        # Step 4: check manifest
        return self._check_manifest(hello)

    def _spawn_guiserver(self) -> None:
        """Spawn guiserver subprocess (detached, new session)."""
        if not self.binary_path.exists():
            raise RuntimeError(f"eigen binary not found at {self.binary_path}")

        # Spawn detached (start_new_session isolates from parent's signal group)
        self._proc = subprocess.Popen(
            [str(self.binary_path), "guiserver"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )

    def _poll_hello(self, timeout: float) -> dict[str, Any]:
        """Poll for hello response (blocking, up to timeout)."""
        deadline = time.time() + timeout
        last_error = None

        while time.time() < deadline:
            try:
                hello = self._try_hello(timeout=1.0)
                return hello
            except Exception as e:
                last_error = e
                time.sleep(0.2)

        raise TimeoutError(f"guiserver did not respond to hello within {timeout}s: {last_error}")

    def _try_hello(self, timeout: float) -> dict[str, Any]:
        """Attempt single hello RPC call. Raises on failure."""
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.settimeout(timeout)
        sock.connect(str(self.sock_path))

        try:
            # Declare role
            self._send(sock, {"role": "rpc"})

            # Call hello
            self._send(sock, {"id": 1, "call": "hello", "args": []})

            # Recv response
            resp = self._recv(sock)

            if "error" in resp:
                raise RuntimeError(f"hello error: {resp['error']}")

            result = resp.get("result", {})
            if "sha" not in result or "manifest" not in result:
                raise RuntimeError(f"hello missing sha/manifest: {result}")

            return result

        finally:
            sock.close()

    def _check_manifest(self, hello: dict[str, Any]) -> dict[str, Any]:
        """
        Compare hello manifest vs on-disk binary expectation.
        On mismatch: emit signal, kill, respawn once, re-check.
        Returns hello payload if OK; raises RuntimeError on persistent mismatch.
        """
        hello_sha = hello["sha"]
        hello_manifest = hello["manifest"]

        # Compute expected manifest from the source manifest (cached).
        if self._expected_manifest is None:
            self._expected_manifest = self._compute_expected_manifest()

        # Packaged/installed runs may not have the source manifest next to the
        # Python files. In that case, do not kill a responsive guiserver just
        # because we cannot locally recompute the build-time manifest hash.
        if not self._expected_manifest:
            self._respawn_attempted = False
            return hello

        # If match, we're done
        if hello_manifest == self._expected_manifest:
            self._respawn_attempted = False  # reset for next cycle
            return hello

        # Mismatch detected
        self.mismatch.emit(hello_sha, hello_manifest)

        # If we already respawned once, give up (binary on disk is stale)
        if self._respawn_attempted:
            raise RuntimeError(
                f"guiserver manifest mismatch persists after respawn:\n"
                f"  running: {hello_manifest}\n"
                f"  on-disk: {self._expected_manifest}\n"
                f"Binary on disk is stale — run 'make' to rebuild."
            )

        # Respawn once
        self._respawn_attempted = True
        self._kill_running_guiserver()
        self._spawn_guiserver()

        # Re-check
        hello = self._poll_hello(timeout=10.0)
        return self._check_manifest(hello)

    def _compute_expected_manifest(self) -> Optional[str]:
        """
        Compute the expected manifest hash using the same FNV-1a contract as
        internal/gui/manifest.go. The guiserver's hello response embeds the
        hash of internal/gui/bridge.manifest.json, not a binary freshness proxy.
        """
        manifest_path = (
            Path(__file__).resolve().parents[3]
            / "internal"
            / "gui"
            / "bridge.manifest.json"
        )
        if not manifest_path.exists():
            return None

        h = 14695981039346656037
        for b in manifest_path.read_bytes():
            h ^= b
            h = (h * 1099511628211) & 0xFFFFFFFFFFFFFFFF
        return f"{h:016x}"

    def _kill_running_guiserver(self) -> None:
        """
        Kill running guiserver process.

        Strategy:
        1. If we spawned it (_proc is set), SIGTERM that pid
        2. Otherwise, find pid via socket (fuser/lsof) and SIGTERM

        Limitation: if we didn't spawn it and fuser/lsof aren't available,
        we can't kill it cheaply. In that case, the respawn will fail because
        the socket is still held. Document this as a known limitation.
        """
        if self._proc:
            try:
                self._proc.terminate()
                self._proc.wait(timeout=5.0)
            except:
                pass
            self._proc = None
            return

        # Try fuser to find pid holding the socket
        try:
            result = subprocess.run(
                ["fuser", str(self.sock_path)],
                capture_output=True,
                text=True,
                timeout=2.0,
            )
            if result.returncode == 0:
                pid_str = result.stdout.strip()
                if pid_str.isdigit():
                    pid = int(pid_str)
                    os.kill(pid, signal.SIGTERM)
                    time.sleep(0.5)  # give it a moment
                    return
        except:
            pass

        # Fallback: remove socket and hope for the best
        self.sock_path.unlink(missing_ok=True)

    def shutdown(self) -> None:
        """Gracefully shut down supervisor (does NOT kill guiserver — it lingers per plan)."""
        # Plan specifies 5-min linger, so we don't kill on Qt exit
        self._proc = None

    @staticmethod
    def _send(sock: socket.socket, obj: dict) -> None:
        """Send JSON line."""
        line = json.dumps(obj).encode("utf-8") + b"\n"
        sock.sendall(line)

    @staticmethod
    def _recv(sock: socket.socket) -> dict:
        """Receive one JSON line."""
        buf = b""
        while b"\n" not in buf:
            chunk = sock.recv(4096)
            if not chunk:
                raise ConnectionError("socket closed")
            buf += chunk

        line, _ = buf.split(b"\n", 1)
        return json.loads(line.decode("utf-8"))
