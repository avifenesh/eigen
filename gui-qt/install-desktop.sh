#!/usr/bin/env bash
# Install the user-local Eigen Qt launcher and desktop entries.
#
# The desktop entry and launcher both point at this checkout so the icon opens
# the Qt app the developer is actually working on. Tests can override the
# install roots with EIGEN_QT_BIN_DIR and EIGEN_QT_APPLICATIONS_DIR.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="${EIGEN_QT_REPO:-$(cd "$SCRIPT_DIR/.." && pwd)}"
BIN_DIR="${EIGEN_QT_BIN_DIR:-$HOME/.local/bin}"
APPLICATIONS_DIR="${EIGEN_QT_APPLICATIONS_DIR:-$HOME/.local/share/applications}"
LAUNCHER="$BIN_DIR/eigen-qt"

mkdir -p "$BIN_DIR" "$APPLICATIONS_DIR"

cat >"$LAUNCHER" <<EOF
#!/usr/bin/env bash
# Launch the Eigen Qt GUI. Builds a fresh guiserver binary from the current
# checkout when the source is newer than the cached build, bootstraps the Qt
# venv, then launches the Qt app.
set -euo pipefail

REPO="$REPO_DIR"
BIN="\$REPO/bin/eigen"
GUI_QT="\$REPO/gui-qt"

cd "\$REPO"

needs_build=0
if [[ ! -x "\$BIN" ]]; then
  needs_build=1
elif [[ -n "\$(find . \( -name '.git' -o -name '.venv' -o -name 'node_modules' -o -name 'bin' \) -prune -o -name '*.go' -newer "\$BIN" -print 2>/dev/null | head -1)" ]]; then
  needs_build=1
fi

if [[ "\$needs_build" -eq 1 ]]; then
  echo "eigen-qt: building core binary via 'make core'..." >&2
  make core >&2 || { echo "eigen-qt: build failed" >&2; exit 1; }
  [[ -x "\$BIN" ]] || { echo "eigen-qt: build failed" >&2; exit 1; }
fi

exec "\$GUI_QT/run.sh" "\$@"
EOF
chmod 0755 "$LAUNCHER"

write_desktop_entry() {
  local file="$1"
  local name="$2"
  local generic_name="$3"
  cat >"$APPLICATIONS_DIR/$file" <<EOF
[Desktop Entry]
Type=Application
Name=$name
GenericName=$generic_name
Comment=Eigen Qt desktop GUI - local-first AI coding agent
Exec="$LAUNCHER"
Icon=eigen
Terminal=false
Categories=Development;
Keywords=eigen;ai;agent;coding;llm;qt;
StartupNotify=true
StartupWMClass=main
EOF
}

write_desktop_entry "eigen-qt.desktop" "Eigen (Qt)" "AI Coding Agent (Qt)"
write_desktop_entry "eigen-gui.desktop" "Eigen" "AI Coding Agent (Qt)"

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$APPLICATIONS_DIR" >/dev/null 2>&1 || true
fi

echo "Installed Eigen Qt launcher: $LAUNCHER"
echo "Installed desktop entries: $APPLICATIONS_DIR/eigen-qt.desktop, $APPLICATIONS_DIR/eigen-gui.desktop"
