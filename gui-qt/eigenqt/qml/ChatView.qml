import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Chat view for a single session
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property string sessionId

    signal backClicked()

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // Header
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 56
            color: Theme.colors.bgWell
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.margins: Theme.space.lg
                spacing: Theme.space.lg

                Button {
                    text: "← Back"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.body
                    onClicked: root.backClicked()

                    background: Rectangle {
                        color: parent.hovered ? Theme.colors.stateHover : "transparent"
                        radius: Theme.radius.sm
                    }

                    contentItem: Label {
                        text: parent.text
                        font: parent.font
                        color: Theme.colors.textSecondary
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }
                }

                Label {
                    text: "Session"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h3
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textPrimary
                    Layout.fillWidth: true
                }

                Button {
                    text: "Interrupt"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.body
                    visible: isStreaming
                    onClicked: rpcClient.call("Interrupt", [root.sessionId])

                    background: Rectangle {
                        color: parent.hovered ? Theme.colors.errorBg : "transparent"
                        border.width: 1
                        border.color: Theme.colors.error
                        radius: Theme.radius.sm
                    }

                    contentItem: Label {
                        text: parent.text
                        font: parent.font
                        color: Theme.colors.error
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }
                }
            }
        }

        // Transcript
        ListView {
            id: transcriptListView
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            spacing: Theme.space.lg
            topMargin: Theme.space.xl
            bottomMargin: Theme.space.xl
            leftMargin: Theme.space.xxxl
            rightMargin: Theme.space.xxxl

            model: transcriptModel

            delegate: TranscriptRow {
                width: transcriptListView.width - transcriptListView.leftMargin - transcriptListView.rightMargin
                kind: model.kind
                text: model.text
                toolName: model.toolName
                toolStatus: model.toolStatus
                streaming: model.streaming
                reasoning: model.reasoning
            }

            // Auto-scroll to bottom while at bottom
            property bool atBottom: atYEnd
            onCountChanged: {
                if (atBottom) {
                    Qt.callLater(positionViewAtEnd)
                }
            }

            // Approval overlay (if pending approvals)
            Loader {
                active: approvalsModel && approvalsModel.rowCount() > 0
                sourceComponent: ApprovalOverlay {
                    model: approvalsModel
                    onApprove: function(approvalId, allow) {
                        approvalsModel.approve(approvalId, allow)
                    }
                }
                anchors.centerIn: parent
            }
        }

        // Composer
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: composerColumn.height + Theme.space.lg * 2
            color: Theme.colors.bgWell
            border.width: 1
            border.color: Theme.colors.borderHairline

            ColumnLayout {
                id: composerColumn
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.top: parent.top
                anchors.margins: Theme.space.lg
                spacing: Theme.space.md

                ScrollView {
                    Layout.fillWidth: true
                    Layout.preferredHeight: Math.min(composerTextArea.contentHeight + Theme.space.md * 2, 120)
                    clip: true

                    TextArea {
                        id: composerTextArea
                        placeholderText: "Type a message (Enter to send, Shift+Enter for newline)"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textPrimary
                        wrapMode: TextArea.Wrap

                        background: Rectangle {
                            color: Theme.colors.surfaceRaised
                            radius: Theme.radius.md
                            border.width: composerTextArea.activeFocus ? 1 : 0
                            border.color: Theme.colors.borderBrand
                        }

                        Keys.onReturnPressed: function(event) {
                            if (event.modifiers & Qt.ShiftModifier) {
                                // Allow default (newline)
                                event.accepted = false
                            } else {
                                sendButton.clicked()
                                event.accepted = true
                            }
                        }
                    }
                }

                RowLayout {
                    Layout.fillWidth: true
                    spacing: Theme.space.md

                    Item { Layout.fillWidth: true }

                    Button {
                        id: sendButton
                        text: "Send"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.medium
                        enabled: composerTextArea.text.trim().length > 0

                        onClicked: {
                            var msg = composerTextArea.text.trim()
                            if (msg.length === 0) return
                            rpcClient.call("SendInput", [root.sessionId, msg, [], []])
                            composerTextArea.text = ""
                        }

                        background: Rectangle {
                            color: parent.enabled ? (parent.hovered ? Theme.colors.brandStrong : Theme.colors.brand) : Theme.colors.borderSubtle
                            radius: Theme.radius.sm
                            implicitWidth: 80
                            implicitHeight: 32
                        }

                        contentItem: Label {
                            text: parent.text
                            font: parent.font
                            color: parent.enabled ? "#06100e" : Theme.colors.textMuted
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                        }
                    }
                }
            }
        }
    }

    property bool isStreaming: {
        if (!transcriptModel) return false
        for (var i = 0; i < transcriptModel.rowCount(); i++) {
            var streaming = transcriptModel.data(transcriptModel.index(i, 0), 262)  // StreamingRole
            if (streaming) return true
        }
        return false
    }
}
