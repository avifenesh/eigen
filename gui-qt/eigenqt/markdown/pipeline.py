"""
pipeline.py — Markdown text → flat list[Block] (parse is pure, pytest-able).

Block types: para|code|heading|list|table|quote|hr
Inline formatting → Qt rich-text HTML subset (bold/italic/code-span/links).
Code blocks carry {lang, source} for Pygments highlighting (done by highlight.py).
Tables carry rows as list[list[str]] (cells).

STREAMING: parse is re-run on growing text per 16ms flush (from TranscriptModel).
Must be fast for 10k-char messages (target <5ms; measure first, cache only if needed).
"""

from dataclasses import dataclass, field
from typing import Literal
from markdown_it import MarkdownIt

from .palette import MarkdownPalette, palette_for


@dataclass
class Block:
    """A single markdown block (para, code, heading, list, table, quote, hr)."""

    type: Literal["para", "code", "heading", "list", "table", "quote", "hr"]
    content: str = ""  # HTML for para/heading; plain text for others
    lang: str = ""  # code block language
    source: str = ""  # code block source (unhighlighted)
    level: int = 0  # heading level (1-6)
    rows: list[list[str]] = field(default_factory=list)  # table rows (list of cells)
    items: list[str] = field(default_factory=list)  # list items (HTML per item)
    ordered: bool = False  # list ordered flag


def parse(text: str, theme: str = "deepteal") -> list[Block]:
    """
    Parse markdown text → flat list[Block].

    Inline formatting in paragraphs/list-items is converted to Qt rich-text HTML:
    - **bold** → <b>bold</b>
    - *italic* → <i>italic</i>
    - `code` → <code style="...">code</code>
    - [link](url) → <a href="url">link</a>

    Code blocks produce {type: "code", lang, source} for Pygments highlighting.
    Tables produce {type: "table", rows: [[cell, ...], ...]}.
    Lists produce {type: "list", items: [...], ordered: bool}.
    """
    palette = palette_for(theme)

    # Use commonmark preset + enable tables, strikethrough (but disable linkify)
    md = MarkdownIt("commonmark").enable(["table", "strikethrough"])
    tokens = md.parse(text)

    blocks: list[Block] = []
    i = 0
    while i < len(tokens):
        token = tokens[i]

        if token.type == "paragraph_open":
            # Paragraph: para_open → inline → para_close
            inline_token = tokens[i + 1] if i + 1 < len(tokens) else None
            if inline_token and inline_token.type == "inline":
                html = _render_inline(inline_token, palette)
                blocks.append(Block(type="para", content=html))
            i += 3  # skip para_open, inline, para_close

        elif token.type == "heading_open":
            # Heading: heading_open → inline → heading_close
            level = int(token.tag[1])  # h1 → 1, h2 → 2, etc.
            inline_token = tokens[i + 1] if i + 1 < len(tokens) else None
            if inline_token and inline_token.type == "inline":
                html = _render_inline(inline_token, palette)
                blocks.append(Block(type="heading", content=html, level=level))
            i += 3

        elif token.type == "fence":
            # Code block (fenced)
            lang = token.info.strip() if token.info else ""
            source = token.content
            blocks.append(Block(type="code", lang=lang, source=source))
            i += 1

        elif token.type == "code_block":
            # Code block (indented)
            source = token.content
            blocks.append(Block(type="code", lang="", source=source))
            i += 1

        elif token.type == "bullet_list_open" or token.type == "ordered_list_open":
            # List: list_open → (list_item_open → paragraph_open → inline → para_close → list_item_close)* → list_close
            ordered = token.type == "ordered_list_open"
            items = []
            j = i + 1
            while j < len(tokens) and tokens[j].type != "bullet_list_close" and tokens[j].type != "ordered_list_close":
                if tokens[j].type == "list_item_open":
                    # Find inline token inside list item
                    k = j + 1
                    while k < len(tokens) and tokens[k].type != "list_item_close":
                        if tokens[k].type == "inline":
                            html = _render_inline(tokens[k], palette)
                            items.append(html)
                            break
                        k += 1
                    # Skip to list_item_close
                    while j < len(tokens) and tokens[j].type != "list_item_close":
                        j += 1
                j += 1
            blocks.append(Block(type="list", items=items, ordered=ordered))
            i = j + 1  # skip past list_close

        elif token.type == "table_open":
            # Table: table_open → thead_open → tr_open → (th_open → inline → th_close)* → tr_close → thead_close → tbody_open → (tr_open → (td_open → inline → td_close)* → tr_close)* → tbody_close → table_close
            rows: list[list[str]] = []
            j = i + 1
            current_row: list[str] = []
            while j < len(tokens) and tokens[j].type != "table_close":
                if tokens[j].type == "inline":
                    # Cell content
                    html = _render_inline(tokens[j], palette)
                    current_row.append(html)
                elif tokens[j].type == "tr_close":
                    if current_row:
                        rows.append(current_row)
                        current_row = []
                j += 1
            if current_row:
                rows.append(current_row)
            blocks.append(Block(type="table", rows=rows))
            i = j + 1

        elif token.type == "blockquote_open":
            # Blockquote: blockquote_open → paragraph_open → inline → para_close → blockquote_close
            content_parts = []
            j = i + 1
            while j < len(tokens) and tokens[j].type != "blockquote_close":
                if tokens[j].type == "inline":
                    html = _render_inline(tokens[j], palette)
                    content_parts.append(html)
                j += 1
            html = "<br>".join(content_parts)
            blocks.append(Block(type="quote", content=html))
            i = j + 1

        elif token.type == "hr":
            blocks.append(Block(type="hr"))
            i += 1

        else:
            # Skip unknown tokens
            i += 1

    return blocks


def _render_inline(inline_token, palette: MarkdownPalette) -> str:
    """
    Render inline token children → Qt rich-text HTML.

    Handles: strong, em, code_inline, link_open/link_close, text.
    """
    html_parts = []
    children = inline_token.children or []
    i = 0
    while i < len(children):
        child = children[i]

        if child.type == "text":
            # Escape HTML entities
            escaped = (
                child.content
                .replace("&", "&amp;")
                .replace("<", "&lt;")
                .replace(">", "&gt;")
            )
            html_parts.append(escaped)

        elif child.type == "strong_open":
            html_parts.append("<b>")
        elif child.type == "strong_close":
            html_parts.append("</b>")

        elif child.type == "em_open":
            html_parts.append("<i>")
        elif child.type == "em_close":
            html_parts.append("</i>")

        elif child.type == "code_inline":
            # Inline code uses the same raised petrol surface as code blocks.
            escaped = (
                child.content
                .replace("&", "&amp;")
                .replace("<", "&lt;")
                .replace(">", "&gt;")
            )
            html_parts.append(
                f'<code style="background-color: {palette.inline_background}; '
                f'color: {palette.inline_text}; '
                f'padding: 2px 4px; border-radius: 3px; '
                f'font-family: monospace;">{escaped}</code>'
            )

        elif child.type == "link_open":
            href = child.attrGet("href") or ""
            html_parts.append(
                f'<a href="{href}" style="color: {palette.link}; '
                f'text-decoration: none;">'
            )
        elif child.type == "link_close":
            html_parts.append("</a>")

        elif child.type == "softbreak" or child.type == "hardbreak":
            html_parts.append("<br>")

        i += 1

    return "".join(html_parts)
