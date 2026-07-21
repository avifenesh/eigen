"""Theme-aware colors for Qt rich-text markdown surfaces."""

from dataclasses import dataclass


@dataclass(frozen=True)
class MarkdownPalette:
    inline_background: str
    inline_text: str
    link: str
    syntax_background: str
    syntax_text: str
    syntax_keyword: str
    syntax_type: str
    syntax_function: str
    syntax_string: str
    syntax_number: str
    syntax_comment: str
    syntax_punctuation: str
    syntax_builtin: str
    error: str


PALETTES = {
    "deepteal": MarkdownPalette(
        inline_background="#1d282c",
        inline_text="#69c2b8",
        link="#6fb7e8",
        syntax_background="#11171a",
        syntax_text="#dde4e3",
        syntax_keyword="#c58fd8",
        syntax_type="#e0b36a",
        syntax_function="#6fb7e8",
        syntax_string="#8fc98a",
        syntax_number="#e8a878",
        syntax_comment="#71807c",
        syntax_punctuation="#9ab0ac",
        syntax_builtin="#69c2b8",
        error="#c06a5e",
    ),
    "studio": MarkdownPalette(
        inline_background="#edf1ef",
        inline_text="#0d7167",
        link="#286b91",
        syntax_background="#f0f3f1",
        syntax_text="#24312e",
        syntax_keyword="#783c90",
        syntax_type="#875b0e",
        syntax_function="#1f668d",
        syntax_string="#356f43",
        syntax_number="#9c4f28",
        syntax_comment="#697773",
        syntax_punctuation="#50605c",
        syntax_builtin="#0d7167",
        error="#af4339",
    ),
    "nord": MarkdownPalette(
        inline_background="#2b3140",
        inline_text="#8fbcbb",
        link="#88c0d0",
        syntax_background="#171b22",
        syntax_text="#d8dee9",
        syntax_keyword="#b48ead",
        syntax_type="#ebcb8b",
        syntax_function="#88c0d0",
        syntax_string="#a3be8c",
        syntax_number="#d08770",
        syntax_comment="#616e88",
        syntax_punctuation="#9aa5b8",
        syntax_builtin="#8fbcbb",
        error="#bf616a",
    ),
    "gruvbox": MarkdownPalette(
        inline_background="#3c3836",
        inline_text="#8ec07c",
        link="#83a598",
        syntax_background="#1d2021",
        syntax_text="#ebdbb2",
        syntax_keyword="#fb4934",
        syntax_type="#fabd2f",
        syntax_function="#8ec07c",
        syntax_string="#b8bb26",
        syntax_number="#d3869b",
        syntax_comment="#928374",
        syntax_punctuation="#a89984",
        syntax_builtin="#8ec07c",
        error="#fb4934",
    ),
}


def palette_for(theme: str) -> MarkdownPalette:
    return PALETTES.get(theme, PALETTES["deepteal"])
