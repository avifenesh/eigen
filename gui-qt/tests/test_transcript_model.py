from PySide6.QtCore import QObject, QCoreApplication, Signal
from PySide6.QtTest import QTest

from eigenqt.models.transcript import TranscriptModel


class FakeRpcClient(QObject):
    event = Signal(str, dict)
    dropped = Signal(str)

    def __init__(self):
        super().__init__()
        self.subscribed = []
        self.unsubscribed = []

    def subscribe(self, channels):
        self.subscribed.extend(channels or [])

    def unsubscribe(self, channels):
        self.unsubscribed.extend(channels or [])

    def call(self, *_args, **_kwargs):
        pass


def test_transcript_model_has_streaming_tracks_turn_lifecycle():
    app = QCoreApplication.instance() or QCoreApplication([])
    client = FakeRpcClient()
    model = TranscriptModel(client, "s-stream")
    changes = []
    activity_changes = []
    model.streamingChanged.connect(lambda: changes.append(model.hasStreaming))
    model.activityChanged.connect(lambda: activity_changes.append((model.hasActivity, model.activityLabel)))

    assert model.hasStreaming is False
    assert model.hasActivity is False
    assert model.activityLabel == "Idle"
    assert client.subscribed == ["session:s-stream"]

    client.event.emit(
        "session:s-stream",
        {"event": {"kind": "text", "text": "hello", "step": 1}, "replay": False},
    )
    app.processEvents()

    assert model.hasStreaming is True
    assert model.hasActivity is True
    assert model.activityLabel == "Streaming"
    assert changes == [True]

    client.event.emit(
        "session:s-stream",
        {"event": {"kind": "done", "step": 1}, "replay": False},
    )
    app.processEvents()

    assert model.hasStreaming is False
    assert model.hasActivity is False
    assert model.activityLabel == "Idle"
    assert changes == [True, False]
    assert activity_changes[0] == (True, "Streaming")
    assert activity_changes[-1] == (False, "Idle")


def test_transcript_model_activity_tracks_reasoning_and_tools():
    app = QCoreApplication.instance() or QCoreApplication([])
    client = FakeRpcClient()
    model = TranscriptModel(client, "s-active")

    client.event.emit(
        "session:s-active",
        {"event": {"kind": "reasoning", "text": "thinking", "step": 1}, "replay": False},
    )
    app.processEvents()
    assert model.hasActivity is True
    assert model.activityLabel == "Thinking"
    assert model.hasStreaming is True

    client.event.emit(
        "session:s-active",
        {"event": {"kind": "tool_start", "tool": "bash", "toolId": "t1", "step": 2}, "replay": False},
    )
    app.processEvents()
    assert model.hasActivity is True
    assert model.activityLabel == "Running bash"

    client.event.emit(
        "session:s-active",
        {"event": {"kind": "done", "step": 2}, "replay": False},
    )
    app.processEvents()
    assert model.hasActivity is False
    assert model.activityLabel == "Idle"


def test_transcript_model_stages_stream_rows_until_coalesced_flush():
    app = QCoreApplication.instance() or QCoreApplication([])
    client = FakeRpcClient()
    model = TranscriptModel(client, "s-stream")
    inserted = []
    changed = []
    model.rowsInserted.connect(lambda _parent, first, last: inserted.append((first, last)))
    model.dataChanged.connect(lambda top_left, bottom_right, _roles: changed.append((top_left.row(), bottom_right.row())))

    client.event.emit(
        "session:s-stream",
        {"event": {"kind": "text", "text": "hello", "step": 1}, "replay": False},
    )
    app.processEvents()

    assert model.hasStreaming is True
    assert model.rowCount() == 0
    assert inserted == []

    client.event.emit(
        "session:s-stream",
        {"event": {"kind": "text", "text": " world", "step": 1}, "replay": False},
    )
    app.processEvents()

    assert model.rowCount() == 0
    assert inserted == []

    QTest.qWait(25)
    app.processEvents()

    assert inserted == [(0, 0)]
    assert model.rowCount() == 1
    index = model.index(0, 0)
    assert model.data(index, TranscriptModel.TextRole) == "hello world"
    assert model.data(index, TranscriptModel.StreamingRole) is True
    assert changed == [(0, 0)]


def test_transcript_model_seed_and_clear_reset_streaming_state():
    client = FakeRpcClient()
    model = TranscriptModel(client, "s-stream")

    client.event.emit(
        "session:s-stream",
        {"event": {"kind": "text", "text": "hello", "step": 1}, "replay": False},
    )
    assert model.hasStreaming is True

    model.seed({"messages": [{"role": "assistant", "text": "done"}]})
    assert model.hasStreaming is False

    client.event.emit(
        "session:s-stream",
        {"event": {"kind": "text", "text": "again", "step": 2}, "replay": False},
    )
    assert model.hasStreaming is True

    model.clearRows()
    assert model.hasStreaming is False


def test_transcript_model_last_assistant_text_skips_notes_and_tools():
    client = FakeRpcClient()
    model = TranscriptModel(client, "s-copy")

    model.seed(
        {
            "messages": [
                {"role": "assistant", "text": "First answer"},
                {"role": "user", "text": "next"},
                {"role": "assistant", "text": "Copy this answer"},
            ]
        }
    )
    model.appendNote("local slash note")

    assert model.lastAssistantText() == "Copy this answer"

    model.clearRows()
    assert model.lastAssistantText() == ""
