import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Routing view — read-only model/provider catalog plus observed route behavior.
Rectangle {
    id: root
    objectName: "routingView"
    color: Theme.colors.bgBase

    property var routingModel: null
    property string query: ""
    property string providerFilter: ""
    property bool onlyAvailable: false
    property var filteredModels: computeFilteredModels()
    property var visibleProviders: computeVisibleProviders()
    readonly property int qaFilteredModelCount: filteredModels ? filteredModels.length : 0

    onRoutingModelChanged: syncActiveModel()
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
                        objectName: "routingTitle"
                        text: "Routing"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.h3
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        objectName: "routingSummary"
                        text: summaryText()
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        color: Theme.colors.textMuted
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }

                AppButton {
                    objectName: "routingRefreshButton"
                    text: root.routingModel && root.routingModel.loading ? "Refreshing..." : "Refresh"
                    compact: true
                    toolTipText: "Refresh routing"
                    enabled: root.routingModel && !root.routingModel.loading
                    Layout.preferredWidth: Math.max(86, implicitWidth)
                    Layout.preferredHeight: 32
                    onClicked: if (root.routingModel) root.routingModel.refresh()
                }
            }
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

            Rectangle {
                Layout.preferredWidth: 220
                Layout.minimumWidth: 220
                Layout.maximumWidth: 220
                Layout.fillHeight: true
                color: Theme.colors.bgWell
                border.width: 1
                border.color: Theme.colors.borderHairline

                Flickable {
                    anchors.fill: parent
                    contentWidth: width
                    contentHeight: providerColumn.implicitHeight + Theme.space.xl
                    clip: true

                    ColumnLayout {
                        id: providerColumn
                        width: parent.width
                        spacing: Theme.space.xs

                        Item { Layout.preferredHeight: Theme.space.lg }

                        Label {
                            text: "Providers"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            font.weight: Theme.fontWeight.semibold
                            font.capitalization: Font.AllUppercase
                            color: Theme.colors.textFaint
                            Layout.leftMargin: Theme.space.lg
                            Layout.bottomMargin: Theme.space.xs
                        }

                        Rectangle {
                            objectName: "routingProvider_all"
                            readonly property bool qaTextFits: !allProviderLabel.truncated
                            Layout.fillWidth: true
                            Layout.leftMargin: Theme.space.sm
                            Layout.rightMargin: Theme.space.sm
                            implicitHeight: 32
                            radius: Theme.radius.sm
                            color: root.providerFilter === "" ? Theme.colors.stateSelected : (allProviderMouse.containsMouse ? Theme.colors.stateHover : "transparent")
                            border.width: root.providerFilter === "" ? 1 : 0
                            border.color: Theme.colors.borderBrandFaint

                            MouseArea {
                                id: allProviderMouse
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: root.providerFilter = ""
                            }

                            RowLayout {
                                anchors.fill: parent
                                anchors.leftMargin: Theme.space.lg
                                anchors.rightMargin: Theme.space.md
                                spacing: Theme.space.sm

                                Label {
                                    id: allProviderLabel
                                    text: "All"
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    font.weight: Theme.fontWeight.medium
                                    color: root.providerFilter === "" ? Theme.colors.brandBright : Theme.colors.textSecondary
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }

                                Label {
                                    text: root.routingModel ? String(root.routingModel.model_count) : "0"
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.micro
                                    color: Theme.colors.textMuted
                                }
                            }
                        }

                        Repeater {
                            model: root.visibleProviders
                            delegate: Rectangle {
                                readonly property var provider: modelData || ({})
                                readonly property string providerName: String(provider.name || "")
                                readonly property bool selected: root.providerFilter === providerName
                                readonly property bool qaTextFits: !providerNameLabel.truncated
                                objectName: "routingProvider_" + root.safeObjectName(providerName)
                                Layout.fillWidth: true
                                Layout.leftMargin: Theme.space.sm
                                Layout.rightMargin: Theme.space.sm
                                implicitHeight: 32
                                radius: Theme.radius.sm
                                color: selected ? Theme.colors.stateSelected : (providerMouse.containsMouse ? Theme.colors.stateHover : "transparent")
                                border.width: selected ? 1 : 0
                                border.color: Theme.colors.borderBrandFaint

                                MouseArea {
                                    id: providerMouse
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: root.providerFilter = selected ? "" : providerName
                                }

                                RowLayout {
                                    anchors.fill: parent
                                    anchors.leftMargin: Theme.space.lg
                                    anchors.rightMargin: Theme.space.md
                                    spacing: Theme.space.sm

                                    Rectangle {
                                        width: 7
                                        height: 7
                                        radius: 4
                                        color: provider.credentialed ? Theme.colors.dotOk : Theme.colors.dotIdle
                                    }

                                    Label {
                                        id: providerNameLabel
                                        text: providerName
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        font.weight: Theme.fontWeight.medium
                                        color: selected ? Theme.colors.brandBright : Theme.colors.textSecondary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        text: String(provider.modelCount || 0)
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textMuted
                                    }
                                }
                            }
                        }
                    }
                }
            }

            ColumnLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                spacing: 0

                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredHeight: 52
                    color: Theme.colors.bgBase
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    RowLayout {
                        anchors.fill: parent
                        anchors.leftMargin: Theme.space.xl
                        anchors.rightMargin: Theme.space.xl
                        spacing: Theme.space.lg

                        TextField {
                            id: searchField
                            objectName: "routingSearchField"
                            text: root.query
                            placeholderText: "Filter models"
                            selectByMouse: true
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textPrimary
                            placeholderTextColor: Theme.colors.textGhost
                            Layout.fillWidth: true
                            Layout.preferredHeight: 32
                            onTextChanged: root.query = text
                            background: Rectangle {
                                radius: Theme.radius.sm
                                color: Theme.colors.surfaceRaised2
                                border.width: 1
                                border.color: searchField.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                            }
                        }

                        RowLayout {
                            spacing: Theme.space.sm
                            Layout.alignment: Qt.AlignVCenter

                            Label {
                                text: "Credentialed"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textSecondary
                            }

                            AppSwitch {
                                objectName: "routingAvailableOnlySwitch"
                                checked: root.onlyAvailable
                                accessibleName: "Credentialed only"
                                toolTipText: "Show credentialed providers only"
                                onToggled: root.onlyAvailable = checked
                            }
                        }

                        Rectangle {
                            implicitWidth: Math.max(34, countLabel.implicitWidth + Theme.space.lg)
                            implicitHeight: 24
                            radius: 12
                            color: Theme.colors.bgOverlay
                            border.width: 1
                            border.color: Theme.colors.borderHairline

                            Label {
                                id: countLabel
                                anchors.centerIn: parent
                                text: String(root.qaFilteredModelCount)
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.micro
                                color: Theme.colors.textSecondary
                            }
                        }
                    }
                }

                Flickable {
                    id: routingFlick
                    objectName: "routingFlick"
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    contentWidth: width
                    contentHeight: contentColumn.implicitHeight + Theme.space.xxl
                    clip: true

                    ColumnLayout {
                        id: contentColumn
                        width: Math.min(parent.width - Theme.space.xl * 2, 980)
                        anchors.horizontalCenter: parent.horizontalCenter
                        spacing: Theme.space.lg

                        Item { Layout.preferredHeight: Theme.space.xl }

                        Rectangle {
                            id: refreshErrorBanner
                            objectName: "routingRefreshErrorBanner"
                            readonly property bool qaTextFits: !refreshErrorTitle.truncated
                                && !refreshErrorText.truncated
                                && refreshErrorRetry.qaTextFits
                            readonly property string qaErrorText: root.routingModel ? root.routingModel.load_error : ""
                            visible: root.routingModel && root.routingModel.load_error !== "" && root.routingModel.model_count > 0
                            Layout.fillWidth: true
                            Layout.preferredHeight: visible ? 56 : 0
                            radius: Theme.radius.md
                            color: Theme.colors.errorBg
                            border.width: 1
                            border.color: Theme.colors.error
                            clip: true

                            RowLayout {
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                spacing: Theme.space.md

                                Label {
                                    id: refreshErrorTitle
                                    text: "Refresh failed"
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.error
                                    elide: Text.ElideRight
                                }

                                Label {
                                    id: refreshErrorText
                                    text: refreshErrorBanner.qaErrorText
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textMuted
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }

                                AppButton {
                                    id: refreshErrorRetry
                                    objectName: "routingRefreshErrorRetry"
                                    text: "Retry"
                                    compact: true
                                    variant: "danger"
                                    toolTipText: "Retry routing refresh"
                                    Layout.preferredWidth: 68
                                    Layout.preferredHeight: 28
                                    onClicked: if (root.routingModel) root.routingModel.refresh()
                                }
                            }
                        }

                        Rectangle {
                            id: healthStrip
                            objectName: "routingHealthStrip"
                            visible: root.routingModel && root.routingModel.route_total > 0
                            Layout.fillWidth: true
                            Layout.preferredHeight: visible ? 112 : 0
                            radius: Theme.radius.md
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: Theme.colors.borderHairline
                            clip: true

                            ColumnLayout {
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                spacing: Theme.space.md

                                RowLayout {
                                    Layout.fillWidth: true
                                    spacing: Theme.space.lg

                                    Label {
                                        text: "Route health"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.body
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.textPrimary
                                    }

                                    Label {
                                        text: root.routingModel ? root.formatInt(root.routingModel.route_total) + " decisions" : "0 decisions"
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textMuted
                                    }

                                    Item { Layout.fillWidth: true }
                                }

                                RowLayout {
                                    Layout.fillWidth: true
                                    spacing: Theme.space.md

                                    Repeater {
                                        model: root.routeStages()
                                        delegate: Rectangle {
                                            Layout.fillWidth: true
                                            Layout.preferredHeight: 52
                                            radius: Theme.radius.sm
                                            color: modelData.tone
                                            border.width: 1
                                            border.color: Theme.colors.borderHairline

                                            ColumnLayout {
                                                anchors.fill: parent
                                                anchors.margins: Theme.space.md
                                                spacing: 2

                                                Label {
                                                    text: root.formatInt(modelData.count)
                                                    font.family: Theme.monoFonts[0]
                                                    font.pixelSize: Theme.fontSize.bodySm
                                                    color: Theme.colors.textPrimary
                                                }

                                                Label {
                                                    text: modelData.label
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
                        }

                        Rectangle {
                            visible: root.routingModel && root.routingModel.loading && root.routingModel.model_count === 0
                            Layout.fillWidth: true
                            Layout.preferredHeight: visible ? 120 : 0
                            radius: Theme.radius.md
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: Theme.colors.borderHairline

                            Label {
                                anchors.centerIn: parent
                                text: "Loading routing..."
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.body
                                color: Theme.colors.textMuted
                            }
                        }

                        Rectangle {
                            objectName: "routingEmptyErrorPanel"
                            visible: root.routingModel && root.routingModel.load_error !== "" && root.routingModel.model_count === 0
                            Layout.fillWidth: true
                            Layout.preferredHeight: visible ? 128 : 0
                            radius: Theme.radius.md
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: Theme.colors.borderHairline

                            ColumnLayout {
                                anchors.centerIn: parent
                                spacing: Theme.space.md

                                Label {
                                    text: "Could not load routing"
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.body
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.textPrimary
                                    Layout.alignment: Qt.AlignHCenter
                                }

                                Label {
                                    text: root.routingModel ? root.routingModel.load_error : ""
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textMuted
                                    Layout.alignment: Qt.AlignHCenter
                                }

                                AppButton {
                                    objectName: "routingEmptyErrorRetry"
                                    text: "Retry"
                                    compact: true
                                    Layout.alignment: Qt.AlignHCenter
                                    onClicked: if (root.routingModel) root.routingModel.refresh()
                                }
                            }
                        }

                        Label {
                            visible: root.routingModel && root.routingModel.model_count > 0 && root.qaFilteredModelCount === 0
                            text: "No models match the current filters."
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textMuted
                            Layout.fillWidth: true
                        }

                        Flow {
                            id: modelGrid
                            Layout.fillWidth: true
                            spacing: Theme.space.md
                            visible: root.qaFilteredModelCount > 0
                            property int columnCount: Math.max(1, Math.floor(width / 278))
                            property real cardWidth: Math.floor((width - (columnCount - 1) * spacing) / columnCount)

                            Repeater {
                                model: root.filteredModels
                                delegate: Rectangle {
                                    readonly property var modelInfo: modelData || ({})
                                    readonly property bool qaTextFits: !modelIdLabel.truncated
                                    objectName: "routingModelCard_" + root.safeObjectName(String(modelInfo.id || index))
                                    width: modelGrid.cardWidth
                                    height: 126
                                    radius: Theme.radius.md
                                    color: modelInfo.available ? Theme.colors.surfaceRaised : Theme.colors.bgWell
                                    border.width: 1
                                    border.color: modelInfo.available ? Theme.colors.borderSubtle : Theme.colors.borderHairline

                                    ColumnLayout {
                                        anchors.fill: parent
                                        anchors.margins: Theme.space.lg
                                        spacing: Theme.space.md

                                        RowLayout {
                                            Layout.fillWidth: true
                                            spacing: Theme.space.sm

                                            Rectangle {
                                                width: 8
                                                height: 8
                                                radius: 4
                                                color: modelInfo.available ? Theme.colors.dotOk : Theme.colors.dotIdle
                                                Layout.alignment: Qt.AlignVCenter
                                            }

                                            Label {
                                                id: modelIdLabel
                                                text: modelInfo.id || ""
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.bodySm
                                                font.weight: Theme.fontWeight.semibold
                                                color: modelInfo.available ? Theme.colors.textPrimary : Theme.colors.textMuted
                                                elide: Text.ElideRight
                                                Layout.fillWidth: true
                                            }
                                        }

                                        RowLayout {
                                            Layout.fillWidth: true
                                            spacing: Theme.space.sm

                                            Label {
                                                text: modelInfo.provider || ""
                                                font.family: Theme.monoFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.brandBright
                                                elide: Text.ElideRight
                                                Layout.fillWidth: true
                                            }

                                            Label {
                                                text: root.windowText(modelInfo.contextWindow || 0) + " ctx"
                                                font.family: Theme.monoFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textMuted
                                            }
                                        }

                                        Flow {
                                            Layout.fillWidth: true
                                            spacing: Theme.space.xs

                                            Repeater {
                                                model: root.capList(modelInfo)
                                                delegate: Rectangle {
                                                    height: 22
                                                    implicitWidth: capLabel.implicitWidth + Theme.space.md
                                                    radius: 11
                                                    color: Theme.colors.bgOverlay
                                                    border.width: 1
                                                    border.color: Theme.colors.borderHairline

                                                    Label {
                                                        id: capLabel
                                                        anchors.centerIn: parent
                                                        text: String(modelData)
                                                        font.family: Theme.uiFonts[0]
                                                        font.pixelSize: Theme.fontSize.micro
                                                        color: Theme.colors.textSecondary
                                                    }
                                                }
                                            }
                                        }

                                        Label {
                                            visible: !modelInfo.available
                                            text: "no credentials"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.micro
                                            color: Theme.colors.textFaint
                                        }
                                    }
                                }
                            }
                        }

                        Item { Layout.preferredHeight: Theme.space.xl }
                    }
                }
            }
        }
    }

    function syncActiveModel(activeOverride) {
        if (root.routingModel && root.routingModel.set_active) {
            root.routingModel.set_active(activeOverride === undefined ? root.visible : activeOverride)
        }
    }

    function computeVisibleProviders() {
        var providers = root.routingModel ? root.routingModel.providers || [] : []
        var out = []
        for (var i = 0; i < providers.length; i++) {
            var provider = providers[i] || {}
            if ((provider.modelCount || 0) > 0) out.push(provider)
        }
        return out
    }

    function computeFilteredModels() {
        var models = root.routingModel ? root.routingModel.models || [] : []
        var q = root.query.trim().toLowerCase()
        var out = []
        for (var i = 0; i < models.length; i++) {
            var model = models[i] || {}
            var id = String(model.id || "")
            var provider = String(model.provider || "")
            if (root.providerFilter !== "" && provider !== root.providerFilter) continue
            if (root.onlyAvailable && !model.available) continue
            if (q !== "" && id.toLowerCase().indexOf(q) < 0 && provider.toLowerCase().indexOf(q) < 0) continue
            out.push(model)
        }
        return out
    }

    function summaryText() {
        if (!root.routingModel) return "no catalog"
        var total = root.routingModel.model_count || 0
        var available = root.routingModel.available_count || 0
        var providers = root.routingModel.provider_count || 0
        if (total === 0 && root.routingModel.loading) return "loading catalog"
        return String(total) + " models / " + String(available) + " credentialed / " + String(providers) + " providers"
    }

    function routeStages() {
        var routes = root.routingModel ? root.routingModel.routes || ({}) : ({})
        return [
            { label: "routed", count: routes.routed || 0, tone: Theme.colors.brandBg },
            { label: "assessed", count: routes.assessed || 0, tone: Theme.colors.accentBg },
            { label: "skipped", count: routes.skipped || 0, tone: Theme.colors.bgOverlay },
            { label: "orchestrator", count: routes.orchestrator || 0, tone: Theme.colors.warnBg },
        ]
    }

    function capList(modelInfo) {
        var caps = []
        if (modelInfo.cache) caps.push("cache")
        if (modelInfo.context1m) caps.push("1M")
        if (modelInfo.reasoning) {
            var levels = modelInfo.effortLevels || []
            if (levels.length > 1) caps.push("effort " + levels[0] + "-" + levels[levels.length - 1])
            else if (levels.length === 1) caps.push("effort " + levels[0])
            else if (modelInfo.effort) caps.push("effort " + modelInfo.effort)
            else caps.push("reasoning")
            if ((modelInfo.thinkingBudget || 0) > 0) caps.push("think " + windowText(modelInfo.thinkingBudget))
        }
        if (modelInfo.search) caps.push("search")
        if (modelInfo.vision) caps.push("vision")
        if (modelInfo.social) caps.push("social")
        if (caps.length === 0) caps.push("standard")
        return caps
    }

    function windowText(n) {
        n = Number(n || 0)
        if (n <= 0) return "--"
        if (n >= 1000000) {
            var m = n / 1000000
            return (Math.round(m * 10) / 10).toString().replace(".0", "") + "M"
        }
        if (n >= 1000) return String(Math.round(n / 1000)) + "k"
        return String(n)
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]/g, "_")
    }

    function formatInt(value) {
        return String(Number(value || 0))
    }
}
