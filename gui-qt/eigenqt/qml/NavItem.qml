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
    readonly property int baseHeight: 30
    readonly property bool qaVisualFocus: activeFocus
    readonly property bool qaTextFits: !navLabel.truncated
    signal clicked()

    default property alias contentData: subContainer.data

    objectName: route ? "navItem_" + route : ""
    implicitHeight: baseHeight + subContainer.implicitHeight
    radius: Theme.radius.sm
    activeFocusOnTab: true
    focusPolicy: Qt.StrongFocus
    Accessible.role: Accessible.Button
    Accessible.name: label
    Accessible.description: isActive ? "Current route" : "Open " + label
    Accessible.onPressAction: activate()
    color: navItem.isActive ? Theme.colors.stateSelected : (mainMouseArea.containsMouse ? Theme.colors.stateHover : "transparent")

    Behavior on color {
        ColorAnimation { duration: Theme.duration.fast }
    }

    // Active left edge (teal bar)
    Rectangle {
        visible: navItem.isActive
        anchors.left: parent.left
        anchors.leftMargin: -Theme.space.sm
        anchors.verticalCenter: mainArea.verticalCenter
        width: 2
        height: 24
        radius: 9
        color: Theme.colors.brandBright

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
        anchors.leftMargin: Theme.space.lg
        anchors.rightMargin: Theme.space.sm
        spacing: Theme.space.sm

        // Glyph
        Label {
            text: navItem.glyph
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.bodySm
            color: navItem.isActive ? Theme.colors.brand : Theme.colors.textMuted
            opacity: navItem.isActive ? 1.0 : 0.85
            Layout.preferredWidth: 18
            horizontalAlignment: Text.AlignHCenter

            Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
        }

        // Label
        Label {
            id: navLabel
            text: navItem.label
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.bodySm
            font.weight: Theme.fontWeight.medium
            color: navItem.isActive ? Theme.colors.brandBright : Theme.colors.textSecondary
            elide: Text.ElideRight
            Layout.fillWidth: true

            Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
        }

        // Badge
        Rectangle {
            visible: navItem.badge > 0
            implicitWidth: Math.max(18, badgeLabel.implicitWidth + 10)
            implicitHeight: 18
            radius: 9
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
                anchors.centerIn: parent
                text: navItem.badge
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.micro
                font.weight: Theme.fontWeight.semibold
                color: navItem.badgeLive ? Theme.colors.brandBright : Theme.colors.textSecondary
            }
        }
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
        height: implicitHeight
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
