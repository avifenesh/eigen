#!/usr/bin/env python3
"""
test_reply_watch_simple.py — Simple automated test with 2s polling.

Creates a scratch session, opens a different session, sends input, waits for
the 2s poll to detect the status change.
"""
import sys
import time
import logging

from PySide6.QtCore import QCoreApplication, QTimer
from eigenqt.rpc import RpcClient
from eigenqt.models import SessionsModel, ReplyWatcher

logging.basicConfig(
    level=logging.INFO, format="%(asctime)s [%(levelname)s] %(name)s: %(message)s"
)
logger = logging.getLogger(__name__)


class SimpleTest:
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
        self.scratch_id = None
        self.dummy_id = None
        self.success = False

        # Connect client signals
        self.client.connected.connect(self._on_connected)

        # Set 20s timeout
        QTimer.singleShot(20000, self._timeout)

    def _on_connected(self):
        logger.info("RPC connected, creating sessions...")

        # Create scratch session
        self.client.call(
            "NewSession",
            "/tmp/qt-test-scratch",
            "",
            "",
            callback=self._on_scratch_created,
        )

    def _on_scratch_created(self, result):
        if "error" in result:
            logger.error(f"Failed to create scratch: {result['error']}")
            self._exit(1)
            return

        self.scratch_id = result["result"]
        logger.info(f"Created scratch session: {self.scratch_id[:8]}")

        # Create dummy session
        self.client.call(
            "NewSession",
            "/tmp/qt-test-dummy",
            "",
            "",
            callback=self._on_dummy_created,
        )

    def _on_dummy_created(self, result):
        if "error" in result:
            logger.error(f"Failed to create dummy: {result['error']}")
            self._exit(1)
            return

        self.dummy_id = result["result"]
        logger.info(f"Created dummy session: {self.dummy_id[:8]}")

        # Set dummy as current
        self.reply_watcher.set_current_session(self.dummy_id)
        logger.info(f"Set current session to dummy: {self.dummy_id[:8]}")

        # Send input to scratch
        logger.info("Sending input to scratch session...")
        self.client.call(
            "SendInput",
            self.scratch_id,
            '"Reply with exactly: ok"',
            [],
            [],
            callback=self._on_input_sent,
        )

    def _on_input_sent(self, result):
        if "error" in result:
            logger.error(f"SendInput failed: {result['error']}")
            self._exit(1)
            return

        logger.info("Input sent successfully")
        logger.info(
            "Waiting for ReplyWatcher to detect status change (2s poll)..."
        )
        logger.info("This may take up to 4s...")

    def _on_unread(self, session_id: str):
        """Handle unread signal."""
        logger.info("=" * 60)
        logger.info("✓✓✓ SUCCESS ✓✓✓")
        logger.info("=" * 60)
        logger.info(f"Unread notification for session: {session_id[:8]}")
        logger.info(f"Unread marker set: {self.reply_watcher.is_unread(session_id)}")
        logger.info("NotifyChatReply RPC called (desktop notification)")
        logger.info("=" * 60)
        self.success = True

        # Cleanup and exit
        QTimer.singleShot(500, lambda: self._exit(0))

    def _timeout(self):
        logger.error("=" * 60)
        logger.error("✗✗✗ TIMEOUT ✗✗✗")
        logger.error("=" * 60)
        logger.error("Test did not complete within 20s")
        self._exit(1)

    def _exit(self, code: int):
        logger.info("Cleaning up...")

        if self.scratch_id:
            self.client.call("RemoveSession", self.scratch_id)
            logger.info(f"Removed scratch {self.scratch_id[:8]}")

        if self.dummy_id:
            self.client.call("RemoveSession", self.dummy_id)
            logger.info(f"Removed dummy {self.dummy_id[:8]}")

        QTimer.singleShot(500, lambda: sys.exit(code))

    def run(self):
        return self.app.exec()


def main():
    logger.info("Starting simple ReplyWatcher test...")
    test = SimpleTest()
    sys.exit(test.run())


if __name__ == "__main__":
    main()
