"""
test_transcript_parse.py — test .jsonl transcript parsing (corrupt line tolerance).

The TasksView transcript viewer parses agent .jsonl transcripts line-by-line.
Each line is an llm.Message (PascalCase Go JSON: Role, Text, ToolCalls, etc.).
Corrupt/truncated lines degrade to a raw verbatim entry instead of dropping the whole transcript.
"""

import json
import pytest


def parse_transcript_line(line: str) -> dict:
    """
    Parse a single .jsonl line (Go llm.Message) into a display entry.

    Returns dict with:
        role, text, reasoning, toolName, toolError, toolCalls (list of {id, name, args}), raw (if unparsed)
    """
    try:
        obj = json.loads(line)
        entry = {
            "role": obj.get("Role") or obj.get("role") or "message",
            "text": obj.get("Text") or obj.get("text") or "",
            "reasoning": obj.get("Reasoning") or obj.get("reasoning") or "",
            "toolName": obj.get("ToolName") or obj.get("toolName") or "",
            "toolError": obj.get("ToolError") is True or obj.get("toolError") is True,
            "toolCalls": [],
            "raw": "",
        }

        # Parse tool calls
        raw_calls = obj.get("ToolCalls") or obj.get("toolCalls") or []
        if isinstance(raw_calls, list):
            for c in raw_calls:
                if not isinstance(c, dict):
                    continue
                args = c.get("Arguments") or c.get("Args") or c.get("arguments") or c.get("args")
                args_str = ""
                if isinstance(args, str):
                    args_str = args
                elif args is not None:
                    try:
                        args_str = json.dumps(args, indent=2)
                    except:
                        args_str = ""

                entry["toolCalls"].append({
                    "id": c.get("ID") or c.get("Id") or c.get("id") or "",
                    "name": c.get("Name") or c.get("name") or "",
                    "args": args_str,
                })

        return entry
    except:
        # Tolerate bad line: return verbatim entry
        return {
            "role": "unparsed",
            "text": "",
            "reasoning": "",
            "toolName": "",
            "toolError": False,
            "toolCalls": [],
            "raw": line,
        }


def test_valid_user_message():
    """Parse a well-formed user message."""
    line = '{"Role":"user","Text":"hello world"}'
    entry = parse_transcript_line(line)
    assert entry["role"] == "user"
    assert entry["text"] == "hello world"
    assert entry["raw"] == ""


def test_valid_assistant_with_tool_calls():
    """Parse an assistant message with tool calls (PascalCase Go JSON)."""
    line = json.dumps({
        "Role": "assistant",
        "Text": "Let me check that",
        "ToolCalls": [
            {"ID": "call_1", "Name": "Read", "Arguments": {"file_path": "/tmp/test.txt"}},
            {"ID": "call_2", "Name": "Bash", "Args": "ls -la"},
        ],
    })
    entry = parse_transcript_line(line)
    assert entry["role"] == "assistant"
    assert entry["text"] == "Let me check that"
    assert len(entry["toolCalls"]) == 2
    assert entry["toolCalls"][0]["name"] == "Read"
    assert entry["toolCalls"][0]["id"] == "call_1"
    assert "file_path" in entry["toolCalls"][0]["args"]
    assert entry["toolCalls"][1]["name"] == "Bash"
    assert entry["toolCalls"][1]["args"] == "ls -la"  # Already a string, not JSON-encoded


def test_tool_response_with_error():
    """Parse a tool response with error flag."""
    line = json.dumps({
        "Role": "tool",
        "ToolName": "Bash",
        "Text": "command not found: xyz",
        "ToolError": True,
    })
    entry = parse_transcript_line(line)
    assert entry["role"] == "tool"
    assert entry["toolName"] == "Bash"
    assert entry["toolError"] is True
    assert "command not found" in entry["text"]


def test_reasoning_field():
    """Parse assistant message with reasoning (extended thinking)."""
    line = json.dumps({
        "Role": "assistant",
        "Reasoning": "The user wants to read a file, so I should use the Read tool.",
        "Text": "I'll read that file now.",
    })
    entry = parse_transcript_line(line)
    assert entry["role"] == "assistant"
    assert "Read tool" in entry["reasoning"]
    assert entry["text"] == "I'll read that file now."


def test_corrupt_json_line():
    """Corrupt JSON line degrades to unparsed raw entry."""
    line = '{"Role":"user","Text":"hello'  # truncated/missing closing brace
    entry = parse_transcript_line(line)
    assert entry["role"] == "unparsed"
    assert entry["raw"] == line
    assert entry["text"] == ""


def test_empty_line():
    """Empty line degrades gracefully (unparsed)."""
    entry = parse_transcript_line("")
    assert entry["role"] == "unparsed"
    assert entry["raw"] == ""


def test_lowercase_fields():
    """Tolerate lowercased field names (case-insensitive parsing)."""
    line = json.dumps({
        "role": "user",
        "text": "lowercased fields",
    })
    entry = parse_transcript_line(line)
    assert entry["role"] == "user"
    assert entry["text"] == "lowercased fields"


def test_tool_call_with_string_args():
    """Tool call with string args (not object)."""
    line = json.dumps({
        "Role": "assistant",
        "ToolCalls": [
            {"Name": "Bash", "Arguments": "echo hello"},
        ],
    })
    entry = parse_transcript_line(line)
    assert len(entry["toolCalls"]) == 1
    assert entry["toolCalls"][0]["name"] == "Bash"
    assert entry["toolCalls"][0]["args"] == "echo hello"


def test_multi_line_transcript():
    """Parse a multi-line .jsonl transcript with a corrupt line in the middle."""
    transcript = """{"Role":"user","Text":"first message"}
{"Role":"assistant","Text":"response one"}
{"Role":"tool","ToolName":"Read","Text":"file contents
{"Role":"assistant","Text":"final message"}"""

    lines = transcript.strip().split("\n")
    entries = [parse_transcript_line(line) for line in lines if line.strip()]

    assert len(entries) == 4
    assert entries[0]["role"] == "user"
    assert entries[0]["text"] == "first message"

    assert entries[1]["role"] == "assistant"
    assert entries[1]["text"] == "response one"

    # Line 3 is corrupt (missing closing brace) → unparsed
    assert entries[2]["role"] == "unparsed"
    assert entries[2]["raw"].startswith('{"Role":"tool"')

    # Line 4 parsed successfully despite corrupt predecessor
    assert entries[3]["role"] == "assistant"
    assert entries[3]["text"] == "final message"


def test_tail_cap_200():
    """Transcript viewer caps rendered rows at 200 tail (TX_MAX from Svelte)."""
    # Simulate 250 lines
    lines = [json.dumps({"Role": "user", "Text": f"message {i}"}) for i in range(250)]
    entries = [parse_transcript_line(line) for line in lines]

    TX_MAX = 200
    shown = entries[-TX_MAX:] if len(entries) > TX_MAX else entries

    assert len(shown) == 200
    assert shown[0]["text"] == "message 50"  # (250 - 200)
    assert shown[-1]["text"] == "message 249"
