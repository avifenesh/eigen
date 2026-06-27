# Stability pass — verified in tree

Cross-check after GUI/daemon durability work. Ledger `status: done` in `ledger.json`; this doc records fix locations and smoke/tests.

## Turn lifecycle (GUI)

| ID | Fix | Smoke |
|----|-----|-------|
| GUI-091 | transcript.svelte.ts note/error/interrupt clear running | Error turn → idle |
| GUI-059 | start() liveKinds only (not bg_done) | Bg done, chat idle |
| GUI-060 | seed() resets live coalescer | Reconnect, no dup text |
| GUI-092/093 | done + streamedThisTurn | Non-stream final text |
| GUI-061 | Chat effectiveTokens | Ring during stream |
| GUI-062 | mapMessages + live tool_result | transcript.mapMessages.test.ts |
| GUI-005 | approval ap.args | See args at gate |
| GUI-068 | interrupt bool + interrupting UI | Stop idle toast |

## Durability (APP)

| ID | Fix | Test |
|----|-----|------|
| APP-071 | Save refuses empty over non-empty | transcript_test |
| APP-024 | Load recoverFromBackup | TestLoadRecoverFromBackup |
| APP-073 | SaveMeta atomic | meta_test |
| APP-019 | session.save atomic | session_test |
| APP-018 | session.SharedOpen | GUI export + Wails |
| APP-022 | atomicWrite mode | fsutil_test |
| APP-009 | compact_summary assistant | compact_summary_test |
| APP-037 | Interrupt bool | daemon + GUI |

## MCP APP-013

- maxRPCLineBytes 64 MiB; readLoop marks dead + readErr; lazyClient.get reconnects
- http readSSEResponse: ErrTooLong same error text
- go test ./internal/mcp/

## Streaming (code present; smoke live)

APP-008 converse/anthropic Stream; APP-044 custom Stream; fallback Stream.

## Still open

APP-004 bg count UI smoke; GUI-094 WireEvent parity; Tier 20 remote GUI-080/081.
