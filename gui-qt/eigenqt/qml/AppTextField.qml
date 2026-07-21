import QtQuick
import QtQuick.Controls
import "Theme.js" as Theme

TextField {
    id: control

    property color backgroundColor: Theme.colors.bgInset
    property color borderColor: Theme.colors.borderSubtle
    property color focusBorderColor: Theme.colors.borderFocus
    property real normalBorderWidth: 1
    property real focusedBorderWidth: 1
    property real backgroundRadius: Theme.radius.sm
    property bool qaForceKeyboardFocus: false

    readonly property bool qaIsAppTextField: true
    readonly property string qaText: displayText !== "" ? displayText : placeholderText
    readonly property real qaHorizontalPadding: Math.min(leftPadding, rightPadding)
    readonly property real qaVerticalPadding: Math.min(topPadding, bottomPadding)
    readonly property real qaTextAvailableWidth: Math.max(0, width - leftPadding - rightPadding)
    readonly property bool qaTextFits: qaText === "" || qaTextMetrics.advanceWidth <= qaTextAvailableWidth + 1.0
    readonly property bool qaVisualFocus: activeFocus

    implicitHeight: 32
    selectByMouse: true
    verticalAlignment: TextInput.AlignVCenter
    font.family: Theme.uiFonts[0]
    font.pixelSize: Theme.fontSize.bodySm
    color: enabled ? Theme.colors.textPrimary : Theme.colors.textFaint
    placeholderTextColor: Theme.colors.textGhost
    selectionColor: Theme.colors.brandBg
    selectedTextColor: Theme.colors.textPrimary
    leftPadding: Theme.space.lg
    rightPadding: Theme.space.lg
    topPadding: Theme.space.sm
    bottomPadding: Theme.space.sm

    onQaForceKeyboardFocusChanged: {
        if (qaForceKeyboardFocus) {
            forceActiveFocus(Qt.TabFocusReason)
        }
    }

    TextMetrics {
        id: qaTextMetrics
        font: control.font
        text: control.qaText
    }

    background: Rectangle {
        implicitHeight: 32
        color: control.backgroundColor
        border.width: control.activeFocus ? control.focusedBorderWidth : control.normalBorderWidth
        border.color: control.activeFocus ? control.focusBorderColor : control.borderColor
        radius: control.backgroundRadius
        opacity: control.enabled ? 1.0 : 0.6

        Behavior on border.color { ColorAnimation { duration: Theme.duration.fast } }
    }
}
