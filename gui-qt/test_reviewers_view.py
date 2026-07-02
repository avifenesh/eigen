#!/usr/bin/env python3
"""
test_reviewers_view.py — Offscreen launch test for ReviewersView.

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
    """ReviewersView QML loads without errors in offscreen mode."""
    app = QGuiApplication(sys.argv)
    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()

    # Create mock model
    from eigenqt.rpc.client import RpcClient
    from eigenqt.models.reviewers import ReviewersModel

    client = RpcClient()
    reviewers_model = ReviewersModel(client)

    ctx.setContextProperty("reviewersModel", reviewers_model)

    # Load ReviewersView
    engine.addImportPath(str(ROOT / "eigenqt"))
    engine.load(str(ROOT / "eigenqt" / "qml" / "ReviewersView.qml"))

    if not engine.rootObjects():
        print("FAIL: ReviewersView failed to load")
        sys.exit(1)

    # Quit after 1s
    QTimer.singleShot(1000, app.quit)
    app.exec()
    print("SUCCESS: ReviewersView loaded")


if __name__ == "__main__":
    main()
