import json
import os
import subprocess
import sys
import textwrap
from pathlib import Path

import main
import pytest


ROOT = Path(__file__).resolve().parents[1]


def test_resolve_qt_theme_reads_only_known_config_values(tmp_path):
    config = tmp_path / "config.json"
    config.write_text(json.dumps({"theme": "nord"}))
    assert main.resolve_qt_theme(config) == "nord"

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


@pytest.mark.parametrize(
    ("theme", "expected"),
    [
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


@pytest.mark.parametrize("theme", ["deepteal", "nord", "gruvbox"])
def test_qml_palettes_keep_semantic_roles_distinct_and_readable(theme):
    colors = _probe_qml_theme(theme)

    assert len({colors["brand"], colors["focus"], colors["accent"]}) == 3
    assert _contrast(colors["primary"], colors["background"]) >= 7
    assert _contrast(colors["secondary"], colors["background"]) >= 4.5
    assert _contrast(colors["muted"], colors["background"]) >= 3
    assert _contrast(colors["surface"], colors["background"]) >= 1.08
    danger_fill = _composite(colors["errorBackground"], colors["surface2"])
    assert _contrast(colors["primary"], danger_fill) >= 4.5


def _probe_qml_theme(theme):
    script = r'''
import json
import sys
from pathlib import Path

from PySide6.QtCore import QUrl
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlComponent, QQmlEngine

root = Path.cwd()
theme = sys.argv[1]
app = QGuiApplication(["theme-probe", f"--eigen-qt-theme={theme}"])
engine = QQmlEngine()
component = QQmlComponent(engine)
qml = b"""
import QtQml
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
    property string syntax: Theme.colors.synBg
    property string focus: Theme.colors.focus
    property string accent: Theme.colors.accent
    property string errorBackground: Theme.colors.errorBg
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
    "muted", "brand", "syntax", "focus", "accent", "errorBackground",
)
print(json.dumps({name: obj.property(name) for name in names}))
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
    channels = foreground.removeprefix("rgba(").removesuffix(")").split(",")
    foreground_rgb = tuple(int(channel) / 255 for channel in channels[:3])
    alpha = float(channels[3])
    background_rgb = _rgb(background)
    return tuple(alpha * front + (1 - alpha) * back for front, back in zip(foreground_rgb, background_rgb))
