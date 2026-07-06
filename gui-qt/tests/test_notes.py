"""
test_notes.py — Pytest for NotesModel and NotesController logic.
"""

import pytest
from unittest.mock import Mock
from PySide6.QtCore import QCoreApplication

from eigenqt.models.notes import NotesModel, NotesController
from eigenqt.rpc import RpcClient


@pytest.fixture
def qt_app():
    """Create QCoreApplication for Qt tests."""
    app = QCoreApplication.instance()
    if app is None:
        app = QCoreApplication([])
    yield app


@pytest.fixture
def mock_client():
    """Mock RpcClient."""
    client = Mock(spec=RpcClient)
    client.connected = Mock()
    client.connected.connect = Mock()
    return client


def test_notes_model_empty(qt_app, mock_client):
    """NotesModel starts empty."""
    model = NotesModel(mock_client)
    assert model.rowCount() == 0
    assert not model.loading
    assert model.error == ""


def test_notes_model_load(qt_app, mock_client):
    """NotesModel.load() triggers RPC call and loads notes."""
    model = NotesModel(mock_client)
    model.load("test")

    # Check that ObsidianNotes was called
    mock_client.call.assert_called_once()
    args = mock_client.call.call_args[0]
    assert args[0] == "ObsidianNotes"
    assert args[1] == "test"

    # Simulate RPC result
    callback = mock_client.call.call_args[1]["callback"]
    callback(
        {
            "result": [
                {"path": "Inbox/Note1.md", "title": "Note1"},
                {"path": "Inbox/Note2.md", "title": "Note2"},
            ]
        }
    )

    assert model.rowCount() == 2
    assert not model.loading
    assert model.error == ""

    # Check data
    idx = model.index(0, 0)
    assert model.data(idx, model.PathRole) == "Inbox/Note1.md"
    assert model.data(idx, model.TitleRole) == "Note1"


def test_notes_model_load_error(qt_app, mock_client):
    """NotesModel handles error response."""
    model = NotesModel(mock_client)
    model.load("")

    callback = mock_client.call.call_args[1]["callback"]
    callback({"error": "vault not configured"})

    assert model.rowCount() == 0
    assert not model.loading
    assert model.error == "vault not configured"


def test_notes_controller_status(qt_app, mock_client):
    """NotesController fetches status on connect."""
    controller = NotesController(mock_client)

    # Simulate connected signal
    assert mock_client.connected.connect.called

    # Trigger _on_connected manually
    controller._on_connected()
    mock_client.call.assert_called_once()
    args = mock_client.call.call_args[0]
    assert args[0] == "ObsidianStatus"

    # Simulate status result (vault available)
    callback = mock_client.call.call_args[1]["callback"]
    callback({"result": {"available": True, "vault": "/home/user/vault"}})

    assert controller.available
    assert controller.vault == "/home/user/vault"


def test_notes_controller_open_note(qt_app, mock_client):
    """NotesController.open_note() fetches content."""
    controller = NotesController(mock_client)
    controller.status = {"available": True, "vault": "/home/user/vault"}

    controller.open_note("Inbox/Note.md", "Note")

    # Check selected
    assert controller.selected == {"path": "Inbox/Note.md", "title": "Note"}
    assert not controller.editing
    assert controller.content == ""

    # Check ObsidianRead was called
    args = [c[0] for c in mock_client.call.call_args_list]
    assert ("ObsidianRead", "Inbox/Note.md") in args

    # Simulate content result
    callback = mock_client.call.call_args[1]["callback"]
    callback({"result": "# Note\n\nContent here."})

    assert controller.content == "# Note\n\nContent here."
    assert controller.action_error == ""


def test_notes_controller_open_note_error_surfaces_action_error(qt_app, mock_client):
    """Open-note errors stay visible to the UI."""
    controller = NotesController(mock_client)
    controller.status = {"available": True, "vault": "/home/user/vault"}

    controller.open_note("Inbox/Missing.md", "Missing")

    callback = mock_client.call.call_args[1]["callback"]
    callback({"error": {"message": "read denied"}})

    assert controller.selected == {"path": "Inbox/Missing.md", "title": "Missing"}
    assert controller.content == ""
    assert controller.action_error == "Could not open Inbox/Missing.md: read denied"


def test_notes_controller_ignores_stale_read_results(qt_app, mock_client):
    """A slow first note read must not overwrite a later selected note."""
    controller = NotesController(mock_client)
    controller.status = {"available": True, "vault": "/home/user/vault"}

    controller.open_note("Inbox/First.md", "First")
    first_callback = mock_client.call.call_args_list[-1][1]["callback"]

    controller.open_note("Inbox/Second.md", "Second")
    second_callback = mock_client.call.call_args_list[-1][1]["callback"]

    first_callback({"result": "# First\n\nLate result."})

    assert controller.selected == {"path": "Inbox/Second.md", "title": "Second"}
    assert controller.content == ""
    assert controller.action_error == ""

    first_callback({"error": "late denied"})
    assert controller.content == ""
    assert controller.action_error == ""

    second_callback({"result": "# Second\n\nCurrent result."})

    assert controller.selected == {"path": "Inbox/Second.md", "title": "Second"}
    assert controller.content == "# Second\n\nCurrent result."
    assert controller.action_error == ""


def test_notes_controller_edit_save(qt_app, mock_client):
    """NotesController edit + save flow."""
    controller = NotesController(mock_client)
    controller.selected = {"path": "Inbox/Note.md", "title": "Note"}
    controller.content = "# Note\n\nOriginal."

    # Start edit
    controller.start_edit()
    assert controller.editing
    assert controller.draft == "# Note\n\nOriginal."

    # Modify draft
    controller.draft = "# Note\n\nEdited."

    # Save
    controller.save()
    assert controller.saving

    # Check ObsidianWrite was called
    args = mock_client.call.call_args[0]
    assert args[0] == "ObsidianWrite"
    assert args[1] == "Inbox/Note.md"
    assert args[2] == "# Note\n\nEdited."
    assert args[3] is False

    # Simulate success
    callback = mock_client.call.call_args[1]["callback"]
    callback({"result": "Inbox/Note.md"})

    assert not controller.saving
    assert not controller.editing
    assert controller.content == "# Note\n\nEdited."
    assert controller.action_error == ""


def test_notes_controller_save_error_keeps_draft_editing(qt_app, mock_client):
    """Failed saves keep the editor and draft intact for retry."""
    controller = NotesController(mock_client)
    controller.selected = {"path": "Inbox/Note.md", "title": "Note"}
    controller.content = "# Note\n\nOriginal."
    controller.start_edit()
    controller.draft = "# Note\n\nEdited."

    controller.save()

    callback = mock_client.call.call_args[1]["callback"]
    callback({"error": "write denied"})

    assert not controller.saving
    assert controller.editing
    assert controller.draft == "# Note\n\nEdited."
    assert controller.content == "# Note\n\nOriginal."
    assert controller.action_error == "Could not save Inbox/Note.md: write denied"


def test_notes_controller_create_note(qt_app, mock_client):
    """NotesController.create_note() creates a new note."""
    controller = NotesController(mock_client)
    controller.status = {"available": True, "vault": "/home/user/vault"}

    controller.start_create()
    assert controller.creating

    controller.new_name = "Ideas/NewIdea.md"
    controller.create_note()

    # Check ObsidianWrite was called with template
    args = mock_client.call.call_args[0]
    assert args[0] == "ObsidianWrite"
    assert args[1] == "Ideas/NewIdea.md"
    assert args[2] == "# Ideas/NewIdea\n\n"
    assert args[3] is False

    # Simulate success
    callback = mock_client.call.call_args[1]["callback"]
    callback({"result": "Ideas/NewIdea.md"})

    assert not controller.creating
    assert controller.new_name == ""
    assert not controller.creating_busy
    assert controller.action_error == ""


def test_notes_controller_create_error_keeps_composer(qt_app, mock_client):
    """Failed note creation keeps the pending path visible for retry."""
    controller = NotesController(mock_client)
    controller.status = {"available": True, "vault": "/home/user/vault"}

    controller.start_create()
    controller.new_name = "Ideas/NewIdea.md"
    controller.create_note()

    assert controller.creating_busy

    callback = mock_client.call.call_args[1]["callback"]
    callback({"error": {"message": "write denied"}})

    assert controller.creating
    assert not controller.creating_busy
    assert controller.new_name == "Ideas/NewIdea.md"
    assert controller.action_error == "Could not create Ideas/NewIdea.md: write denied"
