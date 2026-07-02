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

                    ComboBox {
                        id: scopeCombo
                        Layout.preferredWidth: 300
                        model: memoryModel.scopes
                        textRole: "name"
                        valueRole: "key"
                        currentIndex: findScopeIndex(memoryModel.scope_key)

                        onActivated: {
                            memoryModel.select_scope(currentValue)
                        }

                        delegate: ItemDelegate {
                            width: scopeCombo.width
                            text: model.name + (model.noteCount > 0 ? " (" + model.noteCount + ")" : "")

                            contentItem: ColumnLayout {
                                spacing: 2

                                Label {
                                    text: model.name + (model.noteCount > 0 ? " (" + model.noteCount + ")" : "")
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textPrimary
                                }

                                Label {
                                    visible: model.dir
                                    text: model.dir ? memoryModel.short_dir(model.dir) : ""
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
                    visible: !!(memoryModel.current && memoryModel.current.dir)
                    text: memoryModel.current ? memoryModel.short_dir(memoryModel.current.dir || "") : ""
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.label
                    color: Theme.colors.textFaint
                    elide: Text.ElideMiddle
                    Layout.preferredWidth: 240
                }

                Item { Layout.fillWidth: true }

                Button {
                    text: memoryModel.composing ? "Cancel" : "Add note"
                    enabled: memoryModel.current !== null
                    onClicked: memoryModel.composing = !memoryModel.composing
                }
            }
        }

        // Compose area (expanded when composing=true)
        Rectangle {
            visible: memoryModel.composing
            Layout.fillWidth: true
            implicitHeight: composeColumn.implicitHeight
            color: Theme.colors.bgWell
            border.width: 1
            border.color: Theme.colors.borderHairline

            ColumnLayout {
                id: composeColumn
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.top: parent.top
                anchors.margins: Theme.space.lg
                spacing: Theme.space.md

                ScrollView {
                    Layout.fillWidth: true
                    Layout.preferredHeight: Math.min(composeTextArea.contentHeight + Theme.space.md * 2, 200)
                    ScrollBar.horizontal.policy: ScrollBar.AlwaysOff

                    TextArea {
                        id: composeTextArea
                        text: memoryModel.draft
                        placeholderText: "A durable note for " + memoryModel.scope_label + " memory…"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textPrimary
                        wrapMode: Text.Wrap
                        background: Rectangle {
                            color: Theme.colors.bgRaised
                            border.width: 1
                            border.color: composeTextArea.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                            radius: Theme.radius.md
                        }

                        onTextChanged: memoryModel.draft = text

                        Keys.onPressed: (event) => {
                            if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter) && (event.modifiers & Qt.ControlModifier)) {
                                memoryModel.save_note()
                                event.accepted = true
                            }
                        }

                        Component.onCompleted: {
                            if (memoryModel.composing) {
                                forceActiveFocus()
                            }
                        }
                    }
                }

                RowLayout {
                    spacing: Theme.space.sm
                    Layout.alignment: Qt.AlignRight

                    Button {
                        text: "Discard"
                        onClicked: {
                            memoryModel.composing = false
                            memoryModel.draft = ""
                        }
                    }

                    Button {
                        text: memoryModel.saving ? "Saving…" : "Save note"
                        enabled: memoryModel.draft.trim().length > 0 && !memoryModel.saving
                        onClicked: memoryModel.save_note()
                    }
                }
            }
        }

        // Body: main content + sidebar
        Item {
            Layout.fillWidth: true
            Layout.fillHeight: true

            // Loading skeleton
            ColumnLayout {
                visible: memoryModel.loading && !memoryModel.current
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
                visible: memoryModel.load_error && !memoryModel.current
                anchors.centerIn: parent
                spacing: Theme.space.lg

                Label {
                    text: "☾"
                    font.pixelSize: 48
                    color: Theme.colors.textFaint
                    Layout.alignment: Qt.AlignHCenter
                }

                Label {
                    text: "Couldn't load " + memoryModel.scope_label + " memory"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.h3
                    color: Theme.colors.textPrimary
                    Layout.alignment: Qt.AlignHCenter
                }

                Label {
                    text: memoryModel.load_error
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    Layout.alignment: Qt.AlignHCenter
                }

                Button {
                    text: "Retry"
                    onClicked: memoryModel.select_scope(memoryModel.scope_key)
                    Layout.alignment: Qt.AlignHCenter
                }
            }

            // Empty state (no backup history)
            ColumnLayout {
                visible: !!(memoryModel.current && memoryModel.is_empty && !memoryModel.has_backup_history)
                anchors.centerIn: parent
                spacing: Theme.space.lg

                Label {
                    text: "❖"
                    font.pixelSize: 48
                    color: Theme.colors.textFaint
                    Layout.alignment: Qt.AlignHCenter
                }

                Label {
                    text: "No " + memoryModel.scope_label + " memory yet"
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

                Button {
                    text: "Add the first note"
                    onClicked: memoryModel.composing = true
                    Layout.alignment: Qt.AlignHCenter
                }
            }

            // Empty state (has backup history)
            ColumnLayout {
                visible: !!(memoryModel.current && memoryModel.is_empty && memoryModel.has_backup_history)
                anchors.centerIn: parent
                spacing: Theme.space.lg

                Label {
                    text: "☾"
                    font.pixelSize: 48
                    color: Theme.colors.textFaint
                    Layout.alignment: Qt.AlignHCenter
                }

                Label {
                    text: "Nothing injected in " + memoryModel.scope_label + " memory"
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
                visible: !!(memoryModel.current && !memoryModel.is_empty)
                anchors.fill: parent
                spacing: 0

                // Main content (left)
                ScrollView {
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    ScrollBar.horizontal.policy: ScrollBar.AlwaysOff
                    clip: true

                    ColumnLayout {
                        width: parent.width
                        spacing: Theme.space.xl
                        anchors.margins: Theme.space.xl

                        // Summary section
                        ColumnLayout {
                            visible: !!(memoryModel.current && memoryModel.current.summary)
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

                                Rectangle {
                                    visible: !!(memoryModel.current && memoryModel.current.hasSummary)
                                    implicitWidth: distilledLabel.implicitWidth + Theme.space.sm * 2
                                    implicitHeight: 18
                                    radius: Theme.radius.sm
                                    color: Theme.colors.brandBg || "transparent"
                                    border.width: 1
                                    border.color: Theme.colors.brand
                                    Label {
                                        id: distilledLabel
                                        anchors.centerIn: parent
                                        text: "distilled"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        font.weight: Theme.fontWeight.medium
                                        color: Theme.colors.brand
                                    }
                                }
                            }

                            Rectangle {
                                Layout.fillWidth: true
                                implicitHeight: summaryBlocks.implicitHeight
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
                                    blocks: memoryModel.current ? parseMarkdown(memoryModel.current.summary || "") : []
                                }
                            }
                        }

                        // Saved notes (ad-hoc) section
                        ColumnLayout {
                            visible: !!(memoryModel.current && memoryModel.current.adHoc && memoryModel.current.adHoc.length > 0)
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
                                    text: memoryModel.current ? memoryModel.current.adHoc.length : 0
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textGhost
                                }

                                Rectangle {
                                    implicitWidth: manualLabel.implicitWidth + Theme.space.sm * 2
                                    implicitHeight: 18
                                    radius: Theme.radius.sm
                                    color: "transparent"
                                    border.width: 1
                                    border.color: Theme.colors.borderSubtle
                                    Label {
                                        id: manualLabel
                                        anchors.centerIn: parent
                                        text: "manual"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textSecondary
                                    }
                                }
                            }

                            Repeater {
                                model: memoryModel.current ? memoryModel.current.adHoc : []
                                delegate: Rectangle {
                                    Layout.fillWidth: true
                                    implicitHeight: adHocRow.implicitHeight
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

                                        ColumnLayout {
                                            spacing: Theme.space.xs
                                            Layout.alignment: Qt.AlignTop

                                            Button {
                                                text: memoryModel.moving_note === modelData.index ? "Moving…" : "Move"
                                                enabled: memoryModel.moving_note !== modelData.index
                                                onClicked: memoryModel.open_move(modelData.text, modelData.index)
                                            }

                                            Button {
                                                visible: memoryModel.confirm_remove_ad_hoc !== modelData.index
                                                text: "Remove"
                                                onClicked: memoryModel.confirm_remove_ad_hoc = modelData.index
                                            }

                                            Button {
                                                visible: memoryModel.confirm_remove_ad_hoc === modelData.index
                                                text: memoryModel.removing_ad_hoc === modelData.index ? "Removing…" : "Confirm"
                                                enabled: memoryModel.removing_ad_hoc !== modelData.index
                                                onClicked: memoryModel.remove_ad_hoc(modelData.index)
                                            }

                                            Button {
                                                visible: memoryModel.confirm_remove_ad_hoc === modelData.index
                                                text: "Cancel"
                                                enabled: memoryModel.removing_ad_hoc !== modelData.index
                                                onClicked: memoryModel.confirm_remove_ad_hoc = -1
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
                                    text: memoryModel.current ? memoryModel.current.noteCount : 0
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textGhost
                                }
                            }

                            Label {
                                visible: !!(memoryModel.current && memoryModel.current.noteCount === 0)
                                text: "No distilled notes in this scope yet."
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                color: Theme.colors.textMuted
                            }

                            Repeater {
                                model: memoryModel.current ? memoryModel.current.notes : []
                                delegate: Rectangle {
                                    Layout.fillWidth: true
                                    implicitHeight: noteRow.implicitHeight
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

                                        ColumnLayout {
                                            spacing: Theme.space.xs
                                            Layout.alignment: Qt.AlignTop

                                            Button {
                                                text: memoryModel.moving_note === modelData.index ? "Moving…" : "Move"
                                                enabled: memoryModel.moving_note !== modelData.index
                                                onClicked: memoryModel.open_move(modelData.text, modelData.index)
                                            }

                                            Button {
                                                visible: memoryModel.confirm_remove_note !== modelData.index
                                                text: "Remove"
                                                onClicked: memoryModel.confirm_remove_note = modelData.index
                                            }

                                            Button {
                                                visible: memoryModel.confirm_remove_note === modelData.index
                                                text: memoryModel.removing_note === modelData.index ? "Removing…" : "Confirm"
                                                enabled: memoryModel.removing_note !== modelData.index
                                                onClicked: memoryModel.remove_note(modelData.index)
                                            }

                                            Button {
                                                visible: memoryModel.confirm_remove_note === modelData.index
                                                text: "Cancel"
                                                enabled: memoryModel.removing_note !== modelData.index
                                                onClicked: memoryModel.confirm_remove_note = -1
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }

                // Sidebar (right)
                Rectangle {
                    Layout.preferredWidth: 300
                    Layout.fillHeight: true
                    color: Theme.colors.bgWell
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    ScrollView {
                        anchors.fill: parent
                        ScrollBar.horizontal.policy: ScrollBar.AlwaysOff
                        clip: true

                        ColumnLayout {
                            width: parent.width
                            spacing: Theme.space.xl
                            anchors.margins: Theme.space.xl

                            // User profile section (global only)
                            ColumnLayout {
                                visible: memoryModel.is_global
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

                                    Rectangle {
                                        implicitWidth: userMdLabel.implicitWidth + Theme.space.sm * 2
                                        implicitHeight: 18
                                        radius: Theme.radius.sm
                                        color: Theme.colors.brandBg || "transparent"
                                        border.width: 1
                                        border.color: Theme.colors.brand
                                        Label {
                                            id: userMdLabel
                                            anchors.centerIn: parent
                                            text: "USER.md"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.micro
                                            font.weight: Theme.fontWeight.medium
                                            color: Theme.colors.brand
                                        }
                                    }

                                    Button {
                                        visible: !memoryModel.editing_profile
                                        text: "Edit"
                                        onClicked: memoryModel.start_profile()
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
                                    visible: !!(memoryModel.current && memoryModel.current.profileLearned)
                                    Layout.fillWidth: true
                                    implicitHeight: learnedCol.implicitHeight
                                    color: Theme.colors.bgRaised
                                    radius: Theme.radius.md
                                    border.width: 1
                                    border.color: Theme.colors.borderSubtle

                                    ColumnLayout {
                                        id: learnedCol
                                        anchors.fill: parent
                                        anchors.margins: Theme.space.lg
                                        spacing: Theme.space.sm

                                        RowLayout {
                                            spacing: Theme.space.sm

                                            Label {
                                                text: "✧ learned by eigen"
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.label
                                                font.weight: Theme.fontWeight.semibold
                                                color: Theme.colors.brand
                                            }

                                            Label {
                                                text: "auto-maintained from your sessions"
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textFaint
                                            }
                                        }

                                        MarkdownBlocks {
                                            Layout.fillWidth: true
                                            blocks: memoryModel.current ? parseMarkdown(memoryModel.current.profileLearned || "") : []
                                        }
                                    }
                                }

                                // Profile editor
                                ColumnLayout {
                                    visible: memoryModel.editing_profile
                                    Layout.fillWidth: true
                                    spacing: Theme.space.md

                                    ScrollView {
                                        Layout.fillWidth: true
                                        Layout.preferredHeight: Math.min(profileTextArea.contentHeight + Theme.space.md * 2, 300)
                                        ScrollBar.horizontal.policy: ScrollBar.AlwaysOff

                                        TextArea {
                                            id: profileTextArea
                                            text: memoryModel.profile_draft
                                            placeholderText: "Add your own notes — eigen keeps the rest current…"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.bodySm
                                            color: Theme.colors.textPrimary
                                            wrapMode: Text.Wrap
                                            background: Rectangle {
                                                color: Theme.colors.bgRaised
                                                border.width: 1
                                                border.color: profileTextArea.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                                                radius: Theme.radius.md
                                            }

                                            onTextChanged: memoryModel.profile_draft = text
                                        }
                                    }

                                    RowLayout {
                                        spacing: Theme.space.sm

                                        Button {
                                            text: "Cancel"
                                            onClicked: memoryModel.editing_profile = false
                                        }

                                        Button {
                                            text: memoryModel.saving_profile ? "Saving…" : "Save"
                                            enabled: !memoryModel.saving_profile
                                            onClicked: memoryModel.save_profile()
                                        }
                                    }
                                }

                                // Profile view (not editing)
                                Rectangle {
                                    visible: !!(!memoryModel.editing_profile && memoryModel.current && memoryModel.current.profile)
                                    Layout.fillWidth: true
                                    implicitHeight: profileBlocks.implicitHeight
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
                                        blocks: memoryModel.current ? parseMarkdown(memoryModel.current.profile || "") : []
                                    }
                                }

                                Label {
                                    visible: !memoryModel.editing_profile && (!memoryModel.current || !memoryModel.current.profile)
                                    text: "Nothing learned yet. Add your own."
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textMuted

                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: memoryModel.start_profile()
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

                                    Rectangle {
                                        visible: !!(memoryModel.current && memoryModel.current.banList && memoryModel.current.banList.length > 0)
                                        implicitWidth: enforcedLabel.implicitWidth + Theme.space.sm * 2
                                        implicitHeight: 18
                                        radius: Theme.radius.sm
                                        color: Theme.colors.errorBg
                                        border.width: 1
                                        border.color: Theme.colors.error
                                        Label {
                                            id: enforcedLabel
                                            anchors.centerIn: parent
                                            text: "enforced"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.micro
                                            font.weight: Theme.fontWeight.medium
                                            color: Theme.colors.error
                                        }
                                    }

                                    Button {
                                        visible: !memoryModel.adding_ban
                                        text: "Add"
                                        onClicked: memoryModel.adding_ban = true
                                    }
                                }

                                // Add ban form
                                ColumnLayout {
                                    visible: memoryModel.adding_ban
                                    Layout.fillWidth: true
                                    spacing: Theme.space.md

                                    TextField {
                                        id: banTitleField
                                        Layout.fillWidth: true
                                        placeholderText: "Short title"
                                        text: memoryModel.ban_title
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textPrimary
                                        background: Rectangle {
                                            color: Theme.colors.bgRaised
                                            border.width: 1
                                            border.color: banTitleField.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                                            radius: Theme.radius.md
                                        }

                                        onTextChanged: memoryModel.ban_title = text
                                    }

                                    ScrollView {
                                        Layout.fillWidth: true
                                        Layout.preferredHeight: Math.min(banRuleTextArea.contentHeight + Theme.space.md * 2, 150)
                                        ScrollBar.horizontal.policy: ScrollBar.AlwaysOff

                                        TextArea {
                                            id: banRuleTextArea
                                            text: memoryModel.ban_rule
                                            placeholderText: "What the agent must not do (" + memoryModel.scope_label + " scope)…"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.bodySm
                                            color: Theme.colors.textPrimary
                                            wrapMode: Text.Wrap
                                            background: Rectangle {
                                                color: Theme.colors.bgRaised
                                                border.width: 1
                                                border.color: banRuleTextArea.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                                                radius: Theme.radius.md
                                            }

                                            onTextChanged: memoryModel.ban_rule = text
                                        }
                                    }

                                    RowLayout {
                                        spacing: Theme.space.sm

                                        Button {
                                            text: "Cancel"
                                            onClicked: {
                                                memoryModel.adding_ban = false
                                                memoryModel.ban_title = ""
                                                memoryModel.ban_rule = ""
                                            }
                                        }

                                        Button {
                                            text: memoryModel.saving_ban ? "Saving…" : "Save ban"
                                            enabled: memoryModel.ban_title.trim().length > 0 && memoryModel.ban_rule.trim().length > 0 && !memoryModel.saving_ban
                                            onClicked: memoryModel.add_ban()
                                        }
                                    }
                                }

                                // Bans list
                                Repeater {
                                    model: memoryModel.current ? memoryModel.current.banList : []
                                    delegate: Rectangle {
                                        Layout.fillWidth: true
                                        implicitHeight: banRow.implicitHeight
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

                                            Button {
                                                text: memoryModel.removing_ban === modelData.title ? "…" : "✕"
                                                enabled: memoryModel.removing_ban !== modelData.title
                                                onClicked: memoryModel.remove_ban(modelData.title)
                                                Layout.preferredWidth: 24
                                                Layout.preferredHeight: 24
                                                flat: true
                                            }
                                        }
                                    }
                                }

                                Label {
                                    visible: !memoryModel.adding_ban && (!memoryModel.current || !memoryModel.current.banList || memoryModel.current.banList.length === 0)
                                    text: "No bans in this scope. Add one."
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textMuted

                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: memoryModel.adding_ban = true
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
                                        text: memoryModel.current ? memoryModel.current.noteCount : 0
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
                                        text: memoryModel.current && memoryModel.current.adHoc ? memoryModel.current.adHoc.length : 0
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
                                            visible: !memoryModel.current || memoryModel.current.backups === 0
                                            text: "0"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.bodySm
                                            color: Theme.colors.textSecondary
                                            horizontalAlignment: Text.AlignRight
                                            Layout.fillWidth: true
                                        }

                                        Button {
                                            visible: !!(memoryModel.current && memoryModel.current.backups > 0)
                                            text: memoryModel.current ? memoryModel.current.backups + " ▸" : "0"
                                            flat: true
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.bodySm
                                            contentItem: Label {
                                                text: parent.text
                                                font: parent.font
                                                color: Theme.colors.brand
                                                horizontalAlignment: Text.AlignRight
                                            }
                                            onClicked: memoryModel.toggle_backups()
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
                                        text: memoryModel.current ? (memoryModel.current.bytes / 1024).toFixed(1) + " KB" : "0 KB"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textSecondary
                                        horizontalAlignment: Text.AlignRight
                                        Layout.fillWidth: true
                                    }
                                }

                                // Backups list
                                ColumnLayout {
                                    visible: memoryModel.backups_open
                                    Layout.fillWidth: true
                                    spacing: Theme.space.sm

                                    Label {
                                        visible: memoryModel.backups_loading && memoryModel.backup_paths.length === 0
                                        text: "Loading backups…"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                    }

                                    Label {
                                        visible: !memoryModel.backups_loading && memoryModel.backup_paths.length === 0
                                        text: "No backup snapshots on disk."
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textMuted
                                    }

                                    Repeater {
                                        model: memoryModel.backup_paths
                                        delegate: Rectangle {
                                            Layout.fillWidth: true
                                            implicitHeight: backupCol.implicitHeight
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
                                                    text: memoryModel.backup_when(modelData)
                                                    font.family: Theme.uiFonts[0]
                                                    font.pixelSize: Theme.fontSize.bodySm
                                                    color: Theme.colors.textSecondary
                                                }

                                                Label {
                                                    text: memoryModel.backup_name(modelData)
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
                    }
                }
            }
        }
    }

    // Move-to-scope dialog
    Dialog {
        id: moveDialog
        visible: memoryModel.move_open
        modal: true
        anchors.centerIn: parent
        width: 420
        title: "Move note to…"

        onClosed: memoryModel.move_open = false

        ColumnLayout {
            width: parent.width
            spacing: Theme.space.md

            Label {
                visible: !!memoryModel.move_pending
                text: memoryModel.move_pending ? memoryModel.move_pending.text : ""
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
                visible: memoryModel.move_targets.length === 0
                text: "No other scope to move into."
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textMuted
            }

            Repeater {
                model: memoryModel.move_targets
                delegate: Button {
                    Layout.fillWidth: true
                    text: modelData.name
                    onClicked: {
                        memoryModel.move_to(modelData.key, modelData.name)
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
                            text: modelData.dir ? memoryModel.short_dir(modelData.dir) : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            color: Theme.colors.textFaint
                        }
                    }
                }
            }
        }

        standardButtons: Dialog.Close
    }

    // Helper: find scope index by key
    function findScopeIndex(key) {
        for (var i = 0; i < memoryModel.scopes.length; i++) {
            if (memoryModel.scopes[i].key === key) {
                return i
            }
        }
        return 0
    }

    // Helper: parse markdown (calls Python markdown parser via context property)
    function parseMarkdown(source) {
        if (!source || !markdownParser) {
            return [{type: "para", content: source || ""}]
        }
        return markdownParser.parse(source)
    }
}
