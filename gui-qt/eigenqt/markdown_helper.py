"""
markdown_helper.py — Markdown parser helper (exposed to QML as context property).

Provides parse(text: str) -> list[dict] for rendering markdown in QML.
"""

from PySide6.QtCore import QObject, Slot

from eigenqt.markdown.pipeline import parse


class MarkdownHelper(QObject):
    """Markdown parser helper (exposes parse to QML)."""

    @Slot(str, result="QVariantList")
    def parse(self, text: str) -> list[dict]:
        """
        Parse markdown text → list of block dicts for QML.

        Each block is a dict with type + type-specific fields:
        - {type: "para", content: html}
        - {type: "code", lang: str, source: str}
        - {type: "heading", content: html, level: int}
        - {type: "list", items: [html...], ordered: bool}
        - {type: "table", rows: [[html...]...]}
        - {type: "quote", content: html}
        - {type: "hr"}
        """
        blocks = parse(text)
        result = []
        for block in blocks:
            d = {"type": block.type}
            if block.content:
                d["content"] = block.content
            if block.lang:
                d["lang"] = block.lang
            if block.source:
                d["source"] = block.source
            if block.level:
                d["level"] = block.level
            if block.rows:
                d["rows"] = block.rows
            if block.items:
                d["items"] = block.items
            if block.ordered:
                d["ordered"] = block.ordered
            result.append(d)
        return result
