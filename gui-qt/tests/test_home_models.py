"""
test_home_models.py — DashboardModel and FeedModel normalization tests.
"""
import json
from pathlib import Path

import pytest
from PySide6.QtCore import QCoreApplication, QObject, Signal

from eigenqt.models.home import DashboardModel, FeedModel


@pytest.fixture
def dashboard_fixture():
    """Load captured real Dashboard() response."""
    fixture_path = Path(__file__).parent / "fixtures" / "dashboard.json"
    with open(fixture_path) as f:
        return json.load(f)


@pytest.fixture
def feed_fixture():
    """Synthetic feed fixture."""
    return {
        "items": [
            {
                "key": "git:1234",
                "kind": "git",
                "title": "Uncommitted changes in main.go",
                "detail": "3 files changed, 42 insertions(+), 8 deletions(-)",
                "dir": "/home/user/project",
                "dirName": "project",
                "task": "Review and commit the changes",
                "url": "",
            },
            {
                "key": "github:5678",
                "kind": "github",
                "title": "PR #42 ready for review",
                "detail": "",
                "dir": "/home/user/project",
                "dirName": "project",
                "task": "",
                "url": "https://github.com/user/project/pull/42",
            },
        ],
        "scannedMs": 1719878400000,
        "fresh": True,
    }


class MockRpcClient(QObject):
    """Mock RpcClient that replays canned responses."""

    connected = Signal()
    event = Signal(str, dict)

    def __init__(self, canned_responses):
        super().__init__()
        self._canned = canned_responses
        self._subscribed = []

    def call(self, method, params=None, callback=None):
        """Replay canned response for method."""
        if callback and method in self._canned:
            result = {"result": self._canned[method]}
            callback(result)

    def subscribe(self, channels):
        """Track subscriptions."""
        self._subscribed.extend(channels)


def test_dashboard_model_normalization(dashboard_fixture):
    """Dashboard DTO → model properties."""
    app = QCoreApplication.instance() or QCoreApplication([])

    client = MockRpcClient({"Dashboard": dashboard_fixture})
    model = DashboardModel(client)

    # Manually trigger the callback directly
    model._on_dashboard_result({"result": dashboard_fixture})

    # Check properties
    assert model.google_connected is True
    assert len(model.events) == 2
    assert model.events[0]["summary"] == "Team standup"
    assert model.unread_count == 3
    assert len(model.unread) == 3
    assert model.unread[0]["from"] == "Alice <alice@example.com>"

    # Health
    health = model.health
    assert health["loadPerCpu"] == pytest.approx(0.42)
    assert health["memUsedPct"] == pytest.approx(68.5)
    assert health["cpuTempC"] == pytest.approx(62.0)

    # GPUs
    assert len(model.gpus) == 1
    gpu = model.gpus[0]
    assert gpu["name"] == "NVIDIA RTX 3090"
    assert gpu["utilPct"] == pytest.approx(45.0)
    assert gpu["memUsedGb"] == pytest.approx(12.5)
    assert gpu["tempC"] == pytest.approx(68.0)


def test_feed_model_normalization(feed_fixture):
    """Feed DTO → model roles."""
    app = QCoreApplication.instance() or QCoreApplication([])

    client = MockRpcClient({"Feed": feed_fixture})
    model = FeedModel(client)

    # Manually trigger the callback directly
    model._on_feed_result({"result": feed_fixture})

    # Check row count
    assert model.rowCount() == 2

    # Check first item (git)
    idx0 = model.index(0, 0)
    assert model.data(idx0, FeedModel.KeyRole) == "git:1234"
    assert model.data(idx0, FeedModel.KindRole) == "git"
    assert model.data(idx0, FeedModel.TitleRole) == "Uncommitted changes in main.go"
    assert model.data(idx0, FeedModel.DetailRole) == "3 files changed, 42 insertions(+), 8 deletions(-)"
    assert model.data(idx0, FeedModel.DirNameRole) == "project"
    assert model.data(idx0, FeedModel.TaskRole) == "Review and commit the changes"

    # Check second item (github)
    idx1 = model.index(1, 0)
    assert model.data(idx1, FeedModel.KindRole) == "github"
    assert model.data(idx1, FeedModel.URLRole) == "https://github.com/user/project/pull/42"


def test_feed_model_dismiss(feed_fixture):
    """Dismiss removes row optimistically."""
    app = QCoreApplication.instance() or QCoreApplication([])

    client = MockRpcClient({"Feed": feed_fixture})
    model = FeedModel(client)

    model._on_feed_result({"result": feed_fixture})

    assert model.rowCount() == 2

    # Dismiss first item
    model.dismiss("git:1234")

    # Row removed
    assert model.rowCount() == 1
    idx0 = model.index(0, 0)
    assert model.data(idx0, FeedModel.KeyRole) == "github:5678"
