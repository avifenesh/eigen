import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

ApplicationWindow {
    id: root
    visible: true
    width: 1280
    height: 800
    title: "eigen"

    color: Theme.colors.bgBase

    // Fonts: rely on system font stack (Inter if installed, else system sans-serif)
    // No qrc:/ resource compilation exists; Theme.js provides fallback stack

    RowLayout {
        anchors.fill: parent
        spacing: 0

        // Left rail (navigation)
        Rail {
            id: rail
            Layout.preferredWidth: 200
            Layout.fillHeight: true

            currentRoute: root.currentRoute
            sessionsModel: root.ctxSessions
            liveSessionsModel: root.ctxLive
            tasksModel: root.ctxTasks
            statsData: root.ctxStats
            daemonOnline: root.ctxDaemonOnline
            guiserverSha: root.ctxSha

            onRouteChanged: function(route) {
                root.currentRoute = route
            }
        }

        // Center: view routing via StackLayout
        StackLayout {
            id: stackLayout
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
                if (route === "connectors") return 9
                if (route === "config") return 10
                if (route === "reviewers") return 11
                return 0  // default to home
            }

            // Index 0: Home dashboard (default)
            HomeView {
                dashboardModel: root.ctxDashboard
                feedModel: root.ctxFeed
                sessionsModel: root.ctxSessions
                statsData: root.ctxStats

                onSessionClicked: function(sessionId) {
                    root.currentRoute = "chat"
                    sessionController.open_session(sessionId)
                }

                onNewSessionClicked: {
                    // TODO: wire NewSession RPC
                    console.log("New session clicked")
                }
            }

            // Index 1: Sessions view (full list)
            SessionsView {
                onSessionClicked: function(sessionId) {
                    root.currentRoute = "chat"
                    sessionController.open_session(sessionId)
                }
            }

            // Index 2: Live view (working/approval sessions)
            LiveView {
                onOpenSession: function(sessionId) {
                    root.currentRoute = "chat"
                    sessionController.open_session(sessionId)
                }

                onNewSessionRequested: {
                    // TODO: New session dialog (for now, just a placeholder)
                    console.log("New session requested")
                }
            }

            // Index 3: Chat view
            ChatView {
                sessionId: sessionController.session_id
                sessionStateModel: sessionController.session_state_model
                commandsModel: sessionController.commands_model

                onBackClicked: {
                    root.currentRoute = "sessions"
                    sessionController.detach()
                }
            }

            // Index 4: Board view
            BoardView {
                // boardModel and kanbanModel are context properties
            }

            // Index 5: Tasks view (background agents)
            TasksView {
                tasksModel: root.ctxTasks
            }

            // Index 6: Skills view (capability gallery)
            SkillsView {
                skillsModel: root.ctxSkills
                proposalsModel: root.ctxProposals
            }

            // Index 7: Memory view (durable notes browser)
            MemoryView {
                // memoryModel is a context property
            }

            // Index 8: Notes view (Obsidian vault browser)
            NotesView {
                notesController: root.ctxNotes
            }

            // Index 9: Connectors view (MCP connectors management)
            ConnectorsView {
                // connectorsModel is a context property
            }

            // Index 10: Config view (editable config fields + rule chains)
            ConfigView {
                configModel: root.ctxConfigModel
                ruleChainsModel: root.ctxRuleChainsModel
            }

            // Index 11: Reviewers view (revuto cockpit)
            ReviewersView {
                reviewersModel: root.ctxReviewersModel
            }
        }
    }

    // Status strip (bottom)
    Rectangle {
        anchors.bottom: parent.bottom
        anchors.left: parent.left
        anchors.right: parent.right
        height: 28
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
                text: "guiserver: " + (guiserverSha.substring(0, 8) || "unknown")
                font.family: Theme.monoFonts[0]
                font.pixelSize: Theme.fontSize.micro
                color: Theme.colors.textMuted
            }

            Item { Layout.fillWidth: true }

            // Current session
            Label {
                text: stackLayout.currentIndex === 3 ? "session: " + sessionController.session_id : ""
                font.family: Theme.monoFonts[0]
                font.pixelSize: Theme.fontSize.micro
                color: Theme.colors.textMuted
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
    readonly property var ctxSkills: skillsModel
    readonly property var ctxProposals: proposalsModel
    readonly property var ctxNotes: notesController
    readonly property var ctxConfigModel: configModel
    readonly property var ctxRuleChainsModel: ruleChainsModel
    readonly property var ctxReviewersModel: reviewersModel
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
}
