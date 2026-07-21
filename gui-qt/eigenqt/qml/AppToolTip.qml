import QtQuick
import QtQuick.Controls
import "Theme.js" as Theme

ToolTip {
    id: control

    property real maximumWidth: 360
    property bool requestedVisible: false
    property bool persistent: false
    property int timeoutDuration: 4000
    readonly property bool qaThemed: true
    readonly property color qaBackgroundColor: Theme.colors.surfaceOverlay
    readonly property color qaTextColor: Theme.colors.textPrimary
    readonly property color qaBorderColor: Theme.colors.borderStrong
    readonly property real qaHorizontalPadding: Math.min(leftPadding, rightPadding)
    readonly property real qaVerticalPadding: Math.min(topPadding, bottomPadding)

    delay: 600
    timeout: persistent ? -1 : timeoutDuration
    leftPadding: Theme.space.lg
    rightPadding: Theme.space.lg
    topPadding: Theme.space.sm
    bottomPadding: Theme.space.sm
    implicitWidth: Math.min(
        maximumWidth,
        tipLabel.implicitWidth + leftPadding + rightPadding
    )
    onRequestedVisibleChanged: syncVisibility()
    Component.onCompleted: syncVisibility()

    function syncVisibility() {
        if (requestedVisible) {
            open()
        } else {
            close()
        }
    }

    contentItem: Label {
        id: tipLabel
        text: control.text
        font.family: Theme.uiFonts[0]
        font.pixelSize: Theme.fontSize.micro
        font.weight: Theme.fontWeight.medium
        color: Theme.colors.textPrimary
        wrapMode: Text.Wrap
        verticalAlignment: Text.AlignVCenter
    }

    background: Rectangle {
        color: Theme.colors.surfaceOverlay
        radius: Theme.radius.sm
        border.width: 1
        border.color: Theme.colors.borderStrong
    }
}
