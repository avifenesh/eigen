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
    objectName: "dockPanel"

    // Props
    required property string sessionDir  // Session's working directory
    required property var rpcClient      // RPC client for bridge calls

    color: Theme.colors.bgRaised
    border.width: 1
    border.color: Theme.colors.borderSubtle

    // Signals
    signal closed()

    property int currentTab: 0  // 0=Diff, 1=Files
    property int preferredTab: 0

    Component.onCompleted: currentTab = preferredTab
    onPreferredTabChanged: currentTab = preferredTab

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

                    AppButton {
                        objectName: "dockTab_" + modelData
                        text: modelData
                        compact: true
                        variant: "ghost"
                        selected: root.currentTab === index
                        segmentPosition: index === 0 ? "first" : "last"
                        toolTipText: modelData === "Diff" ? "Show working diff" : "Show files"
                        Layout.preferredHeight: 32
                        onClicked: root.currentTab = index
                    }
                }

                Item { Layout.fillWidth: true }

                AppButton {
                    objectName: "dockCloseButton"
                    text: "✕"
                    compact: true
                    variant: "ghost"
                    toolTipText: "Close dock"
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 32
                    onClicked: root.closed()
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
