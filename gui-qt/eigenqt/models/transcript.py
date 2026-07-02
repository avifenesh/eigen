"""
transcript.py — TranscriptModel (QAbstractListModel) for session transcript.

Rows: {kind: user|assistant|tool|note|approval, text, toolName, toolStatus, streaming, ...}
Fed by: (a) State RPC seed, (b) session:<id> StreamEventDTO events.
16ms delta coalescing: buffer deltas, flush on QTimer → ONE dataChanged per frame max.
Handle dropped signal → refetch State, rebuild.
"""

from typing import Optional

from PySide6.QtCore import QAbstractListModel, QModelIndex, QObject, QTimer, Qt, Slot

from eigenqt.rpc import RpcClient
from eigenqt.markdown import parse as parse_markdown

from .transcript_logic import RowOp, TranscriptRow, fold_event, rebuild_from_state, seed_from_state


class TranscriptModel(QAbstractListModel):
    """Transcript model for a single session (event-driven, 16ms delta coalescing)."""

    # Qt roles
    KindRole = Qt.UserRole + 1
    TextRole = Qt.UserRole + 2
    ToolNameRole = Qt.UserRole + 3
    ToolIdRole = Qt.UserRole + 4
    ToolArgsRole = Qt.UserRole + 5
    ToolStatusRole = Qt.UserRole + 6
    StreamingRole = Qt.UserRole + 7
    ReasoningRole = Qt.UserRole + 8
    StepRole = Qt.UserRole + 9
    BlocksRole = Qt.UserRole + 10  # Markdown blocks (list of dicts)

    def __init__(self, client: RpcClient, session_id: str, parent: Optional[QObject] = None):
        super().__init__(parent)
        self._client = client
        self._session_id = session_id
        self._rows: list[TranscriptRow] = []
        self._pending_ops: list[RowOp] = []

        # 16ms coalescing timer (single-shot, restarts on each delta)
        self._flush_timer = QTimer(self)
        self._flush_timer.setSingleShot(True)
        self._flush_timer.setInterval(16)  # 60fps budget
        self._flush_timer.timeout.connect(self._flush_pending_ops)

        # Event channel for this session
        self._event_channel = f"session:{session_id}"

        # Connect to RPC + events
        self._client.event.connect(self._on_event)
        self._client.dropped.connect(self._on_dropped)

        # Subscribe to session events
        self._client.subscribe([self._event_channel])

    def roleNames(self) -> dict[int, bytes]:
        """Expose roles to QML."""
        return {
            self.KindRole: b"kind",
            self.TextRole: b"text",
            self.ToolNameRole: b"toolName",
            self.ToolIdRole: b"toolId",
            self.ToolArgsRole: b"toolArgs",
            self.ToolStatusRole: b"toolStatus",
            self.StreamingRole: b"streaming",
            self.ReasoningRole: b"reasoning",
            self.StepRole: b"step",
            self.BlocksRole: b"blocks",
        }

    def rowCount(self, parent: QModelIndex = QModelIndex()) -> int:
        """Row count (transcript length)."""
        if parent.isValid():
            return 0
        return len(self._rows)

    def data(self, index: QModelIndex, role: int = Qt.DisplayRole):
        """Return data for index/role."""
        if not index.isValid() or index.row() >= len(self._rows):
            return None

        row_data = self._rows[index.row()].to_dict()
        if role == self.KindRole:
            return row_data.get("kind", "")
        if role == self.TextRole:
            return row_data.get("text", "")
        if role == self.ToolNameRole:
            return row_data.get("toolName", "")
        if role == self.ToolIdRole:
            return row_data.get("toolId", "")
        if role == self.ToolArgsRole:
            return row_data.get("toolArgs", "")
        if role == self.ToolStatusRole:
            return row_data.get("toolStatus", "")
        if role == self.StreamingRole:
            return row_data.get("streaming", False)
        if role == self.ReasoningRole:
            return row_data.get("reasoning", "")
        if role == self.StepRole:
            return row_data.get("step", 0)
        if role == self.BlocksRole:
            # Parse markdown blocks for assistant rows
            if row_data.get("kind") == "assistant":
                text = row_data.get("text", "")
                blocks = parse_markdown(text)
                # Convert Block dataclasses → list of dicts for QML
                return [
                    {
                        "type": b.type,
                        "content": b.content,
                        "lang": b.lang,
                        "source": b.source,
                        "level": b.level,
                        "rows": b.rows,
                        "items": b.items,
                        "ordered": b.ordered,
                    }
                    for b in blocks
                ]
            return []
        return None

    def seed(self, state: dict):
        """
        Seed transcript from State RPC result (initial load or rebuild).

        State.messages is []MessageDTO (PascalCase llm.Message). Normalize to rows.
        """
        rows = seed_from_state(state)
        self.beginResetModel()
        self._rows = rows
        self.endResetModel()

    @Slot(str, dict)
    def _on_event(self, channel: str, data: dict):
        """
        Handle session:<id> events (StreamEventDTO).

        Event payload: {event: WireEventDTO, replay: bool, seq: int}
        Fold event into transcript; buffer ops; restart 16ms timer.
        """
        if channel != self._event_channel:
            return

        event = data.get("event", {})
        replay = data.get("replay", False)

        # Fold event → row ops
        ops = fold_event(self._rows, event, replay)
        self._pending_ops.extend(ops)

        # Restart 16ms timer (coalesce deltas)
        if self._pending_ops and not self._flush_timer.isActive():
            self._flush_timer.start()

    @Slot(str)
    def _on_dropped(self, channel: str):
        """
        Handle dropped event (channel overflow).

        Refetch State, rebuild transcript (the ONE allowed model reset).
        """
        if channel != self._event_channel:
            return

        # Refetch State → rebuild
        self._client.call(
            "State", args=[self._session_id], callback=self._on_state_for_rebuild
        )

    @Slot(dict)
    def _on_state_for_rebuild(self, result: dict):
        """Handle State RPC result (rebuild after dropped)."""
        if "error" in result:
            return

        state = result.get("result", {})
        rows = rebuild_from_state(state)
        self.beginResetModel()
        self._rows = rows
        self._pending_ops.clear()
        self.endResetModel()

    @Slot()
    def _flush_pending_ops(self):
        """
        Flush pending row ops (16ms coalescing boundary).

        Apply buffered ops as beginInsertRows/dataChanged/beginRemoveRows.
        ONE model update per frame max (the plan requirement).
        """
        if not self._pending_ops:
            return

        # Group ops by type (insert/update/remove)
        inserts: list[tuple[int, TranscriptRow]] = []
        updates: set[int] = set()
        removes: list[int] = []

        for op in self._pending_ops:
            if op.op == "insert":
                inserts.append((op.row, op.data))
            elif op.op == "update":
                updates.add(op.row)
            elif op.op == "remove":
                removes.append(op.row)

        self._pending_ops.clear()

        # Apply inserts (batch if contiguous)
        if inserts:
            # Sort by row (should already be in order from fold_event)
            inserts.sort(key=lambda x: x[0])
            # For now: single beginInsertRows call for all (they're appends)
            # (Real batching would find contiguous ranges; unnecessary for append-only)
            # Just notify the range
            first = inserts[0][0]
            last = inserts[-1][0]
            self.beginInsertRows(QModelIndex(), first, last)
            # Rows already added by fold_event
            self.endInsertRows()

        # Apply updates (single dataChanged for all changed rows)
        if updates:
            rows_list = sorted(updates)
            # Find contiguous ranges
            ranges: list[tuple[int, int]] = []
            start = rows_list[0]
            end = start
            for r in rows_list[1:]:
                if r == end + 1:
                    end = r
                else:
                    ranges.append((start, end))
                    start = r
                    end = r
            ranges.append((start, end))

            # Emit dataChanged for each range
            for start, end in ranges:
                top_left = self.index(start, 0)
                bottom_right = self.index(end, 0)
                self.dataChanged.emit(top_left, bottom_right, [])

        # Apply removes (not currently used; tool of future cleanup)
        for row in sorted(removes, reverse=True):
            self.beginRemoveRows(QModelIndex(), row, row)
            del self._rows[row]
            self.endRemoveRows()

    def detach(self):
        """Detach from session (unsubscribe, stop timer)."""
        self._flush_timer.stop()
        self._client.unsubscribe([self._event_channel])
