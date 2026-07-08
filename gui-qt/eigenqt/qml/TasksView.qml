import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Tasks view — background agents board (status filters, running tasks, transcript viewer)
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property var tasksModel: null
    property var rpcClient: null
    property string currentFilter: "all"  // all, running, done, error

    // Transcript viewer state
    property var openTask: null
    property string transcriptText: ""
    property string transcriptError: ""
    property bool transcriptLoading: false
    property bool transcriptLoaded: false
    property int transcriptRequestSeq: 0
    property int transcriptElidedCount: 0
    readonly property int qaTranscriptEntryCount: transcriptEntries.count
    readonly property int qaTranscriptPendingCount: Object.keys(transcriptCallTokens).length

    function safeName(value) {
        return String(value || "item").replace(/[^A-Za-z0-9_]/g, "_")
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // Header: KPIs + filter chips
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 72
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xxxl
                anchors.rightMargin: Theme.space.xxxl
                spacing: Theme.space.xxxl

                // KPIs
                RowLayout {
                    spacing: Theme.space.xxxl

                    // Running
                    RowLayout {
                        spacing: Theme.space.sm
                        Label {
                            text: root.tasksModel ? root.tasksModel.running_count : 0
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h2
                            font.weight: Theme.fontWeight.bold
                            color: Theme.colors.brand
                        }
                        Label {
                            text: "running"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            font.capitalization: Font.AllUppercase
                        }
                    }

                    // Done
                    RowLayout {
                        spacing: Theme.space.sm
                        Label {
                            text: root.tasksModel ? root.tasksModel.done_count : 0
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h2
                            font.weight: Theme.fontWeight.bold
                            color: Theme.colors.success
                        }
                        Label {
                            text: "done"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            font.capitalization: Font.AllUppercase
                        }
                    }

                    // Errored
                    RowLayout {
                        spacing: Theme.space.sm
                        Label {
                            text: root.tasksModel ? root.tasksModel.error_count : 0
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h2
                            font.weight: Theme.fontWeight.bold
                            color: Theme.colors.error
                        }
                        Label {
                            text: "errored"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            font.capitalization: Font.AllUppercase
                        }
                    }
                }

                Item { Layout.fillWidth: true }

                // Filter chips
                Rectangle {
                    Layout.preferredHeight: 32
                    color: Theme.colors.bgWell
                    border.width: 1
                    border.color: Theme.colors.borderHairline
                    radius: Theme.radius.md
                    implicitWidth: filterRow.implicitWidth + 8

                    RowLayout {
                        id: filterRow
                        anchors.fill: parent
                        anchors.margins: 4
                        spacing: 4

                        Repeater {
                            model: ["all", "running", "done", "error"]
                            delegate: Rectangle {
                                id: chipRect
                                readonly property bool selected: root.currentFilter === modelData
                                readonly property bool qaVisualFocus: activeFocus
                                readonly property string qaAccessibleName: chipLabel.text + " task filter"

                                objectName: "taskFilterChip_" + root.safeName(modelData)
                                Layout.preferredHeight: 24
                                Layout.preferredWidth: chipLabel.implicitWidth + 16
                                radius: Theme.radius.sm
                                activeFocusOnTab: true
                                focusPolicy: Qt.StrongFocus
                                Accessible.role: Accessible.Button
                                Accessible.name: qaAccessibleName
                                Accessible.description: selected ? "Selected task filter" : "Filter tasks to " + chipLabel.text
                                Accessible.onPressAction: activate()

                                color: selected ? Theme.colors.surfaceRaised2 : (activeFocus ? Theme.colors.stateFocusBg : "transparent")
                                border.width: activeFocus ? 1 : 0
                                border.color: activeFocus ? Theme.colors.brandBright : "transparent"

                                Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
                                Behavior on border.color { ColorAnimation { duration: Theme.duration.fast } }

                                function activate() {
                                    root.currentFilter = modelData
                                }

                                Keys.onReturnPressed: activate()
                                Keys.onEnterPressed: activate()
                                Keys.onSpacePressed: activate()

                                Label {
                                    id: chipLabel
                                    anchors.centerIn: parent
                                    text: modelData.charAt(0).toUpperCase() + modelData.slice(1)
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    font.weight: Theme.fontWeight.medium
                                    color: chipRect.selected ? Theme.colors.textPrimary : Theme.colors.textMuted
                                }

                                MouseArea {
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: chipRect.activate()
                                }
                            }
                        }
                    }
                }
            }
        }

        Rectangle {
            objectName: "taskActionErrorBanner"
            visible: root.tasksModel && root.tasksModel.actionError !== ""
            Layout.fillWidth: true
            Layout.preferredHeight: visible ? taskActionErrorText.implicitHeight + Theme.space.lg * 2 : 0
            color: Theme.colors.errorBg
            border.width: 1
            border.color: Theme.colors.error

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xxxl
                anchors.rightMargin: Theme.space.xxxl
                anchors.topMargin: Theme.space.lg
                anchors.bottomMargin: Theme.space.lg
                spacing: Theme.space.lg

                Label {
                    id: taskActionErrorText
                    objectName: "taskActionErrorText"
                    text: root.tasksModel ? root.tasksModel.actionError : ""
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.error
                    wrapMode: Text.WordWrap
                    Layout.fillWidth: true
                }

                AppButton {
                    objectName: "taskActionErrorDismissButton"
                    text: "✕"
                    compact: true
                    toolTipText: "Dismiss task error"
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 32
                    onClicked: if (root.tasksModel) root.tasksModel.clear_action_error()
                }
            }
        }

        // Tasks list
        ListView {
            id: tasksListView
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            spacing: Theme.space.md
            topMargin: Theme.space.xl
            bottomMargin: Theme.space.xl
            leftMargin: Theme.space.xxxl
            rightMargin: Theme.space.xxxl

            model: root.tasksModel

            delegate: Rectangle {
                id: taskCard
                width: ListView.view.width - ListView.view.leftMargin - ListView.view.rightMargin
                implicitHeight: taskLayout.implicitHeight + Theme.space.lg * 2
                radius: Theme.radius.md
                color: Theme.colors.surfaceRaised
                border.width: 1
                border.color: Theme.colors.borderHairline

                RowLayout {
                    id: taskLayout
                    anchors.fill: parent
                    anchors.margins: Theme.space.lg
                    spacing: Theme.space.lg

                    // Main content
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: Theme.space.sm

                        // Top row: status, id, badges, elapsed
                        RowLayout {
                            spacing: Theme.space.sm
                            Layout.fillWidth: true

                            // Status dot
                            Rectangle {
                                width: 8
                                height: 8
                                radius: 4
                                color: statusDotColor(model.status)

                                // Pulse animation for running
                                SequentialAnimation on opacity {
                                    running: Theme.continuousMotion && model.status === "running"
                                    loops: Animation.Infinite
                                    NumberAnimation { from: 1.0; to: 0.3; duration: Theme.duration.breath / 2 }
                                    NumberAnimation { from: 0.3; to: 1.0; duration: Theme.duration.breath / 2 }
                                }
                            }

                            Label {
                                text: model.taskId
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                font.weight: Theme.fontWeight.semibold
                                color: Theme.colors.textSecondary
                            }

                            // Status badge
                            AppTag {
                                objectName: "taskStatusTag_" + root.safeName(model.taskId)
                                text: model.canceling ? "canceling" : model.status
                                backgroundColor: statusBgColor(model.status)
                                borderColor: "transparent"
                                textColor: statusTextColor(model.status)
                                fontPixelSize: Theme.fontSize.label
                                fontWeight: Theme.fontWeight.medium
                                pill: false
                            }

                            // Role badge (if present)
                            AppTag {
                                objectName: "taskRoleTag_" + root.safeName(model.taskId)
                                visible: model.roleName
                                text: model.roleName
                                backgroundColor: Theme.colors.bgWell
                                borderColor: Theme.colors.borderHairline
                                textColor: Theme.colors.textSecondary
                                fontPixelSize: Theme.fontSize.label
                                pill: false
                            }

                            // Model badge (if present)
                            AppTag {
                                objectName: "taskModelTag_" + root.safeName(model.taskId)
                                visible: model.modelName
                                text: model.modelName
                                backgroundColor: "transparent"
                                borderColor: Theme.colors.borderBrand
                                textColor: Theme.colors.brand
                                fontPixelSize: Theme.fontSize.label
                                pill: false
                            }

                            // Where badge (if present)
                            AppTag {
                                objectName: "taskWhereTag_" + root.safeName(model.taskId)
                                visible: model.where
                                text: model.where
                                backgroundColor: Theme.colors.bgWell
                                borderColor: Theme.colors.borderHairline
                                textColor: Theme.colors.textMuted
                                fontPixelSize: Theme.fontSize.label
                                elideMode: Text.ElideMiddle
                                pill: false
                            }

                            Item { Layout.fillWidth: true }

                            // Elapsed
                            Label {
                                text: model.elapsed
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textGhost
                            }
                        }

                        // Kind/difficulty (if present)
                        RowLayout {
                            visible: model.kind || model.difficulty
                            spacing: Theme.space.lg
                            Layout.fillWidth: true

                            Label {
                                visible: model.kind
                                text: model.kind
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textGhost
                                font.capitalization: Font.AllUppercase
                            }
                            Label {
                                visible: model.difficulty
                                text: model.difficulty
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textGhost
                                font.capitalization: Font.AllUppercase
                            }
                        }

                        // Task description
                        Label {
                            text: model.task
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textPrimary
                            wrapMode: Text.WordWrap
                            maximumLineCount: 2
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                        }

                        // Live status (running tasks only)
                        RowLayout {
                            visible: model.status === "running"
                            spacing: Theme.space.lg
                            Layout.fillWidth: true

                            Label {
                                visible: model.lastTool
                                text: model.lastTool
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                font.weight: Theme.fontWeight.medium
                                color: Theme.colors.working
                            }
                            Label {
                                visible: model.steps > 0
                                text: model.steps + " steps"
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textMuted
                            }
                            Label {
                                visible: model.lastNote
                                text: model.lastNote
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textGhost
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                        }

                        // Error (if present)
                        Label {
                            visible: model.error
                            text: model.error
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.error
                            wrapMode: Text.WordWrap
                            Layout.fillWidth: true
                        }

                        // Token stats
                        Label {
                            visible: (model.inTokens + model.outTokens) > 0
                            text: {
                                var parts = ["↑" + model.inTokens.toLocaleString(), "↓" + model.outTokens.toLocaleString()]
                                if (model.attempts > 1) parts.push("· " + model.attempts + " attempts")
                                if (model.escalated) parts.push("· escalated")
                                return parts.join(" ")
                            }
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            color: Theme.colors.textGhost
                        }
                    }

                    // Actions
                    ColumnLayout {
                        Layout.alignment: Qt.AlignTop
                        Layout.preferredWidth: 116
                        spacing: Theme.space.sm

                        AppButton {
                            objectName: "taskTranscriptButton_" + root.safeName(model.taskId)
                            text: "Transcript"
                            compact: true
                            toolTipText: "Open task transcript"
                            Layout.fillWidth: true
                            onClicked: openTranscript(model)
                        }

                        AppButton {
                            visible: model.status === "running"
                            enabled: !model.canceling
                            objectName: "taskCancelButton_" + root.safeName(model.taskId)
                            variant: "danger"
                            compact: true
                            toolTipText: model.canceling ? "Task cancel request is pending" : "Cancel running task"
                            text: model.canceling ? "Stopping…" : "Cancel"
                            Layout.fillWidth: true
                            onClicked: root.tasksModel.cancel(model.taskId)
                        }
                    }
                }
            }

            // Empty state
            Label {
                visible: tasksListView.count === 0
                anchors.centerIn: parent
                text: root.currentFilter === "all" ? "No tasks" : "No " + root.currentFilter + " tasks"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.body
                color: Theme.colors.textMuted
            }
        }
    }

    // Filtering happens INSIDE TasksModel (python `filter` property) — the
    // old QML ListModel re-copy proxy produced 70×N "undefined member" spam
    // (data() returns undefined for roles QML enums resolve to undefined
    // attribute lookups) and re-copied every row per poll.
    onCurrentFilterChanged: if (tasksModel) tasksModel.filter = currentFilter

    // Transcript viewer (slide-over sheet)
    Rectangle {
        id: transcriptScrim
        visible: root.openTask !== null
        anchors.fill: parent
        color: "black"
        opacity: 0.5
        z: 100

        MouseArea {
            anchors.fill: parent
            onClicked: closeTranscript()
        }
    }

    Rectangle {
        id: transcriptSheet
        visible: root.openTask !== null
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        width: Math.min(620, parent.width * 0.84)
        color: Theme.colors.bgRaised
        border.width: 1
        border.color: Theme.colors.borderSubtle
        z: 101

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: Theme.space.xxxl
            spacing: Theme.space.lg

            // Header
            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.space.lg

                // Status dot
                Rectangle {
                    width: 8
                    height: 8
                    radius: 4
                    color: root.openTask ? statusDotColor(root.openTask.status) : Theme.colors.dotIdle

                    SequentialAnimation on opacity {
                        running: Theme.continuousMotion && root.openTask && root.openTask.status === "running"
                        loops: Animation.Infinite
                        NumberAnimation { from: 1.0; to: 0.3; duration: Theme.duration.breath / 2 }
                        NumberAnimation { from: 0.3; to: 1.0; duration: Theme.duration.breath / 2 }
                    }
                }

                Label {
                    text: root.openTask ? root.openTask.taskId : ""
                    font.family: Theme.monoFonts[0]
                    font.pixelSize: Theme.fontSize.h3
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textPrimary
                }

                AppTag {
                    objectName: "taskTranscriptStatusTag"
                    text: root.openTask ? root.openTask.status : ""
                    backgroundColor: root.openTask ? statusBgColor(root.openTask.status) : "transparent"
                    borderColor: "transparent"
                    textColor: root.openTask ? statusTextColor(root.openTask.status) : Theme.colors.textPrimary
                    fontPixelSize: Theme.fontSize.label
                    fontWeight: Theme.fontWeight.medium
                    pill: false
                }

                Item { Layout.fillWidth: true }

                AppButton {
                    objectName: "taskTranscriptCloseButton"
                    text: "✕"
                    toolTipText: "Close transcript"
                    compact: true
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 32
                    onClicked: closeTranscript()
                }
            }

            // Task description
            Label {
                text: root.openTask ? root.openTask.task : ""
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textSecondary
                wrapMode: Text.WordWrap
                Layout.fillWidth: true
            }

            // Result (if present)
            ColumnLayout {
                visible: root.openTask && root.openTask.result
                spacing: Theme.space.sm
                Layout.fillWidth: true

                Label {
                    text: "result"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    color: Theme.colors.textGhost
                    font.capitalization: Font.AllUppercase
                }

                ScrollView {
                    Layout.fillWidth: true
                    Layout.preferredHeight: Math.min(200, resultText.implicitHeight + 16)
                    clip: true

                    Rectangle {
                        implicitWidth: parent.width
                        implicitHeight: resultText.implicitHeight + 16
                        color: Theme.colors.bgWell
                        border.width: 1
                        border.color: Theme.colors.borderHairline
                        radius: Theme.radius.sm

                        Label {
                            id: resultText
                            anchors.fill: parent
                            anchors.margins: 8
                            text: root.openTask ? root.openTask.result : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textPrimary
                            wrapMode: Text.Wrap
                        }
                    }
                }
            }

            // Transcript section
            Label {
                text: "transcript"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.micro
                color: Theme.colors.textGhost
                font.capitalization: Font.AllUppercase
            }

            ScrollView {
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true

                ColumnLayout {
                    width: transcriptSheet.width - Theme.space.xxxl * 2
                    spacing: Theme.space.lg

                    // Loading state
                    Label {
                        visible: root.transcriptLoading
                        text: "Loading…"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textMuted
                    }

                    Rectangle {
                        objectName: "taskTranscriptError"
                        visible: !root.transcriptLoading && root.transcriptError !== ""
                        Layout.fillWidth: true
                        implicitHeight: transcriptErrorContent.implicitHeight + Theme.space.lg * 2
                        color: Theme.colors.errorBg
                        border.width: 1
                        border.color: Theme.colors.error
                        radius: Theme.radius.sm

                        RowLayout {
                            id: transcriptErrorContent
                            anchors.fill: parent
                            anchors.margins: Theme.space.lg
                            spacing: Theme.space.md

                            Label {
                                id: transcriptErrorText
                                objectName: "taskTranscriptErrorText"
                                text: root.transcriptError
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                color: Theme.colors.error
                                wrapMode: Text.Wrap
                                Layout.fillWidth: true
                            }

                            AppButton {
                                objectName: "taskTranscriptErrorRetryButton"
                                text: "Retry"
                                variant: "ghost"
                                compact: true
                                toolTipText: "Retry transcript load"
                                onClicked: root.retryTranscript()
                            }
                        }
                    }

                    // Transcript entries
                    Rectangle {
                        objectName: "taskTranscriptElidedNotice"
                        visible: root.openTask !== null && !root.transcriptLoading && root.transcriptError === "" && root.transcriptElidedCount > 0
                        Layout.fillWidth: true
                        implicitHeight: elidedNoticeText.implicitHeight + Theme.space.md * 2
                        color: Theme.colors.bgWell
                        border.width: 1
                        border.color: Theme.colors.borderHairline
                        radius: Theme.radius.sm

                        Label {
                            id: elidedNoticeText
                            anchors.fill: parent
                            anchors.margins: Theme.space.md
                            text: root.transcriptElidedCount.toLocaleString()
                                + " earlier "
                                + (root.transcriptElidedCount === 1 ? "message" : "messages")
                                + " hidden; showing the most recent "
                                + transcriptEntries.count.toLocaleString()
                                + "."
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textMuted
                            wrapMode: Text.Wrap
                        }
                    }

                    Repeater {
                        model: root.openTask === null || root.transcriptLoading || root.transcriptError !== "" ? 0 : transcriptEntries.count
                        delegate: Rectangle {
                            Layout.fillWidth: true
                            implicitHeight: entryLayout.implicitHeight + Theme.space.lg * 2
                            color: Theme.colors.bgRaised
                            border.width: 1
                            border.color: transcriptEntries.get(index).toolError ? Theme.colors.error : Theme.colors.borderHairline
                            // Left accent border
                            Rectangle {
                                anchors.left: parent.left
                                anchors.top: parent.top
                                anchors.bottom: parent.bottom
                                width: 2
                                color: transcriptEntries.get(index).toolError ? Theme.colors.error : Theme.colors.borderSubtle
                            }
                            radius: Theme.radius.sm

                            ColumnLayout {
                                id: entryLayout
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                spacing: Theme.space.sm

                                // Header: role badge + tool name + error badge
                                RowLayout {
                                    spacing: Theme.space.sm
                                    Layout.fillWidth: true

                                    AppTag {
                                        text: transcriptEntries.get(index).role
                                        backgroundColor: roleToneBg(transcriptEntries.get(index).role)
                                        borderColor: "transparent"
                                        textColor: roleToneText(transcriptEntries.get(index).role)
                                        fontPixelSize: Theme.fontSize.label
                                        pill: false
                                    }

                                    Label {
                                        visible: transcriptEntries.get(index).toolName
                                        text: transcriptEntries.get(index).toolName
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.codeSm
                                        color: Theme.colors.textSecondary
                                    }

                                    AppTag {
                                        visible: transcriptEntries.get(index).toolError
                                        text: "error"
                                        backgroundColor: Theme.colors.errorBg
                                        borderColor: "transparent"
                                        textColor: Theme.colors.error
                                        fontPixelSize: Theme.fontSize.label
                                        pill: false
                                    }
                                }

                                // Reasoning (if present)
                                ColumnLayout {
                                    visible: transcriptEntries.get(index).reasoning
                                    spacing: Theme.space.xs
                                    Layout.fillWidth: true

                                    Label {
                                        text: "reasoning"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textGhost
                                        font.capitalization: Font.AllUppercase
                                    }

                                    Label {
                                        text: transcriptEntries.get(index).reasoning
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                        wrapMode: Text.Wrap
                                        Layout.fillWidth: true
                                    }
                                }

                                // Text content
                                Label {
                                    visible: transcriptEntries.get(index).text
                                    text: transcriptEntries.get(index).text
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textPrimary
                                    wrapMode: Text.Wrap
                                    Layout.fillWidth: true
                                }

                                // Tool calls
                                Repeater {
                                    model: transcriptEntries.get(index).toolCalls
                                    delegate: ColumnLayout {
                                        id: toolCallDelegate
                                        required property string name
                                        required property string args

                                        spacing: Theme.space.xs
                                        Layout.fillWidth: true

                                        Label {
                                            text: toolCallDelegate.name || "tool"
                                            font.family: Theme.monoFonts[0]
                                            font.pixelSize: Theme.fontSize.codeSm
                                            font.weight: Theme.fontWeight.medium
                                            color: Theme.colors.accent
                                        }

                                        Rectangle {
                                            visible: toolCallDelegate.args !== ""
                                            Layout.fillWidth: true
                                            implicitHeight: Math.min(280, argsText.implicitHeight + 16)
                                            color: Theme.colors.synBg
                                            border.width: 1
                                            border.color: Theme.colors.borderHairline
                                            radius: Theme.radius.sm

                                            ScrollView {
                                                anchors.fill: parent
                                                clip: true

                                                Label {
                                                    id: argsText
                                                    text: toolCallDelegate.args
                                                    font.family: Theme.monoFonts[0]
                                                    font.pixelSize: Theme.fontSize.codeSm
                                                    color: Theme.colors.synText
                                                    wrapMode: Text.Wrap
                                                    padding: 8
                                                }
                                            }
                                        }
                                    }
                                }

                                // Raw unparsed line
                                Rectangle {
                                    visible: transcriptEntries.get(index).raw
                                    Layout.fillWidth: true
                                    implicitHeight: Math.min(220, rawText.implicitHeight + 16)
                                    color: Theme.colors.synBg
                                    border.width: 1
                                    border.color: Theme.colors.borderHairline
                                    radius: Theme.radius.sm

                                    ScrollView {
                                        anchors.fill: parent
                                        clip: true

                                        Label {
                                            id: rawText
                                            text: transcriptEntries.get(index).raw
                                            font.family: Theme.monoFonts[0]
                                            font.pixelSize: Theme.fontSize.codeSm
                                            color: Theme.colors.textMuted
                                            wrapMode: Text.Wrap
                                            padding: 8
                                        }
                                    }
                                }
                            }
                        }
                    }

                    // Empty transcript message
                    Label {
                        objectName: "taskTranscriptEmpty"
                        visible: root.openTask !== null && !root.transcriptLoading && root.transcriptLoaded && root.transcriptError === "" && transcriptEntries.count === 0
                        text: "No transcript snapshot on disk for this task."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textMuted
                    }
                }
            }
        }
    }

    // Transcript entries model (parsed from .jsonl)
    ListModel {
        id: transcriptEntries
    }

    // Functions
    function statusDotColor(status) {
        if (status === "running") return Theme.colors.dotWorking
        if (status === "done") return Theme.colors.dotOk
        if (status === "error" || status === "lost") return Theme.colors.dotError
        if (status === "canceled") return Theme.colors.dotWarn
        return Theme.colors.dotIdle
    }

    function statusBgColor(status) {
        if (status === "running") return Theme.colors.brandBright + "20"
        if (status === "done") return Theme.colors.successBg
        if (status === "error" || status === "lost") return Theme.colors.errorBg
        if (status === "canceled") return Theme.colors.warnBg
        return Theme.colors.bgWell
    }

    function statusTextColor(status) {
        if (status === "running") return Theme.colors.brand
        if (status === "done") return Theme.colors.success
        if (status === "error" || status === "lost") return Theme.colors.error
        if (status === "canceled") return Theme.colors.warn
        return Theme.colors.textMuted
    }

    function roleToneBg(role) {
        if (role === "tool") return "rgba(91,176,196,0.12)"  // info tone
        if (role === "unparsed") return Theme.colors.warnBg
        return Theme.colors.bgWell
    }

    function roleToneText(role) {
        if (role === "tool") return Theme.colors.accent
        if (role === "unparsed") return Theme.colors.warn
        return Theme.colors.textSecondary
    }

    function openTranscript(taskData) {
        root.transcriptRequestSeq += 1
        var seq = root.transcriptRequestSeq
        root.openTask = taskData
        root.transcriptText = ""
        root.transcriptError = ""
        root.transcriptLoading = true
        root.transcriptLoaded = false
        root.transcriptElidedCount = 0
        transcriptEntries.clear()

        if (!root.rpcClient) {
            root.transcriptLoading = false
            root.transcriptLoaded = true
            root.transcriptError = "Could not load transcript: RPC client is unavailable."
            return
        }

        // Call AgentTranscript RPC
        var token = root.rpcClient.callToken("AgentTranscript", [taskData.taskId])
        var tokens = root.transcriptCallTokens
        tokens[token] = seq
        root.transcriptCallTokens = tokens
    }

    function closeTranscript() {
        root.transcriptRequestSeq += 1
        root.transcriptCallTokens = ({})
        root.openTask = null
        root.transcriptText = ""
        root.transcriptError = ""
        root.transcriptLoading = false
        root.transcriptLoaded = false
        root.transcriptElidedCount = 0
        transcriptEntries.clear()
    }

    function retryTranscript() {
        if (root.openTask === null || root.transcriptLoading) return
        root.openTranscript(root.openTask)
    }

    // Track transcript RPC tokens
    property var transcriptCallTokens: ({})

    Connections {
        target: root.rpcClient ? root.rpcClient : null
        function onCallDone(token, payload) {
            var seq = root.transcriptCallTokens[token]
            if (seq === undefined) return
            var tokens = root.transcriptCallTokens
            delete tokens[token]
            root.transcriptCallTokens = tokens
            if (seq !== root.transcriptRequestSeq) return

            root.transcriptLoading = false
            root.transcriptLoaded = true

            if (payload.error) {
                root.transcriptError = "Could not load transcript: " + errorText(payload.error)
                return
            }

            var result = payload.result || {}
            var transcript = result.transcript || ""
            root.transcriptText = transcript

            // Parse .jsonl into transcript entries (cap at 200 tail like Svelte)
            parseTranscript(transcript)
        }
    }

    function errorText(error) {
        if (error === undefined || error === null) return "unknown error"
        if (typeof error === "string") return error
        if (error.message) return String(error.message)
        return JSON.stringify(error)
    }

    function parseTranscript(text) {
        root.transcriptElidedCount = 0
        if (!text.trim()) return

        var lines = text.split("\n")
        var allEntries = []

        for (var i = 0; i < lines.length; i++) {
            var line = lines[i].trim()
            if (!line) continue

            try {
                var obj = JSON.parse(line)
                var entry = {
                    i: allEntries.length,
                    role: obj.Role || obj.role || "message",
                    text: obj.Text || obj.text || "",
                    reasoning: obj.Reasoning || obj.reasoning || "",
                    toolName: obj.ToolName || obj.toolName || "",
                    toolError: obj.ToolError === true || obj.toolError === true,
                    toolCalls: [],
                    raw: ""
                }

                // Parse tool calls
                var rawCalls = obj.ToolCalls || obj.toolCalls || []
                if (Array.isArray(rawCalls)) {
                    for (var j = 0; j < rawCalls.length; j++) {
                        var c = rawCalls[j] || {}
                        var args = c.Arguments || c.Args || c.arguments || c.args
                        var argsStr = ""
                        if (typeof args === "string") {
                            argsStr = args
                        } else if (args !== undefined && args !== null) {
                            try {
                                argsStr = JSON.stringify(args, null, 2)
                            } catch (e) {
                                argsStr = ""
                            }
                        }
                        entry.toolCalls.push({
                            id: c.ID || c.Id || c.id || "",
                            name: c.Name || c.name || "",
                            args: argsStr
                        })
                    }
                }

                allEntries.push(entry)
            } catch (e) {
                // Tolerate bad line: keep as verbatim
                allEntries.push({
                    i: allEntries.length,
                    role: "unparsed",
                    text: "",
                    reasoning: "",
                    toolName: "",
                    toolError: false,
                    toolCalls: [],
                    raw: line
                })
            }
        }

        // Cap at 200 tail (like Svelte TX_MAX)
        var TX_MAX = 200
        var shown = allEntries.length > TX_MAX ? allEntries.slice(allEntries.length - TX_MAX) : allEntries
        root.transcriptElidedCount = Math.max(0, allEntries.length - shown.length)

        for (var k = 0; k < shown.length; k++) {
            transcriptEntries.append(shown[k])
        }
    }
}
