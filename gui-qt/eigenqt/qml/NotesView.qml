import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Notes view — Obsidian vault browser. Left: search + note list. Right: viewer/editor.
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property var notesController: null  // NotesController from Python (context property)

    // Empty state when vault is not available
    Rectangle {
        id: emptyState
        visible: root.notesController && !root.notesController.available
        anchors.fill: parent
        color: Theme.colors.bgBase

        ColumnLayout {
            anchors.centerIn: parent
            spacing: Theme.space.lg

            Label {
                text: "≣"
                font.family: Theme.uiFonts[0]
                font.pixelSize: 48
                color: Theme.colors.textGhost
                Layout.alignment: Qt.AlignHCenter
            }

            Label {
                text: "No Obsidian vault"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.h2
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textPrimary
                Layout.alignment: Qt.AlignHCenter
            }

            Label {
                text: "Set a vault in Connectors → Obsidian (Choose vault), then notes show here."
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textMuted
                wrapMode: Text.WordWrap
                maximumLineCount: 2
                Layout.alignment: Qt.AlignHCenter
                Layout.maximumWidth: 420
                horizontalAlignment: Text.AlignHCenter
            }
        }
    }

    // Main split view (list + pane)
    RowLayout {
        visible: root.notesController && root.notesController.available
        anchors.fill: parent
        spacing: 0

        // LEFT: note list
        Rectangle {
            Layout.preferredWidth: 300
            Layout.fillHeight: true
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.divider

            ColumnLayout {
                anchors.fill: parent
                spacing: 0

                // Search header
                RowLayout {
                    Layout.fillWidth: true
                    Layout.margins: Theme.space.lg
                    Layout.bottomMargin: Theme.space.sm
                    spacing: Theme.space.sm

                    TextField {
                        id: searchField
                        Layout.fillWidth: true
                        placeholderText: "Search notes…"
                        onTextChanged: {
                            searchTimer.restart()
                        }

                        background: Rectangle {
                            color: Theme.colors.bgRaised2
                            border.width: 1
                            border.color: (searchField && searchField.activeFocus) ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                            radius: Theme.radius.sm

                            Behavior on border.color { ColorAnimation { duration: Theme.duration.fast } }
                        }

                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textPrimary
                        leftPadding: Theme.space.lg
                        rightPadding: Theme.space.lg

                        // Debounce search
                        Timer {
                            id: searchTimer
                            interval: 250
                            onTriggered: {
                                if (root.notesController) {
                                    root.notesController.search(searchField.text.trim())
                                }
                            }
                        }
                    }

                    Button {
                        text: "New"
                        onClicked: {
                            if (root.notesController) {
                                root.notesController.start_create()
                            }
                        }

                        background: Rectangle {
                            color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                            radius: Theme.radius.sm
                            border.width: 1
                            border.color: Theme.colors.borderSubtle
                            Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
                        }

                        contentItem: Label {
                            text: parent.text
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textSecondary
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                        }

                        Layout.preferredHeight: 32
                        Layout.preferredWidth: 56
                    }
                }

                // Inline create composer
                RowLayout {
                    visible: root.notesController && root.notesController.creating
                    Layout.fillWidth: true
                    Layout.leftMargin: Theme.space.lg
                    Layout.rightMargin: Theme.space.lg
                    Layout.bottomMargin: Theme.space.sm
                    spacing: Theme.space.sm

                    TextField {
                        id: createField
                        Layout.fillWidth: true
                        placeholderText: "Inbox/Idea.md"
                        text: root.notesController ? root.notesController.new_name : ""
                        onTextChanged: {
                            if (root.notesController) {
                                root.notesController.new_name = text
                            }
                        }

                        Keys.onReturnPressed: {
                            if (root.notesController && root.notesController.new_name.trim() !== "") {
                                root.notesController.create_note()
                            }
                        }

                        Keys.onEscapePressed: {
                            if (root.notesController) {
                                root.notesController.cancel_create()
                            }
                        }

                        background: Rectangle {
                            color: Theme.colors.bgRaised2
                            border.width: 1
                            border.color: (createField && createField.activeFocus) ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                            radius: Theme.radius.sm
                            Behavior on border.color { ColorAnimation { duration: Theme.duration.fast } }
                        }

                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textPrimary
                        leftPadding: Theme.space.lg
                        rightPadding: Theme.space.lg

                        Component.onCompleted: {
                            if (visible) {
                                forceActiveFocus()
                            }
                        }

                        onVisibleChanged: {
                            if (visible) {
                                forceActiveFocus()
                            }
                        }
                    }

                    Button {
                        text: "Create"
                        onClicked: {
                            if (root.notesController && root.notesController.new_name.trim() !== "") {
                                root.notesController.create_note()
                            }
                        }

                        background: Rectangle {
                            color: parent.down ? Theme.colors.brandStrong : (parent.hovered ? Theme.colors.brand : Theme.colors.brandDim)
                            radius: Theme.radius.sm
                            Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
                        }

                        contentItem: Label {
                            text: parent.text
                            font.pixelSize: Theme.fontSize.bodySm
                            font.weight: Theme.fontWeight.medium
                            color: Theme.colors.textPrimary
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                        }

                        Layout.preferredHeight: 32
                    }

                    Button {
                        text: "Cancel"
                        onClicked: {
                            if (root.notesController) {
                                root.notesController.cancel_create()
                            }
                        }

                        background: Rectangle {
                            color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                            radius: Theme.radius.sm
                            border.width: 1
                            border.color: Theme.colors.borderSubtle
                            Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
                        }

                        contentItem: Label {
                            text: parent.text
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textSecondary
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                        }

                        Layout.preferredHeight: 32
                    }
                }

                // Note list
                ListView {
                    id: notesList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    model: root.notesController ? root.notesController.notes_model : null

                    ScrollBar.vertical: ScrollBar {
                        policy: ScrollBar.AsNeeded
                    }

                    delegate: Rectangle {
                        width: notesList.width
                        implicitHeight: 48
                        color: {
                            if (root.notesController && root.notesController.selected &&
                                root.notesController.selected.path === model.path) {
                                return Theme.colors.stateSelected
                            }
                            return mouseArea.containsMouse ? Theme.colors.stateHover : "transparent"
                        }

                        Behavior on color { ColorAnimation { duration: Theme.duration.fast } }

                        MouseArea {
                            id: mouseArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                if (root.notesController) {
                                    root.notesController.open_note(model.path, model.title)
                                }
                            }
                        }

                        ColumnLayout {
                            anchors.fill: parent
                            anchors.leftMargin: Theme.space.lg
                            anchors.rightMargin: Theme.space.lg
                            anchors.topMargin: Theme.space.sm
                            anchors.bottomMargin: Theme.space.sm
                            spacing: 1

                            Label {
                                text: model.title
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                font.weight: Theme.fontWeight.medium
                                color: Theme.colors.textPrimary
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }

                            Label {
                                text: model.path
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.micro
                                color: Theme.colors.textFaint
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                        }
                    }

                    // Empty state (loading or no notes)
                    Label {
                        visible: notesList.count === 0 && (!root.notesController || !root.notesController.notes_model.loading)
                        anchors.centerIn: parent
                        text: {
                            if (!root.notesController) return ""
                            if (searchField.text.trim() !== "") {
                                return "No notes match."
                            }
                            return "No notes."
                        }
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textGhost
                    }

                    // Loading state
                    Label {
                        visible: root.notesController && root.notesController.notes_model.loading
                        anchors.centerIn: parent
                        text: "Loading…"
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textGhost
                    }
                }
            }
        }

        // RIGHT: note pane
        Rectangle {
            Layout.fillWidth: true
            Layout.fillHeight: true
            color: Theme.colors.bgBase

            // Placeholder (no note selected)
            Label {
                visible: !root.notesController || !root.notesController.selected
                anchors.centerIn: parent
                text: "Pick a note, or create one."
                font.pixelSize: Theme.fontSize.body
                color: Theme.colors.textGhost
            }

            // Note content
            ColumnLayout {
                visible: !!(root.notesController && root.notesController.selected)
                anchors.fill: parent
                spacing: 0

                // Header
                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredHeight: 64
                    color: Theme.colors.bgBase
                    border.width: 1
                    border.color: Theme.colors.divider

                    RowLayout {
                        anchors.fill: parent
                        anchors.margins: Theme.space.lg
                        spacing: Theme.space.lg

                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 2

                            Label {
                                text: root.notesController && root.notesController.selected ? root.notesController.selected.title : ""
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.body
                                font.weight: Theme.fontWeight.semibold
                                color: Theme.colors.textPrimary
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }

                            Label {
                                text: root.notesController && root.notesController.selected ? root.notesController.selected.path : ""
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.micro
                                color: Theme.colors.textFaint
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                        }

                        Item { Layout.fillWidth: true }

                        // Action buttons
                        RowLayout {
                            spacing: Theme.space.sm

                            // Edit mode buttons
                            Button {
                                visible: root.notesController && root.notesController.editing
                                text: root.notesController && root.notesController.saving ? "Saving…" : "Save"
                                enabled: root.notesController && !root.notesController.saving
                                onClicked: {
                                    if (root.notesController) {
                                        root.notesController.save()
                                    }
                                }

                                background: Rectangle {
                                    color: parent.down ? Theme.colors.brandStrong : (parent.hovered ? Theme.colors.brand : Theme.colors.brandDim)
                                    radius: Theme.radius.sm
                                    opacity: parent.enabled ? 1.0 : 0.6
                                    Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
                                }

                                contentItem: Label {
                                    text: parent.text
                                    font.pixelSize: Theme.fontSize.bodySm
                                    font.weight: Theme.fontWeight.medium
                                    color: Theme.colors.textPrimary
                                    horizontalAlignment: Text.AlignHCenter
                                    verticalAlignment: Text.AlignVCenter
                                }

                                Layout.preferredHeight: 32
                            }

                            Button {
                                visible: root.notesController && root.notesController.editing
                                text: "Cancel"
                                onClicked: {
                                    if (root.notesController) {
                                        root.notesController.cancel_edit()
                                    }
                                }

                                background: Rectangle {
                                    color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                                    radius: Theme.radius.sm
                                    border.width: 1
                                    border.color: Theme.colors.borderSubtle
                                    Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
                                }

                                contentItem: Label {
                                    text: parent.text
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textSecondary
                                    horizontalAlignment: Text.AlignHCenter
                                    verticalAlignment: Text.AlignVCenter
                                }

                                Layout.preferredHeight: 32
                            }

                            // Read mode button
                            Button {
                                visible: root.notesController && !root.notesController.editing
                                text: "Edit"
                                onClicked: {
                                    if (root.notesController) {
                                        root.notesController.start_edit()
                                    }
                                }

                                background: Rectangle {
                                    color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                                    radius: Theme.radius.sm
                                    border.width: 1
                                    border.color: Theme.colors.borderSubtle
                                    Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
                                }

                                contentItem: Label {
                                    text: parent.text
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textSecondary
                                    horizontalAlignment: Text.AlignHCenter
                                    verticalAlignment: Text.AlignVCenter
                                }

                                Layout.preferredHeight: 32
                            }
                        }
                    }
                }

                // Content area (read or edit)
                Item {
                    Layout.fillWidth: true
                    Layout.fillHeight: true

                    // Edit mode: textarea
                    ScrollView {
                        visible: root.notesController && root.notesController.editing
                        anchors.fill: parent
                        clip: true

                        TextArea {
                            id: editor
                            text: root.notesController ? root.notesController.draft : ""
                            onTextChanged: {
                                if (root.notesController && root.notesController.editing) {
                                    root.notesController.draft = text
                                }
                            }

                            background: Rectangle {
                                color: Theme.colors.bgBase
                            }

                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textPrimary
                            wrapMode: TextEdit.Wrap
                            selectByMouse: true
                            selectByKeyboard: true

                            leftPadding: Theme.space.xxxl
                            rightPadding: Theme.space.xxxl
                            topPadding: Theme.space.xxxl
                            bottomPadding: Theme.space.xxxl
                        }
                    }

                    // Read mode: markdown-rendered text (simplified — just plain text for now)
                    ScrollView {
                        visible: root.notesController && !root.notesController.editing
                        anchors.fill: parent
                        clip: true

                        Label {
                            width: parent.width - Theme.space.xxxl * 2
                            text: root.notesController ? root.notesController.content : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textPrimary
                            wrapMode: Text.Wrap
                            textFormat: Text.MarkdownText
                            leftPadding: Theme.space.xxxl
                            rightPadding: Theme.space.xxxl
                            topPadding: Theme.space.xxxl
                            bottomPadding: Theme.space.xxxl
                        }
                    }
                }
            }
        }
    }
}
