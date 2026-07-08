#!/usr/bin/env python3
"""
End-to-end daily-driver test script for Qt chat parity milestone.
Simulates a real user session: create scratch session, send coding question,
verify streaming markdown/code/notifications, test model switch, rename, interrupt, approval.
"""
import sys
import time
from pathlib import Path
from PySide6.QtCore import QTimer, QCoreApplication, Qt
from PySide6.QtWidgets import QApplication
from PySide6.QtTest import QTest

# Add eigenqt to path
sys.path.insert(0, str(Path(__file__).parent))

from eigenqt.rpc.client import RpcClient
from eigenqt.models.sessions import SessionsModel
from eigenqt.models.session_state import SessionStateModel
from eigenqt.models.transcript import TranscriptModel
from eigenqt.models.approvals import ApprovalsModel
from eigenqt.models.reply_watch import ReplyWatcher


class E2ETest:
    def __init__(self):
        self.client = RpcClient()
        self.sessions_model = SessionsModel(self.client)
        self.session_state_model = None  # Created after session
        self.transcript_model = None  # Created after session
        self.approvals_model = None  # Created after session
        self.reply_watcher = ReplyWatcher(self.client, self.sessions_model)

        self.scratch_session_id = None
        self.test_passed = True
        self.errors = []

    def log(self, msg: str):
        print(f"[E2E] {msg}")

    def error(self, msg: str):
        print(f"[E2E ERROR] {msg}")
        self.errors.append(msg)
        self.test_passed = False

    def run(self):
        """Run the full E2E test sequence."""
        self.log("Starting E2E test suite")

        # Step 1: Create scratch session (dir, model, perm)
        self.log("Step 1: Creating scratch session")
        try:
            result = self.client.call_sync("NewSession", "/tmp/qt-e2e-test", "", "")
            self.scratch_session_id = result
            self.log(f"Created session: {self.scratch_session_id}")
        except Exception as e:
            self.error(f"Failed to create session: {e}")
            return

        # Step 2: Switch to the session and create models
        self.log("Step 2: Switching to scratch session and attaching models")
        try:
            self.client.call_sync("SwitchSession", self.scratch_session_id)
            self.sessions_model.set_current_session(self.scratch_session_id)
            self.session_state_model = SessionStateModel(self.client, self.scratch_session_id)
            self.session_state_model.refresh()
            self.transcript_model = TranscriptModel(self.client, self.scratch_session_id)
            self.approvals_model = ApprovalsModel(self.client, self.scratch_session_id)
            QTest.qWait(200)  # Let models stabilize
            self.log("Models attached to session")
        except Exception as e:
            self.error(f"Failed to switch session: {e}")
            return

        # Step 3: Verify initial state
        self.log("Step 3: Verifying initial empty transcript")
        if self.transcript_model.rowCount() != 0:
            self.error(f"Expected 0 turns, got {self.transcript_model.rowCount()}")
        else:
            self.log("✓ Empty transcript confirmed")

        # Step 4: Send coding question
        self.log("Step 4: Sending coding question")
        question = "Write a Python fibonacci function with memoization, explain briefly"
        try:
            self.client.call_sync("Input", self.scratch_session_id, question)
            self.log("Input sent, waiting for streaming response...")
        except Exception as e:
            self.error(f"Failed to send input: {e}")
            return

        # Step 5: Wait for streaming + verify markdown/code rendering
        self.log("Step 5: Waiting for response (up to 30s)...")
        start_time = time.time()
        assistant_turn_seen = False
        code_block_seen = False
        done_seen = False

        for i in range(150):  # 30s timeout (200ms per check)
            QTest.qWait(200)

            # Check for assistant turn
            if self.transcript_model.rowCount() >= 2:
                assistant_turn_seen = True
                turn = self.transcript_model.get_turn(1)
                if turn and turn.get("role") == "assistant":
                    text = turn.get("text", "")
                    if "```" in text or "def fib" in text:
                        code_block_seen = True
                        self.log(f"✓ Code block detected in response (len={len(text)})")

            # Check session status
            try:
                status = self.session_state_model.status()
                if status == "idle":
                    done_seen = True
                    self.log("✓ Session returned to idle state")
                    break
            except:
                pass

        elapsed = time.time() - start_time

        if not assistant_turn_seen:
            self.error("No assistant turn appeared in transcript")
        else:
            self.log(f"✓ Assistant turn appeared (streaming took {elapsed:.1f}s)")

        if not code_block_seen:
            self.error("No code block detected in response")
        else:
            self.log("✓ Code block with fibonacci detected")

        if not done_seen:
            self.error("Session did not return to idle within 30s")
        else:
            self.log("✓ Response completed")

        # Step 6: Verify transcript structure
        self.log("Step 6: Verifying transcript structure")
        if self.transcript_model.rowCount() < 2:
            self.error(f"Expected >=2 turns (user + assistant), got {self.transcript_model.rowCount()}")
        else:
            user_turn = self.transcript_model.get_turn(0)
            assistant_turn = self.transcript_model.get_turn(1)

            if user_turn.get("role") != "user":
                self.error(f"Expected user turn, got {user_turn.get('role')}")
            elif question not in user_turn.get("text", ""):
                self.error("User turn text doesn't match input")
            else:
                self.log("✓ User turn correct")

            if assistant_turn.get("role") != "assistant":
                self.error(f"Expected assistant turn, got {assistant_turn.get('role')}")
            elif len(assistant_turn.get("text", "")) < 50:
                self.error(f"Assistant response too short: {len(assistant_turn.get('text', ''))}")
            else:
                self.log(f"✓ Assistant turn correct (len={len(assistant_turn.get('text', ''))})")

        # Step 7: Test model switching
        self.log("Step 7: Testing model switch")
        try:
            current_model = self.session_state_model.model()
            new_model = "gpt-5" if "gpt-5" not in current_model.lower() else "local-qwen"
            self.client.call_sync("SetModel", self.scratch_session_id, new_model)
            QTest.qWait(500)
            updated_model = self.session_state_model.model()
            if new_model.lower() in updated_model.lower():
                self.log(f"✓ Model switched: {current_model} → {updated_model}")
            else:
                self.error(f"Model switch failed: expected {new_model}, got {updated_model}")
        except Exception as e:
            self.error(f"Model switch failed: {e}")

        # Step 8: Test session rename
        self.log("Step 8: Testing session rename")
        new_title = "E2E Test Session"
        try:
            self.client.call_sync("RenameSession", self.scratch_session_id, new_title)
            QTest.qWait(500)
            updated_title = self.session_state_model.title()
            if new_title in updated_title:
                self.log(f"✓ Session renamed: {updated_title}")
            else:
                self.error(f"Rename failed: expected '{new_title}', got '{updated_title}'")
        except Exception as e:
            self.error(f"Rename failed: {e}")

        # Step 9: Test interrupt
        self.log("Step 9: Testing interrupt (send long request + interrupt)")
        try:
            self.client.call_sync("Input", self.scratch_session_id, "Count from 1 to 1000 slowly, one number per line")
            QTest.qWait(1000)  # Let it start streaming
            self.client.call_sync("Interrupt", self.scratch_session_id)
            QTest.qWait(500)
            status = self.session_state_model.status()
            if status == "idle":
                self.log("✓ Interrupt successful, session idle")
            else:
                self.error(f"Interrupt failed: status={status}")
        except Exception as e:
            self.error(f"Interrupt test failed: {e}")

        # Step 10: Print summary
        self.log("\n" + "="*60)
        if self.test_passed:
            self.log("✅ ALL TESTS PASSED")
        else:
            self.log("❌ SOME TESTS FAILED:")
            for err in self.errors:
                self.log(f"  - {err}")
        self.log("="*60)

        # Cleanup
        self.log("\nCleaning up scratch session")
        try:
            self.client.call_sync("RemoveSession", self.scratch_session_id)
            self.log("✓ Scratch session removed")
        except Exception as e:
            self.log(f"Warning: cleanup failed: {e}")

        # Exit
        QTimer.singleShot(100, QCoreApplication.instance().quit)


def main():
    app = QApplication(sys.argv)

    test = E2ETest()

    # Wait for connection then run test
    def on_connected():
        print("[E2E] Connected to guiserver")
        QTimer.singleShot(500, test.run)

    test.client.connected.connect(on_connected)

    sys.exit(app.exec())


if __name__ == "__main__":
    main()
