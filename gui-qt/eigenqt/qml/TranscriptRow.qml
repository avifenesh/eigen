import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Transcript row delegate (user, assistant, tool, note, approval)
Item {
    id: root
    height: column.height

    property string kind
    property string text
    property string toolName
    property string toolStatus
    property bool streaming
    property string reasoning

    ColumnLayout {
        id: column
        width: parent.width
        spacing: Theme.space.md

        // User message (right-aligned, teal tint)
        Loader {
            active: root.kind === "user"
            Layout.fillWidth: true
            sourceComponent: Row {
                width: parent.width
                spacing: 0
                layoutDirection: Qt.RightToLeft

                Rectangle {
                    width: Math.min(messageLabel.contentWidth + Theme.space.lg * 2, parent.width * 0.75)
                    height: messageLabel.contentHeight + Theme.space.lg * 2
                    color: Theme.colors.brandDim
                    radius: Theme.radius.md
                    border.width: 1
                    border.color: Theme.colors.borderBrand

                    Label {
                        id: messageLabel
                        anchors.fill: parent
                        anchors.margins: Theme.space.lg
                        text: root.text
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textPrimary
                        wrapMode: Text.Wrap
                    }
                }
            }
        }

        // Assistant message (left-aligned, raised surface, markdown-rendered)
        Loader {
            active: root.kind === "assistant"
            Layout.fillWidth: true
            sourceComponent: Rectangle {
                width: parent.width * 0.85
                implicitHeight: assistantColumn.height + Theme.space.lg * 2
                color: Theme.colors.surfaceRaised
                radius: Theme.radius.md
                border.width: 1
                border.color: Theme.colors.borderHairline

                ColumnLayout {
                    id: assistantColumn
                    anchors.left: parent.left
                    anchors.right: parent.right
                    anchors.top: parent.top
                    anchors.margins: Theme.space.lg
                    spacing: Theme.space.md

                    // Reasoning (if present)
                    Loader {
                        active: root.reasoning && root.reasoning.length > 0
                        Layout.fillWidth: true
                        sourceComponent: Rectangle {
                            width: parent.width
                            implicitHeight: reasoningLabel.contentHeight + Theme.space.md * 2
                            color: Theme.colors.bgInset
                            radius: Theme.radius.sm
                            border.width: 1
                            border.color: Theme.colors.borderSubtle

                            Label {
                                id: reasoningLabel
                                anchors.fill: parent
                                anchors.margins: Theme.space.md
                                text: root.reasoning
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                font.italic: true
                                color: Theme.colors.textMuted
                                wrapMode: Text.Wrap
                            }
                        }
                    }

                    // Text (markdown-rendered via Qt Text.MarkdownText)
                    Label {
                        Layout.fillWidth: true
                        text: root.text
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textPrimary
                        wrapMode: Text.Wrap
                        textFormat: Text.MarkdownText  // Qt 6.2+ markdown support (v1)
                    }

                    // Streaming pulse
                    Loader {
                        active: root.streaming
                        Layout.fillWidth: true
                        sourceComponent: Rectangle {
                            width: parent.width
                            height: 3
                            radius: 2
                            color: Theme.colors.brand

                            SequentialAnimation on opacity {
                                running: true
                                loops: Animation.Infinite
                                NumberAnimation { from: 0.3; to: 1.0; duration: 800; easing.type: Easing.InOutQuad }
                                NumberAnimation { from: 1.0; to: 0.3; duration: 800; easing.type: Easing.InOutQuad }
                            }
                        }
                    }
                }
            }
        }

        // Tool invocation (collapsed, name + status badge)
        Loader {
            active: root.kind === "tool"
            Layout.fillWidth: true
            sourceComponent: Rectangle {
                width: parent.width * 0.7
                height: 40
                color: Theme.colors.surfaceRaised
                radius: Theme.radius.sm
                border.width: 1
                border.color: Theme.colors.borderSubtle

                RowLayout {
                    anchors.fill: parent
                    anchors.margins: Theme.space.md
                    spacing: Theme.space.md

                    Label {
                        text: "⚙"
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textMuted
                    }

                    Label {
                        text: root.toolName
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textPrimary
                        Layout.fillWidth: true
                    }

                    Rectangle {
                        width: statusLabel.contentWidth + Theme.space.md * 2
                        height: 20
                        radius: Theme.radius.sm
                        color: {
                            if (root.toolStatus === "success") return Theme.colors.successBg
                            if (root.toolStatus === "error") return Theme.colors.errorBg
                            return Theme.colors.workingBg
                        }

                        Label {
                            id: statusLabel
                            anchors.centerIn: parent
                            text: root.toolStatus || "running"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            font.weight: Theme.fontWeight.medium
                            color: {
                                if (root.toolStatus === "success") return Theme.colors.success
                                if (root.toolStatus === "error") return Theme.colors.error
                                return Theme.colors.working
                            }
                        }
                    }
                }
            }
        }

        // Note (info/warning, compact)
        Loader {
            active: root.kind === "note"
            Layout.fillWidth: true
            sourceComponent: Rectangle {
                width: parent.width * 0.7
                implicitHeight: noteLabel.contentHeight + Theme.space.md * 2
                color: Theme.colors.bgInset
                radius: Theme.radius.sm
                border.width: 1
                border.color: Theme.colors.borderSubtle

                Label {
                    id: noteLabel
                    anchors.fill: parent
                    anchors.margins: Theme.space.md
                    text: root.text
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textSecondary
                    wrapMode: Text.Wrap
                }
            }
        }

        // Approval (shown in overlay, not inline — placeholder here)
        Loader {
            active: root.kind === "approval"
            Layout.fillWidth: true
            sourceComponent: Rectangle {
                width: parent.width * 0.7
                height: 40
                color: Theme.colors.warnBg
                radius: Theme.radius.sm
                border.width: 1
                border.color: Theme.colors.warn

                Label {
                    anchors.centerIn: parent
                    text: "Approval required: " + root.toolName
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    font.weight: Theme.fontWeight.medium
                    color: Theme.colors.warn
                }
            }
        }
    }
}
