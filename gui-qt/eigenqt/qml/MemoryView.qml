import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Memory view — durable notes browser (Memory.svelte port).
// Scope picker (Global + all projects), distilled summary, curated + ad-hoc notes,
// bans, global-scope USER.md profile editor, backups count.
Rectangle {
    id: root
    color: Theme.colors.bgBase
    property var memoryModel: null
    readonly property var activeMemoryModel: memoryModel || fallbackMemoryModel
    readonly property var fallbackMemoryModel: ({
        scopes: [],
        scope_key: "",
        current: null,
        composing: false,
        draft: "",
        scope_label: "selected",
        saving: false,
        loading: false,
        load_error: "",
        action_error: "",
        is_empty: true,
        has_backup_history: false,
        is_global: false,
        editing_profile: false,
        profile_draft: "",
        saving_profile: false,
        adding_ban: false,
        ban_title: "",
        ban_rule: "",
        saving_ban: false,
        moving_note: -1,
        destructive_action_pending: false,
        confirm_remove_ad_hoc: -1,
        removing_ad_hoc: -1,
        confirm_remove_note: -1,
        removing_note: -1,
        removing_ban: "",
        backups_open: false,
        backups_loading: false,
        backup_paths: [],
        move_open: false,
        move_pending: null,
        move_targets: [],
        short_dir: function(dir) { return dir || "" },
        reload_current: function() {},
        select_scope: function(_key) {},
        save_note: function() {},
        open_move: function(_text, _index) {},
        move_to: function(_key, _name) {},
        remove_ad_hoc: function(_index) {},
        remove_note: function(_index) {},
        start_profile: function() {},
        save_profile: function() {},
        add_ban: function() {},
        remove_ban: function(_title) {},
        toggle_backups: function() {},
        clear_action_error: function() {},
        backup_when: function(path) { return path || "" },
        backup_name: function(path) { return path || "" }
    })

    function memoryNoteCount(current) {
        if (!current) return 0
        if (current.noteCount !== undefined && current.noteCount !== null) return Number(current.noteCount) || 0
        if (current.notes && current.notes.length !== undefined) return current.notes.length
        return 0
    }

    function memoryAdHocCount(current) {
        if (!current || !current.adHoc || current.adHoc.length === undefined) return 0
        return current.adHoc.length
    }

    function memoryBytes(current) {
        if (!current || current.bytes === undefined || current.bytes === null) return 0
        return Number(current.bytes) || 0
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // Header: scope picker + Add note button
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 64
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.margins: Theme.space.xl
                spacing: Theme.space.lg

                // Scope picker
                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Theme.space.xs

                    Label {
                        text: "Memory scope"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        font.weight: Theme.fontWeight.medium
                        color: Theme.colors.textMuted
                    }

                    AppComboBox {
                        id: scopeCombo
                        objectName: "memoryScopeCombo"
                        Layout.preferredWidth: 300
                        Layout.preferredHeight: 32
                        model: activeMemoryModel.scopes
                        textRole: "name"
                        valueRole: "key"
                        currentIndex: findScopeIndex(activeMemoryModel.scope_key)
                        activationUpdatesCurrentIndex: false
                        accessibleName: "Memory scope"
                        toolTipText: "Change memory scope"

                        onActivated: function(index) {
                            var scope = activeMemoryModel.scopes[index]
                            if (scope && scope.key) {
                                activeMemoryModel.select_scope(scope.key)
                            }
                        }

                        delegate: ItemDelegate {
                            objectName: "memoryScopeCombo_option_" + index
                            property var scopeEntry: modelData || ({})

                            width: scopeCombo.width
                            text: scopeEntry.name + (scopeEntry.noteCount > 0 ? " (" + scopeEntry.noteCount + ")" : "")
                            highlighted: scopeCombo.highlightedIndex === index

                            background: Rectangle {
                                color: parent.highlighted ? Theme.colors.stateHover : "transparent"
                            }

                            contentItem: ColumnLayout {
                                spacing: 2

                                Label {
                                    text: scopeEntry.name + (scopeEntry.noteCount > 0 ? " (" + scopeEntry.noteCount + ")" : "")
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textPrimary
                                }

                                Label {
                                    visible: !!scopeEntry.dir
                                    text: scopeEntry.dir ? activeMemoryModel.short_dir(scopeEntry.dir) : ""
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.micro
                                    color: Theme.colors.textFaint
                                }
                            }
                        }
                    }
                }

                // Dir label (beside picker for disambiguation)
                Label {
                    visible: !!(activeMemoryModel.current && activeMemoryModel.current.dir)
                    text: activeMemoryModel.current ? activeMemoryModel.short_dir(activeMemoryModel.current.dir || "") : ""
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.label
                    color: Theme.colors.textFaint
                    elide: Text.ElideMiddle
                    Layout.preferredWidth: 240
                }

                Item { Layout.fillWidth: true }

                AppButton {
                    objectName: "memoryAddNoteButton"
                    text: activeMemoryModel.composing ? "Cancel" : "Add note"
                    enabled: activeMemoryModel.current !== null && !activeMemoryModel.saving
                    onClicked: {
                        if (!activeMemoryModel.saving) {
                            activeMemoryModel.composing = !activeMemoryModel.composing
                        }
                    }
                }
            }
        }

        // Compose area (expanded when composing=true)
        Rectangle {
            visible: activeMemoryModel.composing
            Layout.fillWidth: true
            implicitHeight: composeColumn.implicitHeight + Theme.space.lg * 2
            color: Theme.colors.bgWell
            border.width: 1
            border.color: Theme.colors.borderHairline
            onVisibleChanged: {
                if (visible) {
                    composeTextArea.forceActiveFocus(Qt.TabFocusReason)
                }
            }

            ColumnLayout {
                id: composeColumn
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.top: parent.top
                anchors.margins: Theme.space.lg
                spacing: Theme.space.md

                ScrollView {
                    id: composeScroll
                    Layout.fillWidth: true
                    Layout.preferredHeight: Math.min(composeTextArea.contentHeight + composeTextArea.topPadding + composeTextArea.bottomPadding, 200)
                    contentWidth: availableWidth
                    ScrollBar.horizontal.policy: ScrollBar.AlwaysOff

                    AppTextArea {
                        id: composeTextArea
                        objectName: "memoryComposeTextArea"
                        width: composeScroll.availableWidth
                        text: activeMemoryModel.draft
                        placeholderText: "A durable note for " + activeMemoryModel.scope_label + " memory…"

                        onTextChanged: activeMemoryModel.draft = text

                        Keys.onPressed: (event) => {
                            if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter) && (event.modifiers & Qt.ControlModifier)) {
                                root.saveMemoryNoteIfReady()
                                event.accepted = true
                            }
                        }

                        Component.onCompleted: {
                            if (activeMemoryModel.composing) {
                                forceActiveFocus()
                            }
                        }
                    }
                }

                RowLayout {
                    spacing: Theme.space.sm
                    Layout.alignment: Qt.AlignRight

                    AppButton {
                        objectName: "memoryDiscardNoteButton"
                        text: "Discard"
                        enabled: !activeMemoryModel.saving
                        onClicked: {
                            root.cancelMemoryNoteCompose()
                        }
                    }

                    AppButton {
                        objectName: "memorySaveNoteButton"
                        text: activeMemoryModel.saving ? "Saving…" : "Save note"
                        enabled: activeMemoryModel.draft.trim().length > 0 && !activeMemoryModel.saving
                        onClicked: activeMemoryModel.save_note()
                    }
                }
            }
        }

        Rectangle {
            objectName: "memoryActionError"
            visible: activeMemoryModel.action_error !== ""
            Layout.fillWidth: true
            Layout.preferredHeight: visible ? Math.max(40, memoryActionErrorRow.implicitHeight + Theme.space.md) : 0
            color: Theme.colors.errorBg
            border.width: visible ? 1 : 0
            border.color: Theme.colors.error
            clip: true

            RowLayout {
                id: memoryActionErrorRow
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xl
                anchors.rightMargin: Theme.space.xl
                spacing: Theme.space.md

                Label {
                    text: activeMemoryModel.action_error
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.label
                    color: Theme.colors.error
                    wrapMode: Text.WrapAnywhere
                    Layout.fillWidth: true
                }

                AppButton {
                    objectName: "memoryDismissActionError"
                    text: "X"
                    compact: true
                    toolTipText: "Dismiss memory error"
                    Layout.preferredWidth: 28
                    Layout.minimumWidth: 28
                    Layout.preferredHeight: 28
                    onClicked: activeMemoryModel.clear_action_error()
                }
            }
        }

        // Body: main content + sidebar
        Item {
            Layout.fillWidth: true
            Layout.fillHeight: true

            Rectangle {
                id: memoryRefreshErrorBanner
                objectName: "memoryRefreshErrorBanner"
                visible: !!(activeMemoryModel.load_error && activeMemoryModel.current)
                anchors.top: parent.top
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.margins: Theme.space.lg
                height: visible ? Math.max(40, memoryRefreshErrorRow.implicitHeight + Theme.space.md) : 0
                color: Theme.colors.errorBg
                border.width: visible ? 1 : 0
                border.color: Theme.colors.error
                radius: Theme.radius.md
                clip: true
                z: 2

                RowLayout {
                    id: memoryRefreshErrorRow
                    anchors.fill: parent
                    anchors.leftMargin: Theme.space.lg
                    anchors.rightMargin: Theme.space.lg
                    spacing: Theme.space.md

                    Label {
                        objectName: "memoryRefreshErrorText"
                        text: activeMemoryModel.load_error
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.error
                        wrapMode: Text.WrapAnywhere
                        Layout.fillWidth: true
                    }

                    AppButton {
                        objectName: "memoryRefreshErrorRetry"
                        text: "Retry"
                        compact: true
                        toolTipText: "Retry loading memory"
                        Layout.preferredWidth: 64
                        Layout.preferredHeight: 28
                        onClicked: activeMemoryModel.reload_current()
                    }
                }
            }

            // Loading skeleton
            ColumnLayout {
                visible: activeMemoryModel.loading && !activeMemoryModel.current
                anchors.fill: parent
                anchors.margins: Theme.space.xl
                spacing: Theme.space.lg

                Repeater {
                    model: 3
                    Rectangle {
                        Layout.fillWidth: true
                        Layout.preferredHeight: 88
                        color: Theme.colors.bgInset
                        radius: Theme.radius.md
                    }
                }
            }

            // Error state
            ColumnLayout {
                objectName: "memoryLoadError"
                visible: activeMemoryModel.load_error && !activeMemoryModel.current
                anchors.centerIn: parent
                spacing: Theme.space.lg

                Label {
                    text: "☾"
                    font.pixelSize: 48
                    color: Theme.colors.textFaint
                    Layout.alignment: Qt.AlignHCenter
                }

                Label {
                    text: "Couldn't load " + activeMemoryModel.scope_label + " memory"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h3
                    color: Theme.colors.textPrimary
                    Layout.alignment: Qt.AlignHCenter
                }

                Label {
                    objectName: "memoryLoadErrorText"
                    text: activeMemoryModel.load_error
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    Layout.alignment: Qt.AlignHCenter
                }

                AppButton {
                    objectName: "memoryLoadErrorRetry"
                    text: "Retry"
                    onClicked: activeMemoryModel.reload_current()
                    Layout.alignment: Qt.AlignHCenter
                }
            }

            // Empty state (no backup history)
            ColumnLayout {
                visible: !!(activeMemoryModel.current && activeMemoryModel.is_empty && !activeMemoryModel.has_backup_history)
                anchors.centerIn: parent
                spacing: Theme.space.lg

                Label {
                    text: "❖"
                    font.pixelSize: 48
                    color: Theme.colors.textFaint
                    Layout.alignment: Qt.AlignHCenter
                }

                Label {
                    text: "No " + activeMemoryModel.scope_label + " memory yet"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h3
                    color: Theme.colors.textPrimary
                    Layout.alignment: Qt.AlignHCenter
                }

                Label {
                    text: "Notes you save — or the agent distills — live here and carry across sessions."
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.Wrap
                    Layout.preferredWidth: 400
                }

                AppButton {
                    text: "Add the first note"
                    onClicked: activeMemoryModel.composing = true
                    Layout.alignment: Qt.AlignHCenter
                }
            }

            // Empty state (has backup history)
            ColumnLayout {
                visible: !!(activeMemoryModel.current && activeMemoryModel.is_empty && activeMemoryModel.has_backup_history)
                anchors.centerIn: parent
                spacing: Theme.space.lg

                Label {
                    text: "☾"
                    font.pixelSize: 48
                    color: Theme.colors.textFaint
                    Layout.alignment: Qt.AlignHCenter
                }

                Label {
                    text: "Nothing injected in " + activeMemoryModel.scope_label + " memory"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h3
                    color: Theme.colors.textPrimary
                    Layout.alignment: Qt.AlignHCenter
                }

                Label {
                    text: "This scope consolidated down to nothing currently injected — but its snapshot history is preserved."
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.Wrap
                    Layout.preferredWidth: 400
                }
            }

            // Content: main + sidebar (split horizontal)
            RowLayout {
                visible: !!(activeMemoryModel.current && !activeMemoryModel.is_empty)
                anchors.fill: parent
                anchors.topMargin: memoryRefreshErrorBanner.visible ? memoryRefreshErrorBanner.height + Theme.space.sm : 0
                spacing: 0

                // Main content (left)
                Flickable {
                    id: memoryFlick
                    objectName: "memoryFlick"
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    contentWidth: width
                    contentHeight: memoryContent.implicitHeight + Theme.space.xl * 2
                    clip: true
                    boundsBehavior: Flickable.StopAtBounds

                    ColumnLayout {
                        id: memoryContent
                        width: Math.max(0, memoryFlick.width - Theme.space.xl * 2)
                        x: Theme.space.xl
                        y: Theme.space.xl
                        spacing: Theme.space.xl

                        // Summary section
                        ColumnLayout {
                            visible: !!(activeMemoryModel.current && activeMemoryModel.current.summary)
                            Layout.fillWidth: true
                            spacing: Theme.space.md

                            RowLayout {
                                spacing: Theme.space.md

                                Label {
                                    text: "SUMMARY"
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.textFaint
                                }

                                AppTag {
                                    visible: !!(activeMemoryModel.current && activeMemoryModel.current.hasSummary)
                                    text: "distilled"
                                    backgroundColor: Theme.colors.brandBg || "transparent"
                                    borderColor: Theme.colors.brand
                                    textColor: Theme.colors.brand
                                    fontWeight: Theme.fontWeight.medium
                                    minimumHeight: 18
                                    pill: false
                                }
                            }

                            Rectangle {
                                Layout.fillWidth: true
                                implicitHeight: summaryBlocks.implicitHeight + Theme.space.lg * 2
                                color: Theme.colors.bgRaised
                                radius: Theme.radius.md
                                border.width: 1
                                border.color: Theme.colors.borderSubtle

                                MarkdownBlocks {
                                    id: summaryBlocks
                                    width: parent.width - Theme.space.lg * 2
                                    anchors.margins: Theme.space.lg
                                    anchors.left: parent.left
                                    anchors.top: parent.top
                                    blocks: activeMemoryModel.current ? parseMarkdown(activeMemoryModel.current.summary || "") : []
                                }
                            }
                        }

                        // Saved notes (ad-hoc) section
                        ColumnLayout {
                            visible: !!(activeMemoryModel.current && activeMemoryModel.current.adHoc && activeMemoryModel.current.adHoc.length > 0)
                            Layout.fillWidth: true
                            spacing: Theme.space.md

                            RowLayout {
                                spacing: Theme.space.md

                                Label {
                                    text: "SAVED NOTES"
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.textFaint
                                }

                                Label {
                                    text: activeMemoryModel.current ? activeMemoryModel.current.adHoc.length : 0
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textGhost
                                }

                                AppTag {
                                    text: "manual"
                                    backgroundColor: "transparent"
                                    borderColor: Theme.colors.borderSubtle
                                    textColor: Theme.colors.textSecondary
                                    minimumHeight: 18
                                    pill: false
                                }
                            }

                            Repeater {
                                model: activeMemoryModel.current ? activeMemoryModel.current.adHoc : []
                                delegate: Rectangle {
                                    Layout.fillWidth: true
                                    implicitHeight: adHocRow.implicitHeight + Theme.space.lg * 2
                                    color: Theme.colors.bgRaised
                                    radius: Theme.radius.md
                                    border.width: 1
                                    border.color: Theme.colors.borderSubtle

                                    RowLayout {
                                        id: adHocRow
                                        anchors.fill: parent
                                        anchors.margins: Theme.space.lg
                                        spacing: Theme.space.md

                                        MarkdownBlocks {
                                            Layout.fillWidth: true
                                            blocks: parseMarkdown(modelData.text || "")
                                        }

                                        RowLayout {
                                            spacing: Theme.space.sm
                                            Layout.alignment: Qt.AlignTop | Qt.AlignRight

                                            AppButton {
                                                objectName: "memoryAdHocMoveButton_" + modelData.index
                                                text: activeMemoryModel.moving_note === modelData.index ? "Moving…" : "Move"
                                                enabled: !activeMemoryModel.destructive_action_pending
                                                compact: true
                                                toolTipText: "Move saved note"
                                                onClicked: activeMemoryModel.open_move(modelData.text, modelData.index)
                                            }

                                            AppButton {
                                                objectName: "memoryAdHocRemoveButton_" + modelData.index
                                                visible: activeMemoryModel.confirm_remove_ad_hoc !== modelData.index
                                                text: activeMemoryModel.removing_ad_hoc === modelData.index ? "Removing…" : "Remove"
                                                enabled: !activeMemoryModel.destructive_action_pending
                                                compact: true
                                                variant: "danger"
                                                toolTipText: "Remove saved note"
                                                onClicked: activeMemoryModel.confirm_remove_ad_hoc = modelData.index
                                            }

                                            AppButton {
                                                objectName: "memoryAdHocRemoveConfirmButton_" + modelData.index
                                                visible: activeMemoryModel.confirm_remove_ad_hoc === modelData.index
                                                text: activeMemoryModel.removing_ad_hoc === modelData.index ? "Removing…" : "Confirm"
                                                enabled: !activeMemoryModel.destructive_action_pending
                                                compact: true
                                                variant: "danger"
                                                toolTipText: "Confirm saved note removal"
                                                onClicked: activeMemoryModel.remove_ad_hoc(modelData.index)
                                            }

                                            AppButton {
                                                objectName: "memoryAdHocRemoveCancelButton_" + modelData.index
                                                visible: activeMemoryModel.confirm_remove_ad_hoc === modelData.index
                                                text: "Cancel"
                                                enabled: !activeMemoryModel.destructive_action_pending
                                                compact: true
                                                toolTipText: "Cancel saved note removal"
                                                onClicked: activeMemoryModel.confirm_remove_ad_hoc = -1
                                            }
                                        }
                                    }
                                }
                            }
                        }

                        // Notes section (distilled)
                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: Theme.space.md

                            RowLayout {
                                spacing: Theme.space.md

                                Label {
                                    text: "NOTES"
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.textFaint
                                }

                                Label {
                                    text: String(root.memoryNoteCount(activeMemoryModel.current))
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textGhost
                                }
                            }

                            Label {
                                visible: !!(activeMemoryModel.current && root.memoryNoteCount(activeMemoryModel.current) === 0)
                                text: "No distilled notes in this scope yet."
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                color: Theme.colors.textMuted
                            }

                            Repeater {
                                model: activeMemoryModel.current ? activeMemoryModel.current.notes : []
                                delegate: Rectangle {
                                    Layout.fillWidth: true
                                    implicitHeight: noteRow.implicitHeight + Theme.space.lg * 2
                                    color: Theme.colors.bgRaised
                                    radius: Theme.radius.md
                                    border.width: 1
                                    border.color: Theme.colors.borderSubtle

                                    RowLayout {
                                        id: noteRow
                                        anchors.fill: parent
                                        anchors.margins: Theme.space.lg
                                        spacing: Theme.space.md

                                        MarkdownBlocks {
                                            Layout.fillWidth: true
                                            blocks: parseMarkdown(modelData.text || "")
                                        }

                                        RowLayout {
                                            spacing: Theme.space.sm
                                            Layout.alignment: Qt.AlignTop | Qt.AlignRight

                                            AppButton {
                                                objectName: "memoryNoteMoveButton_" + modelData.index
                                                text: activeMemoryModel.moving_note === modelData.index ? "Moving…" : "Move"
                                                enabled: !activeMemoryModel.destructive_action_pending
                                                compact: true
                                                toolTipText: "Move distilled note"
                                                onClicked: activeMemoryModel.open_move(modelData.text, modelData.index)
                                            }

                                            AppButton {
                                                objectName: "memoryNoteRemoveButton_" + modelData.index
                                                visible: activeMemoryModel.confirm_remove_note !== modelData.index
                                                text: activeMemoryModel.removing_note === modelData.index ? "Removing…" : "Remove"
                                                enabled: !activeMemoryModel.destructive_action_pending
                                                compact: true
                                                variant: "danger"
                                                toolTipText: "Remove distilled note"
                                                onClicked: activeMemoryModel.confirm_remove_note = modelData.index
                                            }

                                            AppButton {
                                                objectName: "memoryNoteRemoveConfirmButton_" + modelData.index
                                                visible: activeMemoryModel.confirm_remove_note === modelData.index
                                                text: activeMemoryModel.removing_note === modelData.index ? "Removing…" : "Confirm"
                                                enabled: !activeMemoryModel.destructive_action_pending
                                                compact: true
                                                variant: "danger"
                                                toolTipText: "Confirm distilled note removal"
                                                onClicked: activeMemoryModel.remove_note(modelData.index)
                                            }

                                            AppButton {
                                                objectName: "memoryNoteRemoveCancelButton_" + modelData.index
                                                visible: activeMemoryModel.confirm_remove_note === modelData.index
                                                text: "Cancel"
                                                enabled: !activeMemoryModel.destructive_action_pending
                                                compact: true
                                                toolTipText: "Cancel distilled note removal"
                                                onClicked: activeMemoryModel.confirm_remove_note = -1
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }

                    ScrollBar.vertical: ScrollBar {}
                }

                // Sidebar (right)
                Rectangle {
                    Layout.preferredWidth: 300
                    Layout.fillHeight: true
                    color: Theme.colors.bgWell
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Flickable {
                        id: memorySidebarFlick
                        objectName: "memorySidebarFlick"
                        anchors.fill: parent
                        contentWidth: width
                        contentHeight: sidebarContent.implicitHeight + Theme.space.xl * 2
                        clip: true
                        boundsBehavior: Flickable.StopAtBounds

                        ColumnLayout {
                            id: sidebarContent
                            width: Math.max(0, memorySidebarFlick.width - Theme.space.xl * 2)
                            x: Theme.space.xl
                            y: Theme.space.xl
                            spacing: Theme.space.xl

                            // User profile section (global only)
                            ColumnLayout {
                                visible: activeMemoryModel.is_global
                                Layout.fillWidth: true
                                spacing: Theme.space.md

                                RowLayout {
                                    spacing: Theme.space.md

                                    Label {
                                        text: "USER PROFILE"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.label
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.textFaint
                                    }

                                    AppTag {
                                        text: "USER.md"
                                        backgroundColor: Theme.colors.brandBg || "transparent"
                                        borderColor: Theme.colors.brand
                                        textColor: Theme.colors.brand
                                        fontWeight: Theme.fontWeight.medium
                                        minimumHeight: 18
                                        pill: false
                                    }

                                    AppButton {
                                        objectName: "memoryEditProfileButton"
                                        visible: !activeMemoryModel.editing_profile
                                        text: "Edit"
                                        onClicked: activeMemoryModel.start_profile()
                                    }
                                }

                                Label {
                                    Layout.fillWidth: true
                                    text: "Your durable personalization prompt — eigen keeps it current as it learns; your own additions sit alongside."
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textFaint
                                    wrapMode: Text.Wrap
                                }

                                // Learned section
                                Rectangle {
                                    visible: !!(activeMemoryModel.current && activeMemoryModel.current.profileLearned)
                                    Layout.fillWidth: true
                                    implicitHeight: learnedCol.implicitHeight + Theme.space.lg * 2
                                    color: Theme.colors.bgRaised
                                    radius: Theme.radius.md
                                    border.width: 1
                                    border.color: Theme.colors.borderSubtle

                                    ColumnLayout {
                                        id: learnedCol
                                        anchors.fill: parent
                                        anchors.margins: Theme.space.lg
                                        spacing: Theme.space.sm

                                        ColumnLayout {
                                            spacing: 2
                                            Layout.fillWidth: true

                                            Label {
                                                text: "✧ learned by eigen"
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.label
                                                font.weight: Theme.fontWeight.semibold
                                                color: Theme.colors.brand
                                            }

                                            Label {
                                                text: "Auto-maintained from your sessions"
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textFaint
                                                wrapMode: Text.Wrap
                                                Layout.fillWidth: true
                                            }
                                        }

                                        MarkdownBlocks {
                                            Layout.fillWidth: true
                                            blocks: activeMemoryModel.current ? parseMarkdown(activeMemoryModel.current.profileLearned || "") : []
                                        }
                                    }
                                }

                                // Profile editor
                                ColumnLayout {
                                    visible: activeMemoryModel.editing_profile
                                    Layout.fillWidth: true
                                    spacing: Theme.space.md

                                    ScrollView {
                                        id: profileScroll
                                        Layout.fillWidth: true
                                        Layout.preferredHeight: Math.min(profileTextArea.contentHeight + profileTextArea.topPadding + profileTextArea.bottomPadding, 300)
                                        contentWidth: availableWidth
                                        ScrollBar.horizontal.policy: ScrollBar.AlwaysOff

                                        AppTextArea {
                                            id: profileTextArea
                                            objectName: "memoryProfileTextArea"
                                            width: profileScroll.availableWidth
                                            text: activeMemoryModel.profile_draft
                                            placeholderText: "Add your own notes — eigen keeps the rest current…"

                                            onTextChanged: activeMemoryModel.profile_draft = text

                                            Keys.onPressed: (event) => {
                                                if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter) && (event.modifiers & Qt.ControlModifier)) {
                                                    if (!activeMemoryModel.saving_profile) {
                                                        activeMemoryModel.save_profile()
                                                    }
                                                    event.accepted = true
                                                } else if (event.key === Qt.Key_Escape) {
                                                    if (!activeMemoryModel.saving_profile) {
                                                        root.cancelProfileEdit()
                                                    }
                                                    event.accepted = true
                                                }
                                            }
                                        }
                                    }

                                    RowLayout {
                                        spacing: Theme.space.sm

                                        AppButton {
                                            objectName: "memoryCancelProfileButton"
                                            text: "Cancel"
                                            enabled: !activeMemoryModel.saving_profile
                                            onClicked: root.cancelProfileEdit()
                                        }

                                        AppButton {
                                            objectName: "memorySaveProfileButton"
                                            text: activeMemoryModel.saving_profile ? "Saving…" : "Save"
                                            enabled: !activeMemoryModel.saving_profile
                                            onClicked: activeMemoryModel.save_profile()
                                        }
                                    }
                                }

                                // Profile view (not editing)
                                Rectangle {
                                    visible: !!(!activeMemoryModel.editing_profile && activeMemoryModel.current && activeMemoryModel.current.profile)
                                    Layout.fillWidth: true
                                    implicitHeight: profileBlocks.implicitHeight + Theme.space.lg * 2
                                    color: Theme.colors.bgRaised
                                    radius: Theme.radius.md
                                    border.width: 1
                                    border.color: Theme.colors.borderSubtle

                                    MarkdownBlocks {
                                        id: profileBlocks
                                        width: parent.width - Theme.space.lg * 2
                                        anchors.margins: Theme.space.lg
                                        anchors.left: parent.left
                                        anchors.top: parent.top
                                        blocks: activeMemoryModel.current ? parseMarkdown(activeMemoryModel.current.profile || "") : []
                                    }
                                }

                                Label {
                                    visible: !activeMemoryModel.editing_profile && (!activeMemoryModel.current || !activeMemoryModel.current.profile)
                                    text: "Nothing learned yet. Add your own."
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textMuted

                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: activeMemoryModel.start_profile()
                                    }
                                }
                            }

                            // Banned behaviors section
                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: Theme.space.md

                                RowLayout {
                                    spacing: Theme.space.md

                                    Label {
                                        text: "BANNED BEHAVIORS"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.label
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.textFaint
                                    }

                                    AppTag {
                                        visible: !!(activeMemoryModel.current && activeMemoryModel.current.banList && activeMemoryModel.current.banList.length > 0)
                                        text: "enforced"
                                        backgroundColor: Theme.colors.errorBg
                                        borderColor: Theme.colors.error
                                        textColor: Theme.colors.error
                                        fontWeight: Theme.fontWeight.medium
                                        minimumHeight: 18
                                        pill: false
                                    }

                                    AppButton {
                                        objectName: "memoryAddBanButton"
                                        visible: !activeMemoryModel.adding_ban
                                        text: "Add"
                                        onClicked: activeMemoryModel.adding_ban = true
                                    }
                                }

                                // Add ban form
                                ColumnLayout {
                                    visible: activeMemoryModel.adding_ban
                                    Layout.fillWidth: true
                                    spacing: Theme.space.md

                                    TextField {
                                        id: banTitleField
                                        objectName: "memoryBanTitleInput"
                                        Layout.fillWidth: true
                                        placeholderText: "Short title"
                                        placeholderTextColor: Theme.colors.textGhost
                                        text: activeMemoryModel.ban_title
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textPrimary
                                        background: Rectangle {
                                            color: Theme.colors.bgRaised
                                            border.width: 1
                                            border.color: banTitleField.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                                            radius: Theme.radius.md
                                        }

                                        onTextChanged: activeMemoryModel.ban_title = text

                                        Keys.onPressed: (event) => {
                                            if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter) && (event.modifiers & Qt.ControlModifier)) {
                                                root.saveBanIfReady()
                                                event.accepted = true
                                            } else if (event.key === Qt.Key_Escape) {
                                                if (!activeMemoryModel.saving_ban) {
                                                    root.cancelBanEdit()
                                                }
                                                event.accepted = true
                                            }
                                        }
                                    }

                                    ScrollView {
                                        id: banRuleScroll
                                        Layout.fillWidth: true
                                        Layout.preferredHeight: Math.min(banRuleTextArea.contentHeight + banRuleTextArea.topPadding + banRuleTextArea.bottomPadding, 150)
                                        contentWidth: availableWidth
                                        ScrollBar.horizontal.policy: ScrollBar.AlwaysOff

                                        AppTextArea {
                                            id: banRuleTextArea
                                            objectName: "memoryBanRuleTextArea"
                                            width: banRuleScroll.availableWidth
                                            text: activeMemoryModel.ban_rule
                                            placeholderText: "What the agent must not do (" + activeMemoryModel.scope_label + " scope)…"

                                            onTextChanged: activeMemoryModel.ban_rule = text

                                            Keys.onPressed: (event) => {
                                                if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter) && (event.modifiers & Qt.ControlModifier)) {
                                                    root.saveBanIfReady()
                                                    event.accepted = true
                                                } else if (event.key === Qt.Key_Escape) {
                                                    if (!activeMemoryModel.saving_ban) {
                                                        root.cancelBanEdit()
                                                    }
                                                    event.accepted = true
                                                }
                                            }
                                        }
                                    }

                                    RowLayout {
                                        spacing: Theme.space.sm

                                        AppButton {
                                            objectName: "memoryCancelBanButton"
                                            text: "Cancel"
                                            enabled: !activeMemoryModel.saving_ban
                                            onClicked: root.cancelBanEdit()
                                        }

                                        AppButton {
                                            objectName: "memorySaveBanButton"
                                            text: activeMemoryModel.saving_ban ? "Saving…" : "Save ban"
                                            enabled: activeMemoryModel.ban_title.trim().length > 0 && activeMemoryModel.ban_rule.trim().length > 0 && !activeMemoryModel.saving_ban
                                            onClicked: root.saveBanIfReady()
                                        }
                                    }
                                }

                                // Bans list
                                Repeater {
                                    model: activeMemoryModel.current ? activeMemoryModel.current.banList : []
                                    delegate: Rectangle {
                                        Layout.fillWidth: true
                                        implicitHeight: banRow.implicitHeight + Theme.space.lg * 2
                                        color: Theme.colors.bgRaised
                                        radius: Theme.radius.md
                                        border.width: 1
                                        border.color: Theme.colors.borderSubtle

                                        RowLayout {
                                            id: banRow
                                            anchors.fill: parent
                                            anchors.margins: Theme.space.lg
                                            spacing: Theme.space.md

                                            ColumnLayout {
                                                Layout.fillWidth: true
                                                spacing: Theme.space.sm

                                                Label {
                                                    text: modelData.title
                                                    font.family: Theme.uiFonts[0]
                                                    font.pixelSize: Theme.fontSize.bodySm
                                                    font.weight: Theme.fontWeight.semibold
                                                    color: Theme.colors.textPrimary
                                                    wrapMode: Text.Wrap
                                                    Layout.fillWidth: true
                                                }

                                                Label {
                                                    text: modelData.rule
                                                    font.family: Theme.uiFonts[0]
                                                    font.pixelSize: Theme.fontSize.bodySm
                                                    color: Theme.colors.textSecondary
                                                    wrapMode: Text.Wrap
                                                    Layout.fillWidth: true
                                                }
                                            }

                                            AppButton {
                                                objectName: "memoryBanRemoveButton_" + root.safeObjectName(modelData.title)
                                                text: activeMemoryModel.removing_ban === modelData.title ? "…" : "✕"
                                                enabled: !activeMemoryModel.destructive_action_pending
                                                onClicked: activeMemoryModel.remove_ban(modelData.title)
                                                Layout.preferredWidth: 24
                                                Layout.preferredHeight: 24
                                                flat: true
                                            }
                                        }
                                    }
                                }

                                Label {
                                    visible: !activeMemoryModel.adding_ban && (!activeMemoryModel.current || !activeMemoryModel.current.banList || activeMemoryModel.current.banList.length === 0)
                                    text: "No bans in this scope. Add one."
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textMuted

                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: activeMemoryModel.adding_ban = true
                                    }
                                }
                            }

                            // Scope meta section
                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: Theme.space.md

                                Label {
                                    text: "SCOPE"
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.textFaint
                                }

                                GridLayout {
                                    columns: 2
                                    columnSpacing: Theme.space.lg
                                    rowSpacing: Theme.space.sm
                                    Layout.fillWidth: true

                                    Label {
                                        text: "notes"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                    }

                                    Label {
                                        text: String(root.memoryNoteCount(activeMemoryModel.current))
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textSecondary
                                        horizontalAlignment: Text.AlignRight
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        text: "ad-hoc"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                    }

                                    Label {
                                        text: String(root.memoryAdHocCount(activeMemoryModel.current))
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textSecondary
                                        horizontalAlignment: Text.AlignRight
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        text: "backups"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                    }

                                    RowLayout {
                                        Layout.fillWidth: true
                                        spacing: Theme.space.xs

                                        Label {
                                            visible: !activeMemoryModel.current || activeMemoryModel.current.backups === 0
                                            text: "0"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.bodySm
                                            color: Theme.colors.textSecondary
                                            horizontalAlignment: Text.AlignRight
                                            Layout.fillWidth: true
                                        }

                                        AppButton {
                                            visible: !!(activeMemoryModel.current && activeMemoryModel.current.backups > 0)
                                            text: activeMemoryModel.current ? activeMemoryModel.current.backups + " ▸" : "0"
                                            flat: true
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.bodySm
                                            contentItem: Label {
                                                text: parent.text
                                                font: parent.font
                                                color: Theme.colors.brand
                                                horizontalAlignment: Text.AlignRight
                                            }
                                            onClicked: activeMemoryModel.toggle_backups()
                                            Layout.fillWidth: true
                                        }
                                    }

                                    Label {
                                        text: "size"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                    }

                                    Label {
                                        text: (root.memoryBytes(activeMemoryModel.current) / 1024).toFixed(1) + " KB"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textSecondary
                                        horizontalAlignment: Text.AlignRight
                                        Layout.fillWidth: true
                                    }
                                }

                                // Backups list
                                ColumnLayout {
                                    visible: activeMemoryModel.backups_open
                                    Layout.fillWidth: true
                                    spacing: Theme.space.sm

                                    Label {
                                        visible: activeMemoryModel.backups_loading && activeMemoryModel.backup_paths.length === 0
                                        text: "Loading backups…"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                    }

                                    Label {
                                        visible: !activeMemoryModel.backups_loading && activeMemoryModel.backup_paths.length === 0
                                        text: "No backup snapshots on disk."
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                    }

                                    Repeater {
                                        model: activeMemoryModel.backup_paths
                                        delegate: Rectangle {
                                            Layout.fillWidth: true
                                            implicitHeight: backupCol.implicitHeight + Theme.space.sm * 2
                                            color: Theme.colors.bgRaised
                                            radius: Theme.radius.sm
                                            border.width: 1
                                            border.color: Theme.colors.borderHairline

                                            ColumnLayout {
                                                id: backupCol
                                                anchors.fill: parent
                                                anchors.margins: Theme.space.sm
                                                spacing: 2

                                                Label {
                                                    text: activeMemoryModel.backup_when(modelData)
                                                    font.family: Theme.uiFonts[0]
                                                    font.pixelSize: Theme.fontSize.bodySm
                                                    color: Theme.colors.textSecondary
                                                }

                                                Label {
                                                    text: activeMemoryModel.backup_name(modelData)
                                                    font.family: Theme.uiFonts[0]
                                                    font.pixelSize: Theme.fontSize.micro
                                                    color: Theme.colors.textFaint
                                                    wrapMode: Text.Wrap
                                                    Layout.fillWidth: true
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }

                        ScrollBar.vertical: ScrollBar {}
                    }
                }
            }
        }
    }

    // Move-to-scope dialog
    Dialog {
        id: moveDialog
        visible: moveDialogModel() ? moveDialogModel().move_open : false
        modal: true
        anchors.centerIn: parent
        width: 420
        padding: Theme.space.lg
        standardButtons: Dialog.NoButton
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside

        onClosed: {
            var model = moveDialogModel()
            if (model) {
                model.move_open = false
            }
        }

        background: Rectangle {
            color: Theme.colors.bgWell
            radius: Theme.radius.md
            border.width: 1
            border.color: Theme.colors.borderSubtle
        }

        header: Rectangle {
            color: Theme.colors.bgWell
            implicitHeight: 44
            radius: Theme.radius.md

            Label {
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.verticalCenter: parent.verticalCenter
                anchors.leftMargin: Theme.space.lg
                anchors.rightMargin: Theme.space.lg
                text: "Move note to..."
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textPrimary
            }
        }

        contentItem: ColumnLayout {
            spacing: Theme.space.md

            Label {
                visible: !!moveDialogPending()
                text: moveDialogPending() ? moveDialogPending().text : ""
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textSecondary
                wrapMode: Text.Wrap
                Layout.fillWidth: true
                background: Rectangle {
                    color: Theme.colors.bgRaised
                    radius: Theme.radius.md
                }
                padding: Theme.space.md
            }

            Label {
                visible: moveDialogTargets().length === 0
                text: "No other scope to move into."
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textMuted
            }

            Repeater {
                model: moveDialogTargets()
                delegate: AppButton {
                    objectName: "memoryMoveTargetButton_" + root.safeObjectName(modelData.key)
                    Layout.fillWidth: true
                    text: modelData.name
                    toolTipText: "Move note to " + modelData.name
                    enabled: {
                        var model = moveDialogModel()
                        return !!model && !model.destructive_action_pending
                    }
                    onClicked: {
                        var model = moveDialogModel()
                        if (model) {
                            model.move_to(modelData.key, modelData.name)
                        }
                        moveDialog.close()
                    }

                    contentItem: ColumnLayout {
                        spacing: 2

                        Label {
                            text: modelData.name
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            font.weight: Theme.fontWeight.medium
                            color: Theme.colors.textPrimary
                        }

                        Label {
                            visible: modelData.dir
                            text: modelData.dir && moveDialogModel() ? moveDialogModel().short_dir(modelData.dir) : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            color: Theme.colors.textFaint
                        }
                    }
                }
            }

            AppButton {
                objectName: "memoryMoveDialogCloseButton"
                text: "Close"
                toolTipText: "Close move dialog"
                Layout.alignment: Qt.AlignRight
                Layout.preferredWidth: 84
                onClicked: moveDialog.close()
            }
        }
    }

    // Helper: find scope index by key
    function findScopeIndex(key) {
        for (var i = 0; i < activeMemoryModel.scopes.length; i++) {
            if (activeMemoryModel.scopes[i].key === key) {
                return i
            }
        }
        return 0
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]+/g, "_")
    }

    // Helper: parse markdown (calls Python markdown parser via context property)
    function parseMarkdown(source) {
        if (!source || typeof markdownParser === "undefined" || !markdownParser) {
            return [{type: "para", content: source || ""}]
        }
        return markdownParser.parse(source)
    }

    function moveDialogModel() {
        if (typeof activeMemoryModel === "undefined" || activeMemoryModel === null) {
            return null
        }
        return activeMemoryModel
    }

    function moveDialogPending() {
        var model = moveDialogModel()
        return model ? model.move_pending : null
    }

    function moveDialogTargets() {
        var model = moveDialogModel()
        return model && model.move_targets ? model.move_targets : []
    }

    function saveMemoryNoteIfReady() {
        if (activeMemoryModel.draft.trim().length > 0 && !activeMemoryModel.saving) {
            activeMemoryModel.save_note()
        }
    }

    function cancelMemoryNoteCompose() {
        if (activeMemoryModel.saving) {
            return
        }
        activeMemoryModel.composing = false
        activeMemoryModel.draft = ""
    }

    function cancelProfileEdit() {
        if (activeMemoryModel.saving_profile) {
            return
        }
        activeMemoryModel.profile_draft = activeMemoryModel.current ? (activeMemoryModel.current.profile || "") : ""
        activeMemoryModel.editing_profile = false
    }

    function saveBanIfReady() {
        if (activeMemoryModel.ban_title.trim().length > 0
                && activeMemoryModel.ban_rule.trim().length > 0
                && !activeMemoryModel.saving_ban) {
            activeMemoryModel.add_ban()
        }
    }

    function cancelBanEdit() {
        if (activeMemoryModel.saving_ban) {
            return
        }
        activeMemoryModel.adding_ban = false
        activeMemoryModel.ban_title = ""
        activeMemoryModel.ban_rule = ""
    }
}
