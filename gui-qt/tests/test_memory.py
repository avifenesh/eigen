"""Test MemoryModel helper logic (pure Python functions)."""

import re


def backup_name(path: str) -> str:
    """Extract filename from backup path."""
    parts = path.split("/")
    return parts[-1] if parts else path


def backup_when(path: str) -> str:
    """Format backup timestamp from filename."""
    name = backup_name(path)
    m = re.match(r".*\.(\d{8})-(\d{6})\.bak$", name)
    if not m:
        return name

    d, t = m.groups()
    date = f"{d[0:4]}-{d[4:6]}-{d[6:8]}"
    time = f"{t[0:2]}:{t[2:4]}:{t[4:6]}"
    return f"{date} {time}"


def short_dir(d: str) -> str:
    """Extract basename from directory path."""
    d = d.rstrip("/")
    parts = d.split("/")
    return parts[-1] if parts else d


def test_backup_name():
    """Test backup_name helper extraction."""
    assert (
        backup_name("/path/to/MEMORY.md.20240101-120000.bak")
        == "MEMORY.md.20240101-120000.bak"
    )
    assert backup_name("MEMORY.md.20240101-120000.bak") == "MEMORY.md.20240101-120000.bak"


def test_backup_when():
    """Test backup_when timestamp parsing."""
    # Valid timestamp
    assert backup_when("MEMORY.md.20240315-143022.bak") == "2024-03-15 14:30:22"

    # Invalid format (no timestamp)
    assert backup_when("MEMORY.md.bak") == "MEMORY.md.bak"


def test_short_dir():
    """Test short_dir basename extraction."""
    assert short_dir("/home/user/eigen") == "eigen"
    assert short_dir("/home/user/eigen/") == "eigen"
    assert short_dir("eigen") == "eigen"
    assert short_dir("/") == ""
