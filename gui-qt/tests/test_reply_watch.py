"""
test_reply_watch.py — Tests for ReplyWatcher transition detection.

Tests the core logic: working/approval → idle transitions trigger notifications
when the session is NOT the currently-open one.
"""
import pytest
from unittest.mock import MagicMock, call
from PySide6.QtCore import QObject

from eigenqt.models.reply_watch import ReplyWatcher, ACTIVE_STATUSES


@pytest.fixture
def mock_client():
    """Create a mock RpcClient."""
    client = MagicMock()
    client.call = MagicMock()
    return client


@pytest.fixture
def mock_sessions_model():
    """Create a mock SessionsModel."""
    model = MagicMock()
    model.IdRole = 1
    model.TitleRole = 2
    model.DirRole = 3
    model.StatusRole = 5
    model.rowCount = MagicMock(return_value=0)
    model.index = MagicMock()
    model.data = MagicMock()
    model.dataChanged = MagicMock()
    model.modelReset = MagicMock()
    return model


@pytest.fixture
def reply_watcher(mock_client, mock_sessions_model):
    """Create a ReplyWatcher instance."""
    watcher = ReplyWatcher(mock_client, mock_sessions_model)
    return watcher


def test_active_statuses_constant():
    """Verify ACTIVE_STATUSES contains working and approval."""
    assert "working" in ACTIVE_STATUSES
    assert "approval" in ACTIVE_STATUSES


def test_reply_watcher_init(reply_watcher, mock_sessions_model):
    """Test ReplyWatcher initialization."""
    assert reply_watcher._current_session_id == ""
    assert reply_watcher._window_focused is True
    assert len(reply_watcher._unread_ids) == 0


def test_set_current_session(reply_watcher):
    """Test setting the current session."""
    reply_watcher.set_current_session("session-123")
    assert reply_watcher._current_session_id == "session-123"


def test_set_current_session_clears_unread(reply_watcher):
    """Test that opening a session clears its unread status."""
    # Mark unread first
    reply_watcher._unread_ids.add("session-123")
    assert reply_watcher.is_unread("session-123")

    # Open the session
    reply_watcher.set_current_session("session-123")

    # Unread should be cleared
    assert not reply_watcher.is_unread("session-123")


def test_transition_working_to_idle_triggers_notify(
    reply_watcher, mock_client, mock_sessions_model
):
    """Test that working → idle transition triggers notification."""
    # Setup: session in working state
    session_id = "session-abc"
    reply_watcher._prev_status[session_id] = "working"

    # Mock sessions model with one session transitioning to idle
    mock_sessions_model.rowCount.return_value = 1
    mock_index = MagicMock()
    mock_sessions_model.index.return_value = mock_index

    def data_side_effect(index, role):
        if role == mock_sessions_model.IdRole:
            return session_id
        if role == mock_sessions_model.StatusRole:
            return "idle"
        if role == mock_sessions_model.TitleRole:
            return "Test Session"
        return ""

    mock_sessions_model.data.side_effect = data_side_effect

    # Trigger check
    reply_watcher._check_all_sessions()

    # Verify NotifyChatReply was called
    mock_client.call.assert_called_once()
    call_args = mock_client.call.call_args
    assert call_args[0][0] == "NotifyChatReply"
    assert call_args[0][1] == session_id
    assert call_args[0][2] == "Test Session"

    # Verify session was marked unread
    assert reply_watcher.is_unread(session_id)


def test_transition_approval_to_idle_triggers_notify(
    reply_watcher, mock_client, mock_sessions_model
):
    """Test that approval → idle transition triggers notification."""
    session_id = "session-xyz"
    reply_watcher._prev_status[session_id] = "approval"

    mock_sessions_model.rowCount.return_value = 1
    mock_index = MagicMock()
    mock_sessions_model.index.return_value = mock_index

    def data_side_effect(index, role):
        if role == mock_sessions_model.IdRole:
            return session_id
        if role == mock_sessions_model.StatusRole:
            return "idle"
        if role == mock_sessions_model.TitleRole:
            return "Approval Test"
        return ""

    mock_sessions_model.data.side_effect = data_side_effect

    reply_watcher._check_all_sessions()

    # Verify NotifyChatReply was called
    assert mock_client.call.call_count == 1
    assert reply_watcher.is_unread(session_id)


def test_transition_idle_to_idle_no_notify(
    reply_watcher, mock_client, mock_sessions_model
):
    """Test that idle → idle does not trigger notification."""
    session_id = "session-456"
    reply_watcher._prev_status[session_id] = "idle"

    mock_sessions_model.rowCount.return_value = 1
    mock_index = MagicMock()
    mock_sessions_model.index.return_value = mock_index

    def data_side_effect(index, role):
        if role == mock_sessions_model.IdRole:
            return session_id
        if role == mock_sessions_model.StatusRole:
            return "idle"
        return ""

    mock_sessions_model.data.side_effect = data_side_effect

    reply_watcher._check_all_sessions()

    # No notification should be sent
    mock_client.call.assert_not_called()
    assert not reply_watcher.is_unread(session_id)


def test_current_session_transition_no_notify(
    reply_watcher, mock_client, mock_sessions_model
):
    """Test that transitions for the currently-open session don't notify."""
    session_id = "current-session"
    reply_watcher._prev_status[session_id] = "working"
    reply_watcher.set_current_session(session_id)

    mock_sessions_model.rowCount.return_value = 1
    mock_index = MagicMock()
    mock_sessions_model.index.return_value = mock_index

    def data_side_effect(index, role):
        if role == mock_sessions_model.IdRole:
            return session_id
        if role == mock_sessions_model.StatusRole:
            return "idle"
        return ""

    mock_sessions_model.data.side_effect = data_side_effect

    reply_watcher._check_all_sessions()

    # No notification for current session
    mock_client.call.assert_not_called()
    assert not reply_watcher.is_unread(session_id)


def test_no_previous_status_no_notify(
    reply_watcher, mock_client, mock_sessions_model
):
    """Test that sessions without previous status don't trigger notification."""
    session_id = "new-session"
    # No prev_status set

    mock_sessions_model.rowCount.return_value = 1
    mock_index = MagicMock()
    mock_sessions_model.index.return_value = mock_index

    def data_side_effect(index, role):
        if role == mock_sessions_model.IdRole:
            return session_id
        if role == mock_sessions_model.StatusRole:
            return "idle"
        return ""

    mock_sessions_model.data.side_effect = data_side_effect

    reply_watcher._check_all_sessions()

    # No notification for first-seen session
    mock_client.call.assert_not_called()
    assert not reply_watcher.is_unread(session_id)


def test_title_fallback_to_dir(reply_watcher, mock_client, mock_sessions_model):
    """Test that title falls back to directory basename if empty."""
    session_id = "session-dir-test"
    reply_watcher._prev_status[session_id] = "working"

    mock_sessions_model.rowCount.return_value = 1
    mock_index = MagicMock()
    mock_sessions_model.index.return_value = mock_index

    def data_side_effect(index, role):
        if role == mock_sessions_model.IdRole:
            return session_id
        if role == mock_sessions_model.StatusRole:
            return "idle"
        if role == mock_sessions_model.TitleRole:
            return ""  # Empty title
        if role == mock_sessions_model.DirRole:
            return "/home/user/projects/myapp/"
        return ""

    mock_sessions_model.data.side_effect = data_side_effect

    reply_watcher._check_all_sessions()

    # Verify title was extracted from dir
    call_args = mock_client.call.call_args
    assert call_args[0][2] == "myapp"


def test_title_fallback_to_chat(reply_watcher, mock_client, mock_sessions_model):
    """Test that title falls back to 'Chat' if both title and dir are empty."""
    session_id = "session-no-meta"
    reply_watcher._prev_status[session_id] = "working"

    mock_sessions_model.rowCount.return_value = 1
    mock_index = MagicMock()
    mock_sessions_model.index.return_value = mock_index

    def data_side_effect(index, role):
        if role == mock_sessions_model.IdRole:
            return session_id
        if role == mock_sessions_model.StatusRole:
            return "idle"
        if role == mock_sessions_model.TitleRole:
            return ""
        if role == mock_sessions_model.DirRole:
            return ""
        return ""

    mock_sessions_model.data.side_effect = data_side_effect

    reply_watcher._check_all_sessions()

    # Verify title defaulted to "Chat"
    call_args = mock_client.call.call_args
    assert call_args[0][2] == "Chat"


def test_reconcile_sessions(reply_watcher):
    """Test reconcile removes unread markers for deleted sessions."""
    reply_watcher._unread_ids.add("session-1")
    reply_watcher._unread_ids.add("session-2")
    reply_watcher._unread_ids.add("session-3")

    # Only session-1 and session-3 exist now
    reply_watcher.reconcile_sessions(["session-1", "session-3"])

    assert reply_watcher.is_unread("session-1")
    assert not reply_watcher.is_unread("session-2")  # Removed
    assert reply_watcher.is_unread("session-3")


def test_multiple_sessions_multiple_transitions(
    reply_watcher, mock_client, mock_sessions_model
):
    """Test handling multiple sessions transitioning simultaneously."""
    # Setup two sessions transitioning
    reply_watcher._prev_status["session-a"] = "working"
    reply_watcher._prev_status["session-b"] = "approval"

    mock_sessions_model.rowCount.return_value = 2
    mock_index = MagicMock()
    mock_sessions_model.index.return_value = mock_index

    def data_side_effect(index, role):
        row = index.row() if hasattr(index, "row") else 0
        if role == mock_sessions_model.IdRole:
            return f"session-{'a' if row == 0 else 'b'}"
        if role == mock_sessions_model.StatusRole:
            return "idle"
        if role == mock_sessions_model.TitleRole:
            return f"Session {'A' if row == 0 else 'B'}"
        return ""

    mock_sessions_model.data.side_effect = data_side_effect

    # Create proper index mocks
    def index_func(row, col):
        idx = MagicMock()
        idx.row.return_value = row
        return idx

    mock_sessions_model.index = index_func

    reply_watcher._check_all_sessions()

    # Both should be notified
    assert mock_client.call.call_count == 2
    assert reply_watcher.is_unread("session-a")
    assert reply_watcher.is_unread("session-b")
