#!/usr/bin/env python3
"""Quick script to verify Tasks view and take a screenshot."""
import sys
from pathlib import Path
from PySide6.QtCore import QTimer
from PySide6.QtGui import QGuiApplication, QPixmap
from PySide6.QtQml import QQmlApplicationEngine
from PySide6.QtQuickControls2 import QQuickStyle

ROOT = Path(__file__).resolve().parent

# Import models
from eigenqt.models import TasksModel
from eigenqt.rpc import RpcClient

def take_screenshot(window, filename):
    """Grab the window and save as PNG."""
    pixmap = window.grabWindow()
    pixmap.save(filename)
    print(f"Screenshot saved: {filename}")

def main():
    QQuickStyle.setStyle("Basic")
    app = QGuiApplication(sys.argv)

    # Create RPC client + model
    rpc_client = RpcClient()
    tasks_model = TasksModel(rpc_client)

    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()

    # Expose to QML
    ctx.setContextProperty("tasksModel", tasks_model)
    ctx.setContextProperty("rpcClient", rpc_client)
    ctx.setContextProperty("daemonOnline", False)
    ctx.setContextProperty("guiserverSha", "test")

    # Load a minimal test QML that shows TasksView
    qml_code = """
import QtQuick
import QtQuick.Controls

ApplicationWindow {
    id: root
    visible: true
    width: 1280
    height: 800
    title: "Tasks View Test"

    Component.onCompleted: {
        // Wait for model to load, then screenshot
        Qt.callLater(function() {
            // Switch to tasks view (if using StackLayout in Main)
            console.log("Tasks view loaded")
        })
    }

    TasksView {
        anchors.fill: parent
        tasksModel: root.tasksModel
    }

    property var tasksModel: null
}
"""

    # Save temp QML
    temp_qml = ROOT / "temp_test.qml"
    temp_qml.write_text(qml_code)

    engine.addImportPath(str(ROOT / "eigenqt"))
    engine.load(str(temp_qml))

    if not engine.rootObjects():
        print("Failed to load QML")
        sys.exit(-1)

    root_obj = engine.rootObjects()[0]

    # Schedule screenshot after 3 seconds (allow time for RPC + rendering)
    def do_screenshot():
        screenshot_path = str(ROOT / "screenshots" / "tasks-view.png")
        Path(screenshot_path).parent.mkdir(exist_ok=True)
        take_screenshot(root_obj, screenshot_path)
        app.quit()

    QTimer.singleShot(3000, do_screenshot)

    sys.exit(app.exec())

if __name__ == "__main__":
    main()
