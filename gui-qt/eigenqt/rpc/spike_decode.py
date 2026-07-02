#!/usr/bin/env python3
"""
spike_decode.py — 8MB JSON decode spike (mandatory measurement for plan §8 day 6-7).

Generates an 8MB session-state-like JSON payload, measures:
  (a) json.loads time on worker thread
  (b) GUI-thread stall during decode+signal handoff (max inter-tick gap)

Target: GUI-thread max gap < 32ms during an 8MB decode (60fps = 16.67ms budget,
        with 2x margin for safety).

Usage:
    cd gui-qt && .venv/bin/python3 eigenqt/rpc/spike_decode.py

Returns:
    Exit 0 with measurements printed
    Exit 1 on failure (GUI-thread stall > 32ms — architecture risk)
"""

import json
import sys
import time
from typing import Optional

from PySide6.QtCore import QCoreApplication, QObject, QThread, QTimer, Signal, Slot
from PySide6.QtCore import Qt


class PayloadWrapper:
    """Opaque wrapper for parsed payload (avoids deep-copy on signal handoff)."""

    def __init__(self, data: dict):
        self.data = data


class SpikeWorker(QObject):
    """Worker that decodes 8MB JSON on a background thread."""

    decoded = Signal(object)  # Emitted with PayloadWrapper (queued → GUI thread)

    def __init__(self, payload_json: str):
        super().__init__()
        self.payload_json = payload_json
        self.decode_ms: Optional[float] = None

    @Slot()
    def decode(self) -> None:
        """Decode JSON on worker thread and emit result."""
        start = time.perf_counter()
        parsed = json.loads(self.payload_json)
        end = time.perf_counter()

        self.decode_ms = (end - start) * 1000.0
        # Wrap in opaque object to avoid deep-copy on queued signal
        self.decoded.emit(PayloadWrapper(parsed))


class GuiThreadMonitor(QObject):
    """Monitors GUI thread responsiveness via 60fps timer."""

    finished = Signal(float)  # Emitted with max_gap_ms when test completes

    def __init__(self):
        super().__init__()
        self.last_tick = time.perf_counter()
        self.max_gap_ms = 0.0
        self.tick_count = 0
        self.running = False

        self.timer = QTimer()
        self.timer.timeout.connect(self._on_tick)

    def start(self) -> None:
        """Start 60fps monitoring."""
        self.last_tick = time.perf_counter()
        self.max_gap_ms = 0.0
        self.tick_count = 0
        self.running = True
        self.timer.start(16)  # ~60fps

    def stop(self) -> None:
        """Stop monitoring and emit result."""
        self.running = False
        self.timer.stop()
        self.finished.emit(self.max_gap_ms)

    @Slot()
    def _on_tick(self) -> None:
        """Timer tick — measure inter-tick gap."""
        now = time.perf_counter()
        gap_ms = (now - self.last_tick) * 1000.0
        self.max_gap_ms = max(self.max_gap_ms, gap_ms)
        self.last_tick = now
        self.tick_count += 1


def generate_8mb_payload() -> str:
    """
    Generate an 8MB session-state-like JSON payload.

    Shape mirrors SessionStateDTO: nested transcript with many turns, each with
    tool calls, content blocks, etc. Target size: ~8MB of JSON text.
    """
    # Build a large transcript (many turns)
    turns = []
    for i in range(4000):  # 4000 turns × ~2KB each ≈ 8MB
        turn = {
            "role": "assistant",
            "content": [
                {
                    "type": "text",
                    "text": f"Turn {i}: " + ("x" * 500),  # 500 chars of padding
                },
                {
                    "type": "tool_use",
                    "id": f"tool_{i}",
                    "name": "Read",
                    "input": {"file_path": f"/path/to/file_{i}.py", "limit": 1000},
                },
                {
                    "type": "tool_result",
                    "tool_use_id": f"tool_{i}",
                    "content": "x" * 1000,  # 1KB result
                },
            ],
            "stop_reason": "tool_use",
        }
        turns.append(turn)

    state = {
        "id": "session_spike_test",
        "title": "8MB Decode Spike Test",
        "transcript": turns,
        "settings": {
            "model": "claude-sonnet-4",
            "effort": "medium",
            "fast": False,
            "goal": "Test 8MB decode performance",
        },
        "metadata": {"created_ms": 1234567890000, "dir": "/tmp/spike"},
    }

    payload_json = json.dumps(state)
    size_mb = len(payload_json) / (1024 * 1024)
    print(f"Generated payload: {size_mb:.2f} MB")

    return payload_json


def run_spike() -> tuple[float, float]:
    """
    Run the spike test. Returns (decode_ms, max_gui_gap_ms).

    Lifecycle:
    1. Generate 8MB payload
    2. Start GUI-thread monitor (60fps timer)
    3. Spawn worker thread, decode on worker
    4. Signal crosses to GUI thread (queued)
    5. Measure max inter-tick gap during decode+handoff
    6. Return measurements
    """
    app = QCoreApplication.instance()
    if app is None:
        app = QCoreApplication(sys.argv)

    payload_json = generate_8mb_payload()

    # Monitor GUI thread
    monitor = GuiThreadMonitor()
    monitor.start()

    # Worker thread
    thread = QThread()
    worker = SpikeWorker(payload_json)
    worker.moveToThread(thread)

    # Result tracking
    result_box = {}

    @Slot(object)
    def on_decoded(wrapper: PayloadWrapper):
        """Callback on GUI thread after decode completes."""
        # Access wrapper.data to simulate real usage
        _ = len(wrapper.data.get("transcript", []))
        # Stop monitoring after a brief delay (let signal handoff settle)
        QTimer.singleShot(50, lambda: monitor.stop())

    @Slot(float)
    def on_monitor_finished(max_gap_ms: float):
        """Monitoring finished — record results and quit."""
        result_box["decode_ms"] = worker.decode_ms
        result_box["max_gap_ms"] = max_gap_ms
        app.quit()

    worker.decoded.connect(on_decoded, Qt.QueuedConnection)
    monitor.finished.connect(on_monitor_finished)

    thread.started.connect(worker.decode)
    thread.start()

    # Run event loop until test completes
    app.exec()

    thread.quit()
    thread.wait()

    return result_box["decode_ms"], result_box["max_gap_ms"]


def main():
    print("=" * 60)
    print("8MB JSON Decode Spike Test")
    print("=" * 60)

    decode_ms, max_gap_ms = run_spike()

    print("\nResults:")
    print(f"  Worker-thread decode time: {decode_ms:.2f} ms")
    print(f"  GUI-thread max gap:        {max_gap_ms:.2f} ms")

    # Target: max_gap < 32ms (60fps budget × 2)
    TARGET_MS = 32.0

    if max_gap_ms <= TARGET_MS:
        print(f"\n✓ PASS: GUI-thread stall ({max_gap_ms:.2f} ms) < {TARGET_MS} ms target")
        sys.exit(0)
    else:
        print(f"\n✗ FAIL: GUI-thread stall ({max_gap_ms:.2f} ms) > {TARGET_MS} ms target")
        print("  Architecture risk: dict handoff via queued signal may be too slow.")
        print("  Mitigation: pass opaque wrapper object instead of dict.")
        sys.exit(1)


if __name__ == "__main__":
    main()
