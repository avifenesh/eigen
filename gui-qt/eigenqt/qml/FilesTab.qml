/*
 * Files tab: file explorer + read-only file viewer.
 *
 * Layout:
 * - Top bar: "files" title + refresh button
 * - Split: tree (collapsible folders) + inline viewer (monospace, close button)
 *
 * Tree: indent per depth, folder chevrons (▸/▾), click to toggle dir or open file.
 * Viewer: file basename + close button, monospace content.
 */

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Eigen
import "Theme.js" as Theme

Item {
    id: root
    objectName: "filesTabRoot"

    required property string sessionDir
    required property var rpcClient

    // State
    property bool loading: false
    property string error: ""
    property bool truncated: false

    // File viewer state
    property string viewPath: ""
    property string viewText: ""
    property bool viewLoading: false
    property string viewError: ""
    readonly property int qaTreeRowCount: treeList.count
    readonly property bool qaViewerOpen: viewPath !== ""
    readonly property bool qaViewerCloseFits: !qaViewerOpen || viewerCloseButton.qaTextFits

    FileTreeModel {
        id: fileTreeModel
    }

    // Token-based RPC results (see rpc/client.py callToken — JS arrow
    // callbacks across event-loop turns are unreliable in PySide6).
    property int treeToken: -1
    property int readToken: -1

    Connections {
        target: rpcClient
        function onCallDone(token, payload) {
            if (token === root.treeToken) {
                root.treeToken = -1
                loading = false
                if (payload.error) {
                    error = payload.error
                    return
                }
                const data = payload.result
                truncated = data.truncated || false
                fileTreeModel.load(data.entries || [])
            } else if (token === root.readToken) {
                root.readToken = -1
                viewLoading = false
                if (payload.error) {
                    viewError = payload.error
                    return
                }
                viewText = payload.result || ""
            }
        }
    }

    Component.onCompleted: load()

    function load() {
        if (!sessionDir) return

        loading = true
        error = ""

        treeToken = rpcClient.callToken("FileTree", [sessionDir])
    }

    function openFile(path, isDir) {
        if (isDir) {
            fileTreeModel.toggle_dir(path)
            return
        }

        viewPath = path
        viewText = ""
        viewError = ""
        viewLoading = true

        readToken = rpcClient.callToken("ReadFileForView", [path])
    }

    function closeViewer() {
        viewPath = ""
        viewText = ""
        viewError = ""
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // Top bar
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 44
            color: Theme.colors.bgRaised

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.md
                anchors.rightMargin: Theme.space.md
                spacing: Theme.space.sm

                Text {
                    text: "files"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textSecondary
                }

                Item { Layout.fillWidth: true }

                AppButton {
                    objectName: "filesRefreshButton"
                    text: root.loading ? "…" : "Refresh"
                    compact: true
                    variant: "ghost"
                    toolTipText: "Refresh files"
                    enabled: !root.loading
                    Layout.preferredWidth: 72
                    Layout.preferredHeight: 28
                    onClicked: root.load()
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 1
            color: Theme.colors.borderHairline
        }

        // Content
        Item {
            Layout.fillWidth: true
            Layout.fillHeight: true

            Rectangle {
                anchors.fill: parent
                color: Theme.colors.bgBase
            }

            // Empty state: error
            Column {
                anchors.centerIn: parent
                visible: !loading && error !== ""
                spacing: Theme.space.md

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "⊟"
                    font.pixelSize: 48
                    color: Theme.colors.error
                }

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "Couldn't read files"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.body
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textSecondary
                }

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: error
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    width: 300
                    wrapMode: Text.WordWrap
                    horizontalAlignment: Text.AlignHCenter
                }
            }

            // Split: tree + viewer
            Item {
                id: splitPane
                anchors.fill: parent
                visible: !loading && error === ""

                // Tree
                Item {
                    id: treePane
                    anchors.left: parent.left
                    anchors.top: parent.top
                    anchors.bottom: parent.bottom
                    width: viewPath !== "" ? Math.round(parent.width * 0.4) : parent.width

                    ListView {
                        id: treeList
                        anchors.fill: parent
                        model: fileTreeModel
                        clip: true
                        spacing: 0

                        ScrollBar.vertical: ScrollBar {
                            policy: ScrollBar.AsNeeded
                        }

                        delegate: Rectangle {
                            width: ListView.view ? ListView.view.width : 0
                            height: 28
                            color: model.path === viewPath ? Theme.colors.stateSelected : (treeArea.containsMouse ? Theme.colors.stateHover : "transparent")

                            Row {
                                anchors.fill: parent
                                leftPadding: model.depth * Theme.space.md + Theme.space.sm
                                spacing: Theme.space.xs

                                // Chevron/dot glyph
                                Text {
                                    anchors.verticalCenter: parent.verticalCenter
                                    text: {
                                        if (model.isDir) return model.expanded ? "▾" : "▸"
                                        return "·"
                                    }
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textFaint
                                    width: 12
                                    horizontalAlignment: Text.AlignHCenter
                                }

                                Text {
                                    anchors.verticalCenter: parent.verticalCenter
                                    width: parent.width - 12 - Theme.space.xs - parent.leftPadding - Theme.space.md
                                    text: model.name || ""
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: model.path === viewPath ? Theme.colors.brandBright : Theme.colors.textSecondary
                                    elide: Text.ElideRight
                                }
                            }

                            MouseArea {
                                id: treeArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: openFile(model.path, model.isDir)
                            }
                        }
                    }

                    // Truncation warning
                    Rectangle {
                        visible: truncated
                        anchors.left: parent.left
                        anchors.right: parent.right
                        anchors.bottom: parent.bottom
                        height: 32
                        color: Theme.colors.bgRaised

                        Text {
                            anchors.centerIn: parent
                            text: "Tree truncated (large directory)."
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.warn
                        }
                    }
                }

                // Divider
                Rectangle {
                    id: splitDivider
                    visible: viewPath !== ""
                    anchors.left: treePane.right
                    anchors.top: parent.top
                    anchors.bottom: parent.bottom
                    width: 1
                    color: Theme.colors.borderHairline
                }

                // File viewer
                Item {
                    visible: viewPath !== ""
                    anchors.left: splitDivider.right
                    anchors.right: parent.right
                    anchors.top: parent.top
                    anchors.bottom: parent.bottom
                    clip: true

                    // Use explicit anchors here: this item is created hidden, and
                    // layout children can keep a zero-width header after the viewer opens.
                    Rectangle {
                        id: viewerHeader
                        anchors.left: parent.left
                        anchors.right: parent.right
                        anchors.top: parent.top
                        height: 40
                        color: Theme.colors.bgRaised

                        Text {
                            anchors.left: parent.left
                            anchors.right: viewerCloseButton.left
                            anchors.verticalCenter: parent.verticalCenter
                            anchors.leftMargin: Theme.space.md
                            anchors.rightMargin: Theme.space.sm
                            text: viewPath ? viewPath.split("/").pop() : ""
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            elide: Text.ElideMiddle
                        }

                        AppButton {
                            id: viewerCloseButton
                            objectName: "filesViewerCloseButton"
                            z: 10
                            anchors.right: parent.right
                            anchors.rightMargin: Theme.space.sm
                            anchors.verticalCenter: parent.verticalCenter
                            width: 28
                            height: 28
                            text: "✕"
                            compact: true
                            variant: "ghost"
                            toolTipText: "Close file"
                            onClicked: root.closeViewer()
                        }
                    }

                    Rectangle {
                        id: viewerDivider
                        anchors.left: parent.left
                        anchors.right: parent.right
                        anchors.top: viewerHeader.bottom
                        height: 1
                        color: Theme.colors.borderHairline
                    }

                    // Viewer body
                    Item {
                        anchors.left: parent.left
                        anchors.right: parent.right
                        anchors.top: viewerDivider.bottom
                        anchors.bottom: parent.bottom

                        // Loading
                        Text {
                            anchors.centerIn: parent
                            visible: viewLoading
                            text: "Loading…"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textMuted
                        }

                        // Error
                        Text {
                            anchors.centerIn: parent
                            visible: !viewLoading && viewError !== ""
                            text: viewError
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.warn
                            width: parent.width - Theme.space.xl * 2
                            wrapMode: Text.WordWrap
                            horizontalAlignment: Text.AlignHCenter
                        }

                        // Content
                        ScrollView {
                            anchors.fill: parent
                            visible: !viewLoading && viewError === ""
                            clip: true

                            AppTextArea {
                                id: filesViewerTextArea
                                objectName: "filesViewerTextArea"
                                text: viewText
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.codeSm
                                color: Theme.colors.synText
                                readOnly: true
                                wrapMode: TextArea.NoWrap
                                qaAllowHorizontalOverflow: true
                                backgroundColor: Theme.colors.synBg
                                borderColor: Theme.colors.borderHairline
                                focusBorderColor: Theme.colors.borderFocus
                                normalBorderWidth: 0
                                focusedBorderWidth: 1
                                backgroundRadius: 0
                                Accessible.name: "File viewer contents"
                            }
                        }
                    }
                }
            }

            // Loading state
            Text {
                anchors.centerIn: parent
                visible: loading
                text: "Loading…"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textMuted
            }
        }
    }
}
