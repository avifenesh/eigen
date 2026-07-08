import QtQuick
import QtQuick.Controls
import "Theme.js" as Theme

Button {
    id: control

    property string variant: "secondary" // primary | secondary | ghost | danger
    property string toolTipText: ""
    property bool selected: false
    property bool compact: false
    property bool pill: false
    property bool leadingDotVisible: false
    property color leadingDotColor: Theme.colors.textFaint
    property string badgeText: ""
    property string segmentPosition: "none" // none | first | middle | last
    property real compactHorizontalPadding: Theme.space.lg
    property real compactIconHorizontalPadding: Theme.space.md
    property int contentAlignment: Text.AlignHCenter
    property bool qaForceKeyboardFocus: false
    readonly property bool qaIsAppButton: true
    readonly property bool qaTextFits: textContentFits(contentItem)
    readonly property string qaText: text
    readonly property real qaHorizontalPadding: Math.min(leftPadding, rightPadding)
    readonly property real qaVerticalPadding: Math.min(topPadding, bottomPadding)
    readonly property bool showingFocus: visualFocus || activeFocus
    readonly property bool qaVisualFocus: showingFocus

    implicitWidth: Math.max(32, contentItem.implicitWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(compact ? 24 : 32, contentItem.implicitHeight + topPadding + bottomPadding)
    focusPolicy: Qt.StrongFocus
    hoverEnabled: true
    leftPadding: compact ? effectiveCompactHorizontalPadding() : Theme.space.xl
    rightPadding: compact ? effectiveCompactHorizontalPadding() : Theme.space.xl
    topPadding: compact ? Theme.space.xs : Theme.space.sm
    bottomPadding: compact ? Theme.space.xs : Theme.space.sm
    Accessible.name: toolTipText || text
    ToolTip.delay: 600
    ToolTip.timeout: 4000
    ToolTip.visible: toolTipText !== "" && hovered
    ToolTip.text: toolTipText
    onQaForceKeyboardFocusChanged: syncQaKeyboardFocus()
    Component.onCompleted: syncQaKeyboardFocus()
    Keys.onReturnPressed: function(event) { activateFromKey(event) }
    Keys.onEnterPressed: function(event) { activateFromKey(event) }
    Keys.onSpacePressed: function(event) { activateFromKey(event) }

    background: Rectangle {
        radius: control.pill ? Theme.radius.full : (control.segmentPosition === "middle" ? 0 : Theme.radius.sm)
        color: {
            if (!control.enabled) return control.variant === "primary" ? Theme.colors.surfaceRaised2 : Theme.colors.bgInset
            if (control.down) return Theme.colors.stateActive
            if (control.selected) return Theme.colors.stateSelected
            if (control.hovered) return control.variant === "primary" ? Theme.colors.brand : Theme.colors.stateHover
            if (control.variant === "primary") return Theme.colors.brandBright
            if (control.variant === "danger") return Theme.colors.errorBg
            return control.variant === "ghost" ? "transparent" : Theme.colors.bgRaised
        }
        border.width: control.showingFocus ? 2 : (control.variant === "primary" ? 0 : 1)
        border.color: {
            if (control.showingFocus) return control.variant === "primary" ? Theme.colors.textPrimary : Theme.colors.brandBright
            if (!control.enabled) return control.variant === "primary" ? Theme.colors.borderBrandFaint : Theme.colors.borderHairline
            if (control.variant === "danger") return Theme.colors.error
            if (control.selected) return Theme.colors.borderBrandFaint
            return control.hovered ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
        }

        Rectangle {
            visible: control.segmentPosition === "first"
            anchors.top: parent.top
            anchors.right: parent.right
            anchors.bottom: parent.bottom
            width: parent.radius
            color: parent.color
            border.width: 0
        }

        Rectangle {
            visible: control.segmentPosition === "last"
            anchors.top: parent.top
            anchors.left: parent.left
            anchors.bottom: parent.bottom
            width: parent.radius
            color: parent.color
            border.width: 0
        }

        Rectangle {
            visible: control.segmentPosition === "first" || control.segmentPosition === "middle"
            anchors.right: parent.right
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            width: 1
            color: control.showingFocus ? Theme.colors.brandBright : Theme.colors.borderSubtle
            opacity: control.selected ? 0.45 : 0.7
        }
    }

    contentItem: Item {
        id: defaultContentItem

        implicitWidth: (control.leadingDotVisible ? 7 + Theme.space.sm : 0)
            + buttonLabel.implicitWidth
            + (control.badgeText !== "" ? Theme.space.sm + badgeLabel.implicitWidth : 0)
        implicitHeight: Math.max(buttonLabel.implicitHeight, control.leadingDotVisible ? 7 : 0, badgeLabel.implicitHeight)

        Row {
            id: contentRow
            x: control.contentAlignment === Text.AlignLeft
                ? 0
                : (control.contentAlignment === Text.AlignRight
                    ? parent.width - width
                    : Math.max(0, (parent.width - width) / 2))
            width: Math.max(0, parent.width)
            spacing: Theme.space.sm
            anchors.verticalCenter: parent.verticalCenter

            Rectangle {
                visible: control.leadingDotVisible
                width: 7
                height: 7
                radius: 4
                color: control.leadingDotColor
                anchors.verticalCenter: parent.verticalCenter
            }

            Label {
                id: buttonLabel
                width: Math.max(0, contentRow.width
                    - (control.leadingDotVisible ? 7 + contentRow.spacing : 0)
                    - (control.badgeText !== "" ? badgeLabel.implicitWidth + contentRow.spacing : 0))
                anchors.verticalCenter: parent.verticalCenter
                text: control.text
                font.family: Theme.uiFonts[0]
                font.pixelSize: control.compact ? Theme.fontSize.label : Theme.fontSize.bodySm
                font.weight: (control.variant === "primary" || control.selected) ? Theme.fontWeight.semibold : Theme.fontWeight.medium
                color: {
                    if (!control.enabled) return control.variant === "primary" ? Theme.colors.textMuted : Theme.colors.textFaint
                    if (control.variant === "primary") return Theme.colors.bgBase
                    if (control.variant === "danger") return Theme.colors.error
                    if (control.selected) return Theme.colors.brandBright
                    return Theme.colors.textSecondary
                }
                horizontalAlignment: control.contentAlignment
                verticalAlignment: Text.AlignVCenter
                elide: Text.ElideRight
            }

            Label {
                id: badgeLabel
                visible: control.badgeText !== ""
                anchors.verticalCenter: parent.verticalCenter
                text: control.badgeText
                font.family: Theme.monoFonts[0]
                font.pixelSize: Theme.fontSize.micro
                color: control.selected ? Theme.colors.brand : Theme.colors.textFaint
                verticalAlignment: Text.AlignVCenter
            }
        }
    }

    function syncQaKeyboardFocus() {
        if (qaForceKeyboardFocus) {
            forceActiveFocus(Qt.TabFocusReason)
        }
    }

    function effectiveCompactHorizontalPadding() {
        if (String(text).length <= 1 && badgeText === "" && !leadingDotVisible) {
            return compactIconHorizontalPadding
        }
        return compactHorizontalPadding
    }

    function textContentFits(item) {
        if (!item || item.visible === false) {
            return true
        }
        if (item.qaTextFits !== undefined && item.qaTextFits !== null) {
            return !!item.qaTextFits
        }
        if (item.text !== undefined && item.text !== null && String(item.text).length > 0
                && item.paintedWidth !== undefined && item.paintedWidth !== null
                && item.width !== undefined && item.width !== null && item.width > 0) {
            if (item.truncated !== undefined && item.truncated !== null && item.truncated) {
                return false
            }
            if (item.paintedWidth > Math.max(0, item.width) + 0.5) {
                return false
            }
        }

        if (item.children !== undefined && item.children !== null) {
            for (var i = 0; i < item.children.length; i++) {
                if (!textContentFits(item.children[i])) {
                    return false
                }
            }
        }
        return true
    }

    function activateFromKey(event) {
        if (enabled) {
            click()
        }
        event.accepted = true
    }
}
