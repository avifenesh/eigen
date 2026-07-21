import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Home dashboard view: cockpit, stats strip, Today/Inbox/Machine/GPU cards, Act-On feed, working-now, resume.
Rectangle {
    id: root
    color: Theme.colors.bgBase

    signal sessionClicked(string sessionId)
    signal newSessionClicked()
    signal sessionStarted(string sessionId)

    property var dashboardModel: null  // DashboardModel from Python
    property var feedModel: null       // FeedModel from Python
    property var sessionsModel: null   // SessionsModel (for working/recent slices)
    property var statsData: null       // daemon.DaemonStats from main
    property var rpcClient: null       // RpcClient from Python
    property var pendingActions: ({})
    property var tokenActions: ({})
    property string actionError: ""
    property int feedCount: 0
    readonly property int qaFeedCount: feedCount
    readonly property bool qaDashboardLoaded: dashboardLoaded()
    readonly property bool qaFeedFresh: feedFresh()
    readonly property bool qaSessionsLoaded: sessionsLoaded()
    readonly property bool qaStatsReady: statsReady()

    Connections {
        target: root.rpcClient ? root.rpcClient : null
        function onCallDone(token, payload) {
            root.handleCallDone(token, payload)
        }
    }

    Connections {
        target: root.feedModel ? root.feedModel : null
        ignoreUnknownSignals: true
        function onModelReset() { root.syncFeedCount() }
        function onRowsInserted() { root.syncFeedCount() }
        function onRowsRemoved() { root.syncFeedCount() }
    }

    onFeedModelChanged: syncFeedCount()

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]+/g, "_")
    }

    function errorText(error) {
        if (error === undefined || error === null) return "Something went wrong."
        if (typeof error === "string") return error
        if (error.message) return String(error.message)
        return JSON.stringify(error)
    }

    function isPending(key) {
        return pendingActions[key] === true
    }

    function setPending(key, pending) {
        var next = Object.assign({}, pendingActions)
        if (pending) next[key] = true
        else delete next[key]
        pendingActions = next
    }

    function rememberToken(token, key, method) {
        var next = Object.assign({}, tokenActions)
        next[token] = { key: key, method: method }
        tokenActions = next
    }

    function forgetToken(token) {
        var next = Object.assign({}, tokenActions)
        delete next[token]
        tokenActions = next
    }

    function startNewSession() {
        var key = "new-session"
        if (!rpcClient || isPending(key)) return
        actionError = ""
        setPending(key, true)
        var token = rpcClient.callToken("NewSession", ["", "", ""])
        rememberToken(token, key, "NewSession")
    }

    function startFeedItem(feedKey, dir, task, url) {
        if (!task) {
            if (url) Qt.openUrlExternally(url)
            return
        }
        var key = "feed:" + feedKey
        if (!rpcClient || isPending(key)) return
        actionError = ""
        setPending(key, true)
        var token = rpcClient.callToken("StartFromFeed", [dir || "", task])
        rememberToken(token, key, "StartFromFeed")
    }

    function handleCallDone(token, payload) {
        var action = tokenActions[token]
        if (!action) return
        forgetToken(token)
        setPending(action.key, false)
        if (payload && payload.error !== undefined && payload.error !== null) {
            actionError = errorText(payload.error)
            return
        }
        var sessionId = payload ? String(payload.result || "") : ""
        if (sessionId !== "") {
            sessionStarted(sessionId)
        }
    }

    // Inline component definitions
    component StatItem: ColumnLayout {
        property string label: ""
        property var value: ""
        property bool ready: true
        property bool highlight: false

        spacing: Theme.space.xs

        Label {
            objectName: "homeStatValue_" + root.safeObjectName(label)
            readonly property string qaText: text
            text: ready && value !== undefined && value !== null ? String(value) : "—"
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.h3
            font.weight: Theme.fontWeight.semibold
            color: highlight ? Theme.colors.brandBright : Theme.colors.textPrimary
        }

        Label {
            text: label
            font.pixelSize: Theme.fontSize.micro
            color: Theme.colors.textMuted
            textFormat: Text.PlainText
        }
    }

    component DashboardPanel: Rectangle {
        property string icon: ""
        property string title: ""
        property string badge: ""
        property string tempLabel: ""
        property bool tempHot: false
        property bool tempWarm: false
        property Item content: null

        color: Theme.colors.bgRaised
        border.width: 1
        border.color: Theme.colors.borderHairline
        radius: Theme.radius.lg

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: Theme.space.lg
            spacing: Theme.space.sm

            RowLayout {
                spacing: Theme.space.sm

                Label {
                    text: icon
                    font.pixelSize: Theme.fontSize.body
                    color: Theme.colors.textMuted
                }

                Label {
                    text: title
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.label
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textMuted
                    textFormat: Text.PlainText
                }

                Item { Layout.fillWidth: true }

                AppTag {
                    objectName: "homePanelBadge_" + root.safeObjectName(title)
                    visible: badge !== ""
                    text: badge
                    backgroundColor: Theme.colors.stateSelected
                    borderColor: Theme.colors.borderBrandFaint
                    textColor: Theme.colors.brandBright
                    fontPixelSize: Theme.fontSize.micro
                    fontWeight: Theme.fontWeight.bold
                    minimumHeight: 20
                }

                Label {
                    visible: tempLabel !== ""
                    text: tempLabel
                    font.family: Theme.monoFonts[0]
                    font.pixelSize: Theme.fontSize.label
                    font.weight: tempHot ? Theme.fontWeight.bold : Theme.fontWeight.regular
                    color: tempHot ? Theme.colors.error : (tempWarm ? Theme.colors.warn : Theme.colors.textMuted)
                }
            }

            Item {
                Layout.fillWidth: true
                Layout.fillHeight: true
                children: content ? [content] : []
            }
        }
    }

    component MetricRow: ColumnLayout {
        property string label: ""
        property string value: ""
        property real pct: 0
        property string qaName: ""

        spacing: Theme.space.xs

        RowLayout {
            Layout.fillWidth: true
            spacing: Theme.space.lg

            Label {
                text: label
                font.pixelSize: Theme.fontSize.label
                color: Theme.colors.textMuted
            }

            Item { Layout.fillWidth: true }

            Label {
                objectName: qaName
                readonly property string qaText: text
                text: value
                font.pixelSize: Theme.fontSize.label
                color: Theme.colors.textSecondary
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 6
            radius: 3
            color: Theme.colors.bgInset

            Rectangle {
                width: Math.min(parent.width, parent.width * (pct / 100))
                height: parent.height
                radius: parent.radius
                color: {
                    if (pct >= 90) return Theme.colors.error
                    if (pct >= 70) return Theme.colors.warn
                    return Theme.colors.brand
                }
                Behavior on width { NumberAnimation { duration: Theme.duration.slow } }
            }
        }
    }

    component GPUCard: Rectangle {
        property var gpu: null

        color: Theme.colors.bgRaised
        border.width: 1
        border.color: Theme.colors.borderHairline
        radius: Theme.radius.lg
        height: 120

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: Theme.space.lg
            spacing: Theme.space.lg

            RowLayout {
                spacing: Theme.space.sm

                Label {
                    text: "▣"
                    color: Theme.colors.textMuted
                }

                Label {
                    text: gpu ? gpu.name : ""
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textPrimary
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }

                Label {
                    visible: gpu && gpu.powerW > 0
                    text: Math.round(gpu.powerW) + "W"
                    font.pixelSize: Theme.fontSize.label
                    color: Theme.colors.textFaint
                }

                Label {
                    visible: !!gpu
                    text: Math.round(gpu.tempC) + "°C"
                    font.pixelSize: Theme.fontSize.label
                    color: {
                        if (gpu.tempC >= 90) return Theme.colors.error
                        if (gpu.tempC >= 80) return Theme.colors.warn
                        return Theme.colors.textMuted
                    }
                    font.weight: gpu && gpu.tempC >= 90 ? Theme.fontWeight.bold : Theme.fontWeight.regular
                }
            }

            ColumnLayout {
                Layout.fillWidth: true
                spacing: Theme.space.lg

                MetricRow {
                    label: "GPU util"
                    value: gpu ? Math.round(gpu.utilPct) + "%" : "0%"
                    pct: gpu ? gpu.utilPct : 0
                }

                MetricRow {
                    label: "VRAM"
                    value: gpu ? gpu.memUsedGb.toFixed(1) + "/" + gpu.memTotalGb.toFixed(1) + " GB" : "0/0 GB"
                    pct: gpu ? gpu.memUsedPct : 0
                }
            }
        }
    }

    component FeedCard: Rectangle {
        property string kind: ""
        property string title: ""
        property string detail: ""
        property string dirName: ""
        property string url: ""
        property string task: ""
        property string feedKey: ""
        property bool starting: false

        signal dismissed()
        signal startClicked()

        color: mouseArea.containsMouse ? Theme.colors.bgRaised2 : Theme.colors.bgRaised
        border.width: 1
        border.color: Theme.colors.borderHairline
        radius: Theme.radius.md
        height: detailText.visible ? 110 : 80

        Behavior on color { ColorAnimation { duration: Theme.duration.fast } }

        MouseArea {
            id: mouseArea
            anchors.fill: parent
            hoverEnabled: true
        }

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: Theme.space.lg
            spacing: Theme.space.sm

            RowLayout {
                spacing: Theme.space.sm

                Label {
                    text: kindGlyph(kind)
                    font.pixelSize: Theme.fontSize.body
                    color: kindColor(kind)
                }

                Label {
                    text: title
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textPrimary
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }

                AppButton {
                    objectName: "homeFeedDismiss_" + root.safeObjectName(feedKey)
                    text: "×"
                    onClicked: dismissed()
                    variant: "ghost"
                    compact: true
                    toolTipText: "Dismiss"
                    Layout.preferredWidth: 24
                    Layout.preferredHeight: 24
                }
            }

            Label {
                id: detailText
                visible: detail !== ""
                text: detail
                font.pixelSize: Theme.fontSize.label
                color: Theme.colors.textMuted
                wrapMode: Text.WordWrap
                maximumLineCount: 2
                elide: Text.ElideRight
                Layout.fillWidth: true
            }

            RowLayout {
                spacing: Theme.space.sm
                Layout.topMargin: Theme.space.xs

                AppTag {
                    objectName: "homeFeedDirTag_" + root.safeObjectName(feedKey)
                    visible: dirName !== ""
                    text: dirName
                    backgroundColor: Theme.colors.bgInset
                    borderColor: Theme.colors.borderHairline
                    textColor: Theme.colors.textMuted
                    fontPixelSize: Theme.fontSize.label
                    pill: false
                }

                Item { Layout.fillWidth: true }

                AppButton {
                    objectName: "homeFeedOpen_" + root.safeObjectName(feedKey)
                    visible: url !== ""
                    text: "Open"
                    onClicked: Qt.openUrlExternally(url)
                    variant: "ghost"
                    compact: true
                    Layout.preferredHeight: 28
                }

                AppButton {
                    objectName: "homeFeedStart_" + root.safeObjectName(feedKey)
                    visible: task !== ""
                    enabled: !starting
                    text: starting ? "Starting..." : "Start"
                    onClicked: startClicked()
                    variant: "primary"
                    compact: true
                    Layout.preferredHeight: 28
                }
            }
        }

        function kindGlyph(k) {
            if (k === "git") return "±"
            if (k === "github") return "◉"
            if (k === "memory") return "↺"
            if (k === "suggest") return "✧"
            return "•"
        }

        function kindColor(k) {
            if (k === "git") return Theme.colors.warn
            if (k === "github") return Theme.colors.accent
            if (k === "memory") return Theme.colors.brand
            if (k === "suggest") return Theme.colors.success
            return Theme.colors.textSecondary
        }
    }

    component LiveSessionRow: Rectangle {
        property var sessionData: null
        signal clicked()

        color: mouseArea.containsMouse ? Theme.colors.bgRaised2 : Theme.colors.bgRaised
        border.width: 1
        border.color: Theme.colors.borderHairline
        property color borderLeftColor: sessionData && sessionData.status === "working" ? Theme.colors.brand : Theme.colors.warn
        radius: Theme.radius.md
        height: 56

        Behavior on color { ColorAnimation { duration: Theme.duration.fast } }

        // Left border hack (QML doesn't support per-side border widths)
        Rectangle {
            anchors.left: parent.left
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            width: 2
            color: parent.borderLeftColor
            radius: parent.radius
        }

        MouseArea {
            id: mouseArea
            anchors.fill: parent
            hoverEnabled: true
            onClicked: parent.clicked()
            cursorShape: Qt.PointingHandCursor
        }

        RowLayout {
            anchors.fill: parent
            anchors.margins: Theme.space.lg
            spacing: Theme.space.lg

            // Status dot
            Rectangle {
                width: 8
                height: 8
                radius: 4
                color: Theme.statusColor(sessionData ? sessionData.status : "idle")
                SequentialAnimation on opacity {
                    running: Theme.continuousMotion && sessionData && (sessionData.status === "working" || sessionData.status === "approval")
                    loops: Animation.Infinite
                    NumberAnimation { from: 1.0; to: 0.3; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
                    NumberAnimation { from: 0.3; to: 1.0; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
                }
            }

            Label {
                text: sessionData ? sessionData.title : ""
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                font.weight: Theme.fontWeight.medium
                color: Theme.colors.textPrimary
                Layout.fillWidth: true
            }

            AppTag {
                objectName: "homeLiveApprovalTag_" + root.safeObjectName(sessionData ? sessionData.id : "")
                visible: sessionData && sessionData.status === "approval"
                text: "needs approval"
                backgroundColor: Theme.colors.warnBg
                borderColor: Theme.colors.warn
                textColor: Theme.colors.warn
                fontPixelSize: Theme.fontSize.label
                pill: false
            }

            Label {
                text: sessionData ? sessionData.dir.split('/').pop() : ""
                font.pixelSize: Theme.fontSize.label
                color: Theme.colors.textMuted
            }
        }
    }

    component ResumeSessionRow: Rectangle {
        property var sessionData: null
        property bool showDivider: true
        signal clicked()

        color: mouseArea.containsMouse ? Theme.colors.stateHover : "transparent"
        height: 48
        border.width: 0

        Behavior on color { ColorAnimation { duration: Theme.duration.fast } }

        MouseArea {
            id: mouseArea
            anchors.fill: parent
            hoverEnabled: true
            onClicked: parent.clicked()
            cursorShape: Qt.PointingHandCursor
        }

        RowLayout {
            anchors.fill: parent
            anchors.margins: Theme.space.sm
            spacing: Theme.space.lg

            Rectangle {
                width: 6
                height: 6
                radius: 3
                color: Theme.statusColor(sessionData ? sessionData.status : "idle")
            }

            Label {
                text: sessionData ? sessionData.title : ""
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textPrimary
                elide: Text.ElideRight
                Layout.fillWidth: true
            }

            Label {
                text: sessionData ? sessionData.dir.split('/').pop() : ""
                font.pixelSize: Theme.fontSize.label
                color: Theme.colors.textMuted
            }

            Label {
                text: sessionData ? sessionData.turns + " turn" + (sessionData.turns === 1 ? "" : "s") : ""
                font.pixelSize: Theme.fontSize.label
                color: Theme.colors.textGhost
                Layout.minimumWidth: 64
                horizontalAlignment: Text.AlignRight
            }

            Label {
                text: sessionData ? relTime(sessionData.updated) : ""
                font.pixelSize: Theme.fontSize.label
                color: Theme.colors.textFaint
                Layout.minimumWidth: 64
                horizontalAlignment: Text.AlignRight
            }
        }

        Rectangle {
            visible: showDivider
            anchors.bottom: parent.bottom
            anchors.left: parent.left
            anchors.right: parent.right
            height: 1
            color: Theme.colors.divider
        }

        function relTime(ts) {
            ts = timestampMs(ts)
            if (!ts) return ""
            var now = Date.now()
            var diff = Math.max(0, Math.floor((now - ts) / 1000))
            if (diff < 60) return "just now"
            if (diff < 3600) return Math.floor(diff / 60) + "m ago"
            if (diff < 86400) return Math.floor(diff / 3600) + "h ago"
            return Math.floor(diff / 86400) + "d ago"
        }

        function timestampMs(ts) {
            var value = Number(ts || 0)
            if (!isFinite(value) || value <= 0) return 0
            if (value > 100000000000000) return Math.floor(value / 1000000)  // ns -> ms
            if (value < 10000000000) return value * 1000  // s -> ms
            return value
        }
    }

    Flickable {
        anchors.fill: parent
        contentWidth: parent.width
        contentHeight: contentColumn.height
        clip: true

        ColumnLayout {
            id: contentColumn
            width: Math.min(parent.width - Theme.space.xxxxl, 1080)
            anchors.horizontalCenter: parent.horizontalCenter
            spacing: Theme.space.xl

            // ZONE 1: COCKPIT
            RowLayout {
                Layout.fillWidth: true
                Layout.topMargin: Theme.space.xl
                spacing: Theme.space.xl

                ColumnLayout {
                    spacing: Theme.space.xs

                    Label {
                        text: greeting()
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.h2
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        text: "Your agent, everywhere — here's what's worth your attention."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textMuted
                    }
                }

                Item { Layout.fillWidth: true }

                AppButton {
                    objectName: "homeStartSessionButton"
                    text: root.isPending("new-session") ? "Starting..." : "Start a session"
                    enabled: !root.isPending("new-session")
                    onClicked: root.startNewSession()
                    variant: "primary"
                    Layout.preferredHeight: 36
                    Layout.preferredWidth: 140
                }
            }

            Rectangle {
                objectName: "homeActionErrorBanner"
                visible: root.actionError !== ""
                Layout.fillWidth: true
                Layout.preferredHeight: visible ? homeActionErrorText.implicitHeight + Theme.space.lg * 2 : 0
                color: Theme.colors.errorBg
                border.width: 1
                border.color: Theme.colors.error
                radius: Theme.radius.md

                RowLayout {
                    anchors.fill: parent
                    anchors.leftMargin: Theme.space.lg
                    anchors.rightMargin: Theme.space.lg
                    anchors.topMargin: Theme.space.lg
                    anchors.bottomMargin: Theme.space.lg
                    spacing: Theme.space.lg

                    Label {
                        id: homeActionErrorText
                        objectName: "homeActionErrorText"
                        text: root.actionError
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.error
                        wrapMode: Text.WordWrap
                        Layout.fillWidth: true
                    }

                    AppButton {
                        objectName: "homeActionErrorDismissButton"
                        text: "X"
                        onClicked: root.actionError = ""
                        variant: "ghost"
                        compact: true
                        toolTipText: "Dismiss home error"
                        Layout.preferredWidth: 28
                        Layout.preferredHeight: 28
                    }
                }
            }

            // Stats strip
            Rectangle {
                Layout.fillWidth: true
                Layout.preferredHeight: 64
                color: Theme.colors.bgRaised
                border.width: 1
                border.color: Theme.colors.borderHairline
                radius: Theme.radius.lg

                RowLayout {
                    anchors.fill: parent
                    anchors.margins: Theme.space.lg
                    spacing: Theme.space.xxxl

                    StatItem {
                        label: "sessions"
                        ready: root.sessionStatReady()
                        value: root.sessionStatValue()
                    }

                    Rectangle {
                        width: 1
                        Layout.fillHeight: true
                        color: Theme.colors.divider
                    }

                    StatItem {
                        label: "running"
                        ready: root.statsReady()
                        value: root.statsValue("running_turns")
                        highlight: ready && Number(value) > 0
                    }

                    Rectangle {
                        width: 1
                        Layout.fillHeight: true
                        color: Theme.colors.divider
                    }

                    StatItem {
                        label: "tasks"
                        ready: root.statsReady()
                        value: root.statsValue("bg_tasks")
                    }

                    Rectangle {
                        width: 1
                        Layout.fillHeight: true
                        color: Theme.colors.divider
                    }

                    StatItem {
                        label: "cache hit"
                        ready: root.statsReady()
                        value: root.cacheHitPct() + "%"
                    }

                    Item { Layout.fillWidth: true }
                }
            }

            // ZONE 1.5: TODAY (command center: calendar, mail, machine)
            GridLayout {
                Layout.fillWidth: true
                columns: 3
                columnSpacing: Theme.space.lg
                rowSpacing: Theme.space.lg

                // Calendar panel
                DashboardPanel {
                    icon: "◷"
                    title: "Today"
                    badge: ""
                    Layout.fillWidth: true
                    Layout.minimumHeight: 140

                    content: Item {
                        anchors.fill: parent
                        Label {
                            objectName: "homeDashboardState_Today"
                            readonly property string qaText: text
                            visible: !root.dashboardLoaded() || !dashboardModel.google_connected || dashboardModel.events.length === 0
                            anchors.centerIn: parent
                            text: root.dashboardSectionText("calendar")
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textGhost
                        }
                        ListView {
                            visible: root.dashboardLoaded() && dashboardModel.google_connected && dashboardModel.events.length > 0
                            anchors.fill: parent
                            model: dashboardModel ? dashboardModel.events.slice(0, 5) : []
                            spacing: Theme.space.sm
                            delegate: RowLayout {
                                width: ListView.view.width
                                spacing: Theme.space.lg
                                Label {
                                    text: formatEventTime(modelData.start, modelData.allDay)
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.brandBright
                                    Layout.preferredWidth: 64
                                }
                                Label {
                                    text: modelData.summary
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textSecondary
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                            }
                        }
                    }
                }

                // Inbox panel
                DashboardPanel {
                    icon: "⊠"
                    title: "Inbox"
                    badge: root.dashboardLoaded() && dashboardModel.google_connected && dashboardModel.unread_count > 0 ? dashboardModel.unread_count.toString() : ""
                    Layout.fillWidth: true
                    Layout.minimumHeight: 140

                    content: Item {
                        anchors.fill: parent
                        Label {
                            objectName: "homeDashboardState_Inbox"
                            readonly property string qaText: text
                            visible: !root.dashboardLoaded() || !dashboardModel.google_connected || dashboardModel.unread.length === 0
                            anchors.centerIn: parent
                            text: root.dashboardSectionText("inbox")
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textGhost
                        }
                        ListView {
                            visible: root.dashboardLoaded() && dashboardModel.google_connected && dashboardModel.unread.length > 0
                            anchors.fill: parent
                            model: dashboardModel ? dashboardModel.unread.slice(0, 5) : []
                            spacing: Theme.space.sm
                            delegate: RowLayout {
                                width: ListView.view.width
                                spacing: Theme.space.lg
                                Label {
                                    text: extractFromName(modelData.from)
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textPrimary
                                    elide: Text.ElideRight
                                    Layout.preferredWidth: 96
                                }
                                Label {
                                    text: modelData.subject
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textSecondary
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                            }
                        }
                    }
                }

                // Machine health panel
                DashboardPanel {
                    icon: "▦"
                    title: "Machine"
                    badge: ""
                    tempLabel: root.healthText("cpuTempC", 1, "°C", false)
                    tempHot: root.healthNumber("cpuTempC") >= 90
                    tempWarm: root.healthNumber("cpuTempC") >= 80
                    Layout.fillWidth: true
                    Layout.minimumHeight: 140

                    content: ColumnLayout {
                        anchors.fill: parent
                        spacing: Theme.space.lg

                        // CPU load
                        MetricRow {
                            label: "CPU load"
                            qaName: "homeMachineCpuValue"
                            value: root.healthText("loadPerCpu", 100, "%", false)
                            pct: root.healthPercent("loadPerCpu", 100)
                        }

                        // Memory
                        MetricRow {
                            label: "Memory"
                            qaName: "homeMachineMemoryValue"
                            value: root.memoryText()
                            pct: root.healthPercent("memUsedPct", 1)
                        }

                        // Disk
                        MetricRow {
                            label: "Disk /"
                            qaName: "homeMachineDiskValue"
                            value: root.healthText("diskUsedPct", 1, "%", false)
                            pct: root.healthPercent("diskUsedPct", 1)
                        }
                    }
                }
            }

            // GPU cards (full-width)
            GridLayout {
                visible: dashboardModel && dashboardModel.gpus.length > 0
                Layout.fillWidth: true
                columns: 2
                columnSpacing: Theme.space.lg
                rowSpacing: Theme.space.lg

                Repeater {
                    model: dashboardModel ? dashboardModel.gpus : []
                    delegate: GPUCard {
                        gpu: modelData
                        Layout.fillWidth: true
                    }
                }
            }

            // ZONE 2: ACT ON (feed)
            ColumnLayout {
                Layout.fillWidth: true
                spacing: Theme.space.lg

                RowLayout {
                    spacing: Theme.space.lg
                    Label {
                        text: "Act on"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.label
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textFaint
                        textFormat: Text.PlainText
                    }
                    Label {
                        objectName: "homeFeedCount"
                        visible: root.feedCount > 0
                        text: root.feedCount
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.textGhost
                    }
                }

                Label {
                    objectName: "homeFeedEmpty"
                    visible: root.feedCount === 0
                    text: root.feedFresh()
                        ? "Nothing loose to act on — clean tree, no open work."
                        : "Scanning your projects for things to act on…"
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                }

                GridLayout {
                    objectName: "homeFeedGrid"
                    visible: root.feedCount > 0
                    Layout.fillWidth: true
                    columns: 2
                    columnSpacing: Theme.space.lg
                    rowSpacing: Theme.space.lg

                    Repeater {
                        model: feedModel
                        delegate: FeedCard {
                            kind: model.kind
                            title: model.title
                            detail: model.detail
                            dirName: model.dirName
                            url: model.url
                            task: model.task
                            feedKey: model.key
                            starting: root.isPending("feed:" + model.key)
                            onDismissed: feedModel.dismiss(feedKey)
                            onStartClicked: {
                                root.startFeedItem(feedKey, model.dir, model.task, model.url)
                            }
                            Layout.fillWidth: true
                        }
                    }
                }
            }

            // ZONE 4: WORKING NOW (live sessions)
            ColumnLayout {
                visible: liveSessions().length > 0
                Layout.fillWidth: true
                spacing: Theme.space.lg

                RowLayout {
                    spacing: Theme.space.lg
                    Label {
                        text: "Working now"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.label
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textFaint
                    }
                    Label {
                        text: liveSessions().length
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.textGhost
                    }
                }

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Theme.space.sm

                    Repeater {
                        model: liveSessions()
                        delegate: LiveSessionRow {
                            sessionData: modelData
                            onClicked: root.sessionClicked(sessionData.id)
                            Layout.fillWidth: true
                        }
                    }
                }
            }

            // ZONE 5: RESUME (recent sessions)
            ColumnLayout {
                Layout.fillWidth: true
                spacing: Theme.space.lg

                RowLayout {
                    spacing: Theme.space.lg
                    Label {
                        text: "Resume"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.label
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textFaint
                    }
                }

                Label {
                    objectName: "homeResumeEmpty"
                    readonly property string qaText: text
                    visible: root.recentList.length === 0
                    text: root.resumeEmptyText()
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                }

                ColumnLayout {
                    visible: root.recentList.length > 0
                    Layout.fillWidth: true
                    spacing: 0

                    Repeater {
                        model: root.recentList.slice(0, 6)
                        delegate: ResumeSessionRow {
                            sessionData: modelData
                            onClicked: root.sessionClicked(sessionData.id)
                            Layout.fillWidth: true
                            showDivider: index < root.recentList.length - 1
                        }
                    }
                }
            }

            Item { Layout.preferredHeight: Theme.space.xxxl }
        }
    }

    // Helper functions
    function greeting() {
        var h = new Date().getHours()
        if (h < 5) return "Burning the midnight oil"
        if (h < 12) return "Good morning"
        if (h < 18) return "Good afternoon"
        return "Good evening"
    }

    function cacheHitPct() {
        if (!statsReady()) return 0
        var read = statsData.cache_read_tokens || 0
        var total = (statsData.input_tokens || 0) + read + (statsData.cache_write_tokens || 0)
        if (total <= 0) return 0
        return Math.min(100, Math.max(0, Math.round((read / total) * 100)))
    }

    function dashboardLoaded() {
        if (!dashboardModel) return false
        if (dashboardModel.loaded === undefined || dashboardModel.loaded === null) return true
        return !!dashboardModel.loaded
    }

    function dashboardLoadError() {
        if (!dashboardModel || dashboardModel.loadError === undefined || dashboardModel.loadError === null) return ""
        return String(dashboardModel.loadError)
    }

    function dashboardSectionText(section) {
        if (!dashboardLoaded()) {
            return dashboardLoadError().length > 0 ? "Dashboard unavailable — retrying." : "Loading…"
        }
        if (!dashboardModel.google_connected) {
            return section === "calendar"
                ? "Connect Google to see your calendar."
                : "Connect Google to see your inbox."
        }
        return section === "calendar" ? "No upcoming events." : "Inbox zero — nothing unread."
    }

    function feedFresh() {
        if (!feedModel) return false
        if (feedModel.fresh === undefined || feedModel.fresh === null) return true
        return !!feedModel.fresh
    }

    function sessionsLoaded() {
        if (!sessionsModel) return false
        if (sessionsModel.loaded === undefined || sessionsModel.loaded === null) return true
        return !!sessionsModel.loaded
    }

    function sessionsLoadError() {
        if (!sessionsModel || sessionsModel.loadError === undefined || sessionsModel.loadError === null) return ""
        return String(sessionsModel.loadError)
    }

    function statsReady() {
        return !!statsData && statsData.sessions !== undefined && statsData.sessions !== null
    }

    function statsValue(name) {
        if (!statsReady() || statsData[name] === undefined || statsData[name] === null) return 0
        return statsData[name]
    }

    function sessionStatReady() {
        return statsReady() || sessionsLoaded()
    }

    function sessionStatValue() {
        if (statsReady()) return statsData.sessions
        if (!sessionsLoaded()) return 0
        if (sessionsModel.totalCount !== undefined && sessionsModel.totalCount !== null) return sessionsModel.totalCount
        return sessionsModel.rowCount()
    }

    function healthNumber(name) {
        if (!dashboardLoaded() || !dashboardModel.health) return NaN
        var value = Number(dashboardModel.health[name])
        return isFinite(value) ? value : NaN
    }

    function healthPercent(name, scale) {
        var value = healthNumber(name)
        return isFinite(value) ? Math.max(0, Math.min(100, value * scale)) : 0
    }

    function healthText(name, scale, suffix, decimal) {
        var value = healthNumber(name)
        if (!isFinite(value)) return "—"
        var scaled = value * scale
        return (decimal ? scaled.toFixed(1) : Math.round(scaled)) + suffix
    }

    function memoryText() {
        var used = healthNumber("memUsedGb")
        var total = healthNumber("memTotalGb")
        if (!isFinite(used) || !isFinite(total)) return "—"
        return used.toFixed(1) + "/" + total.toFixed(1) + " GB"
    }

    function resumeEmptyText() {
        if (!sessionsLoaded()) {
            return sessionsLoadError().length > 0 ? "Could not load sessions. Open Sessions to retry." : "Loading sessions…"
        }
        return "No sessions yet — start one above."
    }

    function formatEventTime(start, allDay) {
        if (allDay) return "all day"
        var d = new Date(start)
        var h = d.getHours().toString().padStart(2, '0')
        var m = d.getMinutes().toString().padStart(2, '0')
        return h + ":" + m
    }

    function extractFromName(from) {
        // "Name <email>" → Name; bare email → local part
        var m = from.match(/^\s*"?([^"<]+?)"?\s*</)
        if (m) return m[1].trim()
        var at = from.indexOf("@")
        return at > 0 ? from.slice(0, at) : from
    }

    function liveSessions() {
        if (!sessionsModel) return []
        var live = []
        for (var i = 0; i < sessionsModel.rowCount(); i++) {
            var idx = sessionsModel.index(i, 0)
            var status = sessionsModel.data(idx, 261)  // StatusRole
            if (status === "working" || status === "approval") {
                live.push({
                    id: sessionsModel.data(idx, 257),  // IdRole
                    title: sessionsModel.data(idx, 258),
                    dir: sessionsModel.data(idx, 259),
                    model: sessionsModel.data(idx, 260),
                    status: status
                })
            }
        }
        return live
    }

    // QML function-call bindings are NOT reactive to model resets — the
    // Resume zone stayed "No sessions yet" forever with 71 sessions loaded.
    // recentList is re-computed on the model's reset/insert signals instead.
    property var recentList: []

    Connections {
        target: root.sessionsModel ? root.sessionsModel : null
        ignoreUnknownSignals: true
        function onModelReset() { root.recentList = recentSessions() }
        function onRowsInserted() { root.recentList = recentSessions() }
        function onRowsRemoved() { root.recentList = recentSessions() }
        function onDataChanged() { root.recentList = recentSessions() }
    }
    Component.onCompleted: {
        recentList = recentSessions()
        syncFeedCount()
    }

    function syncFeedCount() {
        root.feedCount = root.feedModel ? root.feedModel.rowCount() : 0
    }

    function recentSessions() {
        if (!sessionsModel) return []
        var recent = []
        var count = Math.min(6, sessionsModel.rowCount())
        for (var i = 0; i < count; i++) {
            var idx = sessionsModel.index(i, 0)
            recent.push({
                id: sessionsModel.data(idx, 257),
                title: sessionsModel.data(idx, 258),
                dir: sessionsModel.data(idx, 259),
                turns: sessionsModel.data(idx, 262),
                updated: sessionsModel.data(idx, 263),
                status: sessionsModel.data(idx, 261)
            })
        }
        return recent
    }
}
