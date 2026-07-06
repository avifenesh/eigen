import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Live view — running/approval sessions only, inline actions.
// Reference: internal/gui/frontend/src/views/Live.svelte
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property var sessionsModel: null
    property var liveSessionsModel: null
    property var rpcClient: null

    signal openSession(string sessionId)
    signal newSessionRequested()

    // Per-session UI state (inline confirm for Remove, gate expansion for Approve)
    property var confirmRemove: ({})
    property var gateOpen: ({})
    property var gatePending: ({})
    property var gateLoading: ({})
    property var gateError: ({})
    property var acting: ({})
    property var interrupting: ({})
    property var removing: ({})
    property string actionError: ""
    property bool newSessionPending: false

    // Status counts (computed from full sessions list, not just live)
    property int workingCount: 0
    property int approvalCount: 0
    property int idleCount: 0
    property int errorCount: 0

    // Compute counts from sessionsModel (full list)
    Connections {
        target: sessionsModel ? sessionsModel : null
        ignoreUnknownSignals: true
        function onDataChanged() { updateCounts() }
        function onModelReset() { updateCounts() }
    }

    Component.onCompleted: updateCounts()

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]+/g, "_")
    }

    function updateCounts() {
        var wc = 0, ac = 0, ic = 0, ec = 0
        if (!sessionsModel) return
        for (var i = 0; i < sessionsModel.rowCount(); i++) {
            var idx = sessionsModel.index(i, 0)
            // StatusRole = Qt.UserRole + 5 = 261 (see sessions.py:24)
            var st = sessionsModel.data(idx, 261)
            if (st === "working") wc++
            else if (st === "approval") ac++
            else if (st === "error") ec++
            else ic++
        }
        workingCount = wc
        approvalCount = ac
        idleCount = ic
        errorCount = ec
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // Header with KPIs + New session button
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 72
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.margins: Theme.space.xl
                spacing: Theme.space.xl

                // KPI row
                RowLayout {
                    Layout.fillWidth: true
                    spacing: Theme.space.xxxl

                    // Working KPI
                    RowLayout {
                        spacing: Theme.space.sm
                        Label {
                            text: root.workingCount
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h2
                            font.weight: Theme.fontWeight.bold
                            color: root.workingCount > 0 ? Theme.colors.brandBright : Theme.colors.textPrimary
                        }
                        Label {
                            text: "WORKING"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                        }
                    }

                    // Approval KPI
                    RowLayout {
                        spacing: Theme.space.sm
                        Label {
                            text: root.approvalCount
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h2
                            font.weight: Theme.fontWeight.bold
                            color: root.approvalCount > 0 ? Theme.colors.warn : Theme.colors.textPrimary
                        }
                        Label {
                            text: "APPROVAL"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                        }
                    }

                    // Idle KPI
                    RowLayout {
                        spacing: Theme.space.sm
                        Label {
                            text: root.idleCount
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h2
                            font.weight: Theme.fontWeight.bold
                            color: Theme.colors.textPrimary
                        }
                        Label {
                            text: "IDLE"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                        }
                    }

                    // Error KPI
                    RowLayout {
                        spacing: Theme.space.sm
                        Label {
                            text: root.errorCount
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h2
                            font.weight: Theme.fontWeight.bold
                            color: root.errorCount > 0 ? Theme.colors.error : Theme.colors.textPrimary
                        }
                        Label {
                            text: "ERROR"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                        }
                    }

                    Item { Layout.fillWidth: true }
                }

                AppButton {
                    objectName: "liveNewSessionButton"
                    text: root.newSessionPending ? "Starting..." : "New session"
                    enabled: !root.newSessionPending
                    variant: "primary"
                    onClicked: root.newSessionRequested()
                }
            }
        }

        Rectangle {
            objectName: "liveActionError"
            Layout.fillWidth: true
            Layout.preferredHeight: visible ? Math.max(40, liveActionErrorRow.implicitHeight + Theme.space.md) : 0
            visible: root.actionError !== ""
            color: Theme.colors.errorBg
            border.width: visible ? 1 : 0
            border.color: Theme.colors.error
            clip: true

            RowLayout {
                id: liveActionErrorRow
                anchors.fill: parent
                anchors.margins: Theme.space.md
                spacing: Theme.space.md

                Label {
                    objectName: "liveActionErrorText"
                    text: root.actionError
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.error
                    wrapMode: Text.Wrap
                    Layout.fillWidth: true
                }

                AppButton {
                    objectName: "liveActionErrorClearButton"
                    text: "Clear"
                    compact: true
                    variant: "ghost"
                    toolTipText: "Clear error"
                    onClicked: root.actionError = ""
                }
            }
        }

        // Live sessions list (working + approval only)
        ListView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            spacing: Theme.space.sm
            model: liveSessionsModel

            // Padding around list
            topMargin: Theme.space.xl
            bottomMargin: Theme.space.xl
            leftMargin: Theme.space.xxxl
            rightMargin: Theme.space.xxxl

            // Empty state
            Label {
                visible: parent.count === 0
                anchors.centerIn: parent
                text: "◐\n\nNothing running\n\nNo sessions are working or awaiting approval right now."
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.h3
                color: Theme.colors.textMuted
                horizontalAlignment: Text.AlignHCenter
                lineHeight: 1.6
            }

            delegate: Item {
                id: delegateRoot
                width: ListView.view.width - Theme.space.xxxl * 2
                height: col.implicitHeight

                required property string sessionId
                required property string title
                required property string dir
                required property string modelName
                required property string status
                required property int turns
                required property var updated

                // Per-row state accessors (avoid self-shadow footgun)
                readonly property bool isConfirmingRemove: root.confirmRemove[sessionId] || false
                readonly property bool isGateOpen: root.gateOpen[sessionId] || false
                readonly property bool isInterrupting: root.interrupting[sessionId] || false
                readonly property bool isRemoving: root.removing[sessionId] || false

                ColumnLayout {
                    id: col
                    width: parent.width
                    spacing: 0

                    // Main row
                    Rectangle {
                        Layout.fillWidth: true
                        implicitHeight: 64
                        radius: Theme.radius.md
                        color: Theme.colors.bgRaised
                        border.width: delegateRoot.status === "working" ? 2 : 1
                        border.color: {
                            if (delegateRoot.status === "working") return Theme.colors.brand
                            if (delegateRoot.status === "approval") return Theme.colors.warn
                            return Theme.colors.borderHairline
                        }

                        Rectangle {
                            anchors.fill: parent
                            anchors.margins: 1
                            radius: parent.radius - 1
                            visible: delegateRoot.status === "working" || delegateRoot.status === "approval"
                            color: delegateRoot.status === "working" ? Theme.colors.brand : Theme.colors.warn
                            opacity: 0.07
                        }

                        RowLayout {
                            anchors.fill: parent
                            anchors.margins: Theme.space.lg
                            spacing: Theme.space.lg

                            // Status dot
                            Rectangle {
                                width: 9
                                height: 9
                                radius: 4.5
                                color: Theme.statusColor(delegateRoot.status)
                                Layout.alignment: Qt.AlignVCenter

                                // Breathing animation for live sessions
                                SequentialAnimation on opacity {
                                    running: delegateRoot.status === "working" || delegateRoot.status === "approval"
                                    loops: Animation.Infinite
                                    NumberAnimation { from: 1.0; to: 0.3; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
                                    NumberAnimation { from: 0.3; to: 1.0; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
                                }
                            }

                            // Title + metadata (clickable main area)
                            MouseArea {
                                Layout.fillWidth: true
                                Layout.fillHeight: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: root.openSession(delegateRoot.sessionId)

                                ColumnLayout {
                                    anchors.fill: parent
                                    spacing: Theme.space.xs

                                    Label {
                                        text: delegateRoot.title || "untitled session"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        font.weight: Theme.fontWeight.medium
                                        color: Theme.colors.textPrimary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    RowLayout {
                                        spacing: Theme.space.sm
                                        Layout.fillWidth: true

                                        // Approval badge
                                        Rectangle {
                                            visible: delegateRoot.status === "approval"
                                            implicitWidth: approvalBadgeLabel.implicitWidth + Theme.space.md
                                            implicitHeight: 18
                                            radius: Theme.radius.sm
                                            color: Theme.colors.warnBg
                                            border.width: 1
                                            border.color: Theme.colors.warn
                                            Label {
                                                id: approvalBadgeLabel
                                                anchors.centerIn: parent
                                                text: "needs approval"
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                font.weight: Theme.fontWeight.medium
                                                color: Theme.colors.warn
                                            }
                                        }

                                        Label {
                                            text: baseName(delegateRoot.dir)
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.label
                                            color: Theme.colors.textMuted
                                            elide: Text.ElideMiddle
                                            Layout.fillWidth: true
                                        }
                                    }
                                }
                            }

                            // Model badge
                            Rectangle {
                                visible: delegateRoot.modelName !== ""
                                implicitWidth: modelLabel.implicitWidth + Theme.space.md
                                implicitHeight: 20
                                radius: Theme.radius.sm
                                color: "transparent"
                                border.width: 1
                                border.color: Theme.colors.borderSubtle
                                Label {
                                    id: modelLabel
                                    anchors.centerIn: parent
                                    text: delegateRoot.modelName
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.micro
                                    color: Theme.colors.textSecondary
                                    elide: Text.ElideRight
                                }
                            }

                            // Turns
                            Label {
                                text: delegateRoot.turns + (delegateRoot.turns === 1 ? " turn" : " turns")
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textGhost
                                Layout.preferredWidth: 60
                                horizontalAlignment: Text.AlignRight
                            }

                            // Elapsed time
                            Label {
                                objectName: "liveUpdatedLabel_" + root.safeObjectName(delegateRoot.sessionId)
                                readonly property string qaText: text
                                text: relTime(delegateRoot.updated)
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textFaint
                                Layout.preferredWidth: 64
                                horizontalAlignment: Text.AlignRight
                            }

                            // Action buttons
                            RowLayout {
                                spacing: Theme.space.xs
                                Layout.alignment: Qt.AlignVCenter

                                // Approve… (for approval status)
                                AppButton {
                                    objectName: "liveApproveButton_" + root.safeObjectName(delegateRoot.sessionId)
                                    visible: delegateRoot.status === "approval" && !delegateRoot.isGateOpen
                                    text: "Approve…"
                                    compact: true
                                    variant: "primary"
                                    Layout.preferredWidth: 96
                                    Layout.preferredHeight: 32
                                    toolTipText: "Review pending approvals"
                                    onClicked: openApprovalGate(delegateRoot.sessionId)
                                }

                                // Close gate (when gate is open)
                                AppButton {
                                    objectName: "liveCloseApprovalButton_" + root.safeObjectName(delegateRoot.sessionId)
                                    visible: delegateRoot.isGateOpen
                                    text: "Close"
                                    compact: true
                                    variant: "ghost"
                                    Layout.preferredWidth: 72
                                    Layout.preferredHeight: 32
                                    toolTipText: "Close approvals"
                                    onClicked: closeApprovalGate(delegateRoot.sessionId)
                                }

                                // Open
                                AppButton {
                                    objectName: "liveOpenButton_" + root.safeObjectName(delegateRoot.sessionId)
                                    text: "Open"
                                    compact: true
                                    variant: "secondary"
                                    Layout.preferredWidth: 72
                                    Layout.preferredHeight: 32
                                    toolTipText: "Open session"
                                    onClicked: root.openSession(delegateRoot.sessionId)
                                }

                                // Interrupt (for live sessions)
                                AppButton {
                                    objectName: "liveInterruptButton_" + root.safeObjectName(delegateRoot.sessionId)
                                    visible: delegateRoot.status === "working" || delegateRoot.status === "approval"
                                    text: delegateRoot.isInterrupting ? "Interrupting…" : "Interrupt"
                                    enabled: !delegateRoot.isInterrupting
                                    compact: true
                                    variant: "secondary"
                                    Layout.preferredWidth: 100
                                    Layout.preferredHeight: 32
                                    toolTipText: "Interrupt session"
                                    onClicked: interruptSession(delegateRoot.sessionId)
                                }

                                // Remove / Confirm / Cancel
                                AppButton {
                                    objectName: "liveRemoveButton_" + root.safeObjectName(delegateRoot.sessionId)
                                    visible: !delegateRoot.isConfirmingRemove
                                    text: "Remove"
                                    compact: true
                                    variant: "danger"
                                    Layout.preferredWidth: 86
                                    Layout.preferredHeight: 32
                                    toolTipText: "Remove session"
                                    onClicked: {
                                        var c = Object.assign({}, root.confirmRemove)
                                        c[delegateRoot.sessionId] = true
                                        root.confirmRemove = c
                                    }
                                }
                                AppButton {
                                    objectName: "liveConfirmRemoveButton_" + root.safeObjectName(delegateRoot.sessionId)
                                    visible: delegateRoot.isConfirmingRemove
                                    text: delegateRoot.isRemoving ? "Removing…" : "Confirm"
                                    enabled: !delegateRoot.isRemoving
                                    compact: true
                                    variant: "danger"
                                    Layout.preferredWidth: 96
                                    Layout.preferredHeight: 32
                                    toolTipText: "Confirm remove"
                                    onClicked: removeSession(delegateRoot.sessionId)
                                }
                                AppButton {
                                    objectName: "liveCancelRemoveButton_" + root.safeObjectName(delegateRoot.sessionId)
                                    visible: delegateRoot.isConfirmingRemove
                                    text: "Cancel"
                                    enabled: !delegateRoot.isRemoving
                                    compact: true
                                    variant: "ghost"
                                    Layout.preferredWidth: 80
                                    Layout.preferredHeight: 32
                                    toolTipText: "Cancel remove"
                                    onClicked: {
                                        var c = Object.assign({}, root.confirmRemove)
                                        delete c[delegateRoot.sessionId]
                                        root.confirmRemove = c
                                    }
                                }
                            }
                        }
                    }

                    // Inline approval gate (expanded under approval rows)
                    Rectangle {
                        visible: delegateRoot.isGateOpen
                        Layout.fillWidth: true
                        implicitHeight: gateContent.implicitHeight + Theme.space.lg * 2
                        color: Theme.colors.warnBg
                        border.width: 1
                        border.color: Theme.colors.warn
                        radius: Theme.radius.md

                        ColumnLayout {
                            id: gateContent
                            anchors.fill: parent
                            anchors.margins: Theme.space.lg
                            spacing: Theme.space.sm

                            Label {
                                visible: root.gateLoading[delegateRoot.sessionId] || false
                                text: "Loading approval…"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textMuted
                            }

                            Label {
                                objectName: "liveApprovalGateError_" + root.safeObjectName(delegateRoot.sessionId)
                                visible: root.gateError[delegateRoot.sessionId] || false
                                text: root.gateError[delegateRoot.sessionId] || ""
                                readonly property string qaText: text
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.error
                                wrapMode: Text.Wrap
                                Layout.fillWidth: true
                            }

                            Repeater {
                                model: root.gatePending[delegateRoot.sessionId] || []
                                delegate: RowLayout {
                                    spacing: Theme.space.lg
                                    Layout.fillWidth: true

                                    Label {
                                        text: modelData.tool || ""
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        font.weight: Theme.fontWeight.medium
                                        color: Theme.colors.textPrimary
                                    }

                                    Label {
                                        text: modelData.args || ""
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textMuted
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    RowLayout {
                                        spacing: Theme.space.xs
                                        AppButton {
                                            readonly property string approvalActionKey: delegateRoot.sessionId + ":" + modelData.id
                                            objectName: "liveAllowApprovalButton_" + root.safeObjectName(delegateRoot.sessionId) + "_" + root.safeObjectName(modelData.id)
                                            text: root.acting[approvalActionKey] ? "Allowing…" : "Allow"
                                            enabled: !root.acting[approvalActionKey]
                                            compact: true
                                            variant: "primary"
                                            Layout.preferredWidth: 72
                                            Layout.preferredHeight: 30
                                            toolTipText: "Allow tool call"
                                            onClicked: approveDecision(delegateRoot.sessionId, modelData.id, true)
                                        }
                                        AppButton {
                                            readonly property string approvalActionKey: delegateRoot.sessionId + ":" + modelData.id
                                            objectName: "liveDenyApprovalButton_" + root.safeObjectName(delegateRoot.sessionId) + "_" + root.safeObjectName(modelData.id)
                                            text: root.acting[approvalActionKey] ? "Denying…" : "Deny"
                                            enabled: !root.acting[approvalActionKey]
                                            compact: true
                                            variant: "danger"
                                            Layout.preferredWidth: 72
                                            Layout.preferredHeight: 30
                                            toolTipText: "Deny tool call"
                                            onClicked: approveDecision(delegateRoot.sessionId, modelData.id, false)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    // Helper functions
    function baseName(path) {
        if (!path) return ""
        var parts = path.split("/")
        return parts[parts.length - 1]
    }

    function relTime(updated) {
        updated = timestampMs(updated)
        if (!updated) return ""
        var elapsed = Math.max(0, Math.floor((Date.now() - updated) / 1000))
        if (elapsed < 60) return "just now"
        if (elapsed < 3600) return Math.floor(elapsed / 60) + "m ago"
        if (elapsed < 86400) return Math.floor(elapsed / 3600) + "h ago"
        return Math.floor(elapsed / 86400) + "d ago"
    }

    function timestampMs(ts) {
        var value = Number(ts || 0)
        if (!isFinite(value) || value <= 0) return 0
        if (value > 100000000000000) return Math.floor(value / 1000000)  // ns -> ms
        if (value < 10000000000) return value * 1000  // s -> ms
        return value
    }

    function errorText(error) {
        if (error === undefined || error === null) return "Something went wrong."
        if (typeof error === "string") return error
        if (error.message) return String(error.message)
        try {
            return JSON.stringify(error)
        } catch (e) {
            return String(error)
        }
    }

    function rpcUnavailable(prefix) {
        root.actionError = prefix + ": RPC client is unavailable."
    }

    function refreshLiveSessions() {
        if (liveSessionsModel && liveSessionsModel.refresh) liveSessionsModel.refresh()
    }

    function setGateError(sessionId, message) {
        var gateError = Object.assign({}, root.gateError)
        gateError[sessionId] = message
        root.gateError = gateError
    }

    function clearGateError(sessionId) {
        var gateError = Object.assign({}, root.gateError)
        delete gateError[sessionId]
        root.gateError = gateError
    }

    // Action handlers
    function interruptSession(sessionId) {
        root.actionError = ""
        if (!root.rpcClient || typeof root.rpcClient.callToken !== "function") {
            rpcUnavailable("Interrupt failed")
            return
        }
        var client = root.rpcClient
        var interrupting = Object.assign({}, root.interrupting)
        interrupting[sessionId] = true
        root.interrupting = interrupting

        var token = client.callToken("Interrupt", [sessionId])
        var handler = function(t, payload) {
            if (t !== token) return
            client.callDone.disconnect(handler)
            payload = payload || {}

            var interrupting = Object.assign({}, root.interrupting)
            delete interrupting[sessionId]
            root.interrupting = interrupting

            if (payload.error !== undefined && payload.error !== null) {
                root.actionError = "Interrupt failed: " + errorText(payload.error)
                return
            }

            refreshLiveSessions()
        }
        client.callDone.connect(handler)
    }

    function removeSession(sessionId) {
        root.actionError = ""
        if (!root.rpcClient || typeof root.rpcClient.callToken !== "function") {
            rpcUnavailable("Remove failed")
            return
        }
        var client = root.rpcClient
        var removing = Object.assign({}, root.removing)
        removing[sessionId] = true
        root.removing = removing

        var token = client.callToken("RemoveSession", [sessionId])
        var handler = function(t, payload) {
            if (t !== token) return
            client.callDone.disconnect(handler)
            payload = payload || {}

            var removing = Object.assign({}, root.removing)
            delete removing[sessionId]
            root.removing = removing

            if (payload.error !== undefined && payload.error !== null) {
                root.actionError = "Remove failed: " + errorText(payload.error)
                return
            }

            var confirmRemove = Object.assign({}, root.confirmRemove)
            delete confirmRemove[sessionId]
            root.confirmRemove = confirmRemove

            refreshLiveSessions()
        }
        client.callDone.connect(handler)
    }

    function openApprovalGate(sessionId) {
        root.actionError = ""
        var gateOpen = Object.assign({}, root.gateOpen)
        gateOpen[sessionId] = true
        root.gateOpen = gateOpen
        clearGateError(sessionId)

        var gateLoading = Object.assign({}, root.gateLoading)
        gateLoading[sessionId] = true
        root.gateLoading = gateLoading

        if (!root.rpcClient || typeof root.rpcClient.callToken !== "function") {
            delete gateLoading[sessionId]
            root.gateLoading = gateLoading
            setGateError(sessionId, "Could not load approvals: RPC client is unavailable.")
            return
        }
        var client = root.rpcClient

        // Fetch State to get pending approvals
        var token = client.callToken("State", [sessionId])
        var handler = function(t, payload) {
            if (t !== token) return
            client.callDone.disconnect(handler)
            payload = payload || {}

            var gateLoading = Object.assign({}, root.gateLoading)
            delete gateLoading[sessionId]
            root.gateLoading = gateLoading

            if (payload.error !== undefined && payload.error !== null) {
                setGateError(sessionId, errorText(payload.error))
                return
            }

            var state = payload.result || {}
            var pending = state.pending || []

            if (pending.length === 0) {
                // Nothing to resolve → close gate and open chat
                closeApprovalGate(sessionId)
                root.openSession(sessionId)
                return
            }

            var gatePending = Object.assign({}, root.gatePending)
            gatePending[sessionId] = pending
            root.gatePending = gatePending
        }
        client.callDone.connect(handler)
    }

    function closeApprovalGate(sessionId) {
        var gateOpen = Object.assign({}, root.gateOpen)
        delete gateOpen[sessionId]
        root.gateOpen = gateOpen

        var gatePending = Object.assign({}, root.gatePending)
        delete gatePending[sessionId]
        root.gatePending = gatePending

        var gateLoading = Object.assign({}, root.gateLoading)
        delete gateLoading[sessionId]
        root.gateLoading = gateLoading

        var gateError = Object.assign({}, root.gateError)
        delete gateError[sessionId]
        root.gateError = gateError
    }

    function approveDecision(sessionId, approvalId, allow) {
        root.actionError = ""
        if (!root.rpcClient || typeof root.rpcClient.callToken !== "function") {
            rpcUnavailable(allow ? "Approve failed" : "Deny failed")
            return
        }
        var client = root.rpcClient
        var acting = Object.assign({}, root.acting)
        var key = sessionId + ":" + approvalId
        acting[key] = true
        root.acting = acting

        var token = client.callToken("Approve", [sessionId, approvalId, allow])
        var handler = function(t, payload) {
            if (t !== token) return
            client.callDone.disconnect(handler)
            payload = payload || {}

            var acting = Object.assign({}, root.acting)
            delete acting[key]
            root.acting = acting

            if (payload.error !== undefined && payload.error !== null) {
                root.actionError = (allow ? "Approve failed: " : "Deny failed: ") + errorText(payload.error)
                return
            }

            closeApprovalGate(sessionId)
            refreshLiveSessions()
        }
        client.callDone.connect(handler)
    }
}
