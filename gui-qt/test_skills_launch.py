#!/usr/bin/env python3
"""Test Skills view launch and screenshot."""
import sys
from pathlib import Path
from PySide6.QtCore import QTimer
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine

ROOT = Path(__file__).resolve().parent

def main():
    app = QGuiApplication(sys.argv)

    # Minimal QML to test SkillsView load
    engine = QQmlApplicationEngine()

    # Set minimal context (no RPC client needed for load test)
    ctx = engine.rootContext()
    ctx.setContextProperty("skillsModel", None)
    ctx.setContextProperty("proposalsModel", None)
    ctx.setContextProperty("client", None)

    # Try to load SkillsView
    engine.addImportPath(str(ROOT / "eigenqt"))
    qml_path = ROOT / "eigenqt" / "qml" / "SkillsView.qml"

    print(f"Loading {qml_path}...")
    engine.load(str(qml_path))

    if not engine.rootObjects():
        print("ERROR: Failed to load SkillsView.qml")
        return 1

    print("SUCCESS: SkillsView.qml loaded without errors")

    # Exit after 1s
    QTimer.singleShot(1000, app.quit)
    return app.exec()

if __name__ == "__main__":
    sys.exit(main())
