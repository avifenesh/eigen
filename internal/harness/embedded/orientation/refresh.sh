#!/usr/bin/env bash
# action-graph refresh — full ingest → condense → graph, for all allowlisted
# projects. This is the manual/backfill path. Hooks use hook.js for per-session
# cursor ingest, then rebuild only the affected project.
N="${NODE:-$(command -v node)}"
A="${ORIENTATION_ENGINE_DIR:-$HOME/.claude/action-graph}"
"$N" --max-old-space-size=4096 "$A/ingest.js" >/dev/null 2>&1 \
  && "$N" "$A/condense.js" >/dev/null 2>&1 \
  && "$N" "$A/graph.js" >/dev/null 2>&1
exit 0
