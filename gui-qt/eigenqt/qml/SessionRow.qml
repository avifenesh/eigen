import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Session row in left rail (compact)
Rectangle {
    id: root
    height: 56
    radius: Theme.radius.sm
    color: isActive ? Theme.colors.stateSelected : mouseArea.containsMouse ? Theme.colors.stateHover : "transparent"

    property string sessionId
    property string title
    property string status
    property string dir
    property string modelBadge
    property string updated
    property bool isActive: false

    signal clicked()

    Behavior on color { ColorAnimation { duration: Theme.duration.fast } }

    MouseArea {
        id: mouseArea
        anchors.fill: parent
        hoverEnabled: true
        onClicked: root.clicked()
    }

    RowLayout {
        anchors.fill: parent
        anchors.margins: Theme.space.md
        spacing: Theme.space.md

        // Status dot
        Rectangle {
            width: 8
            height: 8
            radius: 4
            color: Theme.statusColor(root.status)
            Layout.alignment: Qt.AlignVCenter

            // Breathing animation for working status
            SequentialAnimation on opacity {
                running: root.status === "working"
                loops: Animation.Infinite
                NumberAnimation { from: 1.0; to: 0.3; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
                NumberAnimation { from: 0.3; to: 1.0; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
            }
        }

        ColumnLayout {
            Layout.fillWidth: true
            spacing: 2

            Label {
                text: root.title
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.body
                font.weight: Theme.fontWeight.medium
                color: Theme.colors.textPrimary
                elide: Text.ElideRight
                Layout.fillWidth: true
            }

            RowLayout {
                spacing: Theme.space.sm
                Layout.fillWidth: true

                Label {
                    text: root.dir ? root.dir.split("/").pop() : ""
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    color: Theme.colors.textMuted
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }

                Label {
                    text: root.modelBadge
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    font.weight: Theme.fontWeight.medium
                    color: Theme.colors.textSecondary
                }
            }
        }
    }
}
