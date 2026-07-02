"""
Worktree models: DiffModel + FileTreeModel + file content fetcher.

These models parse and present git working-tree diffs and file trees for the
right-side dock. They mirror the Svelte DiffPanel/FilesPanel logic: parse the
unified diff into file sections + hunks with add/del/context classification,
flatten a nested file tree with expand state, and fetch file content for
the viewer.
"""

from dataclasses import dataclass
from enum import Enum
from typing import Optional
from PySide6.QtCore import QAbstractListModel, QModelIndex, Qt, Signal, Slot
from PySide6.QtQml import QmlElement


QML_IMPORT_NAME = "Eigen"
QML_IMPORT_MAJOR_VERSION = 1


class DiffRowKind(Enum):
    """Diff row classification (mirrors DiffView.svelte Kind)."""

    FILE_HEADER = "file"  # File path header with +N/-N badges
    HUNK = "hunk"  # @@ range header
    ADD = "add"  # + line
    DEL = "del"  # - line
    CTX = "ctx"  # context line
    META = "meta"  # diff/index/rename/etc


@dataclass
class DiffRow:
    """
    One row in the diff view.

    kind: row classification (file header, hunk, add, del, context, meta)
    text: the line content (leading +/- stripped for add/del rows)
    sign: the gutter sign ("+" or "−" for add/del, empty otherwise)
    file_path: for FILE_HEADER rows, the changed file path
    adds: for FILE_HEADER rows, the +N count
    dels: for FILE_HEADER rows, the -N count
    """

    kind: DiffRowKind
    text: str
    sign: str = ""
    file_path: str = ""
    adds: int = 0
    dels: int = 0


@QmlElement
class DiffModel(QAbstractListModel):
    """
    Parse a unified diff into rows for the diff view.

    Input: unified patch string + per-file stats (from WorkingDiff RPC).
    Output: flat list of DiffRow objects (file headers, hunks, add/del/ctx lines).

    File headers are expandable/collapsible; collapsed files hide their hunks.
    The model stores expand state per file path.
    """

    # Roles
    KindRole = Qt.UserRole + 1
    TextRole = Qt.UserRole + 2
    SignRole = Qt.UserRole + 3
    FilePathRole = Qt.UserRole + 4
    AddsRole = Qt.UserRole + 5
    DelsRole = Qt.UserRole + 6
    ExpandedRole = Qt.UserRole + 7

    def __init__(self, parent=None):
        super().__init__(parent)
        self._rows: list[DiffRow] = []
        self._all_rows: list[DiffRow] = []  # full parsed diff
        self._expanded: set[str] = set()  # expanded file paths

    def roleNames(self) -> dict[int, bytes]:
        return {
            self.KindRole: b"kind",
            self.TextRole: b"text",
            self.SignRole: b"sign",
            self.FilePathRole: b"filePath",
            self.AddsRole: b"adds",
            self.DelsRole: b"dels",
            self.ExpandedRole: b"expanded",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        if parent.isValid():
            return 0
        return len(self._rows)

    def data(self, index: QModelIndex, role: int):
        if not index.isValid() or index.row() >= len(self._rows):
            return None

        row = self._rows[index.row()]

        if role == self.KindRole:
            return row.kind.value
        elif role == self.TextRole:
            return row.text
        elif role == self.SignRole:
            return row.sign
        elif role == self.FilePathRole:
            return row.file_path
        elif role == self.AddsRole:
            return row.adds
        elif role == self.DelsRole:
            return row.dels
        elif role == self.ExpandedRole:
            return row.file_path in self._expanded

        return None

    @Slot(str, list)
    def load(self, patch: str, files: list[dict]) -> None:
        """
        Parse unified diff + per-file stats.

        patch: unified diff text (from WorkingDiff.patch)
        files: [{"path": "...", "adds": N, "dels": M}, ...] (from WorkingDiff.files)
        """
        self.beginResetModel()
        self._all_rows = []
        self._expanded.clear()

        if not patch.strip():
            self._rows = []
            self.endResetModel()
            return

        # Build a map of file -> (adds, dels) for file header rows
        file_stats = {f["path"]: (f["adds"], f["dels"]) for f in files}

        lines = patch.rstrip("\n").split("\n")
        current_file: Optional[str] = None
        pending_file_header: Optional[tuple[str, int, int]] = None

        for line in lines:
            if line.startswith("diff "):
                # New file starts - extract path from "diff --git a/... b/..."
                parts = line.split()
                if len(parts) >= 4:
                    # parts[2] is a/path, parts[3] is b/path
                    path = parts[3]
                    if path.startswith("b/"):
                        path = path[2:]
                    adds, dels = file_stats.get(path, (0, 0))
                    pending_file_header = (path, adds, dels)
                self._all_rows.append(DiffRow(DiffRowKind.META, line))
            elif line.startswith("index "):
                # Insert file header before index line
                if pending_file_header:
                    path, adds, dels = pending_file_header
                    self._all_rows.append(
                        DiffRow(
                            DiffRowKind.FILE_HEADER,
                            path,
                            file_path=path,
                            adds=adds,
                            dels=dels,
                        )
                    )
                    self._expanded.add(path)
                    pending_file_header = None
                self._all_rows.append(DiffRow(DiffRowKind.META, line))
            elif line.startswith("--- ") or line.startswith("+++ "):
                # File boundary markers
                self._all_rows.append(DiffRow(DiffRowKind.META, line))
            elif line.startswith("@@"):
                # Hunk header
                self._all_rows.append(DiffRow(DiffRowKind.HUNK, line))
            elif line.startswith("+"):
                # Addition
                self._all_rows.append(
                    DiffRow(DiffRowKind.ADD, line[1:], sign="+")
                )
            elif line.startswith("-"):
                # Deletion (use U+2212 minus to match the Svelte renderer)
                self._all_rows.append(
                    DiffRow(DiffRowKind.DEL, line[1:], sign="−")
                )
            elif line.startswith(" "):
                # Context
                self._all_rows.append(DiffRow(DiffRowKind.CTX, line[1:]))
            elif line.startswith("new file") or line.startswith("deleted file"):
                # Mode change meta
                self._all_rows.append(DiffRow(DiffRowKind.META, line))
            elif line.startswith("rename ") or line.startswith("similarity "):
                # Rename meta
                self._all_rows.append(DiffRow(DiffRowKind.META, line))
            elif line.startswith("\\ "):
                # No newline at EOF
                self._all_rows.append(DiffRow(DiffRowKind.META, line))
            else:
                # Context (no leading space)
                self._all_rows.append(DiffRow(DiffRowKind.CTX, line))

        self._rebuild_visible()
        self.endResetModel()

    def _rebuild_visible(self) -> None:
        """
        Rebuild _rows from _all_rows, respecting expand state.

        FILE_HEADER rows are always visible. Rows after a collapsed FILE_HEADER
        are hidden until the next FILE_HEADER.
        """
        self._rows = []
        current_file_expanded = True

        for row in self._all_rows:
            if row.kind == DiffRowKind.FILE_HEADER:
                self._rows.append(row)
                current_file_expanded = row.file_path in self._expanded
            elif current_file_expanded:
                self._rows.append(row)

    @Slot(str)
    def toggle_file(self, file_path: str) -> None:
        """Toggle expand/collapse for a file."""
        if file_path in self._expanded:
            self._expanded.remove(file_path)
        else:
            self._expanded.add(file_path)
        self.beginResetModel()
        self._rebuild_visible()
        self.endResetModel()


# ── File Tree Model ────────────────────────────────────────────────────────


@dataclass
class FileTreeNode:
    """
    One node in the file tree.

    name: display name (basename)
    path: absolute path (for ReadFileForView RPC)
    is_dir: directory flag
    children: nested children (if is_dir)
    depth: indent level for QML rendering
    expanded: expand state (for directories)
    """

    name: str
    path: str
    is_dir: bool
    children: list["FileTreeNode"]
    depth: int = 0
    expanded: bool = True


@QmlElement
class FileTreeModel(QAbstractListModel):
    """
    Flatten a nested file tree into a list for QML rendering.

    Input: FileTreeDTO from FileTree RPC (nested structure).
    Output: flat list of FileTreeNode objects with depth + expand state.

    Expand/collapse toggles re-flatten the visible nodes (never model reset,
    just dataChanged for the toggled directory + insert/remove for children).
    """

    # Roles
    NameRole = Qt.UserRole + 1
    PathRole = Qt.UserRole + 2
    IsDirRole = Qt.UserRole + 3
    DepthRole = Qt.UserRole + 4
    ExpandedRole = Qt.UserRole + 5

    def __init__(self, parent=None):
        super().__init__(parent)
        self._nodes: list[FileTreeNode] = []
        self._tree: list[FileTreeNode] = []  # top-level entries
        self._expanded: set[str] = set()  # expanded dir paths

    def roleNames(self) -> dict[int, bytes]:
        return {
            self.NameRole: b"name",
            self.PathRole: b"path",
            self.IsDirRole: b"isDir",
            self.DepthRole: b"depth",
            self.ExpandedRole: b"expanded",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        if parent.isValid():
            return 0
        return len(self._nodes)

    def data(self, index: QModelIndex, role: int):
        if not index.isValid() or index.row() >= len(self._nodes):
            return None

        node = self._nodes[index.row()]

        if role == self.NameRole:
            return node.name
        elif role == self.PathRole:
            return node.path
        elif role == self.IsDirRole:
            return node.is_dir
        elif role == self.DepthRole:
            return node.depth
        elif role == self.ExpandedRole:
            return node.path in self._expanded

        return None

    @Slot(list)
    def load(self, entries: list[dict]) -> None:
        """
        Load file tree from FileTreeDTO.entries.

        entries: [{"name": "...", "path": "...", "isDir": bool, "children": [...]}, ...]
        """
        self.beginResetModel()
        self._tree = []
        self._expanded.clear()

        # Mark top-level dirs as expanded BEFORE parsing
        for e in entries:
            if e.get("isDir"):
                self._expanded.add(e["path"])

        for e in entries:
            node = self._parse_entry(e, depth=0)
            self._tree.append(node)

        self._flatten()
        self.endResetModel()

    def _parse_entry(self, entry: dict, depth: int) -> FileTreeNode:
        """Recursively parse a FileEntryDTO into a FileTreeNode."""
        children = []
        if entry.get("isDir") and "children" in entry:
            for child in entry["children"]:
                children.append(self._parse_entry(child, depth + 1))

        return FileTreeNode(
            name=entry["name"],
            path=entry["path"],
            is_dir=entry.get("isDir", False),
            children=children,
            depth=depth,
            expanded=entry["path"] in self._expanded,
        )

    def _flatten(self) -> None:
        """Flatten _tree into _nodes, respecting expand state."""
        self._nodes = []

        def walk(node: FileTreeNode) -> None:
            self._nodes.append(node)
            if node.is_dir and node.path in self._expanded:
                for child in node.children:
                    walk(child)

        for top in self._tree:
            walk(top)

    @Slot(str)
    def toggle_dir(self, path: str) -> None:
        """Toggle expand/collapse for a directory."""
        # Find the node in _nodes
        idx = None
        for i, node in enumerate(self._nodes):
            if node.path == path:
                idx = i
                break

        if idx is None:
            return

        node = self._nodes[idx]
        if not node.is_dir:
            return

        if path in self._expanded:
            # Collapse: remove all descendants
            self._expanded.remove(path)
            # Count how many descendants are currently visible
            desc_count = 0
            for i in range(idx + 1, len(self._nodes)):
                if self._nodes[i].depth <= node.depth:
                    break
                desc_count += 1

            if desc_count > 0:
                self.beginRemoveRows(QModelIndex(), idx + 1, idx + desc_count)
                del self._nodes[idx + 1 : idx + 1 + desc_count]
                self.endRemoveRows()

            # Update the dir row itself (expanded flag changed)
            model_idx = self.index(idx, 0)
            self.dataChanged.emit(model_idx, model_idx, [self.ExpandedRole])
        else:
            # Expand: insert children
            self._expanded.add(path)

            # Collect children to insert
            children = []

            def walk(n: FileTreeNode) -> None:
                children.append(n)
                if n.is_dir and n.path in self._expanded:
                    for child in n.children:
                        walk(child)

            for child in node.children:
                walk(child)

            if children:
                self.beginInsertRows(QModelIndex(), idx + 1, idx + len(children))
                for i, child in enumerate(children):
                    self._nodes.insert(idx + 1 + i, child)
                self.endInsertRows()

            # Update the dir row itself
            model_idx = self.index(idx, 0)
            self.dataChanged.emit(model_idx, model_idx, [self.ExpandedRole])
