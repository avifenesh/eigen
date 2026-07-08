import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// BoardView — cross-project work board with lanes (git state + cards) and kanban view
Rectangle {
    id: root
    color: Theme.colors.bgBase

    signal sessionStarted(string sessionId)

    // View toggle: "projects" or "kanban"
    property string viewMode: "projects"

    // Filter state
    property string ownerFilter: "all"
    property string stateFilter: "all"

    property var boardModel: null
    property var kanbanModel: null
    property var rpcClient: null
    property var sessionsModel: null
    property var pendingActions: ({})
    property var tokenActions: ({})
    property string actionError: ""
    property string boardActionError: ""
    readonly property string visibleActionError: actionError !== "" ? actionError : boardActionError

    // Role constants (Qt.UserRole + N, matching BoardModel Python roles)
    readonly property int dirRole: 257
    readonly property int nameRole: 258
    readonly property int repoRole: 259
    readonly property int branchRole: 260
    readonly property int urlRole: 261
    readonly property int remoteRole: 262
    readonly property int pinnedRole: 263
    readonly property int dirtyRole: 264
    readonly property int unpushedRole: 265
    readonly property int behindRole: 266
    readonly property int todosRole: 267
    readonly property int openPrsRole: 268
    readonly property int openIssRole: 269
    readonly property int itemsRole: 270

    // Owners derived from lanes
    function computeOwners() {
        if (!boardModel) return []
        var ownerSet = {}
        for (var i = 0; i < boardModel.rowCount(); i++) {
            var idx = boardModel.index(i, 0)
            var remote = boardModel.data(idx, remoteRole)
            var repo = boardModel.data(idx, repoRole)
            if (remote && repo && repo.indexOf("/") >= 0) {
                var owner = repo.split("/")[0]
                ownerSet[owner] = true
            }
        }
        var result = []
        for (var o in ownerSet) {
            result.push(o)
        }
        return result.sort()
    }
    property int boardRows: 0
    property var owners: []

    // Owner filter options
    property var ownerOptions: {
        var opts = [
            { value: "all", label: "All" },
            { value: "local", label: "Local" }
        ]
        for (var i = 0; i < owners.length; i++) {
            opts.push({ value: owners[i], label: owners[i] })
        }
        return opts
    }

    readonly property var stateOptions: [
        { value: "all", label: "Everything" },
        { value: "prs", label: "PRs" },
        { value: "issues", label: "Issues" },
        { value: "dirty", label: "Uncommitted" }
    ]

    component BoardBadge: Rectangle {
        id: badge
        property string text: ""
        property color backgroundColor: Theme.colors.bgRaised2
        property color borderColor: Theme.colors.borderSubtle
        property color textColor: Theme.colors.textSecondary
        property string fontFamily: Theme.monoFonts[0]
        property int fontPixelSize: Theme.fontSize.micro
        property int fontWeight: Theme.fontWeight.medium

        readonly property bool qaIsBoardBadge: true
        readonly property bool qaTextFits: badgeLabel.implicitWidth <= badgeLabel.width + 1.0
        readonly property real qaLeftTextInset: badgeLabel.x + Math.max(0, (badgeLabel.width - badgeLabel.paintedWidth) / 2)
        readonly property real qaRightTextInset: badge.width - (badgeLabel.x + badgeLabel.width / 2 + badgeLabel.paintedWidth / 2)
        readonly property real qaTopTextInset: badgeLabel.y + Math.max(0, (badgeLabel.height - badgeLabel.paintedHeight) / 2)
        readonly property real qaBottomTextInset: badge.height - (badgeLabel.y + badgeLabel.height / 2 + badgeLabel.paintedHeight / 2)
        readonly property real qaHorizontalPadding: Math.min(qaLeftTextInset, qaRightTextInset)
        readonly property real qaVerticalPadding: Math.min(qaTopTextInset, qaBottomTextInset)

        width: implicitWidth
        height: implicitHeight
        implicitWidth: Math.max(badgeLabel.implicitWidth + Theme.space.xl * 2, Theme.space.xl * 2 + 4)
        implicitHeight: Math.max(24, badgeLabel.implicitHeight + Theme.space.sm * 2)
        radius: Theme.radius.full
        color: backgroundColor
        border.width: 1
        border.color: borderColor
        clip: true

        Label {
            id: badgeLabel
            anchors.fill: parent
            anchors.leftMargin: Theme.space.xl
            anchors.rightMargin: Theme.space.xl
            anchors.topMargin: Theme.space.sm
            anchors.bottomMargin: Theme.space.sm
            text: badge.text
            font.family: badge.fontFamily
            font.pixelSize: badge.fontPixelSize
            font.weight: badge.fontWeight
            color: badge.textColor
            horizontalAlignment: Text.AlignHCenter
            verticalAlignment: Text.AlignVCenter
            elide: Text.ElideRight
            maximumLineCount: 1
        }
    }

    Connections {
        target: root.rpcClient ? root.rpcClient : null
        function onCallDone(token, payload) {
            root.handleCallDone(token, payload)
        }
    }

    Connections {
        target: root.boardModel ? root.boardModel : null
        ignoreUnknownSignals: true
        function onModelReset() { root.syncBoardRows() }
        function onRowsInserted() { root.syncBoardRows() }
        function onRowsRemoved() { root.syncBoardRows() }
        function onDataChanged() { root.syncBoardRows() }
        function onActionErrorChanged() { root.syncActionError() }
    }

    Component.onCompleted: {
        syncBoardRows()
        syncActionError()
        if (boardModel) boardModel.load()
        if (kanbanModel) kanbanModel.load()
    }

    // Helpers
    function syncBoardRows() {
        boardRows = boardModel ? boardModel.rowCount() : 0
        filteredBoardRows = computeFilteredBoardRows(boardRows)
        owners = computeOwners()
    }

    function syncActionError() {
        boardActionError = boardModel ? String(boardModel.actionError || "") : ""
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]+/g, "_")
    }

    function laneKeyAt(row) {
        if (!boardModel) return ""
        var idx = boardModel.index(row, 0)
        return boardModel.data(idx, remoteRole)
            ? boardModel.data(idx, repoRole)
            : boardModel.data(idx, dirRole)
    }

    function errorText(error) {
        if (error === undefined || error === null) return "Something went wrong."
        if (typeof error === "string") return error
        if (error.message) return String(error.message)
        return JSON.stringify(error)
    }

    function clearActionError() {
        actionError = ""
        if (boardModel) boardModel.clearActionError()
    }

    function isPending(key) {
        return pendingActions[key] === true
    }

    function setPending(key, pending) {
        var next = Object.assign({}, pendingActions)
        if (pending) next[key] = true
        else delete next[key]
        pendingActions = next
    }

    function rememberToken(token, key, method) {
        var next = Object.assign({}, tokenActions)
        next[token] = { key: key, method: method }
        tokenActions = next
    }

    function forgetToken(token) {
        var next = Object.assign({}, tokenActions)
        delete next[token]
        tokenActions = next
    }

    function startAction(key, method, args) {
        if (!rpcClient || isPending(key)) return
        clearActionError()
        setPending(key, true)
        var token = rpcClient.callToken(method, args || [])
        rememberToken(token, key, method)
    }

    function handleCallDone(token, payload) {
        var action = tokenActions[token]
        if (!action) return
        forgetToken(token)
        setPending(action.key, false)
        if (payload && payload.error !== undefined && payload.error !== null) {
            actionError = errorText(payload.error)
            return
        }
        var sessionId = payload ? String(payload.result || "") : ""
        if (sessionId !== "") {
            sessionStarted(sessionId)
        }
    }

    function startLaneSession(dir) {
        if (!dir) return
        startAction("lane:" + dir, "NewSession", [dir, "", ""])
    }

    function startBoardItem(item) {
        if (item.kind === "github" && item.url) {
            startAction(
                "item:" + item.key,
                isPrItem(item) ? "ReviewPR" : "WorkIssue",
                [item.url]
            )
            return
        }
        if (item.task) {
            startAction("item:" + item.key, "StartFromFeed", [item.dir || "", item.task])
            return
        }
        if (item.url) Qt.openUrlExternally(item.url)
    }

    function startKanbanCard(card) {
        if (card.kind === "pr" && card.url) {
            startAction("card:" + card.key, "ReviewPR", [card.url])
            return
        }
        if (card.kind === "issue" && card.url) {
            startAction("card:" + card.key, "WorkIssue", [card.url])
            return
        }
        if (card.kind === "git" && card.task) {
            startAction("card:" + card.key, "StartFromFeed", [card.dir || "", card.task])
            return
        }
        if (card.url) Qt.openUrlExternally(card.url)
    }

    function isPrItem(item) {
        return (item.detail || "").indexOf("PR") === 0
    }

    function laneMatches(idx) {
        var remote = boardModel.data(idx, remoteRole)
        var repo = boardModel.data(idx, repoRole)
        var dir = boardModel.data(idx, dirRole)
        var openPrs = boardModel.data(idx, openPrsRole) || 0
        var openIss = boardModel.data(idx, openIssRole) || 0
        var dirty = boardModel.data(idx, dirtyRole) || 0
        var unpushed = boardModel.data(idx, unpushedRole) || 0
        var behind = boardModel.data(idx, behindRole) || 0

        // Owner filter
        if (ownerFilter !== "all") {
            if (ownerFilter === "local") {
                if (remote) return false
            } else {
                if (!remote || !repo || !repo.startsWith(ownerFilter + "/")) return false
            }
        }

        // State filter
        if (stateFilter === "prs" && openPrs === 0) return false
        if (stateFilter === "issues" && openIss === 0) return false
        if (stateFilter === "dirty" && dirty === 0 && unpushed === 0 && behind === 0) return false

        return true
    }

    function ageClass(hours) {
        if (!hours) return ""
        if (hours >= 168) return "old"
        if (hours >= 48) return "warn"
        return ""
    }

    function ageLabel(hours) {
        if (!hours) return ""
        if (hours >= 48) return Math.round(hours / 24) + "d"
        return hours + "h"
    }

    function cardVerb(kind) {
        if (kind === "pr") return "Review →"
        if (kind === "issue") return "Work →"
        return "Start →"
    }

    function itemVerb(item) {
        if (item.kind === "github") {
            return isPrItem(item) ? "Review →" : "Work →"
        }
        return "Start →"
    }

    property int filteredBoardRows: 0
    readonly property bool boardLoading: !!(boardModel && boardModel.loading)
    readonly property string boardLoadError: boardModel ? String(boardModel.error || "") : ""
    readonly property bool kanbanLoading: !!(kanbanModel && kanbanModel.loading)
    readonly property string kanbanLoadError: kanbanModel ? String(kanbanModel.error || "") : ""
    readonly property int kanbanColumns: (kanbanModel && kanbanModel.columns) ? kanbanModel.columns.length : 0
    readonly property string projectsState: {
        if (viewMode !== "projects") return ""
        if (!boardModel) return "missing"
        if (boardLoading && boardRows === 0) return "loading"
        if (boardLoadError !== "") return "error"
        if (boardRows === 0) return "empty"
        if (filteredBoardRows === 0) return "filtered"
        return ""
    }
    readonly property string kanbanState: {
        if (viewMode !== "kanban") return ""
        if (!kanbanModel) return "missing"
        if (kanbanLoading && kanbanColumns === 0) return "loading"
        if (kanbanLoadError !== "") return "error"
        if (kanbanColumns === 0) return "empty"
        return ""
    }

    onOwnerFilterChanged: syncBoardRows()
    onStateFilterChanged: syncBoardRows()

    function computeFilteredBoardRows(rowCount) {
        if (!boardModel) return 0
        var rows = rowCount === undefined ? boardModel.rowCount() : rowCount
        var visibleRows = 0
        for (var i = 0; i < rows; i++) {
            if (laneMatches(boardModel.index(i, 0))) {
                visibleRows += 1
            }
        }
        return visibleRows
    }

    function stateTitle(state, view) {
        if (state === "loading") return "Loading " + view
        if (state === "error") return "Could not load " + view
        if (state === "filtered") return "No projects match these filters"
        if (state === "missing") return "Board model unavailable"
        return view === "kanban" ? "No kanban cards yet" : "No projects on the board yet"
    }

    function stateDetail(state, view) {
        if (state === "loading") return "Fetching the latest workspace state."
        if (state === "error") return view === "kanban" ? kanbanLoadError : boardLoadError
        if (state === "filtered") return "Try another owner or state filter to bring lanes back into view."
        if (state === "missing") return "Reconnect the desktop shell and refresh."
        return view === "kanban"
            ? "When PRs, issues, or git work appear, cards will be grouped here."
            : "When Eigen finds local or remote work, project lanes will appear here."
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // Header
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 90
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.margins: Theme.space.xl
                spacing: Theme.space.xl

                ColumnLayout {
                    spacing: Theme.space.xs
                    Layout.fillWidth: true

                    Label {
                        text: "Work board"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.h3
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        text: "Every project at a glance — git state, open PRs/issues, loose ends. One place to pick up work."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.textMuted
                        wrapMode: Text.WordWrap
                        Layout.maximumWidth: 600
                    }
                }

                RowLayout {
                    spacing: Theme.space.md

                    // View toggle (Segmented-style)
                    Row {
                        spacing: 2
                        Repeater {
                            model: [
                                { value: "projects", label: "Projects" },
                                { value: "kanban", label: "Kanban" }
                            ]
                            delegate: Rectangle {
                                width: 90
                                height: 32
                                radius: Theme.radius.sm
                                color: modelData.value === viewMode ? Theme.colors.brandBright : "transparent"
                                border.width: 1
                                border.color: modelData.value === viewMode ? Theme.colors.brandBright : Theme.colors.borderSubtle

                                Label {
                                    anchors.centerIn: parent
                                    text: modelData.label
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.medium
                                    color: modelData.value === viewMode ? Theme.colors.bgBase : Theme.colors.textSecondary
                                }

                                MouseArea {
                                    anchors.fill: parent
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: viewMode = modelData.value
                                }
                            }
                        }
                    }

                    AppButton {
                        objectName: "boardRefreshButton"
                        text: "Refresh"
                        compact: true
                        toolTipText: "Refresh board data"
                        onClicked: {
                            if (boardModel) boardModel.load()
                            if (kanbanModel) kanbanModel.load()
                        }
                    }
                }
            }
        }

        Rectangle {
            objectName: "boardActionErrorBanner"
            visible: root.visibleActionError !== ""
            Layout.fillWidth: true
            Layout.preferredHeight: visible ? Math.max(44, boardActionErrorText.implicitHeight + Theme.space.md) : 0
            Layout.leftMargin: Theme.space.xl
            Layout.rightMargin: Theme.space.xl
            Layout.topMargin: Theme.space.sm
            color: Theme.colors.bgRaised
            border.width: 1
            border.color: Theme.colors.error
            radius: Theme.radius.md

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.md
                anchors.rightMargin: Theme.space.sm
                spacing: Theme.space.sm

                Label {
                    objectName: "boardActionErrorText"
                    id: boardActionErrorText
                    text: root.visibleActionError
                    font.pixelSize: Theme.fontSize.label
                    color: Theme.colors.error
                    wrapMode: Text.WordWrap
                    Layout.fillWidth: true
                }

                AppButton {
                    objectName: "boardActionErrorDismissButton"
                    text: "Dismiss"
                    variant: "ghost"
                    compact: true
                    onClicked: root.clearActionError()
                }
            }
        }

        // Filters row (only for projects view)
        Rectangle {
            visible: viewMode === "projects" && boardModel && boardRows > 0
            Layout.fillWidth: true
            Layout.preferredHeight: visible ? 50 : 0
            color: Theme.colors.bgBase

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xl
                anchors.rightMargin: Theme.space.xl
                spacing: Theme.space.xl

                // Owner filter chips
                Row {
                    spacing: Theme.space.xs
                    Repeater {
                        model: ownerOptions
                        delegate: Rectangle {
                            objectName: "boardOwnerFilterChip_" + root.safeObjectName(modelData.value)
                            readonly property bool qaIsBoardChip: true
                            readonly property bool qaTextFits: ownerLabel.implicitWidth <= ownerLabel.width + 1.0
                            readonly property real qaLeftTextInset: ownerLabel.x + Math.max(0, (ownerLabel.width - ownerLabel.paintedWidth) / 2)
                            readonly property real qaRightTextInset: width - (ownerLabel.x + ownerLabel.width / 2 + ownerLabel.paintedWidth / 2)
                            readonly property real qaTopTextInset: ownerLabel.y + Math.max(0, (ownerLabel.height - ownerLabel.paintedHeight) / 2)
                            readonly property real qaBottomTextInset: height - (ownerLabel.y + ownerLabel.height / 2 + ownerLabel.paintedHeight / 2)
                            readonly property real qaHorizontalPadding: Math.min(qaLeftTextInset, qaRightTextInset)
                            readonly property real qaVerticalPadding: Math.min(qaTopTextInset, qaBottomTextInset)

                            width: ownerLabel.implicitWidth + Theme.space.xl * 2
                            height: 30
                            radius: Theme.radius.full
                            color: modelData.value === ownerFilter ? Theme.colors.brandBright : Theme.colors.bgRaised
                            border.width: 1
                            border.color: modelData.value === ownerFilter ? Theme.colors.brandBright : Theme.colors.borderSubtle

                            Label {
                                id: ownerLabel
                                anchors.centerIn: parent
                                text: modelData.label
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                font.weight: Theme.fontWeight.medium
                                color: modelData.value === ownerFilter ? Theme.colors.bgBase : Theme.colors.textSecondary
                            }

                            MouseArea {
                                anchors.fill: parent
                                cursorShape: Qt.PointingHandCursor
                                onClicked: ownerFilter = modelData.value
                            }
                        }
                    }
                }

                // State filter chips
                Row {
                    spacing: Theme.space.xs
                    Repeater {
                        model: stateOptions
                        delegate: Rectangle {
                            objectName: "boardStateFilterChip_" + root.safeObjectName(modelData.value)
                            readonly property bool qaIsBoardChip: true
                            readonly property bool qaTextFits: stateLabel.implicitWidth <= stateLabel.width + 1.0
                            readonly property real qaLeftTextInset: stateLabel.x + Math.max(0, (stateLabel.width - stateLabel.paintedWidth) / 2)
                            readonly property real qaRightTextInset: width - (stateLabel.x + stateLabel.width / 2 + stateLabel.paintedWidth / 2)
                            readonly property real qaTopTextInset: stateLabel.y + Math.max(0, (stateLabel.height - stateLabel.paintedHeight) / 2)
                            readonly property real qaBottomTextInset: height - (stateLabel.y + stateLabel.height / 2 + stateLabel.paintedHeight / 2)
                            readonly property real qaHorizontalPadding: Math.min(qaLeftTextInset, qaRightTextInset)
                            readonly property real qaVerticalPadding: Math.min(qaTopTextInset, qaBottomTextInset)

                            width: stateLabel.implicitWidth + Theme.space.xl * 2
                            height: 30
                            radius: Theme.radius.full
                            color: modelData.value === stateFilter ? Theme.colors.brandBright : Theme.colors.bgRaised
                            border.width: 1
                            border.color: modelData.value === stateFilter ? Theme.colors.brandBright : Theme.colors.borderSubtle

                            Label {
                                id: stateLabel
                                anchors.centerIn: parent
                                text: modelData.label
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                font.weight: Theme.fontWeight.medium
                                color: modelData.value === stateFilter ? Theme.colors.bgBase : Theme.colors.textSecondary
                            }

                            MouseArea {
                                anchors.fill: parent
                                cursorShape: Qt.PointingHandCursor
                                onClicked: stateFilter = modelData.value
                            }
                        }
                    }
                }

                Item { Layout.fillWidth: true }
            }
        }

        // Content area
        Item {
            Layout.fillWidth: true
            Layout.fillHeight: true

            // Projects view (horizontal scroll lanes)
            Flickable {
                visible: viewMode === "projects"
                anchors.fill: parent
                anchors.margins: Theme.space.xl
                contentWidth: lanesRow.implicitWidth
                contentHeight: height
                clip: true

                Row {
                    id: lanesRow
                    spacing: Theme.space.lg
                    height: parent.height

                    Repeater {
                        model: boardModel ? boardRows : 0
                        delegate: Rectangle {
                            readonly property int idx: index
                            visible: laneMatches(boardModel.index(index, 0))
                            width: visible ? 300 : 0
                            height: visible ? parent.height : 0
                            color: Theme.colors.bgRaised
                            border.width: 1
                            border.color: boardModel.data(boardModel.index(index, 0), remoteRole) ? Theme.colors.borderSubtle : Theme.colors.borderHairline
                            radius: Theme.radius.lg

                            ColumnLayout {
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                spacing: Theme.space.md

                                // Lane header
                                RowLayout {
                                    Layout.fillWidth: true
                                    spacing: Theme.space.sm

                                    Label {
                                        objectName: {
                                            var laneKey = boardModel.data(boardModel.index(idx, 0), remoteRole)
                                                ? boardModel.data(boardModel.index(idx, 0), repoRole)
                                                : boardModel.data(boardModel.index(idx, 0), dirRole)
                                            return "boardLaneName_" + root.safeObjectName(laneKey)
                                        }
                                        text: boardModel.data(boardModel.index(idx, 0), nameRole)
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.textPrimary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                        opacity: root.isPending("lane:" + boardModel.data(boardModel.index(idx, 0), dirRole)) ? 0.55 : 1.0

                                        MouseArea {
                                            anchors.fill: parent
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: {
                                                var remote = boardModel.data(boardModel.index(idx, 0), remoteRole)
                                                var url = boardModel.data(boardModel.index(idx, 0), urlRole)
                                                var dir = boardModel.data(boardModel.index(idx, 0), dirRole)
                                                if (remote && url) {
                                                    Qt.openUrlExternally(url)
                                                } else {
                                                    root.startLaneSession(dir)
                                                }
                                            }
                                        }
                                    }

                                    // Branch (local only)
                                    Label {
                                        visible: !boardModel.data(boardModel.index(idx, 0), remoteRole)
                                        text: boardModel.data(boardModel.index(idx, 0), branchRole)
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textFaint
                                        elide: Text.ElideRight
                                    }

                                    // Pin button
                                    Label {
                                        objectName: {
                                            var pinRemote = boardModel.data(boardModel.index(idx, 0), remoteRole)
                                            var pinKey = pinRemote ? boardModel.data(boardModel.index(idx, 0), repoRole) : boardModel.data(boardModel.index(idx, 0), dirRole)
                                            return "boardPinButton_" + root.safeObjectName(pinKey)
                                        }
                                        text: boardModel.data(boardModel.index(idx, 0), pinnedRole) ? "★" : "☆"
                                        font.pixelSize: Theme.fontSize.body
                                        color: boardModel.data(boardModel.index(idx, 0), pinnedRole) ? Theme.colors.warn : Theme.colors.textFaint

                                        MouseArea {
                                            anchors.fill: parent
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: {
                                                var remote = boardModel.data(boardModel.index(idx, 0), remoteRole)
                                                var key = remote ? boardModel.data(boardModel.index(idx, 0), repoRole) : boardModel.data(boardModel.index(idx, 0), dirRole)
                                                boardModel.toggle_pin(key)
                                            }
                                        }
                                    }
                                }

                                // Git stats badges
                                Flow {
                                    Layout.fillWidth: true
                                    spacing: Theme.space.xs

                                    // Uncommitted
                                    BoardBadge {
                                        objectName: "boardDirtyBadge_" + root.safeObjectName(root.laneKeyAt(idx))
                                        visible: boardModel.data(boardModel.index(idx, 0), dirtyRole) > 0
                                        text: "±" + boardModel.data(boardModel.index(idx, 0), dirtyRole)
                                        borderColor: Theme.colors.warn
                                        textColor: Theme.colors.warn
                                    }

                                    // Unpushed
                                    BoardBadge {
                                        objectName: "boardUnpushedBadge_" + root.safeObjectName(root.laneKeyAt(idx))
                                        visible: boardModel.data(boardModel.index(idx, 0), unpushedRole) > 0
                                        text: "↑" + boardModel.data(boardModel.index(idx, 0), unpushedRole)
                                    }

                                    // Behind
                                    BoardBadge {
                                        objectName: "boardBehindBadge_" + root.safeObjectName(root.laneKeyAt(idx))
                                        visible: boardModel.data(boardModel.index(idx, 0), behindRole) > 0
                                        text: "↓" + boardModel.data(boardModel.index(idx, 0), behindRole)
                                    }

                                    // TODOs
                                    BoardBadge {
                                        objectName: "boardTodosBadge_" + root.safeObjectName(root.laneKeyAt(idx))
                                        visible: boardModel.data(boardModel.index(idx, 0), todosRole) > 0
                                        text: "⊙" + boardModel.data(boardModel.index(idx, 0), todosRole)
                                        textColor: Theme.colors.textFaint
                                    }

                                    // Open PRs
                                    BoardBadge {
                                        objectName: "boardPrsBadge_" + root.safeObjectName(root.laneKeyAt(idx))
                                        visible: boardModel.data(boardModel.index(idx, 0), openPrsRole) > 0
                                        text: "PR " + boardModel.data(boardModel.index(idx, 0), openPrsRole)
                                        borderColor: Theme.colors.borderBrandFaint
                                        textColor: Theme.colors.info
                                    }

                                    // Open issues
                                    BoardBadge {
                                        objectName: "boardIssuesBadge_" + root.safeObjectName(root.laneKeyAt(idx))
                                        visible: boardModel.data(boardModel.index(idx, 0), openIssRole) > 0
                                        text: "⊘" + boardModel.data(boardModel.index(idx, 0), openIssRole)
                                        borderColor: Theme.colors.borderBrandFaint
                                        textColor: Theme.colors.info
                                    }

                                    // Clean state (when nothing to show)
                                    Label {
                                        visible: {
                                            var remote = boardModel.data(boardModel.index(idx, 0), remoteRole)
                                            var dirty = boardModel.data(boardModel.index(idx, 0), dirtyRole) || 0
                                            var unpushed = boardModel.data(boardModel.index(idx, 0), unpushedRole) || 0
                                            var behind = boardModel.data(boardModel.index(idx, 0), behindRole) || 0
                                            var prs = boardModel.data(boardModel.index(idx, 0), openPrsRole) || 0
                                            var iss = boardModel.data(boardModel.index(idx, 0), openIssRole) || 0
                                            var items = boardModel.data(boardModel.index(idx, 0), itemsRole) || []

                                            if (remote) return prs === 0 && iss === 0
                                            return dirty === 0 && unpushed === 0 && behind === 0 && items.length === 0
                                        }
                                        text: boardModel.data(boardModel.index(idx, 0), remoteRole) ? "no open work" : "clean"
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        font.weight: Theme.fontWeight.medium
                                        color: Theme.colors.success
                                    }
                                }

                                // Cards (items)
                                Flickable {
                                    Layout.fillWidth: true
                                    Layout.fillHeight: true
                                    contentWidth: width
                                    contentHeight: cardsColumn.implicitHeight
                                    clip: true

                                    ColumnLayout {
                                        id: cardsColumn
                                        width: parent.width
                                        spacing: Theme.space.md

                                        Repeater {
                                            model: boardModel.data(boardModel.index(idx, 0), itemsRole)
                                            delegate: Rectangle {
                                                Layout.fillWidth: true
                                                implicitHeight: cardContent.implicitHeight + Theme.space.lg
                                                color: Theme.colors.bgRaised2
                                                border.width: 1
                                                border.color: Theme.colors.borderHairline
                                                radius: Theme.radius.md

                                                // Left border accent
                                                Rectangle {
                                                    anchors.left: parent.left
                                                    anchors.top: parent.top
                                                    anchors.bottom: parent.bottom
                                                    width: 2
                                                    color: modelData.kind === "github" ? Theme.colors.info : Theme.colors.warn
                                                }

                                                ColumnLayout {
                                                    id: cardContent
                                                    anchors.fill: parent
                                                    anchors.margins: Theme.space.md
                                                    spacing: Theme.space.xs

                                                    RowLayout {
                                                        spacing: Theme.space.xs

                                                        Label {
                                                            text: modelData.kind === "github" ? "◉" : "±"
                                                            font.pixelSize: Theme.fontSize.label
                                                            color: Theme.colors.textMuted
                                                        }

                                                        Label {
                                                            text: modelData.title || ""
                                                            font.family: Theme.uiFonts[0]
                                                            font.pixelSize: Theme.fontSize.label
                                                            font.weight: Theme.fontWeight.medium
                                                            color: Theme.colors.textPrimary
                                                            wrapMode: Text.WordWrap
                                                            Layout.fillWidth: true
                                                        }
                                                    }

                                                    Label {
                                                        visible: !!modelData.detail
                                                        text: modelData.detail || ""
                                                        font.family: Theme.uiFonts[0]
                                                        font.pixelSize: Theme.fontSize.micro
                                                        color: Theme.colors.textMuted
                                                        wrapMode: Text.WordWrap
                                                        Layout.fillWidth: true
                                                    }

                                                    RowLayout {
                                                        Layout.fillWidth: true
                                                        spacing: Theme.space.xs

                                                        Item { Layout.fillWidth: true }

                                                        AppButton {
                                                            objectName: "boardItemOpen_" + root.safeObjectName(modelData.key)
                                                            visible: !!modelData.url
                                                            text: "Open"
                                                            onClicked: Qt.openUrlExternally(modelData.url)
                                                            variant: "ghost"
                                                            compact: true
                                                        }

                                                        AppButton {
                                                            objectName: "boardItemAction_" + root.safeObjectName(modelData.key)
                                                            visible: modelData.kind === "github" || !!modelData.task
                                                            enabled: !root.isPending("item:" + modelData.key)
                                                            text: root.isPending("item:" + modelData.key) ? "Starting..." : root.itemVerb(modelData)
                                                            variant: "secondary"
                                                            compact: true
                                                            onClicked: {
                                                                root.startBoardItem(modelData)
                                                            }
                                                        }
                                                    }
                                                }
                                            }
                                        }

                                        // Empty state
                                        Label {
                                            visible: {
                                                var items = boardModel.data(boardModel.index(idx, 0), itemsRole) || []
                                                var remote = boardModel.data(boardModel.index(idx, 0), remoteRole)
                                                return items.length === 0 && !remote
                                            }
                                            text: "Nothing loose here."
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.label
                                            color: Theme.colors.textGhost
                                            Layout.alignment: Qt.AlignHCenter
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // Kanban view (horizontal scroll columns)
            Flickable {
                visible: viewMode === "kanban"
                anchors.fill: parent
                anchors.margins: Theme.space.xl
                contentWidth: kanbanRow.implicitWidth
                contentHeight: height
                clip: true

                Row {
                    id: kanbanRow
                    spacing: Theme.space.md
                    height: parent.height

                    Repeater {
                        model: kanbanModel ? kanbanModel.columns : []
                        delegate: Rectangle {
                            width: 270
                            height: parent.height
                            color: Theme.colors.bgWell
                            border.width: 1
                            border.color: Theme.colors.borderHairline
                            radius: Theme.radius.lg

                            // Top border accent by column
                            Rectangle {
                                anchors.top: parent.top
                                anchors.left: parent.left
                                anchors.right: parent.right
                                height: 2
                                color: {
                                    var colId = modelData.id
                                    if (colId === "needs-you") return Theme.colors.warn
                                    if (colId === "in-review") return Theme.colors.info
                                    if (colId === "done") return Theme.colors.success
                                    return "transparent"
                                }
                            }

                            ColumnLayout {
                                anchors.fill: parent
                                anchors.margins: Theme.space.md
                                spacing: Theme.space.md

                                // Column header
                                RowLayout {
                                    spacing: Theme.space.sm

                                    Label {
                                        text: modelData.title || ""
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.label
                                        font.weight: Theme.fontWeight.semibold
                                        font.capitalization: Font.AllUppercase
                                        // letterSpacing: 0.8
                                        color: Theme.colors.textFaint
                                    }

                                    Label {
                                        text: (modelData.cards || []).length
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.label
                                        color: {
                                            var count = (modelData.cards || []).length
                                            if (modelData.id === "in-review" && count > 6) return Theme.colors.warn
                                            return Theme.colors.textGhost
                                        }
                                        font.weight: {
                                            var count = (modelData.cards || []).length
                                            if (modelData.id === "in-review" && count > 6) return Theme.fontWeight.bold
                                            return Theme.fontWeight.regular
                                        }
                                    }
                                }

                                // Cards
                                Flickable {
                                    Layout.fillWidth: true
                                    Layout.fillHeight: true
                                    contentWidth: width
                                    contentHeight: kcardsColumn.implicitHeight
                                    clip: true

                                    ColumnLayout {
                                        id: kcardsColumn
                                        width: parent.width
                                        spacing: Theme.space.md

                                        Repeater {
                                            model: modelData.cards
                                            delegate: Rectangle {
                                                Layout.fillWidth: true
                                                implicitHeight: kcardContent.implicitHeight + Theme.space.md
                                                color: Theme.colors.bgRaised
                                                border.width: 1
                                                border.color: Theme.colors.borderHairline
                                                radius: Theme.radius.md

                                                // Left border accent for cards needing attention
                                                Rectangle {
                                                    visible: !!modelData.needsYou
                                                    anchors.left: parent.left
                                                    anchors.top: parent.top
                                                    anchors.bottom: parent.bottom
                                                    width: 2
                                                    color: Theme.colors.warn
                                                }

                                                ColumnLayout {
                                                    id: kcardContent
                                                    anchors.fill: parent
                                                    anchors.margins: Theme.space.md
                                                    spacing: Theme.space.xs

                                                    // Card top (kind + repo + number + age)
                                                    RowLayout {
                                                        spacing: Theme.space.xs

                                                        Label {
                                                            text: {
                                                                if (modelData.kind === "pr") return "PR"
                                                                if (modelData.kind === "issue") return "issue"
                                                                return "git"
                                                            }
                                                            font.family: Theme.uiFonts[0]
                                                            font.pixelSize: Theme.fontSize.micro
                                                            font.weight: Theme.fontWeight.semibold
                                                            font.capitalization: Font.AllUppercase
                                                            color: {
                                                                if (modelData.kind === "pr") return Theme.colors.info
                                                                if (modelData.kind === "issue") return Theme.colors.success
                                                                return Theme.colors.warn
                                                            }
                                                        }

                                                        Label {
                                                            text: modelData.repo || ""
                                                            font.family: Theme.uiFonts[0]
                                                            font.pixelSize: Theme.fontSize.micro
                                                            color: Theme.colors.textMuted
                                                            elide: Text.ElideRight
                                                            Layout.fillWidth: true
                                                        }

                                                        Label {
                                                            visible: !!modelData.number
                                                            text: "#" + (modelData.number || "")
                                                            font.family: Theme.monoFonts[0]
                                                            font.pixelSize: Theme.fontSize.micro
                                                            color: Theme.colors.textFaint
                                                        }

                                                        Label {
                                                            visible: !!modelData.ageHours
                                                            text: ageLabel(modelData.ageHours)
                                                            font.family: Theme.uiFonts[0]
                                                            font.pixelSize: Theme.fontSize.micro
                                                            color: {
                                                                var cls = ageClass(modelData.ageHours)
                                                                if (cls === "old") return Theme.colors.error
                                                                if (cls === "warn") return Theme.colors.warn
                                                                return Theme.colors.textFaint
                                                            }
                                                            font.weight: ageClass(modelData.ageHours) === "old" ? Theme.fontWeight.bold : Theme.fontWeight.regular
                                                        }
                                                    }

                                                    // Card title
                                                    Label {
                                                        text: modelData.title || ""
                                                        font.family: Theme.uiFonts[0]
                                                        font.pixelSize: Theme.fontSize.bodySm
                                                        color: Theme.colors.textPrimary
                                                        wrapMode: Text.WordWrap
                                                        maximumLineCount: 2
                                                        elide: Text.ElideRight
                                                        Layout.fillWidth: true
                                                    }

                                                    // Badges (session, draft, review)
                                                    Flow {
                                                        Layout.fillWidth: true
                                                        spacing: Theme.space.xs

                                                        AppTag {
                                                            objectName: "kanbanSessionTag_" + root.safeObjectName(modelData.key)
                                                            visible: !!modelData.session
                                                            text: "◆ session"
                                                            backgroundColor: Theme.colors.bgRaised2
                                                            borderColor: Theme.colors.borderBrandFaint
                                                            textColor: Theme.colors.brandBright
                                                            fontPixelSize: Theme.fontSize.micro
                                                            fontWeight: Theme.fontWeight.medium
                                                            minimumHeight: 18
                                                        }

                                                        AppTag {
                                                            objectName: "kanbanDraftTag_" + root.safeObjectName(modelData.key)
                                                            visible: !!modelData.draft
                                                            text: "draft"
                                                            backgroundColor: Theme.colors.bgRaised2
                                                            borderColor: Theme.colors.borderSubtle
                                                            textColor: Theme.colors.textMuted
                                                            fontPixelSize: Theme.fontSize.micro
                                                            fontWeight: Theme.fontWeight.medium
                                                            minimumHeight: 18
                                                        }

                                                        AppTag {
                                                            objectName: "kanbanChangesTag_" + root.safeObjectName(modelData.key)
                                                            visible: modelData.review === "changes"
                                                            text: "changes requested"
                                                            backgroundColor: Theme.colors.bgRaised2
                                                            borderColor: Theme.colors.warn
                                                            textColor: Theme.colors.warn
                                                            fontPixelSize: Theme.fontSize.micro
                                                            fontWeight: Theme.fontWeight.medium
                                                            minimumHeight: 18
                                                        }

                                                        AppTag {
                                                            objectName: "kanbanApprovedTag_" + root.safeObjectName(modelData.key)
                                                            visible: modelData.review === "approved"
                                                            text: "approved"
                                                            backgroundColor: Theme.colors.bgRaised2
                                                            borderColor: Theme.colors.success
                                                            textColor: Theme.colors.success
                                                            fontPixelSize: Theme.fontSize.micro
                                                            fontWeight: Theme.fontWeight.medium
                                                            minimumHeight: 18
                                                        }
                                                    }

                                                    // Card actions
                                                    RowLayout {
                                                        spacing: Theme.space.sm

                                                        Item { Layout.fillWidth: true }

                                                        AppButton {
                                                            objectName: "kanbanCardOpen_" + root.safeObjectName(modelData.key)
                                                            visible: !!modelData.url
                                                            text: "Open"
                                                            onClicked: Qt.openUrlExternally(modelData.url)
                                                            variant: "ghost"
                                                            compact: true
                                                        }

                                                        AppButton {
                                                            objectName: "kanbanCardAction_" + root.safeObjectName(modelData.key)
                                                            enabled: !root.isPending("card:" + modelData.key)
                                                            text: root.isPending("card:" + modelData.key) ? "Starting..." : cardVerb(modelData.kind)
                                                            variant: "secondary"
                                                            compact: true
                                                            onClicked: {
                                                                root.startKanbanCard(modelData)
                                                            }
                                                        }
                                                    }
                                                }
                                            }
                                        }

                                        // Empty column state
                                        Label {
                                            visible: (modelData.cards || []).length === 0
                                            text: "—"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.label
                                            color: Theme.colors.textGhost
                                            Layout.alignment: Qt.AlignHCenter
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }

            Column {
                objectName: root.projectsState === "" ? "boardProjectsStatePanel" : "boardProjectsState_" + root.projectsState
                visible: root.projectsState !== ""
                anchors.centerIn: parent
                width: Math.max(240, Math.min(parent.width - Theme.space.xl * 2, 480))
                spacing: Theme.space.md

                Label {
                    objectName: "boardProjectsStateTitle"
                    width: parent.width
                    text: root.stateTitle(root.projectsState, "projects")
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h3
                    font.weight: Theme.fontWeight.semibold
                    color: root.projectsState === "error" ? Theme.colors.error : Theme.colors.textPrimary
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.WordWrap
                }

                Label {
                    objectName: "boardProjectsStateDetail"
                    width: parent.width
                    text: root.stateDetail(root.projectsState, "projects")
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.WordWrap
                }

                AppButton {
                    objectName: "boardProjectsStateAction"
                    visible: !!boardModel && root.projectsState !== "loading"
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: root.projectsState === "filtered" ? "Reset filters" : "Refresh"
                    variant: "secondary"
                    toolTipText: root.projectsState === "filtered" ? "Show all board lanes" : "Refresh board data"
                    onClicked: {
                        if (root.projectsState === "filtered") {
                            root.ownerFilter = "all"
                            root.stateFilter = "all"
                        } else {
                            boardModel.load()
                            if (kanbanModel) kanbanModel.load()
                        }
                    }
                }
            }

            Column {
                objectName: root.kanbanState === "" ? "boardKanbanStatePanel" : "boardKanbanState_" + root.kanbanState
                visible: root.kanbanState !== ""
                anchors.centerIn: parent
                width: Math.max(240, Math.min(parent.width - Theme.space.xl * 2, 480))
                spacing: Theme.space.md

                Label {
                    objectName: "boardKanbanStateTitle"
                    width: parent.width
                    text: root.stateTitle(root.kanbanState, "kanban")
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h3
                    font.weight: Theme.fontWeight.semibold
                    color: root.kanbanState === "error" ? Theme.colors.error : Theme.colors.textPrimary
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.WordWrap
                }

                Label {
                    objectName: "boardKanbanStateDetail"
                    width: parent.width
                    text: root.stateDetail(root.kanbanState, "kanban")
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.WordWrap
                }

                AppButton {
                    objectName: "boardKanbanStateAction"
                    visible: !!kanbanModel && root.kanbanState !== "loading"
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "Refresh"
                    variant: "secondary"
                    toolTipText: "Refresh kanban data"
                    onClicked: {
                        if (kanbanModel) kanbanModel.load()
                    }
                }
            }
        }
    }
}
