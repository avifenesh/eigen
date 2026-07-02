#!/usr/bin/env python3
"""
test_board.py — Test BoardModel basic data handling.

Tests model loading, role access, and data flow. No RPC (mock data).

Usage:
    cd gui-qt && .venv/bin/python3 test_board.py

Exit 0 on success, 1 on failure.
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from PySide6.QtCore import QCoreApplication, QTimer

from eigenqt.models.board import BoardModel, KanbanModel
from eigenqt.rpc import RpcClient


def test_board_model(app):
    """Test BoardModel with mock data."""
    print("Testing BoardModel...")

    # Create model (no real RPC needed for data tests)
    client = RpcClient()
    model = BoardModel(client)

    # Inject mock data directly (simulating RPC result)
    mock_lanes = [
        {
            "dir": "/home/user/project1",
            "name": "project1",
            "repo": "user/project1",
            "branch": "main",
            "url": "https://github.com/user/project1",
            "remote": True,
            "pinned": True,
            "dirty": 2,
            "unpushed": 1,
            "behind": 0,
            "todos": 5,
            "openPrs": 1,
            "openIss": 2,
            "items": [
                {
                    "key": "item1",
                    "kind": "github",
                    "title": "PR #123",
                    "detail": "PR in review",
                    "url": "https://github.com/user/project1/pull/123",
                    "task": "",
                    "dir": "/home/user/project1"
                }
            ]
        },
        {
            "dir": "/home/user/project2",
            "name": "project2",
            "repo": "",
            "branch": "feat/new",
            "url": "",
            "remote": False,
            "pinned": False,
            "dirty": 0,
            "unpushed": 0,
            "behind": 0,
            "todos": 0,
            "openPrs": 0,
            "openIss": 0,
            "items": []
        }
    ]

    model.beginResetModel()
    model._lanes = mock_lanes
    model.endResetModel()

    # Test row count
    assert model.rowCount() == 2, f"Expected 2 rows, got {model.rowCount()}"

    # Test data access
    idx0 = model.index(0, 0)
    name0 = model.data(idx0, model.NameRole)
    assert name0 == "project1", f"Expected 'project1', got '{name0}'"

    repo0 = model.data(idx0, model.RepoRole)
    assert repo0 == "user/project1", f"Expected 'user/project1', got '{repo0}'"

    pinned0 = model.data(idx0, model.PinnedRole)
    assert pinned0 is True, f"Expected True, got {pinned0}"

    dirty0 = model.data(idx0, model.DirtyRole)
    assert dirty0 == 2, f"Expected 2, got {dirty0}"

    items0 = model.data(idx0, model.ItemsRole)
    assert len(items0) == 1, f"Expected 1 item, got {len(items0)}"
    assert items0[0]["key"] == "item1", f"Expected item1, got {items0[0].get('key')}"

    # Test second row
    idx1 = model.index(1, 0)
    name1 = model.data(idx1, model.NameRole)
    assert name1 == "project2", f"Expected 'project2', got '{name1}'"

    remote1 = model.data(idx1, model.RemoteRole)
    assert remote1 is False, f"Expected False, got {remote1}"

    print("✓ BoardModel tests passed")
    return True


def test_kanban_model(app):
    """Test KanbanModel with mock data."""
    print("Testing KanbanModel...")

    client = RpcClient()
    model = KanbanModel(client)

    # Inject mock columns
    mock_columns = [
        {
            "id": "needs-you",
            "title": "Needs You",
            "cards": [
                {
                    "key": "card1",
                    "kind": "pr",
                    "title": "Review PR #456",
                    "repo": "user/repo",
                    "number": 456,
                    "ageHours": 72,
                    "needsYou": True,
                    "draft": False,
                    "review": "changes",
                    "session": False,
                    "url": "https://github.com/user/repo/pull/456",
                    "task": "",
                    "dir": "/home/user/repo"
                }
            ]
        },
        {
            "id": "in-review",
            "title": "In Review",
            "cards": []
        }
    ]

    model._columns = mock_columns
    model.columnsChanged.emit()

    # Test columns property
    columns = model.columns
    assert len(columns) == 2, f"Expected 2 columns, got {len(columns)}"
    assert columns[0]["id"] == "needs-you", f"Expected 'needs-you', got {columns[0]['id']}"
    assert len(columns[0]["cards"]) == 1, f"Expected 1 card, got {len(columns[0]['cards'])}"
    assert columns[0]["cards"][0]["key"] == "card1", f"Expected 'card1', got {columns[0]['cards'][0]['key']}"

    print("✓ KanbanModel tests passed")
    return True


def main():
    """Run all tests."""
    app = QCoreApplication(sys.argv)

    try:
        if not test_board_model(app):
            sys.exit(1)
        if not test_kanban_model(app):
            sys.exit(1)

        print("\n✓ All board tests passed!")
        sys.exit(0)
    except AssertionError as e:
        print(f"\n✗ Test failed: {e}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"\n✗ Unexpected error: {e}", file=sys.stderr)
        import traceback
        traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    main()
