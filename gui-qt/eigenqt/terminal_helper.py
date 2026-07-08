"""Terminal output helper exposed to QML."""

import base64
import re

from PySide6.QtCore import QObject, Slot


_ANSI_RE = re.compile(r"\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])")


class TerminalHelper(QObject):
    """Decode PTY event bytes for the native Qt terminal view."""

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
