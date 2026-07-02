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
    property string toolId
    property string toolArgs
    property string toolStatus
    property bool streaming
    property string reasoning
    property var blocks: []  // markdown blocks for assistant rows

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

                    // Text (markdown-rendered via MarkdownBlocks)
                    MarkdownBlocks {
                        Layout.fillWidth: true
                        blocks: root.blocks
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

        // Tool invocation (expandable card)
        Loader {
            active: root.kind === "tool"
            Layout.fillWidth: true
            sourceComponent: ToolCallCard {
                toolName: root.toolName
                toolId: root.toolId
                toolArgs: root.toolArgs
                toolResult: root.text  // result stored in text field
                toolStatus: root.toolStatus
                done: root.toolStatus === "success" || root.toolStatus === "error"
                // Auto-open running tools
                open: root.toolStatus === "running"
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
