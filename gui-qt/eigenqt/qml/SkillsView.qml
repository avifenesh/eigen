import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Skills view — capability gallery. Discovered SKILL.md skills as cards,
// grouped into source shelves (user/project/extra), with proposal review strip.
Rectangle {
    id: root
    objectName: "skillsView"
    color: Theme.colors.bgBase

    property var skillsModel: null      // SkillsModel from Python
    property var proposalsModel: null   // ProposalsModel from Python
    property var rpcClient: null

    // Filter state
    property string query: ""
    property int modelEpoch: 0

    // Slide-over preview state
    property var openSkill: null  // {name, description, source, path}
    property string body: ""
    property var bodyBlocks: []
    property bool bodyLoading: false
    property bool confirmRemove: false
    property bool removing: false

    // Add-skill controls
    property string addMode: "path"  // "path" or "github"
    property string addInput: ""
    property bool installing: false
    property string actionError: ""

    // Per-proposal acting guards (map proposal name → bool)
    property var acting: ({})
    readonly property bool qaPreviewOpen: openSkill !== null
    readonly property int qaBodyBlockCount: bodyBlocks.length
    property int bodyToken: 0
    property int removeToken: 0
    property int installToken: 0

    // Paging for proposals and active skills
    readonly property int pageSize: 24
    readonly property real qaProposalStripHeight: proposalReviewStrip.visible ? proposalReviewStrip.height : 0
    readonly property real qaProposalScrollerHeight: proposalScroller.visible ? proposalScroller.height : 0
    readonly property int qaProposalCount: proposalCount()
    property int proposalsShown: pageSize
    property int activeShown: pageSize
    onSkillsModelChanged: {
        root.modelEpoch += 1
        syncActiveModels()
    }
    onProposalsModelChanged: {
        root.modelEpoch += 1
        syncActiveModels()
    }
    onVisibleChanged: syncActiveModels()

    Component.onCompleted: syncActiveModels()
    Component.onDestruction: syncActiveModels(false)

    // Inline component definitions
    component SkillCard: Rectangle {
        id: skillCard
        objectName: skillName ? "skillCard_" + skillName : ""
        property string skillName: ""
        property string skillDescription: ""
        property string skillSource: ""
        property string skillPath: ""
        property bool qaForceKeyboardFocus: false
        readonly property bool qaVisualFocus: activeFocus
        signal clicked()

        color: skillCard.activeFocus
            ? Theme.colors.stateFocusBg
            : (mouseArea.containsMouse ? Theme.colors.bgRaised2 : Theme.colors.bgRaised)
        border.width: skillCard.activeFocus ? 2 : 1
        border.color: skillCard.activeFocus ? Theme.colors.brandBright : Theme.colors.borderHairline
        radius: Theme.radius.md
        implicitHeight: 92
        activeFocusOnTab: true
        focusPolicy: Qt.StrongFocus

        Behavior on color { ColorAnimation { duration: Theme.duration.fast } }

        function openCard() {
            skillCard.clicked()
        }

        onQaForceKeyboardFocusChanged: {
            if (qaForceKeyboardFocus) {
                forceActiveFocus(Qt.TabFocusReason)
            }
        }

        Keys.onReturnPressed: openCard()
        Keys.onEnterPressed: openCard()
        Keys.onSpacePressed: openCard()

        MouseArea {
            id: mouseArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: skillCard.openCard()
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
                opacity: (mouseArea.containsMouse || skillCard.activeFocus) ? 1.0 : 0.55

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

                    AppTag {
                        text: skillSource
                        backgroundColor: sourceBackground(skillSource)
                        borderColor: sourceBorder(skillSource)
                        textColor: sourceTint(skillSource)
                        fontWeight: Theme.fontWeight.semibold
                        minimumHeight: 20
                        pill: false
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
        id: proposalCard
        objectName: proposalName ? "proposalCard_" + proposalName : ""
        property string proposalName: ""
        property string proposalDescription: ""
        property string actingAction: ""
        readonly property bool isActing: actingAction !== ""
        property bool qaForceKeyboardFocus: false
        readonly property bool qaVisualFocus: activeFocus
        signal accepted()
        signal rejected()

        implicitWidth: 268
        implicitHeight: contentLayout.implicitHeight + Theme.space.lg * 2
        color: proposalCard.activeFocus ? Theme.colors.stateFocusBg : Theme.colors.bgBase
        border.width: proposalCard.activeFocus ? 2 : 1
        border.color: proposalCard.activeFocus ? Theme.colors.brandBright : Theme.colors.workingBg
        radius: Theme.radius.md
        activeFocusOnTab: true
        focusPolicy: Qt.StrongFocus

        onQaForceKeyboardFocusChanged: {
            if (qaForceKeyboardFocus) {
                forceActiveFocus(Qt.TabFocusReason)
            }
        }

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

                AppButton {
                    objectName: proposalCard.proposalName ? "proposalAcceptButton_" + proposalCard.proposalName : ""
                    text: proposalCard.actingAction === "accept" ? "Accepting…" : "Accept"
                    toolTipText: "Accept " + proposalCard.proposalName
                    variant: "primary"
                    enabled: !isActing
                    onClicked: accepted()

                    Layout.preferredHeight: 28
                    Layout.fillWidth: true
                }

                AppButton {
                    objectName: proposalCard.proposalName ? "proposalRejectButton_" + proposalCard.proposalName : ""
                    text: proposalCard.actingAction === "reject" ? "Rejecting…" : "Reject"
                    toolTipText: "Reject " + proposalCard.proposalName
                    enabled: !isActing
                    onClicked: rejected()

                    Layout.preferredHeight: 28
                    Layout.fillWidth: true
                }
            }
        }
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // HEADER - title, filter, and add controls
        Rectangle {
            id: headerBar
            Layout.fillWidth: true
            Layout.preferredHeight: headerLayout.implicitHeight + Theme.space.xl * 2
            Layout.minimumHeight: 84
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline
            readonly property bool compact: width < 940

            GridLayout {
                id: headerLayout
                anchors.fill: parent
                anchors.margins: Theme.space.xl
                columns: headerBar.compact ? 1 : 2
                columnSpacing: Theme.space.xxxl
                rowSpacing: Theme.space.md

                ColumnLayout {
                    Layout.fillWidth: headerBar.compact
                    Layout.minimumWidth: 200
                    Layout.preferredWidth: headerBar.compact ? headerLayout.width : 460
                    spacing: Theme.space.sm

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.space.md

                        Label {
                            id: skillsTitle
                            objectName: "skillsHeaderTitle"
                            text: "Skills"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h2
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.fillWidth: true
                        }

                        AppTag {
                            visible: root.skillsModel
                            text: filteredSkills().length + (root.query.trim() === "" ? " active" : " shown")
                            backgroundColor: Theme.colors.brandBg
                            borderColor: Theme.colors.borderBrandFaint
                            textColor: Theme.colors.brandBright
                            fontWeight: Theme.fontWeight.semibold
                            minimumHeight: 24
                            pill: false
                        }
                    }

                    TextField {
                        id: filterInput
                        Layout.fillWidth: true
                        Layout.preferredHeight: 32
                        placeholderText: "Filter skills…"
                        placeholderTextColor: Theme.colors.textGhost
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
                }

                ColumnLayout {
                    Layout.fillWidth: true
                    Layout.minimumWidth: headerBar.compact ? 0 : 430
                    Layout.alignment: headerBar.compact ? Qt.AlignLeft : Qt.AlignRight
                    spacing: Theme.space.sm

                    Label {
                        text: "Add skill"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.label
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textMuted
                        Layout.alignment: headerBar.compact ? Qt.AlignLeft : Qt.AlignRight
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.space.sm

                        // Segmented source toggle
                        RowLayout {
                            spacing: 0

                            Repeater {
                                model: ["path", "github"]
                                delegate: AppButton {
                                    id: addModeButton
                                    objectName: "skillsAddMode_" + modelData
                                    text: modelData === "path" ? "Path" : "GitHub"
                                    toolTipText: modelData === "path" ? "Install from local path" : "Install from GitHub"
                                    selected: root.addMode === modelData
                                    compact: true
                                    segmentPosition: index === 0 ? "first" : "last"
                                    onClicked: {
                                        root.addMode = modelData
                                        root.actionError = ""
                                    }

                                    Layout.preferredHeight: 32
                                    Layout.preferredWidth: 64
                                }
                            }
                        }

                    TextField {
                        id: addInputField
                        objectName: "skillsAddInput"
                        Layout.fillWidth: true
                        Layout.minimumWidth: headerBar.compact ? 160 : 140
                        Layout.preferredWidth: 240
                        Layout.preferredHeight: 32
                        placeholderText: root.addMode === "path" ? "Skill path or SKILL.md" : "owner/repo"
                        placeholderTextColor: Theme.colors.textGhost
                        text: root.addInput
                        onTextChanged: root.addInput = text
                        enabled: !root.installing
                        property bool qaForceKeyboardFocus: false
                        readonly property bool qaVisualFocus: activeFocus
                        readonly property bool qaTextFits: !addInputField.contentItem || !addInputField.contentItem.text
                            || (!addInputField.contentItem.truncated
                                && addInputField.contentItem.paintedWidth <= addInputField.contentItem.width + 0.5)
                        readonly property string qaText: text || placeholderText
                        onQaForceKeyboardFocusChanged: {
                            if (qaForceKeyboardFocus) {
                                forceActiveFocus(Qt.TabFocusReason)
                            }
                        }

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
                            if (addInputField.text.trim() !== "" && !root.installing) {
                                installSkill(addInputField.text)
                            }
                        }
                    }

                    AppButton {
                        objectName: "skillsAddButton"
                        text: root.installing ? "Installing…" : "Add"
                        toolTipText: root.addMode === "path" ? "Install skill from path" : "Install skill from GitHub"
                        variant: "primary"
                        enabled: addInputField.text.trim() !== "" && !root.installing
                        onClicked: installSkill(addInputField.text)

                        Layout.preferredHeight: 32
                        Layout.preferredWidth: 72
                    }

                    AppButton {
                        objectName: "skillsAddClearButton"
                        text: "Clear"
                        toolTipText: "Clear add-skill input"
                        visible: root.addInput !== "" || root.actionError !== ""
                        enabled: !root.installing
                        onClicked: {
                            root.addInput = ""
                            root.actionError = ""
                        }

                        Layout.preferredHeight: 32
                        Layout.preferredWidth: 72
                    }
                    }
                }
            }
        }

        Rectangle {
            objectName: "skillsActionError"
            visible: root.actionError !== ""
            Layout.fillWidth: true
            Layout.preferredHeight: visible ? Math.max(40, skillsErrorText.implicitHeight + Theme.space.md) : 0
            color: Theme.colors.errorBg
            border.width: visible ? 1 : 0
            border.color: Theme.colors.error

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xl
                anchors.rightMargin: Theme.space.xl
                spacing: Theme.space.md

                Label {
                    id: skillsErrorText
                    text: root.actionError
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.error
                    wrapMode: Text.Wrap
                    Layout.fillWidth: true
                }

                AppButton {
                    objectName: "skillsDismissErrorButton"
                    text: "X"
                    toolTipText: "Dismiss skills action error"
                    compact: true
                    onClicked: root.actionError = ""
                    Layout.preferredWidth: 28
                    Layout.preferredHeight: 28
                }
            }
        }

        // SCROLLABLE CONTENT
        Flickable {
            objectName: "skillsFlick"
            Layout.fillWidth: true
            Layout.fillHeight: true
            contentWidth: width
            contentHeight: contentColumn.implicitHeight
            clip: true

            ColumnLayout {
                id: contentColumn
                width: Math.min(Math.max(parent.width - Theme.space.xxxxl, 0), 1080)
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: Theme.space.xxxl

                Item { height: Theme.space.lg }

                Rectangle {
                    objectName: "skillsLoadError"
                    visible: root.hasInitialLoadError()
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 132 : 0
                    color: Theme.colors.bgRaised
                    border.width: visible ? 1 : 0
                    border.color: Theme.colors.borderHairline
                    radius: Theme.radius.md

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.md

                        Label {
                            text: "Could not load skills"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            objectName: "skillsLoadErrorText"
                            text: root.skillsLoadError()
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            elide: Text.ElideRight
                            Layout.maximumWidth: 720
                            Layout.alignment: Qt.AlignHCenter
                        }

                        AppButton {
                            objectName: "skillsLoadErrorRetry"
                            text: "Retry"
                            compact: true
                            toolTipText: "Retry loading skills"
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: root.retryLoad()
                        }
                    }
                }

                Rectangle {
                    objectName: "skillsRefreshErrorBanner"
                    visible: root.hasRefreshLoadError()
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? Math.max(44, skillsRefreshErrorRow.implicitHeight + Theme.space.md) : 0
                    color: Theme.colors.errorBg
                    border.width: visible ? 1 : 0
                    border.color: Theme.colors.error
                    radius: Theme.radius.md
                    clip: true

                    RowLayout {
                        id: skillsRefreshErrorRow
                        anchors.fill: parent
                        anchors.leftMargin: Theme.space.lg
                        anchors.rightMargin: Theme.space.lg
                        spacing: Theme.space.md

                        Label {
                            objectName: "skillsRefreshErrorText"
                            text: root.skillsLoadError()
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.error
                            wrapMode: Text.WrapAnywhere
                            Layout.fillWidth: true
                        }

                        AppButton {
                            objectName: "skillsRefreshErrorRetry"
                            text: "Retry"
                            compact: true
                            toolTipText: "Retry loading skills"
                            Layout.preferredWidth: 64
                            Layout.preferredHeight: 28
                            onClicked: root.retryLoad()
                        }
                    }
                }

                // PROPOSALS STRIP (if any)
                Rectangle {
                    id: proposalReviewStrip
                    objectName: "skillsProposalReviewStrip"
                    visible: root.proposalsModel && root.proposalsModel.rowCount() > 0
                    Layout.fillWidth: true
                    implicitHeight: proposalsContent.implicitHeight + Theme.space.lg * 2
                    color: "transparent"
                    border.width: 1
                    border.color: Theme.colors.workingBg
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
                            GradientStop { position: 1; color: Theme.colors.workingBg }
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
                                    running: Theme.continuousMotion
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

                            AppTag {
                                text: proposalCount()
                                backgroundColor: Theme.colors.warnBg
                                borderColor: Theme.colors.warnBg
                                textColor: Theme.colors.warn
                                fontWeight: Theme.fontWeight.bold
                                minimumHeight: 20
                                pill: false
                            }
                        }

                        // Horizontal scroll of proposals
                        Flickable {
                            id: proposalScroller
                            objectName: "skillsProposalScroller"
                            Layout.fillWidth: true
                            Layout.preferredHeight: Math.max(96, proposalsRow.implicitHeight)
                            contentWidth: proposalsRow.implicitWidth
                            contentHeight: proposalsRow.implicitHeight
                            clip: true

                            Row {
                                id: proposalsRow
                                spacing: Theme.space.lg

                                Repeater {
                                    model: visibleProposals()
                                    delegate: ProposalCard {
                                        proposalName: modelData.name
                                        proposalDescription: modelData.description
                                        actingAction: root.acting[modelData.name] || ""

                                        onAccepted: {
                                            root.runProposalAction(modelData.name, "accept")
                                        }

                                        onRejected: {
                                            root.runProposalAction(modelData.name, "reject")
                                        }
                                    }
                                }

                                // "Show more" tile
                                Rectangle {
                                    visible: proposalRemaining() > 0
                                    implicitWidth: 132
                                    implicitHeight: 160
                                    color: moreMouseArea.containsMouse ? Theme.colors.workingBg : "transparent"
                                    border.width: 1
                                    border.color: Theme.colors.workingBg
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
                                            text: "+" + proposalRemaining()
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
                                    Layout.preferredWidth: 3
                                    Layout.preferredHeight: 14
                                    radius: 2
                                    color: sourceTint(modelData.source)
                                }

                                Label {
                                    text: shelfLabel(modelData.source)
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    font.weight: Theme.fontWeight.semibold
                                    font.capitalization: Font.AllUppercase
                                    color: Theme.colors.textSecondary
                                }

                                AppTag {
                                    text: modelData.total
                                    backgroundColor: Theme.colors.bgRaised
                                    borderColor: Theme.colors.borderHairline
                                    textColor: Theme.colors.textFaint
                                    fontWeight: Theme.fontWeight.semibold
                                    minimumHeight: 18
                                    pill: false
                                }
                            }

                            // Shelf grid
                            GridLayout {
                                Layout.fillWidth: true
                                columns: contentColumn.width < 720 ? 1 : 2
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
                    AppButton {
                        visible: root.activeShown < filteredSkills().length
                        text: "Show " + Math.min(root.pageSize, filteredSkills().length - root.activeShown) + " more · " + (filteredSkills().length - root.activeShown) + " remaining"
                        onClicked: root.activeShown += root.pageSize
                        Layout.alignment: Qt.AlignHCenter
                        Layout.topMargin: Theme.space.lg

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
                    visible: !root.hasInitialLoadError() && (!root.skillsModel || (root.skillsModel.rowCount() === 0 && (!root.proposalsModel || root.proposalsModel.rowCount() === 0)))
                    text: "No skills yet — add one above."
                    font.pixelSize: Theme.fontSize.bodySm
                    color: Theme.colors.textMuted
                    Layout.alignment: Qt.AlignHCenter
                }

                Item { height: Theme.space.xxxl }
            }
        }
    }

    Connections {
        target: root.skillsModel ? root.skillsModel : null
        function onModelReset() {
            root.modelEpoch += 1
        }
        function onRowsInserted() {
            root.modelEpoch += 1
        }
        function onRowsRemoved() {
            root.modelEpoch += 1
        }
    }

    Connections {
        target: root.proposalsModel ? root.proposalsModel : null
        function onModelReset() {
            root.modelEpoch += 1
        }
        function onRowsInserted() {
            root.modelEpoch += 1
        }
        function onRowsRemoved() {
            root.modelEpoch += 1
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

                        AppTag {
                            visible: root.openSkill !== null
                            text: root.openSkill ? root.openSkill.source : ""
                            backgroundColor: sourceBackground(root.openSkill ? root.openSkill.source : "")
                            borderColor: sourceBorder(root.openSkill ? root.openSkill.source : "")
                            textColor: sourceTint(root.openSkill ? root.openSkill.source : "")
                            fontWeight: Theme.fontWeight.semibold
                            minimumHeight: 20
                            pill: false
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
                    readonly property bool canRemove: !!(root.openSkill && root.openSkill.source === "user")

                    Layout.preferredWidth: root.confirmRemove ? (root.removing ? 216 : 180) : 84
                    Layout.preferredHeight: canRemove ? 72 : 32
                    Layout.maximumHeight: Layout.preferredHeight
                    spacing: Theme.space.sm

                    RowLayout {
                        visible: parent.canRemove
                        Layout.alignment: Qt.AlignRight
                        spacing: Theme.space.sm

                        AppButton {
                            objectName: root.openSkill ? "skillRemoveConfirmButton_" + root.openSkill.name : ""
                            visible: root.confirmRemove
                            text: root.removing ? "Removing…" : "Confirm"
                            toolTipText: root.openSkill ? "Remove " + root.openSkill.name : "Remove skill"
                            variant: "danger"
                            enabled: !root.removing
                            implicitWidth: Math.max(root.removing ? 128 : 84, contentItem.implicitWidth + leftPadding + rightPadding)
                            Layout.preferredWidth: implicitWidth
                            Layout.preferredHeight: 32
                            onClicked: removeSkill()
                        }

                        AppButton {
                            objectName: root.openSkill ? "skillRemoveButton_" + root.openSkill.name : ""
                            text: root.confirmRemove ? "Cancel" : "Remove"
                            toolTipText: root.confirmRemove ? "Cancel removal" : (root.openSkill ? "Remove " + root.openSkill.name : "Remove skill")
                            variant: root.confirmRemove ? "secondary" : "danger"
                            enabled: !root.removing
                            Layout.preferredWidth: Math.max(84, implicitWidth)
                            Layout.preferredHeight: 32
                            onClicked: {
                                if (root.confirmRemove) {
                                    root.confirmRemove = false
                                } else {
                                    root.confirmRemove = true
                                }
                            }
                        }
                    }

                    AppButton {
                        objectName: "skillPreviewCloseButton"
                        text: "✕"
                        toolTipText: "Close preview"
                        variant: "ghost"
                        enabled: !root.removing
                        leftPadding: Theme.space.sm
                        rightPadding: Theme.space.sm
                        Layout.alignment: Qt.AlignRight
                        Layout.preferredWidth: 32
                        Layout.preferredHeight: 32
                        onClicked: closePreview()
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

                    MarkdownBlocks {
                        objectName: "skillPreviewMarkdownBody"
                        visible: !root.bodyLoading && root.bodyBlocks.length > 0
                        width: parent.width
                        blocks: root.bodyBlocks
                    }

                    Label {
                        visible: !root.bodyLoading && root.body !== "" && root.bodyBlocks.length === 0
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

    Connections {
        target: root.rpcClient ? root.rpcClient : null
        function onCallDone(token, payload) {
            if (token === root.bodyToken) {
                root.bodyToken = 0
                root.bodyLoading = false
                if (payload.error) {
                    root.setBodyContent("")
                    root.actionError = "Could not load skill body: " + errorText(payload)
                } else {
                    root.setBodyContent(payload.result || "")
                }
                return
            }

            if (token === root.removeToken) {
                root.removeToken = 0
                root.removing = false
                root.confirmRemove = false
                if (payload.error) {
                    root.actionError = "Could not remove skill: " + errorText(payload)
                } else {
                    root.actionError = ""
                    closePreview()
                    if (root.skillsModel) {
                        root.skillsModel.refresh()
                    }
                }
                return
            }

            if (token === root.installToken) {
                root.installToken = 0
                root.installing = false
                if (payload.error) {
                    root.actionError = "Could not install skill: " + errorText(payload)
                } else if (payload.result) {
                    root.actionError = ""
                    root.addInput = ""
                    if (root.skillsModel) {
                        root.skillsModel.refresh()
                    }
                }
            }
        }
    }

    Connections {
        target: root.proposalsModel
        function onProposal_done(name, action, success, error_msg) {
            root.clearProposalActing(name)
            if (!success) {
                var label = action === "accept" ? "accept" : "reject"
                root.actionError = "Could not " + label + " " + name + ": " + (error_msg || "unknown error")
                return
            }

            root.actionError = ""
            if (action === "accept" && root.skillsModel) {
                root.skillsModel.refresh()
            }
        }
    }

    // Helper functions
    function errorText(payload) {
        var error = payload ? payload.error : null
        if (!error) return "unknown error"
        if (typeof error === "string") return error
        if (error.message) return error.message
        return String(error)
    }

    function parseMarkdown(source) {
        if (!source || typeof markdownParser === "undefined" || !markdownParser) return []
        return markdownParser.parse(source)
    }

    function setBodyContent(source) {
        root.body = source || ""
        root.bodyBlocks = root.body ? parseMarkdown(root.body) : []
    }

    function syncActiveModels(activeOverride) {
        var active = activeOverride === undefined ? root.visible : activeOverride
        if (root.skillsModel) {
            root.skillsModel.set_active(active)
        }
        if (root.proposalsModel) {
            root.proposalsModel.set_active(active)
        }
    }

    function skillsLoadError() {
        if (root.skillsModel && root.skillsModel.load_error) {
            return String(root.skillsModel.load_error)
        }
        if (root.proposalsModel && root.proposalsModel.load_error) {
            return String(root.proposalsModel.load_error)
        }
        return ""
    }

    function hasInitialLoadError() {
        return root.skillsLoadError() !== "" && skillsRowCount() === 0 && proposalCount() === 0
    }

    function hasRefreshLoadError() {
        return root.skillsLoadError() !== "" && !root.hasInitialLoadError()
    }

    function skillsRowCount() {
        return root.skillsModel ? root.skillsModel.rowCount() : 0
    }

    function retryLoad() {
        if (root.skillsModel) {
            root.skillsModel.refresh()
        }
        if (root.proposalsModel) {
            root.proposalsModel.refresh()
        }
    }

    function cloneMap(map) {
        var next = {}
        var source = map || {}
        for (var key in source) next[key] = source[key]
        return next
    }

    function clearProposalActing(name) {
        var next = cloneMap(root.acting)
        delete next[name]
        root.acting = next
    }

    function runProposalAction(name, action) {
        if (!root.proposalsModel || !name || root.acting[name]) return
        root.actionError = ""
        var next = cloneMap(root.acting)
        next[name] = action
        root.acting = next
        if (action === "accept") {
            root.proposalsModel.accept(name)
        } else {
            root.proposalsModel.reject(name)
        }
    }

    function proposalCount() {
        root.modelEpoch
        return root.proposalsModel ? root.proposalsModel.rowCount() : 0
    }

    function proposalRemaining() {
        return Math.max(0, proposalCount() - root.proposalsShown)
    }

    function filteredSkills() {
        root.modelEpoch
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
        var count = Math.min(root.proposalsShown, proposalCount())
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
        if (source === "project") return Theme.colors.accentBg
        return Theme.colors.stateHover
    }

    function sourceBorder(source) {
        if (source === "user") return Theme.colors.borderBrandFaint
        if (source === "project") return Theme.colors.borderAccentFaint
        return Theme.colors.borderSubtle
    }

    function openPreview(skill) {
        root.openSkill = skill
        root.setBodyContent("")
        root.bodyLoading = false
        root.confirmRemove = false

        if (!root.rpcClient) {
            root.bodyToken = 0
            root.actionError = "Could not load skill body: RPC client is unavailable."
            return
        }

        root.actionError = ""
        root.bodyLoading = true
        root.bodyToken = root.rpcClient.callToken("SkillBody", [skill.name])
    }

    function closePreview() {
        if (root.removing) return
        root.bodyToken = 0
        root.bodyLoading = false
        root.openSkill = null
        root.setBodyContent("")
        root.confirmRemove = false
    }

    function removeSkill() {
        if (!root.openSkill || root.removing) return
        if (!root.rpcClient) {
            root.actionError = "Could not remove skill: RPC client is unavailable."
            return
        }
        root.removing = true
        var name = root.openSkill.name

        root.removeToken = root.rpcClient.callToken("RemoveSkill", [name])
    }

    function installSkill(inputText) {
        if (inputText !== undefined) root.addInput = String(inputText)
        var ref = root.addInput.trim()
        if (!ref || root.installing) return

        root.actionError = ""
        if (!root.rpcClient) {
            root.actionError = "Could not install skill: RPC client is unavailable."
            return
        }

        root.installing = true
        var method = root.addMode === "path" ? "InstallSkillFromPath" : "InstallSkillFromGitHub"

        root.installToken = root.rpcClient.callToken(method, [ref])
    }

    // Reset activeShown when query changes
    onQueryChanged: {
        root.activeShown = root.pageSize
    }

}
