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
        ("deepteal", ("#15191e", "#5bd6c2", "#11161b")),
        ("nord", ("#1b1f27", "#81a1c1", "#171b22")),
        ("gruvbox", ("#282828", "#83a598", "#1d2021")),
    ],
)
def test_qml_theme_uses_the_startup_palette_argument(theme, expected):
    script = r'''
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
    property string brand: Theme.colors.brand
    property string syntax: Theme.colors.synBg
}
"""
component.setData(
    qml,
    QUrl.fromLocalFile(str(root / "eigenqt" / "qml" / "theme-probe.qml")),
)
obj = component.create()
if obj is None:
    raise SystemExit(str(component.errors()))
actual = (obj.property("palette"), obj.property("background"), obj.property("brand"), obj.property("syntax"))
print("\t".join(actual))
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
    assert tuple(result.stdout.strip().split("\t")) == (theme, *expected)
