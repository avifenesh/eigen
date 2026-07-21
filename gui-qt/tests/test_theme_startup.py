import json
import os
import subprocess
import sys
import textwrap
from pathlib import Path

import main
import pytest

from eigenqt.markdown.palette import palette_for


ROOT = Path(__file__).resolve().parents[1]


def test_resolve_qt_theme_reads_only_known_config_values(tmp_path):
    config = tmp_path / "config.json"
    config.write_text(json.dumps({"theme": "nord"}))
    assert main.resolve_qt_theme(config) == "nord"

    config.write_text(json.dumps({"theme": "studio"}))
    assert main.resolve_qt_theme(config) == "studio"

    config.write_text(json.dumps({"theme": "unknown"}))
    assert main.resolve_qt_theme(config) == "deepteal"

    config.write_text("not json")
    assert main.resolve_qt_theme(config) == "deepteal"

    config.write_text("[]")
    assert main.resolve_qt_theme(config) == "deepteal"

    assert main.resolve_qt_theme(tmp_path / "missing.json") == "deepteal"


def test_qt_theme_argument_keeps_the_startup_palette_closed():
    assert main.qt_theme_argument("gruvbox") == "--eigen-qt-theme=gruvbox"
    assert main.qt_theme_argument("invalid") == "--eigen-qt-theme=deepteal"


def test_qt_theme_argument_is_added_once_from_the_persisted_setting(tmp_path):
    config = tmp_path / "config.json"
    config.write_text(json.dumps({"theme": "gruvbox"}))

    assert main.with_qt_theme_argument(["eigen-qt"], config) == [
        "eigen-qt",
        "--eigen-qt-theme=gruvbox",
    ]
    assert main.with_qt_theme_argument(
        ["eigen-qt", "--eigen-qt-theme=nord"], config
    ) == ["eigen-qt", "--eigen-qt-theme=nord"]


def test_qt_theme_from_argv_keeps_helpers_on_the_qml_palette():
    assert main.qt_theme_from_argv(["eigen-qt", "--eigen-qt-theme=gruvbox"]) == "gruvbox"
    assert main.qt_theme_from_argv(["eigen-qt", "--eigen-qt-theme=unknown"]) == "deepteal"
    assert main.qt_theme_from_argv(["eigen-qt"]) == "deepteal"


@pytest.mark.parametrize(
    ("theme", "expected"),
    [
        (
            "studio",
            {"background": "#f4f6f8", "brand": "#0b736a", "syntax": "#f1f3f5", "focus": "#c55338", "accent": "#66558f"},
        ),
        (
            "deepteal",
            {"background": "#0b0e0f", "brand": "#3e9e96", "syntax": "#11171a", "focus": "#d08c5e", "accent": "#9e7ba6"},
        ),
        (
            "nord",
            {"background": "#1b1f27", "brand": "#81a1c1", "syntax": "#171b22", "focus": "#d1a0b0", "accent": "#b48ead"},
        ),
        (
            "gruvbox",
            {"background": "#282828", "brand": "#83a598", "syntax": "#1d2021", "focus": "#d3869b", "accent": "#b16286"},
        ),
    ],
)
def test_qml_theme_uses_the_startup_palette_argument(theme, expected):
    actual = _probe_qml_theme(theme)

    assert actual["palette"] == theme
    assert {key: actual[key] for key in expected} == expected


@pytest.mark.parametrize("theme", ["studio", "deepteal", "nord", "gruvbox"])
def test_qml_palettes_keep_semantic_roles_distinct_and_readable(theme):
    colors = _probe_qml_theme(theme)

    assert len({colors["brand"], colors["focus"], colors["accent"]}) == 3
    assert _contrast(colors["primary"], colors["background"]) >= 7
    assert _contrast(colors["secondary"], colors["background"]) >= 4.5
    assert _contrast(colors["muted"], colors["background"]) >= 3
    assert _contrast(colors["surface"], colors["background"]) >= 1.08
    assert _contrast(colors["brandForeground"], colors["brand"]) >= 4.5
    assert _contrast(colors["brandDimForeground"], colors["brandDim"]) >= 4.5
    for state_fill in ("selectedFill", "successFill", "errorFill"):
        assert colors[state_fill] not in {"#ff000000", "#00000000"}
    danger_fill = _composite(colors["errorBackground"], colors["surface2"])
    assert _contrast(colors["primary"], danger_fill) >= 4.5
    markdown = palette_for(theme)
    assert {
        "inlineBackground": markdown.inline_background,
        "inlineText": markdown.inline_text,
        "link": markdown.link,
        "syntax": markdown.syntax_background,
        "syntaxText": markdown.syntax_text,
        "syntaxKeyword": markdown.syntax_keyword,
        "syntaxType": markdown.syntax_type,
        "syntaxFunction": markdown.syntax_function,
        "syntaxString": markdown.syntax_string,
        "syntaxNumber": markdown.syntax_number,
        "syntaxComment": markdown.syntax_comment,
        "syntaxPunctuation": markdown.syntax_punctuation,
        "syntaxBuiltin": markdown.syntax_builtin,
        "error": markdown.error,
    } == {key: colors[key] for key in (
        "inlineBackground", "inlineText", "link", "syntax", "syntaxText",
        "syntaxKeyword", "syntaxType", "syntaxFunction", "syntaxString",
        "syntaxNumber", "syntaxComment", "syntaxPunctuation", "syntaxBuiltin",
        "error",
    )}


def _probe_qml_theme(theme):
    script = r'''
import json
import sys
from pathlib import Path

from PySide6.QtCore import QUrl
from PySide6.QtGui import QColor, QGuiApplication
from PySide6.QtQml import QQmlComponent, QQmlEngine

root = Path.cwd()
theme = sys.argv[1]
app = QGuiApplication(["theme-probe", f"--eigen-qt-theme={theme}"])
engine = QQmlEngine()
component = QQmlComponent(engine)
qml = b"""
import QtQuick
import "Theme.js" as Theme
QtObject {
    property string palette: Theme.paletteName
    property string background: Theme.colors.bgBase
    property string surface: Theme.colors.surfaceRaised
    property string surface2: Theme.colors.surfaceRaised2
    property string primary: Theme.colors.textPrimary
    property string secondary: Theme.colors.textSecondary
    property string muted: Theme.colors.textMuted
    property string brand: Theme.colors.brand
    property string brandDim: Theme.colors.brandDim
    property string brandForeground: Theme.colors.brandForeground
    property string brandDimForeground: Theme.colors.brandDimForeground
    property string syntax: Theme.colors.synBg
    property string focus: Theme.colors.focus
    property string accent: Theme.colors.accent
    property string inlineBackground: Theme.colors.surfaceRaised2
    property string inlineText: Theme.colors.synBuiltin
    property string link: Theme.colors.info
    property string syntaxText: Theme.colors.synText
    property string syntaxKeyword: Theme.colors.synKeyword
    property string syntaxType: Theme.colors.synType
    property string syntaxFunction: Theme.colors.synFunc
    property string syntaxString: Theme.colors.synString
    property string syntaxNumber: Theme.colors.synNumber
    property string syntaxComment: Theme.colors.synComment
    property string syntaxPunctuation: Theme.colors.synPunct
    property string syntaxBuiltin: Theme.colors.synBuiltin
    property string error: Theme.colors.error
    property string errorBackground: Theme.colors.errorBg
    property color selectedFill: Theme.colors.stateSelected
    property color successFill: Theme.colors.successBg
    property color errorFill: Theme.colors.errorBg
}
"""
component.setData(
    qml,
    QUrl.fromLocalFile(str(root / "eigenqt" / "qml" / "theme-probe.qml")),
)
obj = component.create()
if obj is None:
    raise SystemExit(str(component.errors()))
names = (
    "palette", "background", "surface", "surface2", "primary", "secondary",
    "muted", "brand", "brandDim", "brandForeground", "brandDimForeground", "syntax", "focus", "accent", "inlineBackground",
    "inlineText", "link", "syntaxText", "syntaxKeyword", "syntaxType",
    "syntaxFunction", "syntaxString", "syntaxNumber", "syntaxComment",
    "syntaxPunctuation", "syntaxBuiltin", "error", "errorBackground",
)
payload = {name: obj.property(name) for name in names}
for name in ("selectedFill", "successFill", "errorFill"):
    payload[name] = obj.property(name).name(QColor.HexArgb)
print(json.dumps(payload))
'''
    env = os.environ.copy()
    env["QT_QPA_PLATFORM"] = "offscreen"
    result = subprocess.run(
        [sys.executable, "-c", textwrap.dedent(script), theme],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stdout + result.stderr
    return json.loads(result.stdout)


def _contrast(foreground, background):
    lighter = max(_luminance(foreground), _luminance(background))
    darker = min(_luminance(foreground), _luminance(background))
    return (lighter + 0.05) / (darker + 0.05)


def _luminance(value):
    channels = _rgb(value)
    linear = [channel / 12.92 if channel <= 0.04045 else ((channel + 0.055) / 1.055) ** 2.4 for channel in channels]
    return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2]


def _rgb(value):
    if isinstance(value, tuple):
        return value
    return tuple(int(value[index:index + 2], 16) / 255 for index in (1, 3, 5))


def _composite(foreground, background):
    if foreground.startswith("#") and len(foreground) == 9:
        alpha = int(foreground[1:3], 16) / 255
        foreground_rgb = tuple(int(foreground[index:index + 2], 16) / 255 for index in (3, 5, 7))
    else:
        channels = foreground.removeprefix("rgba(").removesuffix(")").split(",")
        foreground_rgb = tuple(int(channel) / 255 for channel in channels[:3])
        alpha = float(channels[3])
    background_rgb = _rgb(background)
    return tuple(alpha * front + (1 - alpha) * back for front, back in zip(foreground_rgb, background_rgb))
