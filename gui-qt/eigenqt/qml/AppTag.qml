import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

Rectangle {
    id: root

    property string text: ""
    property color backgroundColor: Theme.colors.bgOverlay
    property color borderColor: Theme.colors.borderHairline
    property color textColor: Theme.colors.textSecondary
    property string fontFamily: Theme.uiFonts[0]
    property int fontPixelSize: Theme.fontSize.micro
    property int fontWeight: Theme.fontWeight.regular
    property int capitalization: Font.MixedCase
    property int elideMode: Text.ElideRight
    property real horizontalPadding: Theme.space.md
    property real verticalPadding: Theme.space.xxs
    property real minimumHeight: 20
    property bool pill: true
    property int borderWidth: 1

    readonly property bool qaIsAppTag: true
    readonly property bool qaTextFits: tagLabel.implicitWidth <= tagLabel.width + 1.0
    readonly property real qaLeftTextInset: tagLabel.x + Math.max(0, (tagLabel.width - tagLabel.paintedWidth) / 2)
    readonly property real qaRightTextInset: root.width - (tagLabel.x + tagLabel.width / 2 + tagLabel.paintedWidth / 2)
    readonly property real qaHorizontalPadding: Math.min(qaLeftTextInset, qaRightTextInset)

    implicitWidth: Math.max(tagLabel.implicitWidth + horizontalPadding * 2, horizontalPadding * 2 + 4)
    implicitHeight: Math.max(minimumHeight, tagLabel.implicitHeight + verticalPadding * 2)
    Layout.minimumWidth: implicitWidth
    Layout.preferredWidth: implicitWidth
    Layout.minimumHeight: implicitHeight
    Layout.preferredHeight: implicitHeight
    radius: pill ? height / 2 : Theme.radius.sm
    color: backgroundColor
    border.width: borderWidth
    border.color: borderColor
    clip: true

    Label {
        id: tagLabel
        anchors.fill: parent
        anchors.leftMargin: root.horizontalPadding
        anchors.rightMargin: root.horizontalPadding
        text: root.text
        font.family: root.fontFamily
        font.pixelSize: root.fontPixelSize
        font.weight: root.fontWeight
        font.capitalization: root.capitalization
        color: root.textColor
        horizontalAlignment: Text.AlignHCenter
        verticalAlignment: Text.AlignVCenter
        elide: root.elideMode
        maximumLineCount: 1
    }
}
