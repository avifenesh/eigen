import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Plugins view - read-only installed plugin and marketplace inventory.
Rectangle {
    id: root
    objectName: "pluginsView"
    color: Theme.colors.bgBase

    property var pluginsModel: null
    readonly property var plugins: pluginsModel ? pluginsModel.plugins || [] : []
    readonly property var marketplaces: pluginsModel ? pluginsModel.marketplaces || [] : []
    readonly property int qaPluginCount: pluginsModel ? pluginsModel.plugin_count : 0
    readonly property int qaMarketplaceCount: pluginsModel ? pluginsModel.marketplace_count : 0

    onPluginsModelChanged: syncActiveModel()
    onVisibleChanged: syncActiveModel()
    Component.onCompleted: syncActiveModel()

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 56
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline

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
                        font.pixelSize: Theme.fontSize.h3
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        objectName: "pluginsSummary"
                        text: summaryText()
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        color: Theme.colors.textMuted
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }

                AppButton {
                    objectName: "pluginsRefreshButton"
                    text: root.pluginsModel && root.pluginsModel.loading ? "Refreshing..." : "Refresh"
                    compact: true
                    toolTipText: "Refresh plugins"
                    enabled: root.pluginsModel && !root.pluginsModel.loading
                    Layout.preferredWidth: Math.max(86, implicitWidth)
                    Layout.preferredHeight: 32
                    onClicked: if (root.pluginsModel) root.pluginsModel.refresh()
                }
            }
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
                width: Math.min(parent.width - Theme.space.xl * 2, 1040)
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: Theme.space.lg

                Item { Layout.preferredHeight: Theme.space.xl }

                Rectangle {
                    visible: root.pluginsModel && root.pluginsModel.loading && root.qaPluginCount === 0 && root.qaMarketplaceCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 120 : 0
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
                    visible: root.pluginsModel && root.pluginsModel.load_error !== "" && root.qaPluginCount === 0 && root.qaMarketplaceCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 132 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    ColumnLayout {
                        anchors.centerIn: parent
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
                            text: root.pluginsModel ? root.pluginsModel.load_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            Layout.alignment: Qt.AlignHCenter
                        }

                        AppButton {
                            text: "Retry"
                            compact: true
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: if (root.pluginsModel) root.pluginsModel.refresh()
                        }
                    }
                }

                Rectangle {
                    visible: root.pluginsModel && !root.pluginsModel.loading && root.pluginsModel.load_error === "" && root.qaPluginCount === 0 && root.qaMarketplaceCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 132 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Label {
                        anchors.centerIn: parent
                        text: "No plugins installed."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }
                }

                ColumnLayout {
                    visible: root.qaPluginCount > 0
                    Layout.fillWidth: true
                    spacing: Theme.space.md

                    SectionHeader {
                        title: "Installed plugins"
                        count: root.qaPluginCount
                    }

                    Repeater {
                        model: root.plugins
                        delegate: Rectangle {
                            id: pluginRow
                            readonly property var plugin: modelData || ({})
                            readonly property string pluginName: String(plugin.name || "")
                            readonly property bool qaTextFits: !pluginNameLabel.truncated && !pluginMetaLabel.truncated && !pluginDescLabel.truncated && !pluginWarnLabel.truncated
                            objectName: "pluginsInstalledRow_" + root.safeObjectName(pluginName || index)
                            Layout.fillWidth: true
                            implicitHeight: Math.max(98, pluginColumn.implicitHeight + Theme.space.xl)
                            radius: Theme.radius.md
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: root.scanStatus(plugin) === "forced" ? Theme.colors.warn : Theme.colors.borderHairline

                            ColumnLayout {
                                id: pluginColumn
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                spacing: Theme.space.sm

                                RowLayout {
                                    Layout.fillWidth: true
                                    spacing: Theme.space.sm

                                    Rectangle {
                                        width: 7
                                        height: 7
                                        radius: 4
                                        color: plugin.enabled ? Theme.colors.dotOk : Theme.colors.dotIdle
                                        Layout.alignment: Qt.AlignVCenter
                                    }

                                    Label {
                                        id: pluginNameLabel
                                        text: pluginName || "plugin"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.textPrimary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    Rectangle {
                                        visible: String(plugin.version || "") !== ""
                                        height: 21
                                        implicitWidth: versionLabel.implicitWidth + Theme.space.md
                                        radius: 11
                                        color: Theme.colors.bgOverlay
                                        border.width: 1
                                        border.color: Theme.colors.borderHairline

                                        Label {
                                            id: versionLabel
                                            anchors.centerIn: parent
                                            text: plugin.version || ""
                                            font.family: Theme.monoFonts[0]
                                            font.pixelSize: Theme.fontSize.micro
                                            color: Theme.colors.textSecondary
                                        }
                                    }

                                    Rectangle {
                                        visible: root.scanStatus(plugin) !== ""
                                        height: 21
                                        implicitWidth: scanLabel.implicitWidth + Theme.space.md
                                        radius: 11
                                        color: root.scanStatus(plugin) === "forced" ? Theme.colors.warnBg : Theme.colors.successBg
                                        border.width: 1
                                        border.color: root.scanStatus(plugin) === "forced" ? Theme.colors.warn : Theme.colors.success

                                        Label {
                                            id: scanLabel
                                            anchors.centerIn: parent
                                            text: root.scanText(plugin)
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.micro
                                            color: Theme.colors.textSecondary
                                        }
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
                                    id: pluginDescLabel
                                    visible: String(plugin.description || "") !== ""
                                    text: plugin.description || ""
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textMuted
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }

                                Flow {
                                    Layout.fillWidth: true
                                    spacing: Theme.space.xs

                                    Repeater {
                                        model: root.components(plugin)
                                        delegate: Rectangle {
                                            height: 22
                                            implicitWidth: componentLabel.implicitWidth + Theme.space.md
                                            radius: 11
                                            color: Theme.colors.bgOverlay
                                            border.width: 1
                                            border.color: Theme.colors.borderHairline

                                            Label {
                                                id: componentLabel
                                                anchors.centerIn: parent
                                                text: String(modelData.n) + " " + modelData.label
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textSecondary
                                            }
                                        }
                                    }
                                }

                                Label {
                                    id: pluginWarnLabel
                                    visible: root.warningText(plugin) !== ""
                                    text: root.warningText(plugin)
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.micro
                                    color: root.scanStatus(plugin) === "forced" ? Theme.colors.warn : Theme.colors.textMuted
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                            }
                        }
                    }
                }

                ColumnLayout {
                    visible: root.qaMarketplaceCount > 0
                    Layout.fillWidth: true
                    spacing: Theme.space.md

                    SectionHeader {
                        title: "Marketplaces"
                        count: root.qaMarketplaceCount
                    }

                    Repeater {
                        model: root.marketplaces
                        delegate: Rectangle {
                            id: marketRow
                            readonly property var market: modelData || ({})
                            readonly property string marketName: String(market.name || "")
                            readonly property bool qaTextFits: !marketNameLabel.truncated && !marketSourceLabel.truncated && !marketMetaLabel.truncated
                            objectName: "pluginsMarketRow_" + root.safeObjectName(marketName || index)
                            Layout.fillWidth: true
                            implicitHeight: 70
                            radius: Theme.radius.md
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: Theme.colors.borderHairline

                            RowLayout {
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                spacing: Theme.space.md

                                Rectangle {
                                    width: 7
                                    height: 7
                                    radius: 4
                                    color: market.disabled ? Theme.colors.dotIdle : Theme.colors.dotOk
                                    Layout.alignment: Qt.AlignVCenter
                                }

                                ColumnLayout {
                                    Layout.fillWidth: true
                                    spacing: 2

                                    RowLayout {
                                        Layout.fillWidth: true
                                        spacing: Theme.space.sm

                                        Label {
                                            id: marketNameLabel
                                            text: marketName || "marketplace"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.bodySm
                                            font.weight: Theme.fontWeight.semibold
                                            color: Theme.colors.textPrimary
                                            elide: Text.ElideRight
                                            Layout.fillWidth: true
                                        }

                                        Rectangle {
                                            height: 21
                                            implicitWidth: marketStateLabel.implicitWidth + Theme.space.md
                                            radius: 11
                                            color: market.disabled ? Theme.colors.bgOverlay : Theme.colors.successBg
                                            border.width: 1
                                            border.color: market.disabled ? Theme.colors.borderHairline : Theme.colors.success

                                            Label {
                                                id: marketStateLabel
                                                anchors.centerIn: parent
                                                text: market.disabled ? "disabled" : "enabled"
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textSecondary
                                            }
                                        }
                                    }

                                    Label {
                                        id: marketSourceLabel
                                        text: market.source || ""
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.codeSm
                                        color: Theme.colors.textSecondary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        id: marketMetaLabel
                                        text: root.marketMeta(market)
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textMuted
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
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

    component SectionHeader: RowLayout {
        id: sectionHeader
        property string title: ""
        property int count: 0
        Layout.fillWidth: true
        spacing: Theme.space.sm

        Rectangle {
            width: 3
            height: 14
            radius: 2
            color: Theme.colors.brandBright
            Layout.alignment: Qt.AlignVCenter
        }

        Label {
            text: sectionHeader.title
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.micro
            font.weight: Theme.fontWeight.semibold
            font.capitalization: Font.AllUppercase
            color: Theme.colors.textFaint
        }

        Label {
            text: String(sectionHeader.count)
            font.family: Theme.monoFonts[0]
            font.pixelSize: Theme.fontSize.micro
            color: Theme.colors.textGhost
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
