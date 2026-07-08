import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Profile view - usage rollups plus the global USER.md editor.
Rectangle {
    id: root
    objectName: "profileView"
    color: Theme.colors.bgBase

    property var profileModel: null
    property var statsData: ({})
    readonly property var models: profileModel ? profileModel.models || [] : []
    readonly property int qaRecordCount: profileModel ? profileModel.records : 0
    readonly property int qaModelCount: models.length

    onProfileModelChanged: syncActiveModel()
    onVisibleChanged: syncActiveModel()
    Component.onCompleted: syncActiveModel()

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 56
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xxxl
                anchors.rightMargin: Theme.space.xxxl
                spacing: Theme.space.lg

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: 2

                    Label {
                        objectName: "profileTitle"
                        text: "Profile"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.h3
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        objectName: "profileSummary"
                        text: summaryText()
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        color: Theme.colors.textMuted
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }

                AppButton {
                    objectName: "profileRefreshButton"
                    text: root.profileModel && (root.profileModel.summary_loading || root.profileModel.memory_loading) ? "Refreshing..." : "Refresh"
                    compact: true
                    toolTipText: "Refresh profile"
                    enabled: root.profileModel && !root.profileModel.summary_loading && !root.profileModel.memory_loading
                    Layout.preferredWidth: Math.max(86, implicitWidth)
                    Layout.preferredHeight: 32
                    onClicked: if (root.profileModel) root.profileModel.refresh()
                }
            }
        }

        Flickable {
            id: profileFlick
            objectName: "profileFlick"
            Layout.fillWidth: true
            Layout.fillHeight: true
            contentWidth: width
            contentHeight: contentColumn.implicitHeight + Theme.space.xxl
            clip: true

            ColumnLayout {
                id: contentColumn
                width: Math.min(parent.width - Theme.space.xl * 2, 1040)
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: Theme.space.lg

                Item { Layout.preferredHeight: Theme.space.xl }

                Rectangle {
                    visible: root.profileModel && root.profileModel.summary_loading && root.qaRecordCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 104 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Label {
                        anchors.centerIn: parent
                        text: "Loading usage..."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textMuted
                    }
                }

                Rectangle {
                    visible: root.profileModel && root.profileModel.summary_error !== "" && root.qaRecordCount === 0 && root.qaModelCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 128 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.md

                        Label {
                            text: "Could not load usage"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            text: root.profileModel ? root.profileModel.summary_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            elide: Text.ElideRight
                            Layout.maximumWidth: 720
                            Layout.alignment: Qt.AlignHCenter
                        }

                        AppButton {
                            text: "Retry"
                            compact: true
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: if (root.profileModel) root.profileModel.refresh()
                        }
                    }
                }

                RefreshErrorBanner {
                    objectName: "profileSummaryRefreshErrorBanner"
                    visible: root.profileModel && root.profileModel.summary_error !== "" && (root.qaRecordCount > 0 || root.qaModelCount > 0)
                    message: root.profileModel ? root.profileModel.summary_error : ""
                    textObjectName: "profileSummaryRefreshErrorText"
                    retryObjectName: "profileSummaryRefreshErrorRetry"
                    retryToolTipText: "Retry loading usage"
                    onRetry: if (root.profileModel) root.profileModel.refresh()
                }

                Flow {
                    objectName: "profileKpiFlow"
                    visible: root.profileModel && (!root.profileModel.summary_loading || root.qaRecordCount > 0 || root.qaModelCount > 0)
                    Layout.fillWidth: true
                    spacing: Theme.space.md
                    property int columnCount: Math.max(2, Math.floor(width / 164))
                    property real cardWidth: Math.floor((width - (columnCount - 1) * spacing) / columnCount)

                    Repeater {
                        model: root.kpis()
                        delegate: Rectangle {
                            readonly property bool qaTextFits: !kpiValueLabel.truncated && !kpiLabel.truncated
                            objectName: "profileKpi_" + modelData.key
                            width: parent.cardWidth
                            height: 88
                            radius: Theme.radius.md
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: modelData.hot ? Theme.colors.error : Theme.colors.borderHairline

                            ColumnLayout {
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                spacing: Theme.space.xs

                                Label {
                                    id: kpiValueLabel
                                    text: String(modelData.value)
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: 18
                                    font.weight: Theme.fontWeight.semibold
                                    color: modelData.hot ? Theme.colors.error : Theme.colors.textPrimary
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }

                                Label {
                                    id: kpiLabel
                                    text: modelData.label
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.micro
                                    color: Theme.colors.textMuted
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                            }
                        }
                    }
                }

                Rectangle {
                    objectName: "profileModelsPanel"
                    visible: root.profileModel && root.qaModelCount > 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? Math.max(112, 54 + Math.min(root.qaModelCount, 5) * 38) : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline
                    clip: true

                    ColumnLayout {
                        anchors.fill: parent
                        anchors.margins: Theme.space.lg
                        spacing: Theme.space.md

                        RowLayout {
                            Layout.fillWidth: true
                            spacing: Theme.space.md

                            Label {
                                text: "Top models"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.body
                                font.weight: Theme.fontWeight.semibold
                                color: Theme.colors.textPrimary
                            }

                            Label {
                                text: root.qaModelCount + " active"
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.micro
                                color: Theme.colors.textMuted
                            }

                            Item { Layout.fillWidth: true }
                        }

                        Repeater {
                            model: root.topItems(root.models, 5)
                            delegate: Rectangle {
                                readonly property var modelInfo: modelData || ({})
                                readonly property bool qaTextFits: !modelNameLabel.truncated && !modelStatsLabel.truncated
                                objectName: "profileModelRow_" + root.safeObjectName(modelInfo.name || index)
                                Layout.fillWidth: true
                                implicitHeight: 34
                                radius: Theme.radius.sm
                                color: index % 2 === 0 ? Theme.colors.bgInset : "transparent"

                                RowLayout {
                                    anchors.fill: parent
                                    anchors.leftMargin: Theme.space.md
                                    anchors.rightMargin: Theme.space.md
                                    spacing: Theme.space.md

                                    Label {
                                        id: modelNameLabel
                                        text: modelInfo.name || ""
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textSecondary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        id: modelStatsLabel
                                        text: root.formatInt(modelInfo.turns || 0) + " turns  in " + root.compactInt(modelInfo.inTokens || 0) + "  out " + root.compactInt(modelInfo.outTokens || 0)
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textMuted
                                        elide: Text.ElideRight
                                        Layout.preferredWidth: 236
                                    }
                                }
                            }
                        }
                    }
                }

                Rectangle {
                    visible: root.profileModel && !root.profileModel.summary_loading && root.profileModel.summary_error === "" && root.qaModelCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 88 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Label {
                        anchors.centerIn: parent
                        text: "No model activity recorded yet."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }
                }

                ColumnLayout {
                    objectName: "profileUserSection"
                    Layout.fillWidth: true
                    spacing: Theme.space.md

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.space.md

                        Label {
                            text: "User profile"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                        }

                        AppTag {
                            text: "USER.md"
                            backgroundColor: Theme.colors.stateSelected
                            borderColor: Theme.colors.borderBrandFaint
                            textColor: Theme.colors.brandBright
                            fontFamily: Theme.monoFonts[0]
                            minimumHeight: 22
                        }

                        Item { Layout.fillWidth: true }

                        AppButton {
                            objectName: "profileEditButton"
                            visible: root.profileModel && !root.profileModel.editing_profile && !root.profileModel.memory_loading
                            text: root.profileModel && root.profileModel.profile !== "" ? "Edit" : "Add"
                            compact: true
                            variant: "ghost"
                            toolTipText: "Edit USER.md profile"
                            Layout.preferredWidth: 64
                            Layout.preferredHeight: 28
                            onClicked: if (root.profileModel) root.profileModel.start_edit()
                        }
                    }

                    Rectangle {
                        visible: root.profileModel && root.profileModel.memory_error !== "" && root.profileModel.profile === "" && !root.profileModel.editing_profile
                        Layout.fillWidth: true
                        Layout.preferredHeight: visible ? 112 : 0
                        radius: Theme.radius.md
                        color: Theme.colors.surfaceRaised
                        border.width: 1
                        border.color: Theme.colors.borderHairline

                        ColumnLayout {
                            anchors.centerIn: parent
                            spacing: Theme.space.sm

                            Label {
                                text: "Could not load profile"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.body
                                font.weight: Theme.fontWeight.semibold
                                color: Theme.colors.textPrimary
                                Layout.alignment: Qt.AlignHCenter
                            }

                            Label {
                                text: root.profileModel ? root.profileModel.memory_error : ""
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textMuted
                                elide: Text.ElideRight
                                Layout.maximumWidth: 720
                                Layout.alignment: Qt.AlignHCenter
                            }
                        }
                    }

                    Rectangle {
                        objectName: "profileActionError"
                        visible: root.profileModel && root.profileModel.action_error !== ""
                        Layout.fillWidth: true
                        Layout.preferredHeight: visible ? Math.max(44, actionErrorRow.implicitHeight + Theme.space.md) : 0
                        radius: Theme.radius.sm
                        color: Theme.colors.errorBg
                        border.width: 1
                        border.color: Theme.colors.error

                        RowLayout {
                            id: actionErrorRow
                            anchors.fill: parent
                            anchors.margins: Theme.space.md
                            spacing: Theme.space.sm

                            Label {
                                objectName: "profileActionErrorText"
                                text: root.profileModel ? root.profileModel.action_error : ""
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                color: Theme.colors.error
                                wrapMode: Text.Wrap
                                Layout.fillWidth: true
                            }

                            AppButton {
                                objectName: "profileDismissActionError"
                                text: "Dismiss"
                                compact: true
                                variant: "ghost"
                                toolTipText: "Dismiss profile error"
                                onClicked: if (root.profileModel) root.profileModel.clear_action_error()
                            }
                        }
                    }

                    Rectangle {
                        visible: root.profileModel && root.profileModel.editing_profile
                        Layout.fillWidth: true
                        implicitHeight: profileEditColumn.implicitHeight + Theme.space.lg * 2
                        radius: Theme.radius.md
                        color: Theme.colors.surfaceRaised
                        border.width: 1
                        border.color: Theme.colors.borderHairline

                        ColumnLayout {
                            id: profileEditColumn
                            anchors.left: parent.left
                            anchors.right: parent.right
                            anchors.top: parent.top
                            anchors.margins: Theme.space.lg
                            spacing: Theme.space.md

                            Rectangle {
                                Layout.fillWidth: true
                                Layout.preferredHeight: 220
                                radius: Theme.radius.sm
                                color: Theme.colors.bgInset
                                border.width: 1
                                border.color: profileTextArea.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle

                                TextArea {
                                    id: profileTextArea
                                    objectName: "profileTextArea"
                                    anchors.fill: parent
                                    anchors.margins: Theme.space.md
                                    text: root.profileModel ? root.profileModel.profile_draft : ""
                                    selectByMouse: true
                                    wrapMode: TextEdit.Wrap
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textPrimary
                                    placeholderText: "USER.md"
                                    background: Item {}
                                    onTextChanged: {
                                        if (root.profileModel && root.profileModel.profile_draft !== text) {
                                            root.profileModel.profile_draft = text
                                        }
                                    }
                                    Keys.onPressed: function(event) {
                                        if ((event.modifiers & Qt.ControlModifier) && (event.key === Qt.Key_Return || event.key === Qt.Key_Enter)) {
                                            if (root.profileModel && !root.profileModel.saving_profile) {
                                                root.profileModel.save_profile()
                                            }
                                            event.accepted = true
                                        } else if (event.key === Qt.Key_Escape) {
                                            if (root.profileModel && !root.profileModel.saving_profile) {
                                                root.profileModel.cancel_edit()
                                            }
                                            event.accepted = true
                                        }
                                    }
                                }
                            }

                            RowLayout {
                                Layout.fillWidth: true
                                spacing: Theme.space.sm

                                Item { Layout.fillWidth: true }

                                AppButton {
                                    objectName: "profileCancelButton"
                                    text: "Cancel"
                                    compact: true
                                    enabled: root.profileModel && !root.profileModel.saving_profile
                                    Layout.preferredWidth: 78
                                    Layout.preferredHeight: 28
                                    onClicked: if (root.profileModel) root.profileModel.cancel_edit()
                                }

                                AppButton {
                                    objectName: "profileSaveButton"
                                    text: root.profileModel && root.profileModel.saving_profile ? "Saving..." : "Save"
                                    compact: true
                                    variant: "primary"
                                    enabled: root.profileModel && !root.profileModel.saving_profile
                                    Layout.preferredWidth: 78
                                    Layout.preferredHeight: 28
                                    onClicked: if (root.profileModel) root.profileModel.save_profile()
                                }
                            }
                        }
                    }

                    Rectangle {
                        objectName: "profileUserCard"
                        visible: root.profileModel && !root.profileModel.editing_profile && root.profileModel.profile !== ""
                        Layout.fillWidth: true
                        implicitHeight: profileBlocks.implicitHeight + Theme.space.lg * 2
                        radius: Theme.radius.md
                        color: Theme.colors.surfaceRaised
                        border.width: 1
                        border.color: Theme.colors.borderHairline

                        MarkdownBlocks {
                            id: profileBlocks
                            width: parent.width - Theme.space.lg * 2
                            anchors.left: parent.left
                            anchors.top: parent.top
                            anchors.margins: Theme.space.lg
                            blocks: root.parseMarkdown(root.profileModel ? root.profileModel.profile : "")
                        }
                    }

                    Rectangle {
                        visible: root.profileModel && !root.profileModel.memory_loading && !root.profileModel.editing_profile && root.profileModel.profile === "" && root.profileModel.memory_error === ""
                        Layout.fillWidth: true
                        Layout.preferredHeight: visible ? 88 : 0
                        radius: Theme.radius.md
                        color: Theme.colors.surfaceRaised
                        border.width: 1
                        border.color: Theme.colors.borderHairline

                        Label {
                            anchors.centerIn: parent
                            text: "No profile set yet."
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                        }
                    }
                }
            }
        }
    }

    function syncActiveModel() {
        if (profileModel && profileModel.set_active) {
            profileModel.set_active(root.visible)
        }
    }

    function summaryText() {
        if (!profileModel) return "usage unavailable"
        if (profileModel.summary_loading && qaRecordCount === 0) return "loading usage"
        if (profileModel.summary_error !== "" && qaRecordCount === 0) return "usage unavailable"
        return formatInt(qaRecordCount) + " turns logged / " + qaModelCount + " models / " + formatInt(profileModel.error_count || 0) + " errors"
    }

    function kpis() {
        if (!profileModel) return []
        return [
            {key: "sessions", label: "sessions", value: formatInt(sessionCount()), hot: false},
            {key: "turns", label: "turns logged", value: formatInt(profileModel.records || 0), hot: false},
            {key: "in", label: "tokens in", value: compactInt(profileModel.in_tokens || 0), hot: false},
            {key: "out", label: "tokens out", value: compactInt(profileModel.out_tokens || 0), hot: false},
            {key: "cache", label: "cache hit", value: String(profileModel.cache_hit || 0) + "%", hot: false},
            {key: "errors", label: "errors", value: formatInt(profileModel.error_count || 0), hot: (profileModel.error_count || 0) > 0}
        ]
    }

    function sessionCount() {
        if (!statsData) return 0
        return Number(statsData.sessions || statsData.sessionCount || 0)
    }

    function topItems(items, limit) {
        var out = []
        for (var i = 0; i < items.length && out.length < limit; i++) {
            out.push(items[i])
        }
        return out
    }

    function formatInt(value) {
        return Number(value || 0).toLocaleString(Qt.locale(), "f", 0)
    }

    function compactInt(value) {
        var n = Number(value || 0)
        if (n >= 1000000) return (n / 1000000).toFixed(1) + "M"
        if (n >= 1000) return (n / 1000).toFixed(1) + "k"
        return String(Math.round(n))
    }

    function parseMarkdown(source) {
        if (!source || typeof markdownParser === "undefined" || !markdownParser) {
            return [{type: "para", content: source || ""}]
        }
        return markdownParser.parse(source)
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]/g, "_")
    }
}
