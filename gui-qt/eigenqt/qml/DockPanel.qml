/*
 * Right-side dock panel with Diff | Files tabs.
 *
 * Toggleable from SessionSettingsStrip; state is per-session.
 * Tabs switch between DiffTab (git working-tree diff) and FilesTab (file explorer).
 */

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Eigen
import "Theme.js" as Theme

Rectangle {
    id: root

    // Props
    required property string sessionDir  // Session's working directory
    required property var rpcClient      // RPC client for bridge calls

    color: Theme.colors.bgRaised
    border.width: 1
    border.color: Theme.colors.borderSubtle

    // Signals
    signal closed()

    property int currentTab: 0  // 0=Diff, 1=Files

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // Tab bar
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 44
            color: Theme.colors.bgRaised

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.md
                anchors.rightMargin: Theme.space.md
                spacing: Theme.space.xs

                // Tab buttons
                Repeater {
                    model: ["Diff", "Files"]

                    Rectangle {
                        Layout.preferredHeight: 32
                        Layout.preferredWidth: tabLabel.implicitWidth + Theme.space.lg * 2
                        color: currentTab === index ? Theme.colors.stateFocusBg : "transparent"
                        radius: Theme.radius.sm
                        border.width: currentTab === index ? 1 : 0
                        border.color: Theme.colors.borderBrandFaint

                        Text {
                            id: tabLabel
                            anchors.centerIn: parent
                            text: modelData
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            font.weight: currentTab === index ? Theme.fontWeight.semibold : Theme.fontWeight.regular
                            color: currentTab === index ? Theme.colors.brandBright : Theme.colors.textSecondary
                        }

                        MouseArea {
                            anchors.fill: parent
                            cursorShape: Qt.PointingHandCursor
                            onClicked: currentTab = index
                        }
                    }
                }

                Item { Layout.fillWidth: true }

                // Close button
                Rectangle {
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 32
                    color: closeArea.containsMouse ? Theme.colors.stateHover : "transparent"
                    radius: Theme.radius.sm

                    Text {
                        anchors.centerIn: parent
                        text: "✕"
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textMuted
                    }

                    MouseArea {
                        id: closeArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: root.closed()
                    }
                }
            }
        }

        // Divider
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 1
            color: Theme.colors.borderHairline
        }

        // Tab content
        StackLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            currentIndex: currentTab

            // Diff tab
            DiffTab {
                sessionDir: root.sessionDir
                rpcClient: root.rpcClient
            }

            // Files tab
            FilesTab {
                sessionDir: root.sessionDir
                rpcClient: root.rpcClient
            }
        }
    }
}
