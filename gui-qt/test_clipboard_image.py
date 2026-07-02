#!/usr/bin/env python3
"""Test clipboard_helper.pasteImage() with a synthetic QImage."""
import sys
import base64
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from PySide6.QtWidgets import QApplication
from PySide6.QtGui import QImage, QPainter, QColor
from eigenqt.clipboard_helper import ClipboardHelper


def test_paste_image():
    """Test that pasteImage can encode a QImage to base64."""
    app = QApplication(sys.argv)
    helper = ClipboardHelper()

    # Create a small test image (10x10 red square)
    test_image = QImage(10, 10, QImage.Format.Format_RGB32)
    test_image.fill(QColor(255, 0, 0))  # Red

    # Verify image is valid
    assert not test_image.isNull(), "Test image should be valid"

    # Put it on clipboard
    app.clipboard().setImage(test_image)

    # Call pasteImage
    result = helper.pasteImage()

    # Verify result
    assert isinstance(result, str), f"Expected str, got {type(result)}"
    assert len(result) > 0, "Expected non-empty base64 string"

    # Verify it's valid base64
    try:
        decoded = base64.b64decode(result)
        assert len(decoded) > 0, "Decoded bytes should be non-empty"
        print(f"✓ PASS: pasteImage returned {len(result)} chars base64, {len(decoded)} bytes PNG")
    except Exception as e:
        print(f"✗ FAIL: base64 decode failed: {e}")
        sys.exit(1)

    print("✓ All tests passed")


if __name__ == "__main__":
    test_paste_image()
