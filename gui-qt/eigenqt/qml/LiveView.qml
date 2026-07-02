import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Live view — running/approval sessions only, inline actions.
// Reference: internal/gui/frontend/src/views/Live.svelte
Rectangle {
    id: root
    color: Theme.colors.bgBase

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

    // Status counts (computed from full sessions list, not just live)
    property int workingCount: 0
    property int approvalCount: 0
    property int idleCount: 0
    property int errorCount: 0

    // Compute counts from sessionsModel (full list)
    Connections {
        target: sessionsModel
        function onDataChanged() { updateCounts() }
        function onModelReset() { updateCounts() }
    }

    Component.onCompleted: updateCounts()

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

                Button {
                    text: "New session"
                    onClicked: root.newSessionRequested()
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

                        // Glow for live sessions
                        layer.enabled: delegateRoot.status === "working" || delegateRoot.status === "approval"
                        layer.effect: ShaderEffect {
                            property color glowColor: delegateRoot.status === "working" ? Theme.colors.brand : Theme.colors.warn
                            fragmentShader: "
                                uniform lowp float qt_Opacity;
                                uniform lowp vec4 glowColor;
                                void main() {
                                    gl_FragColor = glowColor * 0.2 * qt_Opacity;
                                }
                            "
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
                                visible: delegateRoot.modelName
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
                                Button {
                                    visible: delegateRoot.status === "approval" && !delegateRoot.isGateOpen
                                    text: "Approve…"
                                    onClicked: openApprovalGate(delegateRoot.sessionId)
                                }

                                // Close gate (when gate is open)
                                Button {
                                    visible: delegateRoot.isGateOpen
                                    text: "Close"
                                    onClicked: closeApprovalGate(delegateRoot.sessionId)
                                }

                                // Open
                                Button {
                                    text: "Open"
                                    onClicked: root.openSession(delegateRoot.sessionId)
                                }

                                // Interrupt (for live sessions)
                                Button {
                                    visible: delegateRoot.status === "working" || delegateRoot.status === "approval"
                                    text: delegateRoot.isInterrupting ? "Interrupting…" : "Interrupt"
                                    enabled: !delegateRoot.isInterrupting
                                    onClicked: interruptSession(delegateRoot.sessionId)
                                }

                                // Remove / Confirm / Cancel
                                Button {
                                    visible: !delegateRoot.isConfirmingRemove
                                    text: "Remove"
                                    onClicked: {
                                        var c = root.confirmRemove
                                        c[delegateRoot.sessionId] = true
                                        root.confirmRemove = c
                                    }
                                }
                                Button {
                                    visible: delegateRoot.isConfirmingRemove
                                    text: delegateRoot.isRemoving ? "Removing…" : "Confirm"
                                    enabled: !delegateRoot.isRemoving
                                    onClicked: removeSession(delegateRoot.sessionId)
                                }
                                Button {
                                    visible: delegateRoot.isConfirmingRemove
                                    text: "Cancel"
                                    enabled: !delegateRoot.isRemoving
                                    onClicked: {
                                        var c = root.confirmRemove
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
                                visible: root.gateError[delegateRoot.sessionId] || false
                                text: root.gateError[delegateRoot.sessionId] || ""
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.error
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
                                        Button {
                                            text: "Allow"
                                            onClicked: approveDecision(delegateRoot.sessionId, modelData.id, true)
                                        }
                                        Button {
                                            text: "Deny"
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

    function relTime(updatedNano) {
        if (!updatedNano) return ""
        var now = Date.now() * 1000000  // ms → ns
        var elapsed = (now - updatedNano) / 1000000000  // ns → s
        if (elapsed < 60) return Math.floor(elapsed) + "s"
        if (elapsed < 3600) return Math.floor(elapsed / 60) + "m"
        if (elapsed < 86400) return Math.floor(elapsed / 3600) + "h"
        return Math.floor(elapsed / 86400) + "d"
    }

    // Action handlers
    function interruptSession(sessionId) {
        var interrupting = root.interrupting
        interrupting[sessionId] = true
        root.interrupting = interrupting

        rpcClient.callFire("Interrupt", [sessionId])

        // Clear flag after a delay (actual status change comes from events)
        Qt.callLater(function() {
            var interrupting = root.interrupting
            delete interrupting[sessionId]
            root.interrupting = interrupting
            liveSessionsModel.refresh()
        })
    }

    function removeSession(sessionId) {
        var removing = root.removing
        removing[sessionId] = true
        root.removing = removing

        rpcClient.callFire("RemoveSession", [sessionId])

        Qt.callLater(function() {
            var removing = root.removing
            delete removing[sessionId]
            root.removing = removing

            var confirmRemove = root.confirmRemove
            delete confirmRemove[sessionId]
            root.confirmRemove = confirmRemove

            liveSessionsModel.refresh()
        })
    }

    function openApprovalGate(sessionId) {
        var gateOpen = root.gateOpen
        gateOpen[sessionId] = true
        root.gateOpen = gateOpen

        var gateLoading = root.gateLoading
        gateLoading[sessionId] = true
        root.gateLoading = gateLoading

        // Fetch State to get pending approvals
        var token = rpcClient.callToken("State", [sessionId])
        var handler = function(t, payload) {
            if (t !== token) return
            rpcClient.callDone.disconnect(handler)

            var gateLoading = root.gateLoading
            delete gateLoading[sessionId]
            root.gateLoading = gateLoading

            if (payload.error) {
                var gateError = root.gateError
                gateError[sessionId] = payload.error
                root.gateError = gateError
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

            var gatePending = root.gatePending
            gatePending[sessionId] = pending
            root.gatePending = gatePending
        }
        rpcClient.callDone.connect(handler)
    }

    function closeApprovalGate(sessionId) {
        var gateOpen = root.gateOpen
        delete gateOpen[sessionId]
        root.gateOpen = gateOpen

        var gatePending = root.gatePending
        delete gatePending[sessionId]
        root.gatePending = gatePending

        var gateLoading = root.gateLoading
        delete gateLoading[sessionId]
        root.gateLoading = gateLoading

        var gateError = root.gateError
        delete gateError[sessionId]
        root.gateError = gateError
    }

    function approveDecision(sessionId, approvalId, allow) {
        var acting = root.acting
        var key = sessionId + ":" + approvalId
        acting[key] = true
        root.acting = acting

        rpcClient.callFire("Approve", [sessionId, approvalId, allow])

        Qt.callLater(function() {
            var acting = root.acting
            delete acting[key]
            root.acting = acting

            closeApprovalGate(sessionId)
            liveSessionsModel.refresh()
        })
    }
}
