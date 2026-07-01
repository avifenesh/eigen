import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Approval overlay (centered sheet over transcript)
Rectangle {
    id: root
    width: 480
    height: column.height + Theme.space.xl * 2
    color: Theme.colors.surfaceOverlay
    radius: Theme.radius.lg
    border.width: 1
    border.color: Theme.colors.borderStrong

    property var model

    signal approve(string approvalId, bool allow)

    ColumnLayout {
        id: column
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: Theme.space.xl
        spacing: Theme.space.lg

        Label {
            text: "Approval Required"
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.h2
            font.weight: Theme.fontWeight.semibold
            color: Theme.colors.textPrimary
        }

        ListView {
            Layout.fillWidth: true
            Layout.preferredHeight: contentHeight
            clip: true
            spacing: Theme.space.md
            interactive: false
            model: root.model

            delegate: Rectangle {
                width: ListView.view.width
                height: delegateColumn.height + Theme.space.lg * 2
                color: Theme.colors.surfaceRaised
                radius: Theme.radius.md
                border.width: 1
                border.color: Theme.colors.borderSubtle

                ColumnLayout {
                    id: delegateColumn
                    anchors.left: parent.left
                    anchors.right: parent.right
                    anchors.top: parent.top
                    anchors.margins: Theme.space.lg
                    spacing: Theme.space.md

                    Label {
                        text: model.tool
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                        Layout.fillWidth: true
                    }

                    Label {
                        text: model.args
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textSecondary
                        wrapMode: Text.Wrap
                        Layout.fillWidth: true
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.space.md

                        Button {
                            text: "Deny"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            onClicked: root.approve(model.id, false)

                            background: Rectangle {
                                color: parent.hovered ? Theme.colors.errorBg : "transparent"
                                border.width: 1
                                border.color: Theme.colors.error
                                radius: Theme.radius.sm
                                implicitWidth: 80
                                implicitHeight: 32
                            }

                            contentItem: Label {
                                text: parent.text
                                font: parent.font
                                color: Theme.colors.error
                                horizontalAlignment: Text.AlignHCenter
                                verticalAlignment: Text.AlignVCenter
                            }
                        }

                        Item { Layout.fillWidth: true }

                        Button {
                            text: "Allow"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.medium
                            onClicked: root.approve(model.id, true)

                            background: Rectangle {
                                color: parent.hovered ? Theme.colors.brandStrong : Theme.colors.brand
                                radius: Theme.radius.sm
                                implicitWidth: 80
                                implicitHeight: 32
                            }

                            contentItem: Label {
                                text: parent.text
                                font: parent.font
                                color: "#06100e"
                                horizontalAlignment: Text.AlignHCenter
                                verticalAlignment: Text.AlignVCenter
                            }
                        }
                    }
                }
            }
        }
    }
}
