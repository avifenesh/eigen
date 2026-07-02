"""
highlighter_helper.py — Pygments highlighter helper (exposed to QML as context property).

Provides highlight(lang: str, source: str) -> str for code blocks.
"""

from PySide6.QtCore import QObject, Slot

from eigenqt.markdown.highlight import highlight as pygments_highlight


class HighlighterHelper(QObject):
    """Pygments highlighter helper (exposes highlight to QML)."""

    @Slot(str, str, result=str)
    def highlight(self, lang: str, source: str) -> str:
        """Highlight source code → Qt rich-text HTML."""
        return pygments_highlight(lang, source)
