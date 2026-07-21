import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

Rectangle {
    id: root
    objectName: "terminalTab"

    color: Theme.colors.bgRaised

    property string sessionDir: ""
    property var rpcClient: null
    property var terminalHelper: null
    property bool active: true
    property string terminalId: ""
    property int startToken: -1
    property bool usingHelperStart: false
    property bool starting: false
    property bool started: false
    property bool exited: false
    property bool subscribed: false
    property string errorText: ""
    property string output: ""
    property string pendingOutput: ""
    property bool outputFlushScheduled: false
    property int outputFlushCount: 0
    property int maxOutputChars: 60000
    readonly property string qaOutputText: output
    readonly property int qaOutputLength: output.length
    readonly property int qaPendingOutputLength: pendingOutput.length
    readonly property bool qaOutputFlushScheduled: outputFlushScheduled
    readonly property int qaOutputFlushCount: outputFlushCount
    readonly property string qaStatusText: statusText()

    Connections {
        target: root.rpcClient ? root.rpcClient : null

        function onCallDone(token, payload) {
            if (root.usingHelperStart || token !== root.startToken) return
            root.finishTerminalStart(payload || {})
        }

        function onEvent(channel, data) {
            if (channel !== "eigen:terminal" || !data) return
            var eventId = String(data.id || "")
            if (eventId !== root.terminalId) return

            if (data.exited) {
                root.started = false
                root.starting = false
                root.exited = true
                root.appendOutput("\n[terminal exited]\n")
                return
            }

            var encoded = String(data.data || "")
            var decoded = root.terminalHelper && typeof root.terminalHelper.decodeData === "function"
                ? root.terminalHelper.decodeData(encoded)
                : ""
            if (decoded !== "") {
                root.appendOutput(decoded)
            }
        }
    }

    Connections {
        target: root.terminalHelper ? root.terminalHelper : null

        function onTerminalStarted(token, terminalId, error) {
            if (!root.usingHelperStart || token !== root.startToken) return
            root.finishTerminalStart({
                "result": terminalId || "",
                "error": error || ""
            })
        }
    }

    Timer {
        id: resizeTimer
        interval: 80
        repeat: false
        onTriggered: root.resizeTerminal()
    }

    Component.onCompleted: startTerminal()
    Component.onDestruction: stopTerminal()

    onActiveChanged: {
        if (active) {
            root.requestResize()
            Qt.callLater(function() {
                if (terminalOutputArea) {
                    terminalOutputArea.forceActiveFocus()
                }
            })
        }
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 44
            color: Theme.colors.bgRaised
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.md
                anchors.rightMargin: Theme.space.md
                spacing: Theme.space.sm

                Label {
                    text: "terminal"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textSecondary
                }

                AppTag {
                    objectName: "terminalStatusTag"
                    text: root.statusText()
                    backgroundColor: statusToneColor()
                    borderColor: Theme.colors.borderHairline
                    textColor: Theme.colors.textSecondary
                    fontPixelSize: Theme.fontSize.micro
                    pill: false
                }

                Item { Layout.fillWidth: true }

                AppButton {
                    objectName: "terminalStartButton"
                    text: root.starting ? "Starting" : "Start"
                    compact: true
                    variant: "ghost"
                    toolTipText: "Start terminal"
                    enabled: !root.starting && !root.started
                    Layout.preferredWidth: 68
                    Layout.preferredHeight: 28
                    onClicked: root.startTerminal()
                }

                AppButton {
                    objectName: "terminalStopButton"
                    text: "Stop"
                    compact: true
                    variant: "ghost"
                    toolTipText: "Stop terminal"
                    enabled: root.started || root.starting
                    Layout.preferredWidth: 56
                    Layout.preferredHeight: 28
                    onClicked: root.stopTerminal()
                }

                AppButton {
                    objectName: "terminalClearButton"
                    text: "Clear"
                    compact: true
                    variant: "ghost"
                    toolTipText: "Clear terminal output"
                    Layout.preferredWidth: 58
                    Layout.preferredHeight: 28
                    onClicked: root.clearOutput()
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 1
            color: Theme.colors.borderHairline
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.fillHeight: true
            color: Theme.colors.bgWell
            clip: true

            ScrollView {
                anchors.fill: parent
                anchors.margins: Theme.space.md
                clip: true

                AppTextArea {
                    id: terminalOutputArea
                    objectName: "terminalOutputArea"
                    text: root.output
                    readOnly: true
                    persistentSelection: true
                    wrapMode: TextArea.NoWrap
                    qaAllowHorizontalOverflow: true
                    focus: true
                    font.family: Theme.monoFonts[0]
                    font.pixelSize: Theme.fontSize.codeSm
                    backgroundColor: Theme.colors.synBg
                    borderColor: Theme.colors.borderHairline
                    focusBorderColor: Theme.colors.borderFocus
                    normalBorderWidth: 0
                    focusedBorderWidth: 1
                    backgroundRadius: Theme.radius.sm
                    color: Theme.colors.synText
                    selectedTextColor: Theme.colors.bgWell
                    selectionColor: Theme.colors.brand
                    Accessible.name: "Terminal output"

                    Keys.onPressed: function(event) {
                        root.handleTerminalKey(event)
                    }

                    onPressed: function(_event) {
                        terminalOutputArea.forceActiveFocus()
                    }

                    onWidthChanged: root.requestResize()
                    onHeightChanged: root.requestResize()
                }
            }

            Rectangle {
                id: terminalErrorBadge
                objectName: "terminalErrorBadge"
                visible: root.errorText !== ""
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.bottom: parent.bottom
                anchors.leftMargin: Theme.space.lg
                anchors.rightMargin: Theme.space.lg
                anchors.bottomMargin: Theme.space.lg
                height: Math.max(28, terminalErrorLabel.implicitHeight + Theme.space.sm * 2)
                radius: Theme.radius.sm
                color: Theme.colors.errorBg
                border.width: 1
                border.color: Theme.colors.error

                Label {
                    id: terminalErrorLabel
                    anchors.fill: parent
                    anchors.leftMargin: Theme.space.md
                    anchors.rightMargin: Theme.space.md
                    verticalAlignment: Text.AlignVCenter
                    text: root.errorText
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textSecondary
                    elide: Text.ElideRight
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 44
            color: Theme.colors.bgRaised
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.md
                anchors.rightMargin: Theme.space.md
                spacing: Theme.space.sm

                AppTextField {
                    id: terminalCommandField
                    objectName: "terminalCommandField"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 30
                    backgroundColor: Theme.colors.bgInset
                    borderColor: Theme.colors.borderHairline
                    focusBorderColor: Theme.colors.borderFocus
                    enabled: root.started
                    placeholderText: root.started ? "Run a shell command" : "Terminal is not running"
                    font.family: Theme.monoFonts[0]
                    font.pixelSize: Theme.fontSize.codeSm
                    Accessible.name: "Terminal command"

                    Keys.onReturnPressed: function(event) {
                        root.sendCommand()
                        event.accepted = true
                    }
                    Keys.onEnterPressed: function(event) {
                        root.sendCommand()
                        event.accepted = true
                    }
                }

                AppButton {
                    objectName: "terminalSendButton"
                    text: "Send"
                    compact: true
                    variant: "secondary"
                    toolTipText: "Send command"
                    enabled: root.started && terminalCommandField.text.length > 0
                    Layout.preferredWidth: 60
                    Layout.preferredHeight: 30
                    onClicked: root.sendCommand()
                }
            }
        }
    }

    function startTerminal() {
        if (root.starting || root.started) return
        if (!root.rpcClient || typeof root.rpcClient.callToken !== "function") {
            root.errorText = "RPC client is unavailable."
            return
        }

        root.errorText = ""
        root.exited = false
        root.starting = true
        root.subscribeTerminal()
        if (root.terminalHelper && typeof root.terminalHelper.startTerminal === "function") {
            root.usingHelperStart = true
            root.startToken = root.terminalHelper.startTerminal(
                root.rpcClient,
                root.columns(),
                root.rows(),
                root.sessionDir || ""
            )
        } else {
            root.usingHelperStart = false
            root.startToken = root.rpcClient.callToken("TerminalStart", [
                root.columns(),
                root.rows(),
                root.sessionDir || ""
            ])
        }
    }

    function stopTerminal() {
        var id = root.terminalId
        var token = root.startToken
        var helperStart = root.usingHelperStart
        root.startToken = -1
        root.usingHelperStart = false
        root.starting = false
        root.started = false
        root.terminalId = ""
        if (helperStart && token > 0 && root.terminalHelper && typeof root.terminalHelper.cancelTerminalStart === "function") {
            root.terminalHelper.cancelTerminalStart(token)
        }
        if (root.rpcClient && id !== "" && typeof root.rpcClient.callFire === "function") {
            root.rpcClient.callFire("TerminalKill", [id])
        }
        root.unsubscribeTerminal()
    }

    function finishTerminalStart(payload) {
        root.startToken = -1
        root.usingHelperStart = false
        root.starting = false
        payload = payload || {}
        if (payload.error) {
            root.errorText = String(payload.error)
            root.started = false
            root.terminalId = ""
            root.unsubscribeTerminal()
            return
        }

        root.terminalId = String(payload.result || "")
        if (root.terminalId === "") {
            root.errorText = "Terminal did not return an id."
            root.started = false
            root.unsubscribeTerminal()
            return
        }

        root.started = true
        root.exited = false
        root.errorText = ""
        root.resizeTerminal()
        Qt.callLater(function() {
            if (terminalOutputArea) {
                terminalOutputArea.forceActiveFocus()
            }
        })
    }

    function subscribeTerminal() {
        if (root.subscribed) return
        if (root.rpcClient && typeof root.rpcClient.subscribe === "function") {
            root.rpcClient.subscribe(["eigen:terminal"])
            root.subscribed = true
        }
    }

    function unsubscribeTerminal() {
        if (!root.subscribed) return
        if (root.rpcClient && typeof root.rpcClient.unsubscribe === "function") {
            root.rpcClient.unsubscribe(["eigen:terminal"])
        }
        root.subscribed = false
    }

    function sendCommand() {
        var command = terminalCommandField.text
        if (command.length === 0) return
        if (!root.writeData(command + "\r")) return
        terminalCommandField.text = ""
        terminalOutputArea.forceActiveFocus()
    }

    function writeData(data) {
        if (!root.started || root.terminalId === "") return false
        if (!root.rpcClient || typeof root.rpcClient.callFire !== "function") {
            root.errorText = "RPC client is unavailable."
            return false
        }
        root.rpcClient.callFire("TerminalWrite", [root.terminalId, data])
        return true
    }

    function handleTerminalKey(event) {
        if (!root.started || root.terminalId === "") return

        var control = (event.modifiers & Qt.ControlModifier) !== 0
        var alt = (event.modifiers & Qt.AltModifier) !== 0

        if (control) {
            if (event.key === Qt.Key_C) {
                root.writeData(String.fromCharCode(3))
                event.accepted = true
                return
            }
            if (event.key === Qt.Key_D) {
                root.writeData(String.fromCharCode(4))
                event.accepted = true
                return
            }
        }

        if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
            root.writeData("\r")
            event.accepted = true
            return
        }
        if (event.key === Qt.Key_Backspace) {
            root.writeData(String.fromCharCode(127))
            event.accepted = true
            return
        }
        if (event.key === Qt.Key_Tab) {
            root.writeData("\t")
            event.accepted = true
            return
        }
        if (event.key === Qt.Key_Up) {
            root.writeData("\x1b[A")
            event.accepted = true
            return
        }
        if (event.key === Qt.Key_Down) {
            root.writeData("\x1b[B")
            event.accepted = true
            return
        }
        if (event.key === Qt.Key_Right) {
            root.writeData("\x1b[C")
            event.accepted = true
            return
        }
        if (event.key === Qt.Key_Left) {
            root.writeData("\x1b[D")
            event.accepted = true
            return
        }

        if (!control && !alt && event.text && event.text.length > 0) {
            root.writeData(event.text)
            event.accepted = true
        }
    }

    function appendOutput(text) {
        text = String(text || "")
        if (text === "") return
        root.pendingOutput = root.capOutput(root.pendingOutput + text)
        root.scheduleOutputFlush()
    }

    function scheduleOutputFlush() {
        if (root.outputFlushScheduled) return
        root.outputFlushScheduled = true
        Qt.callLater(root.flushOutput)
    }

    function flushOutput() {
        root.outputFlushScheduled = false
        if (root.pendingOutput !== "") {
            root.output = root.capOutput(root.output + root.pendingOutput)
            root.pendingOutput = ""
            root.outputFlushCount += 1
        }
        if (terminalOutputArea) {
            terminalOutputArea.cursorPosition = terminalOutputArea.text.length
        }
    }

    function clearOutput() {
        root.pendingOutput = ""
        root.output = ""
        root.outputFlushScheduled = false
    }

    function capOutput(text) {
        var limit = Math.max(0, root.maxOutputChars)
        if (limit === 0) return ""
        return text.length > limit ? text.slice(text.length - limit) : text
    }

    function requestResize() {
        if (root.started && root.terminalId !== "") {
            resizeTimer.restart()
        }
    }

    function resizeTerminal() {
        if (!root.started || root.terminalId === "") return
        if (!root.rpcClient || typeof root.rpcClient.callFire !== "function") return
        root.rpcClient.callFire("TerminalResize", [root.terminalId, root.columns(), root.rows()])
    }

    function columns() {
        return Math.max(40, Math.floor(Math.max(1, terminalOutputArea.width) / 8))
    }

    function rows() {
        return Math.max(12, Math.floor(Math.max(1, terminalOutputArea.height) / 18))
    }

    function statusText() {
        if (root.errorText !== "") return "error"
        if (root.starting) return "starting"
        if (root.started) return "connected"
        if (root.exited) return "exited"
        return "idle"
    }

    function statusToneColor() {
        if (root.errorText !== "") return Theme.colors.errorBg
        if (root.starting) return Theme.colors.workingBg
        if (root.started) return Theme.colors.successBg
        return Theme.colors.bgInset
    }
}
