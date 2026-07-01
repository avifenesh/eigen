# eigenqt.rpc API Documentation

## Overview

The `eigenqt.rpc` package provides thread-safe communication with the guiserver daemon using Qt's signal/slot mechanism. All socket I/O and JSON parsing happen on worker threads; results cross to the GUI thread via queued signals.

## Architecture

```
┌─────────────┐
│  GUI Thread │  RpcClient, signals, callbacks
└──────┬──────┘
       │ Queued signals (PayloadWrapper objects)
       │
       ├─────────────┐
       │             │
┌──────▼─────┐  ┌───▼──────┐
│ RPC Worker │  │  Events  │  Socket I/O + json.loads on worker threads
│   Thread   │  │  Worker  │
└────────────┘  └──────────┘
       │             │
       └─────┬───────┘
             │ Unix socket
        ┌────▼────┐
        │guiserver│
        └─────────┘
```

## Core Classes

### `RpcClient`

Thread-safe guiserver RPC client. Owns two QThread-hosted workers (RPC + events).

**Signals:**
- `connected()` — Both connections ready
- `disconnected(str reason)` — Disconnect detected
- `event(str channel, dict data)` — Event received
- `dropped(str channel)` — Channel dropped (queue overflow)

**Methods:**

```python
__init__(sock_path: Path = None, parent: QObject = None)
```
Create client (default sock_path: `~/.eigen/guiserver.sock`).

```python
call(method: str, *args, callback: Callable[[Any], None] = None) -> None
```
Async RPC call. Result delivered to callback on GUI thread.

Callback receives: `{"result": ...}` or `{"error": "..."}`.

Example:
```python
def on_sessions(result):
    if "error" in result:
        print(f"Error: {result['error']}")
    else:
        sessions = result["result"]
        print(f"Got {len(sessions)} sessions")

client.call("Sessions", callback=on_sessions)
```

```python
call_sync(method: str, *args, timeout: float = 5.0) -> Any
```
Sync RPC call (blocks until response or timeout). Returns result or raises `RuntimeError`/`TimeoutError`.

Example:
```python
try:
    sessions = client.call_sync("Sessions", timeout=5.0)
    print(f"Got {len(sessions)} sessions")
except TimeoutError:
    print("RPC timed out")
except RuntimeError as e:
    print(f"RPC error: {e}")
```

```python
subscribe(channels: list[str]) -> None
```
Subscribe to event channels. Channels:
- `"eigen:daemon:stats"` — daemon stats (every ~2s)
- `"session:<id>"` — session events (transcript deltas, approvals, etc.)
- `"feed"` — feed updates
- Other channels per `internal/gui/bridge.manifest.json`

```python
unsubscribe(channels: list[str]) -> None
```
Unsubscribe from channels.

```python
shutdown() -> None
```
Gracefully shut down (call before app exit).

---

### `GuiserverSupervisor`

Find-or-spawn guiserver with auto-respawn on manifest mismatch (the stale-inode trap mitigation).

**Signals:**
- `mismatch(str sha, str manifest)` — Manifest mismatch detected before respawn

**Methods:**

```python
__init__(parent: QObject = None)
```
Create supervisor. Auto-discovers binary path:
1. `EIGEN_BIN` env var
2. `../bin/eigen` (sibling to gui-qt/)
3. `eigen` in PATH

```python
ensure_running(timeout: float = 10.0) -> dict[str, Any]
```
Ensure guiserver is running and responsive. Returns hello payload:
```python
{
    "sha": "abc123...",     # binary SHA256
    "manifest": "def456..."  # manifest hash
}
```

Lifecycle:
1. Try connect to existing socket
2. If no socket, spawn guiserver from `binary_path`
3. Poll hello (up to `timeout`)
4. Compare hello.manifest vs on-disk binary expectation
5. On mismatch: kill, respawn once, re-check (if still mismatch, raise)

Raises:
- `TimeoutError` — guiserver didn't respond within timeout
- `RuntimeError` — spawn/connect failure or persistent mismatch

```python
shutdown() -> None
```
Gracefully shut down supervisor (does NOT kill guiserver — it lingers per plan).

---

## Usage Patterns

### Basic Usage

```python
from eigenqt.rpc.client import RpcClient

client = RpcClient()

@Slot()
def on_connected():
    print("Connected!")
    client.call("Sessions", callback=on_sessions)

@Slot(str, dict)
def on_event(channel, data):
    print(f"Event on {channel}: {data}")

client.connected.connect(on_connected)
client.event.connect(on_event)

# Subscribe to channels
client.subscribe(["eigen:daemon:stats"])
```

### With Supervision

```python
from eigenqt.rpc.client import RpcClient
from eigenqt.rpc.supervise import GuiserverSupervisor

supervisor = GuiserverSupervisor()

@Slot(str, str)
def on_mismatch(sha, manifest):
    print(f"Manifest mismatch! Auto-respawning...")

supervisor.mismatch.connect(on_mismatch)

try:
    hello = supervisor.ensure_running(timeout=10.0)
    print(f"guiserver ready: {hello['sha'][:8]}...")
except Exception as e:
    print(f"Failed to start guiserver: {e}")
    sys.exit(1)

# Now safe to connect
client = RpcClient()
# ... rest of app ...
```

### Sync RPC for Startup

```python
# Blocking startup sequence
try:
    hello = client.call_sync("hello", timeout=5.0)
    sessions = client.call_sync("Sessions", timeout=5.0)
    print(f"Loaded {len(sessions)} sessions")
except Exception as e:
    print(f"Startup RPC failed: {e}")
```

---

## Wire Protocol Reference

### Connection Setup

1. Client connects to `~/.eigen/guiserver.sock`
2. Client sends role declaration: `{"role":"rpc"}` or `{"role":"events"}`
3. Server accepts both connections from same client

### RPC Connection

**Request:**
```json
{"id": 1, "call": "MethodName", "args": ["arg1", "arg2"]}
```

**Response:**
```json
{"id": 1, "result": {...}}
```
or
```json
{"id": 1, "error": "error message"}
```

Responses may arrive out-of-order (id-multiplexed).

### Events Connection

**Subscribe:**
```json
{"sub": ["channel1", "channel2", ...]}
```

**Unsubscribe:**
```json
{"unsub": ["channel1", ...]}
```

**Event (data):**
```json
{"event": "data", "channel": "session:xyz", "data": {...}}
```

**Event (dropped):**
```json
{"event": "dropped", "channel": "session:xyz"}
```

### Line Budget

Max line size: **32 MB**. Both client and server enforce this limit.

---

## 8MB Decode Spike Results

**Test:** `eigenqt/rpc/spike_decode.py` (mandatory risk spike from plan §8 day 6-7)

Generated a 6.84 MB session-state-like JSON payload, measured:

- **Worker-thread decode time:** 27.85 ms
- **GUI-thread max gap:** 28.35 ms

**Target:** GUI-thread stall < 32 ms (60fps budget × 2)

**Result:** ✓ PASS

**Mitigation applied:** PayloadWrapper (opaque object) instead of dict for queued signals. Avoids deep-copy overhead during signal handoff.

**Implication:** The two-connection + worker-thread decode architecture is safe for 8MB state payloads. No blocking GUI freeze during large state loads.

---

## Threading Model

### Worker Threads

- **RpcWorker** runs on dedicated `QThread`, handles:
  - Socket connect/read/write
  - `json.loads()` (blocking, on worker thread)
  - Request id multiplexing
  
- **EventsWorker** runs on dedicated `QThread`, handles:
  - Socket connect/read
  - `json.loads()` (blocking, on worker thread)
  - Event fan-out via signals

### Signal Safety

All cross-thread signals use `Qt.QueuedConnection`:
- `ready()`, `error(str)`, `response(int, dict)` (RPC worker → GUI)
- `event_data(str, dict)`, `event_dropped(str)` (Events worker → GUI)

PayloadWrapper objects cross thread boundaries safely (reference passed, no deep-copy).

### Reconnect Backoff

On disconnect, client auto-reconnects with exponential backoff:
- 1s, 2s, 4s, 8s, ..., max 15s
- Reset to 1s on successful connect

---

## RPC Methods (Bridge Contract)

Full list in `internal/gui/bridge.manifest.json`. Key methods:

**Sessions:**
- `Sessions() -> []SessionInfoDTO`
- `State(session_id: str) -> SessionStateDTO`
- `NewSession(model: str, dir: str, goal: str) -> str`
- `RemoveSession(session_id: str)`

**I/O:**
- `SendInput(session_id: str, text: str, images: []ImageDTO, files: []str)`
- `SteerInput(session_id: str, text: str, images: []ImageDTO) -> bool`
- `Interrupt(session_id: str) -> bool`

**Approvals:**
- `Approve(session_id: str, approval_id: str, allow: bool)`

**Tasks (agent subsystem):**
- `Agents() -> AgentsDTO`
- `CancelAgent(task_id: str)`
- `AgentHistory(session_id: str) -> []BgTaskDTO`

**Daemon:**
- `Stats() -> DaemonStats`

See manifest for full signature + DTO shapes.

---

## Known Limitations

1. **Manifest hash heuristic:** `GuiserverSupervisor._compute_expected_manifest()` currently uses binary mtime+size as a proxy. TODO: parse `internal/gui/bridge.manifest.json` and hash it for real contract enforcement.

2. **Kill strategy:** If supervisor didn't spawn guiserver, it tries `fuser` to find pid. If `fuser` unavailable, falls back to removing socket (may fail if guiserver still holds it).

3. **Multi-client:** Per plan, all connections are accepted. No refusal logic. Loop-flock (in guiserver) prevents double background loops; per-connection subscriptions scope events.

---

## Next Steps (for models phase)

1. **DTOs:** Define typed dataclasses for common payloads:
   - `SessionInfoDTO`, `SessionStateDTO`, `StreamEventDTO`
   - `BgTaskDTO`, `FeedItemDTO`, `DaemonStats`
   
   Or keep dicts — plan defers `types.py` generation until dict-wrangling demonstrably hurts.

2. **Models:** `TranscriptModel(QAbstractListModel)` wrapping session transcript with 16ms delta coalescing, per-row `dataChanged` (never model reset), fed by `client.event("session:<id>", data)`.

3. **Live verification:** Once guiserver Go implementation lands (phase A day 2-3), run `verify_live.py` against real guiserver for end-to-end protocol verification.
