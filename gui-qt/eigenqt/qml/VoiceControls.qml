import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Capability-gated bridge to Eigen's host-side STT/TTS implementation.
Item {
    id: root

    property var rpcClient: null
    property string sessionId: ""
    property bool sttAvailable: false
    property bool ttsAvailable: false
    property bool probed: false
    property bool statusLoading: false
    property bool modeOn: false
    property bool dictationCancelled: false
    property string phase: "idle"
    property string lastText: ""
    property string speakingText: ""
    property var pendingCalls: ({})
    property var subscribedClient: null

    readonly property bool dictating: !modeOn && (phase === "listening" || phase === "transcribing")
    readonly property bool speaking: !modeOn && phase === "speaking"
    readonly property bool qaAvailable: sttAvailable || ttsAvailable
    readonly property bool qaProbed: probed
    readonly property bool qaDictating: dictating
    readonly property bool qaModeOn: modeOn
    readonly property string qaPhase: phase

    implicitWidth: voiceActions.implicitWidth
    implicitHeight: voiceActions.implicitHeight
    visible: qaAvailable

    signal dictated(string text)
    signal actionFailed(string message)

    Component.onCompleted: Qt.callLater(root.initialize)
    Component.onDestruction: root.stopMode(true)
    onRpcClientChanged: {
        root.subscribedClient = null
        Qt.callLater(root.initialize)
    }
    onSessionIdChanged: {
        if (root.modeOn) root.stopMode(false)
    }

    Connections {
        target: root.rpcClient ? root.rpcClient : null

        function onConnected() {
            root.initialize()
        }

        function onCallDone(token, payload) {
            var pending = root.pendingCalls[token]
            if (!pending) return
            var next = Object.assign({}, root.pendingCalls)
            delete next[token]
            root.pendingCalls = next
            root.handleCallResult(pending, payload || {})
        }

        function onEvent(channel, data) {
            if (channel === "eigen:voice") root.applyVoiceEvent(data || {})
        }
    }

    RowLayout {
        id: voiceActions
        spacing: Theme.space.sm

        AppButton {
            id: dictateButton
            objectName: "chatDictateButton"
            visible: root.sttAvailable
            text: root.dictating ? "◉" : "○"
            compact: true
            variant: root.dictating ? "secondary" : "ghost"
            selected: root.dictating
            enabled: !root.modeOn
            toolTipText: root.dictating ? "Stop listening" : "Dictate a message"
            onClicked: root.toggleDictation()
        }

        AppButton {
            id: voiceModeButton
            objectName: "chatVoiceModeButton"
            visible: root.sttAvailable && root.ttsAvailable
            text: "◍"
            compact: true
            variant: root.modeOn ? "secondary" : "ghost"
            selected: root.modeOn
            enabled: root.sessionId !== ""
            toolTipText: root.modeOn ? "End voice conversation" : "Start hands-free voice conversation"
            onClicked: root.toggleMode()
        }
    }

    function initialize() {
        if (!root.rpcClient) return
        if (root.subscribedClient !== root.rpcClient && typeof root.rpcClient.subscribe === "function") {
            root.rpcClient.subscribe(["eigen:voice"])
            root.subscribedClient = root.rpcClient
        }
        root.refreshCapabilities()
    }

    function refreshCapabilities() {
        if (root.statusLoading || !root.rpcClient) return
        root.statusLoading = true
        if (!root.request("status", "VoiceStatus", [], true)) {
            root.statusLoading = false
            root.probed = true
        }
    }

    function request(kind, method, args, silent) {
        if (!root.rpcClient || typeof root.rpcClient.callToken !== "function") {
            if (!silent) root.actionFailed("Could not " + root.actionLabel(kind) + ": RPC client is unavailable.")
            return false
        }
        var token = root.rpcClient.callToken(method, args || [])
        if (!token || token < 1) {
            if (!silent) root.actionFailed("Could not " + root.actionLabel(kind) + ": RPC client is unavailable.")
            return false
        }
        var next = Object.assign({}, root.pendingCalls)
        next[token] = {"kind": kind, "silent": !!silent}
        root.pendingCalls = next
        return true
    }

    function handleCallResult(pending, payload) {
        var error = root.payloadError(payload)
        var kind = String(pending.kind || "")
        if (error !== "") {
            if (kind === "status") {
                root.statusLoading = false
                root.probed = true
                root.sttAvailable = false
                root.ttsAvailable = false
                return
            }
            if (kind === "dictate" && root.dictationCancelled) {
                root.dictationCancelled = false
                root.phase = "idle"
                return
            }
            root.resetFailedAction(kind)
            if (!pending.silent) root.actionFailed("Could not " + root.actionLabel(kind) + ": " + error)
            return
        }

        var result = payload ? payload.result : null
        if (kind === "status") {
            root.statusLoading = false
            root.probed = true
            root.sttAvailable = !!(result && result.stt)
            root.ttsAvailable = !!(result && result.tts)
            return
        }
        if (kind === "dictate") {
            root.phase = "idle"
            if (root.dictationCancelled) {
                root.dictationCancelled = false
                return
            }
            var heard = String(result || "").trim()
            if (heard) root.dictated(heard)
            return
        }
        if (kind === "cancelDictate") {
            root.phase = "idle"
            return
        }
        if (kind === "speak" || kind === "stopSpeak") {
            root.phase = "idle"
            root.speakingText = ""
            return
        }
        if (kind === "modeStart") {
            root.modeOn = true
            return
        }
        if (kind === "modeStop") {
            root.modeOn = false
            root.phase = "off"
        }
    }

    function resetFailedAction(kind) {
        if (kind === "dictate" || kind === "cancelDictate") root.phase = "idle"
        if (kind === "speak" || kind === "stopSpeak") {
            root.phase = "idle"
            root.speakingText = ""
        }
        if (kind === "modeStart" || kind === "modeStop") {
            root.modeOn = false
            root.phase = "off"
        }
    }

    function applyVoiceEvent(data) {
        var nextPhase = String(data.phase || "idle")
        root.phase = nextPhase
        if (data.text) root.lastText = String(data.text)
        if (nextPhase === "off" || nextPhase === "error") root.modeOn = false
        else if (data.mode) root.modeOn = true
        if (nextPhase === "idle" || nextPhase === "off" || nextPhase === "error") {
            root.speakingText = ""
        }
    }

    function toggleDictation() {
        if (!root.sttAvailable || root.modeOn) return
        if (root.dictating) {
            root.dictationCancelled = true
            root.phase = "idle"
            root.request("cancelDictate", "VoiceCancelListen", [], false)
            return
        }
        root.dictationCancelled = false
        root.phase = "listening"
        root.request("dictate", "VoiceListen", [], false)
    }

    function toggleSpeak(text) {
        var nextText = String(text || "").trim()
        if (!root.ttsAvailable || root.modeOn || nextText === "") return
        if (root.speaking) {
            root.request("stopSpeak", "VoiceStopSpeak", [], false)
            return
        }
        root.speakingText = nextText
        root.phase = "speaking"
        root.request("speak", "VoiceSpeak", [nextText], false)
    }

    function toggleMode() {
        if (!root.sttAvailable || !root.ttsAvailable || root.sessionId === "") return
        if (root.modeOn) {
            root.stopMode(false)
            return
        }
        root.lastText = ""
        root.modeOn = true
        root.phase = "listening"
        root.request("modeStart", "VoiceModeStart", [root.sessionId], false)
    }

    function stopMode(silent) {
        if (!root.modeOn) return
        root.modeOn = false
        root.phase = "off"
        root.request("modeStop", "VoiceModeStop", [], !!silent)
    }

    function payloadError(payload) {
        if (!payload || payload.error === undefined || payload.error === null || payload.error === "") return ""
        return typeof payload.error === "string" ? payload.error : JSON.stringify(payload.error)
    }

    function actionLabel(kind) {
        if (kind === "dictate" || kind === "cancelDictate") return "use dictation"
        if (kind === "speak" || kind === "stopSpeak") return "control read-aloud"
        if (kind === "modeStart" || kind === "modeStop") return "control voice conversation"
        return "check voice availability"
    }
}
