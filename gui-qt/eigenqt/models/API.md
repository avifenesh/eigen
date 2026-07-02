# eigenqt.models API — Qt Models for Session Views

Qt models bridging `eigenqt.rpc.RpcClient` to QML views. All models are QAbstractListModel subclasses exposing data via Qt roles.

---

## SessionsModel

**Purpose:** Sessions list (id, title, dir, model, status, turns, updated). Auto-populated from `Sessions` RPC; live-updated from session events.

### Constructor

```python
SessionsModel(client: RpcClient, parent: Optional[QObject] = None)
```

- `client`: RpcClient instance (must be connected or will connect)
- Auto-fetches sessions on `connected` signal
- Subscribes to session events for live updates

### Qt Roles

- `IdRole` (Qt.UserRole + 1): session ID (str)
- `TitleRole` (Qt.UserRole + 2): session title (str)
- `DirRole` (Qt.UserRole + 3): working directory (str)
- `ModelRole` (Qt.UserRole + 4): LLM model name (str)
- `StatusRole` (Qt.UserRole + 5): status ("idle", "running", "approval", etc.)
- `TurnsRole` (Qt.UserRole + 6): turn count (int)
- `UpdatedRole` (Qt.UserRole + 7): last updated timestamp (int64, milliseconds)

### Methods

#### `refresh()`
Manually trigger a sessions list refresh (e.g., after `RemoveSession`).

### Sorting

Sessions are sorted **newest-updated first** (mirrors Svelte).

### Usage Example

```python
from eigenqt.rpc import RpcClient
from eigenqt.models import SessionsModel

client = RpcClient()
sessions_model = SessionsModel(client)

# QML binding
# ListView { model: sessionsModel; delegate: ... }
```

---

## TranscriptModel

**Purpose:** Session transcript (user/assistant/tool/note/approval rows). Fed by `State` RPC seed + `session:<id>` StreamEventDTO events. **16ms delta coalescing** for smooth 60fps updates.

### Constructor

```python
TranscriptModel(client: RpcClient, session_id: str, parent: Optional[QObject] = None)
```

- `client`: RpcClient instance
- `session_id`: session ID to subscribe to
- Auto-subscribes to `session:<id>` event channel

### Qt Roles

- `KindRole` (Qt.UserRole + 1): row kind ("user", "assistant", "tool", "note", "approval")
- `TextRole` (Qt.UserRole + 2): message text (str)
- `ToolNameRole` (Qt.UserRole + 3): tool name (for tool rows)
- `ToolIdRole` (Qt.UserRole + 4): tool call ID (for tool rows)
- `ToolArgsRole` (Qt.UserRole + 5): tool arguments JSON (str)
- `ToolStatusRole` (Qt.UserRole + 6): tool status ("running", "success", "error")
- `StreamingRole` (Qt.UserRole + 7): is streaming (bool)
- `ReasoningRole` (Qt.UserRole + 8): reasoning text (str, for extended thinking)
- `StepRole` (Qt.UserRole + 9): step number (int, for multi-step turns)

### Methods

#### `seed(state: dict)`
Seed transcript from `State` RPC result (initial load). Call once after construction.

**State schema:**
```python
{
    "messages": [
        {"role": "user", "text": "...", ...},
        {"role": "assistant", "text": "...", "toolCalls": [...], ...},
        ...
    ],
    "pending": [...],  # approvals
    ...
}
```

#### `detach()`
Detach from session (unsubscribe, stop coalescing timer). Call before destroying model.

### Event Handling

**Supported event kinds** (from `WireEventDTO`):
- `"text"`: assistant text delta → APPEND to streaming row
- `"reasoning"`: reasoning delta → APPEND to reasoning field
- `"tool_start"`: new tool invocation → INSERT tool row (status: running)
- `"tool_result"`: tool completed → UPDATE tool row (status: success/error, text: result)
- `"done"`: turn finished → mark assistant row as non-streaming
- `"note"`: agent note → INSERT note row
- `"approval"`: approval required → INSERT approval row

**16ms delta coalescing:**
- Text deltas buffer for 16ms (one 60fps frame)
- ONE `dataChanged` signal per frame max (prevents UI stutter)
- QTimer restarts on each delta

**Dropped events:**
- On `dropped` signal (channel overflow), model refetches `State` and rebuilds (the ONE allowed model reset)

### Usage Example

```python
from eigenqt.rpc import RpcClient
from eigenqt.models import TranscriptModel

client = RpcClient()
model = TranscriptModel(client, "session-id-123")

# Seed from State
client.call("State", args=["session-id-123"], callback=lambda r: model.seed(r["result"]))

# QML binding
# ListView { model: transcriptModel; delegate: ... }

# Cleanup
model.detach()
```

---

## ApprovalsModel

**Purpose:** Pending approvals for a session (id, tool, args). Driven from `session:<id>` events + `State.pending` seed.

### Constructor

```python
ApprovalsModel(client: RpcClient, session_id: str, parent: Optional[QObject] = None)
```

- `client`: RpcClient instance
- `session_id`: session ID to subscribe to
- Auto-subscribes to `session:<id>` event channel

### Qt Roles

- `IdRole` (Qt.UserRole + 1): approval ID (str)
- `ToolRole` (Qt.UserRole + 2): tool name (str)
- `ArgsRole` (Qt.UserRole + 3): tool arguments summary (str)

### Methods

#### `seed(state: dict)`
Seed approvals from `State` RPC result (initial load).

**State schema:**
```python
{
    "pending": [
        {"id": "appr_123", "tool": "Bash", "args": "rm -rf /"},
        ...
    ],
    ...
}
```

#### `approve(approval_id: str, allow: bool)`
Approve or deny an approval via RPC `Approve(session_id, approval_id, allow)`. On success, removes approval from list.

#### `detach()`
Detach from session (unsubscribe). Call before destroying model.

### Usage Example

```python
from eigenqt.rpc import RpcClient
from eigenqt.models import ApprovalsModel

client = RpcClient()
approvals = ApprovalsModel(client, "session-id-123")

# Seed from State
client.call("State", args=["session-id-123"], callback=lambda r: approvals.seed(r["result"]))

# Approve
approvals.approve("appr_123", allow=True)

# QML binding
# ListView { model: approvalsModel; delegate: Button { onClicked: approvalsModel.approve(id, true) } }

# Cleanup
approvals.detach()
```

---

## Internal: transcript_logic.py

**Purpose:** Pure event-folding logic (unit-testable, no Qt dependencies).

### Functions

#### `seed_from_state(state: dict) -> list[TranscriptRow]`
Build initial transcript rows from State RPC result. Normalizes PascalCase `MessageDTO` to `TranscriptRow`.

#### `fold_event(rows: list[TranscriptRow], event: dict, replay: bool) -> list[RowOp]`
Fold a StreamEventDTO event into the transcript, returning row operations (insert/update/remove).

#### `rebuild_from_state(state: dict) -> list[TranscriptRow]`
Rebuild transcript from State RPC (on dropped event). Alias for `seed_from_state`.

### Data Structures

#### `TranscriptRow`
```python
TranscriptRow(
    kind: "user" | "assistant" | "tool" | "note" | "approval",
    text: str = "",
    tool_name: str = "",
    tool_id: str = "",
    tool_args: str = "",
    tool_status: "running" | "success" | "error" = "running",
    streaming: bool = False,
    reasoning: str = "",
    step: int = 0,
)
```

#### `RowOp`
```python
RowOp(
    op: "insert" | "update" | "remove",
    row: int,
    data: TranscriptRow | None = None,
)
```

---

## Threading Model

- **All socket I/O + JSON decode on worker threads** (via `RpcClient`)
- **Models live on GUI thread** (QAbstractListModel requirement)
- **Event folding is synchronous** (fold_event called on GUI thread)
- **16ms coalescing timer runs on GUI thread** (QTimer)

---

## Testing

### Unit Tests (pytest)

```bash
cd gui-qt
.venv/bin/python -m pytest tests/test_transcript_logic.py -v
```

**Coverage:**
- Empty state seed
- User/assistant messages
- Tool calls (start → result)
- Text deltas (append)
- Done event (mark non-streaming)
- Note/approval events
- Full sequence (user → text deltas → tool → done)

### Live Verification (headless)

```bash
cd gui-qt
python verify_models.py [session_id]
```

Seeds TranscriptModel from real session State, monitors live events for 5s, prints row count/last row.

---

## Design Notes

### Why 16ms coalescing?
Text deltas can arrive at 100+ Hz (LLM streaming). Without coalescing, each delta triggers a `dataChanged` signal → QML relayout → UI stutter. Buffering deltas for one frame (16ms @ 60fps) guarantees **one update per frame max**, smooth 60fps.

### Why separate transcript_logic.py?
Pure event-folding logic (no Qt) is unit-testable via pytest with recorded fixtures. Models layer wraps this with Qt boilerplate (signals, roles, beginInsertRows).

### Why per-session models?
Each session has its own event stream (`session:<id>`). Creating one model per active session (view-lifecycle pattern) avoids multiplexing all events into one global model.

### Why dropped → rebuild?
On channel overflow (guiserver sends `{"event":"dropped","channel":"..."}`), the client has missed events → transcript may be incomplete. The ONE safe recovery: refetch State (full session snapshot) and rebuild. This is the ONLY model reset after seed.

---

## Next Steps (Shell Phase)

Once guiserver is implemented (Phase A day 2-3):
1. Run `verify_models.py <existing-session-id>` → verify live event streaming
2. Build QML chat view:
   - `SessionsModel` → session list
   - `TranscriptModel` → chat transcript (ListView with delegates per row kind)
   - `ApprovalsModel` → approval prompt (Sheet with allow/deny buttons)
3. Hook up input: `RpcClient.call("SendInput", args=[session_id, text, [], []])`
4. Hook up interrupt: `RpcClient.call("Interrupt", args=[session_id])`

---

## File Paths

- `/home/avifenesh/projects/eigen/gui-qt/eigenqt/models/__init__.py`
- `/home/avifenesh/projects/eigen/gui-qt/eigenqt/models/sessions.py`
- `/home/avifenesh/projects/eigen/gui-qt/eigenqt/models/transcript.py`
- `/home/avifenesh/projects/eigen/gui-qt/eigenqt/models/transcript_logic.py`
- `/home/avifenesh/projects/eigen/gui-qt/eigenqt/models/approvals.py`
- `/home/avifenesh/projects/eigen/gui-qt/tests/test_transcript_logic.py`
- `/home/avifenesh/projects/eigen/gui-qt/verify_models.py`
- `/home/avifenesh/projects/eigen/gui-qt/pytest.ini`
