"""Pytest bootstrap for Qt tests."""

import os


# PySide aborts the process when QGuiApplication starts without a usable display.
# Keep focused pytest commands safe in headless shells while preserving explicit
# platform choices from callers and live GUI checks.
os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")
