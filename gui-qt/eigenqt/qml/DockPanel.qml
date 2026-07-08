/*
 * Right-side dock panel with Diff | Files | Info tabs.
 *
 * Toggleable from SessionSettingsStrip; state is per-session.
 * Tabs switch between DiffTab (git working-tree diff), FilesTab (file
 * explorer), and DockInfoTab (session metadata).
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
    property var sessionStateModel: null

    color: Theme.colors.bgRaised
    border.width: 1
    border.color: Theme.colors.borderSubtle

    // Signals
    signal closed()

    property int currentTab: 0  // 0=Diff, 1=Files, 2=Info
    property int preferredTab: 0
    readonly property var tabLabels: ["Diff", "Files", "Info"]

    Component.onCompleted: currentTab = clampTab(preferredTab)
    onPreferredTabChanged: currentTab = clampTab(preferredTab)

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
                    model: root.tabLabels

                    AppButton {
                        objectName: "dockTab_" + modelData
                        text: modelData
                        compact: true
                        variant: "ghost"
                        selected: root.currentTab === index
                        segmentPosition: index === 0 ? "first" : (index === root.tabLabels.length - 1 ? "last" : "middle")
                        toolTipText: root.tabToolTip(modelData)
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

            // Info tab
            DockInfoTab {
                sessionStateModel: root.sessionStateModel
            }
        }
    }

    function clampTab(tabIndex) {
        var value = Number(tabIndex)
        if (isNaN(value)) value = 0
        return Math.max(0, Math.min(root.tabLabels.length - 1, value))
    }

    function tabToolTip(label) {
        if (label === "Diff") return "Show working diff"
        if (label === "Files") return "Show files"
        return "Show session info"
    }
}
