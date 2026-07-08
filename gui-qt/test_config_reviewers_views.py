#!/usr/bin/env python3
"""
test_config_reviewers_views.py — Offscreen launch test for Qt system views.

Tests QML loading with QT_QPA_PLATFORM=offscreen (no window shown).
"""

import sys
import os
from pathlib import Path
from PySide6.QtCore import QCoreApplication, QTimer, Qt, QUrl
from PySide6.QtGui import QGuiApplication
from PySide6.QtQml import QQmlApplicationEngine
from PySide6.QtQuick import QQuickView

# Set offscreen platform before any Qt imports
os.environ["QT_QPA_PLATFORM"] = "offscreen"

ROOT = Path(__file__).resolve().parent


def find_item(item, object_name):
    """Find a QQuickItem by objectName in a loaded QML tree."""
    if item is None:
        return None
    if item.objectName() == object_name:
        return item
    if hasattr(item, "childItems"):
        for child in item.childItems():
            found = find_item(child, object_name)
            if found is not None:
                return found
    return None


def create_inert_client():
    from eigenqt.rpc.client import RpcClient

    original_start_workers = RpcClient._start_workers
    RpcClient._start_workers = lambda self: None
    try:
        return RpcClient()
    finally:
        RpcClient._start_workers = original_start_workers


def app_instance():
    """Reuse the singleton QGuiApplication when this file is run as a script."""
    app = QCoreApplication.instance()
    if app is None:
        return QGuiApplication(sys.argv)
    if isinstance(app, QGuiApplication):
        return app

    if "pytest" in sys.modules:
        import pytest

        pytest.skip("QML launch tests require QGuiApplication")
    raise RuntimeError("QML launch tests require QGuiApplication")


def test_config_view_loads():
    """ConfigView QML loads without errors in offscreen mode."""
    app = app_instance()
    view = QQuickView()
    view.setResizeMode(QQuickView.SizeRootObjectToView)
    view.setWidth(1200)
    view.setHeight(800)
    ctx = view.rootContext()

    # Create mock models (won't fetch data, but QML can bind)
    from eigenqt.models.config import ConfigModel, RuleChainsModel

    client = create_inert_client()
    config_model = ConfigModel(client)
    rule_chains_model = RuleChainsModel(client)

    ctx.setContextProperty("configModel", config_model)
    ctx.setContextProperty("ruleChainsModel", rule_chains_model)

    # Load ConfigView
    view.engine().addImportPath(str(ROOT / "eigenqt"))
    view.setInitialProperties(
        {"configModel": config_model, "ruleChainsModel": rule_chains_model}
    )
    view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / "ConfigView.qml")))

    assert view.status() != QQuickView.Error, [error.toString() for error in view.errors()]
    assert view.rootObject() is not None, "ConfigView failed to load"
    root = view.rootObject()
    view.show()
    errors = []

    def assert_load_state():
        empty = find_item(root, "configEmptyState")
        empty_refresh = find_item(root, "configEmptyRefreshButton")
        load_error = find_item(root, "configLoadError")
        load_retry = find_item(root, "configLoadErrorRetry")

        if load_error is not None and load_error.property("visible") is True:
            if load_retry is None or load_retry.property("qaTextFits") is not True:
                errors.append("ConfigView load-error retry action is missing or clipped")
        elif empty is not None and empty.property("visible") is True:
            if empty_refresh is None:
                errors.append("ConfigView missing no-fields refresh action")
        else:
            errors.append("ConfigView showed neither empty state nor load-error state")
        app.quit()

    QTimer.singleShot(50, assert_load_state)
    QTimer.singleShot(1000, app.quit)
    app.exec()
    view.hide()
    view.setSource(QUrl())
    client.shutdown()
    assert not errors, "; ".join(errors)
    print("ConfigView loaded successfully")


def test_reviewers_view_loads():
    """ReviewersView QML loads without errors in offscreen mode."""
    app = app_instance()
    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()

    # Create mock model
    from eigenqt.models.reviewers import ReviewersModel

    client = create_inert_client()
    reviewers_model = ReviewersModel(client)

    ctx.setContextProperty("reviewersModel", reviewers_model)

    # Load ReviewersView
    engine.addImportPath(str(ROOT / "eigenqt"))
    engine.load(str(ROOT / "eigenqt" / "qml" / "ReviewersView.qml"))

    assert len(engine.rootObjects()) > 0, "ReviewersView failed to load"

    # Quit after 1s
    QTimer.singleShot(1000, app.quit)
    app.exec()
    client.shutdown()
    print("ReviewersView loaded successfully")


if __name__ == "__main__":
    test_config_view_loads()
    test_reviewers_view_loads()
