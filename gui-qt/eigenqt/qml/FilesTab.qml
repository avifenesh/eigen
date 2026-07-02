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

                // Refresh button
                Rectangle {
                    Layout.preferredWidth: 60
                    Layout.preferredHeight: 28
                    color: refreshArea.containsMouse ? Theme.colors.stateHover : "transparent"
                    radius: Theme.radius.sm

                    Text {
                        anchors.centerIn: parent
                        text: root.loading ? "…" : "refresh"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        font.weight: Theme.fontWeight.medium
                        color: Theme.colors.textMuted
                    }

                    MouseArea {
                        id: refreshArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        enabled: !root.loading
                        onClicked: load()
                    }
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
            Row {
                anchors.fill: parent
                spacing: 0
                visible: !loading && error === ""

                // Tree
                Item {
                    width: viewPath !== "" ? parent.width * 0.4 : parent.width
                    height: parent.height

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
                    visible: viewPath !== ""
                    width: 1
                    height: parent.height
                    color: Theme.colors.borderHairline
                }

                // File viewer
                Item {
                    visible: viewPath !== ""
                    width: viewPath !== "" ? parent.width * 0.6 - 1 : 0
                    height: parent.height

                    ColumnLayout {
                        anchors.fill: parent
                        spacing: 0

                        // Viewer header
                        Rectangle {
                            Layout.fillWidth: true
                            Layout.preferredHeight: 40
                            color: Theme.colors.bgRaised

                            RowLayout {
                                anchors.fill: parent
                                anchors.leftMargin: Theme.space.md
                                anchors.rightMargin: Theme.space.sm
                                spacing: Theme.space.sm

                                Text {
                                    Layout.fillWidth: true
                                    text: viewPath ? viewPath.split("/").pop() : ""
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.textPrimary
                                    elide: Text.ElideMiddle
                                }

                                // Close button
                                Rectangle {
                                    Layout.preferredWidth: 28
                                    Layout.preferredHeight: 28
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
                                        onClicked: closeViewer()
                                    }
                                }
                            }
                        }

                        Rectangle {
                            Layout.fillWidth: true
                            Layout.preferredHeight: 1
                            color: Theme.colors.borderHairline
                        }

                        // Viewer body
                        Item {
                            Layout.fillWidth: true
                            Layout.fillHeight: true

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

                                TextArea {
                                    text: viewText
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.codeSm
                                    color: Theme.colors.synText
                                    readOnly: true
                                    selectByMouse: true
                                    wrapMode: TextArea.NoWrap
                                    background: Rectangle {
                                        color: Theme.colors.synBg
                                    }
                                }
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
