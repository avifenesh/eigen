import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Slash-command popup (shows on "/" at composer start, filterable list)
Popup {
    id: root
    objectName: "slashCommandPopup"
    modal: false
    focus: false
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
    margins: Theme.space.lg

    property var commandsModel  // CommandsModel instance
    property string filterText: ""
    property var filteredCommands: []
    readonly property int qaCurrentIndex: commandListView.currentIndex
    readonly property int qaCommandCount: commandListView.count
    readonly property string qaSelectedCommand: selectedCommandName()
    readonly property bool qaPopupInsideWindow: insideWindow()

    signal commandSelected(string commandName)

    width: 400
    height: Math.min(Math.max(commandListView.contentHeight, 48) + Theme.space.md * 2, 300)

    onCommandsModelChanged: refreshFilteredCommands()

    background: Rectangle {
        color: Theme.colors.surfaceRaised
        radius: Theme.radius.md
        border.width: 1
        border.color: Theme.colors.borderSubtle
    }

    // Filter update handler (replaces hidden TextField listener)
    onFilterTextChanged: {
        commandListView.currentIndex = -1
        refreshFilteredCommands()
    }

    contentItem: ColumnLayout {
        spacing: 0

        // Command list
        ListView {
            id: commandListView
            objectName: "slashCommandList"
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            focus: false
            keyNavigationEnabled: false
            boundsBehavior: Flickable.StopAtBounds

            model: root.filteredCommands
            onCountChanged: root.clampCurrentIndex()

            delegate: ItemDelegate {
                readonly property string commandName: modelData && modelData.name ? String(modelData.name) : ""
                readonly property string commandDescription: modelData && modelData.description ? String(modelData.description) : ""

                objectName: commandName ? ("slashCommandOption_" + root.safeObjectName(commandName)) : ""
                width: commandListView.width
                height: 48

                background: Rectangle {
                    color: parent.highlighted || parent.hovered ? Theme.colors.stateHover : "transparent"
                }

                contentItem: ColumnLayout {
                    anchors.fill: parent
                    anchors.margins: Theme.space.md
                    spacing: Theme.space.xs

                    Label {
                        text: "/" + commandName
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                        Layout.fillWidth: true
                        elide: Text.ElideRight
                    }

                    Label {
                        text: commandDescription
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        color: Theme.colors.textMuted
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }
                readonly property bool qaTextFits: !contentItem || root.textFits(contentItem)
                readonly property string qaText: "/" + commandName

                onClicked: {
                    root.commandSelected(commandName)
                    root.close()
                }
            }

            // Highlight first item by default
            highlightMoveDuration: 0
            highlight: Rectangle {
                color: Theme.colors.stateHover
            }
            currentIndex: -1
        }
    }

    onOpened: {
        refreshFilteredCommands()
        clampCurrentIndex()
    }

    function moveSelection(delta) {
        if (commandListView.count <= 0) return
        commandListView.currentIndex = Math.max(0, Math.min(commandListView.count - 1, commandListView.currentIndex + delta))
        commandListView.positionViewAtIndex(commandListView.currentIndex, ListView.Contain)
    }

    function acceptSelection() {
        var cmdName = selectedCommandName()
        if (!cmdName) return false
        root.commandSelected(cmdName)
        root.close()
        return true
    }

    function hasSelection() {
        return !!selectedCommandName()
    }

    function selectedCommandName() {
        if (commandListView.currentIndex < 0 || commandListView.currentIndex >= root.filteredCommands.length) {
            return ""
        }
        var command = root.filteredCommands[commandListView.currentIndex]
        return command && command.name ? String(command.name) : ""
    }

    function refreshFilteredCommands() {
        if (!root.commandsModel || typeof root.commandsModel.filteredCommands !== "function") {
            root.filteredCommands = []
            return
        }
        root.filteredCommands = root.commandsModel.filteredCommands(root.filterText)
        clampCurrentIndex()
    }

    function clampCurrentIndex() {
        if (commandListView.count <= 0) {
            commandListView.currentIndex = -1
            return
        }
        if (commandListView.currentIndex < 0 || commandListView.currentIndex >= commandListView.count) {
            commandListView.currentIndex = 0
        }
    }

    function insideWindow() {
        if (!root.opened || !parent) return true
        return root.x >= -0.5
            && root.y >= -0.5
            && root.x + root.width <= parent.width + 0.5
            && root.y + root.height <= parent.height + 0.5
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]/g, "_")
    }

    function textFits(item) {
        if (!item || item.visible === false) return true
        if (item.text !== undefined && item.text !== null && String(item.text).length > 0
                && item.paintedWidth !== undefined && item.width !== undefined) {
            if (item.truncated) return false
            if (item.paintedWidth > Math.max(0, item.width) + 0.5) return false
        }
        if (item.children !== undefined && item.children !== null) {
            for (var i = 0; i < item.children.length; i++) {
                if (!textFits(item.children[i])) return false
            }
        }
        return true
    }
}
