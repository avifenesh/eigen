import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Dreaming view - read-only memory rollout and consolidation timeline.
Rectangle {
    id: root
    objectName: "dreamingView"
    color: Theme.colors.bgBase

    property var dreamingModel: null
    property string strand: "rollouts"
    readonly property var rollouts: dreamingModel ? dreamingModel.rollouts || [] : []
    readonly property var consolidations: dreamingModel ? dreamingModel.consolidations || [] : []
    readonly property int qaRolloutCount: rollouts.length
    readonly property int qaConsolidationCount: consolidations.length
    readonly property string qaStrand: strand

    onDreamingModelChanged: syncActiveModel()
    onVisibleChanged: syncActiveModel()
    Component.onCompleted: syncActiveModel()

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 64
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
                        objectName: "dreamingTitle"
                        text: "Dreaming"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.h3
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        objectName: "dreamingSummary"
                        text: summaryText()
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        color: Theme.colors.textMuted
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }

                AppComboBox {
                    id: scopeCombo
                    objectName: "dreamingScopeCombo"
                    Layout.preferredWidth: 280
                    Layout.preferredHeight: 32
                    model: root.dreamingModel ? root.dreamingModel.scopes || [] : []
                    textRole: "name"
                    valueRole: "key"
                    fallbackText: root.dreamingModel ? root.dreamingModel.scope_label : "Project"
                    currentIndex: root.findScopeIndex(root.dreamingModel ? root.dreamingModel.scope_key : "")
                    activationUpdatesCurrentIndex: false
                    accessibleName: "Dreaming scope"
                    toolTipText: "Change dreaming scope"
                    enabled: !!root.dreamingModel && !root.dreamingModel.loading && count > 0
                    onActivated: function(index) {
                        var scope = model[index]
                        if (root.dreamingModel && scope && scope.key) {
                            root.dreamingModel.select_scope(scope.key)
                        }
                    }
                }

                AppButton {
                    objectName: "dreamingRefreshButton"
                    text: root.dreamingModel && root.dreamingModel.loading ? "Refreshing..." : "Refresh"
                    compact: true
                    toolTipText: "Refresh dreaming"
                    enabled: !!root.dreamingModel && !root.dreamingModel.loading
                    Layout.preferredWidth: Math.max(86, implicitWidth)
                    Layout.preferredHeight: 32
                    onClicked: if (root.dreamingModel) root.dreamingModel.refresh()
                }
            }
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.leftMargin: Theme.space.xxxl
            Layout.rightMargin: Theme.space.xxxl
            Layout.topMargin: Theme.space.lg
            spacing: 0

            AppButton {
                objectName: "dreamingTab_rollouts"
                text: "Rollouts"
                badgeText: String(root.qaRolloutCount)
                selected: root.strand === "rollouts"
                compact: true
                segmentPosition: "first"
                Layout.preferredHeight: 30
                Layout.preferredWidth: Math.max(112, implicitWidth)
                onClicked: root.strand = "rollouts"
            }

            AppButton {
                objectName: "dreamingTab_consolidations"
                text: "Consolidations"
                badgeText: String(root.qaConsolidationCount)
                selected: root.strand === "consolidations"
                compact: true
                segmentPosition: "last"
                Layout.preferredHeight: 30
                Layout.preferredWidth: Math.max(150, implicitWidth)
                onClicked: root.strand = "consolidations"
            }

            Item { Layout.fillWidth: true }
        }

        Flickable {
            id: dreamingFlick
            objectName: "dreamingFlick"
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

                Item { Layout.preferredHeight: Theme.space.lg }

                Rectangle {
                    visible: root.dreamingModel && root.dreamingModel.loading && root.qaRolloutCount === 0 && root.qaConsolidationCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 112 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Label {
                        anchors.centerIn: parent
                        text: "Loading dreaming..."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textMuted
                    }
                }

                Rectangle {
                    objectName: "dreamingLoadError"
                    visible: root.dreamingModel && root.dreamingModel.load_error !== "" && root.qaRolloutCount === 0 && root.qaConsolidationCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 132 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.md

                        Label {
                            text: "Could not load dreaming"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            objectName: "dreamingLoadErrorText"
                            text: root.dreamingModel ? root.dreamingModel.load_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            elide: Text.ElideRight
                            Layout.maximumWidth: 720
                            Layout.alignment: Qt.AlignHCenter
                        }

                        AppButton {
                            objectName: "dreamingLoadErrorRetry"
                            text: "Retry"
                            compact: true
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: if (root.dreamingModel) root.dreamingModel.refresh()
                        }
                    }
                }

                RefreshErrorBanner {
                    objectName: "dreamingRefreshErrorBanner"
                    visible: root.dreamingModel && root.dreamingModel.load_error !== "" && (root.qaRolloutCount > 0 || root.qaConsolidationCount > 0)
                    message: root.dreamingModel ? root.dreamingModel.load_error : ""
                    textObjectName: "dreamingRefreshErrorText"
                    retryObjectName: "dreamingRefreshErrorRetry"
                    retryToolTipText: "Retry loading dreaming"
                    onRetry: if (root.dreamingModel) root.dreamingModel.refresh()
                }

                Rectangle {
                    visible: root.dreamingModel && !root.dreamingModel.loading && root.dreamingModel.load_error === "" && root.qaRolloutCount === 0 && root.qaConsolidationCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 112 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Label {
                        anchors.centerIn: parent
                        text: "Nothing dreamed yet"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }
                }

                ColumnLayout {
                    id: rolloutSection
                    readonly property bool active: root.strand === "rollouts" && root.qaRolloutCount > 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: active ? implicitHeight : 0
                    spacing: Theme.space.md
                    clip: true

                    Repeater {
                        model: root.rollouts
                        delegate: Rectangle {
                            id: rolloutRow
                            readonly property var rollout: modelData || ({})
                            readonly property string rolloutText: String(rollout.text || "")
                            readonly property bool qaTextFits: !visible || (!rolloutTitleLabel.truncated && !rolloutMetaLabel.truncated)
                            objectName: "dreamingRolloutRow_" + root.safeObjectName(rollout.index !== undefined ? rollout.index : index)
                            visible: rolloutSection.active
                            Layout.fillWidth: true
                            implicitHeight: visible ? Math.max(118, rolloutColumn.implicitHeight + Theme.space.lg * 2) : 0
                            radius: Theme.radius.md
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: Theme.colors.borderHairline

                            ColumnLayout {
                                id: rolloutColumn
                                anchors.fill: parent
                                anchors.margins: Theme.space.lg
                                spacing: Theme.space.sm

                                RowLayout {
                                    Layout.fillWidth: true
                                    spacing: Theme.space.md

                                    Label {
                                        id: rolloutTitleLabel
                                        text: root.titleFromText(rolloutRow.rolloutText)
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.textPrimary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    Rectangle {
                                        visible: String(rollout.outcome || "") !== ""
                                        implicitWidth: outcomeLabel.implicitWidth + Theme.space.md
                                        implicitHeight: 22
                                        radius: 11
                                        color: root.outcomeTone(rollout.outcome)
                                        border.width: 1
                                        border.color: Theme.colors.borderHairline

                                        Label {
                                            id: outcomeLabel
                                            anchors.centerIn: parent
                                            text: rollout.outcome || ""
                                            font.family: Theme.monoFonts[0]
                                            font.pixelSize: Theme.fontSize.micro
                                            color: Theme.colors.textPrimary
                                        }
                                    }

                                    Label {
                                        id: rolloutMetaLabel
                                        text: root.whenText(rollout.whenMs)
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textMuted
                                        elide: Text.ElideRight
                                        Layout.preferredWidth: 132
                                    }
                                }

                                Label {
                                    text: rolloutRow.rolloutText
                                    textFormat: Text.PlainText
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textSecondary
                                    wrapMode: Text.Wrap
                                    maximumLineCount: 6
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                            }
                        }
                    }
                }

                ColumnLayout {
                    id: consolidationSection
                    readonly property bool active: root.strand === "consolidations" && root.qaConsolidationCount > 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: active ? implicitHeight : 0
                    spacing: Theme.space.md
                    clip: true

                    Repeater {
                        model: root.consolidations
                        delegate: Rectangle {
                            id: consRow
                            readonly property var cons: modelData || ({})
                            readonly property bool qaTextFits: !visible || (!consLabel.truncated && !consMeta.truncated)
                            readonly property bool qaLabelTruncated: consLabel.truncated
                            readonly property bool qaMetaTruncated: consMeta.truncated
                            readonly property string qaMetaText: consMeta.text
                            objectName: "dreamingConsolidationRow_" + root.safeObjectName(index)
                            visible: consolidationSection.active
                            Layout.fillWidth: true
                            implicitHeight: visible ? 64 : 0
                            radius: Theme.radius.md
                            color: Theme.colors.surfaceRaised
                            border.width: 1
                            border.color: Theme.colors.borderHairline

                            RowLayout {
                                anchors.fill: parent
                                anchors.leftMargin: Theme.space.lg
                                anchors.rightMargin: Theme.space.lg
                                spacing: Theme.space.md

                                Rectangle {
                                    width: 7
                                    height: 7
                                    radius: 4
                                    color: index === 0 ? Theme.colors.brandBright : Theme.colors.dotIdle
                                    Layout.alignment: Qt.AlignVCenter
                                }

                                Label {
                                    id: consLabel
                                    text: cons.label || "snapshot"
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.textPrimary
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }

                                Label {
                                    id: consMeta
                                    text: root.whenText(cons.whenMs) + "  " + root.kb(cons.bytes)
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.micro
                                    color: Theme.colors.textMuted
                                    elide: Text.ElideRight
                                    Layout.preferredWidth: 232
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    function syncActiveModel() {
        if (dreamingModel && dreamingModel.set_active) {
            dreamingModel.set_active(root.visible)
        }
    }

    function findScopeIndex(key) {
        if (!dreamingModel || !dreamingModel.scopes) return -1
        for (var i = 0; i < dreamingModel.scopes.length; i++) {
            if (dreamingModel.scopes[i].key === key) return i
        }
        return -1
    }

    function summaryText() {
        if (!dreamingModel) return "timeline unavailable"
        if (dreamingModel.loading && qaRolloutCount === 0 && qaConsolidationCount === 0) return "loading timeline"
        if (dreamingModel.load_error !== "" && qaRolloutCount === 0 && qaConsolidationCount === 0) return "timeline unavailable"
        return dreamingModel.scope_label + " / " + qaRolloutCount + " rollouts / " + qaConsolidationCount + " snapshots / " + kb(dreamingModel.current_bytes || 0)
    }

    function titleFromText(text) {
        var lines = String(text || "").split("\n")
        for (var i = 0; i < lines.length; i++) {
            var line = lines[i].replace(/^#+\s*/, "").replace(/\*\*/g, "").trim()
            if (line.length > 0) {
                return line.length > 90 ? line.slice(0, 90) + "..." : line
            }
        }
        return "rollout"
    }

    function whenText(ms) {
        var n = Number(ms || 0)
        if (n <= 0) return "unknown"
        return Qt.formatDateTime(new Date(n), "yyyy-MM-dd hh:mm")
    }

    function kb(bytes) {
        var n = Number(bytes || 0)
        if (n >= 1024 * 1024) return (n / (1024 * 1024)).toFixed(1) + " MB"
        return (n / 1024).toFixed(1) + " KB"
    }

    function outcomeTone(outcome) {
        var value = String(outcome || "")
        if (value === "success") return Theme.colors.stateSelected
        if (value === "partial" || value === "skip") return Theme.colors.warnBg
        if (value === "failed") return Theme.colors.errorBg
        return Theme.colors.bgOverlay
    }

    function safeObjectName(value) {
        if (value === undefined || value === null) return ""
        return String(value).replace(/[^A-Za-z0-9_]/g, "_")
    }
}
