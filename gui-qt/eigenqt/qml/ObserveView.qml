import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Observe view — metadata-only telemetry summary.
Rectangle {
    id: root
    objectName: "observeView"
    color: Theme.colors.bgBase

    property var observeModel: null
    readonly property var summary: observeModel ? observeModel.summary || ({}) : ({})
    readonly property int qaRecordCount: observeModel ? observeModel.records : 0
    readonly property int qaToolCount: toolList().length
    readonly property int qaModelCount: modelList().length

    onObserveModelChanged: syncActiveModel()
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
                        objectName: "observeTitle"
                        text: "Observe"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.h3
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        objectName: "observeSummary"
                        text: summaryText()
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        color: Theme.colors.textMuted
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }

                AppButton {
                    objectName: "observeRefreshButton"
                    text: root.observeModel && root.observeModel.loading ? "Refreshing..." : "Refresh"
                    compact: true
                    toolTipText: "Refresh telemetry"
                    enabled: root.observeModel && !root.observeModel.loading
                    Layout.preferredWidth: Math.max(86, implicitWidth)
                    Layout.preferredHeight: 32
                    onClicked: if (root.observeModel) root.observeModel.refresh()
                }
            }
        }

        Flickable {
            id: observeFlick
            objectName: "observeFlick"
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
                    visible: root.observeModel && root.observeModel.loading && root.qaRecordCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 120 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Label {
                        anchors.centerIn: parent
                        text: "Loading telemetry..."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textMuted
                    }
                }

                Rectangle {
                    objectName: "observeLoadError"
                    visible: root.observeModel && root.observeModel.load_error !== "" && root.qaRecordCount === 0
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
                            text: "Could not load telemetry"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            objectName: "observeLoadErrorText"
                            text: root.observeModel ? root.observeModel.load_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            Layout.alignment: Qt.AlignHCenter
                        }

                        AppButton {
                            objectName: "observeLoadErrorRetry"
                            text: "Retry"
                            compact: true
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: if (root.observeModel) root.observeModel.refresh()
                        }
                    }
                }

                Rectangle {
                    visible: root.observeModel && !root.observeModel.loading && root.observeModel.load_error === "" && !root.observeModel.available
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 132 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.sm

                        Label {
                            text: "No telemetry log yet"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            text: "Session activity will appear here after the observe log has records."
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                }

                Flow {
                    objectName: "observeKpiFlow"
                    visible: root.observeModel && root.observeModel.available
                    Layout.fillWidth: true
                    spacing: Theme.space.md
                    property int columnCount: Math.max(2, Math.floor(width / 166))
                    property real cardWidth: Math.floor((width - (columnCount - 1) * spacing) / columnCount)

                    Repeater {
                        model: root.kpis()
                        delegate: Rectangle {
                            readonly property bool qaTextFits: !kpiValueLabel.truncated && !kpiLabel.truncated
                            objectName: "observeKpi_" + modelData.key
                            width: parent.cardWidth
                            height: 88
                            radius: Theme.radius.md
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: modelData.hot ? Theme.colors.borderBrandFaint : Theme.colors.borderHairline

                            ColumnLayout {
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                spacing: Theme.space.xs

                                Label {
                                    id: kpiValueLabel
                                    text: String(modelData.value)
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: 18
                                    font.weight: Theme.fontWeight.semibold
                                    color: modelData.hot ? Theme.colors.brandBright : Theme.colors.textPrimary
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }

                                Label {
                                    id: kpiLabel
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

                Rectangle {
                    id: routeMix
                    objectName: "observeRouteMix"
                    visible: root.observeModel && root.observeModel.available && root.observeModel.route_total > 0
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
                                text: "Routes"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.body
                                font.weight: Theme.fontWeight.semibold
                                color: Theme.colors.textPrimary
                            }

                            Label {
                                text: root.observeModel ? root.formatInt(root.observeModel.route_total) + " decisions" : "0 decisions"
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
                                    objectName: "observeRouteCard_" + modelData.key
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

                RowLayout {
                    visible: root.observeModel && root.observeModel.available
                    Layout.fillWidth: true
                    spacing: Theme.space.lg

                    Rectangle {
                        objectName: "observeToolsPanel"
                        Layout.fillWidth: true
                        Layout.preferredHeight: 220
                        radius: Theme.radius.md
                        color: Theme.colors.surfaceRaised
                        border.width: 1
                        border.color: Theme.colors.borderHairline
                        clip: true

                        ColumnLayout {
                            anchors.fill: parent
                            anchors.margins: Theme.space.lg
                            spacing: Theme.space.md

                            Label {
                                text: "Tools"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.body
                                font.weight: Theme.fontWeight.semibold
                                color: Theme.colors.textPrimary
                            }

                            ColumnLayout {
                                Layout.fillWidth: true
                                Layout.preferredHeight: implicitHeight
                                Layout.alignment: Qt.AlignTop
                                spacing: Theme.space.xs

                                Repeater {
                                    model: root.topItems(root.toolList(), 5)
                                    delegate: Rectangle {
                                        readonly property var tool: modelData || ({})
                                        readonly property bool qaTextFits: !toolNameLabel.truncated
                                        objectName: "observeToolRow_" + root.safeObjectName(tool.name || index)
                                        Layout.fillWidth: true
                                        implicitHeight: 34
                                        radius: Theme.radius.sm
                                        color: index % 2 === 0 ? Theme.colors.bgInset : "transparent"

                                        RowLayout {
                                            anchors.fill: parent
                                            anchors.leftMargin: Theme.space.md
                                            anchors.rightMargin: Theme.space.md
                                            spacing: Theme.space.md

                                            Label {
                                                id: toolNameLabel
                                                text: tool.name || ""
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.bodySm
                                                color: Theme.colors.textSecondary
                                                elide: Text.ElideRight
                                                Layout.fillWidth: true
                                            }

                                            Label {
                                                text: root.formatInt(tool.calls || 0)
                                                font.family: Theme.monoFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textPrimary
                                            }

                                            Label {
                                                text: (tool.errors || 0) > 0 ? root.formatInt(tool.errors) + " err" : "ok"
                                                font.family: Theme.monoFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: (tool.errors || 0) > 0 ? Theme.colors.error : Theme.colors.textMuted
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }

                    Rectangle {
                        objectName: "observeModelsPanel"
                        Layout.fillWidth: true
                        Layout.preferredHeight: 220
                        radius: Theme.radius.md
                        color: Theme.colors.surfaceRaised
                        border.width: 1
                        border.color: Theme.colors.borderHairline
                        clip: true

                        ColumnLayout {
                            anchors.fill: parent
                            anchors.margins: Theme.space.lg
                            spacing: Theme.space.md

                            Label {
                                text: "Models"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.body
                                font.weight: Theme.fontWeight.semibold
                                color: Theme.colors.textPrimary
                            }

                            ColumnLayout {
                                Layout.fillWidth: true
                                Layout.preferredHeight: implicitHeight
                                Layout.alignment: Qt.AlignTop
                                spacing: Theme.space.xs

                                Repeater {
                                    model: root.topItems(root.modelList(), 5)
                                    delegate: Rectangle {
                                        readonly property var modelInfo: modelData || ({})
                                        readonly property bool qaTextFits: !modelNameLabel.truncated
                                        objectName: "observeModelRow_" + root.safeObjectName(modelInfo.name || index)
                                        Layout.fillWidth: true
                                        implicitHeight: 38
                                        radius: Theme.radius.sm
                                        color: index % 2 === 0 ? Theme.colors.bgInset : "transparent"

                                        ColumnLayout {
                                            anchors.fill: parent
                                            anchors.leftMargin: Theme.space.md
                                            anchors.rightMargin: Theme.space.md
                                            anchors.topMargin: Theme.space.xs
                                            anchors.bottomMargin: Theme.space.xs
                                            spacing: 0

                                            RowLayout {
                                                Layout.fillWidth: true
                                                spacing: Theme.space.md

                                                Label {
                                                    id: modelNameLabel
                                                    text: modelInfo.name || ""
                                                    font.family: Theme.uiFonts[0]
                                                    font.pixelSize: Theme.fontSize.bodySm
                                                    color: Theme.colors.textSecondary
                                                    elide: Text.ElideRight
                                                    Layout.fillWidth: true
                                                }

                                                Label {
                                                    text: root.formatInt(modelInfo.turns || 0) + " turns"
                                                    font.family: Theme.monoFonts[0]
                                                    font.pixelSize: Theme.fontSize.micro
                                                    color: Theme.colors.textPrimary
                                                }
                                            }

                                            Label {
                                                text: "in " + root.compactInt(modelInfo.inTokens || 0) + " / out " + root.compactInt(modelInfo.outTokens || 0) + " / cache " + root.compactInt(modelInfo.cacheReadTokens || 0)
                                                font.family: Theme.monoFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textFaint
                                                elide: Text.ElideRight
                                                Layout.fillWidth: true
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }

                Rectangle {
                    objectName: "observeErrorsPanel"
                    visible: root.observeModel && root.observeModel.available && root.errorList().length > 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? Math.max(96, 48 + root.errorList().length * 34) : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline
                    clip: true

                    ColumnLayout {
                        anchors.fill: parent
                        anchors.margins: Theme.space.lg
                        spacing: Theme.space.md

                        Label {
                            text: "Errors"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                        }

                        Repeater {
                            model: root.errorList()
                            delegate: Rectangle {
                                readonly property var errorInfo: modelData || ({})
                                readonly property bool qaTextFits: !errorNameLabel.truncated
                                objectName: "observeErrorRow_" + root.safeObjectName(errorInfo.name || index)
                                Layout.fillWidth: true
                                implicitHeight: 30
                                radius: Theme.radius.sm
                                color: Theme.colors.errorBg
                                border.width: 1
                                border.color: Theme.colors.error

                                RowLayout {
                                    anchors.fill: parent
                                    anchors.leftMargin: Theme.space.md
                                    anchors.rightMargin: Theme.space.md
                                    spacing: Theme.space.md

                                    Label {
                                        id: errorNameLabel
                                        text: errorInfo.name || ""
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.error
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        text: root.formatInt(errorInfo.count || 0)
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.error
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

    function syncActiveModel(activeOverride) {
        if (root.observeModel && root.observeModel.set_active) {
            root.observeModel.set_active(activeOverride === undefined ? root.visible : activeOverride)
        }
    }

    function summaryText() {
        if (!root.observeModel) return "no telemetry"
        if (root.observeModel.loading && root.observeModel.records === 0) return "loading telemetry"
        if (!root.observeModel.available) return "no telemetry log"
        return root.formatInt(root.observeModel.records) + " records / "
            + root.formatInt(root.observeModel.tool_calls) + " tool calls / "
            + root.formatInt(root.observeModel.route_total) + " route decisions"
    }

    function kpis() {
        if (!root.observeModel) return []
        return [
            { key: "records", label: "records", value: root.formatInt(root.observeModel.records), hot: false },
            { key: "routes", label: "route decisions", value: root.formatInt(root.observeModel.route_total), hot: root.observeModel.route_total > 0 },
            { key: "tools", label: "tool calls", value: root.formatInt(root.observeModel.tool_calls), hot: false },
            { key: "models", label: "model turns", value: root.formatInt(root.observeModel.model_turns), hot: false },
            { key: "errors", label: "errors", value: root.formatInt(root.observeModel.error_count + root.observeModel.tool_errors + root.observeModel.subagent_errors), hot: root.observeModel.error_count > 0 || root.observeModel.tool_errors > 0 || root.observeModel.subagent_errors > 0 },
            { key: "subagents", label: "subagent errors", value: root.formatInt(root.observeModel.subagent_errors), hot: root.observeModel.subagent_errors > 0 },
        ]
    }

    function routeStages() {
        var routes = root.summary.routes || ({})
        return [
            { key: "routed", label: "routed", count: routes.routed || 0, tone: Theme.colors.brandBg },
            { key: "assessed", label: "assessed", count: routes.assessed || 0, tone: Theme.colors.accentBg },
            { key: "skipped", label: "skipped", count: routes.skipped || 0, tone: Theme.colors.bgOverlay },
            { key: "orchestrator", label: "orchestrator", count: routes.orchestrator || 0, tone: Theme.colors.warnBg },
        ]
    }

    function toolList() {
        return root.summary.tools || []
    }

    function modelList() {
        return root.summary.models || []
    }

    function errorList() {
        return root.summary.errors || []
    }

    function topItems(items, limit) {
        return (items || []).slice(0, limit || 5)
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]/g, "_")
    }

    function formatInt(value) {
        return String(Number(value || 0))
    }

    function compactInt(value) {
        value = Number(value || 0)
        if (value >= 1000000) return (Math.round(value / 100000) / 10).toString().replace(".0", "") + "M"
        if (value >= 1000) return (Math.round(value / 100) / 10).toString().replace(".0", "") + "k"
        return String(value)
    }
}
