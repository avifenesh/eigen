import QtQuick
import QtQuick.Controls
import QtQuick.Window
import "Theme.js" as Theme

ComboBox {
    id: control

    property int popupMaxHeight: 260
    property string accessibleName: ""
    property string toolTipText: ""
    property string fallbackText: ""
    property bool activationUpdatesCurrentIndex: true
    property int popupMinWidth: 220
    readonly property int popupHorizontalMargin: Theme.space.md
    property bool qaPopupOpen: false
    property bool qaForceKeyboardFocus: false
    property int qaActivateIndex: -1
    property int keyboardIndex: -1
    readonly property real popupSpacing: Theme.space.xs
    readonly property real popupNaturalHeight: Math.max(1, count) * 32
    readonly property real popupTargetHeight: Math.min(popupNaturalHeight, popupMaxHeight)
    readonly property real popupAvailableAbove: popupSpaceAbove()
    readonly property real popupAvailableBelow: popupSpaceBelow()
    readonly property bool popupOpensUp: popupAvailableBelow < popupTargetHeight && popupAvailableAbove > popupAvailableBelow
    readonly property real popupEffectiveHeight: Math.max(32, Math.min(popupTargetHeight, popupOpensUp ? popupAvailableAbove : popupAvailableBelow))
    readonly property real popupEffectiveWidth: popupWidth()
    readonly property real popupEffectiveX: popupXOffset()
    readonly property bool qaPopupActuallyOpen: popup.opened
    readonly property int qaCurrentIndex: currentIndex
    readonly property int qaKeyboardIndex: keyboardIndex
    readonly property int qaHighlightedIndex: highlightedIndex
    readonly property string effectiveDisplayText: displayText !== "" ? displayText : fallbackText
    readonly property string qaCurrentText: currentText
    readonly property real qaPopupAvailableAbove: popupAvailableAbove
    readonly property real qaPopupAvailableBelow: popupAvailableBelow
    readonly property real qaPopupEffectiveHeight: popupEffectiveHeight
    readonly property real qaPopupEffectiveWidth: popupEffectiveWidth
    readonly property bool qaPopupOpensUp: popupOpensUp
    readonly property bool qaPopupInsideWindow: popupInsideWindow()
    readonly property bool qaTextFits: !contentItem || !contentItem.text
        || (!contentItem.truncated && contentItem.paintedWidth <= Math.max(0, width - leftPadding - rightPadding) + 0.5)
    readonly property string qaText: effectiveDisplayText
    readonly property bool qaIsAppComboBox: true
    readonly property real qaLeftTextInset: displayLabel.leftPadding
    readonly property real qaRightTextInset: control.width - (displayLabel.leftPadding + displayLabel.paintedWidth)
    readonly property real qaHorizontalPadding: Math.min(qaLeftTextInset, qaRightTextInset)
    readonly property real qaVerticalPadding: Math.max(0, (control.height - displayLabel.paintedHeight) / 2)
    readonly property bool qaVisualFocus: visualFocus

    onQaPopupOpenChanged: syncQaPopup()
    onQaForceKeyboardFocusChanged: syncQaKeyboardFocus()
    onQaActivateIndexChanged: syncQaActivateIndex()
    onCurrentIndexChanged: {
        if (!popup.opened) {
            keyboardIndex = validKeyboardIndex(currentIndex)
        }
    }
    onCountChanged: keyboardIndex = validKeyboardIndex(keyboardIndex)
    Component.onCompleted: {
        syncQaPopup()
        syncQaKeyboardFocus()
        keyboardIndex = validKeyboardIndex(currentIndex)
    }

    implicitHeight: 32
    focusPolicy: Qt.StrongFocus
    leftPadding: Theme.space.lg
    rightPadding: Theme.space.xxxl
    Accessible.name: accessibleName || effectiveDisplayText || objectName
    ToolTip.delay: 600
    ToolTip.timeout: 4000
    ToolTip.visible: toolTipText !== "" && hovered
    ToolTip.text: toolTipText
    Keys.onPressed: function(event) { handleKey(event) }

    background: Rectangle {
        implicitHeight: 32
        color: !control.enabled
            ? Theme.colors.bgInset
            : (control.visualFocus ? Theme.colors.stateFocusBg : Theme.colors.bgRaised)
        border.width: control.visualFocus ? 2 : 1
        border.color: control.visualFocus
            ? Theme.colors.brandBright
            : (control.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle)
        radius: Theme.radius.sm
    }

    contentItem: Label {
        id: displayLabel
        text: control.effectiveDisplayText
        font.family: Theme.uiFonts[0]
        font.pixelSize: Theme.fontSize.bodySm
        color: control.enabled ? Theme.colors.textPrimary : Theme.colors.textFaint
        verticalAlignment: Text.AlignVCenter
        elide: Text.ElideRight
        leftPadding: control.leftPadding
        rightPadding: control.rightPadding
    }

    indicator: Canvas {
        x: control.width - width - Theme.space.md
        y: (control.height - height) / 2
        width: 10
        height: 6

        onPaint: {
            var ctx = getContext("2d")
            ctx.reset()
            ctx.strokeStyle = control.enabled ? Theme.colors.textMuted : Theme.colors.textFaint
            ctx.lineWidth = 1.5
            ctx.lineCap = "round"
            ctx.lineJoin = "round"
            ctx.beginPath()
            ctx.moveTo(1, 1)
            ctx.lineTo(width / 2, height - 1)
            ctx.lineTo(width - 1, 1)
            ctx.stroke()
        }
    }

    delegate: ItemDelegate {
        objectName: control.objectName ? (control.objectName + "_option_" + index) : ""
        width: control.popupEffectiveWidth
        height: 32
        implicitHeight: 32
        text: control.optionText(modelData)
        readonly property bool currentOption: control.currentIndex === index
        readonly property bool keyboardOption: control.popup.opened && control.keyboardIndex === index
        highlighted: control.highlightedIndex === index || keyboardOption
        onClicked: control.activateIndex(index)
        readonly property bool qaSelected: currentOption
        readonly property bool qaKeyboardHighlighted: keyboardOption
        readonly property bool qaIsAppComboBoxOption: true
        readonly property bool qaTextFits: !contentItem || !contentItem.text
            || (!contentItem.truncated && contentItem.paintedWidth <= Math.max(0, width - Theme.space.lg * 2) + 0.5)
        readonly property string qaText: text
        readonly property real qaLeftTextInset: optionLabel.leftPadding
        readonly property real qaRightTextInset: width - (optionLabel.leftPadding + optionLabel.paintedWidth)
        readonly property real qaHorizontalPadding: Math.min(qaLeftTextInset, qaRightTextInset)
        readonly property real qaVerticalPadding: Math.max(0, (height - optionLabel.paintedHeight) / 2)

        background: Rectangle {
            color: parent.highlighted
                ? Theme.colors.stateHover
                : (parent.currentOption ? Theme.colors.stateSelected : "transparent")

            Rectangle {
                anchors.left: parent.left
                anchors.top: parent.top
                anchors.bottom: parent.bottom
                width: 2
                visible: parent.parent.currentOption
                color: Theme.colors.brand
            }
        }

        contentItem: Label {
            id: optionLabel
            text: (parent.currentOption ? "✓  " : "   ") + parent.text
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.bodySm
            color: Theme.colors.textPrimary
            verticalAlignment: Text.AlignVCenter
            leftPadding: Theme.space.lg
            rightPadding: Theme.space.lg
            elide: Text.ElideRight
        }
    }

    popup: Popup {
        x: control.popupEffectiveX
        y: control.popupOpensUp ? -implicitHeight - control.popupSpacing : control.height + control.popupSpacing
        width: control.popupEffectiveWidth
        implicitHeight: control.popupEffectiveHeight
        padding: 0
        onOpened: control.keyboardIndex = control.validKeyboardIndex(control.currentIndex)
        onClosed: control.keyboardIndex = control.validKeyboardIndex(control.currentIndex)

        contentItem: ListView {
            clip: true
            implicitHeight: control.popupEffectiveHeight
            model: control.popup.visible ? control.delegateModel : null
            currentIndex: control.keyboardIndex >= 0 ? control.keyboardIndex : control.highlightedIndex
            cacheBuffer: control.popupMaxHeight
            boundsBehavior: Flickable.StopAtBounds

            ScrollBar.vertical: ScrollBar {
                policy: control.count * 32 > control.popupEffectiveHeight ? ScrollBar.AsNeeded : ScrollBar.AlwaysOff
            }
        }

        background: Rectangle {
            color: Theme.colors.surfaceOverlay
            radius: Theme.radius.sm
            border.width: 1
            border.color: Theme.colors.borderSubtle
        }
    }

    function syncQaPopup() {
        if (qaPopupOpen) {
            popup.open()
            forceActiveFocus(Qt.TabFocusReason)
        } else if (popup.opened) {
            popup.close()
        }
    }

    function syncQaKeyboardFocus() {
        if (qaForceKeyboardFocus) {
            forceActiveFocus(Qt.TabFocusReason)
        }
    }

    function syncQaActivateIndex() {
        if (qaActivateIndex < 0) return
        var index = qaActivateIndex
        qaActivateIndex = -1
        activateIndex(index)
    }

    function handleKey(event) {
        if (!enabled) return

        if (event.key === Qt.Key_Escape && popup.opened) {
            popup.close()
            event.accepted = true
            return
        }

        if (event.key === Qt.Key_Down || event.key === Qt.Key_Up) {
            if (!popup.opened) {
                popup.open()
            }
            moveKeyboardIndex(event.key === Qt.Key_Down ? 1 : -1)
            event.accepted = true
            return
        }

        if (event.key === Qt.Key_Home || event.key === Qt.Key_End) {
            if (!popup.opened) {
                popup.open()
            }
            keyboardIndex = event.key === Qt.Key_Home ? validKeyboardIndex(0) : validKeyboardIndex(count - 1)
            event.accepted = true
            return
        }

        if (event.key === Qt.Key_PageDown || event.key === Qt.Key_PageUp) {
            if (!popup.opened) {
                popup.open()
            }
            moveKeyboardIndex(event.key === Qt.Key_PageDown ? pageStep() : -pageStep())
            event.accepted = true
            return
        }

        if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter || event.key === Qt.Key_Space) {
            if (!popup.opened) {
                popup.open()
            } else {
                activateIndex(keyboardIndex >= 0 ? keyboardIndex : currentIndex)
            }
            event.accepted = true
            return
        }

        event.accepted = false
    }

    function validKeyboardIndex(index) {
        if (count <= 0) return -1
        if (index < 0) return 0
        if (index >= count) return count - 1
        return index
    }

    function moveKeyboardIndex(delta) {
        if (count <= 0) {
            keyboardIndex = -1
            return
        }
        keyboardIndex = validKeyboardIndex((keyboardIndex >= 0 ? keyboardIndex : currentIndex) + delta)
    }

    function pageStep() {
        return Math.max(3, Math.min(8, Math.floor(popupMaxHeight / Math.max(1, implicitHeight))))
    }

    function popupSpaceAbove() {
        var win = control.Window.window
        var content = win && win.contentItem ? win.contentItem : null
        if (!win || !content) return popupMaxHeight
        return Math.max(0, controlTopInWindow(content) - popupSpacing)
    }

    function popupSpaceBelow() {
        var win = control.Window.window
        var content = win && win.contentItem ? win.contentItem : null
        if (!win || !content) return popupMaxHeight
        return Math.max(0, win.height - (controlTopInWindow(content) + control.height) - popupSpacing)
    }

    function popupInsideWindow() {
        if (!popup.opened) return true
        var win = control.Window.window
        var content = win && win.contentItem ? win.contentItem : null
        if (!win || !content) return true
        var popupLeft = controlLeftInWindow(content) + popup.x
        var popupRight = popupLeft + popup.width
        var popupTop = controlTopInWindow(content) + popup.y
        var popupBottom = popupTop + popup.height
        return popupLeft >= -0.5 && popupRight <= win.width + 0.5
            && popupTop >= -0.5 && popupBottom <= win.height + 0.5
    }

    function popupWidth() {
        var win = control.Window.window
        var maxWidth = win ? Math.max(control.width, win.width - popupHorizontalMargin * 2) : popupMinWidth
        return Math.min(Math.max(control.width, popupMinWidth), maxWidth)
    }

    function popupXOffset() {
        var win = control.Window.window
        var content = win && win.contentItem ? win.contentItem : null
        if (!win || !content) return 0
        var left = controlLeftInWindow(content)
        var popupWidth = control.popupEffectiveWidth
        var x = 0
        if (left + x + popupWidth > win.width - popupHorizontalMargin) {
            x = win.width - popupHorizontalMargin - popupWidth - left
        }
        if (left + x < popupHorizontalMargin) {
            x = popupHorizontalMargin - left
        }
        return x
    }

    function controlTopInWindow(content) {
        var y = 0
        var item = control
        while (item && item !== content) {
            y += item.y || 0
            item = item.parent
        }
        return y
    }

    function controlLeftInWindow(content) {
        var x = 0
        var item = control
        while (item && item !== content) {
            x += item.x || 0
            item = item.parent
        }
        return x
    }

    function activateIndex(index) {
        index = validKeyboardIndex(index)
        if (index < 0) return
        if (activationUpdatesCurrentIndex) {
            currentIndex = index
        }
        popup.close()
        activated(index)
    }

    function optionText(value) {
        if (control.textRole && value && typeof value === "object") {
            value = value[control.textRole]
        }
        if (value === undefined || value === null) {
            return ""
        }
        return String(value)
    }
}
