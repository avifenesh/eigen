/*
 * Diff tab: shows git working-tree diff (vs HEAD) for the session's directory.
 *
 * Layout:
 * - Top bar: "working changes" title + branch badge + refresh button
 * - File list: per-file rows with +N/-N badges (collapsible)
 * - Diff body: hunks with add/del/context rows (monospace, colored gutters)
 *
 * Colors: diffAddBg/diffDelBg etc from Theme.colors (mirroring tokens.css).
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
    property bool isRepo: false
    property bool clean: false
    property string branch: ""
    property bool truncated: false

    DiffModel {
        id: diffModel
    }

    // QML RPC results arrive via the callDone(token, payload) signal — a JS
    // arrow-callback held across event-loop turns is unreliable in PySide6
    // (see rpc/client.py callToken).
    property int pendingToken: -1

    Connections {
        target: rpcClient
        function onCallDone(token, payload) {
            if (token !== root.pendingToken) return
            root.pendingToken = -1
            loading = false
            if (payload.error) {
                error = payload.error
                return
            }
            const data = payload.result
            isRepo = data.isRepo || false
            clean = data.clean || false
            branch = data.branch || ""
            truncated = data.truncated || false
            if (!isRepo || clean) {
                diffModel.load("", [])
            } else {
                diffModel.load(data.patch || "", data.files || [])
            }
        }
    }

    Component.onCompleted: load()

    function load() {
        if (!sessionDir) return

        loading = true
        error = ""

        pendingToken = rpcClient.callToken("WorkingDiff", [sessionDir])
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
                    text: "working changes"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textSecondary
                }

                // Branch badge
                Rectangle {
                    visible: branch !== ""
                    Layout.preferredHeight: 20
                    Layout.preferredWidth: branchLabel.implicitWidth + Theme.space.xs * 2
                    color: "transparent"
                    radius: Theme.radius.sm
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Text {
                        id: branchLabel
                        anchors.centerIn: parent
                        text: branch
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.textMuted
                    }
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

            // Empty state: not a repo
            Column {
                anchors.centerIn: parent
                visible: !loading && !isRepo
                spacing: Theme.space.md

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "⇄"
                    font.pixelSize: 48
                    color: Theme.colors.textGhost
                }

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "Not a git repository"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.body
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textSecondary
                }

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "The working directory isn't under version control."
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    width: 300
                    wrapMode: Text.WordWrap
                    horizontalAlignment: Text.AlignHCenter
                }
            }

            // Empty state: clean
            Column {
                anchors.centerIn: parent
                visible: !loading && isRepo && clean && error === ""
                spacing: Theme.space.md

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "✓"
                    font.pixelSize: 48
                    color: Theme.colors.success
                }

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "Working tree clean"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.body
                    font.weight: Theme.fontWeight.semibold
                    color: Theme.colors.textSecondary
                }

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "No pending changes against HEAD."
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                }
            }

            // Empty state: error
            Column {
                anchors.centerIn: parent
                visible: !loading && error !== ""
                spacing: Theme.space.md

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "⚠"
                    font.pixelSize: 48
                    color: Theme.colors.error
                }

                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "Couldn't read changes"
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

            // Diff view
            ListView {
                id: diffList
                anchors.fill: parent
                visible: !loading && isRepo && !clean && error === ""
                model: diffModel
                clip: true
                spacing: 0

                ScrollBar.vertical: ScrollBar {
                    policy: ScrollBar.AsNeeded
                }

                delegate: Item {
                    width: ListView.view ? ListView.view.width : 0
                    height: {
                        if (model.kind === "file") return 32
                        if (model.kind === "hunk") return 24
                        if (model.kind === "meta") return 20
                        return 20
                    }

                    // File header row
                    Rectangle {
                        anchors.fill: parent
                        visible: model.kind === "file"
                        color: Theme.colors.bgRaised
                        border.width: 0
                        border.color: Theme.colors.borderHairline

                        RowLayout {
                            anchors.fill: parent
                            anchors.leftMargin: Theme.space.sm
                            anchors.rightMargin: Theme.space.md
                            spacing: Theme.space.sm

                            Text {
                                Layout.fillWidth: true
                                text: model.filePath || ""
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textSecondary
                                elide: Text.ElideLeft
                                horizontalAlignment: Text.AlignRight
                            }

                            Row {
                                spacing: Theme.space.xs

                                Text {
                                    visible: model.adds > 0
                                    text: "+" + model.adds
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.success
                                }

                                Text {
                                    visible: model.dels > 0
                                    text: "−" + model.dels
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.error
                                }
                            }
                        }

                        MouseArea {
                            anchors.fill: parent
                            cursorShape: Qt.PointingHandCursor
                            onClicked: diffModel.toggle_file(model.filePath)
                        }
                    }

                    // Hunk header row
                    Rectangle {
                        anchors.fill: parent
                        visible: model.kind === "hunk"
                        color: Theme.colors.bgBase

                        Row {
                            anchors.fill: parent
                            anchors.leftMargin: Theme.space.md
                            spacing: 0

                            // Gutter (empty for hunk)
                            Rectangle {
                                width: 32
                                height: parent.height
                                color: Theme.colors.bgBase
                            }

                            Text {
                                width: parent.width - 32 - Theme.space.md
                                text: model.text || ""
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.codeSm
                                color: Theme.colors.textFaint
                            }
                        }
                    }

                    // Add/del/context rows
                    Rectangle {
                        anchors.fill: parent
                        visible: model.kind === "add" || model.kind === "del" || model.kind === "ctx"
                        color: {
                            if (model.kind === "add") return Theme.colors.diffAddBg
                            if (model.kind === "del") return Theme.colors.diffDelBg
                            return "transparent"
                        }

                        Row {
                            anchors.fill: parent
                            spacing: 0

                            // Gutter
                            Rectangle {
                                width: 32
                                height: parent.height
                                color: parent.parent.color

                                // Left edge marker for add/del
                                Rectangle {
                                    visible: model.kind === "add" || model.kind === "del"
                                    anchors.left: parent.left
                                    anchors.top: parent.top
                                    anchors.bottom: parent.bottom
                                    width: 2
                                    color: {
                                        if (model.kind === "add") return Theme.colors.diffAddGutter
                                        if (model.kind === "del") return Theme.colors.diffDelGutter
                                        return "transparent"
                                    }
                                }

                                Text {
                                    anchors.centerIn: parent
                                    text: model.sign || ""
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.codeSm
                                    font.weight: (model.kind === "add" || model.kind === "del") ? Theme.fontWeight.semibold : Theme.fontWeight.regular
                                    color: {
                                        if (model.kind === "add") return Theme.colors.success
                                        if (model.kind === "del") return Theme.colors.error
                                        return Theme.colors.textGhost
                                    }
                                }
                            }

                            // Line content
                            Text {
                                width: parent.width - 32
                                leftPadding: Theme.space.md
                                rightPadding: Theme.space.md
                                text: model.text || ""
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.codeSm
                                color: {
                                    if (model.kind === "add" || model.kind === "del") return Theme.colors.textPrimary
                                    return Theme.colors.textSecondary
                                }
                                wrapMode: Text.NoWrap
                            }
                        }
                    }

                    // Meta rows (diff/index/---)
                    Rectangle {
                        anchors.fill: parent
                        visible: model.kind === "meta"
                        color: Theme.colors.bgBase

                        Row {
                            anchors.fill: parent
                            spacing: 0

                            // Gutter (empty)
                            Rectangle {
                                width: 32
                                height: parent.height
                                color: Theme.colors.bgBase
                            }

                            Text {
                                width: parent.width - 32
                                leftPadding: Theme.space.md
                                text: model.text || ""
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.codeSm
                                color: Theme.colors.textGhost
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

        // Truncation warning
        Rectangle {
            visible: truncated
            Layout.fillWidth: true
            Layout.preferredHeight: 32
            color: Theme.colors.bgRaised

            Text {
                anchors.centerIn: parent
                text: "Diff truncated (very large) — showing the start."
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.warn
            }
        }
    }
}
