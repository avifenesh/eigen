import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Skills view — capability gallery. Discovered SKILL.md skills as cards,
// grouped into source shelves (user/project/extra), with proposal review strip.
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property var skillsModel: null      // SkillsModel from Python
    property var proposalsModel: null   // ProposalsModel from Python

    // Filter state
    property string query: ""

    // Slide-over preview state
    property var openSkill: null  // {name, description, source, path}
    property string body: ""
    property bool bodyLoading: false
    property bool confirmRemove: false
    property bool removing: false

    // Add-skill controls
    property string addMode: "path"  // "path" or "github"
    property string addInput: ""
    property bool installing: false

    // Per-proposal acting guards (map proposal name → bool)
    property var acting: ({})

    // Paging for proposals and active skills
    readonly property int pageSize: 24
    property int proposalsShown: pageSize
    property int activeShown: pageSize

    // Inline component definitions
    component SkillCard: Rectangle {
        property string skillName: ""
        property string skillDescription: ""
        property string skillSource: ""
        property string skillPath: ""
        signal clicked()

        color: mouseArea.containsMouse ? Theme.colors.bgRaised2 : Theme.colors.bgRaised
        border.width: 1
        border.color: Theme.colors.borderHairline
        radius: Theme.radius.md
        implicitHeight: 92

        Behavior on color { ColorAnimation { duration: Theme.duration.fast } }

        MouseArea {
            id: mouseArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: parent.clicked()
        }

        RowLayout {
            anchors.fill: parent
            spacing: 0

            // Left rail — source tint
            Rectangle {
                Layout.preferredWidth: 2
                Layout.fillHeight: true
                Layout.topMargin: Theme.space.md
                Layout.bottomMargin: Theme.space.md
                radius: Theme.radius.sm
                color: sourceTint(skillSource)
                opacity: mouseArea.containsMouse ? 1.0 : 0.55

                Behavior on opacity { NumberAnimation { duration: Theme.duration.fast } }
            }

            ColumnLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                Layout.margins: Theme.space.lg
                spacing: Theme.space.sm

                RowLayout {
                    spacing: Theme.space.sm

                    Label {
                        text: skillName
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }

                    Rectangle {
                        Layout.preferredWidth: sourceLabel.implicitWidth + Theme.space.md
                        Layout.preferredHeight: 20
                        radius: Theme.radius.sm
                        color: sourceBackground(skillSource)
                        border.width: 1
                        border.color: sourceBorder(skillSource)

                        Label {
                            id: sourceLabel
                            anchors.centerIn: parent
                            text: skillSource
                            font.pixelSize: Theme.fontSize.micro
                            font.weight: Theme.fontWeight.semibold
                            color: sourceTint(skillSource)
                        }
                    }
                }

                Label {
                    text: skillDescription
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    wrapMode: Text.WordWrap
                    maximumLineCount: 2
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }
            }
        }
    }

    component ProposalCard: Rectangle {
        property string proposalName: ""
        property string proposalDescription: ""
        property bool isActing: false
        signal accepted()
        signal rejected()

        implicitWidth: 268
        implicitHeight: contentLayout.implicitHeight + Theme.space.lg * 2
        color: Theme.colors.bgBase
        border.width: 1
        border.color: Qt.rgba(
            Theme.colors.working.r,
            Theme.colors.working.g,
            Theme.colors.working.b,
            0.16
        )
        radius: Theme.radius.md

        ColumnLayout {
            id: contentLayout
            anchors.fill: parent
            anchors.margins: Theme.space.lg
            spacing: Theme.space.sm

            Label {
                text: proposalName
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.body
                font.weight: Theme.fontWeight.semibold
                color: Theme.colors.textPrimary
                wrapMode: Text.NoWrap
                elide: Text.ElideRight
                Layout.fillWidth: true
            }

            Label {
                text: proposalDescription
                font.pixelSize: Theme.fontSize.bodySm
                color: Theme.colors.textMuted
                wrapMode: Text.WordWrap
                maximumLineCount: 3
                elide: Text.ElideRight
                Layout.fillWidth: true
            }

            RowLayout {
                Layout.topMargin: Theme.space.xs
                spacing: Theme.space.sm

                Button {
                    text: "Accept"
                    enabled: !isActing
                    onClicked: accepted()

                    background: Rectangle {
                        color: parent.down ? Theme.colors.brandStrong : (parent.hovered ? Theme.colors.brand : Theme.colors.brandDim)
                        radius: Theme.radius.sm
                        opacity: parent.enabled ? 1.0 : 0.5
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

                    Layout.preferredHeight: 28
                    Layout.fillWidth: true
                }

                Button {
                    text: "Reject"
                    enabled: !isActing
                    onClicked: rejected()

                    background: Rectangle {
                        color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                        radius: Theme.radius.sm
                        border.width: 1
                        border.color: Theme.colors.borderSubtle
                        opacity: parent.enabled ? 1.0 : 0.5
                        Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
                    }

                    contentItem: Label {
                        text: parent.text
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textSecondary
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }

                    Layout.preferredHeight: 28
                    Layout.fillWidth: true
                }
            }
        }
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // HEADER — filter box + add controls
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 64
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.margins: Theme.space.lg
                spacing: Theme.space.lg

                // Filter box
                RowLayout {
                    Layout.fillWidth: true
                    Layout.minimumWidth: 200
                    Layout.maximumWidth: 420
                    spacing: Theme.space.sm

                    TextField {
                        id: filterInput
                        Layout.fillWidth: true
                        placeholderText: "Filter skills…"
                        text: root.query
                        onTextChanged: root.query = text

                        background: Rectangle {
                            color: Theme.colors.bgRaised
                            border.width: 1
                            border.color: filterInput.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                            radius: Theme.radius.md

                            Behavior on border.color { ColorAnimation { duration: Theme.duration.fast } }
                        }

                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textPrimary

                        leftPadding: Theme.space.lg
                        rightPadding: Theme.space.lg
                    }

                    Label {
                        visible: root.skillsModel
                        text: filteredSkills().length
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.label
                        color: Theme.colors.textFaint
                    }
                }

                Item { Layout.fillWidth: true }

                // Add controls — source toggle + input + Add button
                RowLayout {
                    spacing: Theme.space.sm

                    // Segmented source toggle
                    RowLayout {
                        spacing: 0

                        Repeater {
                            model: ["path", "github"]
                            delegate: Button {
                                text: modelData === "path" ? "Path" : "GitHub"
                                checkable: true
                                checked: root.addMode === modelData
                                onClicked: root.addMode = modelData

                                background: Rectangle {
                                    color: parent.checked ? Theme.colors.bgRaised : "transparent"
                                    border.width: 1
                                    border.color: Theme.colors.borderSubtle
                                    radius: index === 0 ? Theme.radius.sm : 0
                                    // Right-most button gets right radius
                                    Rectangle {
                                        visible: index === 1
                                        anchors.right: parent.right
                                        width: Theme.radius.sm
                                        height: parent.height
                                        color: parent.color
                                        border.width: 0
                                        radius: Theme.radius.sm
                                    }

                                    Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
                                }

                                contentItem: Label {
                                    text: parent.text
                                    font.pixelSize: Theme.fontSize.bodySm
                                    font.weight: parent.checked ? Theme.fontWeight.medium : Theme.fontWeight.regular
                                    color: parent.checked ? Theme.colors.textPrimary : Theme.colors.textMuted
                                    horizontalAlignment: Text.AlignHCenter
                                    verticalAlignment: Text.AlignVCenter
                                }

                                Layout.preferredHeight: 32
                                Layout.preferredWidth: 64
                            }
                        }
                    }

                    TextField {
                        id: addInputField
                        Layout.preferredWidth: 220
                        placeholderText: root.addMode === "path" ? "/path/to/skill or SKILL.md" : "owner/repo"
                        text: root.addInput
                        onTextChanged: root.addInput = text
                        enabled: !root.installing

                        background: Rectangle {
                            color: Theme.colors.bgRaised
                            border.width: 1
                            border.color: addInputField.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                            radius: Theme.radius.md
                            opacity: addInputField.enabled ? 1.0 : 0.6

                            Behavior on border.color { ColorAnimation { duration: Theme.duration.fast } }
                        }

                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textPrimary

                        leftPadding: Theme.space.lg
                        rightPadding: Theme.space.lg

                        Keys.onReturnPressed: {
                            if (root.addInput.trim() !== "" && !root.installing) {
                                installSkill()
                            }
                        }
                    }

                    Button {
                        text: root.installing ? "Installing…" : "Add"
                        enabled: root.addInput.trim() !== "" && !root.installing
                        onClicked: installSkill()

                        background: Rectangle {
                            color: parent.down ? Theme.colors.brandStrong : (parent.hovered ? Theme.colors.brand : Theme.colors.brandDim)
                            radius: Theme.radius.sm
                            opacity: parent.enabled ? 1.0 : 0.5
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
                        Layout.preferredWidth: 72
                    }
                }
            }
        }

        // SCROLLABLE CONTENT
        Flickable {
            Layout.fillWidth: true
            Layout.fillHeight: true
            contentWidth: width
            contentHeight: contentColumn.implicitHeight
            clip: true

            ColumnLayout {
                id: contentColumn
                width: Math.min(parent.width - Theme.space.xxxxl, 1080)
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: Theme.space.xxxl

                Item { height: Theme.space.lg }

                // PROPOSALS STRIP (if any)
                Rectangle {
                    visible: root.proposalsModel && root.proposalsModel.rowCount() > 0
                    Layout.fillWidth: true
                    implicitHeight: proposalsContent.implicitHeight + Theme.space.xxxl
                    color: "transparent"
                    border.width: 1
                    border.color: Qt.rgba(
                        Theme.colors.working.r,
                        Theme.colors.working.g,
                        Theme.colors.working.b,
                        0.24
                    )
                    radius: Theme.radius.lg

                    gradient: Gradient {
                        GradientStop { position: 0; color: Theme.colors.workingBg }
                        GradientStop { position: 0.64; color: "transparent" }
                    }

                    // Left rail
                    Rectangle {
                        anchors.left: parent.left
                        anchors.top: parent.top
                        anchors.bottom: parent.bottom
                        width: 3
                        radius: parent.radius
                        gradient: Gradient {
                            GradientStop { position: 0; color: Theme.colors.working }
                            GradientStop {
                                position: 1
                                color: Qt.rgba(
                                    Theme.colors.working.r,
                                    Theme.colors.working.g,
                                    Theme.colors.working.b,
                                    0.3
                                )
                            }
                        }
                    }

                    ColumnLayout {
                        id: proposalsContent
                        anchors.fill: parent
                        anchors.leftMargin: Theme.space.xxxl
                        anchors.rightMargin: Theme.space.lg
                        anchors.topMargin: Theme.space.lg
                        anchors.bottomMargin: Theme.space.lg
                        spacing: Theme.space.lg

                        // Header
                        RowLayout {
                            spacing: Theme.space.sm

                            // Pulse dot
                            Rectangle {
                                width: 7
                                height: 7
                                radius: 3.5
                                color: Theme.colors.working

                                SequentialAnimation on opacity {
                                    running: true
                                    loops: Animation.Infinite
                                    NumberAnimation { from: 1.0; to: 0.45; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
                                    NumberAnimation { from: 0.45; to: 1.0; duration: Theme.duration.breath / 2; easing.type: Easing.InOutQuad }
                                }
                            }

                            Label {
                                text: "AWAITING REVIEW"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                font.weight: Theme.fontWeight.semibold
                                font.capitalization: Font.AllUppercase
                                color: Theme.colors.working
                            }

                            Rectangle {
                                Layout.preferredWidth: countLabel.implicitWidth + Theme.space.md
                                Layout.preferredHeight: 20
                                radius: Theme.radius.sm
                                color: Theme.colors.warnBg
                                border.width: 1
                                border.color: Qt.rgba(
                                    Theme.colors.warn.r,
                                    Theme.colors.warn.g,
                                    Theme.colors.warn.b,
                                    0.3
                                )

                                Label {
                                    id: countLabel
                                    anchors.centerIn: parent
                                    text: root.proposalsModel ? root.proposalsModel.rowCount() : 0
                                    font.pixelSize: Theme.fontSize.micro
                                    font.weight: Theme.fontWeight.bold
                                    color: Theme.colors.warn
                                }
                            }
                        }

                        // Horizontal scroll of proposals
                        Flickable {
                            Layout.fillWidth: true
                            Layout.preferredHeight: 160
                            contentWidth: proposalsRow.implicitWidth
                            contentHeight: height
                            clip: true

                            Row {
                                id: proposalsRow
                                spacing: Theme.space.lg

                                Repeater {
                                    model: visibleProposals()
                                    delegate: ProposalCard {
                                        proposalName: modelData.name
                                        proposalDescription: modelData.description
                                        isActing: root.acting[modelData.name] || false

                                        onAccepted: {
                                            var name = modelData.name
                                            var newActing = root.acting
                                            newActing[name] = true
                                            root.acting = newActing

                                            root.proposalsModel.accept(name)

                                            // Clear acting state after a short delay
                                            Qt.callLater(function() {
                                                var updatedActing = root.acting
                                                delete updatedActing[name]
                                                root.acting = updatedActing
                                            })
                                        }

                                        onRejected: {
                                            var name = modelData.name
                                            var newActing = root.acting
                                            newActing[name] = true
                                            root.acting = newActing

                                            root.proposalsModel.reject(name)

                                            Qt.callLater(function() {
                                                var updatedActing = root.acting
                                                delete updatedActing[name]
                                                root.acting = updatedActing
                                            })
                                        }
                                    }
                                }

                                // "Show more" tile
                                Rectangle {
                                    visible: root.proposalsModel && root.proposalsShown < root.proposalsModel.rowCount()
                                    implicitWidth: 132
                                    implicitHeight: 160
                                    color: moreMouseArea.containsMouse ? Theme.colors.workingBg : "transparent"
                                    border.width: 1
                                    border.color: Qt.rgba(
                                        Theme.colors.working.r,
                                        Theme.colors.working.g,
                                        Theme.colors.working.b,
                                        0.3
                                    )
                                    radius: Theme.radius.md

                                    Behavior on color { ColorAnimation { duration: Theme.duration.fast } }

                                    MouseArea {
                                        id: moreMouseArea
                                        anchors.fill: parent
                                        hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: root.proposalsShown += root.pageSize
                                    }

                                    ColumnLayout {
                                        anchors.centerIn: parent
                                        spacing: Theme.space.xs

                                        Label {
                                            text: "+" + (root.proposalsModel.rowCount() - root.proposalsShown)
                                            font.pixelSize: Theme.fontSize.h2
                                            font.weight: Theme.fontWeight.bold
                                            color: Theme.colors.working
                                            Layout.alignment: Qt.AlignHCenter
                                        }

                                        Label {
                                            text: "more to review"
                                            font.pixelSize: Theme.fontSize.label
                                            color: Theme.colors.textMuted
                                            Layout.alignment: Qt.AlignHCenter
                                        }
                                    }
                                }
                            }
                        }
                    }
                }

                // ACTIVE SKILLS — shelves
                ColumnLayout {
                    visible: filteredSkills().length > 0
                    Layout.fillWidth: true
                    spacing: Theme.space.xxxl

                    Repeater {
                        model: visibleShelves()
                        delegate: ColumnLayout {
                            Layout.fillWidth: true
                            spacing: Theme.space.lg

                            // Shelf header
                            RowLayout {
                                spacing: Theme.space.sm

                                Rectangle {
                                    width: 8
                                    height: 8
                                    radius: 4
                                    color: sourceTint(modelData.source)
                                    border.width: 3
                                    border.color: Qt.rgba(
                                        sourceTint(modelData.source).r,
                                        sourceTint(modelData.source).g,
                                        sourceTint(modelData.source).b,
                                        0.2
                                    )
                                }

                                Label {
                                    text: shelfLabel(modelData.source)
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.semibold
                                    font.capitalization: Font.AllUppercase
                                    color: Theme.colors.textSecondary
                                }

                                Label {
                                    text: modelData.total
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textFaint
                                }
                            }

                            // Shelf grid
                            GridLayout {
                                Layout.fillWidth: true
                                columns: 2
                                columnSpacing: Theme.space.lg
                                rowSpacing: Theme.space.lg

                                Repeater {
                                    model: modelData.skills
                                    delegate: SkillCard {
                                        skillName: modelData.name
                                        skillDescription: modelData.description
                                        skillSource: modelData.source
                                        skillPath: modelData.path
                                        Layout.fillWidth: true

                                        onClicked: {
                                            openPreview(modelData)
                                        }
                                    }
                                }
                            }
                        }
                    }

                    // "Show more" button
                    Button {
                        visible: root.activeShown < filteredSkills().length
                        text: "Show " + Math.min(root.pageSize, filteredSkills().length - root.activeShown) + " more · " + (filteredSkills().length - root.activeShown) + " remaining"
                        onClicked: root.activeShown += root.pageSize
                        Layout.alignment: Qt.AlignHCenter
                        Layout.topMargin: Theme.space.lg

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

                // Empty state — no skills match filter
                Label {
                    visible: filteredSkills().length === 0 && root.query !== ""
                    text: "No skills match \"" + root.query + "\"."
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    Layout.alignment: Qt.AlignHCenter
                }

                // Empty state — no skills at all
                Label {
                    visible: !root.skillsModel || (root.skillsModel.rowCount() === 0 && (!root.proposalsModel || root.proposalsModel.rowCount() === 0))
                    text: "No skills yet — add one above."
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    Layout.alignment: Qt.AlignHCenter
                }

                Item { height: Theme.space.xxxl }
            }
        }
    }

    // SLIDE-OVER PREVIEW (sheet + scrim)
    Rectangle {
        id: scrim
        visible: root.openSkill !== null
        anchors.fill: parent
        color: Qt.rgba(0, 0, 0, 0.6)
        z: 50

        MouseArea {
            anchors.fill: parent
            onClicked: closePreview()
        }

        NumberAnimation on opacity {
            running: scrim.visible
            from: 0
            to: 1
            duration: Theme.duration.fast
        }
    }

    Rectangle {
        id: sheet
        visible: root.openSkill !== null
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        width: Math.min(560, parent.width * 0.8)
        color: Theme.colors.bgRaised
        border.width: 1
        border.color: Theme.colors.borderSubtle
        z: 51

        NumberAnimation on x {
            running: sheet.visible
            from: sheet.parent.width
            to: sheet.parent.width - sheet.width
            duration: Theme.duration.base
        }

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: Theme.space.xxxl
            spacing: Theme.space.lg

            // Header
            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.space.lg

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Theme.space.sm

                    RowLayout {
                        spacing: Theme.space.sm

                        Label {
                            text: root.openSkill ? root.openSkill.name : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h2
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.fillWidth: true
                        }

                        Rectangle {
                            visible: root.openSkill
                            Layout.preferredWidth: sourceSheetLabel.implicitWidth + Theme.space.md
                            Layout.preferredHeight: 20
                            radius: Theme.radius.sm
                            color: sourceBackground(root.openSkill ? root.openSkill.source : "")
                            border.width: 1
                            border.color: sourceBorder(root.openSkill ? root.openSkill.source : "")

                            Label {
                                id: sourceSheetLabel
                                anchors.centerIn: parent
                                text: root.openSkill ? root.openSkill.source : ""
                                font.pixelSize: Theme.fontSize.micro
                                font.weight: Theme.fontWeight.semibold
                                color: sourceTint(root.openSkill ? root.openSkill.source : "")
                            }
                        }
                    }

                    Label {
                        text: root.openSkill ? root.openSkill.description : ""
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textSecondary
                        wrapMode: Text.WordWrap
                        Layout.fillWidth: true
                    }

                    Label {
                        text: root.openSkill ? root.openSkill.path : ""
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        color: Theme.colors.textFaint
                        wrapMode: Text.WrapAnywhere
                        Layout.fillWidth: true
                    }
                }

                ColumnLayout {
                    spacing: Theme.space.sm

                    // Remove button (user skills only)
                    RowLayout {
                        visible: root.openSkill && root.openSkill.source === "user"
                        spacing: Theme.space.sm

                        Button {
                            visible: root.confirmRemove
                            text: root.removing ? "Removing…" : "Confirm"
                            enabled: !root.removing
                            onClicked: removeSkill()

                            background: Rectangle {
                                color: parent.down ? Qt.darker(Theme.colors.error, 1.2) : (parent.hovered ? Theme.colors.error : Qt.darker(Theme.colors.error, 1.4))
                                radius: Theme.radius.sm
                                opacity: parent.enabled ? 1.0 : 0.5
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
                            Layout.preferredWidth: 84
                        }

                        Button {
                            text: root.confirmRemove ? "Cancel" : "Remove"
                            enabled: !root.removing
                            onClicked: {
                                if (root.confirmRemove) {
                                    root.confirmRemove = false
                                } else {
                                    root.confirmRemove = true
                                }
                            }

                            background: Rectangle {
                                color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                                radius: Theme.radius.sm
                                border.width: 1
                                border.color: Theme.colors.borderSubtle
                                opacity: parent.enabled ? 1.0 : 0.5
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
                            Layout.preferredWidth: 72
                        }
                    }

                    // Close button
                    Button {
                        text: "✕"
                        onClicked: closePreview()

                        background: Rectangle {
                            color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                            radius: Theme.radius.sm
                            Behavior on color { ColorAnimation { duration: Theme.duration.fast } }
                        }

                        contentItem: Label {
                            text: parent.text
                            font.pixelSize: 15
                            color: parent.hovered ? Theme.colors.textPrimary : Theme.colors.textGhost
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                        }

                        Layout.preferredWidth: 32
                        Layout.preferredHeight: 32
                    }
                }
            }

            // Divider
            Rectangle {
                Layout.fillWidth: true
                Layout.preferredHeight: 1
                color: Theme.colors.divider
            }

            // Body content
            Flickable {
                Layout.fillWidth: true
                Layout.fillHeight: true
                contentWidth: width
                contentHeight: bodyColumn.implicitHeight
                clip: true

                ColumnLayout {
                    id: bodyColumn
                    width: parent.width
                    spacing: Theme.space.md

                    Label {
                        visible: root.bodyLoading
                        text: "Loading…"
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textMuted
                    }

                    Label {
                        visible: !root.bodyLoading && root.body === ""
                        text: "No body content."
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textMuted
                    }

                    // Markdown body (use MarkdownBlocks if body is parsed; for now just plain text)
                    Label {
                        visible: !root.bodyLoading && root.body !== ""
                        text: root.body
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textPrimary
                        wrapMode: Text.Wrap
                        textFormat: Text.PlainText
                        Layout.fillWidth: true
                    }
                }
            }
        }
    }

    // Helper functions
    function filteredSkills() {
        if (!root.skillsModel) return []
        var q = root.query.trim().toLowerCase()
        var all = []
        for (var i = 0; i < root.skillsModel.rowCount(); i++) {
            var idx = root.skillsModel.index(i, 0)
            var skill = {
                name: root.skillsModel.data(idx, 257),  // NameRole
                description: root.skillsModel.data(idx, 258),
                source: root.skillsModel.data(idx, 259),
                path: root.skillsModel.data(idx, 260)
            }
            if (!q || skill.name.toLowerCase().indexOf(q) >= 0 || skill.description.toLowerCase().indexOf(q) >= 0) {
                all.push(skill)
            }
        }
        return all
    }

    function visibleShelves() {
        var filtered = filteredSkills()
        var bySource = {}
        for (var i = 0; i < filtered.length; i++) {
            var s = filtered[i].source
            if (!bySource[s]) bySource[s] = []
            bySource[s].push(filtered[i])
        }

        var shelfOrder = ["user", "project", "extra"]
        var shelves = []
        var budget = root.activeShown

        for (var j = 0; j < shelfOrder.length; j++) {
            var src = shelfOrder[j]
            if (bySource[src] && bySource[src].length > 0) {
                var take = Math.min(budget, bySource[src].length)
                shelves.push({
                    source: src,
                    total: bySource[src].length,
                    skills: bySource[src].slice(0, take)
                })
                budget -= take
                if (budget <= 0) break
            }
        }

        // Handle unexpected sources
        for (var src2 in bySource) {
            if (shelfOrder.indexOf(src2) < 0 && budget > 0) {
                var take2 = Math.min(budget, bySource[src2].length)
                shelves.push({
                    source: src2,
                    total: bySource[src2].length,
                    skills: bySource[src2].slice(0, take2)
                })
                budget -= take2
            }
        }

        return shelves
    }

    function visibleProposals() {
        if (!root.proposalsModel) return []
        var proposals = []
        var count = Math.min(root.proposalsShown, root.proposalsModel.rowCount())
        for (var i = 0; i < count; i++) {
            var idx = root.proposalsModel.index(i, 0)
            proposals.push({
                name: root.proposalsModel.data(idx, 257),  // NameRole
                description: root.proposalsModel.data(idx, 258),
                path: root.proposalsModel.data(idx, 259)
            })
        }
        return proposals
    }

    function shelfLabel(source) {
        if (source === "user") return "Yours"
        if (source === "project") return "This project"
        if (source === "extra") return "Bundled"
        return source
    }

    function sourceTint(source) {
        if (source === "user") return Theme.colors.brand
        if (source === "project") return Theme.colors.accent
        return Theme.colors.textGhost
    }

    function sourceBackground(source) {
        if (source === "user") return Theme.colors.stateSelected
        if (source === "project") return Qt.rgba(
            Theme.colors.accent.r,
            Theme.colors.accent.g,
            Theme.colors.accent.b,
            0.1
        )
        return Theme.colors.stateHover
    }

    function sourceBorder(source) {
        if (source === "user") return Theme.colors.borderBrandFaint
        if (source === "project") return Qt.rgba(
            Theme.colors.accent.r,
            Theme.colors.accent.g,
            Theme.colors.accent.b,
            0.22
        )
        return Theme.colors.borderSubtle
    }

    function openPreview(skill) {
        root.openSkill = skill
        root.body = ""
        root.bodyLoading = true
        root.confirmRemove = false

        // Fetch skill body via RPC
        client.call("SkillBody", [skill.name], function(result) {
            root.bodyLoading = false
            if (result.error) {
                root.body = ""
            } else {
                root.body = result.result || ""
            }
        })
    }

    function closePreview() {
        root.openSkill = null
        root.body = ""
        root.confirmRemove = false
    }

    function removeSkill() {
        if (!root.openSkill) return
        root.removing = true
        var name = root.openSkill.name

        client.call("RemoveSkill", [name], function(result) {
            root.removing = false
            root.confirmRemove = false
            if (!result.error) {
                closePreview()
                // Refresh skills
                if (root.skillsModel) {
                    root.skillsModel.refresh()
                }
            }
        })
    }

    function installSkill() {
        var ref = root.addInput.trim()
        if (!ref || root.installing) return

        root.installing = true
        var method = root.addMode === "path" ? "InstallSkillFromPath" : "InstallSkillFromGitHub"

        client.call(method, [ref], function(result) {
            root.installing = false
            if (!result.error && result.result) {
                root.addInput = ""
                // Refresh skills
                if (root.skillsModel) {
                    root.skillsModel.refresh()
                }
            }
        })
    }

    // Reset activeShown when query changes
    onQueryChanged: {
        root.activeShown = root.pageSize
    }

    // Window visibility refresh (skills can be added externally)
    Connections {
        target: ApplicationWindow.window
        function onActiveChanged() {
            if (ApplicationWindow.window && ApplicationWindow.window.active) {
                if (root.skillsModel) root.skillsModel.refresh()
                if (root.proposalsModel) root.proposalsModel.refresh()
            }
        }
    }
}
