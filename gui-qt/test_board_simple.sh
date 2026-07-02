#!/usr/bin/env bash
# Simple board model test + offscreen launch check
set -e

echo "=== Running board model tests ==="
.venv/bin/python3 test_board.py 2>&1 | grep -v "QThread:"

echo ""
echo "=== Launching GUI offscreen (12s timeout) ==="
/usr/bin/timeout 12s env QT_QPA_PLATFORM=offscreen .venv/bin/python3 main.py 2>&1 | grep -v "Warning:\|Could not\|libEGL\|Failed to create\|QStandardPaths\|qt.qpa\|QThread:" | head -20 || true

echo ""
echo "✓ Board view tests complete"
