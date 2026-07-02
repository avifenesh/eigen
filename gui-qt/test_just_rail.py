#!/usr/bin/env python3
"""Minimal test to verify Rail.qml loads correctly."""
import sys
from pathlib import Path
from PySide6.QtCore import Qt, QObject, Property, Signal
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine, QQmlContext
from PySide6.QtQuickControls2 import QQuickStyle

class MockModel(QObject):
    def __init__(self, parent=None):
        super().__init__(parent)

    def rowCount(self, parent=None):
        return 3

class MockTasksModel(QObject):
    def __init__(self, parent=None):
        super().__init__(parent)
        self._running_count = 2

    @Property(int)
    def running_count(self):
        return self._running_count

def main():
    QQuickStyle.setStyle("Basic")
    app = QGuiApplication(sys.argv)

    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()

    # Mock models
    sessions_model = MockModel()
    tasks_model = MockTasksModel()

    ctx.setContextProperty("sessionsModel", sessions_model)
    ctx.setContextProperty("tasksModel", tasks_model)
    ctx.setContextProperty("statsData", {"running_turns": 2, "sessions": 3})
    ctx.setContextProperty("daemonOnline", True)
    ctx.setContextProperty("guiserverSha", "test123")

    # Load Rail.qml directly
    ROOT = Path(__file__).parent
    rail_path = ROOT / "eigenqt" / "qml" / "Rail.qml"

    print(f"Loading Rail from: {rail_path}")
    engine.load(str(rail_path))

    if not engine.rootObjects():
        print("ERROR: Failed to load Rail.qml")
        sys.exit(-1)

    print("SUCCESS: Rail.qml loaded")
    print("Root objects:", engine.rootObjects())

    # Don't actually show window, just verify it loaded
    sys.exit(0)

if __name__ == "__main__":
    main()
