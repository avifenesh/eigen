import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

Rectangle {
    id: root

    property string message: ""
    property string textObjectName: objectName + "Text"
    property string retryObjectName: objectName + "Retry"
    property string retryToolTipText: "Retry"

    signal retry()

    Layout.fillWidth: true
    Layout.preferredHeight: visible ? Math.max(40, refreshErrorRow.implicitHeight + Theme.space.md) : 0
    color: Theme.colors.errorBg
    border.width: visible ? 1 : 0
    border.color: Theme.colors.error
    radius: Theme.radius.sm
    clip: true

    RowLayout {
        id: refreshErrorRow
        anchors.fill: parent
        anchors.leftMargin: Theme.space.md
        anchors.rightMargin: Theme.space.md
        spacing: Theme.space.md

        Label {
            objectName: root.textObjectName
            text: root.message
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.label
            color: Theme.colors.error
            wrapMode: Text.WrapAnywhere
            Layout.fillWidth: true
        }

        AppButton {
            objectName: root.retryObjectName
            text: "Retry"
            compact: true
            toolTipText: root.retryToolTipText
            Layout.preferredWidth: 72
            Layout.preferredHeight: 28
            onClicked: root.retry()
        }
    }
}
