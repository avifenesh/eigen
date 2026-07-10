import os
import subprocess
import sys
import textwrap
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_routing_model_cards_keep_a_real_responsive_width():
    script = r"""
import os
from pathlib import Path

from PySide6.QtCore import QObject, QPointF, QSize, QTimer, QUrl, Signal
from PySide6.QtGui import QGuiApplication
from PySide6.QtQuick import QQuickView
from PySide6.QtQuickControls2 import QQuickStyle

from eigenqt.models.routing import RoutingModel


ROOT = Path.cwd()
HEIGHT = 800


class FakeRpcClient(QObject):
    connected = Signal()

    def call(self, method, *args, callback=None, error_callback=None):
        if callback is not None:
            QTimer.singleShot(0, lambda: callback({"result": self._result(method)}))

    def _result(self, method):
        if method == "Routing":
            return snapshot()
        if method == "ObserveSummary":
            return {"available": True, "records": 3, "routes": {"routed": 2, "assessed": 1}}
        return {}


def snapshot():
    return {
        "models": [
            {
                "id": "gpt-5",
                "provider": "codex",
                "contextWindow": 400000,
                "cache": True,
                "reasoning": True,
                "effortLevels": ["low", "medium", "high"],
                "search": True,
                "vision": True,
                "available": True,
            },
            {
                "id": "grok-4",
                "provider": "grok",
                "contextWindow": 256000,
                "reasoning": True,
                "effortLevels": ["low", "high"],
                "search": True,
                "available": True,
            },
            {
                "id": "local-qwen",
                "provider": "llama",
                "contextWindow": 128000,
                "available": False,
            },
        ],
        "providers": [
            {"name": "codex", "credentialed": True, "modelCount": 1},
            {"name": "grok", "credentialed": True, "modelCount": 1},
            {"name": "llama", "credentialed": False, "modelCount": 1},
        ],
    }


def pump(app, rounds=20):
    for _ in range(rounds):
        app.processEvents()


def find_item(root, object_name):
    matches = []

    def collect(item):
        if item is None:
            return
        if item.objectName() == object_name:
            matches.append(item)
        for child in item.childItems():
            collect(child)

    collect(root)
    return max(matches, key=lambda item: float(item.property("width") or 0) * float(item.property("height") or 0), default=None)


def descendant_tags(item):
    tags = []

    def collect(candidate):
        for child in candidate.childItems():
            if child.property("qaIsAppTag") is True:
                tags.append(child)
            collect(child)

    collect(item)
    return tags


def assert_card_bounds(root, width, expected_max_width=None):
    cards = [
        find_item(root, "routingModelCard_gpt_5"),
        find_item(root, "routingModelCard_grok_4"),
        find_item(root, "routingModelCard_local_qwen"),
    ]
    if any(card is None for card in cards):
        raise AssertionError("Routing view did not render all model cards")

    for card in cards:
        card_width = float(card.property("width") or 0)
        if card_width <= 0:
            raise AssertionError(f"{card.objectName()} has zero width")
        if expected_max_width is not None and card_width > expected_max_width:
            raise AssertionError(f"{card.objectName()} did not narrow with the viewport: {card_width}")
        top_left = card.mapToScene(QPointF(0, 0))
        bottom_right = card.mapToScene(QPointF(card_width, 0))
        if top_left.x() < -0.5 or bottom_right.x() > width + 0.5:
            raise AssertionError(
                f"{card.objectName()} overflows horizontally: "
                f"{top_left.x():.1f} -> {bottom_right.x():.1f} in {width}px"
            )
        if card.property("qaTextFits") is not True:
            raise AssertionError(f"{card.objectName()} model label is clipped")
        card_bottom = card.mapToScene(QPointF(0, float(card.property("height") or 0))).y()
        card_right = card.mapToScene(QPointF(card_width, 0)).x()
        for tag in descendant_tags(card):
            tag_left = tag.mapToScene(QPointF(0, 0)).x()
            tag_right = tag.mapToScene(QPointF(float(tag.property("width") or 0), 0)).x()
            tag_top = tag.mapToScene(QPointF(0, 0)).y()
            tag_bottom = tag.mapToScene(QPointF(0, float(tag.property("height") or 0))).y()
            if (
                tag_left < top_left.x() - 0.5
                or tag_right > card_right + 0.5
                or tag_top < top_left.y() - 0.5
                or tag_bottom > card_bottom + 0.5
            ):
                raise AssertionError(
                    f"{card.objectName()} does not contain capability tag "
                    f"({tag_left:.1f},{tag_top:.1f} -> {tag_right:.1f},{tag_bottom:.1f}; "
                    f"card {top_left.x():.1f},{top_left.y():.1f} -> {card_right:.1f},{card_bottom:.1f})"
                )


QQuickStyle.setStyle("Basic")
app = QGuiApplication([])
model = RoutingModel(FakeRpcClient())
model._on_routing_result({"result": snapshot()})
model._on_observe_result({"result": {"available": True, "records": 3, "routes": {"routed": 2, "assessed": 1}}})

view = QQuickView()
view.setResizeMode(QQuickView.SizeRootObjectToView)
view.setInitialProperties({"routingModel": model})
view.setSource(QUrl.fromLocalFile(str(ROOT / "eigenqt" / "qml" / "RoutingView.qml")))
if view.status() == QQuickView.Error or view.rootObject() is None:
    raise AssertionError([error.toString() for error in view.errors()])

view.setWidth(1200)
view.setHeight(HEIGHT)
view.show()
pump(app, 32)
root = view.rootObject()
assert_card_bounds(root, 1200)

screenshots = os.environ.get("EIGEN_QT_SCREENSHOT_DIR", "")
if screenshots:
    output = Path(screenshots)
    output.mkdir(parents=True, exist_ok=True)
    view.grabWindow().save(str(output / "routing-wide.png"))

view.setWidth(420)
view.setHeight(HEIGHT)
pump(app, 32)
assert_card_bounds(root, 420, expected_max_width=250)

if screenshots:
    view.grabWindow().save(str(Path(screenshots) / "routing-narrow.png"))

model.set_active(False)
view.close()
"""
    env = os.environ.copy()
    env.setdefault("QT_QPA_PLATFORM", "offscreen")
    env["PYTHONPATH"] = str(ROOT) + os.pathsep + env.get("PYTHONPATH", "")
    result = subprocess.run(
        [sys.executable, "-c", textwrap.dedent(script)],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert result.returncode == 0, result.stdout + result.stderr
