"""
highlight.py — Pygments syntax highlighting → Qt rich-text HTML.

Uses Pygments HTML formatter with inline styles (noclasses=True).
Deep-teal control-surface color scheme (matches the QML syntax tokens).
Cache highlighted results by (lang, source-hash) for streaming performance.
"""

import hashlib
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


class GraphiteStyle(Style):
    """
    Pygments style matching the default Qt syntax tokens.
    """

    background_color = "#11171a"  # --syn-bg
    default_style = "#dde4e3"  # --syn-text

    styles = {
        Comment: "#71807c italic",  # --syn-comment
        Keyword: "#c58fd8 bold",  # --syn-keyword
        Keyword.Namespace: "#c58fd8",
        Keyword.Type: "#e0b36a",  # --syn-type
        Name.Class: "#e0b36a",  # --syn-type
        Name.Function: "#6fb7e8",  # --syn-func
        Name.Builtin: "#69c2b8",  # --syn-builtin
        Name.Decorator: "#c58fd8",
        String: "#8fc98a",  # --syn-string
        Number: "#e8a878",  # --syn-number
        Operator: "#9ab0ac",  # --syn-punct
        Text: "#dde4e3",
        Whitespace: "#dde4e3",
        Error: "#c06a5e",  # --error
    }


# Highlight cache: (lang, source_hash) → HTML
_highlight_cache: Dict[Tuple[str, str], str] = {}


def highlight(lang: str, source: str) -> str:
    """
    Highlight source code → Qt rich-text HTML.

    Uses Pygments with GraphiteStyle. Cache by (lang, hash(source)) for streaming.
    Returns HTML <div> with inline styles (no CSS classes).
    """
    # Cache key: (lang, hash of source)
    source_hash = hashlib.sha256(source.encode("utf-8")).hexdigest()[:16]
    cache_key = (lang, source_hash)

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
        html = f'<pre style="margin: 0; padding: 0; font-family: monospace; color: #dde4e3;">{escaped}</pre>'
        _highlight_cache[cache_key] = html
        return html

    # Pygments HTML formatter with inline styles
    formatter = HtmlFormatter(
        style=GraphiteStyle,
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
