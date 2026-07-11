"""
test_transcript_logic.py — Unit tests for transcript_logic (pure event folding).

Tests seed_from_state, fold_event with recorded fixtures.
"""

import json
from pathlib import Path

import pytest

from eigenqt.models.transcript_logic import (
    TranscriptRow,
    fold_event,
    seed_from_state,
)


def test_seed_from_empty_state():
    """Empty state → empty rows."""
    state = {"messages": []}
    rows = seed_from_state(state)
    assert rows == []


def test_seed_user_assistant():
    """User + assistant messages → user + assistant rows."""
    state = {
        "messages": [
            {"role": "user", "text": "Hello"},
            {"role": "assistant", "text": "Hi there", "reasoning": ""},
        ]
    }
    rows = seed_from_state(state)
    assert len(rows) == 2
    assert rows[0].kind == "user"
    assert rows[0].text == "Hello"
    assert rows[1].kind == "assistant"
    assert rows[1].text == "Hi there"
    assert rows[1].streaming is False


def test_seed_tool_calls():
    """Assistant with tool_calls → assistant + tool rows."""
    state = {
        "messages": [
            {
                "role": "assistant",
                "text": "",
                "toolCalls": [
                    {"id": "call_1", "name": "Bash", "args": '{"command":"ls"}'}
                ],
            },
            {
                "role": "tool",
                "toolCallId": "call_1",
                "toolName": "Bash",
                "text": "file1.txt\nfile2.txt",
                "toolError": False,
            },
        ]
    }
    rows = seed_from_state(state)
    assert len(rows) == 1  # tool row (assistant text is empty)
    assert rows[0].kind == "tool"
    assert rows[0].tool_name == "Bash"
    assert rows[0].tool_id == "call_1"
    assert rows[0].text == "file1.txt\nfile2.txt"
    assert rows[0].tool_status == "success"


def test_seed_in_flight_tool_call_stays_running():
    """A running State snapshot keeps its unresolved tool card live."""
    rows = seed_from_state(
        {
            "running": True,
            "messages": [
                {
                    "role": "assistant",
                    "toolCalls": [
                        {"id": "call_live", "name": "Bash", "args": '{"command":"pytest"}'}
                    ],
                }
            ],
        }
    )

    assert len(rows) == 1
    assert rows[0].kind == "tool"
    assert rows[0].tool_status == "running"


def test_fold_text_delta_new_turn():
    """Text delta on empty rows → new assistant row."""
    rows: list[TranscriptRow] = []
    event = {"kind": "text", "text": "Hello", "step": 1}
    ops = fold_event(rows, event, replay=False)

    assert len(ops) == 1
    assert ops[0].op == "insert"
    assert len(rows) == 1
    assert rows[0].kind == "assistant"
    assert rows[0].text == "Hello"
    assert rows[0].streaming is True


def test_fold_text_delta_append():
    """Text delta on existing streaming assistant → append."""
    rows = [TranscriptRow(kind="assistant", text="Hello", streaming=True)]
    event = {"kind": "text", "text": " world", "step": 1}
    ops = fold_event(rows, event, replay=False)

    assert len(ops) == 1
    assert ops[0].op == "update"
    assert rows[0].text == "Hello world"


def test_fold_reasoning_delta_new_turn():
    """Leading reasoning creates a streaming assistant row."""
    rows: list[TranscriptRow] = []
    event = {"kind": "reasoning", "text": "Thinking", "step": 1}
    ops = fold_event(rows, event, replay=False)

    assert len(ops) == 1
    assert ops[0].op == "insert"
    assert len(rows) == 1
    assert rows[0].kind == "assistant"
    assert rows[0].reasoning == "Thinking"
    assert rows[0].streaming is True


def test_fold_tool_start():
    """Tool start → insert tool row (status: running)."""
    rows: list[TranscriptRow] = []
    event = {
        "kind": "tool_start",
        "tool": "Bash",
        "toolId": "call_1",
        "toolArgs": '{"command":"ls"}',
        "step": 2,
    }
    ops = fold_event(rows, event, replay=False)

    assert len(ops) == 1
    assert ops[0].op == "insert"
    assert len(rows) == 1
    assert rows[0].kind == "tool"
    assert rows[0].tool_name == "Bash"
    assert rows[0].tool_status == "running"


def test_fold_tool_result():
    """Tool result → update matching tool row."""
    rows = [
        TranscriptRow(
            kind="tool", tool_name="Bash", tool_id="call_1", tool_status="running"
        )
    ]
    event = {
        "kind": "tool_result",
        "toolId": "call_1",
        "result": "output here",
        "isError": False,
    }
    ops = fold_event(rows, event, replay=False)

    assert len(ops) == 1
    assert ops[0].op == "update"
    assert rows[0].text == "output here"
    assert rows[0].tool_status == "success"


def test_fold_done():
    """Done event → mark assistant as non-streaming."""
    rows = [TranscriptRow(kind="assistant", text="Done", streaming=True)]
    event = {"kind": "done"}
    ops = fold_event(rows, event, replay=False)

    assert len(ops) == 1
    assert ops[0].op == "update"
    assert rows[0].streaming is False


def test_fold_note():
    """Note event → insert note row."""
    rows: list[TranscriptRow] = []
    event = {"kind": "note", "text": "Agent note here", "step": 3}
    ops = fold_event(rows, event, replay=False)

    assert len(ops) == 1
    assert ops[0].op == "insert"
    assert len(rows) == 1
    assert rows[0].kind == "note"
    assert rows[0].text == "Agent note here"


def test_fold_approval():
    """Approval event → insert approval row."""
    rows: list[TranscriptRow] = []
    event = {"kind": "approval", "text": "Bash ls", "result": "appr_123", "step": 4}
    ops = fold_event(rows, event, replay=False)

    assert len(ops) == 1
    assert ops[0].op == "insert"
    assert len(rows) == 1
    assert rows[0].kind == "approval"
    assert rows[0].text == "Bash ls"
    assert rows[0].tool_name == "appr_123"  # result = approval ID


def test_fold_sequence():
    """Full sequence: user → text deltas → tool_start → tool_result → done."""
    rows: list[TranscriptRow] = []

    # User message (from seed, not event)
    rows.append(TranscriptRow(kind="user", text="Run ls"))

    # Text deltas
    fold_event(rows, {"kind": "text", "text": "Sure", "step": 1}, replay=False)
    fold_event(rows, {"kind": "text", "text": ", running", "step": 1}, replay=False)

    # Tool start
    fold_event(
        rows,
        {
            "kind": "tool_start",
            "tool": "Bash",
            "toolId": "c1",
            "toolArgs": '{"command":"ls"}',
            "step": 2,
        },
        replay=False,
    )

    # Tool result
    fold_event(
        rows,
        {"kind": "tool_result", "toolId": "c1", "result": "file1.txt", "isError": False},
        replay=False,
    )

    # Done
    fold_event(rows, {"kind": "done"}, replay=False)

    # Verify final state
    assert len(rows) == 3
    assert rows[0].kind == "user"
    assert rows[1].kind == "assistant"
    assert rows[1].text == "Sure, running"
    assert rows[1].streaming is False
    assert rows[2].kind == "tool"
    assert rows[2].text == "file1.txt"
    assert rows[2].tool_status == "success"
