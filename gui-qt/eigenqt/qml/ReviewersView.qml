import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Reviewers view — revuto AI PR-reviewer cockpit.
// Status header + per-repo list with Review now / Learn / Pause-Resume controls.
Rectangle {
    id: root
    objectName: "reviewersView"
    color: Theme.colors.bgBase

    property var reviewersModel: null  // ReviewersModel from Python

    // Per-repo busy state (map repo → bool)
    property var busy: ({})
    property var busyAction: ({})
    property string actionError: ""
    onReviewersModelChanged: syncActiveModel()
    onVisibleChanged: syncActiveModel()

    Component.onCompleted: syncActiveModel()
    Component.onDestruction: syncActiveModel(false)

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

                AppButton {
                    objectName: "reviewersRefreshButton"
                    visible: root.reviewersModel && (root.reviewersModel.available || root.hasLoadError())
                    text: root.reviewersModel && root.reviewersModel.loading ? "Refreshing…" : "Refresh"
                    toolTipText: "Refresh reviewers"
                    enabled: root.reviewersModel && !root.reviewersModel.loading
                    Layout.preferredHeight: 32
                    Layout.preferredWidth: 112
                    onClicked: root.reviewersModel.refresh()
                }
            }
        }

        Rectangle {
            objectName: "reviewersActionError"
            visible: root.actionError !== ""
            Layout.fillWidth: true
            Layout.preferredHeight: visible ? Math.max(40, reviewersActionErrorRow.implicitHeight + Theme.space.md) : 0
            color: Theme.colors.errorBg
            border.width: 1
            border.color: Theme.colors.error
            clip: true

            RowLayout {
                id: reviewersActionErrorRow
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xxxl
                anchors.rightMargin: Theme.space.xxxl
                spacing: Theme.space.md

                Label {
                    text: root.actionError
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.label
                    color: Theme.colors.error
                    wrapMode: Text.WrapAnywhere
                    Layout.fillWidth: true
                }

                AppButton {
                    objectName: "reviewersDismissErrorButton"
                    text: "X"
                    variant: "ghost"
                    compact: true
                    toolTipText: "Dismiss error"
                    Layout.preferredWidth: 28
                    Layout.preferredHeight: 28
                    onClicked: root.actionError = ""
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

                Rectangle {
                    objectName: "reviewersRefreshErrorBanner"
                    visible: root.hasLoadError() && root.reviewersRowCount() > 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? Math.max(44, reviewersRefreshErrorRow.implicitHeight + Theme.space.md) : 0
                    color: Theme.colors.errorBg
                    border.width: visible ? 1 : 0
                    border.color: Theme.colors.error
                    radius: Theme.radius.md
                    clip: true

                    RowLayout {
                        id: reviewersRefreshErrorRow
                        anchors.fill: parent
                        anchors.leftMargin: Theme.space.lg
                        anchors.rightMargin: Theme.space.lg
                        spacing: Theme.space.md

                        Label {
                            objectName: "reviewersRefreshErrorText"
                            text: root.reviewersModel ? root.reviewersModel.load_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.error
                            wrapMode: Text.WrapAnywhere
                            Layout.fillWidth: true
                        }

                        AppButton {
                            objectName: "reviewersRefreshErrorRetry"
                            text: "Retry"
                            compact: true
                            toolTipText: "Retry loading reviewers"
                            Layout.preferredWidth: 64
                            Layout.preferredHeight: 28
                            onClicked: root.retryLoad()
                        }
                    }
                }

                // REPO ROWS
                ColumnLayout {
                    id: reviewersList
                    objectName: "reviewersList"
                    Layout.fillWidth: true
                    spacing: Theme.space.sm

                    Repeater {
                        model: root.reviewersModel
                        delegate: Rectangle {
                            id: reviewerRow
                            objectName: "reviewerRow_" + reviewerKey
                            Layout.fillWidth: true
                            Layout.preferredHeight: ultraCompactLayout ? 144 : (compactLayout ? 104 : 56)
                            color: Theme.colors.bgRaised
                            border.width: 1
                            border.color: repoBusy ? Theme.colors.borderBrandFaint : Theme.colors.borderHairline
                            radius: Theme.radius.md
                            property string reviewerKey: model.repo ? model.repo.replace(/\//g, "_") : index
                            property string repoName: model.repo || ""
                            property bool repoPaused: model.paused || false
                            property bool repoBusy: !!root.busy[repoName]
                            property string repoBusyAction: root.busyAction[repoName] || ""
                            readonly property bool compactLayout: width < 640
                            readonly property bool ultraCompactLayout: width < 400

                            GridLayout {
                                id: reviewerGrid
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                columns: reviewerRow.ultraCompactLayout ? 2 : (reviewerRow.compactLayout ? 3 : 5)
                                columnSpacing: Theme.space.lg
                                rowSpacing: Theme.space.sm

                                // Repo name
                                Label {
                                    text: reviewerRow.repoName
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.body
                                    font.weight: Theme.fontWeight.medium
                                    color: Theme.colors.textPrimary
                                    elide: Text.ElideMiddle
                                    Layout.fillWidth: true
                                    Layout.row: 0
                                    Layout.column: 0
                                    Layout.columnSpan: reviewerRow.ultraCompactLayout ? 1 : (reviewerRow.compactLayout ? 2 : 1)
                                }

                                // Status badge
                                AppTag {
                                    visible: reviewerRow.repoPaused
                                    text: "paused"
                                    backgroundColor: Theme.colors.bgInset
                                    borderColor: Theme.colors.borderSubtle
                                    textColor: Theme.colors.textMuted
                                    fontPixelSize: Theme.fontSize.label
                                    fontWeight: Theme.fontWeight.medium
                                    minimumHeight: 22
                                    pill: false
                                    Layout.row: 0
                                    Layout.column: reviewerRow.ultraCompactLayout ? 1 : (reviewerRow.compactLayout ? 2 : 1)
                                }

                                AppTag {
                                    visible: !reviewerRow.repoPaused
                                    text: "active"
                                    backgroundColor: Theme.colors.successBg
                                    borderColor: Theme.colors.success
                                    textColor: Theme.colors.success
                                    fontPixelSize: Theme.fontSize.label
                                    fontWeight: Theme.fontWeight.medium
                                    minimumHeight: 22
                                    pill: false
                                    Layout.row: 0
                                    Layout.column: reviewerRow.ultraCompactLayout ? 1 : (reviewerRow.compactLayout ? 2 : 1)
                                }

                                // Actions
                                AppButton {
                                    objectName: "reviewerReviewButton_" + reviewerRow.reviewerKey
                                    text: reviewerRow.repoBusy && reviewerRow.repoBusyAction === "review" ? "Reviewing…" : "Review now"
                                    variant: "primary"
                                    toolTipText: "Run review for " + reviewerRow.repoName
                                    enabled: !reviewerRow.repoBusy
                                    Layout.preferredHeight: 32
                                    Layout.preferredWidth: reviewerRow.ultraCompactLayout
                                        ? reviewerGrid.width
                                        : (reviewerRow.compactLayout
                                        ? Math.max(0, (reviewerGrid.width - reviewerGrid.columnSpacing * 2) / 3)
                                        : 112)
                                    Layout.fillWidth: reviewerRow.compactLayout
                                    Layout.row: reviewerRow.compactLayout ? 1 : 0
                                    Layout.column: reviewerRow.compactLayout ? 0 : 2
                                    Layout.columnSpan: reviewerRow.ultraCompactLayout ? 2 : 1
                                    onClicked: triggerJob(reviewerRow.repoName, "review")
                                }

                                AppButton {
                                    objectName: "reviewerLearnButton_" + reviewerRow.reviewerKey
                                    text: reviewerRow.repoBusy && reviewerRow.repoBusyAction === "learn" ? "Learning…" : "Learn"
                                    toolTipText: "Run learn for " + reviewerRow.repoName
                                    enabled: !reviewerRow.repoBusy
                                    Layout.preferredHeight: 32
                                    Layout.preferredWidth: reviewerRow.ultraCompactLayout
                                        ? Math.max(0, (reviewerGrid.width - reviewerGrid.columnSpacing) / 2)
                                        : (reviewerRow.compactLayout
                                        ? Math.max(0, (reviewerGrid.width - reviewerGrid.columnSpacing * 2) / 3)
                                        : 96)
                                    Layout.fillWidth: reviewerRow.compactLayout
                                    Layout.row: reviewerRow.ultraCompactLayout ? 2 : (reviewerRow.compactLayout ? 1 : 0)
                                    Layout.column: reviewerRow.ultraCompactLayout ? 0 : (reviewerRow.compactLayout ? 1 : 3)
                                    onClicked: triggerJob(reviewerRow.repoName, "learn")
                                }

                                AppButton {
                                    objectName: "reviewerPauseButton_" + reviewerRow.reviewerKey
                                    text: pauseButtonText(reviewerRow.repoPaused, reviewerRow.repoBusyAction)
                                    variant: reviewerRow.repoPaused ? "primary" : "secondary"
                                    toolTipText: (reviewerRow.repoPaused ? "Resume " : "Pause ") + reviewerRow.repoName
                                    enabled: !reviewerRow.repoBusy
                                    Layout.preferredHeight: 32
                                    Layout.preferredWidth: reviewerRow.ultraCompactLayout
                                        ? Math.max(0, (reviewerGrid.width - reviewerGrid.columnSpacing) / 2)
                                        : (reviewerRow.compactLayout
                                        ? Math.max(0, (reviewerGrid.width - reviewerGrid.columnSpacing * 2) / 3)
                                        : 104)
                                    Layout.fillWidth: reviewerRow.compactLayout
                                    Layout.row: reviewerRow.ultraCompactLayout ? 2 : (reviewerRow.compactLayout ? 1 : 0)
                                    Layout.column: reviewerRow.ultraCompactLayout ? 1 : (reviewerRow.compactLayout ? 2 : 4)
                                    onClicked: togglePause(reviewerRow.repoName, reviewerRow.repoPaused)
                                }
                            }
                        }
                    }
                }

                // LOADING STATE — first status/reviewer fetch is still resolving
                Rectangle {
                    objectName: "reviewersLoadingState"
                    visible: root.reviewersModel && root.reviewersModel.loading && root.reviewersRowCount() === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: 240
                    Layout.topMargin: Theme.space.xxxxl
                    color: "transparent"

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.lg

                        Repeater {
                            model: 3
                            delegate: Rectangle {
                                Layout.preferredWidth: 520 - index * 64
                                Layout.preferredHeight: 38
                                radius: Theme.radius.md
                                color: Theme.colors.bgRaised
                                border.width: 1
                                border.color: Theme.colors.borderHairline
                                opacity: 0.55 - index * 0.1
                            }
                        }

                        Label {
                            text: "Loading reviewers…"
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textGhost
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                }

                Rectangle {
                    objectName: "reviewersLoadError"
                    visible: root.reviewersModel && !root.reviewersModel.loading && root.hasLoadError() && root.reviewersRowCount() === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 132 : 0
                    Layout.topMargin: Theme.space.xxxxl
                    color: Theme.colors.bgRaised
                    border.width: visible ? 1 : 0
                    border.color: Theme.colors.borderHairline
                    radius: Theme.radius.md

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.md

                        Label {
                            text: "Could not load reviewers"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h3
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textSecondary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            objectName: "reviewersLoadErrorText"
                            text: root.reviewersModel ? root.reviewersModel.load_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textMuted
                            wrapMode: Text.WrapAnywhere
                            horizontalAlignment: Text.AlignHCenter
                            Layout.maximumWidth: 720
                            Layout.alignment: Qt.AlignHCenter
                        }

                        AppButton {
                            objectName: "reviewersLoadErrorRetry"
                            text: "Retry"
                            compact: true
                            toolTipText: "Retry loading reviewers"
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: root.retryLoad()
                        }
                    }
                }

                // EMPTY STATE — no revuto
                Rectangle {
                    objectName: "reviewersUnavailableEmpty"
                    visible: root.reviewersModel && !root.reviewersModel.loading && !root.hasLoadError() && !root.reviewersModel.available
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
                    objectName: "reviewersNoReviewersEmpty"
                    visible: root.reviewersModel && !root.reviewersModel.loading && !root.hasLoadError() && root.reviewersModel.available && root.reviewersRowCount() === 0
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
            root.clearRepoBusy(repo)

            if (!success) {
                root.actionError = "Could not run " + job + " for " + repo + ": " + (error_msg || "unknown error")
            } else {
                root.actionError = ""
            }
        }
    }

    Connections {
        target: root.reviewersModel
        function onSet_paused_done(repo, success, error_msg) {
            root.clearRepoBusy(repo)

            if (!success) {
                root.actionError = "Could not update " + repo + ": " + (error_msg || "unknown error")
            } else {
                root.actionError = ""
            }
        }
    }

    // Helper functions
    function cloneMap(map) {
        var next = {}
        var source = map || {}
        for (var key in source) next[key] = source[key]
        return next
    }

    function syncActiveModel(activeOverride) {
        if (root.reviewersModel) {
            root.reviewersModel.set_active(activeOverride === undefined ? root.visible : activeOverride)
        }
    }

    function setRepoBusy(repo, action) {
        var newBusy = root.cloneMap(root.busy)
        var newActions = root.cloneMap(root.busyAction)
        newBusy[repo] = true
        newActions[repo] = action
        root.busy = newBusy
        root.busyAction = newActions
    }

    function clearRepoBusy(repo) {
        var newBusy = root.cloneMap(root.busy)
        var newActions = root.cloneMap(root.busyAction)
        delete newBusy[repo]
        delete newActions[repo]
        root.busy = newBusy
        root.busyAction = newActions
    }

    function pauseButtonText(paused, action) {
        if (action === "pause") return "Pausing…"
        if (action === "resume") return "Resuming…"
        return paused ? "Resume" : "Pause"
    }

    function statusSummary() {
        if (!root.reviewersModel) return ""
        if (root.hasLoadError()) {
            return "Could not refresh revuto"
        }
        if (root.reviewersModel.loading && root.reviewersRowCount() === 0) {
            return "Checking revuto…"
        }
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

    function hasLoadError() {
        return !!(root.reviewersModel && root.reviewersModel.load_error && root.reviewersModel.load_error !== "")
    }

    function reviewersRowCount() {
        return root.reviewersModel ? root.reviewersModel.rowCount() : 0
    }

    function retryLoad() {
        if (root.reviewersModel) {
            root.reviewersModel.refresh()
        }
    }

    function triggerJob(repo, job) {
        root.actionError = ""
        root.setRepoBusy(repo, job)

        root.reviewersModel.trigger(repo, job)
    }

    function togglePause(repo, currentPaused) {
        root.actionError = ""
        root.setRepoBusy(repo, currentPaused ? "resume" : "pause")

        root.reviewersModel.set_paused(repo, !currentPaused)
    }
}
