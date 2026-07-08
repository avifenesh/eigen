import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

ScrollView {
    id: root
    objectName: "dockInfoTab"

    property var sessionStateModel: null

    clip: true
    contentWidth: availableWidth
    ScrollBar.vertical.policy: ScrollBar.AsNeeded

    ColumnLayout {
        width: root.availableWidth
        spacing: 0

        Item {
            Layout.fillWidth: true
            Layout.preferredHeight: Theme.space.lg
        }

        Label {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            text: "Session info"
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.h2
            font.weight: Theme.fontWeight.semibold
            color: Theme.colors.textPrimary
        }

        Label {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.xs
            text: root.modeLine()
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.bodySm
            color: Theme.colors.textMuted
            elide: Text.ElideRight
        }

        GridLayout {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.lg
            columns: 2
            columnSpacing: Theme.space.lg
            rowSpacing: Theme.space.md

            Label {
                text: "Title"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.micro
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textMuted
                Layout.preferredWidth: 58
            }

            Label {
                objectName: "dockInfoTitle"
                Layout.fillWidth: true
                text: root.field("title", "Untitled session")
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.body
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textPrimary
                wrapMode: Text.WrapAnywhere
            }

            Label {
                text: "Model"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.micro
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textMuted
                Layout.preferredWidth: 58
            }

            Label {
                objectName: "dockInfoModel"
                Layout.fillWidth: true
                text: root.field("model", "unknown")
                    + " / " + root.field("effort", "default")
                    + " / " + root.field("perm", "unknown")
                font.family: Theme.monoFonts[0]
                font.pixelSize: Theme.fontSize.codeSm
                color: Theme.colors.textSecondary
                wrapMode: Text.WrapAnywhere
            }

            Label {
                visible: root.field("provider", "") !== ""
                text: "Provider"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.micro
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textMuted
                Layout.preferredWidth: 58
            }

            Label {
                objectName: "dockInfoProvider"
                visible: root.field("provider", "") !== ""
                Layout.fillWidth: true
                text: root.field("provider", "")
                font.family: Theme.monoFonts[0]
                font.pixelSize: Theme.fontSize.codeSm
                color: Theme.colors.textMuted
                elide: Text.ElideRight
            }

            Label {
                text: "Goal"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.micro
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textMuted
                Layout.preferredWidth: 58
            }

            Label {
                objectName: "dockInfoGoal"
                Layout.fillWidth: true
                text: root.field("goal", "No goal set")
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: root.field("goal", "") === "" ? Theme.colors.textGhost : Theme.colors.textSecondary
                wrapMode: Text.WordWrap
            }
        }

        ColumnLayout {
            objectName: "dockInfoContext"
            visible: root.maxTokens() > 0
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: visible ? Theme.space.xl : 0
            spacing: Theme.space.sm

            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.space.md

                Label {
                    text: "Context"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.label
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textPrimary
                }

                Label {
                    objectName: "dockInfoContextSummary"
                    Layout.fillWidth: true
                    text: root.contextSummary()
                    font.family: Theme.monoFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    color: root.contextPercent() >= 85 ? Theme.colors.warn : Theme.colors.textMuted
                    horizontalAlignment: Text.AlignRight
                    elide: Text.ElideRight
                }
            }

            Rectangle {
                objectName: "dockInfoContextBar"
                Layout.fillWidth: true
                Layout.preferredHeight: 7
                color: Theme.colors.bgInset
                radius: 4
                border.width: 1
                border.color: Theme.colors.borderHairline
                clip: true

                Rectangle {
                    objectName: "dockInfoContextBarFill"
                    anchors.left: parent.left
                    anchors.top: parent.top
                    anchors.bottom: parent.bottom
                    width: Math.max(0, parent.width * root.contextPercent() / 100)
                    radius: 4
                    color: root.contextPercent() >= 85 ? Theme.colors.warn : Theme.colors.brand
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.lg
            Layout.preferredHeight: 1
            color: Theme.colors.borderHairline
        }

        Label {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.lg
            text: "Working directories"
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.label
            font.weight: Theme.fontWeight.semibold
            color: Theme.colors.textPrimary
        }

        ColumnLayout {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.sm
            spacing: Theme.space.xs

            Label {
                objectName: "dockInfoNoRoots"
                Layout.fillWidth: true
                visible: root.roots().length === 0
                text: "No working directories reported"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textGhost
            }

            Repeater {
                model: root.roots()

                delegate: Rectangle {
                    objectName: "dockInfoRoot_" + index
                    Layout.fillWidth: true
                    Layout.preferredHeight: rootLabel.implicitHeight + Theme.space.md
                    color: Theme.colors.bgInset
                    radius: Theme.radius.sm
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Label {
                        id: rootLabel
                        anchors.fill: parent
                        anchors.margins: Theme.space.sm
                        text: String(modelData)
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.codeSm
                        color: Theme.colors.textSecondary
                        verticalAlignment: Text.AlignVCenter
                        wrapMode: Text.WrapAnywhere
                    }
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.lg
            Layout.preferredHeight: 1
            color: Theme.colors.borderHairline
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.lg
            spacing: Theme.space.md

            Label {
                text: "Activity"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.label
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textPrimary
            }

            Item { Layout.fillWidth: true }

            Label {
                objectName: "dockInfoShellsSummary"
                text: root.shellsSummary()
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.micro
                color: Theme.colors.textMuted
                elide: Text.ElideRight
            }

            Label {
                objectName: "dockInfoPendingSummary"
                text: root.pendingSummary()
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.micro
                color: root.pending().length > 0 ? Theme.colors.warn : Theme.colors.textMuted
                elide: Text.ElideRight
            }
        }

        ColumnLayout {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.sm
            spacing: Theme.space.xs

            Label {
                objectName: "dockInfoNoActivity"
                Layout.fillWidth: true
                visible: root.shells().length === 0 && root.pending().length === 0
                text: "No background shells or approvals"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textGhost
            }

            Repeater {
                model: root.shells()

                delegate: Rectangle {
                    objectName: "dockInfoShell_" + index
                    Layout.fillWidth: true
                    Layout.preferredHeight: shellContent.implicitHeight + Theme.space.md
                    color: Theme.colors.bgInset
                    radius: Theme.radius.sm
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    ColumnLayout {
                        id: shellContent
                        anchors.fill: parent
                        anchors.margins: Theme.space.sm
                        spacing: Theme.space.xs

                        RowLayout {
                            Layout.fillWidth: true
                            spacing: Theme.space.md

                            Label {
                                Layout.preferredWidth: 54
                                text: root.shellStatus(modelData)
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.micro
                                color: root.shellStatus(modelData) === "running" ? Theme.colors.brandBright : Theme.colors.textMuted
                                horizontalAlignment: Text.AlignHCenter
                                elide: Text.ElideRight
                            }

                            Label {
                                Layout.fillWidth: true
                                text: root.shellCommand(modelData)
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.codeSm
                                color: Theme.colors.textSecondary
                                elide: Text.ElideRight
                            }
                        }

                        Label {
                            Layout.fillWidth: true
                            visible: root.shellLastLine(modelData) !== ""
                            text: root.shellLastLine(modelData)
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            color: Theme.colors.textGhost
                            elide: Text.ElideRight
                        }
                    }
                }
            }

            Repeater {
                model: root.pending()

                delegate: Rectangle {
                    objectName: "dockInfoPending_" + index
                    Layout.fillWidth: true
                    Layout.preferredHeight: pendingContent.implicitHeight + Theme.space.md
                    color: Theme.colors.warnBg
                    radius: Theme.radius.sm
                    border.width: 1
                    border.color: Theme.colors.borderSubtle

                    ColumnLayout {
                        id: pendingContent
                        anchors.fill: parent
                        anchors.margins: Theme.space.sm
                        spacing: Theme.space.xs

                        RowLayout {
                            Layout.fillWidth: true
                            spacing: Theme.space.md

                            Label {
                                Layout.preferredWidth: 54
                                text: "approval"
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.micro
                                color: Theme.colors.warn
                                horizontalAlignment: Text.AlignHCenter
                                elide: Text.ElideRight
                            }

                            Label {
                                Layout.fillWidth: true
                                text: root.pendingTool(modelData)
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.codeSm
                                color: Theme.colors.textSecondary
                                elide: Text.ElideRight
                            }
                        }

                        Label {
                            Layout.fillWidth: true
                            visible: root.pendingArgs(modelData) !== ""
                            text: root.pendingArgs(modelData)
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            color: Theme.colors.textMuted
                            elide: Text.ElideRight
                        }
                    }
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.lg
            Layout.preferredHeight: 1
            color: Theme.colors.borderHairline
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.lg
            spacing: Theme.space.md

            Label {
                text: "Tools"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.label
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textPrimary
            }

            Label {
                objectName: "dockInfoToolsSummary"
                Layout.fillWidth: true
                text: root.toolsSummary()
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.micro
                color: Theme.colors.textMuted
                horizontalAlignment: Text.AlignRight
                elide: Text.ElideRight
            }
        }

        ColumnLayout {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.sm
            Layout.bottomMargin: Theme.space.lg
            spacing: Theme.space.xs

            Label {
                objectName: "dockInfoNoTools"
                Layout.fillWidth: true
                visible: root.tools().length === 0
                text: "No tools reported"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textGhost
            }

            Repeater {
                model: root.tools()

                delegate: Rectangle {
                    objectName: "dockInfoTool_" + index
                    Layout.fillWidth: true
                    Layout.preferredHeight: toolRow.implicitHeight + Theme.space.md
                    color: "transparent"
                    border.width: 1
                    border.color: Theme.colors.borderHairline
                    radius: Theme.radius.sm

                    RowLayout {
                        id: toolRow
                        anchors.fill: parent
                        anchors.margins: Theme.space.sm
                        spacing: Theme.space.md

                        Label {
                            Layout.preferredWidth: 44
                            text: root.toolIsReadOnly(modelData) ? "read" : "write"
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            color: root.toolIsReadOnly(modelData) ? Theme.colors.textMuted : Theme.colors.warn
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                        }

                        Label {
                            Layout.fillWidth: true
                            text: root.toolName(modelData)
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.codeSm
                            color: Theme.colors.textSecondary
                            elide: Text.ElideRight
                            verticalAlignment: Text.AlignVCenter
                        }
                    }
                }
            }
        }
    }

    function raw(fieldName, fallbackValue) {
        if (!root.sessionStateModel) return fallbackValue
        var value = root.sessionStateModel[fieldName]
        if (value === undefined || value === null) return fallbackValue
        return value
    }

    function field(fieldName, fallbackValue) {
        var value = raw(fieldName, "")
        if (value === undefined || value === null || String(value) === "") return fallbackValue
        return String(value)
    }

    function roots() {
        var value = raw("roots", [])
        return value || []
    }

    function tools() {
        var value = raw("tools", [])
        return value || []
    }

    function shells() {
        var value = raw("shells", [])
        return value || []
    }

    function pending() {
        var value = raw("pending", [])
        return value || []
    }

    function tokens() {
        return Math.max(0, Number(raw("tokens", 0)) || 0)
    }

    function maxTokens() {
        return Math.max(0, Number(raw("maxTokens", 0)) || 0)
    }

    function contextPercent() {
        var max = maxTokens()
        if (max <= 0) return 0
        return Math.max(0, Math.min(100, Math.round(tokens() * 100 / max)))
    }

    function contextSummary() {
        return formatNumber(tokens()) + " / " + formatNumber(maxTokens()) + " (" + contextPercent() + "%)"
    }

    function formatNumber(value) {
        var text = String(Math.max(0, Math.floor(Number(value) || 0)))
        var out = ""
        while (text.length > 3) {
            out = "," + text.slice(text.length - 3) + out
            text = text.slice(0, text.length - 3)
        }
        return text + out
    }

    function modeLine() {
        var search = field("search", "")
        var fastOk = raw("fastOk", false)
        var fast = raw("fast", false)
        var parts = []
        if (search !== "") parts.push("search " + search)
        parts.push(fastOk ? ("fast " + (fast ? "on" : "off")) : "fast unavailable")
        return parts.join("  ")
    }

    function mapValue(value, key, fallbackValue) {
        if (!value) return fallbackValue
        var next = value[key]
        if (next === undefined || next === null) return fallbackValue
        return next
    }

    function toolName(tool) {
        return String(mapValue(tool, "name", "unknown"))
    }

    function toolIsReadOnly(tool) {
        return mapValue(tool, "read_only", mapValue(tool, "readOnly", false)) === true
    }

    function toolsSummary() {
        var all = tools()
        if (!all || all.length === 0) return "0 tools"
        var readOnly = 0
        var write = 0
        for (var i = 0; i < all.length; i++) {
            if (toolIsReadOnly(all[i])) readOnly++
            else write++
        }
        return all.length + " tool" + (all.length === 1 ? "" : "s")
            + " (" + readOnly + " read, " + write + " write)"
    }

    function shellsSummary() {
        var all = shells()
        return all.length + " shell" + (all.length === 1 ? "" : "s")
    }

    function pendingSummary() {
        var all = pending()
        return all.length + " approval" + (all.length === 1 ? "" : "s")
    }

    function shellCommand(shell) {
        return String(mapValue(shell, "command", "shell"))
    }

    function shellStatus(shell) {
        var status = String(mapValue(shell, "status", "running"))
        var exitCode = Number(mapValue(shell, "exit_code", mapValue(shell, "exitCode", 0)))
        if (status !== "running" && !isNaN(exitCode)) return status + " " + exitCode
        return status
    }

    function shellLastLine(shell) {
        return previewText(mapValue(shell, "last_line", mapValue(shell, "lastLine", "")), 120)
    }

    function pendingTool(item) {
        return String(mapValue(item, "tool", "tool"))
    }

    function pendingArgs(item) {
        return previewText(mapValue(item, "args", ""), 120)
    }

    function previewText(value, limit) {
        var text = String(value || "").replace(/\s+/g, " ").trim()
        if (text.length > limit) return text.slice(0, limit - 3) + "..."
        return text
    }
}
