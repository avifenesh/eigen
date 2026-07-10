#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export EIGEN_GUI_QT="$ROOT"
SHUCK="${SHUCK:-$HOME/.local/bin/shuck}"
[[ -x "$SHUCK" ]] || SHUCK="$(command -v shuck || true)"
[[ -n "$SHUCK" && -x "$SHUCK" ]] || { echo "shuck not found" >&2; exit 127; }
exec "$SHUCK" "$ROOT/install-desktop.shk" "$@"
