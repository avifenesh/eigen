# QA REPORT - Qt GUI Full Application Verification
**Date**: 2026-07-03  
**Branch**: `feat/qt-full-replacement`  
**QA Run**: Phase 2 (post-blocker fixes)  
**Previous blockers addressed**: invented call_parallel API, ConfigView delegate crashes, ~30 undefined-binding warnings

---

## EXECUTIVE SUMMARY

**VERDICT: FAIL — 2 BLOCKERS, 1 MAJOR**

The application has THREE critical issues preventing production readiness:

1. **BLOCKER**: All System section views (Connectors, Config, Reviewers) render **black screens** — window exists but shows nothing. Once triggered, navigation to other views also renders black. App becomes permanently unusable.
2. **BLOCKER**: Memory view fails to load with guiserver RPC error: `"arg 0: json: cannot unmarshal array into Go value of type string"`.
3. **MAJOR**: First Connectors click in session #1 resulted in immediate app termination (SIGTERM), though this was not reproducible in session #2 (showed black screen instead).

**Working views**: Home, Chat, Sessions, Live, Board, Tasks, Skills, Notes (9/12 views functional).

---

## SECTION 1: STATIC TESTS

| Test | Result | Details |
|------|--------|---------|
| **pytest** | ✅ PASS | 78/78 tests passing in 0.20s |
| **Offscreen launch** | ✅ PASS | Clean stderr (only inotify noise); no undefined bindings, no crashes |

---

## SECTION 2: LAUNCH IN WORKSPACE

| Metric | Result |
|--------|--------|
| **Launch time** | < 1s to window visible |
| **Initial stderr (8s)** | ✅ CLEAN (0 bytes) |
| **Window state** | ✅ Rendered correctly |

---

## SECTION 3: VIEW-BY-VIEW VERIFICATION

### 3.1 Home View
- **Screenshot**: `screenshots/qa2-initial.png`
- **Status**: ✅ PASS
- **Real data rendered**: 71 sessions stat, 0 running, 0 tasks, 19% cache hit, GPU stats (RTX 5090), CPU/Memory/Disk meters, recent session list with titles/models/turn counts
- **Empty states**: Today calendar + Inbox (correct)
- **Visual quality**: Excellent; all widgets rendering

### 3.2 Chat View
- **Screenshot**: `screenshots/qa2-chat.png`
- **Status**: ✅ PASS
- **Real data**: "gated" breadcrumb, message input field
- **Empty state**: Correct (mostly empty for gated session)
- **Visual quality**: Clean

### 3.3 Sessions View
- **Screenshot**: `screenshots/qa2-sessions.png`
- **Status**: ✅ PASS
- **Real data**: Full session list with titles, paths, model names (us.anthropic.claude-sonnet-5, openai.gpt-5.5), turn counts (0-136 turns)
- **Visual quality**: Excellent; all cards rendering

### 3.4 Live View
- **Screenshot**: `screenshots/qa2-live.png`
- **Status**: ✅ PASS
- **Real data**: 0 working, 0 approval, 71 idle, 0 error
- **Empty state**: "Nothing running — No sessions are working or awaiting approval right now." (correct)
- **Visual quality**: Clean

### 3.5 Board View
- **Screenshot**: `screenshots/qa2-board.png`
- **Status**: ✅ PASS
- **Real data**: Header, description, buttons (Projects/Kanban/Refresh)
- **Empty state**: Correct (no cards displayed)
- **Visual quality**: Clean

### 3.6 Tasks View
- **Screenshot**: `screenshots/qa2-tasks.png`
- **Status**: ✅ PASS
- **Real data**: 0 running, 10 done, 4 errored; task cards with full details (IDs, researcher/reviewer roles, models, timestamps, descriptions)
- **Visual quality**: Excellent; complex cards rendering correctly

### 3.7 Skills View
- **Screenshot**: `screenshots/qa2-skills.png`
- **Status**: ✅ PASS
- **Real data**: Filter input (0 count), Path/GitHub tabs, Add button
- **Empty state**: "No skills yet — add one above." (correct)
- **Visual quality**: Clean

### 3.8 Memory View
- **Screenshot**: `screenshots/qa2-memory.png`
- **Status**: ❌ **BLOCKER**
- **Issue**: Error displayed: "Couldn't load project memory" with detailed error `"arg 0: json: cannot unmarshal array into Go value of type string"`
- **Visual quality**: Error UI renders correctly; scope dropdown shows "eigen"; Add note button present
- **Root cause**: Guiserver RPC response type mismatch (Python client expects string, Go server sends array)

### 3.9 Notes View
- **Screenshot**: `screenshots/qa2-notes.png`
- **Status**: ✅ PASS
- **Real data**: Extensive list of notes from agent-knowledge (role-contract, xtest-input-integer-arg-parsing, 2026-06-19-* learning candidates)
- **Visual quality**: Excellent; full list with titles and paths

### 3.10 Connectors View
- **Screenshot**: `screenshots/qa2-connectors.png`
- **Status**: ❌ **BLOCKER**
- **Issue**: **BLACK SCREEN** — window renders 259-byte empty PNG (pure black)
- **Reproducibility**: 100% on both test sessions
- **Side effect**: Once triggered, ALL subsequent view navigation (including back to Home) shows black screen — app permanently broken
- **stderr**: Clean (no Python errors logged)
- **Note**: First session click caused immediate SIGTERM crash; second session survived but showed black screen

### 3.11 Config View
- **Screenshot**: `screenshots/qa2-config.png`
- **Status**: ❌ **BLOCKER**
- **Issue**: **BLACK SCREEN** (259-byte empty PNG)
- **Same behavior as Connectors**

### 3.12 Reviewers View
- **Screenshot**: `screenshots/qa2-reviewers.png`
- **Status**: ❌ **BLOCKER**
- **Issue**: **BLACK SCREEN** (259-byte empty PNG)
- **Same behavior as Connectors/Config**

---

## SECTION 4: DEEP FLOWS
**Status**: ❌ NOT TESTED — blocked by System views black screen

Planned tests:
- Markdown+code message → streaming + highlight + copy
- Tool card expand
- "/" slash popup
- Model switch
- Rename
- Dock diff+files
- Interrupt
- Gated approval → Allow from Qt
- Remove with inline confirm

**Cannot proceed** without fixing Connectors/Config/Reviewers rendering.

---

## SECTION 5: STRESS TESTS
**Status**: ❌ NOT TESTED — blocked by black screen blockers

---

## SECTION 6: LIFECYCLE TESTS
**Status**: ❌ NOT TESTED — blocked by black screen blockers

---

## FINDINGS SUMMARY

### BLOCKERS (2)

1. **System Views Black Screen**
   - **Severity**: BLOCKER
   - **Views affected**: Connectors, Config, Reviewers (all 3 System section views)
   - **Failure scenario**: Click any System view → black screen (259-byte PNG). Once triggered, navigation to ANY view (including Home) also shows black screen. App permanently broken until restart.
   - **Screenshots**: `screenshots/qa2-connectors.png`, `screenshots/qa2-config.png`, `screenshots/qa2-reviewers.png`, `screenshots/qa2-home-recovery.png`
   - **Reproducibility**: 100%
   - **Root cause hypothesis**: QML component loading failure for System views; possibly missing delegate implementation, broken import, or undefined property binding that doesn't log to stderr

2. **Memory View RPC Type Mismatch**
   - **Severity**: BLOCKER
   - **Error**: `"arg 0: json: cannot unmarshal array into Go value of type string"`
   - **Failure scenario**: Open Memory view → guiserver mem_list RPC returns array where Python client expects string
   - **Screenshot**: `screenshots/qa2-memory.png`
   - **Reproducibility**: 100%
   - **Root cause**: Guiserver memory/MemoryStore.List() RPC response type changed (possibly returns `[]MemNote` instead of single note); Python gui-qt client not updated

### MAJOR (1)

3. **Connectors Click Crash (Session 1)**
   - **Severity**: MAJOR (not reproducible in session 2)
   - **Failure scenario**: First Connectors click → immediate SIGTERM (app-2258472 died at 1783026609)
   - **Reproducibility**: 1/2 (session 2 showed black screen instead)
   - **Root cause hypothesis**: Race condition or signal handling issue; second attempt may have failed differently due to timing

### MINOR (0)
None.

### COSMETIC (0)
None.

---

## SCREENSHOTS INDEX

| View | Path | Status |
|------|------|--------|
| Initial (Home) | `screenshots/qa2-initial.png` | ✅ Good |
| Chat | `screenshots/qa2-chat.png` | ✅ Good |
| Sessions | `screenshots/qa2-sessions.png` | ✅ Good |
| Live | `screenshots/qa2-live.png` | ✅ Good |
| Board | `screenshots/qa2-board.png` | ✅ Good |
| Tasks | `screenshots/qa2-tasks.png` | ✅ Good |
| Skills | `screenshots/qa2-skills.png` | ✅ Good |
| Memory | `screenshots/qa2-memory.png` | ❌ Error UI |
| Notes | `screenshots/qa2-notes.png` | ✅ Good |
| Connectors | `screenshots/qa2-connectors.png` | ❌ Black screen |
| Config | `screenshots/qa2-config.png` | ❌ Black screen |
| Reviewers | `screenshots/qa2-reviewers.png` | ❌ Black screen |
| Home (recovery) | `screenshots/qa2-home-recovery.png` | ❌ Black screen |

---

## RECOMMENDED FIXES (Priority Order)

1. **Fix System views black screen** (BLOCKER)
   - Check `ConnectorsView.qml`, `ConfigView.qml`, `ReviewersView.qml` for:
     - Missing Component.onCompleted handlers
     - Undefined property bindings (check console.log in QML)
     - Broken imports or delegate definitions
     - Typos in `property` declarations
   - Run offscreen with QML debugging: `QT_LOGGING_RULES="qt.qml.binding.removal.info=true" QT_QPA_PLATFORM=offscreen python3 main.py` and click Connectors to capture QML errors

2. **Fix Memory view RPC type mismatch** (BLOCKER)
   - Investigate guiserver `mem_list` RPC:
     - Check if recent changes to `internal/agent/background.go` or `MemoryStore.List()` changed return type
     - Verify Python `rpc_client.py` mem_list call expects string vs. array
     - Align types: either change Go to return single string, or update Python to handle array

3. **Investigate Connectors crash** (MAJOR, low repro)
   - Add signal handler logging to capture why SIGTERM is sent
   - Check if ConnectorsView.qml has any synchronous blocking RPC calls that could trigger watchdog

---

**VERDICT: FAIL — Black screens and RPC type mismatch block production use. Fix System views + Memory, then re-run full QA.**
