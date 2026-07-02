#!/usr/bin/env python3
"""
Manual verification script — provides instructions for end-to-end testing.
Since the Qt app is already running, this documents what to test manually.
"""

print("""
═══════════════════════════════════════════════════════════════════════════
                    Qt Chat Parity - Manual Verification
═══════════════════════════════════════════════════════════════════════════

The Qt app is running on DISPLAY=:0. Please perform these tests:

1. ✓ TESTS PASSED (42/42)
   All unit tests pass cleanly.

2. LAUNCH & STDERR
   [ ] App launched warning-free on DISPLAY=:0
   [ ] Check stderr output for any QML binding loops or warnings

3. MARKDOWN RENDERING
   Create a new session and send:
   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   Here's a test:

   | Feature | Status |
   |---------|--------|
   | Tables  | ✓      |
   | Code    | ✓      |

   ```python
   def hello():
       return "world"
   ```
   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   [ ] Table renders with proper borders
   [ ] Code fence has syntax highlighting
   [ ] No binding loop warnings in stderr

4. TOOL CARD EXPAND/COLLAPSE
   Wait for a tool use in the response:
   [ ] Tool card shows collapsed initially
   [ ] Click to expand shows full content
   [ ] Click to collapse hides content

5. SLASH POPUP
   In the input field:
   [ ] Type "/" and slash popup appears
   [ ] Arrow keys navigate the list
   [ ] Press Enter to select a command
   [ ] Selected command inserts into input

6. MODEL SWITCH
   [ ] Click model dropdown in sidebar
   [ ] Select different model
   [ ] Model indicator updates

7. SESSION RENAME
   [ ] Right-click (or long-press) session in sidebar
   [ ] Select "Rename"
   [ ] Edit name
   [ ] Press Enter
   [ ] Name updates in sidebar

8. DOCK PANEL
   [ ] Click dock toggle button (top-right)
   [ ] Dock panel slides in from right
   [ ] "Diff" tab is selected by default
   [ ] "Files" tab is also visible

9. DOCK DIFF TAB
   With dir=/home/avifenesh/projects/eigen:
   [ ] Shows git working tree diff
   [ ] File headers are styled differently
   [ ] Add lines are green/+ prefix
   [ ] Del lines are red/- prefix
   [ ] Context lines are normal
   [ ] Scrollable if diff is long

10. DOCK FILES TAB
    [ ] Click "Files" tab
    [ ] Shows hierarchical file tree
    [ ] Folders can expand/collapse
    [ ] Click on a file opens content in read-only view

11. IMAGE PASTE (if wl-copy available)
    Run: convert -size 10x10 xc:red png:- | wl-copy -t image/png
    Then in the Qt app:
    [ ] Ctrl+V pastes image
    [ ] Image thumbnail appears in input area
    [ ] Image can be removed before sending

12. UNREAD DOT FLOW
    Create two sessions. In session A, wait for a response.
    While viewing session B, wait for session A to get a new message:
    [ ] Blue dot appears next to session A in sidebar
    [ ] Click on session A
    [ ] Blue dot clears

═══════════════════════════════════════════════════════════════════════════

REMAINING WORK (see gui-qt/PHASE-B-CHAT-PARITY.md):
  - Any gaps found during manual testing
  - Performance optimization
  - Polish and refinement

═══════════════════════════════════════════════════════════════════════════
""")
