from PySide6.QtCore import QSettings

from main import UiSettings


def test_ui_settings_persist_rail_collapsed(tmp_path):
    path = tmp_path / "ui.ini"
    first_settings = QSettings(str(path), QSettings.IniFormat)
    first = UiSettings(settings=first_settings)

    assert first.railCollapsed is False
    changes = []
    first.railCollapsedChanged.connect(lambda: changes.append(first.railCollapsed))

    first.railCollapsed = True
    first_settings.sync()

    assert changes == [True]
    assert UiSettings(settings=QSettings(str(path), QSettings.IniFormat)).railCollapsed is True


def test_ui_settings_ignore_redundant_rail_writes(tmp_path):
    settings = QSettings(str(tmp_path / "ui.ini"), QSettings.IniFormat)
    state = UiSettings(settings=settings)
    changes = []
    state.railCollapsedChanged.connect(lambda: changes.append(state.railCollapsed))

    state.railCollapsed = False

    assert changes == []
