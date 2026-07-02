#!/usr/bin/env python3
"""
Verification script for Live view implementation.

Checks:
1. LiveSessionsModel imports correctly
2. Filter/sort logic works (pytest)
3. QML syntax is valid
4. Model can be instantiated
"""
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def check_imports():
    """Verify all imports work."""
    print("✓ Checking imports...")
    try:
        from eigenqt.models import LiveSessionsModel
        from eigenqt.models.live import filter_and_sort_live
        print("  ✓ LiveSessionsModel import OK")
        print("  ✓ filter_and_sort_live import OK")
        return True
    except Exception as e:
        print(f"  ✗ Import failed: {e}")
        return False


def check_filter_sort():
    """Verify filter/sort logic."""
    print("✓ Checking filter/sort logic...")
    from eigenqt.models.live import filter_and_sort_live

    sessions = [
        {"id": "approval1", "status": "approval", "updated": 5000},
        {"id": "working1", "status": "working", "updated": 1000},
        {"id": "idle1", "status": "idle", "updated": 3000},
        {"id": "working2", "status": "working", "updated": 2000},
    ]
    result = filter_and_sort_live(sessions)

    # Should have 3 live sessions (2 working, 1 approval)
    if len(result) != 3:
        print(f"  ✗ Expected 3 live sessions, got {len(result)}")
        return False

    # working2 (2000) should be first (newest working)
    if result[0]["id"] != "working2":
        print(f"  ✗ Expected working2 first, got {result[0]['id']}")
        return False

    # approval1 should be last (only approval)
    if result[-1]["id"] != "approval1":
        print(f"  ✗ Expected approval1 last, got {result[-1]['id']}")
        return False

    print("  ✓ Filter/sort logic correct")
    return True


def check_qml_syntax():
    """Verify LiveView.qml syntax."""
    print("✓ Checking QML syntax...")
    try:
        from PySide6.QtQml import QQmlEngine, QQmlComponent
        from PySide6.QtGui import QGuiApplication

        app = QGuiApplication(sys.argv)
        engine = QQmlEngine()

        qml_path = ROOT / "eigenqt" / "qml" / "LiveView.qml"
        component = QQmlComponent(engine, str(qml_path))

        if component.isError():
            print("  ✗ QML Errors:")
            for error in component.errors():
                print(f"    {error.toString()}")
            return False

        print("  ✓ LiveView.qml syntax valid")
        return True
    except Exception as e:
        print(f"  ✗ QML check failed: {e}")
        return False


def check_model_instantiation():
    """Verify model can be instantiated."""
    print("✓ Checking model instantiation...")
    try:
        from eigenqt.rpc import RpcClient
        from eigenqt.models import LiveSessionsModel

        # Create a client (won't connect, but that's OK for this test)
        client = RpcClient(sock_path=Path("/tmp/nonexistent.sock"))
        model = LiveSessionsModel(client)

        print("  ✓ LiveSessionsModel instantiated")
        return True
    except Exception as e:
        print(f"  ✗ Instantiation failed: {e}")
        import traceback
        traceback.print_exc()
        return False


def main():
    print("\n=== Live View Verification ===\n")

    checks = [
        check_imports(),
        check_filter_sort(),
        check_qml_syntax(),
        check_model_instantiation(),
    ]

    print("\n=== Summary ===")
    if all(checks):
        print("✓ All checks passed!")
        print("\nLive view implementation is ready:")
        print("  - Model: gui-qt/eigenqt/models/live.py")
        print("  - View: gui-qt/eigenqt/qml/LiveView.qml")
        print("  - Tests: gui-qt/tests/test_live_model.py")
        print("  - Wired into Main.qml (StackLayout index 1)")
        return 0
    else:
        print("✗ Some checks failed")
        return 1


if __name__ == "__main__":
    sys.exit(main())
