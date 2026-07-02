"""
test_live_model.py — tests for LiveSessionsModel filter/sort logic.
"""

import pytest
from eigenqt.models.live import filter_and_sort_live


def test_filter_live_only():
    """Filter should keep only working and approval sessions."""
    sessions = [
        {"id": "s1", "status": "working", "updated": 1000},
        {"id": "s2", "status": "idle", "updated": 2000},
        {"id": "s3", "status": "approval", "updated": 3000},
        {"id": "s4", "status": "error", "updated": 4000},
    ]
    result = filter_and_sort_live(sessions)
    assert len(result) == 2
    ids = [s["id"] for s in result]
    assert "s1" in ids
    assert "s3" in ids


def test_sort_urgency_working_first():
    """Working sessions should appear before approval sessions."""
    sessions = [
        {"id": "approval1", "status": "approval", "updated": 5000},
        {"id": "working1", "status": "working", "updated": 1000},
        {"id": "approval2", "status": "approval", "updated": 6000},
        {"id": "working2", "status": "working", "updated": 2000},
    ]
    result = filter_and_sort_live(sessions)
    assert len(result) == 4
    assert result[0]["id"] == "working2"  # newest working
    assert result[1]["id"] == "working1"  # older working
    assert result[2]["id"] == "approval2"  # newest approval
    assert result[3]["id"] == "approval1"  # older approval


def test_sort_newest_within_status():
    """Within each status group, newest (highest updated) should come first."""
    sessions = [
        {"id": "w1", "status": "working", "updated": 1000},
        {"id": "w2", "status": "working", "updated": 3000},
        {"id": "w3", "status": "working", "updated": 2000},
    ]
    result = filter_and_sort_live(sessions)
    assert len(result) == 3
    assert result[0]["id"] == "w2"  # 3000
    assert result[1]["id"] == "w3"  # 2000
    assert result[2]["id"] == "w1"  # 1000


def test_empty_list():
    """Empty input should return empty output."""
    result = filter_and_sort_live([])
    assert result == []


def test_no_live_sessions():
    """If no sessions are live, result should be empty."""
    sessions = [
        {"id": "s1", "status": "idle", "updated": 1000},
        {"id": "s2", "status": "error", "updated": 2000},
    ]
    result = filter_and_sort_live(sessions)
    assert result == []


def test_all_live_sessions():
    """If all sessions are live, all should be returned sorted."""
    sessions = [
        {"id": "a1", "status": "approval", "updated": 100},
        {"id": "w1", "status": "working", "updated": 200},
        {"id": "a2", "status": "approval", "updated": 300},
        {"id": "w2", "status": "working", "updated": 400},
    ]
    result = filter_and_sort_live(sessions)
    assert len(result) == 4
    # working (newest first), then approval (newest first)
    assert result[0]["id"] == "w2"  # working 400
    assert result[1]["id"] == "w1"  # working 200
    assert result[2]["id"] == "a2"  # approval 300
    assert result[3]["id"] == "a1"  # approval 100


def test_single_live_session():
    """Single live session should be returned."""
    sessions = [{"id": "only", "status": "working", "updated": 999}]
    result = filter_and_sort_live(sessions)
    assert len(result) == 1
    assert result[0]["id"] == "only"
