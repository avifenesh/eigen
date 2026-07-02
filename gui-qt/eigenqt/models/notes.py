"""
notes.py — NotesModel for the Notes view (Obsidian vault browser).

Loads ObsidianStatus to check vault availability, then ObsidianNotes(query) to
list notes. Selected note content is fetched via ObsidianRead(path). New notes
are created via ObsidianWrite(name, template, False). Read-only against the real
vault; note creation is pytest-verified only (not exercised through the UI).
"""

from typing import Optional

from PySide6.QtCore import QAbstractListModel, QModelIndex, QObject, Property, Qt, Signal, Slot

from eigenqt.rpc import RpcClient


class NotesModel(QAbstractListModel):
    """Note list model — NoteDTO {path, title} from ObsidianNotes."""

    # Qt roles for NoteDTO
    PathRole = Qt.UserRole + 1
    TitleRole = Qt.UserRole + 2

    loadingChanged = Signal()
    errorChanged = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._notes: list[dict] = []
        self._loading = False
        self._error = ""

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.PathRole: b"path",
            self.TitleRole: b"title",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count (notes list length)."""
        if parent.isValid():
            return 0
        return len(self._notes)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._notes):
            return None

        note = self._notes[index.row()]
        if role == self.PathRole:
            return note.get("path", "")
        if role == self.TitleRole:
            return note.get("title", "")
        return None

    @Property(bool, notify=loadingChanged)
    def loading(self):
        """Loading state."""
        return self._loading

    @Property(str, notify=errorChanged)
    def error(self):
        """Error message (empty if none)."""
        return self._error

    @Slot(str)
    def load(self, query: str):
        """Fetch notes matching query from daemon."""
        self._loading = True
        self.loadingChanged.emit()
        self._error = ""
        self.errorChanged.emit()

        self._client.call("ObsidianNotes", query, callback=self._on_notes_result)

    def _on_notes_result(self, result: dict):
        """Handle ObsidianNotes RPC result."""
        self._loading = False
        self.loadingChanged.emit()

        if "error" in result:
            err = result.get("error", "")
            # Error can be a string or dict
            if isinstance(err, dict):
                self._error = err.get("message", "Unknown error")
            else:
                self._error = str(err) or "Unknown error"
            self.errorChanged.emit()
            return

        notes = result.get("result") or []
        self.beginResetModel()
        self._notes = notes
        self.endResetModel()


class NotesController(QObject):
    """Controller for Notes view — holds status, note list model, selected note state."""

    # Status signals
    status_changed = Signal()
    # Selected note signals
    selected_changed = Signal()
    content_changed = Signal()
    editing_changed = Signal()
    draft_changed = Signal()
    saving_changed = Signal()
    # Creating note signals
    creating_changed = Signal()
    new_name_changed = Signal()

    def __init__(self, client: RpcClient, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._status: Optional[dict] = None
        self._notes_model = NotesModel(client, self)
        self._selected: Optional[dict] = None  # {path, title}
        self._content: str = ""
        self._editing: bool = False
        self._draft: str = ""
        self._saving: bool = False
        self._creating: bool = False
        self._new_name: str = ""

        # Load on connected
        self._client.connected.connect(self._on_connected)

    # Property: status (ObsidianStatusDTO)
    @Property("QVariant", notify=status_changed)
    def status(self):
        return self._status

    @status.setter
    def status(self, value):
        if self._status != value:
            self._status = value
            self.status_changed.emit()

    # Property: notes_model (QAbstractListModel)
    @Property(QObject, constant=True)
    def notes_model(self):
        return self._notes_model

    # Property: selected (NoteDTO or None)
    @Property("QVariant", notify=selected_changed)
    def selected(self):
        return self._selected

    @selected.setter
    def selected(self, value):
        if self._selected != value:
            self._selected = value
            self.selected_changed.emit()

    # Property: content (current note content)
    @Property(str, notify=content_changed)
    def content(self):
        return self._content

    @content.setter
    def content(self, value: str):
        if self._content != value:
            self._content = value
            self.content_changed.emit()

    # Property: editing
    @Property(bool, notify=editing_changed)
    def editing(self):
        return self._editing

    @editing.setter
    def editing(self, value: bool):
        if self._editing != value:
            self._editing = value
            self.editing_changed.emit()

    # Property: draft
    @Property(str, notify=draft_changed)
    def draft(self):
        return self._draft

    @draft.setter
    def draft(self, value: str):
        if self._draft != value:
            self._draft = value
            self.draft_changed.emit()

    # Property: saving
    @Property(bool, notify=saving_changed)
    def saving(self):
        return self._saving

    @saving.setter
    def saving(self, value: bool):
        if self._saving != value:
            self._saving = value
            self.saving_changed.emit()

    # Property: creating
    @Property(bool, notify=creating_changed)
    def creating(self):
        return self._creating

    @creating.setter
    def creating(self, value: bool):
        if self._creating != value:
            self._creating = value
            self.creating_changed.emit()

    # Property: new_name
    @Property(str, notify=new_name_changed)
    def new_name(self):
        return self._new_name

    @new_name.setter
    def new_name(self, value: str):
        if self._new_name != value:
            self._new_name = value
            self.new_name_changed.emit()

    # Derived: available (vault is connected)
    @Property(bool, notify=status_changed)
    def available(self):
        return (self._status or {}).get("available", False)

    # Derived: vault (vault path)
    @Property(str, notify=status_changed)
    def vault(self):
        return (self._status or {}).get("vault", "")

    @Slot()
    def _on_connected(self):
        """Load status on connect."""
        self._client.call("ObsidianStatus", callback=self._on_status_result)

    def _on_status_result(self, result: dict):
        """Handle ObsidianStatus RPC result."""
        if "error" in result:
            return

        self.status = result.get("result")
        # If vault available, load initial empty-query note list
        if self.available:
            self._notes_model.load("")

    @Slot(str)
    def search(self, query: str):
        """Re-load notes with a search query."""
        if not self.available:
            return
        self._notes_model.load(query)

    @Slot(str, str)
    def open_note(self, path: str, title: str):
        """Open a note by path."""
        self.selected = {"path": path, "title": title}
        self.editing = False
        self.content = ""

        self._client.call("ObsidianRead", path, callback=self._on_read_result)

    def _on_read_result(self, result: dict):
        """Handle ObsidianRead RPC result."""
        if "error" in result:
            # Could surface error toast, but for now just leave content empty
            return

        self.content = result.get("result", "")

    @Slot()
    def start_edit(self):
        """Start editing the current note."""
        self.draft = self.content
        self.editing = True

    @Slot()
    def cancel_edit(self):
        """Cancel editing."""
        self.editing = False

    @Slot()
    def save(self):
        """Save the edited note."""
        if not self.selected:
            return

        self.saving = True
        path = self.selected.get("path", "")
        self._client.call(
            "ObsidianWrite", path, self.draft, False, callback=self._on_save_result
        )

    def _on_save_result(self, result: dict):
        """Handle ObsidianWrite result."""
        self.saving = False
        if "error" in result:
            # Could surface error toast
            return

        self.content = self.draft
        self.editing = False

    @Slot()
    def start_create(self):
        """Start the inline new-note composer."""
        self.creating = True
        self.new_name = ""

    @Slot()
    def cancel_create(self):
        """Cancel the inline new-note composer."""
        self.creating = False
        self.new_name = ""

    @Slot()
    def create_note(self):
        """Create a new note."""
        name = self.new_name.strip()
        if not name:
            return

        # Template: # <title>\n\n (strip .md extension for title)
        title = name.replace(".md", "")
        template = f"# {title}\n\n"

        self._client.call(
            "ObsidianWrite", name, template, False, callback=self._on_create_result
        )

    def _on_create_result(self, result: dict):
        """Handle ObsidianWrite result for new note."""
        if "error" in result:
            # Could surface error toast
            return

        rel = result.get("result", "")
        self.creating = False
        self.new_name = ""

        # Reload note list
        self._notes_model.load("")

        # Open the new note in edit mode
        title = rel.replace(".md", "")
        self.open_note(rel, title)
        self.start_edit()
