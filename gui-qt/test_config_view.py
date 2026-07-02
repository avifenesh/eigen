#!/usr/bin/env python3
"""
test_config_view.py — Offscreen launch test for ConfigView.

Tests QML loading with QT_QPA_PLATFORM=offscreen (no window shown).
"""

import sys
import os
from pathlib import Path
from PySide6.QtCore import QTimer
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine

# Set offscreen platform before any Qt imports
os.environ["QT_QPA_PLATFORM"] = "offscreen"

ROOT = Path(__file__).resolve().parent


def main():
    """ConfigView QML loads without errors in offscreen mode."""
    app = QGuiApplication(sys.argv)
    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()

    # Create mock models (won't fetch data, but QML can bind)
    from eigenqt.rpc.client import RpcClient
    from eigenqt.models.config import ConfigModel, RuleChainsModel

    client = RpcClient()
    config_model = ConfigModel(client)
    rule_chains_model = RuleChainsModel(client)

    ctx.setContextProperty("configModel", config_model)
    ctx.setContextProperty("ruleChainsModel", rule_chains_model)

    # Load ConfigView
    engine.addImportPath(str(ROOT / "eigenqt"))
    engine.load(str(ROOT / "eigenqt" / "qml" / "ConfigView.qml"))

    if not engine.rootObjects():
        print("FAIL: ConfigView failed to load")
        sys.exit(1)

    # Quit after 1s
    QTimer.singleShot(1000, app.quit)
    app.exec()
    print("SUCCESS: ConfigView loaded")


if __name__ == "__main__":
    main()
