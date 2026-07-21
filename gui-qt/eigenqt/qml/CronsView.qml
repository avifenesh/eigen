import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Crons view - scheduled work inventory and guarded native controls.
Rectangle {
    id: root
    objectName: "cronsView"
    color: Theme.colors.bgBase

    property var cronsModel: null
    property bool addJobOpen: false
    property string addSpec: ""
    property string addCommand: ""
    property string confirmingKey: ""
    readonly property var crons: cronsModel ? cronsModel.crons || [] : []
    readonly property var timers: timerRows()
    readonly property var crontab: crontabRows()
    readonly property var pendingActions: cronsModel ? cronsModel.pending_actions || [] : []
    readonly property int qaTimerCount: timers.length
    readonly property int qaCrontabCount: crontab.length
    readonly property bool compact: width < 720

    onCronsModelChanged: syncActiveModel()
    onVisibleChanged: {
        if (!visible) confirmingKey = ""
        syncActiveModel()
    }
    Component.onCompleted: syncActiveModel()

    Connections {
        target: root.cronsModel

        function onJobAdded() {
            root.addSpec = ""
            root.addCommand = ""
            root.addJobOpen = false
        }
    }

    component FeedbackBanner: Rectangle {
        id: banner
        property string tone: "success"
        property string message: ""
        property string textObjectName: ""
        property string buttonObjectName: ""
        signal dismissed()

        Layout.fillWidth: true
        Layout.preferredHeight: visible ? Math.max(42, bannerText.implicitHeight + Theme.space.lg) : 0
        color: tone === "error" ? Theme.colors.errorBg : Theme.colors.successBg
        border.width: visible ? 1 : 0
        border.color: tone === "error" ? Theme.colors.error : Theme.colors.success

        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: Theme.space.xl
            anchors.rightMargin: Theme.space.xl
            spacing: Theme.space.md

            Label {
                id: bannerText
                objectName: banner.textObjectName
                text: banner.message
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: banner.tone === "error" ? Theme.colors.error : Theme.colors.textPrimary
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }

            AppButton {
                objectName: banner.buttonObjectName
                text: "X"
                compact: true
                toolTipText: "Dismiss message"
                Layout.preferredWidth: 28
                Layout.preferredHeight: 28
                onClicked: banner.dismissed()
            }
        }
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 56
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xxxl
                anchors.rightMargin: Theme.space.xxxl
                spacing: Theme.space.lg

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: 2

                    Label {
                        objectName: "cronsTitle"
                        text: "Crons"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.h3
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        objectName: "cronsSummary"
                        text: summaryText()
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        color: Theme.colors.textMuted
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }

                AppButton {
                    objectName: "cronsRefreshButton"
                    text: root.cronsModel && root.cronsModel.loading ? "Refreshing..." : "Refresh"
                    compact: true
                    toolTipText: "Refresh scheduled work"
                    enabled: root.cronsModel && !root.cronsModel.loading
                    Layout.preferredWidth: Math.max(86, implicitWidth)
                    Layout.preferredHeight: 32
                    onClicked: if (root.cronsModel) root.cronsModel.refresh()
                }
            }
        }

        FeedbackBanner {
            objectName: "cronsActionError"
            visible: root.cronsModel && root.cronsModel.action_error !== ""
            tone: "error"
            message: root.cronsModel ? root.cronsModel.action_error : ""
            textObjectName: "cronsActionErrorText"
            buttonObjectName: "cronsActionErrorDismissButton"
            onDismissed: if (root.cronsModel) root.cronsModel.clear_action_error()
        }

        FeedbackBanner {
            objectName: "cronsActionMessage"
            visible: root.cronsModel && root.cronsModel.action_message !== ""
            tone: "success"
            message: root.cronsModel ? root.cronsModel.action_message : ""
            textObjectName: "cronsActionMessageText"
            buttonObjectName: "cronsActionMessageDismissButton"
            onDismissed: if (root.cronsModel) root.cronsModel.clear_action_message()
        }

        Flickable {
            id: cronsFlick
            objectName: "cronsFlick"
            Layout.fillWidth: true
            Layout.fillHeight: true
            contentWidth: width
            contentHeight: contentColumn.implicitHeight + Theme.space.xxl
            clip: true

            ColumnLayout {
                id: contentColumn
                width: Math.min(parent.width - Theme.space.xl * 2, 1040)
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: Theme.space.lg

                Item { Layout.preferredHeight: Theme.space.xl }

                RowLayout {
                    visible: !root.addJobOpen
                    Layout.fillWidth: true
                    spacing: Theme.space.md

                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 2

                        Label {
                            text: "Automate a command"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                        }

                        Label {
                            text: "Create a crontab entry without leaving Eigen."
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                        }
                    }

                    AppButton {
                        objectName: "cronsScheduleJobButton"
                        text: "Schedule job"
                        variant: "primary"
                        compact: true
                        toolTipText: "Create a crontab job"
                        onClicked: {
                            root.confirmingKey = ""
                            root.addJobOpen = true
                        }
                    }
                }

                Rectangle {
                    objectName: "cronsAddPanel"
                    visible: root.addJobOpen
                    Layout.fillWidth: true
                    Layout.minimumWidth: 0
                    Layout.preferredHeight: visible ? addJobColumn.implicitHeight + Theme.space.xxl * 2 : 0
                    radius: Theme.radius.sm
                    color: Theme.colors.bgInset
                    border.width: 1
                    border.color: Theme.colors.borderSubtle

                    ColumnLayout {
                        id: addJobColumn
                        anchors.fill: parent
                        anchors.margins: Theme.space.xxl
                        spacing: Theme.space.lg

                        RowLayout {
                            Layout.fillWidth: true
                            spacing: Theme.space.md

                            ColumnLayout {
                                Layout.fillWidth: true
                                Layout.minimumWidth: 0
                                spacing: 2

                                Label {
                                    text: "Schedule a command"
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.body
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.textPrimary
                                }

                                Label {
                                    text: "The schedule is validated by the system bridge before your crontab changes."
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textMuted
                                    wrapMode: Text.Wrap
                                    Layout.fillWidth: true
                                }
                            }

                            AppButton {
                                objectName: "cronsAddCloseButton"
                                text: "X"
                                compact: true
                                toolTipText: "Close schedule form"
                                enabled: root.cronsModel && !root.cronsModel.adding_job
                                Layout.preferredWidth: 30
                                Layout.preferredHeight: 30
                                onClicked: root.addJobOpen = false
                            }
                        }

                        ColumnLayout {
                            Layout.fillWidth: true
                            Layout.minimumWidth: 0
                            spacing: Theme.space.sm

                            Label {
                                text: "Schedule"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.micro
                                font.weight: Theme.fontWeight.semibold
                                font.capitalization: Font.AllUppercase
                                color: Theme.colors.textFaint
                            }

                            AppTextField {
                                objectName: "cronsScheduleInput"
                                Layout.fillWidth: true
                                Layout.minimumWidth: 0
                                placeholderText: "@daily or 0 9 * * 1-5"
                                text: root.addSpec
                                enabled: root.cronsModel && !root.cronsModel.adding_job
                                onTextChanged: if (root.addSpec !== text) root.addSpec = text
                            }

                            Flow {
                                id: presetFlow
                                Layout.fillWidth: true
                                Layout.minimumWidth: 0
                                Layout.preferredHeight: childrenRect.height
                                spacing: Theme.space.sm

                                Repeater {
                                    model: [
                                        {"label": "Every hour", "spec": "@hourly", "key": "hourly"},
                                        {"label": "Every day 9am", "spec": "0 9 * * *", "key": "daily"},
                                        {"label": "Weekdays 9am", "spec": "0 9 * * 1-5", "key": "weekdays"},
                                        {"label": "Weekly Monday", "spec": "0 9 * * 1", "key": "weekly"}
                                    ]

                                    delegate: AppButton {
                                        required property var modelData
                                        objectName: "cronsPreset_" + modelData.key
                                        text: modelData.label
                                        compact: true
                                        pill: true
                                        variant: "ghost"
                                        selected: root.addSpec === modelData.spec
                                        enabled: root.cronsModel && !root.cronsModel.adding_job
                                        onClicked: root.addSpec = modelData.spec
                                    }
                                }
                            }
                        }

                        ColumnLayout {
                            Layout.fillWidth: true
                            Layout.minimumWidth: 0
                            spacing: Theme.space.sm

                            Label {
                                text: "Command"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.micro
                                font.weight: Theme.fontWeight.semibold
                                font.capitalization: Font.AllUppercase
                                color: Theme.colors.textFaint
                            }

                            AppTextField {
                                objectName: "cronsCommandInput"
                                Layout.fillWidth: true
                                Layout.minimumWidth: 0
                                placeholderText: "eigen run ... or notify-send 'standup'"
                                text: root.addCommand
                                enabled: root.cronsModel && !root.cronsModel.adding_job
                                onTextChanged: if (root.addCommand !== text) root.addCommand = text
                                onAccepted: root.submitJob()
                            }
                        }

                        Flow {
                            Layout.fillWidth: true
                            Layout.minimumWidth: 0
                            Layout.preferredHeight: childrenRect.height
                            spacing: Theme.space.sm

                            AppButton {
                                objectName: "cronsAddJobButton"
                                text: root.cronsModel && root.cronsModel.adding_job ? "Adding..." : "Add job"
                                variant: "primary"
                                enabled: root.cronsModel && !root.cronsModel.adding_job
                                    && root.addSpec.trim() !== "" && root.addCommand.trim() !== ""
                                onClicked: root.submitJob()
                            }

                            AppButton {
                                objectName: "cronsAddCancelButton"
                                text: "Cancel"
                                variant: "ghost"
                                enabled: root.cronsModel && !root.cronsModel.adding_job
                                onClicked: root.addJobOpen = false
                            }
                        }
                    }
                }

                Rectangle {
                    visible: root.cronsModel && root.cronsModel.loading && root.crons.length === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 120 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Label {
                        anchors.centerIn: parent
                        text: "Loading scheduled work..."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textMuted
                    }
                }

                Rectangle {
                    objectName: "cronsLoadError"
                    visible: root.cronsModel && root.cronsModel.load_error !== "" && root.crons.length === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 132 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.md

                        Label {
                            text: "Could not load scheduled work"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            objectName: "cronsLoadErrorText"
                            text: root.cronsModel ? root.cronsModel.load_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            Layout.alignment: Qt.AlignHCenter
                        }

                        AppButton {
                            objectName: "cronsLoadErrorRetry"
                            text: "Retry"
                            compact: true
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: if (root.cronsModel) root.cronsModel.refresh()
                        }
                    }
                }

                RefreshErrorBanner {
                    objectName: "cronsRefreshErrorBanner"
                    visible: root.cronsModel && root.cronsModel.load_error !== "" && root.crons.length > 0
                    message: root.cronsModel ? root.cronsModel.load_error : ""
                    textObjectName: "cronsRefreshErrorText"
                    retryObjectName: "cronsRefreshErrorRetry"
                    retryToolTipText: "Retry loading scheduled work"
                    onRetry: if (root.cronsModel) root.cronsModel.refresh()
                }

                Rectangle {
                    visible: root.cronsModel && !root.cronsModel.loading && root.cronsModel.load_error === ""
                        && root.crons.length === 0 && !root.addJobOpen
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 132 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Label {
                        anchors.centerIn: parent
                        text: "No scheduled work yet"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }
                }

                ColumnLayout {
                    visible: root.qaTimerCount > 0
                    Layout.fillWidth: true
                    spacing: Theme.space.md

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.space.sm

                        Rectangle {
                            width: 3
                            height: 14
                            radius: 2
                            color: Theme.colors.brandBright
                            Layout.alignment: Qt.AlignVCenter
                        }

                        Label {
                            text: "Systemd timers"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            font.weight: Theme.fontWeight.semibold
                            font.capitalization: Font.AllUppercase
                            color: Theme.colors.textFaint
                        }

                        Label {
                            text: String(root.qaTimerCount)
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            color: Theme.colors.textGhost
                        }

                        Item { Layout.fillWidth: true }

                        Label {
                            text: "by next run"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            font.capitalization: Font.AllUppercase
                            color: Theme.colors.textFaint
                        }
                    }

                    Repeater {
                        model: root.timers
                        delegate: Rectangle {
                            id: timerRow
                            readonly property var cron: modelData || ({})
                            readonly property string timerKey: "timer:" + String(cron.unit || "")
                            readonly property bool pending: root.pendingActions.indexOf(timerKey) >= 0
                            readonly property bool lead: index === 0 && cron.active && !root.isEmptyWhen(cron.next || "")
                            readonly property bool qaTextFits: !timerWhenLabel.truncated && !timerNameLabel.truncated
                                && !timerCommandLabel.truncated && !timerLastLabel.truncated
                                && timerRunButton.qaTextFits && timerEnableButton.qaTextFits
                            objectName: "cronsTimerRow_" + root.safeObjectName(cron.unit || cron.name || index)
                            Layout.fillWidth: true
                            implicitHeight: Math.max(82, timerRowLayout.implicitHeight + Theme.space.xxl)
                            radius: Theme.radius.md
                            color: lead ? Theme.colors.stateSelected : Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: lead ? Theme.colors.borderBrandFaint : Theme.colors.borderHairline

                            Rectangle {
                                visible: timerRow.lead
                                anchors.left: parent.left
                                anchors.top: parent.top
                                anchors.bottom: parent.bottom
                                width: 2
                                radius: 2
                                color: Theme.colors.brandBright
                            }

                            GridLayout {
                                id: timerRowLayout
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                rowSpacing: Theme.space.lg
                                columnSpacing: Theme.space.lg
                                columns: root.compact ? 1 : 3

                                ColumnLayout {
                                    Layout.preferredWidth: root.compact ? -1 : 88
                                    Layout.minimumWidth: root.compact ? 0 : 88
                                    Layout.fillWidth: root.compact
                                    spacing: 2

                                    Label {
                                        id: timerWhenLabel
                                        text: root.relativeText(cron.next || "")
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        font.weight: Theme.fontWeight.semibold
                                        color: timerRow.lead ? Theme.colors.brandBright : Theme.colors.textSecondary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        text: root.nextText(cron.next || "")
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textGhost
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }
                                }

                                ColumnLayout {
                                    Layout.fillWidth: true
                                    spacing: Theme.space.sm

                                    RowLayout {
                                        Layout.fillWidth: true
                                        spacing: Theme.space.sm

                                        Rectangle {
                                            width: 7
                                            height: 7
                                            radius: 4
                                            color: cron.active ? Theme.colors.dotOk : Theme.colors.dotIdle
                                            Layout.alignment: Qt.AlignVCenter
                                        }

                                        Label {
                                            id: timerNameLabel
                                            text: cron.name || cron.unit || "timer"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.bodySm
                                            font.weight: Theme.fontWeight.semibold
                                            color: Theme.colors.textPrimary
                                            elide: Text.ElideRight
                                            Layout.fillWidth: true
                                        }

                                        AppTag {
                                            text: cron.active ? "running" : "stopped"
                                            backgroundColor: cron.active ? Theme.colors.successBg : Theme.colors.bgOverlay
                                            borderColor: cron.active ? Theme.colors.success : Theme.colors.borderHairline
                                            textColor: Theme.colors.textSecondary
                                            minimumHeight: 21
                                        }

                                        AppTag {
                                            text: cron.enabled ? "enabled" : "disabled"
                                            backgroundColor: cron.enabled ? Theme.colors.accentBg : Theme.colors.bgOverlay
                                            borderColor: cron.enabled ? Theme.colors.borderAccentFaint : Theme.colors.borderHairline
                                            textColor: Theme.colors.textSecondary
                                            minimumHeight: 21
                                        }
                                    }

                                    Label {
                                        id: timerCommandLabel
                                        text: cron.command || ""
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.codeSm
                                        color: Theme.colors.textSecondary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        id: timerLastLabel
                                        visible: !root.isEmptyWhen(cron.last || "")
                                        text: "last " + cron.last
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textGhost
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }
                                }

                                Flow {
                                    Layout.fillWidth: root.compact
                                    Layout.preferredWidth: root.compact ? -1 : 176
                                    Layout.preferredHeight: childrenRect.height
                                    spacing: Theme.space.sm

                                    AppButton {
                                        id: timerRunButton
                                        objectName: "cronsTimerRunButton_" + root.safeObjectName(cron.unit || index)
                                        text: timerRow.pending ? "Applying..." : (cron.active ? "Stop" : "Start")
                                        variant: cron.active ? "secondary" : "primary"
                                        compact: true
                                        enabled: root.cronsModel && !timerRow.pending
                                        toolTipText: (cron.active ? "Stop " : "Start ") + String(cron.name || cron.unit || "timer")
                                        onClicked: if (root.cronsModel) {
                                            root.confirmingKey = ""
                                            root.cronsModel.set_timer(cron.unit || "", cron.active ? "stop" : "start")
                                        }
                                    }

                                    AppButton {
                                        id: timerEnableButton
                                        objectName: "cronsTimerEnableButton_" + root.safeObjectName(cron.unit || index)
                                        text: cron.enabled ? "Disable" : "Enable"
                                        variant: "ghost"
                                        compact: true
                                        enabled: root.cronsModel && !timerRow.pending
                                        toolTipText: (cron.enabled ? "Disable " : "Enable ") + String(cron.name || cron.unit || "timer")
                                        onClicked: if (root.cronsModel) {
                                            root.confirmingKey = ""
                                            root.cronsModel.set_timer(cron.unit || "", cron.enabled ? "disable" : "enable")
                                        }
                                    }
                                }
                            }
                        }
                    }
                }

                ColumnLayout {
                    visible: root.qaCrontabCount > 0
                    Layout.fillWidth: true
                    spacing: Theme.space.md

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.space.sm

                        Rectangle {
                            width: 3
                            height: 14
                            radius: 2
                            color: Theme.colors.textFaint
                            Layout.alignment: Qt.AlignVCenter
                        }

                        Label {
                            text: "Crontab"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            font.weight: Theme.fontWeight.semibold
                            font.capitalization: Font.AllUppercase
                            color: Theme.colors.textFaint
                        }

                        Label {
                            text: String(root.qaCrontabCount)
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            color: Theme.colors.textGhost
                        }
                    }

                    Repeater {
                        model: root.crontab
                        delegate: Rectangle {
                            id: tabRow
                            readonly property var cron: modelData || ({})
                            readonly property string resourceKey: root.crontabKey(cron.next || "", cron.command || "")
                            readonly property bool pending: root.pendingActions.indexOf(resourceKey) >= 0
                            readonly property bool confirming: root.confirmingKey === resourceKey
                            readonly property bool qaTextFits: !tabCadenceLabel.truncated && tabSpecTag.qaTextFits
                                && !tabCommandLabel.truncated && tabRemoveButton.qaTextFits
                                && (!tabConfirmButton.visible || tabConfirmButton.qaTextFits)
                            objectName: "cronsTabRow_" + root.safeObjectName(cron.next + "_" + cron.command || index)
                            Layout.fillWidth: true
                            implicitHeight: Math.max(64, tabRowLayout.implicitHeight + Theme.space.xxl)
                            radius: Theme.radius.md
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: Theme.colors.borderHairline

                            GridLayout {
                                id: tabRowLayout
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                rowSpacing: Theme.space.lg
                                columnSpacing: Theme.space.lg
                                columns: root.compact ? 1 : 3

                                ColumnLayout {
                                    Layout.preferredWidth: root.compact ? -1 : 170
                                    Layout.minimumWidth: root.compact ? 0 : 150
                                    Layout.fillWidth: root.compact
                                    spacing: Theme.space.xs

                                    Label {
                                        id: tabCadenceLabel
                                        text: root.cadence(cron.next || "") || "custom schedule"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        font.weight: Theme.fontWeight.medium
                                        color: Theme.colors.textPrimary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    AppTag {
                                        id: tabSpecTag
                                        text: cron.next || ""
                                        backgroundColor: Theme.colors.bgOverlay
                                        borderColor: Theme.colors.borderHairline
                                        textColor: Theme.colors.brandBright
                                        fontFamily: Theme.monoFonts[0]
                                        minimumHeight: 22
                                        pill: false
                                    }
                                }

                                Label {
                                    id: tabCommandLabel
                                    text: cron.command || ""
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.codeSm
                                    color: Theme.colors.textSecondary
                                    wrapMode: Text.WrapAnywhere
                                    Layout.fillWidth: true
                                }

                                Flow {
                                    Layout.fillWidth: root.compact
                                    Layout.preferredWidth: root.compact ? -1 : 250
                                    Layout.preferredHeight: childrenRect.height
                                    spacing: Theme.space.sm

                                    Label {
                                        visible: tabRow.confirming
                                        height: 28
                                        verticalAlignment: Text.AlignVCenter
                                        text: "Remove this job?"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textMuted
                                    }

                                    AppButton {
                                        id: tabConfirmButton
                                        objectName: "cronsRemoveConfirm_" + root.safeObjectName(resourceKey)
                                        visible: tabRow.confirming
                                        text: tabRow.pending ? "Removing..." : "Confirm"
                                        variant: "danger"
                                        compact: true
                                        enabled: root.cronsModel && !tabRow.pending
                                        onClicked: if (root.cronsModel) {
                                            root.cronsModel.remove_crontab(cron.next || "", cron.command || "")
                                            root.confirmingKey = ""
                                        }
                                    }

                                    AppButton {
                                        id: tabRemoveButton
                                        objectName: "cronsRemoveButton_" + root.safeObjectName(resourceKey)
                                        text: tabRow.pending ? "Removing..." : (tabRow.confirming ? "Cancel" : "Remove")
                                        variant: tabRow.confirming ? "secondary" : "ghost"
                                        compact: true
                                        enabled: root.cronsModel && !tabRow.pending
                                        onClicked: root.confirmingKey = tabRow.confirming ? "" : tabRow.resourceKey
                                    }
                                }
                            }
                        }
                    }
                }

                Item { Layout.preferredHeight: Theme.space.xl }
            }
        }
    }

    function syncActiveModel(activeOverride) {
        if (root.cronsModel && root.cronsModel.set_active) {
            root.cronsModel.set_active(activeOverride === undefined ? root.visible : activeOverride)
        }
    }

    function submitJob() {
        if (!root.cronsModel || root.cronsModel.adding_job) return
        root.confirmingKey = ""
        root.cronsModel.add_crontab(root.addSpec, root.addCommand)
    }

    function crontabKey(spec, command) {
        return "crontab:" + String(spec || "").trim() + "\n" + String(command || "").trim()
    }

    function timerRows() {
        var rows = []
        for (var i = 0; i < root.crons.length; i++) {
            var cron = root.crons[i] || {}
            if (cron.kind === "timer") rows.push(cron)
        }
        rows.sort(function(a, b) {
            var at = sortTime(a.next || "")
            var bt = sortTime(b.next || "")
            if (at === 0 && bt === 0) return String(a.name || "").localeCompare(String(b.name || ""))
            if (at === 0) return 1
            if (bt === 0) return -1
            return at - bt
        })
        return rows
    }

    function crontabRows() {
        var rows = []
        for (var i = 0; i < root.crons.length; i++) {
            var cron = root.crons[i] || {}
            if (cron.kind === "crontab") rows.push(cron)
        }
        return rows
    }

    function summaryText() {
        if (!root.cronsModel) return "no scheduler"
        if (root.crons.length === 0 && root.cronsModel.loading) return "loading schedule"
        var systemd = root.cronsModel.systemd_available ? "systemd available" : "systemd unavailable"
        return String(root.cronsModel.timers_count || 0) + " timers / "
            + String(root.cronsModel.crontab_count || 0) + " crontab / "
            + String(root.cronsModel.active_timer_count || 0) + " active / " + systemd
    }

    function sortTime(text) {
        text = String(text || "")
        if (root.isEmptyWhen(text)) return 0
        var date = new Date()
        if (text.indexOf("today ") === 0) {
            var hm = text.substring(6).split(":")
            if (hm.length !== 2) return 0
            date.setHours(Number(hm[0] || 0), Number(hm[1] || 0), 0, 0)
            return date.getTime()
        }
        var parsed = Date.parse(text.replace(" ", "T"))
        return isNaN(parsed) ? 0 : parsed
    }

    function relativeText(text) {
        text = String(text || "")
        if (root.isEmptyWhen(text)) return "idle"
        var ts = sortTime(text)
        if (ts <= 0) return text.indexOf("today ") === 0 ? "today" : text
        var delta = ts - Date.now()
        if (delta <= 0) return "due"
        var minutes = Math.round(delta / 60000)
        if (minutes < 60) return "in " + String(minutes) + "m"
        var hours = Math.round(minutes / 60)
        if (hours < 24) return "in " + String(hours) + "h"
        var days = Math.round(hours / 24)
        if (days < 14) return "in " + String(days) + "d"
        return "in " + String(Math.round(days / 7)) + "w"
    }

    function nextText(text) {
        text = String(text || "")
        if (text.indexOf("today ") === 0) return text.substring(6)
        return text || "idle"
    }

    function isEmptyWhen(text) {
        text = String(text || "")
        return text === "" || text === "-" || (text.length === 1 && text.charCodeAt(0) === 8212)
    }

    function cadence(spec) {
        spec = String(spec || "").trim()
        if (spec === "@hourly") return "hourly"
        if (spec === "@daily") return "daily"
        if (spec === "@midnight") return "daily at midnight"
        if (spec === "@weekly") return "weekly"
        if (spec === "@monthly") return "monthly"
        if (spec === "@yearly" || spec === "@annually") return "yearly"
        if (spec === "@reboot") return "on reboot"
        var fields = spec.split(/\s+/)
        if (fields.length !== 5) return ""
        var mi = fields[0]
        var h = fields[1]
        var dom = fields[2]
        var mon = fields[3]
        var dow = fields[4]
        if (mi !== "*" && h === "*" && dom === "*" && mon === "*" && dow === "*") return "every hour at :" + pad2(mi)
        if (mi.indexOf("*/") === 0 && h === "*" && dom === "*" && mon === "*" && dow === "*") return "every " + mi.substring(2) + "m"
        if (h.indexOf("*/") === 0 && mi === "0" && dom === "*" && mon === "*" && dow === "*") return "every " + h.substring(2) + "h"
        if (mi !== "*" && h !== "*" && dom === "*" && mon === "*" && dow === "*") return "daily at " + pad2(h) + ":" + pad2(mi)
        if (mi !== "*" && h !== "*" && dow !== "*" && dom === "*" && mon === "*") return "weekly at " + pad2(h) + ":" + pad2(mi)
        if (mi === "*" && h === "*") return "every minute"
        return ""
    }

    function pad2(value) {
        value = String(value || "")
        return value.length === 1 ? "0" + value : value
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]/g, "_")
    }
}
