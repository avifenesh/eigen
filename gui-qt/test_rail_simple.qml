import QtQuick
import QtQuick.Controls
import QtQuick.Window
import "eigenqt/qml/Theme.js" as Theme

Window {
    id: root
    visible: true
    width: 1280
    height: 800
    title: "Rail Test"
    color: Theme.colors.bgBase

    property string currentRoute: "home"

    Row {
        anchors.fill: parent

        // Rail component
        Loader {
            id: railLoader
            width: 200
            height: parent.height
            source: "eigenqt/qml/Rail.qml"

            onLoaded: {
                item.currentRoute = Qt.binding(function() { return root.currentRoute })
                item.sessionsModel = mockSessionsModel
                item.liveSessionsModel = null
                item.tasksModel = mockTasksModel
                item.statsData = mockStats
                item.daemonOnline = true
                item.guiserverSha = "abc12345"

                item.routeChanged.connect(function(route) {
                    console.log("Route changed to:", route)
                    root.currentRoute = route
                })
            }
        }

        // Mock content area showing current route
        Rectangle {
            width: parent.width - 200
            height: parent.height
            color: Theme.colors.bgBase

            Label {
                anchors.centerIn: parent
                text: "Current route: " + root.currentRoute
                font.pixelSize: 32
                color: Theme.colors.textPrimary
            }
        }
    }

    // Mock models
    ListModel {
        id: mockSessionsModel

        Component.onCompleted: {
            // Add some mock sessions
            append({sessionId: "s1", title: "Test session 1", dir: "/home/user/project1", modelName: "sonnet", status: "working", turns: 5, updated: Date.now() * 1000000})
            append({sessionId: "s2", title: "Test session 2", dir: "/home/user/project2", modelName: "opus", status: "approval", turns: 3, updated: Date.now() * 1000000})
            append({sessionId: "s3", title: "Test session 3", dir: "/home/user/project3", modelName: "haiku", status: "idle", turns: 10, updated: Date.now() * 1000000})
        }
    }

    QtObject {
        id: mockTasksModel
        property int running_count: 2
    }

    property var mockStats: ({
        running_turns: 2,
        sessions: 3,
        bg_tasks: 2
    })

    // Test sequence: cycle through routes
    Timer {
        id: testTimer
        interval: 2000
        repeat: true
        running: true
        property int routeIndex: 0
        property var routes: ["home", "sessions", "live", "chat", "tasks"]

        onTriggered: {
            routeIndex = (routeIndex + 1) % routes.length
            root.currentRoute = routes[routeIndex]
            console.log("Switched to route:", root.currentRoute)
        }
    }
}
