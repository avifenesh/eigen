#!/usr/bin/env python3
"""Quick QML syntax test for HomeView."""
import sys
from pathlib import Path

from PySide6.QtCore import QTimer
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine, QQmlComponent
from PySide6.QtQuickControls2 import QQuickStyle

ROOT = Path(__file__).resolve().parent


def main():
    QQuickStyle.setStyle("Basic")
    app = QGuiApplication(sys.argv)

    engine = QQmlApplicationEngine()
    engine.addImportPath(str(ROOT / "eigenqt"))

    # Try to instantiate just the component to check for syntax errors
    qml_path = str(ROOT / "eigenqt" / "qml" / "HomeView.qml")
    component = QQmlComponent(engine, qml_path)

    if component.isError():
        print("QML Errors:")
        for error in component.errors():
            print(f"  {error.toString()}")
        sys.exit(1)

    print("✓ HomeView.qml has no syntax errors")

    # Quit immediately (we just validated syntax)
    QTimer.singleShot(0, app.quit)
    sys.exit(app.exec())


if __name__ == "__main__":
    main()
