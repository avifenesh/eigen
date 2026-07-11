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
    objectName: "connectorsView"
    color: Theme.colors.bgBase

    property var connectorsModel: null  // ConnectorsModel from Python
    property var googleDialogFocusTarget: null
    readonly property bool qaGoogleClientDialogOpen: googleClientDialog.visible
    readonly property bool qaVaultDialogOpen: vaultDialog.visible

    Flickable {
        id: connectorsFlick
        objectName: "connectorsFlick"
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
                visible: !!(connectorsModel && connectorsModel.loading && !connectorsLoaded() && !root.hasInitialLoadError())
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
                objectName: "connectorsLoadError"
                visible: root.hasInitialLoadError()
                Layout.fillWidth: true
                Layout.preferredHeight: 120
                color: Theme.colors.bgRaised
                border.width: 1
                border.color: Theme.colors.borderHairline
                radius: Theme.radius.md

                ColumnLayout {
                    anchors.centerIn: parent
                    spacing: Theme.space.md

                    Rectangle {
                        implicitWidth: 48
                        implicitHeight: 30
                        radius: Theme.radius.sm
                        color: Theme.colors.bgInset
                        border.width: 1
                        border.color: Theme.colors.borderHairline
                        Layout.alignment: Qt.AlignHCenter

                        Label {
                            anchors.centerIn: parent
                            text: "MCP"
                            font.pixelSize: Theme.fontSize.label
                            font.weight: Theme.fontWeight.bold
                            color: Theme.colors.textMuted
                        }
                    }

                    Label {
                        text: "Couldn't load connectors"
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                        Layout.alignment: Qt.AlignHCenter
                    }

                    Label {
                        objectName: "connectorsLoadErrorText"
                        text: connectorsModel ? connectorsModel.load_error : ""
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.textMuted
                        Layout.alignment: Qt.AlignHCenter
                    }

                    AppButton {
                        objectName: "connectorsLoadErrorRetry"
                        text: "Retry"
                        onClicked: connectorsModel.load()
                        Layout.alignment: Qt.AlignHCenter
                    }
                }
            }

            Rectangle {
                objectName: "connectorsRefreshErrorBanner"
                visible: root.hasRefreshLoadError()
                Layout.fillWidth: true
                Layout.preferredHeight: visible ? Math.max(40, connectorsRefreshErrorRow.implicitHeight + Theme.space.md) : 0
                color: Theme.colors.errorBg
                border.width: visible ? 1 : 0
                border.color: Theme.colors.error
                radius: Theme.radius.sm
                clip: true

                RowLayout {
                    id: connectorsRefreshErrorRow
                    anchors.fill: parent
                    anchors.leftMargin: Theme.space.md
                    anchors.rightMargin: Theme.space.md
                    spacing: Theme.space.md

                    Label {
                        objectName: "connectorsRefreshErrorText"
                        text: connectorsModel ? connectorsModel.load_error : ""
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.error
                        wrapMode: Text.Wrap
                        Layout.fillWidth: true
                    }

                    AppButton {
                        objectName: "connectorsRefreshErrorRetry"
                        text: "Retry"
                        compact: true
                        toolTipText: "Retry loading connectors"
                        Layout.preferredWidth: 72
                        onClicked: {
                            if (connectorsModel) connectorsModel.load()
                        }
                    }
                }
            }

            // Content (when loaded)
            ColumnLayout {
                visible: connectorsLoaded()
                Layout.fillWidth: true
                spacing: Theme.space.lg

                // Google built-in
                ConnectorCard {
                    visible: !!(connectorsModel && connectorsModel.google_status)
                    Layout.fillWidth: true
                    actionKey: "google"
                    glyph: "◷"
                    connectorName: "Google"
                    description: "Calendar + Gmail — read events & email, create events."
                    statusBadge: gStatusBadge()
                    statusColor: gStatusColor()
                    metaLine: gMetaLine()
                    busy: connectorsModel ? connectorsModel.google_busy : false
                    primaryAction: gPrimaryAction()
                    primaryLabel: gPrimaryLabel()
                    onPrimaryClicked: function(source) {
                        if (!connectorsModel || !connectorsModel.google_status) return
                        var gs = connectorsModel.google_status
                        if (gs.connected) connectorsModel.disconnect_google()
                        else if (gs.configured) connectorsModel.connect_google()
                        else {
                            root.googleDialogFocusTarget = source
                            if (gs.setupUrl) Qt.openUrlExternally(gs.setupUrl)
                            googleClientDialog.open()
                        }
                    }
                }

                // Obsidian built-in
                ConnectorCard {
                    visible: !!(connectorsModel && connectorsModel.obsidian_status)
                    Layout.fillWidth: true
                    actionKey: "obsidian"
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
                    visible: !!(connectorsModel && connectorsModel.revuto_status)
                    Layout.fillWidth: true
                    actionKey: "revuto"
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

                                    AppButton {
                                        objectName: reviewer.repo ? "connectorRevutoReviewButton_" + reviewer.repo : ""
                                        text: "Review now"
                                        enabled: !isRevBusy(reviewer.repo)
                                        onClicked: connectorsModel.revuto_trigger(reviewer.repo)
                                    }

                                    AppButton {
                                        objectName: reviewer.repo ? "connectorRevutoPauseButton_" + reviewer.repo : ""
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

                    AppButton {
                        objectName: "connectorsAddButton"
                        visible: connectorsModel && !connectorsModel.add_open
                        text: "+ Add connector"
                        toolTipText: "Add remote connector"
                        onClicked: connectorsModel.add_open = true
                        Layout.preferredWidth: 136
                    }
                }

                Rectangle {
                    objectName: "connectorsActionError"
                    visible: connectorsModel && connectorsModel.action_error !== "" && !connectorsModel.srv_open
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? Math.max(36, connectorsActionErrorRow.implicitHeight + Theme.space.md) : 0
                    color: Theme.colors.errorBg
                    border.width: visible ? 1 : 0
                    border.color: Theme.colors.error
                    radius: Theme.radius.sm
                    clip: true

                    RowLayout {
                        id: connectorsActionErrorRow
                        anchors.fill: parent
                        anchors.leftMargin: Theme.space.md
                        anchors.rightMargin: Theme.space.md
                        spacing: Theme.space.md

                        Label {
                            text: connectorsModel ? connectorsModel.action_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.error
                            wrapMode: Text.Wrap
                            Layout.fillWidth: true
                        }

                        AppButton {
                            objectName: "connectorsDismissActionError"
                            text: "X"
                            compact: true
                            toolTipText: "Dismiss connector error"
                            Layout.preferredWidth: 28
                            Layout.minimumWidth: 28
                            Layout.preferredHeight: 28
                            onClicked: {
                                if (connectorsModel) connectorsModel.clear_action_error()
                            }
                        }
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
                    onVisibleChanged: {
                        if (visible) {
                            Qt.callLater(function() {
                                if (connectorsModel && connectorsModel.add_open && addNameField.visible && addNameField.enabled) {
                                    addNameField.forceActiveFocus(Qt.TabFocusReason)
                                }
                            })
                        }
                    }

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

                            AppTextField {
                                id: addNameField
                                objectName: "connectorsAddNameInput"
                                Layout.fillWidth: true
                                backgroundColor: Theme.colors.bgRaised2
                                placeholderText: "notion"
                                text: connectorsModel ? connectorsModel.add_name : ""
                                enabled: connectorsModel && !connectorsModel.adding
                                onTextChanged: if (connectorsModel) connectorsModel.add_name = text
                                font.family: Theme.monoFonts[0]
                                Keys.onPressed: function(event) {
                                    root.handleAddConnectorKey(event)
                                }
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

                            AppTextField {
                                id: addUrlField
                                objectName: "connectorsAddUrlInput"
                                Layout.fillWidth: true
                                backgroundColor: Theme.colors.bgRaised2
                                placeholderText: "https://mcp.notion.com/mcp"
                                text: connectorsModel ? connectorsModel.add_url : ""
                                enabled: connectorsModel && !connectorsModel.adding
                                onTextChanged: if (connectorsModel) connectorsModel.add_url = text
                                font.family: Theme.monoFonts[0]
                                Keys.onPressed: function(event) {
                                    root.handleAddConnectorKey(event)
                                }
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

                            AppTextField {
                                id: addDescField
                                objectName: "connectorsAddDescInput"
                                Layout.fillWidth: true
                                backgroundColor: Theme.colors.bgRaised2
                                placeholderText: "Notion workspace (optional)"
                                text: connectorsModel ? connectorsModel.add_desc : ""
                                enabled: connectorsModel && !connectorsModel.adding
                                onTextChanged: if (connectorsModel) connectorsModel.add_desc = text
                                Keys.onPressed: function(event) {
                                    root.handleAddConnectorKey(event)
                                }
                            }
                        }

                        // Actions
                        RowLayout {
                            Layout.topMargin: Theme.space.sm
                            spacing: Theme.space.sm

                            AppButton {
                                objectName: "connectorsAddAuthorizeButton"
                                text: connectorsModel && connectorsModel.adding ? "Saving…" : "Add & authorize"
                                toolTipText: "Add and authorize connector"
                                enabled: connectorsModel && !connectorsModel.adding && root.canSubmitAddConnector()
                                onClicked: root.submitAddConnector()
                            }

                            AppButton {
                                objectName: "connectorsAddCancelButton"
                                text: "Cancel"
                                toolTipText: "Cancel connector add"
                                enabled: connectorsModel && !connectorsModel.adding
                                onClicked: root.cancelAddConnector()
                            }
                        }
                    }
                }

                // Connector list
                Repeater {
                    model: remoteConnectors()
                    delegate: ConnectorCard {
                        Layout.fillWidth: true
                        property var connector: modelData
                        actionKey: "connector_" + connector.name
                        glyph: (connector.glyph || "")
                        connectorName: (connector.display || connector.name)
                        description: (connector.description || "")
                        statusBadge: connStatusBadge(connector)
                        statusColor: connStatusColor(connector)
                        metaLine: connector.url
                        busy: isBusy(connector.name) || connectorIsConnecting(connector.name)
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
                    visible: connectorsLoaded() && remoteConnectors().length === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: 120
                    color: Theme.colors.bgRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline
                    radius: Theme.radius.md

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.sm

                        Rectangle {
                            implicitWidth: 48
                            implicitHeight: 30
                            radius: Theme.radius.sm
                            color: Theme.colors.bgInset
                            border.width: 1
                            border.color: Theme.colors.borderHairline
                            Layout.alignment: Qt.AlignHCenter

                            Label {
                                anchors.centerIn: parent
                                text: "MCP"
                                font.pixelSize: Theme.fontSize.label
                                font.weight: Theme.fontWeight.bold
                                color: Theme.colors.textMuted
                            }
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
                    visible: connectorDirectory().length > 0
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
                            model: connectorDirectory()
                            delegate: CatalogTile {
                                property var entry: modelData
                                width: 160
                                entryName: (entry.name || "")
                                glyph: (entry.glyph || "")
                                displayName: (entry.display || "")
                                category: (entry.category || "")
                                isAdded: entry.added || false
                                isConnecting: connectorIsConnecting(entry.name)
                                onClicked: {
                                    if (!entry.added && !connectorIsConnecting(entry.name)) {
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

                        AppButton {
                            objectName: "connectorsAddServerButton"
                            visible: connectorsModel && !connectorsModel.srv_open
                            text: "+ Add MCP server"
                            toolTipText: "Add local MCP server"
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
                        onVisibleChanged: {
                            if (visible) {
                                Qt.callLater(function() {
                                    if (connectorsModel && connectorsModel.srv_open && serverNameField.visible && serverNameField.enabled) {
                                        serverNameField.forceActiveFocus(Qt.TabFocusReason)
                                    }
                                })
                            }
                        }

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

                                AppTextField {
                                    id: serverNameField
                                    objectName: "connectorsServerNameInput"
                                    Layout.fillWidth: true
                                    Layout.minimumWidth: 220
                                    backgroundColor: Theme.colors.bgRaised2
                                    placeholderText: "github"
                                    text: connectorsModel ? connectorsModel.srv_name : ""
                                    enabled: connectorsModel && !connectorsModel.srv_saving
                                    onTextChanged: if (connectorsModel) connectorsModel.srv_name = text
                                    font.family: Theme.monoFonts[0]
                                    Keys.onPressed: function(event) {
                                        root.handleServerLineKey(event)
                                    }
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

                                AppTextField {
                                    id: serverCommandField
                                    objectName: "connectorsServerCommandInput"
                                    Layout.fillWidth: true
                                    Layout.minimumWidth: 220
                                    backgroundColor: Theme.colors.bgRaised2
                                    placeholderText: "docker run ghcr.io/github/github-mcp-server"
                                    text: connectorsModel ? connectorsModel.srv_command : ""
                                    enabled: connectorsModel && !connectorsModel.srv_saving
                                    onTextChanged: if (connectorsModel) connectorsModel.srv_command = text
                                    font.family: Theme.monoFonts[0]
                                    Keys.onPressed: function(event) {
                                        root.handleServerLineKey(event)
                                    }
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

                                AppTextField {
                                    id: serverDescField
                                    objectName: "connectorsServerDescInput"
                                    Layout.fillWidth: true
                                    Layout.minimumWidth: 220
                                    backgroundColor: Theme.colors.bgRaised2
                                    placeholderText: "GitHub MCP (optional)"
                                    text: connectorsModel ? connectorsModel.srv_desc : ""
                                    enabled: connectorsModel && !connectorsModel.srv_saving
                                    onTextChanged: if (connectorsModel) connectorsModel.srv_desc = text
                                    Keys.onPressed: function(event) {
                                        root.handleServerLineKey(event)
                                    }
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

                                AppTextArea {
                                    id: serverEnvArea
                                    objectName: "connectorsServerEnvInput"
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: 60
                                    placeholderText: "LOG_LEVEL=info"
                                    text: connectorsModel ? connectorsModel.srv_env : ""
                                    enabled: connectorsModel && !connectorsModel.srv_saving
                                    onTextChanged: if (connectorsModel) connectorsModel.srv_env = text
                                    font.family: Theme.monoFonts[0]
                                    backgroundColor: Theme.colors.bgRaised2
                                    backgroundRadius: Theme.radius.sm
                                    Keys.onPressed: function(event) {
                                        root.handleServerTextAreaKey(event)
                                    }
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

                                AppTextArea {
                                    id: serverSecretArea
                                    objectName: "connectorsServerSecretInput"
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: 60
                                    placeholderText: "GITHUB_PERSONAL_ACCESS_TOKEN=ghp_…"
                                    text: connectorsModel ? connectorsModel.srv_secret : ""
                                    enabled: connectorsModel && !connectorsModel.srv_saving
                                    onTextChanged: if (connectorsModel) connectorsModel.srv_secret = text
                                    font.family: Theme.monoFonts[0]
                                    backgroundColor: Theme.colors.bgRaised2
                                    backgroundRadius: Theme.radius.sm
                                    Keys.onPressed: function(event) {
                                        root.handleServerTextAreaKey(event)
                                    }
                                }
                            }

                            Rectangle {
                                objectName: "connectorsServerActionError"
                                visible: connectorsModel && connectorsModel.action_error !== ""
                                Layout.fillWidth: true
                                Layout.preferredHeight: visible ? Math.max(36, serverActionErrorRow.implicitHeight + Theme.space.md) : 0
                                color: Theme.colors.errorBg
                                border.width: visible ? 1 : 0
                                border.color: Theme.colors.error
                                radius: Theme.radius.sm
                                clip: true

                                RowLayout {
                                    id: serverActionErrorRow
                                    anchors.fill: parent
                                    anchors.leftMargin: Theme.space.md
                                    anchors.rightMargin: Theme.space.md
                                    spacing: Theme.space.md

                                    Label {
                                        text: connectorsModel ? connectorsModel.action_error : ""
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.label
                                        color: Theme.colors.error
                                        wrapMode: Text.Wrap
                                        Layout.fillWidth: true
                                    }

                                    AppButton {
                                        objectName: "connectorsDismissServerActionError"
                                        text: "X"
                                        compact: true
                                        toolTipText: "Dismiss connector error"
                                        Layout.preferredWidth: 28
                                        Layout.minimumWidth: 28
                                        Layout.preferredHeight: 28
                                        onClicked: {
                                            if (connectorsModel) connectorsModel.clear_action_error()
                                        }
                                    }
                                }
                            }

                            // Actions
                            RowLayout {
                                Layout.topMargin: Theme.space.sm
                                spacing: Theme.space.sm

                                AppButton {
                                    objectName: "connectorsSaveServerButton"
                                    text: connectorsModel && connectorsModel.srv_saving ? "Saving…" : "Save server"
                                    toolTipText: "Save local MCP server"
                                    enabled: connectorsModel && !connectorsModel.srv_saving && root.canSubmitLocalServer()
                                    onClicked: root.submitLocalServer()
                                }

                                AppButton {
                                    objectName: "connectorsCancelServerButton"
                                    text: "Cancel"
                                    toolTipText: "Cancel MCP server add"
                                    enabled: connectorsModel && !connectorsModel.srv_saving
                                    onClicked: root.cancelLocalServer()
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
                            actionKey: "server_" + server.name
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

    // Qt owns file/directory selection; guiserver receives plain paths.
    FolderDialog {
        id: vaultDialog
        objectName: "connectorsVaultDialog"
        title: "Choose Obsidian Vault"
        currentFolder: "file:///home"
        onAccepted: {
            if (connectorsModel) {
                var path = decodeURIComponent(selectedFolder.toString().replace(/^file:\/\//, ""))
                connectorsModel.choose_vault(path)
            }
        }
    }

    FileDialog {
        id: googleClientDialog
        objectName: "connectorsGoogleClientDialog"
        title: "Choose Google OAuth client JSON"
        nameFilters: ["JSON files (*.json)"]
        fileMode: FileDialog.OpenFile
        onAccepted: {
            if (connectorsModel) {
                var path = decodeURIComponent(selectedFile.toString().replace(/^file:\/\//, ""))
                connectorsModel.setup_google(path)
            }
        }
        onVisibleChanged: {
            if (!visible && root.googleDialogFocusTarget) {
                root.googleDialogFocusTarget.forceActiveFocus(Qt.PopupFocusReason)
                root.googleDialogFocusTarget = null
            }
        }
    }

    // Inline component: ConnectorCard
    component ConnectorCard: Rectangle {
        id: card
        objectName: actionKey ? ("connectorCard_" + actionKey) : ""
        color: Theme.colors.bgRaised
        border.width: 1
        border.color: Theme.colors.borderSubtle
        radius: Theme.radius.md
        implicitHeight: cardColumn.implicitHeight + Theme.space.xl * 2

        property string glyph: ""
        property string connectorName: ""
        property string actionKey: ""
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
        readonly property bool actionsInline: width >= 500
        readonly property string qaIconText: iconText()
        default property alias extraContent: cardExtra.data

        signal primaryClicked(var source)
        signal removeClicked()
        signal removeCancelled()

        function iconText() {
            var source = (connectorName || glyph || "").trim()
            if (!source) return ""
            var parts = source.split(/[\s._-]+/).filter(function(part) { return part.length > 0 })
            if (parts.length === 0) return source.charAt(0).toUpperCase()
            var initials = parts[0].charAt(0)
            if (parts.length > 1) initials += parts[1].charAt(0)
            return initials.toUpperCase()
        }

        ColumnLayout {
            id: cardColumn
            anchors.fill: parent
            anchors.margins: Theme.space.xl
            spacing: Theme.space.lg

            RowLayout {
                id: cardContent
                Layout.fillWidth: true
                spacing: Theme.space.xl

                // Info column
                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Theme.space.sm

                    // Name + badges row
                    RowLayout {
                        spacing: Theme.space.md

                        Rectangle {
                            visible: card.qaIconText !== ""
                            Layout.preferredWidth: 24
                            Layout.preferredHeight: 24
                            radius: 7
                            color: Theme.colors.bgInset
                            border.width: 1
                            border.color: Theme.colors.borderSubtle

                            Label {
                                anchors.centerIn: parent
                                text: card.qaIconText
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.micro
                                font.weight: Theme.fontWeight.semibold
                                color: Theme.colors.brandBright
                            }
                        }

                        Label {
                            text: connectorName
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                        }

                        AppTag {
                            visible: statusBadge
                            text: statusBadge
                            backgroundColor: Qt.rgba(statusColor.r, statusColor.g, statusColor.b, 0.1)
                            borderColor: Qt.rgba(statusColor.r, statusColor.g, statusColor.b, 0.2)
                            textColor: statusColor
                            fontPixelSize: Theme.fontSize.micro
                            fontWeight: Theme.fontWeight.semibold
                            capitalization: Font.AllUppercase
                            minimumHeight: 18
                        }

                        AppTag {
                            visible: secretBadge
                            text: secretBadge
                            backgroundColor: Theme.colors.bgRaised2
                            borderColor: Theme.colors.borderBrandFaint
                            textColor: Theme.colors.textMuted
                            fontPixelSize: Theme.fontSize.micro
                            fontWeight: Theme.fontWeight.semibold
                            minimumHeight: 18
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

                // Inline actions for roomy cards.
                RowLayout {
                    id: inlineActions
                    visible: card.actionsInline
                    spacing: Theme.space.sm

                    AppButton {
                        objectName: visible && actionKey ? ("connectorPrimaryButton_" + actionKey) : ""
                        visible: primaryAction && primaryLabel
                        text: primaryLabel
                        toolTipText: primaryLabel && connectorName ? (primaryLabel + " " + connectorName) : primaryLabel
                        enabled: !busy
                        onClicked: card.primaryClicked(this)
                    }

                    AppButton {
                        objectName: visible && actionKey ? ("connectorRemoveButton_" + actionKey) : ""
                        visible: secondaryActions && !showRemoveConfirm
                        text: "Remove"
                        toolTipText: connectorName ? ("Remove " + connectorName) : "Remove connector"
                        enabled: !busy
                        onClicked: card.removeClicked()
                    }

                    AppButton {
                        objectName: visible && actionKey ? ("connectorConfirmRemoveButton_" + actionKey) : ""
                        visible: secondaryActions && showRemoveConfirm
                        text: "Confirm"
                        toolTipText: connectorName ? ("Confirm removing " + connectorName) : "Confirm remove"
                        enabled: !busy
                        onClicked: card.removeClicked()
                    }

                    AppButton {
                        objectName: visible && actionKey ? ("connectorCancelRemoveButton_" + actionKey) : ""
                        visible: secondaryActions && showRemoveConfirm
                        text: "Cancel"
                        toolTipText: "Cancel removal"
                        enabled: !busy
                        onClicked: card.removeCancelled()
                    }
                }
            }

            RowLayout {
                id: stackedActions
                visible: !card.actionsInline && (primaryAction || secondaryActions)
                Layout.fillWidth: true
                Layout.alignment: Qt.AlignRight
                spacing: Theme.space.sm

                Item { Layout.fillWidth: true }

                AppButton {
                    objectName: visible && actionKey ? ("connectorPrimaryButton_" + actionKey) : ""
                    visible: primaryAction && primaryLabel
                    text: primaryLabel
                    toolTipText: primaryLabel && connectorName ? (primaryLabel + " " + connectorName) : primaryLabel
                    enabled: !busy
                    onClicked: card.primaryClicked(this)
                }

                AppButton {
                    objectName: visible && actionKey ? ("connectorRemoveButton_" + actionKey) : ""
                    visible: secondaryActions && !showRemoveConfirm
                    text: "Remove"
                    toolTipText: connectorName ? ("Remove " + connectorName) : "Remove connector"
                    enabled: !busy
                    onClicked: card.removeClicked()
                }

                AppButton {
                    objectName: visible && actionKey ? ("connectorConfirmRemoveButton_" + actionKey) : ""
                    visible: secondaryActions && showRemoveConfirm
                    text: "Confirm"
                    toolTipText: connectorName ? ("Confirm removing " + connectorName) : "Confirm remove"
                    enabled: !busy
                    onClicked: card.removeClicked()
                }

                AppButton {
                    objectName: visible && actionKey ? ("connectorCancelRemoveButton_" + actionKey) : ""
                    visible: secondaryActions && showRemoveConfirm
                    text: "Cancel"
                    toolTipText: "Cancel removal"
                    enabled: !busy
                    onClicked: card.removeCancelled()
                }
            }

            ColumnLayout {
                id: cardExtra
                Layout.fillWidth: true
                spacing: Theme.space.sm
            }
        }
    }

    // Inline component: CatalogTile
    component CatalogTile: Rectangle {
        id: tile
        objectName: entryName ? ("catalogTile_" + entryName) : ""
        implicitHeight: tileContent.implicitHeight + Theme.space.lg * 2
        color: {
            if (tile.isInteractive && tile.activeFocus) {
                return Theme.colors.stateFocusBg
            }
            return tileMouseArea.containsMouse ? Theme.colors.stateHover : Theme.colors.bgRaised2
        }
        border.width: tile.activeFocus ? 2 : 1
        border.color: tile.activeFocus ? Theme.colors.brandBright : Theme.colors.borderSubtle
        radius: Theme.radius.md
        opacity: isAdded ? 0.55 : 1.0
        activeFocusOnTab: true
        focusPolicy: Qt.StrongFocus

        property string entryName: ""
        property string glyph: ""
        property string displayName: ""
        property string category: ""
        property bool isAdded: false
        property bool isConnecting: false
        property bool qaForceKeyboardFocus: false
        readonly property bool qaVisualFocus: activeFocus
        readonly property string qaAccessibleName: accessibleName()
        readonly property string qaIconText: iconText()
        readonly property bool isInteractive: !isAdded && !isConnecting

        signal clicked()

        Accessible.role: Accessible.Button
        Accessible.name: tile.accessibleName()
        Accessible.description: isAdded
            ? (displayName + " is already added")
            : (isConnecting ? ("Authorizing " + displayName) : ("Connect " + displayName + " connector"))
        Accessible.onPressAction: tile.activateTile()

        Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
        Behavior on border.color { ColorAnimation { duration: Theme.duration.fast } }

        function activateTile() {
            if (tile.isInteractive) {
                tile.clicked()
            }
        }

        onQaForceKeyboardFocusChanged: {
            if (qaForceKeyboardFocus && isInteractive) {
                forceActiveFocus(Qt.TabFocusReason)
            }
        }

        Keys.onReturnPressed: activateTile()
        Keys.onEnterPressed: activateTile()
        Keys.onSpacePressed: activateTile()

        function accessibleName() {
            if (!displayName) return ""
            if (isAdded) return displayName + " connector added"
            if (isConnecting) return displayName + " connector authorizing"
            return "Connect " + displayName
        }

        function iconText() {
            var source = (displayName || entryName || "").trim()
            if (!source) return ""
            var parts = source.split(/[\s._-]+/).filter(function(part) { return part.length > 0 })
            if (parts.length === 0) return source.charAt(0).toUpperCase()
            var initials = parts[0].charAt(0)
            if (parts.length > 1) initials += parts[1].charAt(0)
            return initials.toUpperCase()
        }

        MouseArea {
            id: tileMouseArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: tile.isInteractive ? Qt.PointingHandCursor : Qt.ArrowCursor
            enabled: tile.isInteractive
            onClicked: tile.activateTile()
        }

        ColumnLayout {
            id: tileContent
            anchors.fill: parent
            anchors.margins: Theme.space.lg
            spacing: Theme.space.xs

            Rectangle {
                Layout.preferredWidth: 28
                Layout.preferredHeight: 28
                Layout.alignment: Qt.AlignLeft
                radius: 8
                color: tile.isAdded ? Theme.colors.bgInset : Theme.colors.stateSelected
                border.width: 1
                border.color: tile.isAdded ? Theme.colors.borderSubtle : Theme.colors.borderBrandFaint

                Label {
                    anchors.centerIn: parent
                    text: tile.qaIconText
                    font.family: Theme.monoFonts[0]
                    font.pixelSize: Theme.fontSize.label
                    font.weight: Theme.fontWeight.semibold
                    color: tile.isAdded ? Theme.colors.textFaint : Theme.colors.brandBright
                }
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
        if (connectorIsConnecting(connector.name)) return "authorizing…"
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
        if (connectorIsConnecting(connector.name)) return "Authorizing…"
        if (connector.connected) return "Disconnect"
        return "Connect"
    }

    function isBusy(name) {
        return !!(connectorsModel && connectorsModel.busy && connectorsModel.busy[name])
    }

    function connectorIsConnecting(name) {
        return !!(connectorsModel && connectorsModel.connecting && connectorsModel.connecting[name])
    }

    function isConfirmRemoveConnector(name) {
        return !!(connectorsModel && connectorsModel.confirm_remove_connector && connectorsModel.confirm_remove_connector[name])
    }

    function isConfirmRemoveServer(name) {
        return !!(connectorsModel && connectorsModel.confirm_remove_server && connectorsModel.confirm_remove_server[name])
    }

    function connectorsLoaded() {
        return !!(connectorsModel && connectorsModel.connectors)
    }

    function hasInitialLoadError() {
        return !!(connectorsModel && connectorsModel.load_error && !connectorsLoaded())
    }

    function hasRefreshLoadError() {
        return !!(connectorsModel && connectorsModel.load_error && connectorsLoaded())
    }

    function remoteConnectors() {
        if (!connectorsLoaded() || !connectorsModel.connectors.connectors) return []
        return connectorsModel.connectors.connectors
    }

    function connectorDirectory() {
        if (!connectorsLoaded() || !connectorsModel.connectors.directory) return []
        return connectorsModel.connectors.directory
    }

    function submitAddConnector() {
        if (!connectorsModel || connectorsModel.adding) return
        if (canSubmitAddConnector()) {
            connectorsModel.add_connector(connectorsModel.add_name.trim())
        }
    }

    function canSubmitAddConnector() {
        if (!connectorsModel) return false
        return connectorsModel.add_name.trim().length > 0
            && connectorsModel.add_url.trim().length > 0
    }

    function cancelAddConnector() {
        if (connectorsModel && !connectorsModel.adding) {
            connectorsModel.cancel_add_connector()
        }
    }

    function submitLocalServer() {
        if (connectorsModel && !connectorsModel.srv_saving && canSubmitLocalServer()) {
            connectorsModel.save_local_server()
        }
    }

    function canSubmitLocalServer() {
        if (!connectorsModel) return false
        return connectorsModel.srv_name.trim().length > 0
            && connectorsModel.srv_command.trim().length > 0
    }

    function cancelLocalServer() {
        if (connectorsModel && !connectorsModel.srv_saving) {
            connectorsModel.cancel_local_server()
        }
    }

    function handleAddConnectorKey(event) {
        if (event.key === Qt.Key_Escape) {
            root.cancelAddConnector()
            event.accepted = true
            return
        }
        if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
            root.submitAddConnector()
            event.accepted = true
        }
    }

    function handleServerLineKey(event) {
        if (event.key === Qt.Key_Escape) {
            root.cancelLocalServer()
            event.accepted = true
            return
        }
        if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
            root.submitLocalServer()
            event.accepted = true
        }
    }

    function handleServerTextAreaKey(event) {
        if (event.key === Qt.Key_Escape) {
            root.cancelLocalServer()
            event.accepted = true
            return
        }
        if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter)
                && (event.modifiers & Qt.ControlModifier)) {
            root.submitLocalServer()
            event.accepted = true
        }
    }

    function stdioServers() {
        if (!connectorsModel || !connectorsModel.servers || !connectorsModel.servers.servers) return []
        return connectorsModel.servers.servers.filter(function(s) { return !s.remote })
    }
}
