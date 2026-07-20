"""
highlight.py — Pygments syntax highlighting → Qt rich-text HTML.

Uses Pygments HTML formatter with inline styles (noclasses=True).
Graphite control-surface color scheme (matches the QML --syn-* colors).
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
    Graphite Pygments style matching the Qt syntax tokens.
    """

    background_color = "#11161b"  # --syn-bg
    default_style = "#d5dfdc"  # --syn-text

    styles = {
        Comment: "#71807c italic",  # --syn-comment
        Keyword: "#d6a2ed bold",  # --syn-keyword
        Keyword.Namespace: "#d6a2ed",
        Keyword.Type: "#f2b867",  # --syn-type
        Name.Class: "#f2b867",  # --syn-type
        Name.Function: "#8bb9ff",  # --syn-func
        Name.Builtin: "#5bd6c2",  # --syn-builtin
        Name.Decorator: "#d6a2ed",
        String: "#a6da7a",  # --syn-string
        Number: "#efa979",  # --syn-number
        Operator: "#acbbb7",  # --syn-punct
        Text: "#d5dfdc",
        Whitespace: "#d5dfdc",
        Error: "#ff9382",  # --error
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
        html = f'<pre style="margin: 0; padding: 0; font-family: monospace; color: #c7d2d0;">{escaped}</pre>'
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
