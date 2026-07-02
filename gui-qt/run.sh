#!/usr/bin/env bash
# Launch the Eigen Qt GUI. Bootstraps venv + dependencies on first run, then
# exec's the Qt app. The launcher ensures the venv is fresh and dependencies
# are installed before every launch.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV_DIR="$SCRIPT_DIR/.venv"
REQUIREMENTS="$SCRIPT_DIR/requirements.txt"

# Create venv if missing
if [[ ! -d "$VENV_DIR" ]]; then
  echo "gui-qt: creating venv at $VENV_DIR…" >&2
  python3 -m venv "$VENV_DIR"
fi

# Install/upgrade dependencies if requirements.txt is newer than the venv's
# last install timestamp (or if the timestamp file is missing).
INSTALL_MARKER="$VENV_DIR/.requirements-installed"
if [[ ! -f "$INSTALL_MARKER" ]] || [[ "$REQUIREMENTS" -nt "$INSTALL_MARKER" ]]; then
  echo "gui-qt: installing dependencies from requirements.txt…" >&2
  "$VENV_DIR/bin/pip" install -q --upgrade pip
  "$VENV_DIR/bin/pip" install -q -r "$REQUIREMENTS"
  touch "$INSTALL_MARKER"
fi

# Exec the Qt app
exec "$VENV_DIR/bin/python3" "$SCRIPT_DIR/main.py" "$@"
