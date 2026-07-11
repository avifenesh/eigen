import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs
import "Theme.js" as Theme

// Chat view for a single session (with tool cards, session settings, slash commands, image paste, steer)
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property string sessionId
    property var sessionStateModel  // SessionStateModel
    property var commandsModel  // CommandsModel
    property var transcriptModel: null
    property var approvalsModel: null
    property var rpcClient: null
    property var clipboardHelper: null
    property var highlighter: null
    property var terminalHelper: null
    property bool dockOpen: false
    property int dockTabIndex: 0
    property string actionError: ""
    property string dismissedSessionActionError: ""
    property string dismissedCommandsLoadError: ""
    readonly property string sessionActionError: sessionStateModel ? sessionStateModel.actionError : ""
    readonly property string visibleSessionActionError: sessionActionError !== "" && sessionActionError !== dismissedSessionActionError
        ? sessionActionError : ""
    readonly property string commandsLoadError: commandsModel ? commandsModel.loadError : ""
    readonly property string visibleCommandsLoadError: commandsLoadError !== "" && commandsLoadError !== dismissedCommandsLoadError
        ? commandsLoadError : ""
    readonly property string visibleActionError: visibleSessionActionError !== ""
        ? visibleSessionActionError
        : (visibleCommandsLoadError !== "" ? visibleCommandsLoadError : actionError)
    property string inputMode: "steer"
    property var queuedInputs: []
    readonly property int qaTranscriptRows: transcriptListView.count
    readonly property real qaTranscriptContentHeight: transcriptListView.contentHeight
    readonly property real qaTranscriptContentY: transcriptListView.contentY
    readonly property bool qaTranscriptAtBottom: transcriptListView.atVisualEnd()
    readonly property bool qaTranscriptStickToBottom: transcriptListView.stickToBottom
    readonly property bool qaSlashPopupOpen: slashPopup.opened
    readonly property bool qaSlashPopupInsideWindow: slashPopup.qaPopupInsideWindow
    readonly property int qaSlashCommandCount: slashPopup.qaCommandCount
    readonly property string qaSlashSelectedCommand: slashPopup.qaSelectedCommand
    readonly property string qaInputMode: inputMode
    readonly property int queuedInputCount: queuedInputs ? queuedInputs.length : 0
    readonly property int qaQueuedInputCount: queuedInputCount
    readonly property bool showEmptyStarter: transcriptListView.count === 0
        && !root.isStreaming
        && root.sessionStateModel
        && String(root.sessionStateModel.model || "") !== ""
    readonly property bool qaEmptyStarterVisible: showEmptyStarter
    readonly property int qaStarterPromptCount: starterPrompts.length
    readonly property var starterPrompts: [
        "Give me a tour of this codebase.",
        "What changed in the last few commits?",
        "Find and explain the riskiest function here."
    ]
    // Context property captured under an unshadowed name: inside
    // `DockPanel { rpcClient: ... }` a bare `rpcClient` RHS resolves to
    // DockPanel's OWN property (self-binding → undefined) — the QML
    // delegate-scope footgun, third sighting in this port.
    property var rpcRef: rpcClient
    property int approvalRows: 0
    property var slashTokens: ({})
    property var actionTokens: ({})
    property var recentProjectDirs: []
    property string newSessionDir: ""
    property bool newSessionSelectionTouched: false
    property int newSessionToken: 0
    property int recentDirsToken: 0
    readonly property int qaRecentProjectCount: recentProjectDirs ? recentProjectDirs.length : 0
    readonly property string qaNewSessionDir: newSessionDir
    readonly property bool qaNewSessionPopupOpen: newSessionPopup.opened
    readonly property bool qaNewSessionPopupInBounds: newSessionPopup.x >= 0
        && newSessionPopup.y >= 0
        && newSessionPopup.x + newSessionPopup.width <= root.width
        && newSessionPopup.y + newSessionPopup.height <= root.height
    readonly property bool qaNewSessionControlsFit: !newSessionDirLabel.truncated
        && newSessionBrowseButton.qaTextFits
        && newSessionRefreshButton.qaTextFits
        && newSessionCancelButton.qaTextFits
        && newSessionStartButton.qaTextFits

    signal backClicked()
    signal routeRequested(string route)
    signal railToggleRequested()
    signal sessionStarted(string sessionId)

    onApprovalsModelChanged: Qt.callLater(refreshApprovalRows)
    Component.onCompleted: refreshApprovalRows()
    onIsStreamingChanged: {
        if (!root.isStreaming) Qt.callLater(root.drainQueuedInput)
    }
    onSessionActionErrorChanged: {
        if (root.sessionActionError === "") {
            root.dismissedSessionActionError = ""
        }
    }
    onCommandsLoadErrorChanged: {
        if (root.commandsLoadError === "") {
            root.dismissedCommandsLoadError = ""
        }
    }

    Connections {
        target: root.approvalsModel
        function onModelReset() { root.refreshApprovalRows() }
        function onRowsInserted(parent, first, last) { root.refreshApprovalRows() }
        function onRowsRemoved(parent, first, last) { root.refreshApprovalRows() }
    }

    Connections {
        target: root.rpcClient ? root.rpcClient : null
        function onConnected() {
            if (newSessionPopup.visible && root.recentProjectDirs.length === 0 && root.recentDirsToken === 0) {
                root.refreshRecentProjectDirs()
            }
        }
        function onCallDone(token, payload) {
            if (token === root.recentDirsToken) {
                root.recentDirsToken = 0
                root.handleRecentProjectDirs(payload || {})
                return
            }
            if (token === root.newSessionToken) {
                root.newSessionToken = 0
                root.handleNewSessionResult(payload || {})
                return
            }
            var pending = root.slashTokens[token]
            if (pending) {
                var nextSlash = Object.assign({}, root.slashTokens)
                delete nextSlash[token]
                root.slashTokens = nextSlash
                root.handleSlashRpcResult(pending, payload || {})
                return
            }
            pending = root.actionTokens[token]
            if (!pending) return
            var nextAction = Object.assign({}, root.actionTokens)
            delete nextAction[token]
            root.actionTokens = nextAction
            root.handleActionRpcResult(pending, payload || {})
        }
    }

    Connections {
        target: root.sessionStateModel ? root.sessionStateModel : null
        function onDirChanged() {
            if (newSessionPopup.visible && !root.newSessionSelectionTouched) {
                root.chooseDefaultNewSessionDir()
            }
        }
    }

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

                AppButton {
                    objectName: "chatBackButton"
                    text: "← Back"
                    variant: "ghost"
                    toolTipText: "Back to sessions"
                    onClicked: root.backClicked()
                }

                AppButton {
                    objectName: "chatNewSessionButton"
                    text: root.newSessionToken !== 0 ? "Starting…" : "New chat"
                    variant: "secondary"
                    enabled: root.newSessionToken === 0
                    toolTipText: "Start a new chat in a chosen project directory"
                    onClicked: root.openNewSessionPicker()
                }

                AppButton {
                    objectName: "chatInterruptButton"
                    text: "Interrupt"
                    variant: "danger"
                    toolTipText: "Interrupt current turn"
                    visible: isStreaming
                    onClicked: root.fireRpcAction("Interrupt", [root.sessionId], "Could not interrupt")
                }

                Rectangle {
                    objectName: "chatActivityIndicator"
                    Layout.preferredHeight: 28
                    Layout.minimumWidth: 108
                    implicitWidth: activityRow.implicitWidth + Theme.space.lg * 2
                    radius: Theme.radius.full
                    color: root.isStreaming ? Theme.colors.stateSelected : Theme.colors.bgInset
                    border.width: 1
                    border.color: root.isStreaming ? Theme.colors.borderBrandFaint : Theme.colors.borderHairline

                    RowLayout {
                        id: activityRow
                        anchors.centerIn: parent
                        spacing: Theme.space.sm

                        Rectangle {
                            width: 8
                            height: 8
                            radius: 4
                            color: root.isStreaming ? Theme.colors.working : Theme.colors.dotIdle
                            SequentialAnimation on opacity {
                                running: Theme.continuousMotion && root.isStreaming
                                loops: Animation.Infinite
                                NumberAnimation { from: 0.45; to: 1.0; duration: 750; easing.type: Easing.InOutQuad }
                                NumberAnimation { from: 1.0; to: 0.45; duration: 750; easing.type: Easing.InOutQuad }
                            }
                        }

                        Label {
                            objectName: "chatActivityLabel"
                            text: root.activityLabel
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            font.weight: Theme.fontWeight.semibold
                            color: root.isStreaming ? Theme.colors.brandBright : Theme.colors.textMuted
                            elide: Text.ElideRight
                            Layout.maximumWidth: 150
                        }
                    }
                }

                Item { Layout.fillWidth: true }

                // Worktree/session dock toggle — the panel on the right.
                AppButton {
                    objectName: "chatDockToggleButton"
                    text: root.dockOpen ? "Dock ▸" : "◂ Dock"
                    variant: root.dockOpen ? "secondary" : "ghost"
                    selected: root.dockOpen
                    toolTipText: root.dockOpen ? "Close worktree dock" : "Open worktree dock"
                    onClicked: {
                        if (root.dockOpen) {
                            root.dockOpen = false
                        } else {
                            root.openWorktreeDock(root.dockTabIndex)
                        }
                    }
                }
            }
        }

        // Session settings strip
        SessionSettingsStrip {
            sessionState: root.sessionStateModel
        }

        // Transcript row: transcript fills; the worktree/session dock docks right.
        RowLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

        Item {
            Layout.fillWidth: true
            Layout.fillHeight: true

            ListView {
                id: transcriptListView
                objectName: "chatTranscriptList"
                anchors.fill: parent
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
                    voiceTtsAvailable: voiceControls.ttsAvailable && !voiceControls.modeOn
                    voiceSpeaking: voiceControls.speaking && voiceControls.speakingText === model.text
                    onSpeakRequested: function(message) {
                        voiceControls.toggleSpeak(message)
                    }
                }

                // Auto-scroll only while pinned. Track the user's intent before
                // new content grows; checking atYEnd after an insert is too late.
                property bool stickToBottom: true

                Component.onCompleted: schedulePositionAtEnd()
                onCountChanged: schedulePositionAtEnd()
                onContentHeightChanged: schedulePositionAtEnd()
                onHeightChanged: schedulePositionAtEnd()
                onMovementEnded: stickToBottom = atVisualEnd()

                Timer {
                    id: transcriptAutoScrollTimer
                    interval: 16
                    repeat: false
                    onTriggered: {
                        if (transcriptListView.stickToBottom) {
                            transcriptListView.positionViewAtEnd()
                        }
                    }
                }

                function schedulePositionAtEnd() {
                    if (!stickToBottom) return
                    transcriptAutoScrollTimer.restart()
                }

                function atVisualEnd() {
                    const maxY = Math.max(originY, originY + contentHeight - height)
                    return contentY >= maxY - 1
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
                        transcriptListView.stickToBottom = transcriptListView.atVisualEnd()
                    }
                }

                // Larger offscreen delegate cache: markdown delegates are tall and
                // expensive to instantiate — pre-render a screenful each side.
                cacheBuffer: 1600

                // Approval overlay (if pending approvals)
                Loader {
                    id: approvalLoader
                    z: 50
                    active: root.approvalRows > 0
                    anchors.fill: parent
                    sourceComponent: Item {
                        anchors.fill: parent

                        ApprovalOverlay {
                            anchors.centerIn: parent
                            model: root.approvalsModel
                            onApprove: function(approvalId, allow) {
                                root.approvalsModel.approve(approvalId, allow)
                            }
                        }
                    }
                }
            }

            Column {
                id: chatEmptyStarter
                objectName: "chatEmptyStarter"
                visible: root.showEmptyStarter
                anchors.centerIn: parent
                width: Math.min(520, Math.max(0, parent.width - Theme.space.xxxxl * 2))
                spacing: Theme.space.md
                z: 20

                Label {
                    text: "Ready when you are."
                    width: parent.width
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h2
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textPrimary
                }

                Label {
                    text: "Start with a focused prompt."
                    width: parent.width
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                }

                Repeater {
                    model: root.starterPrompts
                    delegate: AppButton {
                        objectName: "chatStarterPrompt_" + index
                        width: chatEmptyStarter.width
                        height: implicitHeight
                        text: String(modelData || "")
                        variant: "secondary"
                        contentAlignment: Text.AlignLeft
                        toolTipText: "Use this prompt"
                        onClicked: root.useStarterPrompt(text)
                    }
                }
            }
        }

        // Worktree/session dock — lazy so closed docks cost nothing.
        Loader {
            active: root.dockOpen
            visible: active
            Layout.preferredWidth: active ? Math.min(520, root.width * 0.42) : 0
            Layout.fillHeight: true
            sourceComponent: DockPanel {
                sessionDir: root.sessionStateModel ? root.sessionStateModel.dir : ""
                rpcClient: root.rpcRef
                terminalHelper: root.terminalHelper
                sessionStateModel: root.sessionStateModel
                preferredTab: root.dockTabIndex
                onClosed: root.dockOpen = false
            }
        }
        }

        Rectangle {
            id: voiceStatusBar
            objectName: "chatVoiceStatusBar"
            visible: voiceControls.modeOn
            Layout.fillWidth: true
            Layout.preferredHeight: visible ? Math.max(34, voiceStatusRow.implicitHeight + Theme.space.sm * 2) : 0
            color: Theme.colors.workingBg
            border.width: visible ? 1 : 0
            border.color: Theme.colors.borderBrandFaint

            RowLayout {
                id: voiceStatusRow
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xl
                anchors.rightMargin: Theme.space.lg
                spacing: Theme.space.md

                Rectangle {
                    width: 8
                    height: 8
                    radius: 4
                    color: voiceControls.phase === "error" ? Theme.colors.error
                        : (voiceControls.phase === "thinking" ? Theme.colors.working : Theme.colors.brandBright)
                }

                Label {
                    objectName: "chatVoiceStatusLabel"
                    text: root.voicePhaseLabel(voiceControls.phase)
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textPrimary
                }

                Label {
                    objectName: "chatVoiceHeardLabel"
                    visible: voiceControls.lastText !== "" && voiceControls.phase !== "speaking"
                    text: "\"" + voiceControls.lastText + "\""
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textSecondary
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }

                Item { Layout.fillWidth: !voiceControls.lastText || voiceControls.phase === "speaking" }

                AppButton {
                    objectName: "chatStopVoiceModeButton"
                    text: "End"
                    compact: true
                    variant: "ghost"
                    toolTipText: "End voice conversation"
                    onClicked: voiceControls.stopMode(false)
                }
            }
        }

        // Composer
        Rectangle {
            id: composerPanel
            Layout.fillWidth: true
            Layout.preferredHeight: composerColumn.implicitHeight + Theme.space.lg * 2
            Layout.minimumHeight: composerColumn.implicitHeight + Theme.space.lg * 2
            color: Theme.colors.bgWell
            border.width: 1
            border.color: Theme.colors.borderHairline
            clip: true

            readonly property int minimumTextHeight: 44

            ColumnLayout {
                id: composerColumn
                anchors.fill: parent
                anchors.margins: Theme.space.lg
                spacing: Theme.space.md

                // Attachment preview (if image pasted)
                Loader {
                    active: attachedImage.length > 0
                    Layout.fillWidth: true
                    sourceComponent: RowLayout {
                        spacing: Theme.space.md

                        Rectangle {
                            objectName: "chatAttachmentPreview"
                            width: 48
                            height: 48
                            color: Theme.colors.bgInset
                            radius: Theme.radius.sm
                            border.width: 1
                            border.color: Theme.colors.borderSubtle
                            clip: true

                            Image {
                                id: attachmentPreviewImage
                                objectName: "chatAttachmentPreviewImage"
                                anchors.fill: parent
                                anchors.margins: 3
                                source: attachedImage.length > 0 ? "data:image/png;base64," + attachedImage : ""
                                sourceSize.width: 96
                                sourceSize.height: 96
                                fillMode: Image.PreserveAspectFit
                                smooth: true
                                cache: false

                                readonly property bool qaImageReady: status === Image.Ready
                                readonly property bool qaImageError: status === Image.Error
                                readonly property int qaSourceSizeWidth: sourceSize.width
                                readonly property int qaSourceSizeHeight: sourceSize.height
                                readonly property real qaPaintedWidth: paintedWidth
                                readonly property real qaPaintedHeight: paintedHeight
                            }

                            Label {
                                objectName: "chatAttachmentPreviewFallback"
                                visible: !attachmentPreviewImage.qaImageReady
                                anchors.centerIn: parent
                                text: "img"
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.micro
                                color: Theme.colors.textMuted
                            }
                        }

                        Label {
                            text: "Image attached"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textSecondary
                            Layout.fillWidth: true
                        }

                        AppButton {
                            objectName: "chatClearAttachmentButton"
                            text: "✕"
                            compact: true
                            variant: "danger"
                            toolTipText: "Remove image"
                            onClicked: attachedImage = ""
                        }
                    }
                }

                Rectangle {
                    objectName: "chatActionError"
                    visible: root.visibleActionError !== ""
                    z: visible ? 20 : 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? Math.max(36, chatActionErrorRow.implicitHeight + Theme.space.md) : 0
                    color: Theme.colors.errorBg
                    border.width: visible ? 1 : 0
                    border.color: Theme.colors.error
                    radius: Theme.radius.sm
                    clip: true

                    RowLayout {
                        id: chatActionErrorRow
                        anchors.fill: parent
                        anchors.leftMargin: Theme.space.lg
                        anchors.rightMargin: Theme.space.lg
                        spacing: Theme.space.md

                        Label {
                            objectName: "chatActionErrorText"
                            text: root.visibleActionError
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.error
                            wrapMode: Text.Wrap
                            Layout.fillWidth: true
                        }

                        AppButton {
                            objectName: "chatDismissActionError"
                            text: "X"
                            compact: true
                            toolTipText: "Dismiss chat action error"
                            onClicked: {
                                root.dismissedSessionActionError = root.sessionActionError
                                root.dismissedCommandsLoadError = root.commandsLoadError
                                root.actionError = ""
                            }
                            Layout.preferredWidth: 28
                            Layout.preferredHeight: 28
                        }
                    }
                }

                ScrollView {
                    id: composerTextScroll
                    Layout.fillWidth: true
                    Layout.preferredHeight: Math.min(composerTextArea.contentHeight + composerTextArea.topPadding + composerTextArea.bottomPadding, 120)
                    Layout.minimumHeight: composerPanel.minimumTextHeight
                    clip: true

                    AppTextArea {
                        id: composerTextArea
                        objectName: "chatComposerTextArea"
                        placeholderText: "Type a message (Enter to send, Shift+Enter for newline, / for commands)"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        wrapMode: TextArea.Wrap
                        backgroundColor: Theme.colors.surfaceRaised
                        borderColor: "transparent"
                        focusBorderColor: Theme.colors.borderBrand
                        normalBorderWidth: 0
                        focusedBorderWidth: 1
                        backgroundRadius: Theme.radius.md

                        Keys.onReturnPressed: function(event) {
                            if (event.modifiers & Qt.ShiftModifier) {
                                // Allow default (newline)
                                event.accepted = false
                            } else if (slashPopup.opened && slashPopup.hasSelection()) {
                                slashPopup.acceptSelection()
                                event.accepted = true
                            } else {
                                root.sendComposer()
                                event.accepted = true
                            }
                        }

                        // Slash-command popup trigger
                        onTextChanged: {
                            root.syncSlashPopup()
                        }
                        onCursorPositionChanged: root.syncSlashPopup()

                        // Image paste
                        Keys.onPressed: function(event) {
                            if (slashPopup.opened) {
                                if (event.key === Qt.Key_Down) {
                                    slashPopup.moveSelection(1)
                                    event.accepted = true
                                    return
                                }
                                if (event.key === Qt.Key_Up) {
                                    slashPopup.moveSelection(-1)
                                    event.accepted = true
                                    return
                                }
                                if (event.key === Qt.Key_Tab) {
                                    slashPopup.acceptSelection()
                                    event.accepted = true
                                    return
                                }
                                if (event.key === Qt.Key_Escape) {
                                    slashPopup.close()
                                    event.accepted = true
                                    return
                                }
                            }
                            if ((event.key === Qt.Key_V) && (event.modifiers & Qt.ControlModifier)) {
                                // Paste event: check clipboard for image
                                var base64 = root.clipboardHelper ? root.clipboardHelper.pasteImage() : ""
                                if (base64.length > 0) {
                                    attachedImage = base64
                                    event.accepted = true
                                }
                            }
                        }
                    }
                }

                RowLayout {
                    id: composerActionsRow
                    Layout.fillWidth: true
                    spacing: Theme.space.md

                    VoiceControls {
                        id: voiceControls
                        objectName: "chatVoiceControls"
                        rpcClient: root.rpcClient
                        sessionId: root.sessionId
                        Layout.preferredWidth: implicitWidth
                        Layout.preferredHeight: implicitHeight

                        onDictated: function(heard) {
                            var draft = composerTextArea.text.replace(/\s+$/, "")
                            composerTextArea.text = draft ? draft + " " + heard : heard
                            composerTextArea.forceActiveFocus()
                        }
                        onActionFailed: function(message) {
                            root.actionError = message
                        }
                    }

                    Item { Layout.fillWidth: true }

                    AppButton {
                        id: inputModeButton
                        objectName: "chatInputModeButton"
                        text: root.inputMode === "queue" ? "Queue" : "Steer"
                        badgeText: root.queuedInputCount > 0 ? String(root.queuedInputCount) : ""
                        variant: root.inputMode === "queue" ? "secondary" : "ghost"
                        selected: root.inputMode === "queue"
                        toolTipText: root.inputMode === "queue"
                            ? "Queue mid-turn messages until the current turn finishes"
                            : "Steer mid-turn messages into the current turn"
                        Layout.preferredWidth: 92

                        onClicked: {
                            root.switchInputMode(root.inputMode === "queue" ? "steer" : "queue", true)
                        }
                    }

                    AppButton {
                        id: sendButton
                        objectName: "chatSendButton"
                        text: isStreaming ? (root.inputMode === "queue" ? "Queue" : "Steer") : "Send"
                        variant: "primary"
                        toolTipText: isStreaming
                            ? (root.inputMode === "queue" ? "Queue for the next turn" : "Steer current turn")
                            : "Send message"
                        enabled: composerTextArea.text.trim().length > 0
                        Layout.preferredWidth: 88

                        onClicked: {
                            root.sendComposer()
                        }
                    }
                }
            }
        }
    }

    Popup {
        id: newSessionPopup
        objectName: "chatNewSessionPopup"
        modal: true
        focus: true
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
        width: Math.min(520, root.width - Theme.space.xl * 2)
        x: Math.max(Theme.space.lg, (root.width - width) / 2)
        y: Math.max(Theme.space.lg, 72)
        padding: Theme.space.xl
        background: Rectangle {
            color: Theme.colors.surfaceRaised
            radius: Theme.radius.lg
            border.width: 1
            border.color: Theme.colors.borderSubtle
        }

        ColumnLayout {
            width: parent.width
            spacing: Theme.space.lg

            Label {
                text: "Start new chat"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.h3
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textPrimary
                Layout.fillWidth: true
            }

            Label {
                text: "Choose the project directory for the new session."
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textMuted
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }

            ColumnLayout {
                Layout.fillWidth: true
                spacing: Theme.space.sm

                Label {
                    text: "Project"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textFaint
                }

                ScrollView {
                    id: recentProjectsScroll
                    visible: root.recentProjectDirs && root.recentProjectDirs.length > 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible
                        ? Math.min(recentProjectsColumn.implicitHeight, Math.max(64, root.height - 285))
                        : 0
                    clip: true
                    ScrollBar.horizontal.policy: ScrollBar.AlwaysOff

                    ColumnLayout {
                        id: recentProjectsColumn
                        width: recentProjectsScroll.availableWidth
                        spacing: Theme.space.sm

                        Repeater {
                            model: root.recentProjectDirs || []
                            delegate: AppButton {
                                objectName: "chatRecentProject_" + index
                                Layout.fillWidth: true
                                contentAlignment: Text.AlignLeft
                                variant: root.newSessionDir === String(modelData.dir || "") ? "secondary" : "ghost"
                                selected: root.newSessionDir === String(modelData.dir || "")
                                text: (modelData.name || modelData.dir || "project") + "  ·  " + (modelData.dir || "")
                                toolTipText: modelData.dir || ""
                                onClicked: root.selectNewSessionDir(modelData.dir || "")
                            }
                        }
                    }
                }

                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredHeight: 38
                    color: Theme.colors.bgInset
                    radius: Theme.radius.sm
                    border.width: 1
                    border.color: root.newSessionDir !== "" ? Theme.colors.borderBrandFaint : Theme.colors.borderHairline

                    Label {
                        id: newSessionDirLabel
                        objectName: "chatNewSessionDirLabel"
                        anchors.fill: parent
                        anchors.leftMargin: Theme.space.lg
                        anchors.rightMargin: Theme.space.lg
                        text: root.newSessionDir !== "" ? root.newSessionDir : "No project selected"
                        font.family: root.newSessionDir !== "" ? Theme.monoFonts[0] : Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: root.newSessionDir !== "" ? Theme.colors.textPrimary : Theme.colors.textMuted
                        verticalAlignment: Text.AlignVCenter
                        elide: Text.ElideMiddle
                    }
                }
            }

            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.space.md

                AppButton {
                    id: newSessionBrowseButton
                    objectName: "chatBrowseProjectButton"
                    text: "Browse…"
                    variant: "secondary"
                    onClicked: projectFolderDialog.open()
                }

                AppButton {
                    id: newSessionRefreshButton
                    objectName: "chatRefreshProjectsButton"
                    text: root.recentDirsToken !== 0 ? "Loading…" : "Refresh"
                    variant: "ghost"
                    enabled: root.recentDirsToken === 0
                    onClicked: root.refreshRecentProjectDirs()
                }

                Item { Layout.fillWidth: true }

                AppButton {
                    id: newSessionCancelButton
                    objectName: "chatCancelNewSessionButton"
                    text: "Cancel"
                    variant: "ghost"
                    onClicked: newSessionPopup.close()
                }

                AppButton {
                    id: newSessionStartButton
                    objectName: "chatStartProjectSessionButton"
                    text: root.newSessionToken !== 0 ? "Starting…" : "Start"
                    variant: "primary"
                    enabled: root.newSessionToken === 0 && root.newSessionDir !== ""
                    onClicked: root.startNewSessionInProject(root.newSessionDir)
                }
            }
        }
    }

    FolderDialog {
        id: projectFolderDialog
        title: "Choose project directory"
        currentFolder: root.newSessionDir !== "" ? "file://" + root.newSessionDir : "file:///home"
        onAccepted: {
            var raw = selectedFolder.toString().replace(/^file:\/\//, "")
            root.selectNewSessionDir(decodeURIComponent(raw))
        }
    }

    // Slash-command popup
    SlashCommandPopup {
        id: slashPopup
        width: Math.max(180, Math.min(420, root.width - Theme.space.lg * 2))
        x: root.slashPopupX()
        y: root.slashPopupY()
        commandsModel: root.commandsModel

        onCommandSelected: function(commandName) {
            root.completeSlashCommand(commandName)
        }
    }

    function slashPopupX() {
        var point = composerTextArea.mapToItem(root, 0, 0)
        return Math.max(Theme.space.lg, Math.min(point.x, root.width - slashPopup.width - Theme.space.lg))
    }

    function slashPopupY() {
        var point = composerTextArea.mapToItem(root, 0, 0)
        return Math.max(Theme.space.lg, point.y - slashPopup.height - Theme.space.md)
    }

    function voicePhaseLabel(phase) {
        switch (phase) {
        case "listening": return "Listening"
        case "transcribing": return "Transcribing"
        case "thinking": return "Thinking"
        case "speaking": return "Speaking"
        case "error": return "Voice needs attention"
        default: return "Voice ready"
        }
    }

    function syncSlashPopup() {
        var filter = slashFilter()
        if (filter === null || !root.commandsModel) {
            if (slashPopup.opened) slashPopup.close()
            return
        }
        slashPopup.filterText = filter
        if (!slashPopup.opened) {
            slashPopup.open()
        }
    }

    function slashFilter() {
        if (composerTextArea.cursorPosition < 1 || !composerTextArea.text.startsWith("/")) {
            return null
        }
        var token = composerTextArea.text.substring(1, composerTextArea.cursorPosition)
        if (token.indexOf(" ") >= 0 || token.indexOf("\n") >= 0 || token.indexOf("\t") >= 0) {
            return null
        }
        return token
    }

    function completeSlashCommand(commandName) {
        var cursor = composerTextArea.cursorPosition
        var tail = composerTextArea.text.substring(cursor)
        composerTextArea.text = "/" + commandName + " " + tail.replace(/^\s+/, "")
        composerTextArea.cursorPosition = Math.min(composerTextArea.text.length, commandName.length + 2)
        slashPopup.close()
        Qt.callLater(function() {
            composerTextArea.cursorPosition = composerTextArea.text.length
            composerTextArea.forceActiveFocus()
        })
    }

    function fireRpcAction(method, args, errorPrefix, meta) {
        if (!root.rpcClient) {
            root.actionError = errorPrefix + ": RPC client is unavailable."
            return false
        }
        root.actionError = ""
        if (typeof root.rpcClient.callToken === "function") {
            var token = root.rpcClient.callToken(method, args || [])
            var next = Object.assign({}, root.actionTokens)
            next[token] = Object.assign({"method": method, "errorPrefix": errorPrefix}, meta || {})
            root.actionTokens = next
            return true
        }
        if (typeof root.rpcClient.callFire === "function") {
            root.rpcClient.callFire(method, args || [])
            return true
        }
        root.actionError = errorPrefix + ": RPC client is unavailable."
        return false
    }

    function handleActionRpcResult(pending, payload) {
        var error = root.payloadError(payload)
        if (error === "") return
        root.actionError = (pending.errorPrefix || "Action failed") + ": " + error
        if (pending.restoreText && composerTextArea.text.trim().length === 0) {
            composerTextArea.text = String(pending.restoreText)
            composerTextArea.cursorPosition = composerTextArea.text.length
        }
        if (pending.restoreImage && attachedImage.length === 0) {
            attachedImage = String(pending.restoreImage)
        }
        if (pending.restoreQueue) {
            var restored = root.queuedInputs ? root.queuedInputs.slice() : []
            restored.unshift(pending.restoreQueue)
            root.queuedInputs = restored
        }
    }

    function openNewSessionPicker() {
        root.actionError = ""
        root.newSessionSelectionTouched = false
        root.chooseDefaultNewSessionDir()
        if ((!root.recentProjectDirs || root.recentProjectDirs.length === 0) && root.recentDirsToken === 0) {
            root.refreshRecentProjectDirs()
        }
        newSessionPopup.open()
    }

    function refreshRecentProjectDirs() {
        if (!root.rpcClient || typeof root.rpcClient.callToken !== "function" || root.recentDirsToken !== 0) return
        root.recentDirsToken = root.rpcClient.callToken("RecentDirs", [])
    }

    function handleRecentProjectDirs(payload) {
        var error = root.payloadError(payload)
        if (error !== "") {
            root.actionError = "Could not load recent projects: " + error
            return
        }
        var dirs = payload && payload.result ? payload.result : []
        var normalized = []
        var seen = ({})
        if (dirs && dirs.length !== undefined) {
            for (var i = 0; i < dirs.length; i++) {
                var item = dirs[i] || {}
                var dir = String(item.dir || "")
                if (dir.trim() === "" || seen[dir]) continue
                seen[dir] = true
                normalized.push({"dir": dir, "name": String(item.name || dir)})
            }
        }
        root.recentProjectDirs = normalized
        if (!root.newSessionSelectionTouched) {
            root.chooseDefaultNewSessionDir()
        }
    }

    function chooseDefaultNewSessionDir() {
        var hydratedDir = root.sessionStateModel ? String(root.sessionStateModel.dir || "") : ""
        if (hydratedDir !== "") {
            root.newSessionDir = hydratedDir
            return
        }
        root.newSessionDir = root.recentProjectDirs && root.recentProjectDirs.length > 0
            ? String(root.recentProjectDirs[0].dir || "")
            : ""
    }

    function selectNewSessionDir(dir) {
        root.newSessionSelectionTouched = true
        root.newSessionDir = String(dir || "")
    }

    function startNewSessionInProject(dir) {
        dir = String(dir || "")
        if (dir === "") {
            root.actionError = "Choose a project directory before starting a new chat."
            return false
        }
        if (!root.rpcClient || typeof root.rpcClient.callToken !== "function") {
            root.actionError = "Could not start session: RPC client is unavailable."
            return false
        }
        if (root.newSessionToken !== 0) return false
        root.actionError = ""
        root.newSessionToken = root.rpcClient.callToken("NewSession", [dir, "", ""])
        return true
    }

    function handleNewSessionResult(payload) {
        var error = root.payloadError(payload)
        if (error !== "") {
            root.actionError = "Could not start session: " + error
            return
        }
        var sessionId = payload ? String(payload.result || "") : ""
        if (sessionId === "") {
            root.actionError = "Could not start session: the daemon returned no session id."
            return
        }
        newSessionPopup.close()
        root.sessionStarted(sessionId)
    }

    function appendSlashNote(text) {
        root.actionError = ""
        if (root.transcriptModel && typeof root.transcriptModel.appendNote === "function") {
            root.transcriptModel.appendNote(text)
        } else {
            root.actionError = text
        }
    }

    function openWorktreeDock(tabIndex) {
        if (!root.rpcClient) {
            root.actionError = "Could not open dock: RPC client is unavailable."
            return false
        }
        var nextTab = Number(tabIndex)
        if (isNaN(nextTab)) nextTab = 0
        root.dockTabIndex = Math.max(0, Math.min(4, nextTab))
        root.actionError = ""
        root.dockOpen = true
        return true
    }

    function parseSlashCommand(text) {
        var match = text.match(/^\/([A-Za-z0-9][\w-]*)(?:\s+([\s\S]*))?$/)
        if (!match) return null
        return {"rawName": match[1], "name": match[1].toLowerCase(), "args": (match[2] || "").trim()}
    }

    function slashHelpText() {
        return [
            "Qt slash commands",
            "/help - show this list",
            "/model [id] - show or switch the model",
            "/perm [gated|auto] - show or set permission posture",
            "/effort [level] - show or set reasoning effort",
            "/search [off|auto|on] - show or set live search",
            "/fast [on|off] - toggle fast tier",
            "/goal [text|clear] - show, set, or clear the session goal",
            "/route [on|off] - show or set model-assessed routing",
            "/config [key [value]] - inspect or set a config field",
            "/rename <title> - rename this session",
            "/clear - clear the visible conversation",
            "/compact - compact older context",
            "/save or /export - export this session transcript",
            "/add-dir [path] - list or grant a working directory",
            "/workflow [name k=v...] - list or run an authored workflow",
            "/ban <title>: <rule> and /unban <title> - manage project memory bans",
            "/find <text> - count matches in the visible transcript",
            "/voice [doctor], /mute, /dictate, /speak - voice controls",
            "/plugins, /hooks, /observe - summarize plugin and telemetry state",
            "/tools - list tools available to this session",
            "/copy - copy the last assistant answer",
            "/review [target] - ask for a cross-vendor review",
            "/steer or /queue - choose what Enter does during a running turn",
            "/background, /rail, /term, /shells, /loop, /mouse, /rebuild, /quit - local GUI helpers",
            "/home /sessions /tasks /skills /memory /notes /connectors /config /reviewers /board /live - open a view",
            "/changes - toggle the worktree dock",
            "Custom commands from .eigen/commands and ~/.eigen/commands run as authored prompts.",
            "Unknown slash commands stay local until they match an authored command."
        ].join("\n")
    }

    function arrayContains(values, needle) {
        if (!values) return false
        for (var i = 0; i < values.length; i++) {
            if (String(values[i]) === needle) return true
        }
        return false
    }

    function joinValues(values, fallback) {
        if (!values || values.length === 0) return fallback
        var out = []
        for (var i = 0; i < values.length; i++) out.push(String(values[i]))
        return out.join("|")
    }

    function firstValues(values, limit) {
        if (!values || values.length === 0) return ""
        var out = []
        for (var i = 0; i < values.length && i < limit; i++) out.push(String(values[i]))
        return out.join(", ")
    }

    function onOffValue(value) {
        switch (String(value || "").toLowerCase()) {
        case "on":
        case "true":
        case "1":
        case "yes":
            return true
        case "off":
        case "false":
        case "0":
        case "no":
            return false
        default:
            return null
        }
    }

    function currentSessionField(field, fallback) {
        if (!root.sessionStateModel) return fallback
        var value = root.sessionStateModel[field]
        if (value === undefined || value === null || String(value) === "") return fallback
        return String(value)
    }

    function sessionRootsNote() {
        var roots = root.sessionStateModel ? root.sessionStateModel.roots : []
        if (!roots || roots.length === 0) return "no working directories reported"
        var out = ["working directories:"]
        for (var i = 0; i < roots.length; i++) out.push("  " + roots[i])
        out.push("(/add-dir <path> to grant another)")
        return out.join("\n")
    }

    function sessionToolsNote() {
        var tools = root.sessionStateModel ? root.sessionStateModel.tools : []
        if (!tools || tools.length === 0) return "no tools"
        var out = ["tools:"]
        for (var i = 0; i < tools.length; i++) {
            var tool = tools[i] || {}
            var name = String(tool.name || "")
            if (!name) continue
            out.push("  " + (tool.read_only ? "- " : "* ") + name)
        }
        return out.length > 1 ? out.join("\n") : "no tools"
    }

    function workflowListNote(workflows) {
        if (!workflows || workflows.length === 0) {
            return "no workflows yet - author one under ~/.eigen/workflows/<name>.md"
        }
        var out = ["workflows:"]
        for (var i = 0; i < workflows.length; i++) {
            var wf = workflows[i] || {}
            var name = String(wf.name || "")
            if (!name) continue
            var steps = Number(wf.steps || 0)
            var line = "  - " + name + (steps > 0 ? " (" + steps + " step" + (steps === 1 ? "" : "s") + ")" : "")
            var desc = String(wf.description || "")
            if (desc) line += " - " + desc
            out.push(line)
        }
        out.push("/workflow <name> [k=v ...]")
        return out.length > 2 ? out.join("\n") : "no workflows yet - author one under ~/.eigen/workflows/<name>.md"
    }

    function workflowVars(fields) {
        var vars = ({})
        for (var i = 1; i < fields.length; i++) {
            var item = String(fields[i] || "")
            var eq = item.indexOf("=")
            if (eq > 0) vars[item.substring(0, eq)] = item.substring(eq + 1)
        }
        return vars
    }

    function runWorkflowSlash(args) {
        if (!root.requireSessionId("workflow")) return true
        var fields = args.split(/\s+/).filter(function(item) { return item.length > 0 })
        if (fields.length === 0) {
            return root.slashRpc("workflowList", "Workflows", [], {"command": "workflow"})
        }
        var name = fields[0]
        var vars = root.workflowVars(fields)
        if (!root.rpcClient || typeof root.rpcClient.callToken !== "function") {
            root.actionError = "Could not run /workflow: RPC client is unavailable."
            return true
        }
        root.appendSlashNote("workflow " + name + " started")
        return root.slashRpc("runWorkflow", "RunWorkflow", [root.sessionId, name, vars],
            {"command": "workflow", "name": name})
    }

    function banUsageNote() {
        return "/ban <title>: <rule> records a hard prohibition in project memory"
    }

    function splitBanArgs(args) {
        var text = String(args || "")
        var colon = text.indexOf(":")
        if (colon <= 0) return {"title": text.trim(), "rule": ""}
        return {
            "title": text.substring(0, colon).trim(),
            "rule": text.substring(colon + 1).trim()
        }
    }

    function runBanSlash(args) {
        if (!args) {
            root.routeRequested("memory")
            root.appendSlashNote(root.banUsageNote())
            return true
        }
        var parsed = root.splitBanArgs(args)
        if (!parsed.title || !parsed.rule) {
            root.appendSlashNote("usage: /ban <title>: <rule>")
            return true
        }
        return root.slashRpc("addBan", "AddBan", ["project", parsed.title, parsed.rule],
            {"command": "ban", "title": parsed.title})
    }

    function runUnbanSlash(args) {
        var title = String(args || "").trim()
        if (!title) {
            root.appendSlashNote("usage: /unban <title>")
            return true
        }
        return root.slashRpc("removeBan", "RemoveBan", ["project", title],
            {"command": "unban", "title": title})
    }

    function runBackgroundSlash() {
        if (!root.isStreaming) {
            root.appendSlashNote("nothing running to background")
            return true
        }
        root.routeRequested("home")
        root.appendSlashNote("moved to background - the daemon keeps running it; reattach from Sessions or Live")
        return true
    }

    function runRailSlash() {
        root.railToggleRequested()
        root.appendSlashNote("toggled navigation rail")
        return true
    }

    function countMatches(haystack, needle) {
        if (!needle) return 0
        var count = 0
        var start = 0
        while (true) {
            var idx = haystack.indexOf(needle, start)
            if (idx < 0) break
            count += 1
            start = idx + Math.max(needle.length, 1)
        }
        return count
    }

    function runFindSlash(args) {
        var query = String(args || "").trim()
        if (!query) {
            root.appendSlashNote("usage: /find <text>")
            return true
        }
        if (!root.transcriptModel || typeof root.transcriptModel.rowCount !== "function") {
            root.appendSlashNote("no transcript to search")
            return true
        }
        var needle = query.toLowerCase()
        var rows = root.transcriptModel.rowCount()
        var matchedRows = 0
        var matches = 0
        for (var i = 0; i < rows; i++) {
            var text = String(root.transcriptModel.data(root.transcriptModel.index(i, 0), 258) || "").toLowerCase()  // TextRole
            var n = root.countMatches(text, needle)
            if (n > 0) {
                matchedRows += 1
                matches += n
            }
        }
        if (matches === 0) {
            root.appendSlashNote("no matches for " + query)
        } else {
            root.appendSlashNote("find: " + query + " (" + matches + " match" + (matches === 1 ? "" : "es")
                + " in " + matchedRows + " row" + (matchedRows === 1 ? "" : "s") + ")")
        }
        return true
    }

    function switchInputMode(mode, announce) {
        root.inputMode = mode === "queue" ? "queue" : "steer"
        if (announce) {
            root.appendSlashNote(root.inputMode === "queue"
                ? "input mode -> queue (Enter waits for the next turn)"
                : "input mode -> steer (Enter injects into a running turn)")
        }
        return true
    }

    function setInputMode(mode) {
        return root.switchInputMode(mode, true)
    }

    function queueComposerInput(text, images) {
        var next = root.queuedInputs ? root.queuedInputs.slice() : []
        next.push({"text": text, "images": images || []})
        root.queuedInputs = next
        root.appendSlashNote("queued -> will send when the turn finishes (" + root.queuedInputCount + ")")
        return true
    }

    function drainQueuedInput() {
        if (root.isStreaming || root.queuedInputCount === 0 || !root.sessionId) return
        var queue = root.queuedInputs.slice()
        var next = queue.shift()
        root.queuedInputs = queue
        var images = next && next.images ? next.images : []
        var sent = root.fireRpcAction(
            "SendInput",
            [root.sessionId, String(next ? next.text || "" : ""), images, []],
            "Could not send queued message",
            {"restoreQueue": next}
        )
        if (!sent) {
            var restore = root.queuedInputs ? root.queuedInputs.slice() : []
            restore.unshift(next)
            root.queuedInputs = restore
        }
    }

    function lastAssistantText() {
        if (!root.transcriptModel || typeof root.transcriptModel.lastAssistantText !== "function") {
            return ""
        }
        return root.transcriptModel.lastAssistantText()
    }

    function copyLastAssistant() {
        var text = root.lastAssistantText()
        if (!text) {
            root.appendSlashNote("nothing to copy")
            return true
        }
        if (!root.clipboardHelper || typeof root.clipboardHelper.copyText !== "function") {
            root.actionError = "Could not copy answer: clipboard is unavailable."
            return true
        }
        root.clipboardHelper.copyText(text)
        root.appendSlashNote("copied " + text.length + " chars")
        return true
    }

    function runVoiceSlash(args) {
        var mode = String(args || "").trim().toLowerCase()
        if (mode === "doctor" || mode === "setup" || mode === "status") {
            return root.slashRpc("voiceStatus", "VoiceStatus", [], {"command": "voice"})
        }
        if (mode) {
            root.appendSlashNote("usage: /voice [doctor|setup|status]")
            return true
        }
        if (!root.requireSessionId("voice")) return true
        return root.slashRpc("voiceModeStart", "VoiceModeStart", [root.sessionId], {"command": "voice"})
    }

    function runMuteSlash() {
        return root.slashRpc("voiceModeStop", "VoiceModeStop", [], {"command": "mute"})
    }

    function runDictateSlash(commandName) {
        if (!root.requireSessionId(commandName)) return true
        return root.slashRpc("voiceListen", "VoiceListen", [], {"command": commandName})
    }

    function runSpeakSlash() {
        var text = root.lastAssistantText()
        if (!text) {
            root.appendSlashNote("nothing to speak")
            return true
        }
        return root.slashRpc("voiceSpeak", "VoiceSpeak", [text], {"command": "speak", "chars": text.length})
    }

    function reviewPrompt(target) {
        var what = target || "the work you just did in this session"
        return "Use the review tool to get a cross-vendor critique of " + what
            + ". Package the relevant artifact (the plan, diff, or code) into the tool's `artifact` argument with enough context to judge it, set an appropriate `focus`, then act on the critique: fix real issues it raises and note anything you disagree with and why."
    }

    function submitSyntheticTurn(prompt, commandName) {
        if (!root.requireSessionId(commandName || "command")) return true
        if (!root.fireRpcAction("SendInput", [root.sessionId, prompt, [], []], "Could not send " + (commandName || "command"))) {
            return true
        }
        if (root.transcriptModel && typeof root.transcriptModel.appendUserMessage === "function") {
            root.transcriptModel.appendUserMessage(prompt)
        }
        return true
    }

    function requireSessionState(commandName) {
        if (root.sessionStateModel) return true
        root.appendSlashNote("/" + commandName + " needs an active session.")
        return false
    }

    function requireSessionId(commandName) {
        if (root.sessionId && String(root.sessionId).length > 0) return true
        root.appendSlashNote("/" + commandName + " needs an active session.")
        return false
    }

    function setSessionField(commandName, methodName, value, label) {
        if (!root.requireSessionState(commandName)) return true
        if (typeof root.sessionStateModel[methodName] !== "function") {
            root.appendSlashNote("/" + commandName + " is unavailable for this session.")
            return true
        }
        root.sessionStateModel[methodName](value)
        root.appendSlashNote(label + " -> " + value)
        return true
    }

    function slashRpc(kind, method, args, meta) {
        if (!root.rpcClient || typeof root.rpcClient.callToken !== "function") {
            root.actionError = "Could not run /" + (meta.command || kind) + ": RPC client is unavailable."
            return true
        }
        root.actionError = ""
        var token = root.rpcClient.callToken(method, args || [])
        var next = Object.assign({}, root.slashTokens)
        next[token] = Object.assign({"kind": kind, "method": method}, meta || {})
        root.slashTokens = next
        return true
    }

    function configFieldRow(fields, key) {
        if (!fields) return null
        for (var i = 0; i < fields.length; i++) {
            var row = fields[i]
            if (row && String(row.key || "") === key) return row
        }
        return null
    }

    function configField(fields, key) {
        var row = root.configFieldRow(fields, key)
        if (!row) return ""
        return String(row.value || "")
    }

    function configUsageNote() {
        return "usage: /config <key> <value> (or bare /config to open settings)"
    }

    function configFieldNote(row) {
        var key = String(row.key || "")
        var rawValue = row.value
        var value = rawValue === undefined || rawValue === null || String(rawValue) === "" ? "(unset)" : String(rawValue)
        var lines = [key + " = " + value]
        var desc = String(row.desc || "")
        if (desc) lines.push(desc)
        var options = row.options || []
        if (options.length > 0) {
            var values = []
            for (var i = 0; i < options.length; i++) values.push(String(options[i]))
            lines.push("values: " + values.join("|"))
        }
        return lines.join("\n")
    }

    function runConfigSlash(args) {
        if (!args) return root.openRoute("config", "Config")
        var parts = args.split(/\s+/)
        var key = parts.shift()
        if (!key) {
            root.appendSlashNote(root.configUsageNote())
            return true
        }
        if (parts.length === 0) {
            return root.slashRpc("configFieldStatus", "Config", [], {"command": "config", "key": key})
        }
        var value = parts.join(" ")
        return root.slashRpc("setConfig", "SetConfig", [key, value], {"command": "config", "key": key, "value": value})
    }

    function payloadError(payload) {
        if (!payload || payload.error === undefined || payload.error === null) return ""
        return typeof payload.error === "string" ? payload.error : JSON.stringify(payload.error)
    }

    function handleSlashRpcResult(pending, payload) {
        var error = root.payloadError(payload)
        if (error !== "") {
            root.appendSlashNote("/" + (pending.command || pending.kind) + " failed: " + error)
            return
        }
        var result = payload ? payload.result : null
        if (pending.kind === "routeStatus") {
            var fields = result && result.fields ? result.fields : []
            var route = root.configField(fields, "route")
            var providers = root.configField(fields, "route_providers")
            root.appendSlashNote("routing: " + (route === "true" ? "on" : "off")
                + (providers ? " (providers: " + providers + ")" : " (all credentialed providers)"))
            return
        }
        if (pending.kind === "setRoute") {
            root.appendSlashNote("model-assessed routing " + String(pending.mode || "").toUpperCase())
            return
        }
        if (pending.kind === "configFieldStatus") {
            var configFields = result && result.fields ? result.fields : []
            var field = root.configFieldRow(configFields, String(pending.key || ""))
            root.appendSlashNote(field ? root.configFieldNote(field) : root.configUsageNote())
            return
        }
        if (pending.kind === "setConfig") {
            root.appendSlashNote("config: " + pending.key + " = " + (result || pending.value) + " (applies to new sessions)")
            return
        }
        if (pending.kind === "renameSession") {
            if (result && root.sessionStateModel && typeof root.sessionStateModel.seed === "function") {
                root.sessionStateModel.seed(result)
            }
            root.appendSlashNote("renamed -> " + String(pending.title || ""))
            return
        }
        if (pending.kind === "setGoal") {
            if (result && root.sessionStateModel && typeof root.sessionStateModel.seed === "function") {
                root.sessionStateModel.seed(result)
            }
            root.appendSlashNote(pending.goal ? "goal -> " + pending.goal : "goal cleared")
            return
        }
        if (pending.kind === "setSearch") {
            if (result && root.sessionStateModel && typeof root.sessionStateModel.seed === "function") {
                root.sessionStateModel.seed(result)
            }
            var search = result && result.search !== undefined ? result.search : pending.mode
            root.appendSlashNote("live search -> " + String(search || pending.mode || "off"))
            return
        }
        if (pending.kind === "setFast") {
            if (result && root.sessionStateModel && typeof root.sessionStateModel.seed === "function") {
                root.sessionStateModel.seed(result)
            }
            var fast = result && result.fast !== undefined ? !!result.fast : !!pending.fast
            root.appendSlashNote("fast mode -> " + (fast ? "on" : "off"))
            return
        }
        if (pending.kind === "clearSession") {
            if (root.transcriptModel && typeof root.transcriptModel.clearRows === "function") {
                root.transcriptModel.clearRows()
            }
            if (root.approvalsModel && typeof root.approvalsModel.clearRows === "function") {
                root.approvalsModel.clearRows()
            }
            root.appendSlashNote("-- cleared --")
            return
        }
        if (pending.kind === "compactSession") {
            var before = result && result.before !== undefined ? Number(result.before) : 0
            var after = result && result.after !== undefined ? Number(result.after) : 0
            if (before > 0 || after > 0) {
                root.appendSlashNote("compacted " + before + " -> " + after)
            } else {
                root.appendSlashNote("compacted older context")
            }
            return
        }
        if (pending.kind === "exportSession") {
            root.appendSlashNote("exported -> " + String(result || ""))
            return
        }
        if (pending.kind === "addDir") {
            if (root.sessionStateModel && typeof root.sessionStateModel.refresh === "function") {
                root.sessionStateModel.refresh()
            }
            root.appendSlashNote("added working directory -> " + String(result || pending.path || ""))
            return
        }
        if (pending.kind === "workflowList") {
            root.appendSlashNote(root.workflowListNote(result || []))
            return
        }
        if (pending.kind === "runWorkflow") {
            if (root.sessionStateModel && typeof root.sessionStateModel.refresh === "function") {
                root.sessionStateModel.refresh()
            }
            var completed = result && result.completed ? result.completed : []
            var failedAt = result && result.failedAt ? String(result.failedAt) : ""
            var n = completed.length
            if (failedAt) {
                root.appendSlashNote("workflow " + String(pending.name || "") + " stopped at " + failedAt + " after " + n + " step" + (n === 1 ? "" : "s"))
            } else {
                root.appendSlashNote("workflow " + String(pending.name || "") + ": " + n + " step" + (n === 1 ? "" : "s") + " complete")
            }
            return
        }
        if (pending.kind === "skillBody") {
            var body = String(result || "")
            if (!body) {
                root.appendSlashNote("/skills failed: no body returned for " + String(pending.name || ""))
                return
            }
            root.appendSlashNote("skill: " + String(pending.name || "") + "\n\n" + body)
            return
        }
        if (pending.kind === "addBan") {
            root.appendSlashNote((result ? "updated ban: " : "banned: ") + String(pending.title || ""))
            return
        }
        if (pending.kind === "removeBan") {
            root.appendSlashNote((result ? "removed ban: " : "no ban titled ") + String(pending.title || ""))
            return
        }
        if (pending.kind === "voiceStatus") {
            root.appendSlashNote("voice: STT " + (result && result.stt ? "available" : "missing")
                + ", TTS " + (result && result.tts ? "available" : "missing"))
            return
        }
        if (pending.kind === "voiceModeStart") {
            root.appendSlashNote("voice mode on")
            return
        }
        if (pending.kind === "voiceModeStop") {
            root.appendSlashNote("voice mode off")
            return
        }
        if (pending.kind === "voiceListen") {
            var heard = String(result || "").trim()
            if (!heard) {
                root.appendSlashNote("dictation heard nothing (or STT is unavailable)")
                return
            }
            root.submitSyntheticTurn(heard, pending.command || "dictate")
            return
        }
        if (pending.kind === "voiceSpeak") {
            root.appendSlashNote("speaking last assistant answer")
            return
        }
        if (pending.kind === "pluginsStatus") {
            var plugins = result && result.plugins ? result.plugins : []
            var markets = result && result.marketplaces ? result.marketplaces : []
            var names = []
            var hookCount = 0
            for (var pi = 0; pi < plugins.length && pi < 5; pi++) {
                var plugin = plugins[pi] || {}
                names.push(String(plugin.name || "plugin"))
                hookCount += Number(plugin.hooks || 0)
            }
            root.appendSlashNote("plugins: " + plugins.length + " installed, " + markets.length + " marketplaces"
                + (hookCount > 0 ? ", " + hookCount + " hook" + (hookCount === 1 ? "" : "s") : "")
                + (names.length > 0 ? "\n" + names.join(", ") : ""))
            return
        }
        if (pending.kind === "observeStatus") {
            if (!result || result.available === false) {
                root.appendSlashNote("observe: no telemetry log yet")
                return
            }
            var tools = result.tools ? result.tools.length : 0
            var models = result.models ? result.models.length : 0
            var hooks = result.hooks ? result.hooks.length : 0
            var errors = result.errors ? result.errors.length : 0
            root.appendSlashNote("observe: " + Number(result.records || 0) + " records"
                + ", " + tools + " tools"
                + ", " + models + " models"
                + ", " + hooks + " hooks"
                + ", " + errors + " error groups")
            return
        }
        if (pending.kind === "runCustomCommand") {
            var prompt = String(result || "")
            if (prompt && root.transcriptModel && typeof root.transcriptModel.appendUserMessage === "function") {
                root.transcriptModel.appendUserMessage(prompt)
            }
            return
        }
    }

    function openRoute(route, label) {
        root.routeRequested(route)
        if (label) root.appendSlashNote("opened " + label)
        return true
    }

    function commandScope(name) {
        if (!root.commandsModel || typeof root.commandsModel.commandScope !== "function") return ""
        return root.commandsModel.commandScope(name)
    }

    function runCustomSlash(parsed) {
        var scope = root.commandScope(parsed.name)
        if (!scope || scope === "builtin") return false
        if (!root.requireSessionId(parsed.rawName)) return true
        if (root.isStreaming) {
            root.appendSlashNote("finish or stop the current turn before running /" + parsed.rawName)
            return true
        }
        return root.slashRpc("runCustomCommand", "RunCommand", [root.sessionId, parsed.rawName, parsed.args],
            {"command": parsed.rawName})
    }

    function runSlashCommand(text) {
        if (!text.startsWith("/")) return false
        var parsed = root.parseSlashCommand(text)
        if (!parsed) {
            root.appendSlashNote("unknown slash syntax (try /help)")
            return true
        }
        var name = parsed.name
        var args = parsed.args
        if (root.isStreaming && ["model", "perm", "effort", "search", "fast", "goal", "route", "config", "compact", "clear", "rename", "add-dir", "workflow"].indexOf(name) >= 0) {
            root.appendSlashNote("/" + name + " can't run mid-turn - press Interrupt first.")
            return true
        }
        switch (name) {
        case "help":
            root.appendSlashNote(root.slashHelpText())
            return true
        case "home":
            return root.openRoute("home", "Home")
        case "sessions":
        case "resume":
            return root.openRoute("sessions", "Sessions")
        case "tasks":
            return root.openRoute("tasks", "Tasks")
        case "skills":
            if (!args) return root.openRoute("skills", "Skills")
            return root.slashRpc("skillBody", "SkillBody", [args],
                {"command": "skills", "name": args})
        case "memory":
            return root.openRoute("memory", "Memory")
        case "notes":
            return root.openRoute("notes", "Notes")
        case "connectors":
            return root.openRoute("connectors", "Connectors")
        case "reviewers":
            return root.openRoute("reviewers", "Reviewers")
        case "board":
            return root.openRoute("board", "Board")
        case "live":
        case "tray":
            return root.openRoute("live", "Live")
        case "changes":
            if (root.dockOpen) {
                root.dockOpen = false
            } else {
                root.openWorktreeDock(0)
            }
            return true
        case "config":
            return root.runConfigSlash(args)
        case "model":
            if (!args) {
                var models = root.sessionStateModel ? root.sessionStateModel.catalog : []
                var examples = root.firstValues(models, 8)
                root.appendSlashNote("model: " + root.currentSessionField("model", "unknown")
                    + (examples ? "\ntry: /model " + examples : ""))
                return true
            }
            return root.setSessionField("model", "setModel", args, "model")
        case "perm":
            if (!args) {
                root.appendSlashNote("permission posture: " + root.currentSessionField("perm", "unknown") + "  (use /perm gated|auto)")
                return true
            }
            args = args.toLowerCase()
            if (args !== "gated" && args !== "auto") {
                root.appendSlashNote("usage: /perm gated|auto")
                return true
            }
            return root.setSessionField("perm", "setPerm", args, "permission posture")
        case "effort":
            if (!args) {
                root.appendSlashNote("reasoning effort: " + root.currentSessionField("effort", "unknown")
                    + "  (/effort " + root.joinValues(root.sessionStateModel ? root.sessionStateModel.effortLevels : [], "low|medium|high") + ")")
                return true
            }
            if (root.sessionStateModel && root.sessionStateModel.effortLevels
                    && root.sessionStateModel.effortLevels.length > 0
                    && !root.arrayContains(root.sessionStateModel.effortLevels, args)) {
                root.appendSlashNote("usage: /effort " + root.joinValues(root.sessionStateModel.effortLevels, "low|medium|high"))
                return true
            }
            return root.setSessionField("effort", "setEffort", args, "reasoning effort")
        case "search":
            if (!args) {
                var search = root.currentSessionField("search", "")
                root.appendSlashNote(search ? "live search: " + search + "  (/search off|auto|on)" : "the current model does not support live search")
                return true
            }
            args = args.toLowerCase()
            if (["off", "auto", "on"].indexOf(args) < 0) {
                root.appendSlashNote("usage: /search off|auto|on")
                return true
            }
            if (!root.requireSessionId("search")) return true
            return root.slashRpc("setSearch", "SetSearch", [root.sessionId, args],
                {"command": "search", "mode": args})
        case "fast":
            if (!root.requireSessionId("fast")) return true
            if (root.sessionStateModel && root.sessionStateModel.fastOk === false) {
                root.appendSlashNote("the current model does not support fast mode")
                return true
            }
            var fast = args ? root.onOffValue(args) : !(root.sessionStateModel && root.sessionStateModel.fast)
            if (fast === null) {
                root.appendSlashNote("usage: /fast [on|off]")
                return true
            }
            return root.slashRpc("setFast", "SetFast", [root.sessionId, fast],
                {"command": "fast", "fast": fast})
        case "goal":
            if (!args) {
                var goal = root.currentSessionField("goal", "")
                root.appendSlashNote(goal ? "goal: " + goal + "  (/goal <new text> or /goal clear)" : "no goal set  (/goal <text> to set one)")
                return true
            }
            if (!root.requireSessionId("goal")) return true
            var nextGoal = ["clear", "none", "off"].indexOf(args.toLowerCase()) >= 0 ? "" : args
            return root.slashRpc("setGoal", "SetGoal", [root.sessionId, nextGoal],
                {"command": "goal", "goal": nextGoal})
        case "route":
            if (!args) return root.slashRpc("routeStatus", "Config", [], {"command": "route"})
            args = args.toLowerCase()
            if (args !== "on" && args !== "off") {
                root.appendSlashNote("usage: /route on|off")
                return true
            }
            return root.slashRpc("setRoute", "SetConfig", ["route", args === "on" ? "true" : "false"],
                {"command": "route", "mode": args})
        case "compact":
            if (!root.requireSessionId("compact")) return true
            return root.slashRpc("compactSession", "Compact", [root.sessionId, 0], {"command": "compact"})
        case "clear":
            if (!root.requireSessionId("clear")) return true
            return root.slashRpc("clearSession", "Clear", [root.sessionId], {"command": "clear"})
        case "rename":
            if (!root.requireSessionId("rename")) return true
            if (!args) {
                root.appendSlashNote("usage: /rename <title>")
                return true
            }
            return root.slashRpc("renameSession", "SetTitle", [root.sessionId, args],
                {"command": "rename", "title": args})
        case "save":
        case "export":
            if (!root.requireSessionId(name)) return true
            return root.slashRpc("exportSession", "ExportSession", [root.sessionId], {"command": name})
        case "add-dir":
            if (!root.requireSessionId("add-dir")) return true
            if (!args) {
                root.appendSlashNote(root.sessionRootsNote())
                return true
            }
            return root.slashRpc("addDir", "AddDir", [root.sessionId, args],
                {"command": "add-dir", "path": args})
        case "workflow":
            return root.runWorkflowSlash(args)
        case "ban":
            return root.runBanSlash(args)
        case "unban":
            return root.runUnbanSlash(args)
        case "tools":
            root.appendSlashNote(root.sessionToolsNote())
            return true
        case "copy":
            return root.copyLastAssistant()
        case "review":
            return root.submitSyntheticTurn(root.reviewPrompt(args), "review")
        case "steer":
            return root.setInputMode("steer")
        case "queue":
            return root.setInputMode("queue")
        case "background":
        case "bg":
            return root.runBackgroundSlash()
        case "rail":
            return root.runRailSlash()
        case "term":
            if (root.openWorktreeDock(4)) {
                root.appendSlashNote("opened Terminal dock")
            }
            return true
        case "shells":
            if (root.openWorktreeDock(2)) {
                var shells = root.sessionStateModel ? root.sessionStateModel.shells : []
                root.appendSlashNote(shells && shells.length > 0
                    ? "background shells are shown in the Info dock"
                    : "opened Info dock; no background shells reported")
            }
            return true
        case "loop":
            root.appendSlashNote("/loop is TUI-local; in the GUI, set a Goal for persistent autonomous follow-up")
            return true
        case "read":
            root.appendSlashNote("GUI read-aloud is one-shot: use /speak once voice support is available here")
            return true
        case "mouse":
            root.appendSlashNote("/mouse is terminal-only; the Qt GUI always allows normal text selection")
            return true
        case "rebuild":
            root.appendSlashNote("/rebuild is available from a terminal so it does not disrupt daemon sessions")
            return true
        case "quit":
        case "exit":
            root.appendSlashNote("Close this window from your desktop shell to quit the GUI")
            return true
        case "find":
            return root.runFindSlash(args)
        case "voice":
            return root.runVoiceSlash(args)
        case "mute":
            return root.runMuteSlash()
        case "dictate":
        case "talk":
            return root.runDictateSlash(name)
        case "speak":
            return root.runSpeakSlash()
        case "plugins":
        case "hooks":
        case "plugin":
        case "marketplace":
            return root.slashRpc("pluginsStatus", "Plugins", [], {"command": name})
        case "observe":
        case "obs":
            return root.slashRpc("observeStatus", "ObserveSummary", [5000], {"command": name})
        default:
            if (root.runCustomSlash(parsed)) return true
            root.appendSlashNote("unknown command /" + parsed.rawName + " (try /help)")
            return true
        }
    }

    function sendComposer() {
        var msg = composerTextArea.text.trim()
        if (msg.length === 0) return

        if (root.runSlashCommand(msg)) {
            composerTextArea.text = ""
            return
        }

        var images = []
        if (attachedImage.length > 0) {
            images.push({"data": attachedImage, "mediaType": "image/png"})
        }

        if (root.isStreaming && root.inputMode === "queue") {
            root.queueComposerInput(msg, images)
            composerTextArea.text = ""
            attachedImage = ""
            return
        }

        var sent = root.isStreaming
            ? root.fireRpcAction(
                "SteerInput",
                [root.sessionId, msg, images],
                "Could not steer message",
                {"restoreText": msg, "restoreImage": attachedImage}
            )
            : root.fireRpcAction(
                "SendInput",
                [root.sessionId, msg, images, []],
                "Could not send message",
                {"restoreText": msg, "restoreImage": attachedImage}
            )
        if (!sent) return

        composerTextArea.text = ""
        attachedImage = ""
    }

    function useStarterPrompt(prompt) {
        if (root.isStreaming) return false
        composerTextArea.text = String(prompt || "")
        composerTextArea.cursorPosition = composerTextArea.text.length
        composerTextArea.forceActiveFocus()
        return true
    }

    property bool isStreaming: {
        if (!transcriptModel) return false
        if (transcriptModel.hasActivity !== undefined && transcriptModel.hasActivity !== null) {
            return !!transcriptModel.hasActivity
        }
        if (transcriptModel.hasStreaming !== undefined && transcriptModel.hasStreaming !== null) {
            return !!transcriptModel.hasStreaming
        }
        return scanTranscriptStreaming()
    }

    readonly property string activityLabel: {
        if (transcriptModel && transcriptModel.activityLabel !== undefined && transcriptModel.activityLabel !== null) {
            return String(transcriptModel.activityLabel || (root.isStreaming ? "Working" : "Idle"))
        }
        return root.isStreaming ? "Working" : "Idle"
    }
    readonly property string qaActivityLabel: activityLabel

    function scanTranscriptStreaming() {
        if (!transcriptModel) return false
        for (var i = 0; i < transcriptModel.rowCount(); i++) {
            var streaming = transcriptModel.data(transcriptModel.index(i, 0), 263)  // StreamingRole
            if (streaming) return true
        }
        return false
    }

    function refreshApprovalRows() {
        root.approvalRows = root.approvalsModel ? root.approvalsModel.rowCount() : 0
    }

    property string attachedImage: ""  // base64 image data
}
