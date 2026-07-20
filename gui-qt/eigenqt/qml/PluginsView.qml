import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

Rectangle {
    id: root
    objectName: "pluginsView"
    color: Theme.colors.bgBase

    property var pluginsModel: null
    property string confirmingKey: ""
    property string expandedScanName: ""
    readonly property var plugins: pluginsModel ? pluginsModel.plugins || [] : []
    readonly property var marketplaces: pluginsModel ? pluginsModel.marketplaces || [] : []
    readonly property var previews: pluginsModel ? pluginsModel.previews || [] : []
    readonly property int qaPluginCount: pluginsModel ? pluginsModel.plugin_count : 0
    readonly property int qaMarketplaceCount: pluginsModel ? pluginsModel.marketplace_count : 0
    readonly property int qaPreviewCount: previews.length
    readonly property bool compact: width < 700

    onPluginsModelChanged: syncActiveModel()
    onVisibleChanged: {
        if (!visible) confirmingKey = ""
        syncActiveModel()
    }
    Component.onCompleted: syncActiveModel()

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 68
            color: Theme.colors.bgWell

            Rectangle {
                anchors.left: parent.left
                anchors.top: parent.top
                anchors.bottom: parent.bottom
                width: 3
                color: Theme.colors.brand
            }

            Rectangle {
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.bottom: parent.bottom
                height: 1
                color: Theme.colors.borderHairline
            }

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xxxl
                anchors.rightMargin: Theme.space.xxxl
                spacing: Theme.space.lg

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: 2

                    Label {
                        objectName: "pluginsTitle"
                        text: "Plugins"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.h2
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        objectName: "pluginsSummary"
                        text: root.summaryText()
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textSecondary
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }

                AppButton {
                    objectName: "pluginsRefreshButton"
                    text: root.pluginsModel && root.pluginsModel.loading ? "Refreshing..." : "Refresh"
                    compact: true
                    variant: "ghost"
                    toolTipText: "Refresh plugins"
                    enabled: root.pluginsModel && !root.pluginsModel.loading && !root.pluginsModel.registry_busy
                    Layout.preferredWidth: Math.max(86, implicitWidth)
                    Layout.preferredHeight: 32
                    onClicked: if (root.pluginsModel) {
                        root.confirmingKey = ""
                        root.pluginsModel.refresh()
                    }
                }
            }
        }

        FeedbackBanner {
            objectName: "pluginsActionError"
            visible: root.pluginsModel && root.pluginsModel.action_error !== ""
            tone: "error"
            message: root.pluginsModel ? root.pluginsModel.action_error : ""
            textObjectName: "pluginsActionErrorText"
            buttonObjectName: "pluginsActionErrorDismissButton"
            onDismissed: if (root.pluginsModel) root.pluginsModel.clear_action_error()
        }

        FeedbackBanner {
            objectName: "pluginsActionMessage"
            visible: root.pluginsModel && root.pluginsModel.action_message !== ""
            tone: "success"
            message: root.pluginsModel ? root.pluginsModel.action_message : ""
            textObjectName: "pluginsActionMessageText"
            buttonObjectName: "pluginsActionMessageDismissButton"
            onDismissed: if (root.pluginsModel) root.pluginsModel.clear_action_message()
        }

        Flickable {
            id: pluginsFlick
            objectName: "pluginsFlick"
            Layout.fillWidth: true
            Layout.fillHeight: true
            contentWidth: width
            contentHeight: contentColumn.implicitHeight + Theme.space.xxl
            clip: true

            ColumnLayout {
                id: contentColumn
                width: Math.min(Math.max(parent.width - Theme.space.xxxl * 2, 0), 1080)
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: Theme.space.xxxl

                Item { Layout.preferredHeight: Theme.space.xxxl }

                Rectangle {
                    id: addPanel
                    objectName: "pluginsAddPanel"
                    Layout.fillWidth: true
                    implicitHeight: addColumn.implicitHeight + Theme.space.xxl * 2
                    radius: Theme.radius.sm
                    color: Theme.colors.bgInset
                    border.width: 1
                    border.color: Theme.colors.borderBrandFaint

                    Rectangle {
                        anchors.left: parent.left
                        anchors.top: parent.top
                        anchors.bottom: parent.bottom
                        width: 4
                        radius: Theme.radius.sm
                        color: Theme.colors.accent
                    }

                    ColumnLayout {
                        id: addColumn
                        anchors.fill: parent
                        anchors.leftMargin: Theme.space.xxxl
                        anchors.rightMargin: Theme.space.xxl
                        anchors.topMargin: Theme.space.xxl
                        anchors.bottomMargin: Theme.space.xxl
                        spacing: Theme.space.lg

                        RowLayout {
                            Layout.fillWidth: true
                            spacing: Theme.space.sm

                            Label {
                                text: "Add plugin source"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.body
                                font.weight: Theme.fontWeight.semibold
                                color: Theme.colors.textPrimary
                                Layout.fillWidth: true
                            }

                            AppTag {
                                text: "scan required"
                                backgroundColor: Theme.colors.surfaceRaised2
                                borderColor: Theme.colors.borderAccentFaint
                                textColor: Theme.colors.accent
                                minimumHeight: 24
                                pill: false
                            }
                        }

                        GridLayout {
                            Layout.fillWidth: true
                            columns: root.compact ? 1 : 2
                            columnSpacing: Theme.space.md
                            rowSpacing: Theme.space.sm

                            AppTextField {
                                id: sourceField
                                objectName: "pluginsAddSourceField"
                                Layout.fillWidth: true
                                Layout.preferredHeight: 38
                                placeholderText: "owner/repo, URL, or local path"
                                text: root.pluginsModel ? root.pluginsModel.marketplace_source : ""
                                enabled: root.pluginsModel && !root.pluginsModel.registry_busy
                                onTextEdited: if (root.pluginsModel) root.pluginsModel.marketplace_source = text
                                Keys.onReturnPressed: {
                                    if (root.pluginsModel && text.trim() !== "" && !root.pluginsModel.registry_busy) {
                                        root.pluginsModel.add_marketplace()
                                    }
                                }
                            }

                            AppButton {
                                objectName: "pluginsAddSourceButton"
                                text: root.pluginsModel && root.pluginsModel.adding_marketplace ? "Adding..." : "Add source"
                                variant: "primary"
                                toolTipText: "Add marketplace source"
                                enabled: root.pluginsModel
                                    && root.pluginsModel.marketplace_source.trim() !== ""
                                    && !root.pluginsModel.registry_busy
                                Layout.fillWidth: root.compact
                                Layout.preferredWidth: root.compact ? -1 : Math.max(112, implicitWidth)
                                Layout.preferredHeight: 38
                                onClicked: if (root.pluginsModel) root.pluginsModel.add_marketplace()
                            }
                        }
                    }
                }

                Rectangle {
                    visible: root.pluginsModel && root.pluginsModel.loading && root.qaPluginCount === 0 && root.qaMarketplaceCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 104 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Label {
                        anchors.centerIn: parent
                        text: "Loading plugins..."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textMuted
                    }
                }

                Rectangle {
                    objectName: "pluginsLoadError"
                    visible: root.pluginsModel && root.pluginsModel.load_error !== "" && root.qaPluginCount === 0 && root.qaMarketplaceCount === 0
                    Layout.fillWidth: true
                    implicitHeight: loadErrorColumn.implicitHeight + Theme.space.xl * 2
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.error

                    ColumnLayout {
                        id: loadErrorColumn
                        anchors.centerIn: parent
                        width: Math.min(parent.width - Theme.space.xl * 2, 520)
                        spacing: Theme.space.md

                        Label {
                            text: "Could not load plugins"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            objectName: "pluginsLoadErrorText"
                            text: root.pluginsModel ? root.pluginsModel.load_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            wrapMode: Text.Wrap
                            horizontalAlignment: Text.AlignHCenter
                            Layout.fillWidth: true
                        }

                        AppButton {
                            objectName: "pluginsLoadErrorRetry"
                            text: "Retry"
                            compact: true
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: if (root.pluginsModel) root.pluginsModel.refresh()
                        }
                    }
                }

                RefreshErrorBanner {
                    objectName: "pluginsRefreshErrorBanner"
                    visible: root.pluginsModel && root.pluginsModel.load_error !== "" && (root.qaPluginCount > 0 || root.qaMarketplaceCount > 0)
                    message: root.pluginsModel ? root.pluginsModel.load_error : ""
                    textObjectName: "pluginsRefreshErrorText"
                    retryObjectName: "pluginsRefreshErrorRetry"
                    retryToolTipText: "Retry loading plugins"
                    onRetry: if (root.pluginsModel) root.pluginsModel.refresh()
                }

                ColumnLayout {
                    visible: root.qaPreviewCount > 0
                    Layout.fillWidth: true
                    spacing: Theme.space.md

                    SectionHeader {
                        title: "Installable from " + (root.pluginsModel ? root.pluginsModel.preview_marketplace : "")
                        count: root.qaPreviewCount
                    }

                    Repeater {
                        model: root.previews
                        delegate: Rectangle {
                            id: previewRow
                            readonly property var preview: modelData || ({})
                            readonly property string previewName: String(preview.name || "")
                            objectName: "pluginsPreviewRow_" + root.safeObjectName(previewName || index)
                            Layout.fillWidth: true
                            implicitHeight: previewGrid.implicitHeight + Theme.space.xxl * 2
                            radius: Theme.radius.sm
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: Theme.colors.borderAccentFaint

                            Rectangle {
                                anchors.left: parent.left
                                anchors.top: parent.top
                                anchors.bottom: parent.bottom
                                width: 3
                                color: Theme.colors.accent
                            }

                            GridLayout {
                                id: previewGrid
                                anchors.fill: parent
                                anchors.leftMargin: Theme.space.xxl
                                anchors.rightMargin: Theme.space.xl
                                anchors.topMargin: Theme.space.xxl
                                anchors.bottomMargin: Theme.space.xxl
                                columns: root.compact ? 1 : 2
                                columnSpacing: Theme.space.lg
                                rowSpacing: Theme.space.md

                                ColumnLayout {
                                    Layout.fillWidth: true
                                    spacing: Theme.space.xs

                                    RowLayout {
                                        Layout.fillWidth: true
                                        spacing: Theme.space.sm

                                        Label {
                                            text: previewName || "plugin"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.body
                                            font.weight: Theme.fontWeight.semibold
                                            color: Theme.colors.textPrimary
                                            elide: Text.ElideRight
                                            Layout.fillWidth: true
                                        }

                                        AppTag {
                                            visible: String(preview.version || "") !== ""
                                            text: preview.version || ""
                                            backgroundColor: Theme.colors.surfaceRaised2
                                            borderColor: Theme.colors.borderAccentFaint
                                            textColor: Theme.colors.accent
                                            fontFamily: Theme.monoFonts[0]
                                            minimumHeight: 21
                                        }
                                    }

                                    Label {
                                        visible: String(preview.description || "") !== ""
                                        text: preview.description || ""
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.label
                                        color: Theme.colors.textMuted
                                        wrapMode: Text.WordWrap
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        text: root.previewComponents(preview)
                                        visible: text !== ""
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textFaint
                                        Layout.fillWidth: true
                                    }
                                }

                                AppButton {
                                    objectName: "pluginsInstallButton_" + root.safeObjectName(previewName)
                                    text: root.pluginsModel && root.pluginsModel.installing_plugin === previewName ? "Installing..." : "Install"
                                    variant: "primary"
                                    toolTipText: "Install " + previewName
                                    enabled: root.pluginsModel && !root.pluginsModel.registry_busy
                                    Layout.fillWidth: root.compact
                                    Layout.preferredWidth: root.compact ? -1 : Math.max(92, implicitWidth)
                                    Layout.preferredHeight: 36
                                    onClicked: if (root.pluginsModel) {
                                        root.pluginsModel.install_plugin(previewName, String(preview.marketplace || ""))
                                    }
                                }
                            }
                        }
                    }
                }

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Theme.space.md

                    SectionHeader {
                        title: "Installed plugins"
                        count: root.qaPluginCount
                    }

                    Label {
                        visible: root.qaPluginCount === 0 && !(root.pluginsModel && root.pluginsModel.loading)
                        text: "No plugins installed."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textMuted
                        Layout.leftMargin: Theme.space.lg
                    }

                    Repeater {
                        model: root.plugins
                        delegate: Rectangle {
                            id: pluginRow
                            readonly property var plugin: modelData || ({})
                            readonly property string pluginName: String(plugin.name || "")
                            readonly property string resourceKey: "plugin:" + pluginName
                            readonly property bool pending: root.pluginsModel
                                ? (root.pluginsModel.pending_actions || []).indexOf(resourceKey) >= 0
                                : false
                            readonly property bool expanded: root.expandedScanName === pluginName
                            readonly property bool qaTextFits: !pluginNameLabel.truncated && !pluginMetaLabel.truncated
                            objectName: "pluginsInstalledRow_" + root.safeObjectName(pluginName || index)
                            Layout.fillWidth: true
                            implicitHeight: pluginColumn.implicitHeight + Theme.space.xxl * 2
                            radius: 0
                            color: Theme.colors.surfaceRaised
                            border.width: 0

                            Rectangle {
                                anchors.left: parent.left
                                anchors.top: parent.top
                                anchors.bottom: parent.bottom
                                width: root.scanStatus(plugin) === "forced" ? 3 : 2
                                color: root.scanStatus(plugin) === "forced"
                                    ? Theme.colors.warn
                                    : (plugin.enabled ? Theme.colors.success : Theme.colors.textGhost)
                            }

                            Rectangle {
                                anchors.left: parent.left
                                anchors.right: parent.right
                                anchors.bottom: parent.bottom
                                height: 1
                                color: Theme.colors.divider
                            }

                            ColumnLayout {
                                id: pluginColumn
                                anchors.fill: parent
                                anchors.leftMargin: Theme.space.xxl
                                anchors.rightMargin: Theme.space.xl
                                anchors.topMargin: Theme.space.xxl
                                anchors.bottomMargin: Theme.space.xxl
                                spacing: Theme.space.lg

                                RowLayout {
                                    Layout.fillWidth: true
                                    spacing: Theme.space.sm

                                    Rectangle {
                                        width: 8
                                        height: 8
                                        radius: 4
                                        color: plugin.enabled ? Theme.colors.dotOk : Theme.colors.dotIdle
                                        Layout.alignment: Qt.AlignVCenter
                                    }

                                    Label {
                                        id: pluginNameLabel
                                        text: pluginName || "plugin"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.body
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.textPrimary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    AppTag {
                                        visible: String(plugin.version || "") !== ""
                                        text: plugin.version || ""
                                        backgroundColor: Theme.colors.surfaceRaised2
                                        borderColor: Theme.colors.borderSubtle
                                        textColor: Theme.colors.textSecondary
                                        fontFamily: Theme.monoFonts[0]
                                        minimumHeight: 21
                                    }

                                    AppButton {
                                        objectName: "pluginsScanButton_" + root.safeObjectName(pluginName)
                                        visible: root.scanStatus(plugin) !== ""
                                        text: root.scanText(plugin)
                                        compact: true
                                        pill: true
                                        selected: pluginRow.expanded
                                        toolTipText: (plugin.scans || []).length > 0 ? "Toggle scan details" : "Plugin scan status"
                                        enabled: (plugin.scans || []).length > 0
                                        onClicked: root.expandedScanName = pluginRow.expanded ? "" : pluginName
                                    }
                                }

                                Label {
                                    id: pluginMetaLabel
                                    text: root.pluginMeta(plugin)
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.micro
                                    color: Theme.colors.textMuted
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }

                                Label {
                                    visible: String(plugin.description || "") !== ""
                                    text: plugin.description || ""
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textMuted
                                    wrapMode: Text.WordWrap
                                    Layout.fillWidth: true
                                }

                                Flow {
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: childrenRect.height
                                    spacing: Theme.space.xs

                                    Repeater {
                                        model: root.components(plugin)
                                        delegate: AppTag {
                                            text: String(modelData.n) + " " + modelData.label
                                            backgroundColor: Theme.colors.surfaceRaised2
                                            borderColor: Theme.colors.borderBrandFaint
                                            textColor: Theme.colors.brandBright
                                            minimumHeight: 24
                                        }
                                    }
                                }

                                Label {
                                    visible: root.warningText(plugin) !== ""
                                    text: root.warningText(plugin)
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.micro
                                    color: root.scanStatus(plugin) === "forced" ? Theme.colors.warn : Theme.colors.textMuted
                                    wrapMode: Text.WordWrap
                                    Layout.fillWidth: true
                                }

                                Rectangle {
                                    objectName: "pluginsScanDetails_" + root.safeObjectName(pluginName)
                                    visible: pluginRow.expanded && (plugin.scans || []).length > 0
                                    Layout.fillWidth: true
                                    implicitHeight: visible ? scanColumn.implicitHeight + Theme.space.lg * 2 : 0
                                    radius: Theme.radius.sm
                                    color: Theme.colors.bgInset
                                    border.width: visible ? 1 : 0
                                    border.color: Theme.colors.warn

                                    ColumnLayout {
                                        id: scanColumn
                                        anchors.fill: parent
                                        anchors.margins: Theme.space.lg
                                        spacing: Theme.space.sm

                                        Label {
                                            text: "Scan details"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.micro
                                            font.weight: Theme.fontWeight.semibold
                                            font.capitalization: Font.AllUppercase
                                            color: Theme.colors.textFaint
                                        }

                                        Repeater {
                                            model: plugin.scans || []
                                            delegate: ColumnLayout {
                                                readonly property var scan: modelData || ({})
                                                Layout.fillWidth: true
                                                spacing: 2

                                                Label {
                                                    text: parent.scan.component || "component"
                                                    font.family: Theme.monoFonts[0]
                                                    font.pixelSize: Theme.fontSize.codeSm
                                                    color: Theme.colors.textSecondary
                                                    wrapMode: Text.WrapAnywhere
                                                    Layout.fillWidth: true
                                                }

                                                Repeater {
                                                    model: parent.scan.reasons || []
                                                    delegate: Label {
                                                        text: "- " + String(modelData)
                                                        font.family: Theme.uiFonts[0]
                                                        font.pixelSize: Theme.fontSize.micro
                                                        color: Theme.colors.warn
                                                        wrapMode: Text.WordWrap
                                                        Layout.fillWidth: true
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }

                                Flow {
                                    id: pluginActions
                                    objectName: "pluginsActions_" + root.safeObjectName(pluginName)
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: childrenRect.height
                                    spacing: Theme.space.sm

                                    Row {
                                        height: 32
                                        spacing: Theme.space.sm

                                        Label {
                                            anchors.verticalCenter: parent.verticalCenter
                                            text: "Enabled"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.label
                                            color: Theme.colors.textMuted
                                        }

                                        AppSwitch {
                                            id: pluginEnableSwitch
                                            objectName: "pluginsEnableSwitch_" + root.safeObjectName(pluginName)
                                            anchors.verticalCenter: parent.verticalCenter
                                            width: 44
                                            height: 24
                                            checkable: false
                                            enabled: root.pluginsModel && !root.pluginsModel.registry_busy
                                            accessibleName: "Enable " + pluginName
                                            toolTipText: (checked ? "Disable " : "Enable ") + pluginName
                                            onClicked: if (root.pluginsModel) root.pluginsModel.set_plugin_enabled(pluginName, !checked)

                                            Binding {
                                                target: pluginEnableSwitch
                                                property: "checked"
                                                value: !!plugin.enabled
                                            }
                                        }
                                    }

                                    Label {
                                        visible: root.confirmingKey === pluginRow.resourceKey
                                        height: 32
                                        verticalAlignment: Text.AlignVCenter
                                        text: root.pluginConsequence(plugin)
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textMuted
                                    }

                                    AppButton {
                                        objectName: "pluginsUninstallConfirm_" + root.safeObjectName(pluginName)
                                        visible: root.confirmingKey === pluginRow.resourceKey
                                        text: pluginRow.pending ? "Removing..." : "Confirm"
                                        variant: "danger"
                                        compact: true
                                        enabled: root.pluginsModel && !root.pluginsModel.registry_busy
                                        onClicked: if (root.pluginsModel) {
                                            root.pluginsModel.remove_plugin(pluginName)
                                            root.confirmingKey = ""
                                        }
                                    }

                                    AppButton {
                                        objectName: "pluginsUninstallButton_" + root.safeObjectName(pluginName)
                                        text: pluginRow.pending
                                            ? "Removing..."
                                            : (root.confirmingKey === pluginRow.resourceKey ? "Cancel" : "Uninstall")
                                        variant: root.confirmingKey === pluginRow.resourceKey ? "secondary" : "ghost"
                                        compact: true
                                        enabled: root.pluginsModel && !root.pluginsModel.registry_busy
                                        onClicked: root.confirmingKey = root.confirmingKey === pluginRow.resourceKey ? "" : pluginRow.resourceKey
                                    }
                                }
                            }
                        }
                    }
                }

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Theme.space.md

                    SectionHeader {
                        title: "Marketplaces"
                        count: root.qaMarketplaceCount
                    }

                    Label {
                        visible: root.qaMarketplaceCount === 0 && !(root.pluginsModel && root.pluginsModel.loading)
                        text: "No marketplaces configured."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textMuted
                        Layout.leftMargin: Theme.space.lg
                    }

                    Repeater {
                        model: root.marketplaces
                        delegate: Rectangle {
                            id: marketRow
                            readonly property var market: modelData || ({})
                            readonly property string marketName: String(market.name || "")
                            readonly property string resourceKey: "market:" + marketName
                            readonly property bool pending: root.pluginsModel
                                ? (root.pluginsModel.pending_actions || []).indexOf(resourceKey) >= 0
                                : false
                            readonly property bool qaTextFits: !marketNameLabel.truncated && !marketSourceLabel.truncated
                            objectName: "pluginsMarketRow_" + root.safeObjectName(marketName || index)
                            Layout.fillWidth: true
                            implicitHeight: marketColumn.implicitHeight + Theme.space.xxl * 2
                            radius: 0
                            color: Theme.colors.surfaceRaised
                            border.width: 0

                            Rectangle {
                                anchors.left: parent.left
                                anchors.top: parent.top
                                anchors.bottom: parent.bottom
                                width: 2
                                color: market.disabled ? Theme.colors.textGhost : Theme.colors.accent
                            }

                            Rectangle {
                                anchors.left: parent.left
                                anchors.right: parent.right
                                anchors.bottom: parent.bottom
                                height: 1
                                color: Theme.colors.divider
                            }

                            ColumnLayout {
                                id: marketColumn
                                anchors.fill: parent
                                anchors.leftMargin: Theme.space.xxl
                                anchors.rightMargin: Theme.space.xl
                                anchors.topMargin: Theme.space.xxl
                                anchors.bottomMargin: Theme.space.xxl
                                spacing: Theme.space.lg

                                RowLayout {
                                    Layout.fillWidth: true
                                    spacing: Theme.space.sm

                                    Rectangle {
                                        width: 8
                                        height: 8
                                        radius: 4
                                        color: market.disabled ? Theme.colors.dotIdle : Theme.colors.dotOk
                                        Layout.alignment: Qt.AlignVCenter
                                    }

                                    Label {
                                        id: marketNameLabel
                                        text: marketName || "marketplace"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.body
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.textPrimary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    AppTag {
                                        text: market.disabled ? "disabled" : "enabled"
                                        backgroundColor: Theme.colors.surfaceRaised2
                                        borderColor: market.disabled ? Theme.colors.borderSubtle : Theme.colors.success
                                        textColor: market.disabled ? Theme.colors.textMuted : Theme.colors.success
                                        minimumHeight: 24
                                    }
                                }

                                Label {
                                    id: marketSourceLabel
                                    text: market.source || ""
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.codeSm
                                    color: Theme.colors.textSecondary
                                    wrapMode: Text.WrapAnywhere
                                    Layout.fillWidth: true
                                }

                                Label {
                                    text: root.marketMeta(market)
                                    visible: text !== ""
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.micro
                                    color: Theme.colors.textMuted
                                    wrapMode: Text.WordWrap
                                    Layout.fillWidth: true
                                }

                                Flow {
                                    id: marketActions
                                    objectName: "pluginsMarketActions_" + root.safeObjectName(marketName)
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: childrenRect.height
                                    spacing: Theme.space.sm

                                    Row {
                                        height: 32
                                        spacing: Theme.space.sm

                                        Label {
                                            anchors.verticalCenter: parent.verticalCenter
                                            text: "Enabled"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.label
                                            color: Theme.colors.textMuted
                                        }

                                        AppSwitch {
                                            id: marketEnableSwitch
                                            objectName: "pluginsMarketEnableSwitch_" + root.safeObjectName(marketName)
                                            anchors.verticalCenter: parent.verticalCenter
                                            width: 44
                                            height: 24
                                            checkable: false
                                            enabled: root.pluginsModel && !root.pluginsModel.registry_busy
                                            accessibleName: "Enable " + marketName
                                            toolTipText: (checked ? "Disable " : "Enable ") + marketName
                                            onClicked: if (root.pluginsModel) root.pluginsModel.set_market_enabled(marketName, !checked)

                                            Binding {
                                                target: marketEnableSwitch
                                                property: "checked"
                                                value: !market.disabled
                                            }
                                        }
                                    }

                                    AppButton {
                                        objectName: "pluginsBrowseButton_" + root.safeObjectName(marketName)
                                        text: root.pluginsModel && root.pluginsModel.browsing_marketplace === marketName ? "Loading..." : "Browse"
                                        compact: true
                                        enabled: root.pluginsModel && !root.pluginsModel.registry_busy
                                        onClicked: if (root.pluginsModel) root.pluginsModel.browse_marketplace(marketName)
                                    }

                                    Label {
                                        visible: root.confirmingKey === marketRow.resourceKey
                                        height: 32
                                        verticalAlignment: Text.AlignVCenter
                                        text: "Remove source?"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textMuted
                                    }

                                    AppButton {
                                        objectName: "pluginsMarketRemoveConfirm_" + root.safeObjectName(marketName)
                                        visible: root.confirmingKey === marketRow.resourceKey
                                        text: marketRow.pending ? "Removing..." : "Confirm"
                                        variant: "danger"
                                        compact: true
                                        enabled: root.pluginsModel && !root.pluginsModel.registry_busy
                                        onClicked: if (root.pluginsModel) {
                                            root.pluginsModel.remove_marketplace(marketName)
                                            root.confirmingKey = ""
                                        }
                                    }

                                    AppButton {
                                        objectName: "pluginsMarketRemoveButton_" + root.safeObjectName(marketName)
                                        text: marketRow.pending
                                            ? "Removing..."
                                            : (root.confirmingKey === marketRow.resourceKey ? "Cancel" : "Remove")
                                        variant: root.confirmingKey === marketRow.resourceKey ? "secondary" : "ghost"
                                        compact: true
                                        enabled: root.pluginsModel && !root.pluginsModel.registry_busy
                                        onClicked: root.confirmingKey = root.confirmingKey === marketRow.resourceKey ? "" : marketRow.resourceKey
                                    }
                                }
                            }
                        }
                    }
                }

                Item { Layout.preferredHeight: Theme.space.xl }
            }
        }
    }

    component FeedbackBanner: Rectangle {
        id: banner
        property string tone: "success"
        property string message: ""
        property string textObjectName: ""
        property string buttonObjectName: ""
        signal dismissed()

        Layout.fillWidth: true
        Layout.preferredHeight: visible ? Math.max(42, bannerText.implicitHeight + Theme.space.lg) : 0
        color: tone === "error" ? Theme.colors.errorBg : Theme.colors.successBg
        border.width: visible ? 1 : 0
        border.color: tone === "error" ? Theme.colors.error : Theme.colors.success

        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: Theme.space.xl
            anchors.rightMargin: Theme.space.xl
            spacing: Theme.space.md

            Label {
                id: bannerText
                objectName: banner.textObjectName
                text: banner.message
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: banner.tone === "error" ? Theme.colors.error : Theme.colors.textPrimary
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }

            AppButton {
                objectName: banner.buttonObjectName
                text: "X"
                compact: true
                toolTipText: "Dismiss message"
                Layout.preferredWidth: 28
                Layout.preferredHeight: 28
                onClicked: banner.dismissed()
            }
        }
    }

    component SectionHeader: RowLayout {
        id: sectionHeader
        property string title: ""
        property int count: 0
        Layout.fillWidth: true
        spacing: Theme.space.md

        Rectangle {
            width: 18
            height: 2
            radius: 1
            color: Theme.colors.brand
            Layout.alignment: Qt.AlignVCenter
        }

        Label {
            text: sectionHeader.title
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.body
            font.weight: Theme.fontWeight.semibold
            color: Theme.colors.textPrimary
            elide: Text.ElideRight
            Layout.fillWidth: true
        }

        AppTag {
            text: String(sectionHeader.count)
            fontFamily: Theme.monoFonts[0]
            fontPixelSize: Theme.fontSize.micro
            backgroundColor: Theme.colors.surfaceRaised2
            borderColor: Theme.colors.borderSubtle
            textColor: Theme.colors.textSecondary
            minimumHeight: 24
        }
    }

    function syncActiveModel(activeOverride) {
        if (root.pluginsModel && root.pluginsModel.set_active) {
            root.pluginsModel.set_active(activeOverride === undefined ? root.visible : activeOverride)
        }
    }

    function summaryText() {
        if (!root.pluginsModel) return "no plugins"
        if (root.qaPluginCount === 0 && root.qaMarketplaceCount === 0 && root.pluginsModel.loading) return "loading plugins"
        return String(root.qaPluginCount) + " plugins / "
            + String(root.pluginsModel.enabled_count || 0) + " enabled / "
            + String(root.qaMarketplaceCount) + " markets / "
            + String(root.pluginsModel.scan_flag_count || 0) + " scan flags"
    }

    function scanStatus(plugin) {
        return String(plugin.scanStatus || "")
    }

    function scanText(plugin) {
        var status = scanStatus(plugin)
        var count = Number(plugin.scanCount || 0)
        if (status === "" && count <= 0) return ""
        if (count > 0) return status + ": " + String(count)
        return status
    }

    function pluginMeta(plugin) {
        var parts = []
        if (plugin.marketplace) parts.push("from " + plugin.marketplace)
        parts.push(plugin.enabled ? "enabled" : "disabled")
        if (root.shortDateMs(plugin.installedMs || 0) !== "") parts.push("installed " + root.shortDateMs(plugin.installedMs || 0))
        return parts.join(" / ")
    }

    function marketMeta(market) {
        var parts = []
        if (market.owner) parts.push("by " + market.owner)
        if (root.shortDateMs(market.addedMs || 0) !== "") parts.push("added " + root.shortDateMs(market.addedMs || 0))
        if (Number(market.pluginCount || 0) > 0) parts.push(String(market.pluginCount) + " installable")
        return parts.join(" / ")
    }

    function shortDateMs(ms) {
        ms = Number(ms || 0)
        if (!isFinite(ms) || ms <= 0) return ""
        var date = new Date(ms)
        if (isNaN(date.getTime())) return ""
        return String(date.getFullYear()) + "-" + pad2(date.getMonth() + 1) + "-" + pad2(date.getDate())
    }

    function pad2(value) {
        value = Number(value || 0)
        return value < 10 ? "0" + String(value) : String(value)
    }

    function components(plugin) {
        var out = []
        if ((plugin.skills || []).length) out.push({ label: "skills", n: (plugin.skills || []).length })
        if ((plugin.agents || []).length) out.push({ label: "agents", n: (plugin.agents || []).length })
        if ((plugin.mcpServers || []).length) out.push({ label: "mcp", n: (plugin.mcpServers || []).length })
        if ((plugin.commands || []).length) out.push({ label: "commands", n: (plugin.commands || []).length })
        if (Number(plugin.hooks || 0) > 0) out.push({ label: "hooks", n: Number(plugin.hooks || 0) })
        if (out.length === 0) out.push({ label: "components", n: 0 })
        return out
    }

    function previewComponents(preview) {
        var parts = []
        if (Number(preview.skills || 0) > 0) parts.push(String(preview.skills) + " skills")
        if (Number(preview.agents || 0) > 0) parts.push(String(preview.agents) + " agents")
        if (Number(preview.mcpServers || 0) > 0) parts.push(String(preview.mcpServers) + " mcp")
        if (Number(preview.commands || 0) > 0) parts.push(String(preview.commands) + " commands")
        if (Number(preview.hooks || 0) > 0) parts.push(String(preview.hooks) + " hooks")
        return parts.join(" / ")
    }

    function pluginConsequence(plugin) {
        var rows = root.components(plugin)
        if (rows.length === 1 && rows[0].label === "components" && rows[0].n === 0) return "Uninstall?"
        var parts = []
        for (var i = 0; i < rows.length; i++) parts.push(String(rows[i].n) + " " + rows[i].label)
        return "Remove " + parts.join(", ") + "?"
    }

    function warningText(plugin) {
        var warnings = plugin.warnings || []
        if (warnings.length > 0) return String(warnings[0])
        var scans = plugin.scans || []
        if (scans.length > 0) {
            var first = scans[0] || {}
            var reasons = first.reasons || []
            if (reasons.length > 0) return String(first.component || "scan") + ": " + String(reasons[0])
            return String(first.component || "scan")
        }
        return ""
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]/g, "_")
    }
}
