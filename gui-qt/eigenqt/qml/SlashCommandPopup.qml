import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Slash-command popup (shows on "/" at composer start, filterable list)
Popup {
    id: root
    modal: false
    focus: true
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside

    property var commandsModel  // CommandsModel instance
    property string filterText: ""

    signal commandSelected(string commandName)

    width: 400
    height: Math.min(commandListView.contentHeight + Theme.space.md * 2, 300)

    background: Rectangle {
        color: Theme.colors.surfaceRaised
        radius: Theme.radius.md
        border.width: 1
        border.color: Theme.colors.borderSubtle
    }

    contentItem: ColumnLayout {
        spacing: 0

        // Filter text (hidden input, controlled externally)
        TextField {
            id: filterField
            visible: false
            text: root.filterText
            onTextChanged: {
                if (root.commandsModel) {
                    root.commandsModel.setFilter(text)
                }
            }
        }

        // Command list
        ListView {
            id: commandListView
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true

            model: root.commandsModel

            delegate: ItemDelegate {
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
                        text: "/" + model.name
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                        Layout.fillWidth: true
                    }

                    Label {
                        text: model.description
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        color: Theme.colors.textMuted
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }

                onClicked: {
                    root.commandSelected(model.name)
                    root.close()
                }
            }

            // Highlight first item by default
            highlightMoveDuration: 0
            highlight: Rectangle {
                color: Theme.colors.stateHover
            }
            currentIndex: 0

            // Keyboard navigation
            Keys.onUpPressed: {
                if (currentIndex > 0) currentIndex--
            }
            Keys.onDownPressed: {
                if (currentIndex < count - 1) currentIndex++
            }
            Keys.onReturnPressed: {
                if (currentIndex >= 0) {
                    var item = itemAtIndex(currentIndex)
                    if (item) item.clicked()
                }
            }
        }
    }

    // Auto-focus on open
    onOpened: {
        commandListView.forceActiveFocus()
    }
}
