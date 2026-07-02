#!/usr/bin/env python3
"""
test_reply_watch_manual.py — Manual test of ReplyWatcher.

Creates two sessions, opens one, sends a message to the OTHER one (via CLI),
then observes:
1. Desktop notification appears
2. Unread marker set in sessions model

Usage:
    # Terminal 1: Run this script
    python test_reply_watch_manual.py

    # Terminal 2: Send input to the background session (use the ID printed by script)
    ./bin/eigen rpc SendInput s<ID> "Reply with exactly: ok" [] []

    # Then observe Terminal 1 for notification + unread marker
"""
import sys
import logging
from pathlib import Path

from PySide6.QtCore import QCoreApplication
from PySide6.QtWidgets import QApplication
from eigenqt.rpc import RpcClient
from eigenqt.models import SessionsModel, ReplyWatcher

logging.basicConfig(
    level=logging.INFO, format="%(asctime)s [%(levelname)s] %(name)s: %(message)s"
)
logger = logging.getLogger(__name__)


class ManualTest:
    def __init__(self):
        self.app = QCoreApplication(sys.argv)
        self.client = RpcClient()
        self.sessions_model = SessionsModel(self.client)
        self.reply_watcher = ReplyWatcher(
            self.client, self.sessions_model, self.app
        )

        # Wire unread signal
        self.reply_watcher.unread.connect(self._on_unread)

        # State
        self.bg_session_id = None
        self.fg_session_id = None

        # Connect client signals
        self.client.connected.connect(self._on_connected)

    def _on_connected(self):
        logger.info("RPC connected")
        logger.info("=" * 60)
        logger.info("MANUAL TEST SETUP")
        logger.info("=" * 60)

        # Create two sessions
        self.client.call(
            "NewSession",
            "/tmp/qt-test-bg",
            "",
            "",
            callback=self._on_bg_created,
        )

    def _on_bg_created(self, result):
        if "error" in result:
            logger.error(f"Failed to create bg session: {result['error']}")
            sys.exit(1)

        self.bg_session_id = result["result"]
        logger.info(f"✓ Created BACKGROUND session: {self.bg_session_id}")

        # Create foreground session
        self.client.call(
            "NewSession",
            "/tmp/qt-test-fg",
            "",
            "",
            callback=self._on_fg_created,
        )

    def _on_fg_created(self, result):
        if "error" in result:
            logger.error(f"Failed to create fg session: {result['error']}")
            sys.exit(1)

        self.fg_session_id = result["result"]
        logger.info(f"✓ Created FOREGROUND session: {self.fg_session_id}")

        # Set foreground as current
        self.reply_watcher.set_current_session(self.fg_session_id)
        logger.info(f"✓ Set current session to: {self.fg_session_id}")

        logger.info("")
        logger.info("=" * 60)
        logger.info("READY FOR MANUAL TEST")
        logger.info("=" * 60)
        logger.info("")
        logger.info("In another terminal, send input to the BACKGROUND session:")
        logger.info("")
        logger.info(
            f'  ./bin/eigen rpc SendInput {self.bg_session_id} "Reply with exactly: ok" [] []'
        )
        logger.info("")
        logger.info(
            "Then watch this terminal for desktop notification + unread marker."
        )
        logger.info("")
        logger.info("Press Ctrl+C to exit and cleanup.")
        logger.info("=" * 60)

        # Subscribe to session events for both sessions
        self.client.subscribe(
            [f"session:{self.bg_session_id}", f"session:{self.fg_session_id}"]
        )
        self.client.event.connect(self._on_event)

    def _on_event(self, channel: str, data: dict):
        """Handle session events."""
        if channel == f"session:{self.bg_session_id}":
            event = data.get("event", {})
            kind = event.get("kind")

            if kind == "done":
                logger.info(
                    f"⚡ Background session {self.bg_session_id} turn finished!"
                )
                logger.info("   Refreshing sessions model...")
                # Refresh sessions model so ReplyWatcher sees the status change
                self.sessions_model.refresh()

    def _on_unread(self, session_id: str):
        """Handle unread signal from ReplyWatcher."""
        logger.info("")
        logger.info("=" * 60)
        logger.info("✓✓✓ SUCCESS: UNREAD NOTIFICATION TRIGGERED ✓✓✓")
        logger.info("=" * 60)
        logger.info(f"   Session: {session_id}")
        logger.info(
            f"   Unread marker: {self.reply_watcher.is_unread(session_id)}"
        )
        logger.info("   Desktop notification should have appeared!")
        logger.info("=" * 60)
        logger.info("")

    def run(self):
        """Run the test."""
        try:
            return self.app.exec()
        except KeyboardInterrupt:
            logger.info("")
            logger.info("Interrupted, cleaning up...")
            self._cleanup()
            return 0

    def _cleanup(self):
        """Cleanup sessions."""
        if self.bg_session_id:
            self.client.call("RemoveSession", self.bg_session_id)
            logger.info(f"Removed bg session {self.bg_session_id}")

        if self.fg_session_id:
            self.client.call("RemoveSession", self.fg_session_id)
            logger.info(f"Removed fg session {self.fg_session_id}")


def main():
    test = ManualTest()
    sys.exit(test.run())


if __name__ == "__main__":
    main()
