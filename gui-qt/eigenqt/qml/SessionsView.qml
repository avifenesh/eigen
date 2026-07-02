import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Full sessions list view (center pane when no session selected)
Rectangle {
    id: root
    color: Theme.colors.bgBase

    signal sessionClicked(string sessionId)

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: Theme.space.xxxl
        spacing: Theme.space.xl

        Label {
            text: "Sessions"
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.h1
            font.weight: Theme.fontWeight.semibold
            color: Theme.colors.textPrimary
        }

        ListView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            spacing: Theme.space.md
            model: sessionsModel

            delegate: Rectangle {
                width: ListView.view.width
                height: 72
                radius: Theme.radius.md
                color: mouseArea.containsMouse ? Theme.colors.surfaceRaised2 : Theme.colors.surfaceRaised
                border.width: 1
                border.color: Theme.colors.borderHairline

                Behavior on color { ColorAnimation { duration: Theme.duration.fast } }

                MouseArea {
                    id: mouseArea
                    anchors.fill: parent
                    hoverEnabled: true
                    onClicked: root.sessionClicked(model.id)
                }

                RowLayout {
                    anchors.fill: parent
                    anchors.margins: Theme.space.lg
                    spacing: Theme.space.lg

                    // Status dot
                    Rectangle {
                        width: 12
                        height: 12
                        radius: 6
                        color: Theme.statusColor(model.status)
                        Layout.alignment: Qt.AlignVCenter

                        // Breathing animation for working status
                        SequentialAnimation on opacity {
                            running: model.status === "working"
                            loops: Animation.Infinite
                            NumberAnimation { from: 1.0; to: 0.3; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
                            NumberAnimation { from: 0.3; to: 1.0; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
                        }
                    }

                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: Theme.space.xs

                        Label {
                            text: model.title
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h3
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.fillWidth: true
                        }

                        RowLayout {
                            spacing: Theme.space.lg
                            Layout.fillWidth: true

                            Label {
                                text: model.dir
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                color: Theme.colors.textMuted
                                elide: Text.ElideMiddle
                                Layout.fillWidth: true
                            }

                            Label {
                                text: model.model
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                font.weight: Theme.fontWeight.medium
                                color: Theme.colors.textSecondary
                            }

                            Label {
                                text: model.turns + " turns"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                color: Theme.colors.textMuted
                            }

                            Label {
                                text: formatTimestamp(model.updated)
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                color: Theme.colors.textMuted
                            }
                        }
                    }
                }
            }
        }
    }

    function formatTimestamp(ts) {
        if (!ts) return ""
        // Simple relative time (real implementation would use proper time formatting)
        return new Date(ts).toLocaleString()
    }
}
