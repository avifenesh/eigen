#!/usr/bin/env python3
"""
test_connectors.py — Launch ConnectorsView with mock data, take screenshot.
"""
import json
import os
import sys
from pathlib import Path

from PySide6.QtCore import QObject, QTimer, Property, Signal
from PySide6.QtGui import QGuiApplication, QPixmap
from PySide6.QtQml import QQmlApplicationEngine
from PySide6.QtQuick import QQuickWindow
from PySide6.QtQuickControls2 import QQuickStyle

ROOT = Path(__file__).resolve().parent


class MockRpcClient(QObject):
    """Mock RPC client for testing."""

    connected = Signal()
    event = Signal(str, dict)

    def __init__(self, parent=None):
        super().__init__(parent)
        QTimer.singleShot(100, self.connected.emit)

    def call(self, method, *args, callback=None, error_callback=None):
        """Mock RPC call."""
        print(f"RPC call: {method} {args}")
        if callback:
            QTimer.singleShot(50, lambda: callback(self._mock_result(method, *args)))

    def call_parallel(self, calls, callback=None, error_callback=None):
        """Mock parallel RPC calls."""
        results = []
        for method, args in calls:
            results.append(self._mock_result(method, *args))
        if callback:
            QTimer.singleShot(50, lambda: callback(results))

    def _mock_result(self, method, *args):
        """Return mock data for a method."""
        if method == "Connectors":
            return {
                "connectors": [
                    {
                        "name": "notion",
                        "display": "Notion",
                        "glyph": "◈",
                        "url": "https://mcp.notion.com/mcp",
                        "description": "Notion workspace — pages, databases, and blocks.",
                        "connected": True,
                        "disabled": False,
                        "expiry": "",
                        "requiresAuth": True,
                        "type": "remote",
                    },
                    {
                        "name": "slack",
                        "display": "Slack",
                        "glyph": "◆",
                        "url": "https://mcp.slack.com/mcp",
                        "description": "Slack workspace — channels, messages, and users.",
                        "connected": False,
                        "disabled": False,
                        "expiry": "",
                        "requiresAuth": True,
                        "type": "remote",
                    },
                ],
                "directory": [
                    {
                        "name": "github",
                        "display": "GitHub",
                        "glyph": "⬢",
                        "url": "https://mcp.github.com/mcp",
                        "description": "GitHub repositories, issues, and pull requests.",
                        "category": "Development",
                        "added": False,
                    },
                    {
                        "name": "linear",
                        "display": "Linear",
                        "glyph": "◎",
                        "url": "https://mcp.linear.app/mcp",
                        "description": "Linear issues and projects.",
                        "category": "Project Management",
                        "added": False,
                    },
                    {
                        "name": "figma",
                        "display": "Figma",
                        "glyph": "◇",
                        "url": "https://mcp.figma.com/mcp",
                        "description": "Figma designs and prototypes.",
                        "category": "Design",
                        "added": False,
                    },
                ],
            }
        elif method == "MCPServers":
            return {
                "servers": [
                    {
                        "name": "github",
                        "description": "GitHub MCP server",
                        "command": ["docker", "run", "ghcr.io/github/github-mcp-server"],
                        "disabled": False,
                        "remote": False,
                        "secretEnvKeys": ["GITHUB_PERSONAL_ACCESS_TOKEN"],
                    },
                    {
                        "name": "filesystem",
                        "description": "Local filesystem access",
                        "command": ["npx", "-y", "@modelcontextprotocol/server-filesystem"],
                        "disabled": True,
                        "remote": False,
                        "secretEnvKeys": [],
                    },
                ]
            }
        elif method == "GoogleStatus":
            return {
                "configured": True,
                "connected": True,
                "clientPath": "/home/user/.eigen/google_client.json",
                "setupUrl": "https://console.cloud.google.com/apis/credentials",
                "setupHint": "Create a Desktop OAuth client",
            }
        elif method == "ObsidianStatus":
            return {
                "available": True,
                "vault": "/home/user/Documents/vault",
            }
        elif method == "RevutoStatus":
            return {
                "available": True,
                "count": 3,
                "paused": 1,
            }
        elif method == "MCPSecretsAvailable":
            return True
        elif method == "RevutoReviewers":
            return [
                {"repo": "owner/repo1", "paused": False},
                {"repo": "owner/repo2", "paused": True},
                {"repo": "owner/repo3", "paused": False},
            ]
        return {}

    def subscribe(self, channels):
        """Mock subscribe."""
        pass

    def unsubscribe(self, channels):
        """Mock unsubscribe."""
        pass


class MockConnectorsModel(QObject):
    """Mock ConnectorsModel."""

    connectors_changed = Signal()
    servers_changed = Signal()
    google_status_changed = Signal()
    obsidian_status_changed = Signal()
    revuto_status_changed = Signal()
    loading_changed = Signal()
    load_error_changed = Signal()
    busy_changed = Signal()
    connecting_changed = Signal()
    confirm_remove_connector_changed = Signal()
    confirm_remove_server_changed = Signal()
    google_busy_changed = Signal()
    obsidian_busy_changed = Signal()
    revuto_open_changed = Signal()
    reviewers_changed = Signal()
    revuto_busy_changed = Signal()
    secrets_ok_changed = Signal()
    add_open_changed = Signal()
    add_name_changed = Signal()
    add_url_changed = Signal()
    add_desc_changed = Signal()
    adding_changed = Signal()
    srv_open_changed = Signal()
    srv_name_changed = Signal()
    srv_command_changed = Signal()
    srv_desc_changed = Signal()
    srv_env_changed = Signal()
    srv_secret_changed = Signal()
    srv_saving_changed = Signal()

    def __init__(self, client, parent=None):
        super().__init__(parent)
        self._client = client
        self._connectors = None
        self._servers = None
        self._google_status = None
        self._obsidian_status = None
        self._revuto_status = None
        self._loading = False
        self._load_error = ""
        self._busy = {}
        self._connecting = {}
        self._confirm_remove_connector = {}
        self._confirm_remove_server = {}
        self._google_busy = False
        self._obsidian_busy = False
        self._revuto_open = False
        self._reviewers = []
        self._revuto_busy = {}
        self._secrets_ok = True
        self._add_open = False
        self._add_name = ""
        self._add_url = ""
        self._add_desc = ""
        self._adding = False
        self._srv_open = False
        self._srv_name = ""
        self._srv_command = ""
        self._srv_desc = ""
        self._srv_env = ""
        self._srv_secret = ""
        self._srv_saving = False

        # Load mock data
        QTimer.singleShot(200, self.load)

    @Property("QVariant", notify=connectors_changed)
    def connectors(self):
        return self._connectors

    @connectors.setter
    def connectors(self, value):
        self._connectors = value
        self.connectors_changed.emit()

    @Property("QVariant", notify=servers_changed)
    def servers(self):
        return self._servers

    @servers.setter
    def servers(self, value):
        self._servers = value
        self.servers_changed.emit()

    @Property("QVariant", notify=google_status_changed)
    def google_status(self):
        return self._google_status

    @google_status.setter
    def google_status(self, value):
        self._google_status = value
        self.google_status_changed.emit()

    @Property("QVariant", notify=obsidian_status_changed)
    def obsidian_status(self):
        return self._obsidian_status

    @obsidian_status.setter
    def obsidian_status(self, value):
        self._obsidian_status = value
        self.obsidian_status_changed.emit()

    @Property("QVariant", notify=revuto_status_changed)
    def revuto_status(self):
        return self._revuto_status

    @revuto_status.setter
    def revuto_status(self, value):
        self._revuto_status = value
        self.revuto_status_changed.emit()

    @Property(bool, notify=loading_changed)
    def loading(self):
        return self._loading

    @loading.setter
    def loading(self, value):
        self._loading = value
        self.loading_changed.emit()

    @Property(str, notify=load_error_changed)
    def load_error(self):
        return self._load_error

    @Property("QVariant", notify=busy_changed)
    def busy(self):
        return self._busy

    @Property("QVariant", notify=connecting_changed)
    def connecting(self):
        return self._connecting

    @Property("QVariant", notify=confirm_remove_connector_changed)
    def confirm_remove_connector(self):
        return self._confirm_remove_connector

    @Property("QVariant", notify=confirm_remove_server_changed)
    def confirm_remove_server(self):
        return self._confirm_remove_server

    @Property(bool, notify=google_busy_changed)
    def google_busy(self):
        return self._google_busy

    @Property(bool, notify=obsidian_busy_changed)
    def obsidian_busy(self):
        return self._obsidian_busy

    @Property(bool, notify=revuto_open_changed)
    def revuto_open(self):
        return self._revuto_open

    @revuto_open.setter
    def revuto_open(self, value):
        self._revuto_open = value
        self.revuto_open_changed.emit()

    @Property(list, notify=reviewers_changed)
    def reviewers(self):
        return self._reviewers

    @reviewers.setter
    def reviewers(self, value):
        self._reviewers = value
        self.reviewers_changed.emit()

    @Property("QVariant", notify=revuto_busy_changed)
    def revuto_busy(self):
        return self._revuto_busy

    @Property(bool, notify=secrets_ok_changed)
    def secrets_ok(self):
        return self._secrets_ok

    @Property(bool, notify=add_open_changed)
    def add_open(self):
        return self._add_open

    @add_open.setter
    def add_open(self, value):
        self._add_open = value
        self.add_open_changed.emit()

    @Property(str, notify=add_name_changed)
    def add_name(self):
        return self._add_name

    @add_name.setter
    def add_name(self, value):
        self._add_name = value
        self.add_name_changed.emit()

    @Property(str, notify=add_url_changed)
    def add_url(self):
        return self._add_url

    @add_url.setter
    def add_url(self, value):
        self._add_url = value
        self.add_url_changed.emit()

    @Property(str, notify=add_desc_changed)
    def add_desc(self):
        return self._add_desc

    @add_desc.setter
    def add_desc(self, value):
        self._add_desc = value
        self.add_desc_changed.emit()

    @Property(bool, notify=adding_changed)
    def adding(self):
        return self._adding

    @Property(bool, notify=srv_open_changed)
    def srv_open(self):
        return self._srv_open

    @srv_open.setter
    def srv_open(self, value):
        self._srv_open = value
        self.srv_open_changed.emit()

    @Property(str, notify=srv_name_changed)
    def srv_name(self):
        return self._srv_name

    @srv_name.setter
    def srv_name(self, value):
        self._srv_name = value
        self.srv_name_changed.emit()

    @Property(str, notify=srv_command_changed)
    def srv_command(self):
        return self._srv_command

    @srv_command.setter
    def srv_command(self, value):
        self._srv_command = value
        self.srv_command_changed.emit()

    @Property(str, notify=srv_desc_changed)
    def srv_desc(self):
        return self._srv_desc

    @srv_desc.setter
    def srv_desc(self, value):
        self._srv_desc = value
        self.srv_desc_changed.emit()

    @Property(str, notify=srv_env_changed)
    def srv_env(self):
        return self._srv_env

    @srv_env.setter
    def srv_env(self, value):
        self._srv_env = value
        self.srv_env_changed.emit()

    @Property(str, notify=srv_secret_changed)
    def srv_secret(self):
        return self._srv_secret

    @srv_secret.setter
    def srv_secret(self, value):
        self._srv_secret = value
        self.srv_secret_changed.emit()

    @Property(bool, notify=srv_saving_changed)
    def srv_saving(self):
        return self._srv_saving

    def load(self):
        """Load mock data."""
        self.loading = True

        def on_done(results):
            c, s, g = results
            self.connectors = c
            self.servers = s
            self.google_status = g
            self.loading = False

        self._client.call_parallel(
            [("Connectors", []), ("MCPServers", []), ("GoogleStatus", [])],
            callback=on_done,
        )

        self._client.call("ObsidianStatus", callback=lambda r: setattr(self, "obsidian_status", r))
        self._client.call("RevutoStatus", callback=lambda r: setattr(self, "revuto_status", r))
        self._client.call("MCPSecretsAvailable", callback=lambda r: setattr(self, "_secrets_ok", r))


def main():
    os.environ["QT_QPA_PLATFORM"] = "offscreen"
    QQuickStyle.setStyle("Basic")

    app = QGuiApplication(sys.argv)
    app.setOrganizationName("eigen")
    app.setApplicationName("eigen-test")

    engine = QQmlApplicationEngine()
    ctx = engine.rootContext()

    # Create mock client + model
    client = MockRpcClient()
    connectors_model = MockConnectorsModel(client)

    # Expose to QML
    ctx.setContextProperty("connectorsModel", connectors_model)

    # Load QML
    engine.addImportPath(str(ROOT / "eigenqt"))
    qml_path = str(ROOT / "eigenqt" / "qml" / "ConnectorsViewTest.qml")
    engine.load(qml_path)

    if not engine.rootObjects():
        print("Failed to load QML")
        sys.exit(-1)

    root_obj = engine.rootObjects()[0]

    # Wait for rendering
    def capture():
        window = None
        if isinstance(root_obj, QQuickWindow):
            window = root_obj
        else:
            # Find the window
            for obj in engine.rootObjects():
                if isinstance(obj, QQuickWindow):
                    window = obj
                    break

        if window:
            pixmap = window.grabWindow()
            output_path = ROOT / "screenshots" / "connectors_view.png"
            output_path.parent.mkdir(parents=True, exist_ok=True)
            pixmap.save(str(output_path))
            print(f"Screenshot saved: {output_path}")
        else:
            print("Could not find QQuickWindow")

        app.quit()

    QTimer.singleShot(3000, capture)
    sys.exit(app.exec())


if __name__ == "__main__":
    main()
