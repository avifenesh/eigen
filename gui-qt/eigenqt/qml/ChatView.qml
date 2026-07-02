import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Chat view for a single session (with tool cards, session settings, slash commands, image paste, steer)
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property string sessionId
    property var sessionStateModel  // SessionStateModel
    property var commandsModel  // CommandsModel
    property bool dockOpen: false
    // Context property captured under an unshadowed name: inside
    // `DockPanel { rpcClient: ... }` a bare `rpcClient` RHS resolves to
    // DockPanel's OWN property (self-binding → undefined) — the QML
    // delegate-scope footgun, third sighting in this port.
    property var rpcRef: rpcClient

    signal backClicked()

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // Back button + session settings strip
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

                Button {
                    text: "Interrupt"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.body
                    visible: isStreaming
                    onClicked: rpcClient.callFire("Interrupt", [root.sessionId])

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

                Item { Layout.fillWidth: true }

                // Diff/files dock toggle — the worktree panel on the right.
                Button {
                    text: root.dockOpen ? "Dock ▸" : "◂ Dock"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.body
                    onClicked: root.dockOpen = !root.dockOpen

                    background: Rectangle {
                        color: parent.hovered ? Theme.colors.stateHover : "transparent"
                        border.width: root.dockOpen ? 1 : 0
                        border.color: Theme.colors.borderBrandFaint
                        radius: Theme.radius.sm
                    }

                    contentItem: Label {
                        text: parent.text
                        font: parent.font
                        color: root.dockOpen ? Theme.colors.brand : Theme.colors.textSecondary
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }
                }
            }
        }

        // Session settings strip
        SessionSettingsStrip {
            sessionState: root.sessionStateModel
        }

        // Transcript row: transcript fills; the diff/files dock docks right.
        RowLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

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
                toolId: model.toolId || ""
                toolArgs: model.toolArgs || ""
                toolStatus: model.toolStatus
                streaming: model.streaming
                reasoning: model.reasoning
                blocks: model.blocks || []
            }

            // Auto-scroll to bottom while at bottom
            property bool atBottom: atYEnd
            onCountChanged: {
                if (atBottom) {
                    Qt.callLater(positionViewAtEnd)
                }
            }

            // QML ListView's default wheel step is a few px per notch — felt
            // "stuck" on long transcripts. Take over wheel input: ~110px per
            // notch (VS Code-ish), clamped to content bounds.
            WheelHandler {
                acceptedDevices: PointerDevice.Mouse | PointerDevice.TouchPad
                onWheel: (wheel) => {
                    const step = wheel.angleDelta.y / 120
                    let y = transcriptListView.contentY - step * 110
                    const minY = transcriptListView.originY
                    const maxY = transcriptListView.originY
                        + transcriptListView.contentHeight
                        - transcriptListView.height
                    transcriptListView.contentY = Math.max(minY, Math.min(y, Math.max(minY, maxY)))
                }
            }

            // Larger offscreen delegate cache: markdown delegates are tall and
            // expensive to instantiate — pre-render a screenful each side.
            cacheBuffer: 1600

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

        // Diff/files dock — lazy so closed docks cost nothing.
        Loader {
            active: root.dockOpen
            visible: active
            Layout.preferredWidth: active ? Math.min(520, root.width * 0.42) : 0
            Layout.fillHeight: true
            sourceComponent: DockPanel {
                sessionDir: root.sessionStateModel ? root.sessionStateModel.dir : ""
                rpcClient: root.rpcRef
                onClosed: root.dockOpen = false
            }
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

                // Attachment preview (if image pasted)
                Loader {
                    active: attachedImage.length > 0
                    Layout.fillWidth: true
                    sourceComponent: RowLayout {
                        spacing: Theme.space.md

                        Rectangle {
                            width: 48
                            height: 48
                            color: Theme.colors.bgInset
                            radius: Theme.radius.sm
                            border.width: 1
                            border.color: Theme.colors.borderSubtle

                            Label {
                                anchors.centerIn: parent
                                text: "🖼"
                                font.pixelSize: Theme.fontSize.h2
                            }
                        }

                        Label {
                            text: "Image attached"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textSecondary
                            Layout.fillWidth: true
                        }

                        Button {
                            text: "✕"
                            font.pixelSize: Theme.fontSize.body
                            onClicked: attachedImage = ""

                            background: Rectangle {
                                color: parent.hovered ? Theme.colors.errorBg : "transparent"
                                radius: Theme.radius.sm
                                implicitWidth: 24
                                implicitHeight: 24
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

                ScrollView {
                    Layout.fillWidth: true
                    Layout.preferredHeight: Math.min(composerTextArea.contentHeight + Theme.space.md * 2, 120)
                    clip: true

                    TextArea {
                        id: composerTextArea
                        placeholderText: "Type a message (Enter to send, Shift+Enter for newline, / for commands)"
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

                        // Slash-command popup trigger
                        onTextChanged: {
                            if (text === "/" && cursorPosition === 1) {
                                slashPopup.filterText = ""
                                slashPopup.open()
                            } else if (slashPopup.opened && text.startsWith("/")) {
                                slashPopup.filterText = text.substring(1)
                            } else if (slashPopup.opened && !text.startsWith("/")) {
                                slashPopup.close()
                            }
                        }

                        // Image paste
                        Keys.onPressed: function(event) {
                            if ((event.key === Qt.Key_V) && (event.modifiers & Qt.ControlModifier)) {
                                // Paste event: check clipboard for image
                                var base64 = clipboardHelper.pasteImage()
                                if (base64.length > 0) {
                                    attachedImage = base64
                                    event.accepted = true
                                }
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
                        text: isStreaming ? "Steer" : "Send"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.medium
                        enabled: composerTextArea.text.trim().length > 0

                        onClicked: {
                            var msg = composerTextArea.text.trim()
                            if (msg.length === 0) return

                            var images = []
                            if (attachedImage.length > 0) {
                                images.push({"data": attachedImage, "mediaType": "image/png"})
                            }

                            if (isStreaming) {
                                // Steer (send while streaming)
                                rpcClient.callFire("SteerInput", [root.sessionId, msg, images])
                            } else {
                                // Normal send
                                rpcClient.callFire("SendInput", [root.sessionId, msg, images, []])
                            }

                            composerTextArea.text = ""
                            attachedImage = ""
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

    // Slash-command popup
    SlashCommandPopup {
        id: slashPopup
        x: composerTextArea.x
        y: composerTextArea.y - height - Theme.space.md
        commandsModel: root.commandsModel

        onCommandSelected: function(commandName) {
            composerTextArea.text = "/" + commandName + " "
            composerTextArea.cursorPosition = composerTextArea.text.length
            composerTextArea.forceActiveFocus()
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

    property string attachedImage: ""  // base64 image data
}
