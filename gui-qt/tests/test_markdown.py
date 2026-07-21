"""
test_markdown.py — pytest tests for markdown pipeline.

Tests:
- Fenced code block (with lang)
- Nested lists (bullet + ordered)
- Table
- Streaming-truncated markdown mid-fence (must not crash)
- Paragraphs with inline formatting (bold/italic/code/links)
- Headings (h1-h3)
- Blockquote
- Horizontal rule
"""

import pytest
from eigenqt.markdown import parse
from eigenqt.markdown.highlight import clear_cache, highlight


def test_fenced_code_block():
    """Fenced code block with lang."""
    text = """Here's some code:

```python
def hello():
    print("world")
```

Done."""
    blocks = parse(text)
    assert len(blocks) == 3
    assert blocks[0].type == "para"
    assert "Here's some code" in blocks[0].content
    assert blocks[1].type == "code"
    assert blocks[1].lang == "python"
    assert 'def hello():' in blocks[1].source
    assert blocks[2].type == "para"
    assert "Done" in blocks[2].content


def test_nested_lists():
    """Bullet and ordered lists."""
    text = """Shopping list:

- Apples
- Bananas
- Cherries

Steps:

1. First step
2. Second step
3. Third step"""
    blocks = parse(text)
    assert len(blocks) == 4
    assert blocks[0].type == "para"
    assert blocks[1].type == "list"
    assert blocks[1].ordered is False
    assert len(blocks[1].items) == 3
    assert "Apples" in blocks[1].items[0]
    assert blocks[2].type == "para"
    assert blocks[3].type == "list"
    assert blocks[3].ordered is True
    assert len(blocks[3].items) == 3
    assert "First step" in blocks[3].items[0]


def test_table():
    """Table (GFM-style)."""
    text = """| Name | Age |
|------|-----|
| Alice | 30 |
| Bob | 25 |"""
    blocks = parse(text)
    assert len(blocks) == 1
    assert blocks[0].type == "table"
    assert len(blocks[0].rows) == 3  # header + 2 data rows
    assert blocks[0].rows[0] == ["Name", "Age"]
    assert blocks[0].rows[1] == ["Alice", "30"]
    assert blocks[0].rows[2] == ["Bob", "25"]


def test_streaming_truncated_mid_fence():
    """Streaming-truncated markdown mid-fence (must not crash)."""
    text = """Here's code:

```python
def hello():
    print("w"""
    blocks = parse(text)
    assert len(blocks) == 2
    assert blocks[0].type == "para"
    assert blocks[1].type == "code"
    assert blocks[1].lang == "python"
    assert 'def hello():' in blocks[1].source


def test_inline_formatting():
    """Paragraphs with inline formatting (bold/italic/code/links)."""
    text = """This is **bold** and *italic* and `code` and [a link](https://example.com)."""
    blocks = parse(text)
    assert len(blocks) == 1
    assert blocks[0].type == "para"
    html = blocks[0].content
    assert "<b>bold</b>" in html
    assert "<i>italic</i>" in html
    assert "<code" in html and "code</code>" in html
    assert '<a href="https://example.com"' in html
    assert "a link</a>" in html


@pytest.mark.parametrize(
    ("theme", "background", "code", "link"),
    [
        ("studio", "#eef1f4", "#086b63", "#236b9a"),
        ("deepteal", "#1d282c", "#69c2b8", "#6fb7e8"),
        ("nord", "#2b3140", "#8fbcbb", "#88c0d0"),
        ("gruvbox", "#3c3836", "#8ec07c", "#83a598"),
    ],
)
def test_inline_formatting_uses_the_active_palette(theme, background, code, link):
    html = parse("Use `code` and [docs](https://example.com).", theme)[0].content

    assert f"background-color: {background}" in html
    assert f"color: {code}" in html
    assert f"color: {link}" in html


@pytest.mark.parametrize(
    ("theme", "keyword", "number"),
    [
        ("studio", "#694597", "#a3432b"),
        ("deepteal", "#c58fd8", "#e8a878"),
        ("nord", "#b48ead", "#d08770"),
        ("gruvbox", "#fb4934", "#d3869b"),
    ],
)
def test_syntax_highlighting_uses_the_active_palette(theme, keyword, number):
    clear_cache()
    html = highlight("python", "def answer():\n    return 42\n", theme).lower()

    assert keyword in html
    assert number in html


def test_headings():
    """Headings (h1-h3)."""
    text = """# Heading 1
## Heading 2
### Heading 3"""
    blocks = parse(text)
    assert len(blocks) == 3
    assert blocks[0].type == "heading"
    assert blocks[0].level == 1
    assert "Heading 1" in blocks[0].content
    assert blocks[1].type == "heading"
    assert blocks[1].level == 2
    assert "Heading 2" in blocks[1].content
    assert blocks[2].type == "heading"
    assert blocks[2].level == 3
    assert "Heading 3" in blocks[2].content


def test_blockquote():
    """Blockquote."""
    text = """> This is a quote.
> It spans multiple lines."""
    blocks = parse(text)
    assert len(blocks) == 1
    assert blocks[0].type == "quote"
    assert "This is a quote" in blocks[0].content


def test_horizontal_rule():
    """Horizontal rule."""
    text = """Before

---

After"""
    blocks = parse(text)
    assert len(blocks) == 3
    assert blocks[0].type == "para"
    assert blocks[1].type == "hr"
    assert blocks[2].type == "para"


def test_empty_text():
    """Empty text → no blocks."""
    blocks = parse("")
    assert len(blocks) == 0


def test_mixed_content():
    """Mixed content (para + code + list + table)."""
    text = """# Demo

Here's a paragraph.

```python
print("hi")
```

- Item 1
- Item 2

| Col1 | Col2 |
|------|------|
| A    | B    |"""
    blocks = parse(text)
    assert len(blocks) == 5
    assert blocks[0].type == "heading"
    assert blocks[1].type == "para"
    assert blocks[2].type == "code"
    assert blocks[3].type == "list"
    assert blocks[4].type == "table"


def test_streaming_performance():
    """Streaming performance: parse 10k-char message (target <5ms)."""
    import time
    text = "Here's a long paragraph. " * 400  # ~10k chars
    start = time.perf_counter()
    blocks = parse(text)
    elapsed = (time.perf_counter() - start) * 1000  # ms
    assert len(blocks) == 1
    assert blocks[0].type == "para"
    print(f"\nParse time for 10k chars: {elapsed:.2f}ms")
    # Target: <5ms (print for measurement, don't assert — CPU-dependent)
