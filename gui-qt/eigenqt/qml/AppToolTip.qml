import QtQuick
import QtQuick.Controls
import "Theme.js" as Theme

ToolTip {
    id: control

    property real maximumWidth: 360
    readonly property bool qaThemed: true
    readonly property color qaBackgroundColor: Theme.colors.surfaceOverlay
    readonly property color qaTextColor: Theme.colors.textPrimary
    readonly property color qaBorderColor: Theme.colors.borderStrong
    readonly property real qaHorizontalPadding: Math.min(leftPadding, rightPadding)
    readonly property real qaVerticalPadding: Math.min(topPadding, bottomPadding)

    delay: 600
    timeout: 4000
    leftPadding: Theme.space.lg
    rightPadding: Theme.space.lg
    topPadding: Theme.space.sm
    bottomPadding: Theme.space.sm
    implicitWidth: Math.min(
        maximumWidth,
        tipLabel.implicitWidth + leftPadding + rightPadding
    )

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
