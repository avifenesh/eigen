"""
clipboard_helper.py — Qt clipboard helper (exposed to QML as context property).

Provides copyText(text: str) for code block copy buttons.
Provides pasteImage() -> str for image paste (returns base64 data URL).
"""

import base64
from io import BytesIO

from PySide6.QtCore import QObject, Slot
from PySide6.QtGui import QGuiApplication, QImage


class ClipboardHelper(QObject):
    """Qt clipboard helper (exposes copyText + pasteImage to QML)."""

    @Slot(str)
    def copyText(self, text: str):
        """Copy text to system clipboard."""
        clipboard = QGuiApplication.clipboard()
        clipboard.setText(text)

    @Slot(result=str)
    def pasteImage(self) -> str:
        """
        Paste image from clipboard, return base64 data URL (or "" if no image).

        Returns: base64-encoded PNG data (just the data part, no "data:image/png;base64," prefix).
        """
        clipboard = QGuiApplication.clipboard()
        image = clipboard.image()
        if image.isNull():
            return ""

        # Convert QImage to PNG bytes
        buffer = BytesIO()
        image.save(buffer, "PNG")  # type: ignore
        png_bytes = buffer.getvalue()

        # Encode to base64
        b64 = base64.b64encode(png_bytes).decode("utf-8")
        return b64
