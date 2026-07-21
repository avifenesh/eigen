"""
highlight.py — Pygments syntax highlighting → Qt rich-text HTML.

Uses Pygments HTML formatter with inline styles (noclasses=True).
The active Qt palette supplies the syntax roles.
Cache highlighted results by (theme, lang, source-hash) for streaming performance.
"""

import hashlib
from functools import lru_cache
from typing import Dict, Tuple

from pygments import highlight as pygments_highlight
from pygments.formatters import HtmlFormatter
from pygments.lexers import get_lexer_by_name, guess_lexer
from pygments.style import Style
from pygments.token import (
    Comment,
    Error,
    Keyword,
    Name,
    Number,
    Operator,
    String,
    Text,
    Whitespace,
)

from .palette import MarkdownPalette, palette_for


@lru_cache(maxsize=3)
def _style_for(palette: MarkdownPalette) -> type[Style]:
    class PaletteStyle(Style):
        background_color = palette.syntax_background
        default_style = palette.syntax_text
        styles = {
            Comment: f"{palette.syntax_comment} italic",
            Keyword: f"{palette.syntax_keyword} bold",
            Keyword.Namespace: palette.syntax_keyword,
            Keyword.Type: palette.syntax_type,
            Name.Class: palette.syntax_type,
            Name.Function: palette.syntax_function,
            Name.Builtin: palette.syntax_builtin,
            Name.Decorator: palette.syntax_keyword,
            String: palette.syntax_string,
            Number: palette.syntax_number,
            Operator: palette.syntax_punctuation,
            Text: palette.syntax_text,
            Whitespace: palette.syntax_text,
            Error: palette.error,
        }

    return PaletteStyle


# Highlight cache: (theme, lang, source_hash) → HTML
_highlight_cache: Dict[Tuple[str, str, str], str] = {}


def highlight(lang: str, source: str, theme: str = "deepteal") -> str:
    """
    Highlight source code → Qt rich-text HTML.

    Uses Pygments with the active theme. Cache by theme, language, and source.
    Returns HTML <div> with inline styles (no CSS classes).
    """
    # Cache key: (lang, hash of source)
    source_hash = hashlib.sha256(source.encode("utf-8")).hexdigest()[:16]
    palette = palette_for(theme)
    cache_key = (theme, lang, source_hash)

    if cache_key in _highlight_cache:
        return _highlight_cache[cache_key]

    # Get lexer (fallback to text if unknown)
    try:
        if lang:
            lexer = get_lexer_by_name(lang, stripall=False)
        else:
            lexer = guess_lexer(source)
    except Exception:
        # Fallback: plain text (no highlighting)
        escaped = (
            source
            .replace("&", "&amp;")
            .replace("<", "&lt;")
            .replace(">", "&gt;")
        )
        html = (
            '<pre style="margin: 0; padding: 0; font-family: monospace; '
            f'color: {palette.syntax_text};">{escaped}</pre>'
        )
        _highlight_cache[cache_key] = html
        return html

    # Pygments HTML formatter with inline styles
    formatter = HtmlFormatter(
        style=_style_for(palette),
        noclasses=True,
        nowrap=True,  # No <div class="highlight"> wrapper
        prestyles="margin: 0; padding: 0; background-color: transparent;",
    )

    html = pygments_highlight(source, lexer, formatter)

    # Cache result
    _highlight_cache[cache_key] = html
    return html


def clear_cache():
    """Clear highlight cache (for testing or memory cleanup)."""
    _highlight_cache.clear()
