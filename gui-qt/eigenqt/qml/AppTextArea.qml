import QtQuick
import QtQuick.Controls
import "Theme.js" as Theme

TextArea {
    id: control

    property color backgroundColor: Theme.colors.bgInset
    property color borderColor: Theme.colors.borderSubtle
    property color focusBorderColor: Theme.colors.borderFocus
    property real normalBorderWidth: 1
    property real focusedBorderWidth: 1
    property real backgroundRadius: Theme.radius.md
    property bool qaForceKeyboardFocus: false
    property bool qaAllowHorizontalOverflow: false

    readonly property bool qaIsAppTextArea: true
    readonly property string qaText: text !== "" ? text : placeholderText
    readonly property real qaHorizontalPadding: Math.min(leftPadding, rightPadding)
    readonly property real qaVerticalPadding: Math.min(topPadding, bottomPadding)
    readonly property real qaContentWidth: contentWidth
    readonly property real qaTextAvailableWidth: Math.max(0, width - leftPadding - rightPadding)
    readonly property bool qaTextFits: !visible || qaAllowHorizontalOverflow || qaContentWidth <= qaTextAvailableWidth + 1.0
    readonly property bool qaVisualFocus: activeFocus

    implicitHeight: Math.max(72, contentHeight + topPadding + bottomPadding)
    selectByMouse: true
    selectByKeyboard: true
    wrapMode: TextEdit.Wrap
    font.family: Theme.uiFonts[0]
    font.pixelSize: Theme.fontSize.bodySm
    color: enabled ? Theme.colors.textPrimary : Theme.colors.textFaint
    placeholderTextColor: Theme.colors.textGhost
    selectionColor: Theme.colors.brandBg
    selectedTextColor: Theme.colors.textPrimary
    leftPadding: Theme.space.xl
    rightPadding: Theme.space.xl
    topPadding: Theme.space.lg
    bottomPadding: Theme.space.lg

    onQaForceKeyboardFocusChanged: {
        if (qaForceKeyboardFocus) {
            forceActiveFocus(Qt.TabFocusReason)
        }
    }

    background: Rectangle {
        implicitHeight: 72
        color: control.backgroundColor
        border.width: control.activeFocus ? control.focusedBorderWidth : control.normalBorderWidth
        border.color: control.activeFocus ? control.focusBorderColor : control.borderColor
        radius: control.backgroundRadius
        opacity: control.enabled ? 1.0 : 0.6

        Behavior on border.color { ColorAnimation { duration: Theme.duration.fast } }
    }
}
