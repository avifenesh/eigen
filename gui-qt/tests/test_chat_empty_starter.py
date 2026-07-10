#!/usr/bin/env python3
"""Exercise the empty-session Chat starter without sending a model request."""

import os
from pathlib import Path

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PySide6.QtCore import QObject, QPoint, QPointF, QUrl, Qt, Signal
from PySide6.QtGui import QGuiApplication
from PySide6.QtQuick import QQuickView
from PySide6.QtTest import QTest

from eigenqt.models import SessionStateModel, TranscriptModel


ROOT = Path(__file__).resolve().parents[1]


class FakeRpcClient(QObject):
    connected = Signal()
    event = Signal(str, dict)
    dropped = Signal(str)

    def subscribe(self, _channels):
        return None

    def unsubscribe(self, _channels):
        return None


def pump(app, rounds=20):
    for _ in range(rounds):
        app.processEvents()


def find_item(item, object_name):
    if item is None:
        return None
    if item.objectName() == object_name:
        return item
    for child in item.childItems():
        found = find_item(child, object_name)
        if found is not None:
            return found
    return None


def click_item(view, item):
    width = float(item.property("width") or 0)
    height = float(item.property("height") or 0)
    point = item.mapToItem(view.contentItem(), QPointF(width / 2, height / 2))
    QTest.mouseClick(view, Qt.LeftButton, Qt.NoModifier, QPoint(int(point.x()), int(point.y())))


def assert_inside_window(item, width, height):
    item_width = float(item.property("width") or 0)
    item_height = float(item.property("height") or 0)
    if item_width <= 0 or item_height <= 0:
        raise AssertionError(f"{item.objectName()} has invalid size {item_width}x{item_height}")
    top_left = item.mapToScene(QPointF(0, 0))
    bottom_right = item.mapToScene(QPointF(item_width, item_height))
    if (
        top_left.x() < -0.5
        or top_left.y() < -0.5
        or bottom_right.x() > width + 0.5
        or bottom_right.y() > height + 0.5
    ):
        raise AssertionError(
            f"{item.objectName()} escaped the Chat viewport: "
            f"({top_left.x():.1f}, {top_left.y():.1f}) -> "
            f"({bottom_right.x():.1f}, {bottom_right.y():.1f})"
        )


app = QGuiApplication([])
client = FakeRpcClient()
transcript = TranscriptModel(client, "")
transcript.seed({"messages": [], "running": False})
state = SessionStateModel(client, "s-empty")
state.seed(
    {
        "model": "openai.gpt-5.5",
        "provider": "codex",
        "effort": "medium",
        "perm": "gated",
        "title": "Fresh session",
        "catalog": {"models": [{"id": "openai.gpt-5.5"}, {"id": "gpt-5.6-sol"}]},
    }
)

view = QQuickView()
view.setResizeMode(QQuickView.SizeRootObjectToView)
view.setWidth(900)
view.setHeight(620)
view.engine().addImportPath(str(ROOT / "eigenqt"))
view.setInitialProperties(
    {
        "sessionId": "s-empty",
        "transcriptModel": transcript,
        "sessionStateModel": state,
    }
)
view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / "ChatView.qml")))
if view.status() == QQuickView.Error or view.rootObject() is None:
    raise AssertionError([error.toString() for error in view.errors()])
view.show()
root = view.rootObject()
root = view.rootObject()
for width in (900, 420):
    view.setWidth(width)
    pump(app)
    if root.property("qaEmptyStarterVisible") is not True:
        raise AssertionError(f"Empty session did not render the Chat starter at {width}px")
    if int(root.property("qaStarterPromptCount") or 0) != 3:
        raise AssertionError("Chat starter did not expose its complete prompt set")

    for index in range(3):
        prompt = find_item(root, f"chatStarterPrompt_{index}")
        if prompt is None or prompt.property("visible") is not True:
            raise AssertionError(f"Chat starter prompt {index} did not render at {width}px")
        if prompt.property("qaTextFits") is not True:
            raise AssertionError(f"Chat starter prompt {index} text is clipped at {width}px")
        assert_inside_window(prompt, width, 620)

first_prompt = find_item(root, "chatStarterPrompt_0")
composer = find_item(root, "chatComposerTextArea")
if composer is None:
    raise AssertionError("Chat composer did not render beside the starter")
click_item(view, first_prompt)
pump(app)
if composer.property("text") != first_prompt.property("text"):
    raise AssertionError("Starter prompt did not populate the composer")
if composer.property("activeFocus") is not True:
    raise AssertionError("Starter prompt did not focus the composer")

transcript.appendNote("First reply arrived")
QTest.qWait(40)
pump(app)
if root.property("qaEmptyStarterVisible") is not False:
    raise AssertionError(
        "Chat starter stayed visible after the first transcript row: "
        f"rows={root.property('qaTranscriptRows')}"
    )
