import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Global keyboard launcher for actions, views, and recent sessions.
Popup {
    id: root
    objectName: "commandPalette"
    modal: true
    dim: true
    focus: true
    padding: 0
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside

    property var sessionsModel: null
    property string queryText: ""
    property int sessionsEpoch: 0
    property int currentIndex: 0
    property var filteredEntries: []

    readonly property int qaEntryCount: commandList.count
    readonly property int qaCurrentIndex: currentIndex
    readonly property string qaSelectedLabel: selectedEntry() ? String(selectedEntry().label || "") : ""
    readonly property bool qaInputFocused: paletteInput.activeFocus
    readonly property bool qaInsideWindow: !opened || (x >= -0.5 && y >= -0.5
        && x + width <= parent.width + 0.5 && y + height <= parent.height + 0.5)

    signal routeRequested(string route)
    signal sessionRequested(string sessionId)
    signal newSessionRequested()
    signal pruneRequested()
    signal refreshFeedRequested()

    width: Math.max(280, Math.min(620, (parent ? parent.width : 620) - Theme.space.xxxl * 2))
    height: Math.min(
        Math.max(156, paletteInput.implicitHeight + commandList.contentHeight + Theme.space.md * 2),
        Math.max(156, Math.min(
            (parent ? parent.height : 640) - Theme.space.xxxl * 2,
            Math.round((parent ? parent.height : 640) * 0.64)
        ))
    )
    x: Math.max(Theme.space.lg, Math.min(
        (parent ? parent.width : width) - width - Theme.space.lg,
        Math.round(((parent ? parent.width : width) - width) / 2)
    ))
    y: Math.max(Theme.space.lg, Math.min(
        (parent ? parent.height : height) - height - Theme.space.lg,
        Math.round((parent ? parent.height : height) * 0.12)
    ))

    background: Rectangle {
        color: Theme.colors.surfaceOverlay
        radius: Theme.radius.lg
        border.width: 1
        border.color: Theme.colors.borderStrong
    }

    contentItem: ColumnLayout {
        spacing: 0

        AppTextField {
            id: paletteInput
            objectName: "commandPaletteInput"
            Layout.fillWidth: true
            Layout.preferredHeight: 44
            backgroundColor: "transparent"
            borderColor: "transparent"
            focusBorderColor: "transparent"
            focus: root.opened
            placeholderText: "Search actions, views, and sessions"
            Accessible.name: "Command palette search"
            text: root.queryText
            onTextEdited: root.queryText = text
            Keys.onPressed: function(event) { root.handleKey(event) }

            Rectangle {
                anchors.bottom: parent.bottom
                width: parent.width
                height: 1
                color: Theme.colors.borderHairline
            }
        }

        ListView {
            id: commandList
            objectName: "commandPaletteList"
            Layout.fillWidth: true
            Layout.fillHeight: true
            Layout.minimumHeight: 112
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            model: root.filteredEntries
            currentIndex: root.currentIndex
            keyNavigationEnabled: false
            onCountChanged: root.clampCurrentIndex()

            delegate: Item {
                readonly property var entry: modelData || ({})
                readonly property bool showGroup: !!entry.showGroup
                width: commandList.width
                height: showGroup ? 70 : 48

                Label {
                    visible: parent.showGroup
                    anchors.left: parent.left
                    anchors.right: parent.right
                    anchors.leftMargin: Theme.space.lg
                    anchors.rightMargin: Theme.space.lg
                    anchors.top: parent.top
                    anchors.topMargin: Theme.space.md
                    text: parent.entry.group || ""
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    font.weight: Theme.fontWeight.semibold
                    font.capitalization: Font.AllUppercase
                    color: Theme.colors.textFaint
                    elide: Text.ElideRight
                }

                ItemDelegate {
                    id: option
                    objectName: "commandPaletteOption_" + root.safeObjectName(entry.id || entry.label)
                    anchors.left: parent.left
                    anchors.right: parent.right
                    anchors.top: parent.top
                    anchors.topMargin: parent.showGroup ? 22 : 0
                    height: 48
                    leftPadding: Theme.space.lg
                    rightPadding: Theme.space.lg
                    topPadding: Theme.space.xs
                    bottomPadding: Theme.space.xs
                    highlighted: commandList.currentIndex === index
                    enabled: entry.enabled !== false
                    Accessible.role: Accessible.MenuItem
                    Accessible.name: String(entry.label || "")
                    Accessible.description: String(entry.hint || "")

                    background: Rectangle {
                        color: option.highlighted ? Theme.colors.stateSelected
                            : (option.hovered ? Theme.colors.stateHover : "transparent")
                        radius: Theme.radius.sm
                        border.width: option.highlighted ? 1 : 0
                        border.color: Theme.colors.borderBrandFaint
                    }

                    contentItem: RowLayout {
                        spacing: Theme.space.lg

                        Label {
                            text: String(entry.label || "")
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            font.weight: Theme.fontWeight.medium
                            color: option.enabled ? Theme.colors.textPrimary : Theme.colors.textFaint
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                        }

                        Label {
                            text: String(entry.hint || "")
                            visible: text !== ""
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            color: Theme.colors.textMuted
                            elide: Text.ElideRight
                            Layout.maximumWidth: Math.max(84, commandList.width * 0.36)
                        }
                    }

                    onHoveredChanged: {
                        if (hovered) root.currentIndex = index
                    }
                    onClicked: root.activate(index)
                }
            }

            Label {
                objectName: "commandPaletteEmpty"
                visible: commandList.count === 0
                anchors.centerIn: parent
                width: Math.max(0, parent.width - Theme.space.xxxl)
                text: "No matching command"
                horizontalAlignment: Text.AlignHCenter
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textMuted
                elide: Text.ElideRight
            }
        }
    }

    Connections {
        target: root.sessionsModel ? root.sessionsModel : null
        ignoreUnknownSignals: true

        function onSessionEntriesChanged() { root.sessionsEpoch += 1 }
        function onModelReset() { root.sessionsEpoch += 1 }
        function onRowsInserted() { root.sessionsEpoch += 1 }
        function onRowsRemoved() { root.sessionsEpoch += 1 }
        function onDataChanged() { root.sessionsEpoch += 1 }
    }

    onSessionsModelChanged: refreshEntries()
    onSessionsEpochChanged: refreshEntries()
    onQueryTextChanged: refreshEntries()
    onOpened: {
        queryText = ""
        currentIndex = 0
        refreshEntries()
        Qt.callLater(function() { paletteInput.forceActiveFocus(Qt.ShortcutFocusReason) })
    }

    function showPalette() {
        if (opened) {
            close()
            return
        }
        open()
    }

    function handleKey(event) {
        if (event.key === Qt.Key_Down) {
            moveSelection(1)
            event.accepted = true
        } else if (event.key === Qt.Key_Up) {
            moveSelection(-1)
            event.accepted = true
        } else if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
            activate(currentIndex)
            event.accepted = true
        } else if (event.key === Qt.Key_Escape) {
            close()
            event.accepted = true
        }
    }

    function moveSelection(delta) {
        if (commandList.count <= 0) return
        currentIndex = Math.max(0, Math.min(commandList.count - 1, currentIndex + delta))
        commandList.positionViewAtIndex(currentIndex, ListView.Contain)
    }

    function clampCurrentIndex() {
        if (commandList.count <= 0) {
            currentIndex = 0
            return
        }
        currentIndex = Math.max(0, Math.min(commandList.count - 1, currentIndex))
    }

    function selectedEntry() {
        if (currentIndex < 0 || currentIndex >= filteredEntries.length) return null
        return filteredEntries[currentIndex]
    }

    function activate(index) {
        if (index < 0 || index >= filteredEntries.length) return
        var entry = filteredEntries[index]
        if (!entry || entry.enabled === false) return
        close()
        if (entry.kind === "route") routeRequested(String(entry.route || "home"))
        else if (entry.kind === "session") sessionRequested(String(entry.sessionId || ""))
        else if (entry.kind === "new") newSessionRequested()
        else if (entry.kind === "prune") pruneRequested()
        else if (entry.kind === "feed") refreshFeedRequested()
    }

    function refreshEntries() {
        filteredEntries = buildFilteredEntries(queryText)
        clampCurrentIndex()
    }

    function buildFilteredEntries(query) {
        var entries = [
            {
                kind: "new", group: "Actions", label: "Start a session", hint: "new chat",
                keywords: "create new chat session", id: "action-new", enabled: true
            },
            {
                kind: "prune", group: "Actions", label: "Prune empty sessions", hint: "cleanup",
                keywords: "remove delete clean blank sessions", id: "action-prune",
                enabled: !!sessionsModel && !sessionsModel.pruning
            },
            {
                kind: "feed", group: "Actions", label: "Refresh feed", hint: "home",
                keywords: "rescan refresh projects", id: "action-feed", enabled: true
            }
        ]
        var routes = [
            ["home", "Home"], ["sessions", "Sessions"], ["live", "Live"], ["board", "Board"],
            ["tasks", "Tasks"], ["skills", "Skills"], ["memory", "Memory"], ["notes", "Notes"],
            ["dreaming", "Dreaming"], ["observe", "Observe"], ["routing", "Routing"],
            ["machines", "Machines"], ["crons", "Crons"], ["plugins", "Plugins"],
            ["connectors", "Connectors"], ["profile", "Profile"], ["config", "Config"],
            ["reviewers", "Reviewers"]
        ]
        for (var routeIndex = 0; routeIndex < routes.length; routeIndex++) {
            var route = routes[routeIndex]
            entries.push({
                kind: "route", group: "Views", label: route[1], hint: "view", keywords: route[0],
                route: route[0], id: "route-" + route[0], enabled: true
            })
        }

        var sessions = sessionsModel && sessionsModel.session_entries ? sessionsModel.session_entries : []
        for (var sessionIndex = 0; sessionIndex < sessions.length; sessionIndex++) {
            var session = sessions[sessionIndex] || ({})
            var sessionId = String(session.id || "")
            if (sessionId === "") continue
            var title = String(session.title || "").trim()
            var dir = String(session.dir || "")
            var fallback = dir.replace(/\/+$/, "").split("/").pop() || "untitled session"
            entries.push({
                kind: "session", group: "Sessions", label: title || fallback,
                hint: String(session.model || "session"),
                keywords: sessionId + " " + dir + " " + String(session.status || ""),
                sessionId: sessionId, id: "session-" + sessionId, enabled: true
            })
        }

        var needle = String(query || "").trim().toLowerCase()
        var ranked = []
        for (var entryIndex = 0; entryIndex < entries.length; entryIndex++) {
            var entry = entries[entryIndex]
            var score = needle === "" ? 0 : fuzzyScore(needle, entry.label + " " + entry.hint + " " + entry.keywords)
            if (score >= 0) ranked.push({ entry: entry, score: score, order: entryIndex })
        }
        if (needle !== "") {
            var groups = { "Actions": 0, "Views": 1, "Sessions": 2 }
            ranked.sort(function(a, b) {
                if (b.score !== a.score) return b.score - a.score
                var groupOrder = groups[a.entry.group] - groups[b.entry.group]
                return groupOrder !== 0 ? groupOrder : a.order - b.order
            })
        }
        var output = []
        var previousGroup = ""
        for (var rankedIndex = 0; rankedIndex < ranked.length; rankedIndex++) {
            var value = ranked[rankedIndex].entry
            value.showGroup = value.group !== previousGroup
            previousGroup = value.group
            output.push(value)
        }
        return output
    }

    function fuzzyScore(needle, haystack) {
        var text = String(haystack || "").toLowerCase()
        var next = 0
        var score = 0
        var run = 0
        var previous = -2
        for (var i = 0; i < needle.length; i++) {
            var at = text.indexOf(needle.charAt(i), next)
            if (at < 0) return -1
            run = at === previous + 1 ? run + 1 : 0
            score += 1 + run * 2 + ((at === 0 || text.charAt(at - 1) === " " || text.charAt(at - 1) === "-") ? 3 : 0)
            previous = at
            next = at + 1
        }
        return score
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]/g, "_")
    }
}
