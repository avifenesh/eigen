import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Crons view - read-only scheduled work snapshot.
Rectangle {
    id: root
    objectName: "cronsView"
    color: Theme.colors.bgBase

    property var cronsModel: null
    readonly property var crons: cronsModel ? cronsModel.crons || [] : []
    readonly property var timers: timerRows()
    readonly property var crontab: crontabRows()
    readonly property int qaTimerCount: timers.length
    readonly property int qaCrontabCount: crontab.length

    onCronsModelChanged: syncActiveModel()
    onVisibleChanged: syncActiveModel()
    Component.onCompleted: syncActiveModel()

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
                            text: root.cronsModel ? root.cronsModel.load_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            Layout.alignment: Qt.AlignHCenter
                        }

                        AppButton {
                            text: "Retry"
                            compact: true
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: if (root.cronsModel) root.cronsModel.refresh()
                        }
                    }
                }

                Rectangle {
                    visible: root.cronsModel && !root.cronsModel.loading && root.cronsModel.load_error === "" && root.crons.length === 0
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
                            readonly property bool lead: index === 0 && cron.active && !root.isEmptyWhen(cron.next || "")
                            readonly property bool qaTextFits: !timerWhenLabel.truncated && !timerNameLabel.truncated && !timerCommandLabel.truncated && !timerLastLabel.truncated
                            objectName: "cronsTimerRow_" + root.safeObjectName(cron.unit || cron.name || index)
                            Layout.fillWidth: true
                            implicitHeight: Math.max(82, timerRowLayout.implicitHeight + Theme.space.lg)
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

                            RowLayout {
                                id: timerRowLayout
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                spacing: Theme.space.lg

                                ColumnLayout {
                                    Layout.preferredWidth: 88
                                    Layout.minimumWidth: 88
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

                                        Rectangle {
                                            height: 21
                                            implicitWidth: activeBadgeLabel.implicitWidth + Theme.space.md
                                            radius: 11
                                            color: cron.active ? Theme.colors.successBg : Theme.colors.bgOverlay
                                            border.width: 1
                                            border.color: cron.active ? Theme.colors.success : Theme.colors.borderHairline

                                            Label {
                                                id: activeBadgeLabel
                                                anchors.centerIn: parent
                                                text: cron.active ? "running" : "stopped"
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textSecondary
                                            }
                                        }

                                        Rectangle {
                                            height: 21
                                            implicitWidth: enabledBadgeLabel.implicitWidth + Theme.space.md
                                            radius: 11
                                            color: cron.enabled ? Theme.colors.accentBg : Theme.colors.bgOverlay
                                            border.width: 1
                                            border.color: cron.enabled ? Theme.colors.borderAccentFaint : Theme.colors.borderHairline

                                            Label {
                                                id: enabledBadgeLabel
                                                anchors.centerIn: parent
                                                text: cron.enabled ? "enabled" : "disabled"
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textSecondary
                                            }
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
                            readonly property bool qaTextFits: !tabCadenceLabel.truncated && !tabSpecLabel.truncated && !tabCommandLabel.truncated
                            objectName: "cronsTabRow_" + root.safeObjectName(cron.next + "_" + cron.command || index)
                            Layout.fillWidth: true
                            implicitHeight: Math.max(64, tabRowLayout.implicitHeight + Theme.space.lg)
                            radius: Theme.radius.md
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: Theme.colors.borderHairline

                            RowLayout {
                                id: tabRowLayout
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                spacing: Theme.space.lg

                                ColumnLayout {
                                    Layout.preferredWidth: 170
                                    Layout.minimumWidth: 150
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

                                    Rectangle {
                                        implicitWidth: tabSpecLabel.implicitWidth + Theme.space.md
                                        implicitHeight: 22
                                        radius: Theme.radius.xs
                                        color: Theme.colors.bgOverlay
                                        border.width: 1
                                        border.color: Theme.colors.borderHairline

                                        Label {
                                            id: tabSpecLabel
                                            anchors.centerIn: parent
                                            text: cron.next || ""
                                            font.family: Theme.monoFonts[0]
                                            font.pixelSize: Theme.fontSize.micro
                                            color: Theme.colors.brandBright
                                        }
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
