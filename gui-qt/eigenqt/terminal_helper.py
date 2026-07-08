"""Terminal output helper exposed to QML."""

import base64
import re

from PySide6.QtCore import QObject, QTimer, Signal, Slot


_ANSI_RE = re.compile(r"\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])")


class TerminalHelper(QObject):
    """Decode PTY event bytes for the native Qt terminal view."""

    terminalStarted = Signal(int, str, str)

    def __init__(self, parent: QObject | None = None) -> None:
        super().__init__(parent)
        self._token = 0
        self._cancelled: set[int] = set()

    @Slot(QObject, int, int, str, result=int)
    def startTerminal(self, rpc_client: QObject, cols: int, rows: int, work_dir: str) -> int:
        """Start a PTY and kill it if QML cancels before the id arrives."""
        self._token += 1
        token = self._token
        if rpc_client is None or not hasattr(rpc_client, "call"):
            QTimer.singleShot(0, lambda: self.terminalStarted.emit(token, "", "RPC client is unavailable."))
            return token

        def done(payload: object) -> None:
            payload = payload if isinstance(payload, dict) else {}
            error = str(payload.get("error", "") or "")
            terminal_id = str(payload.get("result", "") or "")
            cancelled = token in self._cancelled
            if cancelled:
                self._cancelled.discard(token)
                if terminal_id and hasattr(rpc_client, "callFire"):
                    rpc_client.callFire("TerminalKill", [terminal_id])
                elif terminal_id and hasattr(rpc_client, "call"):
                    rpc_client.call("TerminalKill", terminal_id)
                return
            self.terminalStarted.emit(token, terminal_id, error)

        try:
            rpc_client.call(
                "TerminalStart",
                int(cols),
                int(rows),
                work_dir or "",
                callback=lambda payload: QTimer.singleShot(0, lambda: done(payload)),
            )
        except Exception as exc:
            QTimer.singleShot(0, lambda: self.terminalStarted.emit(token, "", str(exc)))
        return token

    @Slot(int)
    def cancelTerminalStart(self, token: int) -> None:
        if token > 0:
            self._cancelled.add(int(token))

    @Slot(str, result=str)
    def decodeData(self, data: str) -> str:
        """Decode base64 UTF-8 terminal data and remove common ANSI controls."""
        if not data:
            return ""
        try:
            text = base64.b64decode(data, validate=False).decode("utf-8", "replace")
        except Exception:
            return ""
        text = _ANSI_RE.sub("", text)
        return text.replace("\r\n", "\n").replace("\r", "\n")
