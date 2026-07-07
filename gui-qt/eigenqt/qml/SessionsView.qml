import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Full sessions list view (center pane when no session selected)
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property var sessionsModel: null
    property var confirmRemove: ({})
    property int modelEpoch: 0
    property bool autoPruneAttempted: false
    readonly property bool qaAutoPruneAttempted: autoPruneAttempted
    readonly property bool qaAutoPruneTimerRunning: autoPruneTimer.running

    signal sessionClicked(string sessionId)

    Component.onCompleted: scheduleAutoPrune()
    onSessionsModelChanged: {
        root.confirmRemove = ({})
        root.autoPruneAttempted = false
        root.scheduleAutoPrune()
    }
    onVisibleChanged: {
        if (visible) scheduleAutoPrune()
    }

    Timer {
        id: autoPruneTimer
        interval: 120
        repeat: false
        onTriggered: root.runAutoPrune()
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: Theme.space.xxxl
        spacing: Theme.space.lg

        ColumnLayout {
            Layout.fillWidth: true
            spacing: Theme.space.lg

            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.space.lg

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Theme.space.xs

                    Label {
                        text: "Sessions"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.h1
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        text: root.sessionsCountText()
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textMuted
                    }
                }

                AppButton {
                    objectName: "sessionsRefreshButton"
                    text: "Refresh"
                    variant: "ghost"
                    toolTipText: "Refresh sessions"
                    enabled: root.sessionsModel !== null
                    onClicked: root.sessionsModel.refresh()
                }

                AppButton {
                    objectName: "sessionsPruneButton"
                    text: root.sessionsModel && root.sessionsModel.pruning ? "Pruning..." : "Prune now"
                    variant: "secondary"
                    toolTipText: "Remove empty sessions now"
                    Layout.preferredWidth: 104
                    enabled: root.sessionsModel !== null && root.totalSessionsCount() > 0 && !root.sessionsModel.pruning
                    onClicked: root.sessionsModel.pruneSessions()
                }
            }

            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.space.md

                TextField {
                    id: searchField
                    objectName: "sessionsSearchField"
                    Layout.fillWidth: true
                    Layout.maximumWidth: 460
                    Layout.preferredHeight: 34
                    enabled: root.sessionsModel !== null
                    text: root.queryText()
                    placeholderText: "Search sessions..."
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textPrimary
                    selectionColor: Theme.colors.brandBg
                    selectedTextColor: Theme.colors.textPrimary
                    onTextEdited: root.setQuery(text)

                    background: Rectangle {
                        color: Theme.colors.bgRaised
                        radius: Theme.radius.sm
                        border.width: searchField.activeFocus ? 2 : 1
                        border.color: searchField.activeFocus ? Theme.colors.brandBright : Theme.colors.borderSubtle
                    }
                }

                Label {
                    text: root.queryText().length > 0 ? root.filteredSessionsCount() + " shown" : ""
                    visible: text.length > 0
                    font.family: Theme.monoFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    color: Theme.colors.textFaint
                    Layout.alignment: Qt.AlignVCenter
                }
            }
        }

        Label {
            Layout.fillWidth: true
            text: root.sessionsModel ? root.sessionsModel.actionError : ""
            visible: text.length > 0
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.bodySm
            color: Theme.colors.error
            wrapMode: Text.Wrap
        }

        Label {
            Layout.fillWidth: true
            text: root.sessionsModel ? root.sessionsModel.actionMessage : ""
            visible: text.length > 0
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.bodySm
            color: Theme.colors.textSecondary
            elide: Text.ElideMiddle
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.fillHeight: true
            color: "transparent"

            ListView {
                id: sessionsList
                anchors.fill: parent
                clip: true
                spacing: Theme.space.md
                model: root.sessionsModel

                delegate: Rectangle {
                    id: row
                    objectName: "sessionsRow_" + safeId
                    width: ListView.view ? ListView.view.width : 0
                    height: 86
                    radius: Theme.radius.md
                    color: rowHover.hovered ? Theme.colors.surfaceRaised2 : Theme.colors.surfaceRaised
                    border.width: activeFocus ? 2 : 1
                    border.color: activeFocus ? Theme.colors.brandBright : Theme.colors.borderHairline
                    activeFocusOnTab: true
                    Accessible.name: root.textOrFallback(model.title, "untitled session")

                    readonly property string sessionId: root.textOrEmpty(model.sessionId)
                    readonly property string safeId: root.safeObjectName(sessionId)
                    readonly property bool removeConfirming: root.isConfirming(sessionId)
                    readonly property bool removeBusy: root.isRemoving(sessionId)
                    readonly property bool exportBusy: root.isExporting(sessionId)
                    readonly property bool qaVisualFocus: activeFocus

                    Behavior on color { ColorAnimation { duration: Theme.duration.fast } }

                    Keys.onPressed: function(event) {
                        if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter || event.key === Qt.Key_Space) {
                            root.sessionClicked(row.sessionId)
                            event.accepted = true
                        }
                    }

                    HoverHandler {
                        id: rowHover
                    }

                    RowLayout {
                        anchors.fill: parent
                        anchors.margins: Theme.space.lg
                        spacing: Theme.space.lg

                        // Status dot
                        Rectangle {
                            width: 12
                            height: 12
                            radius: 6
                            color: Theme.statusColor(root.textOrEmpty(model.status))
                            Layout.alignment: Qt.AlignVCenter

                            // Breathing animation for working status
                            SequentialAnimation on opacity {
                                running: root.textOrEmpty(model.status) === "working"
                                loops: Animation.Infinite
                                NumberAnimation { from: 1.0; to: 0.3; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
                                NumberAnimation { from: 0.3; to: 1.0; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
                            }
                        }

                        Item {
                            Layout.fillWidth: true
                            Layout.fillHeight: true
                            implicitHeight: 52

                            MouseArea {
                                anchors.fill: parent
                                hoverEnabled: true
                                onClicked: {
                                    row.forceActiveFocus(Qt.MouseFocusReason)
                                    root.sessionClicked(row.sessionId)
                                }
                            }

                            ColumnLayout {
                                anchors.fill: parent
                                spacing: Theme.space.xs

                                Label {
                                    text: root.textOrFallback(model.title, "untitled session")
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.h3
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.textPrimary
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }

                                RowLayout {
                                    spacing: Theme.space.lg
                                    Layout.fillWidth: true

                                    Label {
                                        id: sessionDirLabel
                                        objectName: "sessionsDirLabel_" + row.safeId
                                        readonly property string qaText: text
                                        readonly property string fullPath: root.textOrEmpty(model.dir)
                                        text: root.baseName(fullPath)
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                        elide: Text.ElideMiddle
                                        Layout.fillWidth: true
                                        ToolTip.delay: 600
                                        ToolTip.timeout: 4000
                                        ToolTip.visible: dirHover.hovered && fullPath.length > 0 && fullPath !== text
                                        ToolTip.text: fullPath

                                        HoverHandler {
                                            id: dirHover
                                        }
                                    }

                                    Label {
                                        text: root.textOrEmpty(model.modelName)
                                        visible: text.length > 0
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        font.weight: Theme.fontWeight.medium
                                        color: Theme.colors.textSecondary
                                    }

                                    Label {
                                        text: root.turnsText(model.turns)
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                    }

                                    Label {
                                        objectName: "sessionsUpdatedLabel_" + row.safeId
                                        readonly property string qaText: text
                                        text: root.formatTimestamp(model.updated)
                                        visible: text.length > 0
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                    }
                                }
                            }
                        }

                        AppButton {
                            objectName: "sessionsResumeButton_" + row.safeId
                            text: "Resume"
                            variant: "ghost"
                            toolTipText: "Resume session"
                            onClicked: root.sessionClicked(row.sessionId)
                        }

                        AppButton {
                            objectName: "sessionsExportButton_" + row.safeId
                            text: row.exportBusy ? "Exporting..." : "Export"
                            variant: "ghost"
                            toolTipText: "Export transcript"
                            enabled: !row.exportBusy
                            Layout.preferredWidth: row.exportBusy ? 112 : 78
                            onClicked: root.sessionsModel.exportSession(row.sessionId)
                        }

                        AppButton {
                            objectName: "sessionsRemoveButton_" + row.safeId
                            text: "Remove"
                            variant: "ghost"
                            toolTipText: "Remove session"
                            enabled: !row.removeBusy
                            visible: !row.removeConfirming
                            onClicked: root.setConfirming(row.sessionId, true)
                        }

                        AppButton {
                            objectName: "sessionsRemoveConfirmButton_" + row.safeId
                            text: row.removeBusy ? "Removing..." : "Confirm"
                            variant: "danger"
                            toolTipText: "Confirm remove session"
                            enabled: !row.removeBusy
                            visible: row.removeConfirming
                            Layout.preferredWidth: 104
                            onClicked: {
                                root.sessionsModel.removeSession(row.sessionId)
                                root.setConfirming(row.sessionId, false)
                            }
                        }

                        AppButton {
                            objectName: "sessionsRemoveCancelButton_" + row.safeId
                            text: "Cancel"
                            variant: "ghost"
                            toolTipText: "Cancel remove"
                            enabled: !row.removeBusy
                            visible: row.removeConfirming
                            onClicked: root.setConfirming(row.sessionId, false)
                        }
                    }
                }

                Label {
                    anchors.centerIn: parent
                    visible: root.filteredSessionsCount() === 0
                    text: root.totalSessionsCount() === 0
                        ? "No sessions yet"
                        : "No sessions match \"" + root.queryText() + "\""
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h3
                    color: Theme.colors.textMuted
                }
            }
        }
    }

    Connections {
        target: root.sessionsModel ? root.sessionsModel : null

        function onModelReset() {
            root.modelEpoch += 1
            root.confirmRemove = ({})
            root.scheduleAutoPrune()
        }

        function onRowsInserted() {
            root.modelEpoch += 1
            root.scheduleAutoPrune()
        }

        function onRowsRemoved() {
            root.modelEpoch += 1
            root.confirmRemove = ({})
        }

        function onDataChanged() {
            root.modelEpoch += 1
        }
    }

    function sessionsCount() {
        root.modelEpoch
        return root.sessionsModel ? root.sessionsModel.rowCount() : 0
    }

    function totalSessionsCount() {
        root.modelEpoch
        if (!root.sessionsModel) return 0
        if (root.sessionsModel.totalCount !== undefined && root.sessionsModel.totalCount !== null) {
            return root.sessionsModel.totalCount
        }
        return root.sessionsModel.rowCount()
    }

    function filteredSessionsCount() {
        root.modelEpoch
        if (!root.sessionsModel) return 0
        if (root.sessionsModel.filteredCount !== undefined && root.sessionsModel.filteredCount !== null) {
            return root.sessionsModel.filteredCount
        }
        return root.sessionsModel.rowCount()
    }

    function sessionsCountText() {
        const total = totalSessionsCount()
        const filtered = filteredSessionsCount()
        if (queryText().length > 0) {
            return filtered + " shown · " + total + " total"
        }
        return total + (total === 1 ? " session" : " sessions")
    }

    function isRemoving(sessionId) {
        if (!root.sessionsModel || !root.sessionsModel.removing) return false
        return root.sessionsModel.removing.indexOf(sessionId) >= 0
    }

    function isExporting(sessionId) {
        if (!root.sessionsModel || !root.sessionsModel.exporting) return false
        return root.sessionsModel.exporting.indexOf(sessionId) >= 0
    }

    function isConfirming(sessionId) {
        return !!root.confirmRemove[sessionId]
    }

    function setConfirming(sessionId, value) {
        const next = {}
        for (const key in root.confirmRemove) {
            next[key] = root.confirmRemove[key]
        }
        if (value) {
            next[sessionId] = true
        } else {
            delete next[sessionId]
        }
        root.confirmRemove = next
    }

    function safeObjectName(value) {
        return textOrEmpty(value).replace(/[^A-Za-z0-9_]/g, "_")
    }

    function queryText() {
        if (!root.sessionsModel || root.sessionsModel.query === undefined || root.sessionsModel.query === null) return ""
        return String(root.sessionsModel.query)
    }

    function setQuery(value) {
        if (root.sessionsModel) {
            root.sessionsModel.query = String(value || "")
        }
    }

    function scheduleAutoPrune() {
        if (root.autoPruneAttempted || !root.visible || !root.sessionsModel) return
        if (root.totalSessionsCount() <= 0) return
        if (root.sessionsModel.pruning) return
        autoPruneTimer.restart()
    }

    function runAutoPrune() {
        if (root.autoPruneAttempted || !root.visible || !root.sessionsModel) return
        if (root.totalSessionsCount() <= 0) return
        if (root.sessionsModel.pruning) {
            root.scheduleAutoPrune()
            return
        }
        root.autoPruneAttempted = true
        root.sessionsModel.pruneSessions()
    }

    function textOrEmpty(value) {
        if (value === undefined || value === null) return ""
        return String(value)
    }

    function textOrFallback(value, fallback) {
        const text = textOrEmpty(value)
        return text.length > 0 ? text : fallback
    }

    function turnsText(turns) {
        const count = Number(turns || 0)
        return count + (count === 1 ? " turn" : " turns")
    }

    function formatTimestamp(ts) {
        ts = timestampMs(ts)
        if (!ts) return ""
        const elapsed = Math.max(0, Math.floor((Date.now() - ts) / 1000))
        if (elapsed < 60) return "just now"
        if (elapsed < 3600) return Math.floor(elapsed / 60) + "m ago"
        if (elapsed < 86400) return Math.floor(elapsed / 3600) + "h ago"
        return Math.floor(elapsed / 86400) + "d ago"
    }

    function timestampMs(ts) {
        const value = Number(ts || 0)
        if (!isFinite(value) || value <= 0) return 0
        if (value > 100000000000000) return Math.floor(value / 1000000)  // ns -> ms
        if (value < 10000000000) return value * 1000  // s -> ms
        return value
    }

    function baseName(path) {
        const text = textOrEmpty(path).replace(/\/+$/, "")
        if (text.length === 0) return "—"
        const parts = text.split("/")
        return parts[parts.length - 1] || text
    }
}
