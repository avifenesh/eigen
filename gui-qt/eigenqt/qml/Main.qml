import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Window
import "Theme.js" as Theme

ApplicationWindow {
    id: root
    objectName: "mainWindow"
    visible: true
    readonly property int initialDesktopMarginX: 64
    readonly property int initialDesktopMarginY: 96
    readonly property int initialAvailableWidth: Screen.desktopAvailableWidth > 0 ? Screen.desktopAvailableWidth : 1280
    readonly property int initialAvailableHeight: Screen.desktopAvailableHeight > 0 ? Screen.desktopAvailableHeight : 800
    readonly property int initialWindowWidth: Math.min(1280, Math.max(minimumWidth, initialAvailableWidth - initialDesktopMarginX))
    readonly property int initialWindowHeight: Math.min(800, Math.max(minimumHeight, initialAvailableHeight - initialDesktopMarginY))
    minimumWidth: Math.min(900, Math.max(720, initialAvailableWidth - initialDesktopMarginX))
    minimumHeight: Math.min(420, Math.max(320, initialAvailableHeight - initialDesktopMarginY))
    width: 1280
    height: 800
    title: "eigen"
    property bool railVisible: true
    Component.onCompleted: {
        width = initialWindowWidth
        height = initialWindowHeight
    }

    color: Theme.colors.bgBase

    // Fonts: rely on system font stack (Inter if installed, else system sans-serif)
    // No qrc:/ resource compilation exists; Theme.js provides fallback stack

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

    RowLayout {
        Layout.fillWidth: true
        Layout.fillHeight: true
        spacing: 0

        // Left rail (navigation)
        Rail {
            id: rail
            objectName: "mainRail"
            visible: root.railVisible
            Layout.preferredWidth: root.railVisible ? 200 : 0
            Layout.minimumWidth: root.railVisible ? 200 : 0
            Layout.maximumWidth: root.railVisible ? 200 : 0
            Layout.fillHeight: true

            currentRoute: root.currentRoute
            sessionsModel: root.ctxSessions
            liveSessionsModel: root.ctxLive
            tasksModel: root.ctxTasks
            feedModel: root.ctxFeed
            statsData: root.ctxStats
            daemonOnline: root.ctxDaemonOnline
            guiserverSha: root.ctxSha
            sessionController: root.ctxSessionController

            onRouteChanged: function(route) {
                root.currentRoute = route
            }
        }

        // Center: view routing via StackLayout
        StackLayout {
            id: stackLayout
            objectName: "mainStackLayout"
            Layout.fillWidth: true
            Layout.fillHeight: true
            currentIndex: routeToIndex(root.currentRoute)

            function routeToIndex(route) {
                if (route === "home") return 0
                if (route === "sessions") return 1
                if (route === "live") return 2
                if (route === "chat") return 3
                if (route === "board") return 4
                if (route === "tasks") return 5
                if (route === "skills") return 6
                if (route === "memory") return 7
                if (route === "notes") return 8
                if (route === "observe") return 9
                if (route === "routing") return 10
                if (route === "machines") return 11
                if (route === "connectors") return 12
                if (route === "config") return 13
                if (route === "reviewers") return 14
                return 0  // default to home
            }

            // Index 0: Home dashboard (default)
            HomeView {
                dashboardModel: root.ctxDashboard
                feedModel: root.ctxFeed
                sessionsModel: root.ctxSessions
                statsData: root.ctxStats
                rpcClient: root.ctxRpc

                onSessionClicked: function(sessionId) {
                    root.openCreatedSession(sessionId)
                }

                onSessionStarted: function(sessionId) {
                    root.openCreatedSession(sessionId)
                }
            }

            // Index 1: Sessions view (full list)
            SessionsView {
                sessionsModel: root.ctxSessions
                onSessionClicked: function(sessionId) {
                    root.currentRoute = "chat"
                    root.ctxSessionController.open_session(sessionId)
                }
            }

            // Index 2: Live view (working/approval sessions)
            LiveView {
                sessionsModel: root.ctxSessions
                liveSessionsModel: root.ctxLive
                rpcClient: root.ctxRpc
                newSessionPending: root.pendingNewSessionToken !== 0
                onOpenSession: function(sessionId) {
                    root.currentRoute = "chat"
                    sessionController.open_session(sessionId)
                }

                onNewSessionRequested: {
                    root.requestNewSession("")
                }
            }

            // Index 3: Chat view
            ChatView {
                sessionId: root.ctxSessionController ? root.ctxSessionController.session_id : ""
                sessionStateModel: root.ctxSessionController ? root.ctxSessionController.session_state_model : null
                commandsModel: root.ctxSessionController ? root.ctxSessionController.commands_model : null
                transcriptModel: root.ctxTranscript
                approvalsModel: root.ctxApprovals
                rpcClient: root.ctxRpc
                clipboardHelper: root.ctxClipboard
                highlighter: root.ctxHighlighter

                onBackClicked: {
                    root.currentRoute = "sessions"
                    root.ctxSessionController.detach()
                }
                onRouteRequested: function(route) {
                    root.currentRoute = route
                }
                onRailToggleRequested: {
                    root.railVisible = !root.railVisible
                }
            }

            // Index 4: Board view
            BoardView {
                boardModel: root.ctxBoard
                kanbanModel: root.ctxKanban
                rpcClient: root.ctxRpc
                sessionsModel: root.ctxSessions
                onSessionStarted: function(sessionId) {
                    root.openCreatedSession(sessionId)
                }
            }

            // Index 5: Tasks view (background agents)
            TasksView {
                tasksModel: root.ctxTasks
                rpcClient: root.ctxRpc
            }

            // Index 6: Skills view (capability gallery)
            SkillsView {
                skillsModel: root.ctxSkills
                proposalsModel: root.ctxProposals
                rpcClient: root.ctxRpc
            }

            // Index 7: Memory view (durable notes browser)
            MemoryView {
                memoryModel: root.ctxMemory
            }

            // Index 8: Notes view (Obsidian vault browser)
            NotesView {
                notesController: root.ctxNotes
            }

            // Index 9: Observe view (telemetry summary)
            ObserveView {
                observeModel: root.ctxObserveModel
            }

            // Index 10: Routing view (model/provider catalog)
            RoutingView {
                routingModel: root.ctxRoutingModel
            }

            // Index 11: Machines view (remote host/session drill-in)
            MachinesView {
                machinesModel: root.ctxMachinesModel
                onOpenSession: function(sessionId) {
                    if (!sessionId) return
                    root.currentRoute = "chat"
                    root.ctxSessionController.open_session(sessionId)
                }
            }

            // Index 12: Connectors view (MCP connectors management)
            ConnectorsView {
                connectorsModel: root.ctxConnectors
            }

            // Index 13: Config view (editable config fields + rule chains)
            ConfigView {
                configModel: root.ctxConfigModel
                ruleChainsModel: root.ctxRuleChainsModel
            }

            // Index 14: Reviewers view (revuto cockpit)
            ReviewersView {
                reviewersModel: root.ctxReviewersModel
            }
        }
    }

    Rectangle {
        objectName: "mainActionError"
        Layout.fillWidth: true
        Layout.preferredHeight: visible ? Math.max(38, mainActionErrorRow.implicitHeight + Theme.space.md) : 0
        Layout.minimumHeight: visible ? 38 : 0
        visible: root.actionError !== ""
        color: Theme.colors.errorBg
        border.width: visible ? 1 : 0
        border.color: Theme.colors.error
        clip: true

        RowLayout {
            id: mainActionErrorRow
            anchors.fill: parent
            anchors.leftMargin: Theme.space.lg
            anchors.rightMargin: Theme.space.lg
            spacing: Theme.space.md

            Label {
                objectName: "mainActionErrorText"
                text: root.actionError
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.error
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }

            AppButton {
                objectName: "mainDismissActionError"
                text: "X"
                compact: true
                toolTipText: "Dismiss shell error"
                Layout.preferredWidth: 28
                Layout.preferredHeight: 28
                onClicked: root.actionError = ""
            }
        }
    }

    // Status strip (bottom)
    Rectangle {
        objectName: "mainStatusStrip"
        Layout.fillWidth: true
        Layout.preferredHeight: 28
        Layout.minimumHeight: 28
        color: Theme.colors.bgWell
        border.width: 1
        border.color: Theme.colors.borderHairline

        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: Theme.space.lg
            anchors.rightMargin: Theme.space.lg
            spacing: Theme.space.xl

            // Daemon status
            Row {
                objectName: "mainDaemonStatus"
                spacing: Theme.space.sm
                Rectangle {
                    width: 8
                    height: 8
                    radius: 4
                    color: daemonOnline ? Theme.colors.dotLive : Theme.colors.dotIdle
                    anchors.verticalCenter: parent.verticalCenter
                }
                Label {
                    text: daemonOnline ? "daemon online" : "daemon offline"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    color: Theme.colors.textSecondary
                    anchors.verticalCenter: parent.verticalCenter
                }
            }

            // Guiserver SHA
            Label {
                objectName: "mainGuiserverSha"
                text: "guiserver: " + (guiserverSha.substring(0, 8) || "unknown")
                font.family: Theme.monoFonts[0]
                font.pixelSize: Theme.fontSize.micro
                color: Theme.colors.textMuted
            }

            Item { Layout.fillWidth: true }

            // Current session
            Label {
                objectName: "mainSessionStatus"
                text: stackLayout.currentIndex === 3 && root.ctxSessionController ? "session: " + root.ctxSessionController.session_id : ""
                font.family: Theme.monoFonts[0]
                font.pixelSize: Theme.fontSize.micro
                color: Theme.colors.textMuted
            }
        }
    }
    }

    // Unshadowed aliases for the Python context properties: inside a child
    // instantiation `Foo { sessionsModel: root.ctxSessions }` the RHS resolves
    // to Foo's OWN property (self-shadow). Bind through these instead.
    readonly property var ctxSessions: sessionsModel
    readonly property var ctxLive: liveSessionsModel
    readonly property var ctxTasks: tasksModel
    readonly property var ctxDashboard: dashboardModel
    readonly property var ctxFeed: feedModel
    readonly property var ctxBoard: boardModel
    readonly property var ctxKanban: kanbanModel
    readonly property var ctxSkills: skillsModel
    readonly property var ctxProposals: proposalsModel
    readonly property var ctxMemory: memoryModel
    readonly property var ctxNotes: notesController
    readonly property var ctxConnectors: connectorsModel
    readonly property var ctxObserveModel: observeModel
    readonly property var ctxRoutingModel: routingModel
    readonly property var ctxMachinesModel: machinesModel
    readonly property var ctxConfigModel: configModel
    readonly property var ctxRuleChainsModel: ruleChainsModel
    readonly property var ctxReviewersModel: reviewersModel
    readonly property var ctxTranscript: transcriptModel
    readonly property var ctxApprovals: approvalsModel
    readonly property var ctxClipboard: clipboardHelper
    readonly property var ctxHighlighter: highlighter
    readonly property var ctxRpc: rpcClient
    readonly property var ctxSessionController: sessionController
    readonly property var ctxStats: statsData
    readonly property bool ctxDaemonOnline: daemonOnline
    readonly property string ctxSha: guiserverSha

    // NOTE: the Python-side models/values (sessionsModel, dashboardModel,
    // feedModel, tasksModel, liveSessionsModel, statsData, daemonOnline,
    // guiserverSha) are CONTEXT PROPERTIES — visible in every QML scope
    // without declaration. Declaring same-named `property var X: null` here
    // SHADOWED them all with null (fourth sighting of the QML shadowing
    // footgun — this one blanked the entire app's data). Do not re-add.

    // Current route (navigation state)
    property string currentRoute: "home"
    readonly property int activeRouteIndex: stackLayout.currentIndex
    property int pendingNewSessionToken: 0
    property string actionError: ""

    Connections {
        target: root.ctxRpc ? root.ctxRpc : null
        function onCallDone(token, payload) {
            if (token !== root.pendingNewSessionToken) return
            root.pendingNewSessionToken = 0
            if (payload && payload.error !== undefined && payload.error !== null) {
                var error = typeof payload.error === "string" ? payload.error : JSON.stringify(payload.error)
                root.actionError = "Could not start session: " + (error || "unknown error")
                return
            }
            root.openCreatedSession(payload ? String(payload.result || "") : "")
        }
    }

    function refreshSessionLists() {
        if (root.ctxSessions && root.ctxSessions.refresh) root.ctxSessions.refresh()
        if (root.ctxLive && root.ctxLive.refresh) root.ctxLive.refresh()
    }

    function requestNewSession(dir) {
        if (root.pendingNewSessionToken !== 0) return
        if (!root.ctxRpc || typeof root.ctxRpc.callToken !== "function") {
            root.actionError = "Could not start session: RPC client is unavailable."
            return
        }
        root.actionError = ""
        root.pendingNewSessionToken = root.ctxRpc.callToken("NewSession", [dir || "", "", ""])
    }

    function openCreatedSession(sessionId) {
        if (!sessionId) return
        root.currentRoute = "chat"
        root.ctxSessionController.open_session(sessionId)
        root.refreshSessionLists()
    }
}
