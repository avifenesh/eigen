import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Notes view — Obsidian vault browser. Left: search + note list. Right: viewer/editor.
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property var notesController: null  // NotesController from Python (context property)
    readonly property string readContent: root.notesController ? root.notesController.content : ""
    readonly property var readBlocks: parseMarkdown(readContent)

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

                    AppButton {
                        objectName: "notesNewButton"
                        text: "New"
                        toolTipText: "Create a note"
                        onClicked: {
                            if (root.notesController) {
                                root.notesController.start_create()
                            }
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
                        objectName: "notesCreateNameInput"
                        Layout.fillWidth: true
                        placeholderText: "Inbox/Idea.md"
                        text: root.notesController ? root.notesController.new_name : ""
                        enabled: !(root.notesController && root.notesController.creating_busy)
                        property bool qaForceKeyboardFocus: false
                        readonly property bool qaVisualFocus: activeFocus
                        readonly property bool qaTextFits: !createField.contentItem || !createField.contentItem.text
                            || (!createField.contentItem.truncated
                                && createField.contentItem.paintedWidth <= createField.contentItem.width + 0.5)
                        readonly property string qaText: text || placeholderText
                        onTextChanged: {
                            if (root.notesController) {
                                root.notesController.new_name = text
                            }
                        }

                        onQaForceKeyboardFocusChanged: {
                            if (qaForceKeyboardFocus) {
                                forceActiveFocus(Qt.TabFocusReason)
                            }
                        }

                        Keys.onReturnPressed: {
                            if (root.notesController && !root.notesController.creating_busy && root.notesController.new_name.trim() !== "") {
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

                    AppButton {
                        objectName: "notesCreateButton"
                        text: root.notesController && root.notesController.creating_busy ? "Creating" : "Create"
                        variant: "primary"
                        toolTipText: "Create note"
                        enabled: root.notesController && !root.notesController.creating_busy && root.notesController.new_name.trim() !== ""
                        onClicked: {
                            if (root.notesController && !root.notesController.creating_busy && root.notesController.new_name.trim() !== "") {
                                root.notesController.create_note()
                            }
                        }

                        Layout.preferredHeight: 32
                    }

                    AppButton {
                        objectName: "notesCancelCreateButton"
                        text: "Cancel"
                        toolTipText: "Cancel note creation"
                        enabled: !(root.notesController && root.notesController.creating_busy)
                        onClicked: {
                            if (root.notesController) {
                                root.notesController.cancel_create()
                            }
                        }

                        Layout.preferredHeight: 32
                    }
                }

                Rectangle {
                    objectName: "notesActionError"
                    visible: root.notesController && root.notesController.action_error !== ""
                    Layout.fillWidth: true
                    Layout.leftMargin: Theme.space.lg
                    Layout.rightMargin: Theme.space.lg
                    Layout.bottomMargin: visible ? Theme.space.sm : 0
                    Layout.preferredHeight: visible ? Math.max(36, notesActionErrorRow.implicitHeight + Theme.space.md) : 0
                    color: Theme.colors.errorBg
                    border.width: visible ? 1 : 0
                    border.color: Theme.colors.error
                    radius: Theme.radius.sm
                    clip: true

                    RowLayout {
                        id: notesActionErrorRow
                        anchors.fill: parent
                        anchors.leftMargin: Theme.space.md
                        anchors.rightMargin: Theme.space.md
                        spacing: Theme.space.md

                        Label {
                            text: root.notesController ? root.notesController.action_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.error
                            wrapMode: Text.Wrap
                            Layout.fillWidth: true
                        }

                        AppButton {
                            objectName: "notesDismissActionError"
                            text: "Dismiss"
                            compact: true
                            toolTipText: "Dismiss notes error"
                            Layout.preferredWidth: 84
                            Layout.minimumWidth: 84
                            onClicked: {
                                if (root.notesController) {
                                    root.notesController.clear_action_error()
                                }
                            }
                        }
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
                        id: noteRow
                        objectName: "notesRow_" + index
                        width: notesList.width
                        implicitHeight: 48
                        radius: Theme.radius.sm
                        activeFocusOnTab: true
                        focusPolicy: Qt.StrongFocus
                        property bool qaForceKeyboardFocus: false
                        readonly property bool qaVisualFocus: activeFocus
                        readonly property bool isSelected: !!(root.notesController && root.notesController.selected &&
                            root.notesController.selected.path === model.path)
                        color: {
                            if (noteRow.isSelected) {
                                return Theme.colors.stateSelected
                            }
                            if (noteRow.activeFocus) {
                                return Theme.colors.stateFocusBg
                            }
                            return mouseArea.containsMouse ? Theme.colors.stateHover : "transparent"
                        }
                        border.width: activeFocus ? 1 : 0
                        border.color: Theme.colors.brandBright

                        Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
                        Behavior on border.color { ColorAnimation { duration: Theme.duration.fast } }

                        function openCurrentNote() {
                            if (root.notesController) {
                                notesList.currentIndex = index
                                root.notesController.open_note(model.path, model.title)
                            }
                        }

                        onQaForceKeyboardFocusChanged: {
                            if (qaForceKeyboardFocus) {
                                forceActiveFocus(Qt.TabFocusReason)
                            }
                        }

                        Keys.onReturnPressed: {
                            openCurrentNote()
                        }

                        Keys.onEnterPressed: {
                            openCurrentNote()
                        }

                        Keys.onSpacePressed: {
                            openCurrentNote()
                        }

                        MouseArea {
                            id: mouseArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: noteRow.openCurrentNote()
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
                            id: notesHeaderActions
                            objectName: "notesHeaderActions"
                            readonly property real targetWidth: root.notesController && root.notesController.editing
                                ? (96 + 72 + Theme.space.sm)
                                : 52
                            Layout.preferredWidth: targetWidth
                            Layout.minimumWidth: targetWidth
                            Layout.preferredHeight: 32
                            spacing: Theme.space.sm

                            // Edit mode buttons
                            AppButton {
                                objectName: "notesSaveEditButton"
                                visible: root.notesController && root.notesController.editing
                                text: root.notesController && root.notesController.saving ? "Saving…" : "Save"
                                variant: "primary"
                                toolTipText: "Save note"
                                enabled: root.notesController && !root.notesController.saving
                                onClicked: {
                                    if (root.notesController) {
                                        root.notesController.save()
                                    }
                                }

                                Layout.preferredWidth: 96
                                Layout.minimumWidth: 96
                                Layout.preferredHeight: 32
                            }

                            AppButton {
                                objectName: "notesCancelEditButton"
                                visible: root.notesController && root.notesController.editing
                                text: "Cancel"
                                toolTipText: "Cancel editing"
                                enabled: root.notesController && !root.notesController.saving
                                onClicked: {
                                    if (root.notesController) {
                                        root.notesController.cancel_edit()
                                    }
                                }

                                Layout.preferredWidth: 72
                                Layout.minimumWidth: 72
                                Layout.preferredHeight: 32
                            }

                            // Read mode button
                            AppButton {
                                objectName: "notesEditButton"
                                visible: root.notesController && !root.notesController.editing
                                text: "Edit"
                                toolTipText: "Edit note"
                                onClicked: {
                                    if (root.notesController) {
                                        root.notesController.start_edit()
                                    }
                                }

                                Layout.preferredWidth: 52
                                Layout.minimumWidth: 52
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
                        id: editScroll
                        visible: root.notesController && root.notesController.editing
                        anchors.fill: parent
                        clip: true
                        contentWidth: availableWidth

                        TextArea {
                            id: editor
                            objectName: "notesEditorTextArea"
                            width: editScroll.availableWidth
                            text: root.notesController ? root.notesController.draft : ""
                            property bool qaForceKeyboardFocus: false
                            readonly property bool qaVisualFocus: activeFocus
                            readonly property bool qaTextFits: true
                            readonly property string qaText: text || placeholderText
                            onTextChanged: {
                                if (root.notesController && root.notesController.editing) {
                                    root.notesController.draft = text
                                }
                            }

                            onQaForceKeyboardFocusChanged: {
                                if (qaForceKeyboardFocus) {
                                    forceActiveFocus(Qt.TabFocusReason)
                                }
                            }

                            Keys.onPressed: function(event) {
                                if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter)
                                        && (event.modifiers & Qt.ControlModifier)) {
                                    if (root.notesController && !root.notesController.saving) {
                                        root.notesController.save()
                                    }
                                    event.accepted = true
                                } else if (event.key === Qt.Key_Escape) {
                                    if (root.notesController && !root.notesController.saving) {
                                        root.notesController.cancel_edit()
                                    }
                                    event.accepted = true
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

                    // Read mode: markdown-rendered text
                    ScrollView {
                        id: readScroll
                        visible: root.notesController && !root.notesController.editing
                        anchors.fill: parent
                        clip: true
                        contentWidth: availableWidth

                        Item {
                            width: readScroll.availableWidth
                            implicitHeight: readColumn.implicitHeight + Theme.space.xxxl * 2

                            Column {
                                id: readColumn
                                x: Theme.space.xxxl
                                y: Theme.space.xxxl
                                width: Math.max(0, parent.width - Theme.space.xxxl * 2)
                                spacing: Theme.space.md

                                MarkdownBlocks {
                                    objectName: "notesMarkdownBody"
                                    visible: root.readBlocks.length > 0
                                    width: parent.width
                                    readonly property real qaContentWidth: width
                                    blocks: root.readBlocks
                                }

                                Label {
                                    objectName: "notesMarkdownFallback"
                                    visible: root.readContent !== "" && root.readBlocks.length === 0
                                    width: parent.width
                                    text: root.readContent
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textPrimary
                                    wrapMode: Text.Wrap
                                    textFormat: Text.PlainText
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    function parseMarkdown(source) {
        if (!source) return []
        if (typeof markdownParser === "undefined" || !markdownParser) {
            return [{type: "para", content: source}]
        }
        return markdownParser.parse(source)
    }
}
