#!/usr/bin/env python3
"""
verify_models.py — Headless verification of models layer against live guiserver.

Connects to guiserver, seeds TranscriptModel from a real session, streams live events,
prints row count/last row.

Usage:
    python verify_models.py [session_id]

If no session_id provided, creates a new session.
"""

import sys
import time
from pathlib import Path

from PySide6.QtCore import QCoreApplication, QTimer

# Add gui-qt to path
sys.path.insert(0, str(Path(__file__).parent))

from eigenqt.models import TranscriptModel
from eigenqt.rpc import RpcClient


class ModelVerifier:
    """Headless model verifier."""

    def __init__(self, session_id: str | None = None):
        self.app = QCoreApplication(sys.argv)
        self.client = RpcClient()
        self.session_id = session_id
        self.model: TranscriptModel | None = None

        # Connect signals
        self.client.connected.connect(self.on_connected)
        self.client.disconnected.connect(self.on_disconnected)

    def on_connected(self):
        """Connected → fetch/create session, seed model."""
        print("✓ Connected to guiserver")

        if self.session_id:
            # Use existing session
            print(f"Using session: {self.session_id}")
            self.load_session()
        else:
            # Create new session
            print("Creating new session...")
            self.client.call("NewSession", "", "", "", callback=self.on_new_session)

    def on_new_session(self, result: dict):
        """Handle NewSession result."""
        if "error" in result:
            print(f"✗ NewSession error: {result['error']}")
            self.app.quit()
            return

        self.session_id = result["result"]
        print(f"✓ Created session: {self.session_id}")
        self.load_session()

    def load_session(self):
        """Load session state, seed model."""
        self.client.call("State", self.session_id, callback=self.on_state)

    def on_state(self, result: dict):
        """Handle State result."""
        if "error" in result:
            print(f"✗ State error: {result['error']}")
            self.app.quit()
            return

        state = result["result"]
        print(f"✓ State loaded: {len(state.get('messages', []))} messages")

        # Create model
        self.model = TranscriptModel(self.client, self.session_id)
        self.model.seed(state)

        # Print initial state
        print(f"  Row count: {self.model.rowCount()}")
        if self.model.rowCount() > 0:
            last_row = self.model._rows[-1].to_dict()
            print(f"  Last row: kind={last_row['kind']}, text={last_row['text'][:50]}...")

        # Monitor for 5 seconds (stream live events)
        print("\nMonitoring live events for 5 seconds...")
        QTimer.singleShot(5000, self.print_final_state)

    def print_final_state(self):
        """Print final model state after monitoring."""
        if self.model:
            print(f"\n✓ Final row count: {self.model.rowCount()}")
            if self.model.rowCount() > 0:
                last_row = self.model._rows[-1].to_dict()
                print(f"  Last row: kind={last_row['kind']}, text={last_row['text'][:100]}...")
                print(f"  Streaming: {last_row['streaming']}")

        print("\n✓ Verification complete")
        self.app.quit()

    def on_disconnected(self, reason: str):
        """Handle disconnect."""
        print(f"✗ Disconnected: {reason}")
        self.app.quit()

    def run(self):
        """Run event loop."""
        return self.app.exec()


def main():
    session_id = sys.argv[1] if len(sys.argv) > 1 else None
    verifier = ModelVerifier(session_id)
    sys.exit(verifier.run())


if __name__ == "__main__":
    main()
