import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Primary navigation rail — left edge, eigen λ mark + wordmark, nav items with badges + running session sub-list.
Rectangle {
    id: root
    color: Theme.colors.bgWell
    border.width: 1
    border.color: Theme.colors.borderHairline

    // Current route (controlled by parent)
    property string currentRoute: "home"
    signal routeChanged(string route)

    // Models for badge counts
    property var sessionsModel: null
    property var liveSessionsModel: null
    property var tasksModel: null
    property var feedModel: null
    property var statsData: null

    // Daemon status for footer
    property bool daemonOnline: false
    property string guiserverSha: ""
    property var sessionController: null
    property int sessionsEpoch: 0
    property int feedEpoch: 0

    onSessionsModelChanged: sessionsEpoch += 1
    onFeedModelChanged: feedEpoch += 1

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // BRAND HEADER — λ mark + wordmark
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 48
            color: Theme.colors.bgWell

            // Bottom border separator
            Rectangle {
                anchors.bottom: parent.bottom
                anchors.left: parent.left
                anchors.right: parent.right
                height: 1
                color: Theme.colors.borderHairline
            }

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: 18  // align λ with glyph column below (scroll --sp-3 + item --sp-5)
                anchors.rightMargin: Theme.space.md
                spacing: Theme.space.sm

                // λ mark — spectrum-filled teal (signature eigenvalue mark)
                Label {
                    text: "λ"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: 20
                    font.weight: Theme.fontWeight.bold
                    color: Theme.colors.brandBright
                    // Spectrum gradient (clipped to text) — simplified for QML (no webkit-background-clip)
                    // Just use brandBright directly (Qt doesn't support gradient text easily without shaders)
                }

                Label {
                    text: "eigen"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h2
                    font.weight: Theme.fontWeight.bold
                    color: Theme.colors.textPrimary
                    // letterSpacing: -0.5
                }
            }
        }

        // Scrollable nav area
        Flickable {
            Layout.fillWidth: true
            Layout.fillHeight: true
            contentWidth: width
            contentHeight: navColumn.implicitHeight
            clip: true

            ColumnLayout {
                id: navColumn
                width: parent.width
                spacing: 0

                // ZONE: Work
                ColumnLayout {
                    Layout.fillWidth: true
                    Layout.topMargin: Theme.space.lg
                    spacing: 0

                    // Zone label
                    Label {
                        text: "Work"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        font.weight: Theme.fontWeight.semibold
                        font.capitalization: Font.AllUppercase
                        // letterSpacing: 0.8
                        color: Theme.colors.textFaint
                        Layout.leftMargin: Theme.space.lg
                        Layout.bottomMargin: Theme.space.xs
                    }

                    // Nav items
                    NavItem {
                        id: homeNavItem
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "home"
                        label: "Home"
                        glyph: "◆"
                        badge: actionFeedCount()
                        isActive: root.currentRoute === "home"
                        onClicked: root.routeChanged("home")
                    }

                    NavItem {
                        id: chatNavItem
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "chat"
                        label: "Chat"
                        glyph: "▶"
                        badge: runningSessionsCount()
                        badgeLive: runningSessionsCount() > 0
                        isActive: root.currentRoute === "chat"
                        onClicked: root.routeChanged("chat")

                        // Running session sub-list (expanded under Chat when there are live sessions)
                        property var runningSessions: {
                            root.sessionsEpoch
                            return root.getRunningSessionsList()
                        }
                        readonly property int qaRunningSessionCount: runningSessions ? runningSessions.length : 0
                        readonly property int qaRunningDelegateCount: runningSessionsRepeater.count

                        ColumnLayout {
                            visible: chatNavItem.runningSessions ? chatNavItem.runningSessions.length > 0 : false
                            width: parent.width
                            Layout.fillWidth: true
                            Layout.leftMargin: Theme.space.xxxl
                            spacing: 1

                            Repeater {
                                id: runningSessionsRepeater
                                model: chatNavItem.qaRunningSessionCount
                                delegate: Rectangle {
                                    readonly property var session: chatNavItem.runningSessions[index] || ({})
                                    readonly property bool qaUnread: !!session.unread

                                    objectName: "navRunningSession_" + root.safeObjectName(root.sessionValue(session, "id"))
                                    Layout.fillWidth: true
                                    implicitHeight: 26
                                    color: subMouseArea.containsMouse ? Theme.colors.stateHover : "transparent"
                                    radius: Theme.radius.sm

                                    MouseArea {
                                        id: subMouseArea
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: {
                                            // Open the session FIRST (sets active session), then switch route
                                            if (root.sessionController) {
                                                root.sessionController.open_session(root.sessionValue(session, "id"))
                                            }
                                            root.routeChanged("chat")
                                        }
                                    }

                                    RowLayout {
                                        anchors.fill: parent
                                        anchors.leftMargin: Theme.space.lg
                                        anchors.rightMargin: Theme.space.sm
                                        spacing: Theme.space.sm

                                        // Status dot (6px like Svelte StatusDot)
                                        Rectangle {
                                            width: 6
                                            height: 6
                                            radius: 3
                                            color: root.sessionValue(session, "status") === "approval" ? Theme.colors.dotWarn : Theme.colors.dotLive

                                            // Pulse animation for live sessions
                                            SequentialAnimation on opacity {
                                                running: true
                                                loops: Animation.Infinite
                                                NumberAnimation { from: 1.0; to: 0.3; duration: Theme.duration.breath / 2 }
                                                NumberAnimation { from: 0.3; to: 1.0; duration: Theme.duration.breath / 2 }
                                            }
                                        }

                                        Label {
                                            text: shortTitle(session)
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.label
                                            color: Theme.colors.textSecondary
                                            elide: Text.ElideRight
                                            Layout.fillWidth: true
                                        }

                                        Rectangle {
                                            objectName: "navRunningUnread_" + root.safeObjectName(root.sessionValue(session, "id"))
                                            visible: qaUnread
                                            width: 7
                                            height: 7
                                            radius: 4
                                            color: Theme.colors.brandBright
                                        }
                                    }
                                }
                            }
                        }
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "sessions"
                        label: "Sessions"
                        glyph: "≡"
                        badge: {
                            root.sessionsEpoch
                            return root.sessionsModel ? root.sessionsModel.rowCount() : 0
                        }
                        badgeLive: false
                        isActive: root.currentRoute === "sessions"
                        onClicked: root.routeChanged("sessions")
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "live"
                        label: "Live"
                        glyph: "◐"
                        badge: workingAndApprovalCount()
                        badgeLive: workingAndApprovalCount() > 0
                        isActive: root.currentRoute === "live"
                        onClicked: root.routeChanged("live")
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "board"
                        label: "Board"
                        glyph: "▤"
                        badge: 0
                        badgeLive: false
                        isActive: root.currentRoute === "board"
                        onClicked: root.routeChanged("board")
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "tasks"
                        label: "Tasks"
                        glyph: "⋔"
                        badge: root.tasksModel ? root.tasksModel.running_count : 0
                        badgeLive: root.tasksModel && root.tasksModel.running_count > 0
                        isActive: root.currentRoute === "tasks"
                        onClicked: root.routeChanged("tasks")
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "skills"
                        label: "Skills"
                        glyph: "✦"
                        badge: 0
                        badgeLive: false
                        isActive: root.currentRoute === "skills"
                        onClicked: root.routeChanged("skills")
                    }
                }

                // ZONE: Knowledge
                ColumnLayout {
                    Layout.fillWidth: true
                    Layout.topMargin: Theme.space.lg
                    spacing: 0

                    // Zone label
                    Label {
                        text: "Knowledge"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        font.weight: Theme.fontWeight.semibold
                        font.capitalization: Font.AllUppercase
                        color: Theme.colors.textFaint
                        Layout.leftMargin: Theme.space.lg
                        Layout.bottomMargin: Theme.space.xs
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "memory"
                        label: "Memory"
                        glyph: "❖"
                        badge: 0
                        badgeLive: false
                        isActive: root.currentRoute === "memory"
                        onClicked: root.routeChanged("memory")
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "notes"
                        label: "Notes"
                        glyph: "≣"
                        badge: 0
                        badgeLive: false
                        isActive: root.currentRoute === "notes"
                        onClicked: root.routeChanged("notes")
                    }
                }

                // ZONE: System
                ColumnLayout {
                    Layout.fillWidth: true
                    Layout.topMargin: Theme.space.lg
                    spacing: 0

                    // Zone label
                    Label {
                        text: "System"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        font.weight: Theme.fontWeight.semibold
                        font.capitalization: Font.AllUppercase
                        color: Theme.colors.textFaint
                        Layout.leftMargin: Theme.space.lg
                        Layout.bottomMargin: Theme.space.xs
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "observe"
                        label: "Observe"
                        glyph: "◉"
                        badge: 0
                        badgeLive: false
                        isActive: root.currentRoute === "observe"
                        onClicked: root.routeChanged("observe")
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "routing"
                        label: "Routing"
                        glyph: "⇄"
                        badge: 0
                        badgeLive: false
                        isActive: root.currentRoute === "routing"
                        onClicked: root.routeChanged("routing")
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "connectors"
                        label: "Connectors"
                        glyph: "⟐"
                        badge: 0
                        badgeLive: false
                        isActive: root.currentRoute === "connectors"
                        onClicked: root.routeChanged("connectors")
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "config"
                        label: "Config"
                        glyph: "⚙"
                        badge: 0
                        badgeLive: false
                        isActive: root.currentRoute === "config"
                        onClicked: root.routeChanged("config")
                    }

                    NavItem {
                        Layout.fillWidth: true
                        Layout.leftMargin: Theme.space.sm
                        Layout.rightMargin: Theme.space.sm
                        route: "reviewers"
                        label: "Reviewers"
                        glyph: "⌕"
                        badge: 0
                        badgeLive: false
                        isActive: root.currentRoute === "reviewers"
                        onClicked: root.routeChanged("reviewers")
                    }
                }
            }
        }

        // FOOTER — daemon status dot + state + version
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 36
            color: Theme.colors.bgWell

            // Top border separator
            Rectangle {
                anchors.top: parent.top
                anchors.left: parent.left
                anchors.right: parent.right
                height: 1
                color: Theme.colors.borderHairline
            }

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.lg
                anchors.rightMargin: Theme.space.lg
                spacing: Theme.space.sm

                // Status dot
                Rectangle {
                    width: 6
                    height: 6
                    radius: 3
                    color: root.daemonOnline ? Theme.colors.dotLive : Theme.colors.dotError

                    // Breathing animation when online
                    SequentialAnimation on opacity {
                        running: root.daemonOnline
                        loops: Animation.Infinite
                        NumberAnimation { from: 1.0; to: 0.62; duration: Theme.duration.breath / 2 }
                        NumberAnimation { from: 0.62; to: 1.0; duration: Theme.duration.breath / 2 }
                    }
                }

                Label {
                    text: root.daemonOnline ? "online" : "offline"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    font.capitalization: Font.AllUppercase
                    // letterSpacing: 0.8
                    color: root.daemonOnline ? Theme.colors.textMuted : Theme.colors.error
                }

                Item { Layout.fillWidth: true }

                Label {
                    visible: root.guiserverSha !== ""
                    text: root.guiserverSha.substring(0, 8)
                    font.family: Theme.monoFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    color: Theme.colors.textFaint
                }
            }
        }
    }

    Connections {
        target: root.sessionsModel ? root.sessionsModel : null
        ignoreUnknownSignals: true

        function onModelReset() { root.sessionsEpoch += 1 }
        function onRowsInserted() { root.sessionsEpoch += 1 }
        function onRowsRemoved() { root.sessionsEpoch += 1 }
        function onDataChanged() { root.sessionsEpoch += 1 }
    }

    Connections {
        target: root.feedModel ? root.feedModel : null
        ignoreUnknownSignals: true

        function onModelReset() { root.feedEpoch += 1 }
        function onRowsInserted() { root.feedEpoch += 1 }
        function onRowsRemoved() { root.feedEpoch += 1 }
        function onDataChanged() { root.feedEpoch += 1 }
    }

    // Helper functions
    function actionFeedCount() {
        root.feedEpoch
        if (!root.feedModel) return 0
        return root.feedModel.rowCount()
    }

    function runningSessionsCount() {
        if (!root.statsData) return 0
        return root.statsData.running_turns || 0
    }

    function workingAndApprovalCount() {
        root.sessionsEpoch
        if (!root.sessionsModel) return 0
        var count = 0
        for (var i = 0; i < root.sessionsModel.rowCount(); i++) {
            var idx = root.sessionsModel.index(i, 0)
            var status = root.sessionsModel.data(idx, 261)  // StatusRole
            if (status === "working" || status === "approval") {
                count++
            }
        }
        return count
    }

    function getRunningSessionsList() {
        root.sessionsEpoch
        if (!root.sessionsModel) return []
        var running = []
        for (var i = 0; i < root.sessionsModel.rowCount(); i++) {
            var idx = root.sessionsModel.index(i, 0)
            var status = root.sessionsModel.data(idx, 261)  // StatusRole
            if (status === "working" || status === "approval") {
                running.push({
                    id: root.sessionsModel.data(idx, 257),  // IdRole
                    title: root.sessionsModel.data(idx, 258),
                    dir: root.sessionsModel.data(idx, 259),
                    status: status,
                    unread: root.sessionsModel.data(idx, 264) === true  // UnreadRole
                })
            }
        }
        return running
    }

    function shortTitle(session) {
        var t = root.sessionValue(session, "title").trim()
        if (t) return t
        var d = root.sessionValue(session, "dir").replace(/\/+$/, "")
        return d.slice(d.lastIndexOf("/") + 1) || "session"
    }

    function sessionValue(session, key) {
        if (!session) return ""
        var value = session[key]
        if (value === undefined || value === null) return ""
        return String(value)
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]/g, "_")
    }
}
