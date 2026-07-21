"""
markdown — Markdown parsing + syntax highlighting for Qt chat.

Converts markdown text → list[Block] (para|code|heading|list|table|quote|hr).
Inline formatting → Qt rich-text HTML (bold/italic/code/link).
Code blocks → Pygments-highlighted Qt HTML with active-palette token colors.
Optimized for streaming: parse re-run on growing text (16ms flush); < 5ms target.
"""

from .pipeline import Block, parse

__all__ = ["parse", "Block"]
