"""
memory.py — MemoryModel for the Memory view.

Loads memory scopes (Global + all known projects) via ListMemoryScopes,
then loads the selected scope detail via MemoryForScope. Handles mutations:
AppendMemory, RemoveMemoryNote, RemoveAdHocMemoryNote, MoveMemoryNote,
AddBan, RemoveBan, WriteUserProfile, MemoryBackups.
"""

from typing import Optional

from PySide6.QtCore import QObject, Property, Signal, Slot

from eigenqt.rpc import RpcClient


class MemoryModel(QObject):
    """Memory model — scope picker + full memory DTO."""

    # Signals for reactive properties
    scopes_changed = Signal()
    scope_key_changed = Signal()
    current_changed = Signal()
    loading_changed = Signal()
    load_error_changed = Signal()
    action_error_changed = Signal()
    composing_changed = Signal()
    draft_changed = Signal()
    saving_changed = Signal()
    editing_profile_changed = Signal()
    profile_draft_changed = Signal()
    saving_profile_changed = Signal()
    adding_ban_changed = Signal()
    ban_title_changed = Signal()
    ban_rule_changed = Signal()
    saving_ban_changed = Signal()
    removing_ban_changed = Signal()
    removing_note_changed = Signal()
    removing_ad_hoc_changed = Signal()
    confirm_remove_note_changed = Signal()
    confirm_remove_ad_hoc_changed = Signal()
    backups_open_changed = Signal()
    backup_paths_changed = Signal()
    backups_loading_changed = Signal()
    moving_note_changed = Signal()
    move_open_changed = Signal()
    move_pending_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._scopes: list[dict] = []
        self._scope_key: str = "project"  # Default scope (cwd project)
        self._current: Optional[dict] = None
        self._loading: bool = True
        self._load_error: str = ""
        self._action_error: str = ""
        self._composing: bool = False
        self._draft: str = ""
        self._saving: bool = False
        self._editing_profile: bool = False
        self._profile_draft: str = ""
        self._saving_profile: bool = False
        self._adding_ban: bool = False
        self._ban_title: str = ""
        self._ban_rule: str = ""
        self._saving_ban: bool = False
        self._removing_ban: str = ""
        self._removing_note: int = -1
        self._removing_ad_hoc: int = -1
        self._confirm_remove_note: int = -1
        self._confirm_remove_ad_hoc: int = -1
        self._backups_open: bool = False
        self._backup_paths: list[str] = []
        self._backups_loading: bool = False
        self._moving_note: int = -1
        self._move_open: bool = False
        self._move_pending: Optional[dict] = None

        # Load on connected
        self._client.connected.connect(self._on_connected)

    # Property: scopes (list of MemoryScopeRefDTO)
    @Property(list, notify=scopes_changed)
    def scopes(self) -> list[dict]:
        return self._scopes

    @scopes.setter
    def scopes(self, value: list[dict]):
        if self._scopes != value:
            self._scopes = value
            self.scopes_changed.emit()

    # Property: scope_key (selected scope key)
    @Property(str, notify=scope_key_changed)
    def scope_key(self) -> str:
        return self._scope_key

    @scope_key.setter
    def scope_key(self, value: str):
        if self._scope_key != value:
            self._scope_key = value
            self.scope_key_changed.emit()
            self._load_scope(value)

    # Property: current (MemoryScopeDTO)
    @Property("QVariant", notify=current_changed)
    def current(self):
        return self._current

    @current.setter
    def current(self, value):
        if self._current != value:
            self._current = value
            self.current_changed.emit()

    # Property: loading
    @Property(bool, notify=loading_changed)
    def loading(self) -> bool:
        return self._loading

    @loading.setter
    def loading(self, value: bool):
        if self._loading != value:
            self._loading = value
            self.loading_changed.emit()

    # Property: load_error
    @Property(str, notify=load_error_changed)
    def load_error(self) -> str:
        return self._load_error

    @load_error.setter
    def load_error(self, value: str):
        if self._load_error != value:
            self._load_error = value
            self.load_error_changed.emit()

    # Property: action_error
    @Property(str, notify=action_error_changed)
    def action_error(self) -> str:
        return self._action_error

    @action_error.setter
    def action_error(self, value: str):
        if self._action_error != value:
            self._action_error = value
            self.action_error_changed.emit()

    # Property: composing
    @Property(bool, notify=composing_changed)
    def composing(self) -> bool:
        return self._composing

    @composing.setter
    def composing(self, value: bool):
        if self._composing != value:
            self._composing = value
            self.composing_changed.emit()

    # Property: draft
    @Property(str, notify=draft_changed)
    def draft(self) -> str:
        return self._draft

    @draft.setter
    def draft(self, value: str):
        if self._draft != value:
            self._draft = value
            self.draft_changed.emit()

    # Property: saving
    @Property(bool, notify=saving_changed)
    def saving(self) -> bool:
        return self._saving

    @saving.setter
    def saving(self, value: bool):
        if self._saving != value:
            self._saving = value
            self.saving_changed.emit()

    # Property: editing_profile
    @Property(bool, notify=editing_profile_changed)
    def editing_profile(self) -> bool:
        return self._editing_profile

    @editing_profile.setter
    def editing_profile(self, value: bool):
        if self._editing_profile != value:
            self._editing_profile = value
            self.editing_profile_changed.emit()

    # Property: profile_draft
    @Property(str, notify=profile_draft_changed)
    def profile_draft(self) -> str:
        return self._profile_draft

    @profile_draft.setter
    def profile_draft(self, value: str):
        if self._profile_draft != value:
            self._profile_draft = value
            self.profile_draft_changed.emit()

    # Property: saving_profile
    @Property(bool, notify=saving_profile_changed)
    def saving_profile(self) -> bool:
        return self._saving_profile

    @saving_profile.setter
    def saving_profile(self, value: bool):
        if self._saving_profile != value:
            self._saving_profile = value
            self.saving_profile_changed.emit()

    # Property: adding_ban
    @Property(bool, notify=adding_ban_changed)
    def adding_ban(self) -> bool:
        return self._adding_ban

    @adding_ban.setter
    def adding_ban(self, value: bool):
        if self._adding_ban != value:
            self._adding_ban = value
            self.adding_ban_changed.emit()

    # Property: ban_title
    @Property(str, notify=ban_title_changed)
    def ban_title(self) -> str:
        return self._ban_title

    @ban_title.setter
    def ban_title(self, value: str):
        if self._ban_title != value:
            self._ban_title = value
            self.ban_title_changed.emit()

    # Property: ban_rule
    @Property(str, notify=ban_rule_changed)
    def ban_rule(self) -> str:
        return self._ban_rule

    @ban_rule.setter
    def ban_rule(self, value: str):
        if self._ban_rule != value:
            self._ban_rule = value
            self.ban_rule_changed.emit()

    # Property: saving_ban
    @Property(bool, notify=saving_ban_changed)
    def saving_ban(self) -> bool:
        return self._saving_ban

    @saving_ban.setter
    def saving_ban(self, value: bool):
        if self._saving_ban != value:
            self._saving_ban = value
            self.saving_ban_changed.emit()

    # Property: removing_ban
    @Property(str, notify=removing_ban_changed)
    def removing_ban(self) -> str:
        return self._removing_ban

    @removing_ban.setter
    def removing_ban(self, value: str):
        if self._removing_ban != value:
            self._removing_ban = value
            self.removing_ban_changed.emit()

    # Property: removing_note
    @Property(int, notify=removing_note_changed)
    def removing_note(self) -> int:
        return self._removing_note

    @removing_note.setter
    def removing_note(self, value: int):
        if self._removing_note != value:
            self._removing_note = value
            self.removing_note_changed.emit()

    # Property: removing_ad_hoc
    @Property(int, notify=removing_ad_hoc_changed)
    def removing_ad_hoc(self) -> int:
        return self._removing_ad_hoc

    @removing_ad_hoc.setter
    def removing_ad_hoc(self, value: int):
        if self._removing_ad_hoc != value:
            self._removing_ad_hoc = value
            self.removing_ad_hoc_changed.emit()

    # Property: confirm_remove_note
    @Property(int, notify=confirm_remove_note_changed)
    def confirm_remove_note(self) -> int:
        return self._confirm_remove_note

    @confirm_remove_note.setter
    def confirm_remove_note(self, value: int):
        if self._confirm_remove_note != value:
            self._confirm_remove_note = value
            self.confirm_remove_note_changed.emit()

    # Property: confirm_remove_ad_hoc
    @Property(int, notify=confirm_remove_ad_hoc_changed)
    def confirm_remove_ad_hoc(self) -> int:
        return self._confirm_remove_ad_hoc

    @confirm_remove_ad_hoc.setter
    def confirm_remove_ad_hoc(self, value: int):
        if self._confirm_remove_ad_hoc != value:
            self._confirm_remove_ad_hoc = value
            self.confirm_remove_ad_hoc_changed.emit()

    # Property: backups_open
    @Property(bool, notify=backups_open_changed)
    def backups_open(self) -> bool:
        return self._backups_open

    @backups_open.setter
    def backups_open(self, value: bool):
        if self._backups_open != value:
            self._backups_open = value
            self.backups_open_changed.emit()

    # Property: backup_paths
    @Property(list, notify=backup_paths_changed)
    def backup_paths(self) -> list[str]:
        return self._backup_paths

    @backup_paths.setter
    def backup_paths(self, value: list[str]):
        if self._backup_paths != value:
            self._backup_paths = value
            self.backup_paths_changed.emit()

    # Property: backups_loading
    @Property(bool, notify=backups_loading_changed)
    def backups_loading(self) -> bool:
        return self._backups_loading

    @backups_loading.setter
    def backups_loading(self, value: bool):
        if self._backups_loading != value:
            self._backups_loading = value
            self.backups_loading_changed.emit()

    # Property: moving_note
    @Property(int, notify=moving_note_changed)
    def moving_note(self) -> int:
        return self._moving_note

    @moving_note.setter
    def moving_note(self, value: int):
        if self._moving_note != value:
            self._moving_note = value
            self.moving_note_changed.emit()

    # Property: move_open
    @Property(bool, notify=move_open_changed)
    def move_open(self) -> bool:
        return self._move_open

    @move_open.setter
    def move_open(self, value: bool):
        if self._move_open != value:
            self._move_open = value
            self.move_open_changed.emit()

    # Property: move_pending
    @Property("QVariant", notify=move_pending_changed)
    def move_pending(self):
        return self._move_pending

    @move_pending.setter
    def move_pending(self, value):
        if self._move_pending != value:
            self._move_pending = value
            self.move_pending_changed.emit()

    # Derived property: is_global (computed from current.scope)
    @Property(bool, notify=current_changed)
    def is_global(self) -> bool:
        return (self._current or {}).get("scope") == "global"

    # Derived property: scope_label (friendly name)
    @Property(str, notify=current_changed)
    def scope_label(self) -> str:
        for s in self._scopes:
            if s.get("key") == self._scope_key:
                return s.get("name", "")
        return (self._current or {}).get("scope", self._scope_key)

    # Derived property: isEmpty
    @Property(bool, notify=current_changed)
    def is_empty(self) -> bool:
        if not self._current:
            return True
        note_count = self._current.get("noteCount", 0)
        ad_hoc = self._current.get("adHoc", [])
        summary = self._current.get("summary", "")
        bans = self._current.get("banList", [])
        profile = self._current.get("profile", "")
        profile_learned = self._current.get("profileLearned", "")
        return (
            note_count == 0
            and len(ad_hoc) == 0
            and not summary
            and len(bans) == 0
            and not profile
            and not profile_learned
        )

    # Derived property: hasBackupHistory
    @Property(bool, notify=current_changed)
    def has_backup_history(self) -> bool:
        return (self._current or {}).get("backups", 0) > 0

    # Derived property: moveTargets (all scopes except current)
    @Property(list, notify=scopes_changed)
    def move_targets(self) -> list[dict]:
        return [s for s in self._scopes if s.get("key") != self._scope_key]

    def _err_text(self, result: dict | None) -> str:
        if not result or "error" not in result:
            return "unknown error"
        e = result.get("error")
        if isinstance(e, str):
            return e or "unknown error"
        if isinstance(e, dict):
            return e.get("message") or "unknown error"
        return str(e) if e else "unknown error"

    def _fail_action(self, label: str, result: dict | None):
        self.action_error = f"{label}: {self._err_text(result)}"

    @Slot()
    def _on_connected(self):
        """Load scopes + initial scope on connect."""
        self._client.call("ListMemoryScopes", callback=self._on_scopes_result)

    @Slot(dict)
    def _on_scopes_result(self, result: dict):
        """Handle ListMemoryScopes RPC result."""
        if "error" in result:
            self.load_error = self._err_text(result)
            return

        refs = result.get("result") or []
        self.scopes = refs

        # Resolve "project" alias to canonical key
        selected_key = self._scope_key
        for r in refs:
            if r.get("current"):
                selected_key = r.get("key", "project")
                break
        if selected_key != self._scope_key:
            self._scope_key = selected_key
            self.scope_key_changed.emit()

        # Load the default scope
        self._load_scope(self._scope_key)

    def _load_scope(self, key: str):
        """Load a scope via MemoryForScope."""
        self.loading = True
        self.load_error = ""
        self._client.call("MemoryForScope", key, callback=self._on_scope_result)

    @Slot(dict)
    def _on_scope_result(self, result: dict):
        """Handle MemoryForScope RPC result."""
        self.loading = False
        if "error" in result:
            e = result.get("error")
            if isinstance(e, str):
                self.load_error = e or "Unknown error"
            elif isinstance(e, dict):
                self.load_error = e.get("message", "Unknown error")
            else:
                self.load_error = str(e) if e else "Unknown error"
            return

        self.current = result.get("result")

    @Slot(str)
    def select_scope(self, key: str):
        """Switch scope (user dropdown selection)."""
        if key == self._scope_key:
            return
        self.composing = False
        self.confirm_remove_note = -1
        self.confirm_remove_ad_hoc = -1
        self.action_error = ""
        self.scope_key = key

    @Slot()
    def save_note(self):
        """Append a new note to the current scope."""
        note = (self._draft or "").strip()
        if not note:
            return

        self.action_error = ""
        self.saving = True
        self._client.call(
            "AppendMemory", self._scope_key, note, callback=self._on_save_note_result
        )

    @Slot(dict)
    def _on_save_note_result(self, result: dict):
        """Handle AppendMemory result."""
        self.saving = False
        if "error" in result:
            self._fail_action("Could not save note", result)
            return

        self.draft = ""
        self.composing = False
        # Reload scope
        self._load_scope(self._scope_key)

    @Slot(int)
    def remove_note(self, index: int):
        """Remove a distilled note by index."""
        self.action_error = ""
        self.removing_note = index
        self.confirm_remove_note = -1
        self._client.call(
            "RemoveMemoryNote",
            self._scope_key,
            index,
            callback=self._on_remove_note_result,
        )

    @Slot(dict)
    def _on_remove_note_result(self, result: dict):
        """Handle RemoveMemoryNote result."""
        self.removing_note = -1
        if "error" in result:
            self._fail_action("Could not remove note", result)
            return

        # Reload scope
        self._load_scope(self._scope_key)

    @Slot(int)
    def remove_ad_hoc(self, index: int):
        """Remove an ad-hoc saved note by index."""
        self.action_error = ""
        self.removing_ad_hoc = index
        self.confirm_remove_ad_hoc = -1
        self._client.call(
            "RemoveAdHocMemoryNote",
            self._scope_key,
            index,
            callback=self._on_remove_ad_hoc_result,
        )

    @Slot(dict)
    def _on_remove_ad_hoc_result(self, result: dict):
        """Handle RemoveAdHocMemoryNote result."""
        self.removing_ad_hoc = -1
        if "error" in result:
            self._fail_action("Could not remove saved note", result)
            return

        # Reload scope
        self._load_scope(self._scope_key)

    @Slot(str, int)
    def open_move(self, note_text: str, idx: int):
        """Open the move-note picker."""
        self.move_pending = {"text": note_text, "idx": idx}
        self.move_open = True

    @Slot(str, str)
    def move_to(self, dst_key: str, dst_name: str):
        """Move the pending note to another scope."""
        if not self._move_pending:
            return

        text = self._move_pending.get("text", "")
        idx = self._move_pending.get("idx", -1)
        self.move_open = False
        self.moving_note = idx
        self.action_error = ""

        self._client.call(
            "MoveMemoryNote",
            self._scope_key,
            dst_key,
            text,
            callback=self._on_move_note_result,
        )

    @Slot(dict)
    def _on_move_note_result(self, result: dict):
        """Handle MoveMemoryNote result."""
        self.moving_note = -1
        if "error" in result:
            self.move_open = True
            self._fail_action("Could not move note", result)
            return

        self.move_pending = None
        # Reload current scope
        self._load_scope(self._scope_key)

    @Slot()
    def add_ban(self):
        """Add a ban to the current scope."""
        title = (self._ban_title or "").strip()
        rule = (self._ban_rule or "").strip()
        if not title or not rule:
            return

        self.action_error = ""
        self.saving_ban = True
        self._client.call(
            "AddBan",
            self._scope_key,
            title,
            rule,
            callback=self._on_add_ban_result,
        )

    @Slot(dict)
    def _on_add_ban_result(self, result: dict):
        """Handle AddBan result."""
        self.saving_ban = False
        if "error" in result:
            self._fail_action("Could not save ban", result)
            return

        self.ban_title = ""
        self.ban_rule = ""
        self.adding_ban = False
        # Reload scope
        self._load_scope(self._scope_key)

    @Slot(str)
    def remove_ban(self, title: str):
        """Remove a ban by title."""
        self.action_error = ""
        self.removing_ban = title
        self._client.call(
            "RemoveBan",
            self._scope_key,
            title,
            callback=self._on_remove_ban_result,
        )

    @Slot(dict)
    def _on_remove_ban_result(self, result: dict):
        """Handle RemoveBan result."""
        self.removing_ban = ""
        if "error" in result:
            self._fail_action("Could not remove ban", result)
            return

        # Reload scope
        self._load_scope(self._scope_key)

    @Slot()
    def start_profile(self):
        """Start editing the USER.md profile."""
        self.profile_draft = (self._current or {}).get("profile", "")
        self.editing_profile = True

    @Slot()
    def save_profile(self):
        """Save the USER.md profile."""
        self.action_error = ""
        self.saving_profile = True
        self._client.call(
            "WriteUserProfile",
            self._profile_draft,
            callback=self._on_save_profile_result,
        )

    @Slot(dict)
    def _on_save_profile_result(self, result: dict):
        """Handle WriteUserProfile result."""
        self.saving_profile = False
        if "error" in result:
            self._fail_action("Could not save profile", result)
            return

        self.editing_profile = False
        # Reload scope
        self._load_scope(self._scope_key)

    @Slot()
    def toggle_backups(self):
        """Toggle the backups list visibility."""
        self.backups_open = not self._backups_open
        if not self._backups_open:
            self.action_error = ""
        if self._backups_open:
            self._load_backups()

    def _load_backups(self):
        """Load backup paths for the current scope."""
        self.action_error = ""
        self.backups_loading = True
        self._client.call(
            "MemoryBackups",
            self._scope_key,
            callback=self._on_backups_result,
        )

    @Slot(dict)
    def _on_backups_result(self, result: dict):
        """Handle MemoryBackups result."""
        self.backups_loading = False
        if "error" in result:
            self._fail_action("Could not load memory backups", result)
            return

        paths = result.get("result") or []
        # Go returns oldest-first; reverse for newest-first
        self.backup_paths = list(reversed(paths))

    @Slot(str, result=str)
    def backup_name(self, path: str) -> str:
        """Extract filename from backup path."""
        parts = path.split("/")
        return parts[-1] if parts else path

    @Slot(str, result=str)
    def backup_when(self, path: str) -> str:
        """Format backup timestamp from filename."""
        import re

        name = self.backup_name(path)
        m = re.match(r".*\.(\d{8})-(\d{6})\.bak$", name)
        if not m:
            return name

        d, t = m.groups()
        date = f"{d[0:4]}-{d[4:6]}-{d[6:8]}"
        time = f"{t[0:2]}:{t[2:4]}:{t[4:6]}"
        return f"{date} {time}"

    @Slot()
    def clear_action_error(self):
        self.action_error = ""

    @Slot(str, result=str)
    def short_dir(self, d: str) -> str:
        """Extract basename from directory path."""
        d = d.rstrip("/")
        parts = d.split("/")
        return parts[-1] if parts else d
