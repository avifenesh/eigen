"""
Unit tests for worktree models (DiffModel + FileTreeModel).

Test diff parsing with a multi-file unified diff fixture (adds/dels/renames),
file tree flattening with expand/collapse, and edge cases.
"""

import pytest
from PySide6.QtCore import Qt
from eigenqt.models.worktree import DiffModel, DiffRowKind, FileTreeModel


# ── Fixtures ──────────────────────────────────────────────────────────────


@pytest.fixture
def diff_patch():
    """
    Real multi-file unified diff with adds, dels, renames, hunks.
    """
    return """diff --git a/internal/agent/background.go b/internal/agent/background.go
index c1ce38a..e6af952 100644
--- a/internal/agent/background.go
+++ b/internal/agent/background.go
@@ -10,7 +10,7 @@ import (
 	"time"
 )

-// BackgroundTask runs an agent loop in the background.
+// BackgroundTask runs an agent loop in the background (updated).
 func BackgroundTask(ctx context.Context, id string) error {
 	log.Printf("Starting background task %s", id)
 	return nil
@@ -20,3 +20,6 @@ func BackgroundTask(ctx context.Context, id string) error {
 	log.Printf("Task %s completed", id)
 }

+// NewFunction added for testing.
+func NewFunction() {}
+
diff --git a/internal/gui/worktree.go b/internal/gui/worktree.go
index d46ea39..509fbf8 100644
--- a/internal/gui/worktree.go
+++ b/internal/gui/worktree.go
@@ -1,6 +1,7 @@
 package gui

 import (
+	"context"
 	"fmt"
 	"os"
 )
"""


@pytest.fixture
def diff_files():
    """Per-file stats matching diff_patch."""
    return [
        {"path": "internal/agent/background.go", "adds": 4, "dels": 1},
        {"path": "internal/gui/worktree.go", "adds": 1, "dels": 0},
    ]


@pytest.fixture
def file_tree_entries():
    """Nested file tree (3 levels)."""
    return [
        {
            "name": "internal",
            "path": "/home/user/proj/internal",
            "isDir": True,
            "children": [
                {
                    "name": "agent",
                    "path": "/home/user/proj/internal/agent",
                    "isDir": True,
                    "children": [
                        {
                            "name": "background.go",
                            "path": "/home/user/proj/internal/agent/background.go",
                            "isDir": False,
                            "children": [],
                        }
                    ],
                },
                {
                    "name": "gui",
                    "path": "/home/user/proj/internal/gui",
                    "isDir": True,
                    "children": [
                        {
                            "name": "worktree.go",
                            "path": "/home/user/proj/internal/gui/worktree.go",
                            "isDir": False,
                            "children": [],
                        }
                    ],
                },
            ],
        },
        {
            "name": "README.md",
            "path": "/home/user/proj/README.md",
            "isDir": False,
            "children": [],
        },
    ]


# ── DiffModel tests ───────────────────────────────────────────────────────


def test_diff_model_empty():
    """Empty patch yields no rows."""
    model = DiffModel()
    model.load("", [])
    assert model.rowCount() == 0


def test_diff_model_parse(diff_patch, diff_files):
    """Parse multi-file diff into classified rows."""
    model = DiffModel()
    model.load(diff_patch, diff_files)

    # Should have:
    # - 2 FILE_HEADER rows (one per file)
    # - Several META rows (diff/index/---/+++)
    # - HUNK rows (@@ ...)
    # - ADD/DEL/CTX rows

    rows = model.rowCount()
    assert rows > 0

    # Find first FILE_HEADER row (may not be at index 0)
    file_header_idx = None
    for i in range(rows):
        if model.data(model.index(i, 0), model.KindRole) == DiffRowKind.FILE_HEADER.value:
            file_header_idx = i
            break

    assert file_header_idx is not None

    idx = model.index(file_header_idx, 0)
    file_path = model.data(idx, model.FilePathRole)
    assert "background.go" in file_path

    adds = model.data(idx, model.AddsRole)
    dels = model.data(idx, model.DelsRole)
    assert adds == 4
    assert dels == 1

    # Find an ADD row (the new comment line)
    add_rows = [
        i
        for i in range(rows)
        if model.data(model.index(i, 0), model.KindRole) == DiffRowKind.ADD.value
    ]
    assert len(add_rows) > 0

    # Check ADD row has sign "+"
    add_idx = model.index(add_rows[0], 0)
    sign = model.data(add_idx, model.SignRole)
    assert sign == "+"

    # Find a DEL row
    del_rows = [
        i
        for i in range(rows)
        if model.data(model.index(i, 0), model.KindRole) == DiffRowKind.DEL.value
    ]
    assert len(del_rows) > 0

    # Check DEL row has sign "−" (U+2212)
    del_idx = model.index(del_rows[0], 0)
    sign = model.data(del_idx, model.SignRole)
    assert sign == "−"


def test_diff_model_toggle_file(diff_patch, diff_files):
    """Collapse a file hides its hunks."""
    model = DiffModel()
    model.load(diff_patch, diff_files)

    initial_count = model.rowCount()
    assert initial_count > 0

    # Find first file header
    file_path = None
    for i in range(initial_count):
        idx = model.index(i, 0)
        if model.data(idx, model.KindRole) == DiffRowKind.FILE_HEADER.value:
            file_path = model.data(idx, model.FilePathRole)
            break

    assert file_path is not None

    # Toggle collapse
    model.toggle_file(file_path)
    collapsed_count = model.rowCount()

    # Collapsed count should be less (hunks hidden)
    assert collapsed_count < initial_count

    # Toggle expand
    model.toggle_file(file_path)
    expanded_count = model.rowCount()

    # Should return to initial count
    assert expanded_count == initial_count


# ── FileTreeModel tests ───────────────────────────────────────────────────


def test_file_tree_model_empty():
    """Empty entries yield no rows."""
    model = FileTreeModel()
    model.load([])
    assert model.rowCount() == 0


def test_file_tree_model_flatten(file_tree_entries):
    """Flatten nested tree into a list with depth."""
    model = FileTreeModel()
    model.load(file_tree_entries)

    rows = model.rowCount()
    assert rows > 0

    # First row: "internal" dir (depth 0, expanded by default)
    idx = model.index(0, 0)
    name = model.data(idx, model.NameRole)
    assert name == "internal"

    is_dir = model.data(idx, model.IsDirRole)
    assert is_dir is True

    depth = model.data(idx, model.DepthRole)
    assert depth == 0

    expanded = model.data(idx, model.ExpandedRole)
    assert expanded is True  # top-level dirs expand by default

    # Check a nested dir (agent, depth 1)
    found_agent = False
    for i in range(rows):
        idx = model.index(i, 0)
        if model.data(idx, model.NameRole) == "agent":
            found_agent = True
            assert model.data(idx, model.IsDirRole) is True
            assert model.data(idx, model.DepthRole) == 1
            break

    assert found_agent

    # background.go (depth 2) is NOT visible by default (agent is collapsed)
    found_background = any(
        model.data(model.index(i, 0), model.NameRole) == "background.go"
        for i in range(rows)
    )
    assert not found_background, "background.go should be hidden (parent dir collapsed)"


def test_file_tree_model_toggle_dir(file_tree_entries):
    """Collapse a dir removes its children from the flat list."""
    model = FileTreeModel()
    model.load(file_tree_entries)

    initial_count = model.rowCount()
    assert initial_count > 0

    # Find "internal" dir (depth 0)
    internal_path = None
    for i in range(initial_count):
        idx = model.index(i, 0)
        if (
            model.data(idx, model.NameRole) == "internal"
            and model.data(idx, model.IsDirRole) is True
        ):
            internal_path = model.data(idx, model.PathRole)
            break

    assert internal_path is not None

    # First, expand "agent" so background.go is visible
    agent_path = None
    for i in range(initial_count):
        idx = model.index(i, 0)
        if model.data(idx, model.NameRole) == "agent":
            agent_path = model.data(idx, model.PathRole)
            break

    assert agent_path is not None
    model.toggle_dir(agent_path)

    # Now background.go should be visible
    found_background_before = any(
        model.data(model.index(i, 0), model.NameRole) == "background.go"
        for i in range(model.rowCount())
    )
    assert found_background_before

    # Store count before collapse
    before_collapse_count = model.rowCount()

    # Collapse "internal"
    model.toggle_dir(internal_path)
    collapsed_count = model.rowCount()

    # Should have fewer rows (all children of "internal" hidden)
    assert collapsed_count < before_collapse_count
    # After collapsing "internal", only top-level nodes remain
    # "internal" dir itself + "README.md" = 2 rows
    assert collapsed_count == 2

    # "background.go" should no longer be in the flat list
    found_background = any(
        model.data(model.index(i, 0), model.NameRole) == "background.go"
        for i in range(collapsed_count)
    )
    assert not found_background

    # Expand "internal"
    model.toggle_dir(internal_path)

    # Should have "internal" + its immediate children (agent, gui) visible
    # But agent is still expanded from before, so background.go should be back
    found_background = any(
        model.data(model.index(i, 0), model.NameRole) == "background.go"
        for i in range(model.rowCount())
    )
    assert found_background


def test_file_tree_model_non_dir_toggle(file_tree_entries):
    """Toggling a file (not a dir) does nothing."""
    model = FileTreeModel()
    model.load(file_tree_entries)

    # Find README.md (a file, not a dir)
    readme_path = None
    for i in range(model.rowCount()):
        idx = model.index(i, 0)
        if model.data(idx, model.NameRole) == "README.md":
            readme_path = model.data(idx, model.PathRole)
            break

    assert readme_path is not None

    initial_count = model.rowCount()

    # Toggle (should be a no-op)
    model.toggle_dir(readme_path)
    assert model.rowCount() == initial_count
