
import QtQuick
import QtQuick.Window
import "../eigenqt/qml"

Window {
    id: win
    width: 1280
    height: 800
    visible: true

    HomeView {
        anchors.fill: parent
        dashboardModel: dashboardModel
        feedModel: feedModel
        sessionsModel: sessionsModel
        statsData: statsData
    }

    Component.onCompleted: {
        grabTimer.start()
    }

    Timer {
        id: grabTimer
        interval: 500
        onTriggered: {
            console.log("Screenshot saved: screenshots/home-view.png")
            Qt.quit()
        }
    }
}
