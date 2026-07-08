import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Expandable tool call card (collapsed = glyph + name + summary + status; expanded = args + result)
Rectangle {
    id: root
    width: parent ? parent.width * widthFactor : 900
    implicitHeight: column.height
    color: open ? Theme.colors.surfaceRaised2 : Theme.colors.surfaceRaised
    radius: Theme.radius.md
    border.width: 1
    border.color: isError ? Qt.rgba(1, 0.3, 0.3, 0.3) : Theme.colors.borderHairline

    property string toolName
    property string toolId
    property string toolArgs
    property string toolResult
    property string toolStatus  // "running", "success", "error"
    property bool done
    property bool open: false
    property real widthFactor: 0.85

    readonly property bool isError: toolStatus === "error"
    readonly property bool isRunning: toolStatus === "running"
    readonly property string glyph: {
        var k = toolName.toLowerCase().trim()
        if (k === "edit" || k === "multi_edit" || k === "multiedit") return "✎"
        if (k === "write") return "＋"
        if (k === "read") return "▤"
        if (k === "bash" || k === "shell") return "❯"
        if (k === "grep" || k === "search") return "⌕"
        return "•"
    }

    // One-line summary from args
    readonly property string summary: {
        try {
            var argsObj = toolArgs ? JSON.parse(toolArgs) : {}
            var k = toolName.toLowerCase().trim()
            if (k === "bash" || k === "shell") {
                return argsObj.command || ""
            }
            if (k === "read") {
                return argsObj.path || argsObj.file_path || ""
            }
            if (k === "write") {
                return argsObj.path || argsObj.file_path || ""
            }
            // Generic: first field
            for (var key in argsObj) {
                if (argsObj[key]) return String(argsObj[key]).split("\n")[0].substring(0, 80)
            }
            return ""
        } catch (e) {
            return toolArgs ? toolArgs.split("\n")[0].substring(0, 80) : ""
        }
    }

    ColumnLayout {
        id: column
        width: parent.width
        spacing: 0

        // Header (clickable)
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 40
            color: headerMouse.containsMouse ? Theme.colors.stateHover : "transparent"
            radius: Theme.radius.md

            RowLayout {
                anchors.fill: parent
                anchors.margins: Theme.space.md
                spacing: Theme.space.md

                // Glyph
                Label {
                    text: root.glyph
                    font.pixelSize: Theme.fontSize.bodySm
                    color: {
                        if (root.isError) return Theme.colors.error
                        if (root.isRunning) return Theme.colors.working
                        return root.done ? Theme.colors.brand : Theme.colors.textGhost
                    }
                }

                // Tool name
                Label {
                    text: root.toolName || "tool"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textPrimary
                }

                // Summary
                Label {
                    text: root.summary || "—"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }

                // Status dot
                Rectangle {
                    width: 8
                    height: 8
                    radius: 4
                    color: {
                        if (root.isError) return Theme.colors.error
                        if (root.isRunning) return Theme.colors.working
                        return Theme.colors.brand
                    }

                    // Pulse animation for running
                    SequentialAnimation on opacity {
                        running: Theme.continuousMotion && root.isRunning
                        loops: Animation.Infinite
                        NumberAnimation { from: 0.4; to: 1.0; duration: 800; easing.type: Easing.InOutQuad }
                        NumberAnimation { from: 1.0; to: 0.4; duration: 800; easing.type: Easing.InOutQuad }
                    }
                }

                // Chevron
                Label {
                    text: root.open ? "▼" : "▸"
                    font.pixelSize: Theme.fontSize.body
                    color: Theme.colors.textGhost
                }
            }

            MouseArea {
                id: headerMouse
                anchors.fill: parent
                hoverEnabled: true
                onClicked: root.open = !root.open
            }
        }

        // Body (args + result, shown when open)
        Loader {
            active: root.open
            Layout.fillWidth: true
            sourceComponent: ColumnLayout {
                width: parent.width
                spacing: Theme.space.md

                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredHeight: 1
                    color: Theme.colors.divider
                }

                // Arguments
                ColumnLayout {
                    Layout.fillWidth: true
                    Layout.leftMargin: Theme.space.lg
                    Layout.rightMargin: Theme.space.lg
                    Layout.topMargin: Theme.space.md
                    spacing: Theme.space.sm

                    Label {
                        text: "ARGUMENTS"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textFaint
                    }

                    ScrollView {
                        Layout.fillWidth: true
                        Layout.preferredHeight: Math.min(argsText.implicitHeight, 200)
                        clip: true

                        AppTextArea {
                            id: argsText
                            objectName: "toolArgsText"
                            text: root.toolArgs || ""
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textPrimary
                            wrapMode: TextArea.Wrap
                            readOnly: true
                            backgroundColor: Theme.colors.bgInset
                            borderColor: Theme.colors.borderHairline
                            focusBorderColor: Theme.colors.borderBrandFaint
                            normalBorderWidth: 0
                            focusedBorderWidth: 1
                            backgroundRadius: Theme.radius.sm
                            Accessible.name: "Tool arguments"
                        }
                    }
                }

                // Result (if present)
                Loader {
                    active: root.toolResult && root.toolResult.length > 0
                    Layout.fillWidth: true
                    Layout.leftMargin: Theme.space.lg
                    Layout.rightMargin: Theme.space.lg
                    Layout.bottomMargin: Theme.space.md
                    sourceComponent: ColumnLayout {
                        spacing: Theme.space.sm

                        Label {
                            text: root.isError ? "ERROR" : "RESULT"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            font.weight: Theme.fontWeight.semibold
                            color: root.isError ? Theme.colors.error : Theme.colors.textFaint
                        }

                        ScrollView {
                            Layout.fillWidth: true
                            Layout.preferredHeight: Math.min(resultText.implicitHeight, 400)
                            clip: true

                            AppTextArea {
                                id: resultText
                                objectName: "toolResultText"
                                text: root.toolResult || ""
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                color: Theme.colors.textPrimary
                                wrapMode: TextArea.Wrap
                                readOnly: true
                                backgroundColor: Theme.colors.bgInset
                                borderColor: root.isError ? Theme.colors.error : Theme.colors.borderHairline
                                focusBorderColor: root.isError ? Theme.colors.error : Theme.colors.borderBrandFaint
                                normalBorderWidth: root.isError ? 1 : 0
                                focusedBorderWidth: 1
                                backgroundRadius: Theme.radius.sm
                                Accessible.name: root.isError ? "Tool error output" : "Tool result"
                            }
                        }
                    }
                }

                // Running indicator
                Loader {
                    active: root.isRunning
                    Layout.fillWidth: true
                    Layout.leftMargin: Theme.space.lg
                    Layout.bottomMargin: Theme.space.md
                    sourceComponent: RowLayout {
                        spacing: Theme.space.sm

                        Rectangle {
                            width: 8
                            height: 8
                            radius: 4
                            color: Theme.colors.working

                            SequentialAnimation on opacity {
                                running: Theme.continuousMotion
                                loops: Animation.Infinite
                                NumberAnimation { from: 0.4; to: 1.0; duration: 800; easing.type: Easing.InOutQuad }
                                NumberAnimation { from: 1.0; to: 0.4; duration: 800; easing.type: Easing.InOutQuad }
                            }
                        }

                        Label {
                            text: "running…"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.working
                        }
                    }
                }
            }
        }
    }
}
