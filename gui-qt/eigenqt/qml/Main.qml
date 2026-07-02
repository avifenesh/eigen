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

        // Left rail (sessions zone)
        Rectangle {
            Layout.preferredWidth: 200
            Layout.fillHeight: true
            color: Theme.colors.bgWell

            ColumnLayout {
                anchors.fill: parent
                anchors.margins: Theme.space.md
                spacing: Theme.space.lg

                // Header
                Label {
                    text: "Sessions"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h3
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textPrimary
                    Layout.fillWidth: true
                }

                // Sessions list
                ListView {
                    id: sessionsListView
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    spacing: Theme.space.xs
                    model: sessionsModel
                    currentIndex: -1

                    delegate: SessionRow {
                        width: ListView.view.width
                        sessionId: sessionId
                        title: title
                        status: status
                        dir: dir
                        modelBadge: modelName
                        updated: updated
                        isActive: ListView.isCurrentItem

                        onClicked: {
                            sessionsListView.currentIndex = index
                            stackLayout.currentIndex = 1  // show chat
                            sessionController.open_session(sessionId)
                        }
                    }
                }
            }
        }

        // Center: sessions list OR chat view
        StackLayout {
            id: stackLayout
            Layout.fillWidth: true
            Layout.fillHeight: true
            currentIndex: 0

            // Index 0: Sessions view (full list)
            SessionsView {
                onSessionClicked: function(sessionId) {
                    stackLayout.currentIndex = 1
                    sessionController.open_session(sessionId)
                    // Set active in left rail
                    for (var i = 0; i < sessionsModel.rowCount(); i++) {
                        var itemId = sessionsModel.data(sessionsModel.index(i, 0), 257)  // IdRole
                        if (itemId === sessionId) {
                            sessionsListView.currentIndex = i
                            break
                        }
                    }
                }
            }

            // Index 1: Chat view
            ChatView {
                sessionId: sessionController.session_id
                sessionStateModel: sessionController.session_state_model
                commandsModel: sessionController.commands_model

                onBackClicked: {
                    stackLayout.currentIndex = 0
                    sessionsListView.currentIndex = -1
                    sessionController.detach()
                }
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
                text: stackLayout.currentIndex === 1 ? "session: " + sessionController.session_id : ""
                font.family: Theme.monoFonts[0]
                font.pixelSize: Theme.fontSize.micro
                color: Theme.colors.textMuted
            }
        }
    }

    // Properties bound from Python
    property bool daemonOnline: false
    property string guiserverSha: ""
}
