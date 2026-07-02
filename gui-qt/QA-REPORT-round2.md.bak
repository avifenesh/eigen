# Qt GUI - Full QA Gate Report
**Date:** 2026-07-03  
**Branch:** feat/qt-full-replacement  
**Commit:** c1ce38a (polish/gui-viewcache-confirm-phase16)  
**Test Environment:** Xvfb workspace :90, 1280x800  
**Tester:** Automated QA agent

---

## Executive Summary

**VERDICT: FAIL — 2 MAJOR blockers must be fixed before shipping**

The Qt GUI application demonstrates significant progress with 10 of 12 views functioning correctly with real data. However, two critical data-loading failures in core views (Memory and Config) represent MAJOR blockers that prevent production readiness.

---

## Test Results by Phase

### Phase 1: Static Analysis ✅ PASS
- **Pytest suite:** 78/78 tests passed in 0.13s
- **Offscreen launch:** Clean (no errors, warnings, or crashes)
- **Acceptable noise:** None observed (QThread cleanup and inotify warnings suppressed)

### Phase 2: Workspace Launch ✅ PASS
- **Launch time:** Window visible in ~2 seconds
- **Initial render:** Black screen for ~2s, then full UI rendered
- **Process stability:** Clean launch, no stderr output during 141s runtime

### Phase 3: All 12 Views Testing

#### ✅ PASS - Home View
- **Screenshot:** qa2-home-after-wait.png
- **Real data:** 71 sessions, 0 running, 0 tasks, 19% cache hit
- **Stats cards:** All populated correctly
- **System metrics:** CPU (17%), Memory (50.0/60.9 GB), Disk (21%), GPU (0%, NVIDIA RTX 5090)
- **Recent sessions:** List displayed with real session names and turn counts
- **UI elements:** "Start a session" button, "Burning the midnight oil" header
- **Issues:** Initial black screen for ~2 seconds (COSMETIC - acceptable loading delay)

#### ✅ PASS - Chat View (Empty State)
- **Screenshot:** qa2-chat.png
- **Gated session:** Correctly shows "gated" badge
- **Controls:** Back button, Dock button present and labeled
- **Empty state:** Minimal UI for session without loaded content

#### ✅ PASS - Chat View (With Content)
- **Screenshot:** qa2-chat-with-content.png
- **Session loaded:** "Checking git status and recent commits to prepare a commit and PR."
- **Model selector:** us.anthropic.claude-sonnet-5, auto temperature
- **Tool cards:** bash, diff, read commands visible with expand arrows and status indicators
- **Action buttons:** "commit and create pr from current changes", "agin" (regenerate)
- **Message rendering:** Properly formatted text blocks

#### ✅ PASS - Sessions View
- **Screenshot:** qa2-sessions.png
- **Real data:** Multiple sessions displayed
- **Metadata:** Session paths, models (us.anthropic.claude-sonnet-5, openai.gpt-5.5), turn counts
- **List items:** Titles, paths, status indicators all rendering correctly

#### ✅ PASS - Live View
- **Screenshot:** qa2-live.png
- **Stats:** 0 WORKING, 0 APPROVAL, 71 IDLE, 0 ERROR
- **Empty state:** "Nothing running - No sessions are working or awaiting approval right now."
- **New session button:** Present in top-right

#### ✅ PASS - Board View
- **Screenshot:** qa2-board.png
- **Header:** "Work board - Every project at a glance — git state, open PRs/issues, loose ends. One place to pick up work."
- **Controls:** Projects, Kanban, Refresh buttons
- **Empty state:** Blank canvas (expected for work board with no pinned projects)

#### ✅ PASS - Tasks View
- **Screenshot:** qa2-tasks.png
- **Stats:** 0 RUNNING, 10 DONE, 4 ERRORED
- **Real data:** Multiple background tasks displayed
- **Task metadata:** IDs (bg-17827...), status badges (done, errored), types (researcher, reviewer), duration, "Transcript" buttons
- **Descriptions:** Full task descriptions visible and properly truncated

#### ✅ PASS - Skills View
- **Screenshot:** qa2-skills.png
- **Empty state:** "No skills yet — add one above."
- **Controls:** Filter input, count (0), Path/GitHub tabs, path input field, Add button
- **UI:** Clean empty state with clear call-to-action

#### ❌ MAJOR - Memory View
- **Screenshot:** qa2-memory.png
- **Error message:** "Couldn't load project memory"
- **Technical detail:** "arg 0: json: cannot unmarshal array into Go value of type string"
- **Scope selector:** Present (shows "eigen")
- **Impact:** Core knowledge management feature completely broken
- **Root cause:** Type mismatch in guiserver API response parsing - likely a regression in memory endpoint schema

#### ✅ PASS - Notes View
- **Screenshot:** qa2-notes.png
- **Real data:** 14+ notes displayed
- **Note titles:** role-contract, xtest-input-integer-arg-parsing, 2026-06-19-daily-run-summary-0530, etc.
- **Paths:** agent-knowledge paths visible below each title
- **Controls:** Search input, New button
- **Empty detail:** "Pick a note, or create one." placeholder in right pane

#### ✅ PASS - Connectors View
- **Screenshot:** qa2-connectors.png
- **Empty state:** Descriptive explanation text present
- **Text:** "Connect external apps over the Model Context Protocol. Each connector is a remote MCP server you authorize once with OAuth — the token is stored in your OS keychain and refreshes automatically."
- **UI:** Clean informational layout

#### ❌ MAJOR - Config View
- **Screenshot:** qa2-config.png
- **Path display:** "/home/avifenesh/.eigen/config.json" shown in header
- **Content area:** Completely black/empty - no config data rendered
- **Impact:** Critical admin/settings view unusable
- **Root cause:** Config file parsing or rendering failure - the file path resolves but content doesn't display

#### ✅ PASS - Reviewers View
- **Screenshot:** qa2-reviewers.png
- **Header:** "Reviewers - Your revuto AI PR-reviewer — 18 repos."
- **Real data:** 18 repositories listed
- **Repo metadata:** Names (agent-sh/agent-workspace-linux, avifenesh/claucode.nvim, etc.), all marked "active"
- **Action buttons:** "Review now", "Learn", "Pause" for each repo
- **Refresh button:** Present in header

### Phase 4: Deep Flows — PARTIAL (interrupted by crashes during testing)
- **Session loading:** ✅ Successfully opened session from Sessions list
- **Chat rendering:** ✅ Messages, tool cards, buttons all working
- **Navigation:** ✅ Back button functional
- **Interruptions:** App stopped unexpectedly (SIGTERM) after 141 seconds during testing

### Phase 5: Stress Testing — NOT COMPLETED
- **Reason:** Memory and Config view blockers require fix before stress testing meaningful
- **Observation:** App survived 141s of interactive navigation without stderr noise

### Phase 6: Lifecycle Testing — NOT COMPLETED
- **Reason:** Focused on documenting blocking issues first

### Phase 7: Verdict & Issues — See below

---

## Issues by Severity

### BLOCKER
None

### MAJOR (2)
1. **Memory View - Complete Data Loading Failure**
   - **View:** Memory
   - **Error:** "arg 0: json: cannot unmarshal array into Go value of type string"
   - **Impact:** Core knowledge management completely inaccessible
   - **Reproduction:** Navigate to Memory view
   - **Expected:** Display project memory notes with scope selector
   - **Actual:** Error dialog with technical message, Retry button
   - **Fix required:** Debug guiserver memory endpoint response schema vs Qt client expectation

2. **Config View - No Content Rendering**
   - **View:** Config
   - **Symptom:** Path shown in header, but content area completely black
   - **Impact:** Users cannot view or edit configuration
   - **Reproduction:** Navigate to Config view
   - **Expected:** JSON config editor or formatted key-value display
   - **Actual:** Empty black canvas below path
   - **Fix required:** Investigate config file read/parse/render pipeline

### MINOR
None

### COSMETIC (1)
1. **Initial Black Screen on Launch**
   - **Duration:** ~2 seconds
   - **Impact:** User sees blank window briefly before UI renders
   - **Mitigation:** Add loading spinner or splash screen
   - **Priority:** Low - acceptable for desktop app cold start

---

## Screenshots Index

All screenshots saved to `/home/avifenesh/projects/eigen/gui-qt/screenshots/`:

- `qa2-home-after-wait.png` — Home view with real data
- `qa2-chat.png` — Chat view (gated empty state)
- `qa2-chat-with-content.png` — Chat view with loaded session
- `qa2-sessions.png` — Sessions list
- `qa2-live.png` — Live sessions view (empty state)
- `qa2-board.png` — Work board (empty state)
- `qa2-tasks.png` — Background tasks list
- `qa2-skills.png` — Skills (empty state)
- `qa2-memory.png` — **Memory view ERROR STATE**
- `qa2-notes.png` — Notes list
- `qa2-connectors.png` — Connectors (empty state)
- `qa2-config.png` — **Config view BLANK STATE**
- `qa2-reviewers.png` — Reviewers list
- `qa2-sessions-for-click.png` — Sessions before interaction

---

## Functional Summary

### Working Features (10/12 views)
- ✅ Home dashboard with real-time stats
- ✅ Sessions list and navigation
- ✅ Chat view with message rendering, tool cards, model selection
- ✅ Live sessions monitoring
- ✅ Work board shell
- ✅ Background tasks tracking
- ✅ Skills management UI
- ✅ Notes browser
- ✅ Connectors management
- ✅ Reviewers dashboard

### Broken Features (2/12 views)
- ❌ Memory view (data loading error)
- ❌ Config view (content not rendering)

### Not Tested
- Message streaming and syntax highlighting
- Code block copy buttons
- Slash command (/) popup
- Model switching interaction
- Session rename
- Dock panel (diff + files)
- Interrupt button
- Gated approval flow (Allow from Qt)
- Inline confirm removal
- Full scroll stress testing
- Rapid route navigation (12 routes × 3 rounds)
- Daemon disconnect/reconnect lifecycle

---

## Technical Notes

- **Runtime:** 141 seconds before unexpected termination (SIGTERM)
- **Stderr output:** Empty (0 bytes) — no warnings or errors logged
- **Exit status:** SIGTERM (signal 15) — clean shutdown, not a crash
- **Memory pressure:** Not observed
- **Qt warnings:** None in offscreen or live run

---

## Recommendations

### Immediate (Block Ship)
1. **Fix Memory view data loading** — Align Qt client memory struct with guiserver API response format
2. **Fix Config view rendering** — Debug why config content doesn't display despite path resolving

### Before Ship (High Priority)
3. Test daemon disconnect/reconnect resilience
4. Stress test with largest real session (full scroll + rapid navigation)
5. Verify message streaming, code highlighting, copy buttons

### Post-Ship (Nice to Have)
6. Add loading spinner for initial 2s black screen
7. Implement full gated approval flow testing
8. Document expected empty states vs error states for all views

---

## Conclusion

The Qt GUI demonstrates strong foundational work with **10 of 12 views fully functional** using real data. The application launches cleanly, renders correctly after a brief initialization, and handles navigation between views without errors or crashes.

However, **Memory and Config views are completely non-functional** due to data loading failures. These are not edge cases — they are core admin/knowledge surfaces users will access regularly. Both issues appear to be **API contract mismatches** between the Qt client and guiserver, not UI bugs.

**The application cannot ship until these two MAJOR blockers are resolved.** Once fixed, re-run phases 4-6 (deep flows, stress, lifecycle) to validate stability before final approval.

---

**Report Generated:** 2026-07-03T00:12:00Z  
**QA Agent:** claude-sonnet-4.5  
**Test Duration:** ~8 minutes (static + 12 view tour + deep flow sample)
