import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Reviewers view — revuto AI PR-reviewer cockpit.
// Status header + per-repo list with Review now / Learn / Pause-Resume controls.
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property var reviewersModel: null  // ReviewersModel from Python

    // Per-repo busy state (map repo → bool)
    property var busy: ({})

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // HEADER — status + refresh
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 64
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.margins: Theme.space.xxxl
                spacing: Theme.space.lg

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Theme.space.xs

                    Label {
                        text: "Reviewers"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.h2
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        text: statusSummary()
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.textMuted
                    }
                }

                Button {
                    visible: root.reviewersModel && root.reviewersModel.available
                    text: "Refresh"
                    Layout.preferredHeight: 32
                    onClicked: root.reviewersModel.refresh()

                    background: Rectangle {
                        color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                        border.width: 1
                        border.color: Theme.colors.borderSubtle
                        radius: Theme.radius.sm
                    }

                    contentItem: Label {
                        text: parent.text
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textSecondary
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }
                }
            }
        }

        // CONTENT — repo list or empty state
        Flickable {
            Layout.fillWidth: true
            Layout.fillHeight: true
            contentWidth: width
            contentHeight: contentColumn.implicitHeight
            clip: true

            ColumnLayout {
                id: contentColumn
                width: Math.min(parent.width - Theme.space.xxxxl, 880)
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: Theme.space.sm

                Item { height: Theme.space.lg }

                // REPO ROWS
                Repeater {
                    model: root.reviewersModel
                    delegate: Rectangle {
                        Layout.fillWidth: true
                        implicitHeight: 56
                        color: Theme.colors.bgRaised
                        border.width: 1
                        border.color: Theme.colors.borderHairline
                        radius: Theme.radius.md

                        RowLayout {
                            anchors.fill: parent
                            anchors.margins: Theme.space.lg
                            spacing: Theme.space.lg

                            // Repo name
                            Label {
                                text: model.repo
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.body
                                font.weight: Theme.fontWeight.medium
                                color: Theme.colors.textPrimary
                                Layout.fillWidth: true
                            }

                            // Status badge
                            Rectangle {
                                visible: model.paused
                                implicitWidth: pausedLabel.implicitWidth + Theme.space.lg
                                implicitHeight: 22
                                radius: Theme.radius.sm
                                color: Theme.colors.bgInset
                                border.width: 1
                                border.color: Theme.colors.borderSubtle

                                Label {
                                    id: pausedLabel
                                    anchors.centerIn: parent
                                    text: "paused"
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.medium
                                    color: Theme.colors.textMuted
                                }
                            }

                            Rectangle {
                                visible: !model.paused
                                implicitWidth: activeLabel.implicitWidth + Theme.space.lg
                                implicitHeight: 22
                                radius: Theme.radius.sm
                                color: Qt.rgba(
                                    Theme.colors.success.r,
                                    Theme.colors.success.g,
                                    Theme.colors.success.b,
                                    0.12
                                )
                                border.width: 1
                                border.color: Qt.rgba(
                                    Theme.colors.success.r,
                                    Theme.colors.success.g,
                                    Theme.colors.success.b,
                                    0.3
                                )

                                Label {
                                    id: activeLabel
                                    anchors.centerIn: parent
                                    text: "active"
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.medium
                                    color: Theme.colors.success
                                }
                            }

                            // Actions
                            Button {
                                text: root.busy[model.repo] ? "Running…" : "Review now"
                                enabled: !root.busy[model.repo]
                                Layout.preferredHeight: 32
                                onClicked: triggerJob(model.repo, "review")

                                background: Rectangle {
                                    color: parent.down ? Theme.colors.brandStrong : (parent.hovered ? Theme.colors.brand : Theme.colors.brandDim)
                                    radius: Theme.radius.sm
                                    opacity: parent.enabled ? 1.0 : 0.6
                                }

                                contentItem: Label {
                                    text: parent.text
                                    font.pixelSize: Theme.fontSize.bodySm
                                    font.weight: Theme.fontWeight.medium
                                    color: Theme.colors.textPrimary
                                    horizontalAlignment: Text.AlignHCenter
                                    verticalAlignment: Text.AlignVCenter
                                }
                            }

                            Button {
                                text: "Learn"
                                enabled: !root.busy[model.repo]
                                Layout.preferredHeight: 32
                                onClicked: triggerJob(model.repo, "learn")

                                background: Rectangle {
                                    color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                                    border.width: 1
                                    border.color: Theme.colors.borderSubtle
                                    radius: Theme.radius.sm
                                    opacity: parent.enabled ? 1.0 : 0.6
                                }

                                contentItem: Label {
                                    text: parent.text
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textSecondary
                                    horizontalAlignment: Text.AlignHCenter
                                    verticalAlignment: Text.AlignVCenter
                                }
                            }

                            Button {
                                text: model.paused ? "Resume" : "Pause"
                                enabled: !root.busy[model.repo]
                                Layout.preferredHeight: 32
                                onClicked: togglePause(model.repo, model.paused)

                                background: Rectangle {
                                    color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                                    border.width: 1
                                    border.color: Theme.colors.borderSubtle
                                    radius: Theme.radius.sm
                                    opacity: parent.enabled ? 1.0 : 0.6
                                }

                                contentItem: Label {
                                    text: parent.text
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textSecondary
                                    horizontalAlignment: Text.AlignHCenter
                                    verticalAlignment: Text.AlignVCenter
                                }
                            }
                        }
                    }
                }

                // EMPTY STATE — no revuto
                Rectangle {
                    visible: root.reviewersModel && !root.reviewersModel.available
                    Layout.fillWidth: true
                    Layout.preferredHeight: 240
                    Layout.topMargin: Theme.space.xxxxl
                    color: "transparent"

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.lg

                        Label {
                            text: "⌕"
                            font.pixelSize: 48
                            color: Theme.colors.textFaint
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            text: "Revuto not installed"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h3
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textSecondary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            text: "Install the `revuto` CLI to manage your AI PR-reviewer from here."
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textMuted
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                }

                // EMPTY STATE — no reviewers registered
                Rectangle {
                    visible: root.reviewersModel && root.reviewersModel.available && root.reviewersModel.rowCount() === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: 240
                    Layout.topMargin: Theme.space.xxxxl
                    color: "transparent"

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.lg

                        Label {
                            text: "⌕"
                            font.pixelSize: 48
                            color: Theme.colors.textFaint
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            text: "No reviewers registered"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h3
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textSecondary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            text: "Register a repo with `revuto init owner/repo`."
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textMuted
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                }

                Item { height: Theme.space.xxxl }
            }
        }
    }

    Connections {
        target: root.reviewersModel
        function onTrigger_done(repo, job, success, error_msg) {
            var newBusy = root.busy
            delete newBusy[repo]
            root.busy = newBusy

            if (success) {
                console.log("revuto " + job + " done: " + repo)
            } else {
                console.error("revuto " + job + " failed for " + repo + ": " + error_msg)
            }
        }
    }

    Connections {
        target: root.reviewersModel
        function onSet_paused_done(repo, success, error_msg) {
            var newBusy = root.busy
            delete newBusy[repo]
            root.busy = newBusy

            if (success) {
                console.log("revuto pause toggled: " + repo)
            } else {
                console.error("revuto pause toggle failed for " + repo + ": " + error_msg)
            }
        }
    }

    // Helper functions
    function statusSummary() {
        if (!root.reviewersModel) return ""
        if (!root.reviewersModel.available) {
            return "CLI not found"
        }
        var count = root.reviewersModel.count
        var paused = root.reviewersModel.paused_count
        if (count === 0) {
            return "No reviewers registered"
        }
        var suffix = count === 1 ? " repo" : " repos"
        var msg = count + suffix
        if (paused > 0) {
            msg += ", " + paused + " paused"
        }
        return "Your revuto AI PR-reviewer — " + msg + "."
    }

    function triggerJob(repo, job) {
        var newBusy = root.busy
        newBusy[repo] = true
        root.busy = newBusy

        console.log("revuto " + job + " " + repo + " — running (may take a while)…")
        root.reviewersModel.trigger(repo, job)
    }

    function togglePause(repo, currentPaused) {
        var newBusy = root.busy
        newBusy[repo] = true
        root.busy = newBusy

        root.reviewersModel.set_paused(repo, !currentPaused)
    }
}
