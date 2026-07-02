import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import "Theme.js" as Theme

// Connectors view — the desktop superapp integrations surface (Connectors.svelte port).
// Remote MCP connectors (OAuth), catalog directory, local MCP servers wiring,
// Google/Obsidian/Revuto native built-ins.
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property var connectorsModel: null  // ConnectorsModel from Python

    Flickable {
        anchors.fill: parent
        contentWidth: width
        contentHeight: contentColumn.implicitHeight
        clip: true

        ColumnLayout {
            id: contentColumn
            width: Math.min(parent.width - Theme.space.xl * 2, 920)
            anchors.horizontalCenter: parent.horizontalCenter
            anchors.top: parent.top
            anchors.topMargin: Theme.space.xl
            spacing: Theme.space.lg

            // Header
            ColumnLayout {
                Layout.fillWidth: true
                spacing: Theme.space.sm

                Label {
                    text: "Connectors"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h3
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textPrimary
                }

                Label {
                    text: "Connect external apps over the Model Context Protocol. Each connector is a remote MCP server you authorize once with OAuth — the token is stored in your OS keychain and refreshes automatically."
                    font.pixelSize: Theme.fontSize.label
                    color: Theme.colors.textMuted
                    wrapMode: Text.WordWrap
                    Layout.fillWidth: true
                    Layout.maximumWidth: 720
                }
            }

            // Loading skeleton
            ColumnLayout {
                visible: connectorsModel && connectorsModel.loading && !connectorsModel.connectors
                Layout.fillWidth: true
                spacing: Theme.space.md

                Repeater {
                    model: 3
                    Rectangle {
                        Layout.fillWidth: true
                        Layout.preferredHeight: 76
                        color: Theme.colors.bgInset
                        radius: Theme.radius.md
                    }
                }
            }

            // Error state
            Rectangle {
                visible: connectorsModel && connectorsModel.load_error && !connectorsModel.connectors
                Layout.fillWidth: true
                Layout.preferredHeight: 120
                color: Theme.colors.bgRaised
                border.width: 1
                border.color: Theme.colors.borderHairline
                radius: Theme.radius.md

                ColumnLayout {
                    anchors.centerIn: parent
                    spacing: Theme.space.md

                    Label {
                        text: "⟐"
                        font.pixelSize: 32
                        color: Theme.colors.textFaint
                        Layout.alignment: Qt.AlignHCenter
                    }

                    Label {
                        text: "Couldn't load connectors"
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                        Layout.alignment: Qt.AlignHCenter
                    }

                    Label {
                        text: connectorsModel ? connectorsModel.load_error : ""
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.textMuted
                        Layout.alignment: Qt.AlignHCenter
                    }

                    Button {
                        text: "Retry"
                        onClicked: connectorsModel.load()
                        Layout.alignment: Qt.AlignHCenter
                    }
                }
            }

            // Content (when loaded)
            ColumnLayout {
                visible: connectorsModel && connectorsModel.connectors
                Layout.fillWidth: true
                spacing: Theme.space.lg

                // Google built-in
                ConnectorCard {
                    visible: connectorsModel && connectorsModel.google_status
                    Layout.fillWidth: true
                    glyph: "◷"
                    connectorName: "Google"
                    description: "Calendar + Gmail — read events & email, create events."
                    statusBadge: gStatusBadge()
                    statusColor: gStatusColor()
                    metaLine: gMetaLine()
                    busy: connectorsModel ? connectorsModel.google_busy : false
                    primaryAction: gPrimaryAction()
                    primaryLabel: gPrimaryLabel()
                    onPrimaryClicked: {
                        if (!connectorsModel || !connectorsModel.google_status) return
                        var gs = connectorsModel.google_status
                        if (gs.connected) connectorsModel.disconnect_google()
                        else if (gs.configured) connectorsModel.connect_google()
                        else connectorsModel.setup_google()
                    }
                }

                // Obsidian built-in
                ConnectorCard {
                    visible: connectorsModel && connectorsModel.obsidian_status
                    Layout.fillWidth: true
                    glyph: "≣"
                    connectorName: "Obsidian"
                    description: "Read, search & write notes in your Obsidian vault."
                    statusBadge: obsAvail() ? "connected" : "no vault"
                    statusColor: obsAvail() ? Theme.colors.brandBright : Theme.colors.textFaint
                    metaLine: obsMetaLine()
                    busy: connectorsModel ? connectorsModel.obsidian_busy : false
                    primaryAction: "secondary"
                    primaryLabel: obsAvail() ? "Change vault" : "Choose vault"
                    onPrimaryClicked: vaultDialog.open()
                }

                // Revuto built-in
                ConnectorCard {
                    visible: connectorsModel && connectorsModel.revuto_status
                    Layout.fillWidth: true
                    glyph: "⌕"
                    connectorName: "Revuto"
                    description: "Your AI PR-reviewer — list reviewers, trigger review/learn/decay, pause/resume."
                    statusBadge: revBadge()
                    statusColor: revColor()
                    metaLine: revMetaLine()
                    busy: false
                    primaryAction: revAvail() ? "secondary" : ""
                    primaryLabel: revAvail() ? (connectorsModel && connectorsModel.revuto_open ? "Hide" : "Manage") : ""
                    onPrimaryClicked: {
                        if (connectorsModel) connectorsModel.toggle_revuto()
                    }

                    // Reviewers list (expanded)
                    ColumnLayout {
                        visible: connectorsModel && connectorsModel.revuto_open
                        Layout.fillWidth: true
                        Layout.topMargin: Theme.space.md
                        spacing: Theme.space.sm

                        Rectangle {
                            Layout.fillWidth: true
                            height: 1
                            color: Theme.colors.divider
                        }

                        Repeater {
                            model: connectorsModel ? connectorsModel.reviewers : []
                            delegate: Rectangle {
                                Layout.fillWidth: true
                                implicitHeight: 32
                                color: revMouseArea.containsMouse ? Theme.colors.stateHover : "transparent"
                                radius: Theme.radius.sm

                                property var reviewer: modelData

                                MouseArea {
                                    id: revMouseArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                }

                                RowLayout {
                                    anchors.fill: parent
                                    anchors.margins: Theme.space.sm
                                    spacing: Theme.space.md

                                    Label {
                                        text: reviewer.repo
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textSecondary
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        visible: reviewer.paused
                                        text: "paused"
                                        font.pixelSize: Theme.fontSize.micro
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.textFaint
                                        font.capitalization: Font.AllUppercase
                                    }

                                    Item { Layout.fillWidth: true }

                                    Button {
                                        text: "Review now"
                                        enabled: !isRevBusy(reviewer.repo)
                                        onClicked: connectorsModel.revuto_trigger(reviewer.repo)
                                    }

                                    Button {
                                        text: reviewer.paused ? "Resume" : "Pause"
                                        enabled: !isRevBusy(reviewer.repo)
                                        onClicked: connectorsModel.revuto_pause(reviewer.repo, !reviewer.paused)
                                    }
                                }
                            }
                        }
                    }
                }

                // Add connector button
                RowLayout {
                    Layout.fillWidth: true
                    Layout.alignment: Qt.AlignRight

                    Button {
                        visible: connectorsModel && !connectorsModel.add_open
                        text: "+ Add connector"
                        onClicked: connectorsModel.add_open = true
                    }
                }

                // Add connector form
                Rectangle {
                    visible: connectorsModel && connectorsModel.add_open
                    Layout.fillWidth: true
                    color: Theme.colors.bgRaised
                    border.width: 1
                    border.color: Theme.colors.borderSubtle
                    radius: Theme.radius.md
                    implicitHeight: addFormColumn.implicitHeight + Theme.space.xl * 2

                    ColumnLayout {
                        id: addFormColumn
                        anchors.fill: parent
                        anchors.margins: Theme.space.xl
                        spacing: Theme.space.md

                        // Name
                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: Theme.space.xs

                            Label {
                                text: "Name"
                                font.pixelSize: Theme.fontSize.label
                                font.weight: Theme.fontWeight.medium
                                color: Theme.colors.textMuted
                            }

                            TextField {
                                Layout.fillWidth: true
                                placeholderText: "notion"
                                text: connectorsModel ? connectorsModel.add_name : ""
                                onTextChanged: if (connectorsModel) connectorsModel.add_name = text
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                            }
                        }

                        // URL
                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: Theme.space.xs

                            Label {
                                text: "Remote MCP URL"
                                font.pixelSize: Theme.fontSize.label
                                font.weight: Theme.fontWeight.medium
                                color: Theme.colors.textMuted
                            }

                            TextField {
                                Layout.fillWidth: true
                                placeholderText: "https://mcp.notion.com/mcp"
                                text: connectorsModel ? connectorsModel.add_url : ""
                                onTextChanged: if (connectorsModel) connectorsModel.add_url = text
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                            }
                        }

                        // Description
                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: Theme.space.xs

                            Label {
                                text: "Description"
                                font.pixelSize: Theme.fontSize.label
                                font.weight: Theme.fontWeight.medium
                                color: Theme.colors.textMuted
                            }

                            TextField {
                                Layout.fillWidth: true
                                placeholderText: "Notion workspace (optional)"
                                text: connectorsModel ? connectorsModel.add_desc : ""
                                onTextChanged: if (connectorsModel) connectorsModel.add_desc = text
                                font.pixelSize: Theme.fontSize.bodySm
                            }
                        }

                        // Actions
                        RowLayout {
                            Layout.topMargin: Theme.space.sm
                            spacing: Theme.space.sm

                            Button {
                                text: connectorsModel && connectorsModel.adding ? "Saving…" : "Add & authorize"
                                enabled: connectorsModel && !connectorsModel.adding
                                onClicked: {
                                    if (connectorsModel && connectorsModel.add_name.trim()) {
                                        connectorsModel.add_connector(connectorsModel.add_name.trim())
                                    }
                                }
                            }

                            Button {
                                text: "Cancel"
                                enabled: connectorsModel && !connectorsModel.adding
                                onClicked: connectorsModel.add_open = false
                            }
                        }
                    }
                }

                // Connector list
                Repeater {
                    model: connectorsModel && connectorsModel.connectors ? connectorsModel.connectors.connectors : []
                    delegate: ConnectorCard {
                        Layout.fillWidth: true
                        property var connector: modelData
                        glyph: (connector.glyph || "")
                        connectorName: (connector.display || connector.name)
                        description: (connector.description || "")
                        statusBadge: connStatusBadge(connector)
                        statusColor: connStatusColor(connector)
                        metaLine: connector.url
                        busy: isBusy(connector.name)
                        primaryAction: connector.connected ? "secondary" : "primary"
                        primaryLabel: connPrimaryLabel(connector)
                        secondaryActions: true
                        onPrimaryClicked: {
                            if (connector.connected) connectorsModel.disconnect_connector(connector.name)
                            else connectorsModel.connect_connector(connector.name)
                        }
                        onRemoveClicked: {
                            if (isConfirmRemoveConnector(connector.name)) {
                                connectorsModel.remove_connector(connector.name)
                            } else {
                                connectorsModel.confirm_remove_connector_set(connector.name)
                            }
                        }
                        onRemoveCancelled: connectorsModel.confirm_remove_connector_cancel(connector.name)
                        showRemoveConfirm: isConfirmRemoveConnector(connector.name)
                    }
                }

                // Empty state
                Rectangle {
                    visible: connectorsModel && connectorsModel.connectors && connectorsModel.connectors.connectors.length === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: 120
                    color: Theme.colors.bgRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline
                    radius: Theme.radius.md

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.sm

                        Label {
                            text: "⟐"
                            font.pixelSize: 32
                            color: Theme.colors.textFaint
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            text: "No connectors yet"
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            text: "Pick one from the directory below, or add a remote MCP server by URL."
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                }

                // Directory section
                ColumnLayout {
                    visible: connectorsModel && connectorsModel.connectors && connectorsModel.connectors.directory.length > 0
                    Layout.fillWidth: true
                    Layout.topMargin: Theme.space.lg
                    spacing: Theme.space.md

                    Label {
                        text: "Directory"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        text: "Popular connectors — one click to add and authorize."
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.textMuted
                    }

                    Grid {
                        Layout.fillWidth: true
                        columns: Math.max(1, Math.floor(parent.width / 180))
                        columnSpacing: Theme.space.md
                        rowSpacing: Theme.space.md

                        Repeater {
                            model: connectorsModel && connectorsModel.connectors ? connectorsModel.connectors.directory : []
                            delegate: CatalogTile {
                                property var entry: modelData
                                width: 160
                                glyph: (entry.glyph || "")
                                displayName: (entry.display || "")
                                category: (entry.category || "")
                                isAdded: entry.added || false
                                isConnecting: isConnecting(entry.name)
                                onClicked: {
                                    if (!entry.added && !isConnecting(entry.name)) {
                                        connectorsModel.add_from_catalog(entry.name)
                                    }
                                }
                            }
                        }
                    }
                }

                // Local MCP servers section
                ColumnLayout {
                    Layout.fillWidth: true
                    Layout.topMargin: Theme.space.xl
                    spacing: Theme.space.md

                    Label {
                        text: "Local MCP servers"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.space.sm

                        Label {
                            text: "Stdio servers wired in mcp.json — add, toggle, or remove them here."
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            Layout.fillWidth: true
                        }

                        Label {
                            visible: connectorsModel && !connectorsModel.secrets_ok
                            text: "No OS keychain detected — secret values would be stored as plaintext."
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.warn
                        }
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        Layout.alignment: Qt.AlignRight

                        Button {
                            visible: connectorsModel && !connectorsModel.srv_open
                            text: "+ Add MCP server"
                            onClicked: connectorsModel.srv_open = true
                        }
                    }

                    // Add server form
                    Rectangle {
                        visible: connectorsModel && connectorsModel.srv_open
                        Layout.fillWidth: true
                        color: Theme.colors.bgRaised
                        border.width: 1
                        border.color: Theme.colors.borderSubtle
                        radius: Theme.radius.md
                        implicitHeight: srvFormColumn.implicitHeight + Theme.space.xl * 2

                        ColumnLayout {
                            id: srvFormColumn
                            anchors.fill: parent
                            anchors.margins: Theme.space.xl
                            spacing: Theme.space.md

                            // Name
                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: Theme.space.xs

                                Label {
                                    text: "Name"
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.medium
                                    color: Theme.colors.textMuted
                                }

                                TextField {
                                    Layout.fillWidth: true
                                    placeholderText: "github"
                                    text: connectorsModel ? connectorsModel.srv_name : ""
                                    onTextChanged: if (connectorsModel) connectorsModel.srv_name = text
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                }
                            }

                            // Command
                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: Theme.space.xs

                                Label {
                                    text: "Command"
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.medium
                                    color: Theme.colors.textMuted
                                }

                                TextField {
                                    Layout.fillWidth: true
                                    placeholderText: "docker run ghcr.io/github/github-mcp-server"
                                    text: connectorsModel ? connectorsModel.srv_command : ""
                                    onTextChanged: if (connectorsModel) connectorsModel.srv_command = text
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                }
                            }

                            // Description
                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: Theme.space.xs

                                Label {
                                    text: "Description"
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.medium
                                    color: Theme.colors.textMuted
                                }

                                TextField {
                                    Layout.fillWidth: true
                                    placeholderText: "GitHub MCP (optional)"
                                    text: connectorsModel ? connectorsModel.srv_desc : ""
                                    onTextChanged: if (connectorsModel) connectorsModel.srv_desc = text
                                    font.pixelSize: Theme.fontSize.bodySm
                                }
                            }

                            // Env
                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: Theme.space.xs

                                Label {
                                    text: "Env (KEY=VALUE per line)"
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.medium
                                    color: Theme.colors.textMuted
                                }

                                TextArea {
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: 60
                                    placeholderText: "LOG_LEVEL=info"
                                    text: connectorsModel ? connectorsModel.srv_env : ""
                                    onTextChanged: if (connectorsModel) connectorsModel.srv_env = text
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                }
                            }

                            // Secret env
                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: Theme.space.xs

                                Label {
                                    text: connectorsModel && connectorsModel.secrets_ok ? "Secret env → keychain (KEY=VALUE per line)" : "Secret env (no keychain — stored as plaintext)"
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.medium
                                    color: Theme.colors.textMuted
                                }

                                TextArea {
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: 60
                                    placeholderText: "GITHUB_PERSONAL_ACCESS_TOKEN=ghp_…"
                                    text: connectorsModel ? connectorsModel.srv_secret : ""
                                    onTextChanged: if (connectorsModel) connectorsModel.srv_secret = text
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                }
                            }

                            // Actions
                            RowLayout {
                                Layout.topMargin: Theme.space.sm
                                spacing: Theme.space.sm

                                Button {
                                    text: connectorsModel && connectorsModel.srv_saving ? "Saving…" : "Save server"
                                    enabled: connectorsModel && !connectorsModel.srv_saving
                                    onClicked: connectorsModel.save_local_server()
                                }

                                Button {
                                    text: "Cancel"
                                    enabled: connectorsModel && !connectorsModel.srv_saving
                                    onClicked: connectorsModel.srv_open = false
                                }
                            }
                        }
                    }

                    // Servers list
                    Repeater {
                        model: stdioServers()
                        delegate: ConnectorCard {
                            Layout.fillWidth: true
                            property var server: modelData
                            glyph: ""
                            connectorName: server.name
                            description: (server.description || "")
                            statusBadge: server.disabled ? "disabled" : "enabled"
                            statusColor: server.disabled ? Theme.colors.textFaint : Theme.colors.brandBright
                            metaLine: (server.command || []).join(" ")
                            secretBadge: (server.secretEnvKeys || []).length > 0 ? ("⚿ " + server.secretEnvKeys.length + " secret") : ""
                            busy: isBusy(server.name)
                            primaryAction: "secondary"
                            primaryLabel: server.disabled ? "Enable" : "Disable"
                            secondaryActions: true
                            onPrimaryClicked: connectorsModel.toggle_server(server.name, !server.disabled)
                            onRemoveClicked: {
                                if (isConfirmRemoveServer(server.name)) {
                                    connectorsModel.remove_server(server.name)
                                } else {
                                    connectorsModel.confirm_remove_server_set(server.name)
                                }
                            }
                            onRemoveCancelled: connectorsModel.confirm_remove_server_cancel(server.name)
                            showRemoveConfirm: isConfirmRemoveServer(server.name)
                        }
                    }
                }
            }
        }
    }

    // File dialog for choosing Obsidian vault
    FolderDialog {
        id: vaultDialog
        title: "Choose Obsidian Vault"
        currentFolder: "file:///home"
        onAccepted: {
            if (connectorsModel) {
                // Extract path from file:// URL
                var path = selectedFolder.toString().replace(/^file:\/\//, "")
                connectorsModel.choose_vault(path)
            }
        }
    }

    // Inline component: ConnectorCard
    component ConnectorCard: Rectangle {
        id: card
        color: Theme.colors.bgRaised
        border.width: 1
        border.color: Theme.colors.borderSubtle
        radius: Theme.radius.md
        implicitHeight: cardContent.implicitHeight + Theme.space.xl * 2

        property string glyph: ""
        property string connectorName: ""
        property string description: ""
        property string statusBadge: ""
        property color statusColor: Theme.colors.textFaint
        property string metaLine: ""
        property string secretBadge: ""
        property bool busy: false
        property string primaryAction: "primary"  // "primary", "secondary", or ""
        property string primaryLabel: ""
        property bool secondaryActions: false
        property bool showRemoveConfirm: false

        signal primaryClicked()
        signal removeClicked()
        signal removeCancelled()

        RowLayout {
            id: cardContent
            anchors.fill: parent
            anchors.margins: Theme.space.xl
            spacing: Theme.space.xl

            // Info column
            ColumnLayout {
                Layout.fillWidth: true
                spacing: Theme.space.sm

                // Name + badges row
                RowLayout {
                    spacing: Theme.space.md

                    Label {
                        visible: glyph
                        text: glyph
                        font.pixelSize: Theme.fontSize.body
                    }

                    Label {
                        text: connectorName
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Rectangle {
                        visible: statusBadge
                        implicitWidth: statusLabel.implicitWidth + Theme.space.md
                        implicitHeight: 18
                        radius: Theme.radius.full
                        color: Qt.rgba(statusColor.r, statusColor.g, statusColor.b, 0.1)
                        border.width: 1
                        border.color: Qt.rgba(statusColor.r, statusColor.g, statusColor.b, 0.2)

                        Label {
                            id: statusLabel
                            anchors.centerIn: parent
                            text: statusBadge
                            font.pixelSize: Theme.fontSize.micro
                            font.weight: Theme.fontWeight.semibold
                            font.capitalization: Font.AllUppercase
                            color: statusColor
                        }
                    }

                    Rectangle {
                        visible: secretBadge
                        implicitWidth: secretLabel.implicitWidth + Theme.space.md
                        implicitHeight: 18
                        radius: Theme.radius.full
                        color: Theme.colors.bgRaised2
                        border.width: 1
                        border.color: Theme.colors.borderBrandFaint

                        Label {
                            id: secretLabel
                            anchors.centerIn: parent
                            text: secretBadge
                            font.pixelSize: Theme.fontSize.micro
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textMuted
                        }
                    }
                }

                Label {
                    visible: description
                    text: description
                    font.pixelSize: Theme.fontSize.label
                    color: Theme.colors.textMuted
                    wrapMode: Text.WordWrap
                    Layout.fillWidth: true
                }

                Label {
                    visible: metaLine
                    text: metaLine
                    font.family: Theme.monoFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    color: Theme.colors.textFaint
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }
            }

            // Actions column
            RowLayout {
                spacing: Theme.space.sm

                Button {
                    visible: primaryAction && primaryLabel
                    text: primaryLabel
                    enabled: !busy
                    onClicked: card.primaryClicked()
                }

                Button {
                    visible: secondaryActions && !showRemoveConfirm
                    text: "Remove"
                    enabled: !busy
                    onClicked: card.removeClicked()
                }

                Button {
                    visible: secondaryActions && showRemoveConfirm
                    text: "Confirm"
                    enabled: !busy
                    onClicked: card.removeClicked()
                }

                Button {
                    visible: secondaryActions && showRemoveConfirm
                    text: "Cancel"
                    enabled: !busy
                    onClicked: card.removeCancelled()
                }
            }
        }
    }

    // Inline component: CatalogTile
    component CatalogTile: Rectangle {
        id: tile
        implicitHeight: tileContent.implicitHeight + Theme.space.lg * 2
        color: tileMouseArea.containsMouse ? Theme.colors.stateHover : Theme.colors.bgRaised2
        border.width: 1
        border.color: Theme.colors.borderSubtle
        radius: Theme.radius.md
        opacity: isAdded ? 0.55 : 1.0

        property string glyph: ""
        property string displayName: ""
        property string category: ""
        property bool isAdded: false
        property bool isConnecting: false

        signal clicked()

        Behavior on color { ColorAnimation { duration: Theme.duration.fast } }

        MouseArea {
            id: tileMouseArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: isAdded || isConnecting ? Qt.ArrowCursor : Qt.PointingHandCursor
            enabled: !isAdded && !isConnecting
            onClicked: tile.clicked()
        }

        ColumnLayout {
            id: tileContent
            anchors.fill: parent
            anchors.margins: Theme.space.lg
            spacing: Theme.space.xs

            Label {
                text: glyph
                font.pixelSize: 22
                Layout.alignment: Qt.AlignLeft
            }

            Label {
                text: displayName
                font.pixelSize: Theme.fontSize.bodySm
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textPrimary
                wrapMode: Text.WordWrap
                Layout.fillWidth: true
            }

            Label {
                text: category
                font.pixelSize: Theme.fontSize.micro
                color: Theme.colors.textFaint
            }

            Label {
                text: isAdded ? "added" : (isConnecting ? "authorizing…" : "+ connect")
                font.pixelSize: Theme.fontSize.label
                font.weight: Theme.fontWeight.medium
                color: isAdded || isConnecting ? Theme.colors.textFaint : Theme.colors.brandBright
                Layout.topMargin: Theme.space.sm
            }
        }
    }

    // Helper functions
    function gStatusBadge() {
        if (!connectorsModel || !connectorsModel.google_status) return ""
        var gs = connectorsModel.google_status
        if (gs.connected) return "connected"
        if (gs.configured) return "not connected"
        return "setup needed"
    }

    function gStatusColor() {
        if (!connectorsModel || !connectorsModel.google_status) return Theme.colors.textFaint
        var gs = connectorsModel.google_status
        if (gs.connected) return Theme.colors.brandBright
        return Theme.colors.textFaint
    }

    function gMetaLine() {
        if (!connectorsModel || !connectorsModel.google_status) return ""
        var gs = connectorsModel.google_status
        if (!gs.configured) return "Set up opens Google Cloud Console — create a Desktop OAuth client, then pick the downloaded JSON."
        return ""
    }

    function gPrimaryAction() {
        if (!connectorsModel || !connectorsModel.google_status) return ""
        var gs = connectorsModel.google_status
        if (gs.connected) return "secondary"
        return "primary"
    }

    function gPrimaryLabel() {
        if (!connectorsModel || !connectorsModel.google_status) return ""
        var gs = connectorsModel.google_status
        if (connectorsModel.google_busy) return gs.connected ? "Disconnecting…" : (gs.configured ? "Authorizing…" : "Importing…")
        if (gs.connected) return "Disconnect"
        if (gs.configured) return "Connect"
        return "Set up"
    }

    function obsAvail() {
        return connectorsModel && connectorsModel.obsidian_status && connectorsModel.obsidian_status.available
    }

    function obsMetaLine() {
        if (!connectorsModel || !connectorsModel.obsidian_status) return ""
        var obs = connectorsModel.obsidian_status
        if (obs.available) return obs.vault
        return "No vault — choose one (any folder with a .obsidian)"
    }

    function revAvail() {
        return connectorsModel && connectorsModel.revuto_status && connectorsModel.revuto_status.available
    }

    function revBadge() {
        if (!connectorsModel || !connectorsModel.revuto_status) return ""
        var rev = connectorsModel.revuto_status
        if (rev.available) {
            var badge = rev.count + " repo" + (rev.count === 1 ? "" : "s")
            if (rev.paused > 0) badge += " · " + rev.paused + " paused"
            return badge
        }
        return "CLI not found"
    }

    function revColor() {
        if (!connectorsModel || !connectorsModel.revuto_status) return Theme.colors.textFaint
        return connectorsModel.revuto_status.available ? Theme.colors.brandBright : Theme.colors.textFaint
    }

    function revMetaLine() {
        if (!connectorsModel || !connectorsModel.revuto_status) return ""
        if (!connectorsModel.revuto_status.available) return "Install the `revuto` CLI to control it from here."
        return ""
    }

    function isRevBusy(repo) {
        return connectorsModel && connectorsModel.revuto_busy && connectorsModel.revuto_busy[repo]
    }

    function connStatusBadge(connector) {
        if (!connector) return ""
        if (isConnecting(connector.name)) return "authorizing…"
        if (connector.connected) return "connected"
        return "not connected"
    }

    function connStatusColor(connector) {
        if (!connector) return Theme.colors.textFaint
        if (connector.connected) return Theme.colors.brandBright
        return Theme.colors.textFaint
    }

    function connPrimaryLabel(connector) {
        if (!connector) return ""
        if (isBusy(connector.name)) return connector.connected ? "Disconnecting…" : "Connecting…"
        if (isConnecting(connector.name)) return "Authorizing…"
        if (connector.connected) return "Disconnect"
        return "Connect"
    }

    function isBusy(name) {
        return connectorsModel && connectorsModel.busy && connectorsModel.busy[name]
    }

    function isConnecting(name) {
        return connectorsModel && connectorsModel.connecting && connectorsModel.connecting[name]
    }

    function isConfirmRemoveConnector(name) {
        return connectorsModel && connectorsModel.confirm_remove_connector && connectorsModel.confirm_remove_connector[name]
    }

    function isConfirmRemoveServer(name) {
        return connectorsModel && connectorsModel.confirm_remove_server && connectorsModel.confirm_remove_server[name]
    }

    function stdioServers() {
        if (!connectorsModel || !connectorsModel.servers || !connectorsModel.servers.servers) return []
        return connectorsModel.servers.servers.filter(function(s) { return !s.remote })
    }
}
