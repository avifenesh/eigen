"""
transcript_logic.py — Pure event-folding logic for TranscriptModel (unit-testable).

Separates StreamEventDTO → row-ops logic from Qt boilerplate. Functions here are pure
(no Qt dependencies) and tested via pytest with recorded event fixtures.
"""

from typing import Any, Literal


# Row kinds (transcript row types)
RowKind = Literal["user", "assistant", "tool", "note", "approval"]


class TranscriptRow:
    """A single transcript row (user/assistant/tool/note/approval)."""

    def __init__(
        self,
        kind: RowKind,
        text: str = "",
        tool_name: str = "",
        tool_id: str = "",
        tool_args: str = "",
        tool_status: Literal["running", "success", "error"] = "running",
        streaming: bool = False,
        reasoning: str = "",
        step: int = 0,
    ):
        self.kind = kind
        self.text = text
        self.tool_name = tool_name
        self.tool_id = tool_id
        self.tool_args = tool_args
        self.tool_status = tool_status
        self.streaming = streaming
        self.reasoning = reasoning
        self.step = step

    def to_dict(self) -> dict[str, Any]:
        """Convert to dict for Qt data() method."""
        return {
            "kind": self.kind,
            "text": self.text,
            "toolName": self.tool_name,
            "toolId": self.tool_id,
            "toolArgs": self.tool_args,
            "toolStatus": self.tool_status,
            "streaming": self.streaming,
            "reasoning": self.reasoning,
            "step": self.step,
        }


class RowOp:
    """
    A row operation (insert/update/remove).

    TranscriptModel applies these ops as beginInsertRows/dataChanged/beginRemoveRows.
    """

    def __init__(
        self,
        op: Literal["insert", "update", "remove"],
        row: int,
        data: TranscriptRow | None = None,
    ):
        self.op = op
        self.row = row
        self.data = data


def seed_from_state(state: dict) -> list[TranscriptRow]:
    """
    Build initial transcript rows from State RPC result.

    State.messages is []MessageDTO (PascalCase llm.Message from Go). Normalize to rows:
    - role: user/assistant/tool/...
    - assistant with tool_calls → assistant text row + tool rows
    - tool results → already handled by tool rows from prior assistant
    """
    rows: list[TranscriptRow] = []
    for msg in state.get("messages") or []:
        role = msg.get("role", "")
        text = msg.get("text", "")
        reasoning = msg.get("reasoning", "")
        tool_calls = msg.get("toolCalls") or []
        tool_call_id = msg.get("toolCallId", "")
        tool_name = msg.get("toolName", "")
        tool_error = msg.get("toolError", False)

        if role == "user":
            rows.append(TranscriptRow(kind="user", text=text))
        elif role == "assistant":
            # Assistant text (may be empty if only tool calls)
            if text or reasoning:
                rows.append(
                    TranscriptRow(kind="assistant", text=text, reasoning=reasoning, streaming=False)
                )
            # A stored snapshot can land while a tool is still in flight. Keep
            # unmatched calls running in that case so the hydrated ToolCallCard
            # stays open; a following tool message resolves it below.
            initial_tool_status = "running" if state.get("running", False) else "success"
            for tc in tool_calls:
                rows.append(
                    TranscriptRow(
                        kind="tool",
                        tool_name=tc.get("name", ""),
                        tool_id=tc.get("id", ""),
                        tool_args=tc.get("args", ""),
                        tool_status=initial_tool_status,
                        text="",  # result filled by later tool role
                    )
                )
        elif role == "tool":
            # Tool result: find matching tool row by tool_call_id, update text
            for row in reversed(rows):
                if row.kind == "tool" and row.tool_id == tool_call_id:
                    row.text = text
                    row.tool_status = "error" if tool_error else "success"
                    break
        # Note: daemon doesn't emit "note" in messages seed, only as events

    return rows


def fold_event(rows: list[TranscriptRow], event: dict, replay: bool) -> list[RowOp]:
    """
    Fold a StreamEventDTO event into the transcript, returning row operations.

    Event kinds (from daemon protocol):
    - "text": assistant text delta → APPEND to current streaming assistant row
    - "reasoning": reasoning delta → APPEND to reasoning field
    - "tool_start": new tool invocation → INSERT tool row (status: running)
    - "tool_result": tool completed → UPDATE matching tool row (status: success/error, text: result)
    - "done": turn finished → mark current assistant row as non-streaming
    - "note": agent note → INSERT note row
    - "approval": approval required → INSERT approval row
    - "bg_done": background task done (ignore, handled by note)

    Replay events: re-apply without creating duplicates (idempotent fold).
    """
    ops: list[RowOp] = []
    kind = event.get("kind", "")
    text = event.get("text", "")
    tool_name = event.get("tool", "")
    tool_id = event.get("toolId", "")
    tool_args = event.get("toolArgs", "")
    result = event.get("result", "")
    is_error = event.get("isError", False)
    step = event.get("step", 0)

    if kind == "text":
        # Text delta: append to current streaming assistant row, or create one
        if rows and rows[-1].kind == "assistant" and rows[-1].streaming:
            rows[-1].text += text
            ops.append(RowOp("update", len(rows) - 1, rows[-1]))
        else:
            # New assistant turn
            row = TranscriptRow(kind="assistant", text=text, streaming=True, step=step)
            rows.append(row)
            ops.append(RowOp("insert", len(rows) - 1, row))

    elif kind == "reasoning":
        # Reasoning delta: append to the current assistant row, or create a
        # streaming assistant row when providers emit thinking before visible
        # answer text.  Dropping leading reasoning made the Qt chat look idle
        # until a later refresh rebuilt the transcript from State.
        if rows and rows[-1].kind == "assistant" and rows[-1].streaming:
            rows[-1].reasoning += text
            ops.append(RowOp("update", len(rows) - 1, rows[-1]))
        else:
            row = TranscriptRow(kind="assistant", reasoning=text, streaming=True, step=step)
            rows.append(row)
            ops.append(RowOp("insert", len(rows) - 1, row))

    elif kind == "tool_start":
        # Tool invocation: insert tool row (status: running)
        row = TranscriptRow(
            kind="tool",
            tool_name=tool_name,
            tool_id=tool_id,
            tool_args=tool_args,
            tool_status="running",
            step=step,
        )
        rows.append(row)
        ops.append(RowOp("insert", len(rows) - 1, row))

    elif kind == "tool_result":
        # Tool result: find matching tool row, update status + text
        for i, row in enumerate(reversed(rows)):
            if row.kind == "tool" and row.tool_id == tool_id:
                idx = len(rows) - 1 - i
                rows[idx].text = result
                rows[idx].tool_status = "error" if is_error else "success"
                ops.append(RowOp("update", idx, rows[idx]))
                break

    elif kind == "done":
        # Turn finished: mark current assistant row as non-streaming
        # (Find last assistant row, not necessarily the last row overall)
        for i in range(len(rows) - 1, -1, -1):
            if rows[i].kind == "assistant" and rows[i].streaming:
                rows[i].streaming = False
                ops.append(RowOp("update", i, rows[i]))
                break

    elif kind == "note":
        # Agent note: insert note row
        row = TranscriptRow(kind="note", text=text, step=step)
        rows.append(row)
        ops.append(RowOp("insert", len(rows) - 1, row))

    elif kind == "approval":
        # Approval required: insert approval row (result field = approval ID)
        row = TranscriptRow(kind="approval", text=text, tool_name=result, step=step)
        rows.append(row)
        ops.append(RowOp("insert", len(rows) - 1, row))

    return ops


def rebuild_from_state(state: dict) -> list[TranscriptRow]:
    """Rebuild transcript from State RPC (on dropped event)."""
    return seed_from_state(state)
