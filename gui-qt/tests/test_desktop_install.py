import os
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_install_desktop_writes_qt_launcher_and_primary_entry(tmp_path):
    fake_repo = tmp_path / "repo with spaces"
    fake_repo.mkdir()
    (fake_repo / "gui-qt").mkdir()
    (fake_repo / "gui-qt" / "run.sh").write_text(
        "#!/usr/bin/env bash\nprintf 'qt-run:%s\\n' \"$*\"\n",
        encoding="utf-8",
    )
    os.chmod(fake_repo / "gui-qt" / "run.sh", 0o755)
    (fake_repo / "main.go").write_text("package main\n", encoding="utf-8")
    (fake_repo / "Makefile").write_text(
        "core:\n"
        "\tmkdir -p bin\n"
        "\tprintf '#!/usr/bin/env bash\\necho eigen\\n' > bin/eigen\n"
        "\tchmod +x bin/eigen\n",
        encoding="utf-8",
    )

    bin_dir = tmp_path / "bin with spaces"
    applications_dir = tmp_path / "applications with spaces"
    env = os.environ.copy()
    env.update(
        {
            "EIGEN_QT_REPO": str(fake_repo),
            "EIGEN_QT_BIN_DIR": str(bin_dir),
            "EIGEN_QT_APPLICATIONS_DIR": str(applications_dir),
        }
    )

    result = subprocess.run(
        ["bash", str(ROOT / "install-desktop.sh")],
        check=True,
        env=env,
        text=True,
        capture_output=True,
    )

    launcher = bin_dir / "eigen-qt"
    qt_entry = applications_dir / "eigen-qt.desktop"
    primary_entry = applications_dir / "eigen-gui.desktop"

    assert "Installed Eigen Qt launcher" in result.stdout
    assert launcher.exists()
    assert os.access(launcher, os.X_OK)
    assert qt_entry.exists()
    assert primary_entry.exists()

    subprocess.run(["bash", "-n", str(launcher)], check=True)

    primary_text = primary_entry.read_text(encoding="utf-8")
    assert "Name=Eigen\n" in primary_text
    assert f'Exec="{launcher}"\n' in primary_text

    qt_text = qt_entry.read_text(encoding="utf-8")
    assert "Name=Eigen (Qt)\n" in qt_text
    assert f'Exec="{launcher}"\n' in qt_text

    launch = subprocess.run(
        [str(launcher), "--probe"],
        check=True,
        text=True,
        capture_output=True,
    )
    assert "eigen-qt: building core binary via 'make core'..." in launch.stderr
    assert launch.stdout == "qt-run:--probe\n"

    launcher_text = launcher.read_text(encoding="utf-8")
    assert "-name '.git'" in launcher_text
    assert "-name '.venv'" in launcher_text
    assert "-name 'node_modules'" in launcher_text
    assert "-name 'bin'" in launcher_text
