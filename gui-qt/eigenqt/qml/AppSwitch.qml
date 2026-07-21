import QtQuick
import QtQuick.Controls
import "Theme.js" as Theme

Switch {
    id: control

    property string accessibleName: ""
    property string toolTipText: ""
    property bool qaForceKeyboardFocus: false
    property bool qaShowToolTip: false
    readonly property bool qaChecked: checked
    readonly property bool qaTextFits: true
    readonly property string qaText: checked ? "on" : "off"
    readonly property string qaAccessibleName: accessibleName || toolTipText || objectName
    readonly property bool showingFocus: visualFocus || activeFocus
    readonly property bool qaVisualFocus: showingFocus
    readonly property bool qaToolTipThemed: appToolTip.qaThemed
    readonly property color qaToolTipBackgroundColor: appToolTip.qaBackgroundColor
    readonly property color qaToolTipTextColor: appToolTip.qaTextColor
    readonly property real qaToolTipHorizontalPadding: appToolTip.qaHorizontalPadding
    readonly property real qaToolTipVerticalPadding: appToolTip.qaVerticalPadding
    readonly property bool qaToolTipVisible: appToolTip.visible

    implicitWidth: 44
    implicitHeight: 24
    padding: 0
    spacing: 0
    focusPolicy: Qt.StrongFocus
    hoverEnabled: true
    Accessible.name: qaAccessibleName
    Accessible.description: checked ? "On" : "Off"
    Accessible.role: Accessible.Button
    Accessible.onPressAction: click()
    onQaForceKeyboardFocusChanged: syncQaKeyboardFocus()
    Component.onCompleted: syncQaKeyboardFocus()
    Keys.onReturnPressed: function(event) { activateFromKey(event) }
    Keys.onEnterPressed: function(event) { activateFromKey(event) }
    Keys.onSpacePressed: function(event) { activateFromKey(event) }

    AppToolTip {
        id: appToolTip
        objectName: control.objectName ? control.objectName + "_tooltip" : "appSwitchTooltip"
        delay: control.qaShowToolTip ? 0 : 600
        persistent: control.qaShowToolTip
        requestedVisible: control.toolTipText !== "" && (control.hovered || control.qaShowToolTip)
        text: control.toolTipText
    }

    indicator: Rectangle {
        x: 0
        y: Math.max(0, (control.height - height) / 2)
        width: control.width
        height: control.height
        radius: Theme.radius.full
        color: {
            if (!control.enabled) return Theme.colors.bgInset
            if (control.down) return control.checked ? Theme.colors.brand : Theme.colors.stateActive
            if (control.checked) return Theme.colors.brandDim
            if (control.hovered) return Theme.colors.stateHover
            return Theme.colors.bgInset
        }
        border.width: control.showingFocus ? 2 : 1
        border.color: {
            if (control.showingFocus) return Theme.colors.borderFocus
            if (!control.enabled) return Theme.colors.borderHairline
            if (control.checked) return Theme.colors.brand
            return Theme.colors.borderSubtle
        }

        Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
        Behavior on border.color { ColorAnimation { duration: Theme.duration.fast } }

        Rectangle {
            width: Math.max(10, parent.height - 6)
            height: width
            radius: width / 2
            x: control.checked ? parent.width - width - 3 : 3
            y: Math.max(0, (parent.height - height) / 2)
            color: {
                if (!control.enabled) return Theme.colors.textFaint
                return control.checked ? Theme.colors.brandBright : Theme.colors.textSecondary
            }

            Behavior on x {
                NumberAnimation { duration: Theme.duration.fast; easing.type: Easing.OutCubic }
            }
            Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
        }
    }

    contentItem: Item {
        implicitWidth: 0
        implicitHeight: 0
    }

    function syncQaKeyboardFocus() {
        if (qaForceKeyboardFocus) {
            forceActiveFocus(Qt.TabFocusReason)
        }
    }

    function activateFromKey(event) {
        if (enabled) {
            click()
        }
        event.accepted = true
    }
}
