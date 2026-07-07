"""Test MemoryModel's Qt-facing RPC contract."""

import pytest
from PySide6.QtCore import QCoreApplication

from eigenqt.models.memory import MemoryModel


class FakeSignal:
    def connect(self, _):
        pass


class FakeClient:
    def __init__(self):
        self.connected = FakeSignal()
        self.calls = []

    def call(self, method, *args, callback=None):
        self.calls.append({"method": method, "args": args, "callback": callback})


@pytest.fixture
def qt_app():
    app = QCoreApplication.instance()
    if app is None:
        app = QCoreApplication([])
    yield app


@pytest.fixture
def client():
    return FakeClient()


@pytest.fixture
def model(qt_app, client):
    return MemoryModel(client)


def test_scopes_resolve_current_scope_once(model, client):
    """ListMemoryScopes sets the canonical project key and fetches it once."""
    model._on_scopes_result(
        {
            "result": [
                {"key": "global", "name": "Global", "dir": "", "current": False},
                {
                    "key": "project:/home/user/eigen",
                    "name": "eigen",
                    "dir": "/home/user/eigen",
                    "current": True,
                },
            ]
        }
    )

    scope_loads = [c for c in client.calls if c["method"] == "MemoryForScope"]
    assert model.scope_key == "project:/home/user/eigen"
    assert len(scope_loads) == 1
    assert scope_loads[0]["args"] == ("project:/home/user/eigen",)
    assert model.move_targets == [
        {"key": "global", "name": "Global", "dir": "", "current": False}
    ]


def test_select_scope_resets_ui_state_and_loads_scope(model, client):
    model.composing = True
    model.confirm_remove_note = 2
    model.confirm_remove_ad_hoc = 3

    model.select_scope("global")

    assert model.scope_key == "global"
    assert model.composing is False
    assert model.confirm_remove_note == -1
    assert model.confirm_remove_ad_hoc == -1
    assert client.calls[-1]["method"] == "MemoryForScope"
    assert client.calls[-1]["args"] == ("global",)


def test_scope_payload_drives_derived_state(model):
    model._on_scope_result(
        {
            "result": {
                "scope": "global",
                "summary": "Known preferences",
                "notes": [{"index": 0, "text": "Use focused diffs."}],
                "adHoc": [],
                "noteCount": 1,
                "profile": "",
                "profileLearned": "",
                "banList": [],
                "backups": 2,
            }
        }
    )

    assert model.loading is False
    assert model.load_error == ""
    assert model.current["summary"] == "Known preferences"
    assert model.is_global is True
    assert model.is_empty is False
    assert model.has_backup_history is True


def test_save_note_trims_note_and_reloads_scope(model, client):
    model.scope_key = "global"
    client.calls.clear()
    model.composing = True
    model.draft = "  keep the Qt follow-up small  "

    model.save_note()

    assert model.saving is True
    assert client.calls[-1]["method"] == "AppendMemory"
    assert client.calls[-1]["args"] == ("global", "keep the Qt follow-up small")

    client.calls[-1]["callback"]({"result": True})
    assert model.saving is False
    assert model.draft == ""
    assert model.composing is False
    assert client.calls[-1]["method"] == "MemoryForScope"
    assert client.calls[-1]["args"] == ("global",)


def test_save_note_error_keeps_draft_and_surfaces_action_error(model, client):
    model.scope_key = "global"
    client.calls.clear()
    model.composing = True
    model.draft = "  keep this note  "

    model.save_note()
    client.calls[-1]["callback"]({"error": "write denied"})

    assert model.saving is False
    assert model.composing is True
    assert model.draft == "  keep this note  "
    assert model.action_error == "Could not save note: write denied"


def test_profile_and_ban_errors_keep_forms_retryable(model, client):
    model.scope_key = "global"
    client.calls.clear()

    model.editing_profile = True
    model.profile_draft = "Profile draft"
    model.save_profile()
    client.calls[-1]["callback"]({"error": {"message": "profile denied"}})

    assert model.saving_profile is False
    assert model.editing_profile is True
    assert model.profile_draft == "Profile draft"
    assert model.action_error == "Could not save profile: profile denied"

    model.adding_ban = True
    model.ban_title = "No drift"
    model.ban_rule = "Stay scoped."
    model.add_ban()
    client.calls[-1]["callback"]({"error": "ban denied"})

    assert model.saving_ban is False
    assert model.adding_ban is True
    assert model.ban_title == "No drift"
    assert model.ban_rule == "Stay scoped."
    assert model.action_error == "Could not save ban: ban denied"


def test_pending_memory_writes_own_retry_forms(model, client):
    model.scope_key = "global"
    client.calls.clear()

    model.composing = True
    model.draft = "Pending memory note"
    model.save_note()
    first_note_callback = client.calls[-1]["callback"]
    model.save_note()

    assert model.saving is True
    assert [call["method"] for call in client.calls].count("AppendMemory") == 1

    model.composing = False
    first_note_callback({"error": "write denied"})

    assert model.saving is False
    assert model.composing is True
    assert model.draft == "Pending memory note"
    assert model.action_error == "Could not save note: write denied"

    model.editing_profile = True
    model.profile_draft = "Pending profile"
    model.save_profile()
    first_profile_callback = client.calls[-1]["callback"]
    model.save_profile()

    assert model.saving_profile is True
    assert [call["method"] for call in client.calls].count("WriteUserProfile") == 1

    model.editing_profile = False
    first_profile_callback({"error": "profile denied"})

    assert model.saving_profile is False
    assert model.editing_profile is True
    assert model.profile_draft == "Pending profile"
    assert model.action_error == "Could not save profile: profile denied"

    model.adding_ban = True
    model.ban_title = "Pending ban"
    model.ban_rule = "Do not hide pending ban forms."
    model.add_ban()
    first_ban_callback = client.calls[-1]["callback"]
    model.add_ban()

    assert model.saving_ban is True
    assert [call["method"] for call in client.calls].count("AddBan") == 1

    model.adding_ban = False
    first_ban_callback({"error": "ban denied"})

    assert model.saving_ban is False
    assert model.adding_ban is True
    assert model.ban_title == "Pending ban"
    assert model.ban_rule == "Do not hide pending ban forms."
    assert model.action_error == "Could not save ban: ban denied"


def test_backup_helpers_use_the_model_implementation(model, client):
    model.scope_key = "global"
    client.calls.clear()

    model.toggle_backups()
    assert model.backups_open is True
    assert model.backups_loading is True
    assert client.calls[-1]["method"] == "MemoryBackups"

    client.calls[-1]["callback"](
        {
            "result": [
                "/tmp/MEMORY.md.20240101-010203.bak",
                "/tmp/MEMORY.md.20240102-030405.bak",
            ]
        }
    )
    assert model.backups_loading is False
    assert model.backup_paths == [
        "/tmp/MEMORY.md.20240102-030405.bak",
        "/tmp/MEMORY.md.20240101-010203.bak",
    ]
    assert model.backup_name(model.backup_paths[0]) == "MEMORY.md.20240102-030405.bak"
    assert model.backup_when(model.backup_paths[0]) == "2024-01-02 03:04:05"
    assert model.short_dir("/home/user/eigen/") == "eigen"
