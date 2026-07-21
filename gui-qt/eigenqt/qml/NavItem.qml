import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// NavItem — rail navigation button with glyph, label, badge, active state
Rectangle {
    id: navItem
    property string route: ""
    property string label: ""
    property string glyph: ""
    property int badge: 0
    property bool badgeLive: false
    property bool isActive: false
    property bool collapsed: false
    property bool qaShowToolTip: false
    readonly property int baseHeight: 32
    readonly property int collapsedBadgeSize: 17
    readonly property bool qaVisualFocus: activeFocus
    readonly property bool qaTextFits: !navLabel.truncated
    readonly property bool qaCollapsed: collapsed
    readonly property bool qaLabelVisible: navLabel.visible
    readonly property bool qaCollapsedBadgeVisible: collapsedBadge.visible
    readonly property bool qaToolTipVisible: collapsedToolTip.visible
    signal clicked()

    default property alias contentData: subContainer.data

    objectName: route ? "navItem_" + route : ""
    implicitHeight: baseHeight + (collapsed ? 0 : subContainer.implicitHeight)
    radius: Theme.radius.sm
    activeFocusOnTab: true
    focusPolicy: Qt.StrongFocus
    Accessible.role: Accessible.Button
    Accessible.name: label
    Accessible.description: isActive ? "Current route" : "Open " + label
    Accessible.onPressAction: activate()
    color: navItem.isActive ? Theme.colors.stateSelected : (mainMouseArea.containsMouse ? Theme.colors.stateHover : "transparent")
    border.width: navItem.activeFocus ? 1 : 0
    border.color: navItem.activeFocus ? Theme.colors.borderFocus : "transparent"

    Behavior on color {
        ColorAnimation { duration: Theme.duration.fast }
    }

    AppToolTip {
        id: collapsedToolTip
        objectName: navItem.route ? "navTooltip_" + navItem.route : "navItemTooltip"
        delay: navItem.qaShowToolTip ? 0 : 600
        persistent: navItem.qaShowToolTip
        requestedVisible: navItem.collapsed && (mainMouseArea.containsMouse || navItem.qaShowToolTip)
        text: navItem.label
    }

    // Focus track: warm clay marks where this window is, leaving teal to brand.
    Rectangle {
        visible: navItem.isActive
        anchors.left: parent.left
        anchors.leftMargin: -Theme.space.sm
        anchors.verticalCenter: mainArea.verticalCenter
        width: 2
        height: 24
        radius: 9
        color: Theme.colors.focus

        // Spring-grow animation
        transform: Scale {
            origin.x: 1
            origin.y: 12
            yScale: navItem.isActive ? 1.0 : 0.0
            Behavior on yScale {
                NumberAnimation { duration: Theme.duration.base; easing.type: Easing.OutBack }
            }
        }
    }

    Item {
        id: mainArea
        anchors.top: parent.top
        anchors.left: parent.left
        anchors.right: parent.right
        height: navItem.baseHeight

    MouseArea {
        id: mainMouseArea
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: navItem.activate()
    }

    RowLayout {
        anchors.fill: parent
        anchors.leftMargin: navItem.collapsed ? Theme.space.sm : Theme.space.lg
        anchors.rightMargin: navItem.collapsed ? Theme.space.sm : Theme.space.sm
        spacing: Theme.space.sm

        // Glyph
        Label {
            text: navItem.glyph
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.bodySm
            color: navItem.isActive ? Theme.colors.focus : Theme.colors.textMuted
            opacity: navItem.isActive ? 1.0 : 0.85
            Layout.preferredWidth: 18
            Layout.fillWidth: navItem.collapsed
            horizontalAlignment: Text.AlignHCenter

            Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
        }

        // Label
        Label {
            id: navLabel
            visible: !navItem.collapsed
            text: navItem.label
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.bodySm
            font.weight: Theme.fontWeight.medium
            color: navItem.isActive ? Theme.colors.focusBright : Theme.colors.textSecondary
            elide: Text.ElideRight
            Layout.fillWidth: true

            Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
        }

        // Badge
        Rectangle {
            id: navBadge
            objectName: navItem.route ? "navBadge_" + navItem.route : ""
            readonly property bool qaIsNavBadge: true
            readonly property bool qaTextFits: badgeLabel.implicitWidth <= badgeLabel.width + 1.0
            readonly property real qaLeftTextInset: badgeLabel.x + Math.max(0, (badgeLabel.width - badgeLabel.paintedWidth) / 2)
            readonly property real qaRightTextInset: navBadge.width - (badgeLabel.x + badgeLabel.width / 2 + badgeLabel.paintedWidth / 2)
            readonly property real qaTopTextInset: badgeLabel.y + Math.max(0, (badgeLabel.height - badgeLabel.paintedHeight) / 2)
            readonly property real qaBottomTextInset: navBadge.height - (badgeLabel.y + badgeLabel.height / 2 + badgeLabel.paintedHeight / 2)
            readonly property real qaHorizontalPadding: Math.min(qaLeftTextInset, qaRightTextInset)
            readonly property real qaVerticalPadding: Math.min(qaTopTextInset, qaBottomTextInset)

            visible: !navItem.collapsed && navItem.badge > 0
            implicitWidth: Math.max(34, badgeLabel.implicitWidth + Theme.space.xl * 2)
            implicitHeight: Math.max(26, badgeLabel.implicitHeight + Theme.space.md * 2)
            radius: implicitHeight / 2
            color: navItem.badgeLive ? Theme.colors.stateSelected : Theme.colors.bgOverlay
            border.width: navItem.badgeLive ? 1 : 0
            border.color: Theme.colors.borderBrandFaint

            // Breathing animation for live badges
            SequentialAnimation on opacity {
                running: Theme.continuousMotion && navItem.badgeLive
                loops: Animation.Infinite
                NumberAnimation { from: 1.0; to: 0.62; duration: Theme.duration.breath / 2 }
                NumberAnimation { from: 0.62; to: 1.0; duration: Theme.duration.breath / 2 }
            }

            Label {
                id: badgeLabel
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xl
                anchors.rightMargin: Theme.space.xl
                anchors.topMargin: Theme.space.md
                anchors.bottomMargin: Theme.space.md
                text: navItem.badge
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.micro
                font.weight: Theme.fontWeight.semibold
                color: navItem.badgeLive ? Theme.colors.focusBright : Theme.colors.textSecondary
                horizontalAlignment: Text.AlignHCenter
                verticalAlignment: Text.AlignVCenter
            }
        }
    }

    }

    Rectangle {
        id: collapsedBadge
        objectName: navItem.route ? "navCollapsedBadge_" + navItem.route : ""
        visible: navItem.collapsed && navItem.badge > 0
        anchors.top: parent.top
        anchors.right: parent.right
        anchors.topMargin: 2
        anchors.rightMargin: 2
        width: Math.max(navItem.collapsedBadgeSize, collapsedBadgeLabel.implicitWidth + Theme.space.sm)
        height: navItem.collapsedBadgeSize
        radius: height / 2
        color: navItem.badgeLive ? Theme.colors.stateSelected : Theme.colors.bgOverlay
        border.width: 1
        border.color: navItem.badgeLive ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle

        Label {
            id: collapsedBadgeLabel
            anchors.centerIn: parent
            text: navItem.badge
            font.family: Theme.monoFonts[0]
            font.pixelSize: 9
            font.weight: Theme.fontWeight.semibold
            color: navItem.badgeLive ? Theme.colors.focusBright : Theme.colors.textSecondary
        }
    }

    // Container for sub-content (like running sessions under Chat)
    Item {
        id: subContainer
        anchors.top: mainArea.bottom
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.topMargin: 0
        implicitHeight: childrenRect.height
        height: navItem.collapsed ? 0 : implicitHeight
        visible: !navItem.collapsed
        clip: true
    }

    Keys.onReturnPressed: function(event) {
        activate()
        event.accepted = true
    }

    Keys.onEnterPressed: function(event) {
        activate()
        event.accepted = true
    }

    Keys.onSpacePressed: function(event) {
        activate()
        event.accepted = true
    }

    function activate() {
        navItem.clicked()
    }
}
