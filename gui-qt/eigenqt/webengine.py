"""Qt WebEngine bootstrap for QML WebEngineView surfaces."""

from PySide6.QtCore import QCoreApplication, Qt


def initialize_webengine() -> None:
    """Initialize Qt WebEngine before QML loads any WebEngineView."""
    QCoreApplication.setAttribute(Qt.AA_ShareOpenGLContexts, True)

    from PySide6.QtWebEngineQuick import QtWebEngineQuick

    QtWebEngineQuick.initialize()
