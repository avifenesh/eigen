import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Approval overlay (centered sheet over transcript)
Rectangle {
    id: root
    readonly property real parentWidth: parent && parent.width > 0 ? parent.width : 560
    readonly property real parentHeight: parent && parent.height > 0 ? parent.height : 760
    readonly property real maxSheetWidth: Math.max(320, parentWidth - Theme.space.xxl * 2)
    readonly property real maxSheetHeight: Math.max(280, parentHeight - Theme.space.xxl * 2)
    objectName: "approvalOverlay"
    width: Math.min(560, maxSheetWidth)
    height: Math.min(column.implicitHeight + Theme.space.xl * 2, maxSheetHeight)
    color: Theme.colors.surfaceOverlay
    radius: Theme.radius.lg
    border.width: 1
    border.color: Theme.colors.borderStrong
    clip: true

    property var model

    signal approve(string approvalId, bool allow)

    ColumnLayout {
        id: column
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: Theme.space.xl
        spacing: Theme.space.lg

        Label {
            id: titleLabel
            text: "Approval Required"
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.h2
            font.weight: Theme.fontWeight.semibold
            color: Theme.colors.textPrimary
            Layout.fillWidth: true
        }

        ListView {
            Layout.fillWidth: true
            Layout.preferredHeight: Math.min(
                contentHeight,
                Math.max(180, root.maxSheetHeight - titleLabel.implicitHeight - Theme.space.xl * 4)
            )
            clip: true
            spacing: Theme.space.md
            interactive: contentHeight > height
            model: root.model

            delegate: Rectangle {
                readonly property string approvalId: String(model.id || "")
                readonly property string qaKey: root.safeObjectName(approvalId || index)
                readonly property string prettyArgs: root.prettyArgs(model.args || "")
                readonly property bool argsOpen: argsToggle.checked
                readonly property bool argsLong: prettyArgs.length > 220 || prettyArgs.indexOf("\n") >= 0
                readonly property string shownArgs: argsOpen || !argsLong
                    ? prettyArgs
                    : prettyArgs.slice(0, 220) + "..."

                width: ListView.view.width
                height: delegateColumn.height + Theme.space.lg * 2
                color: Theme.colors.surfaceRaised
                radius: Theme.radius.md
                border.width: 1
                border.color: Theme.colors.borderSubtle

                ColumnLayout {
                    id: delegateColumn
                    anchors.left: parent.left
                    anchors.right: parent.right
                    anchors.top: parent.top
                    anchors.margins: Theme.space.lg
                    spacing: Theme.space.md

                    Label {
                        text: model.tool
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                        Layout.fillWidth: true
                    }

                    Rectangle {
                        objectName: "approvalArgs_" + qaKey
                        visible: prettyArgs !== ""
                        Layout.fillWidth: true
                        Layout.preferredHeight: Math.min(
                            argsText.implicitHeight + Theme.space.lg,
                            argsOpen ? 220 : 96
                        )
                        color: Theme.colors.bgInset
                        radius: Theme.radius.sm
                        border.width: 1
                        border.color: Theme.colors.borderHairline
                        clip: true

                        ScrollView {
                            anchors.fill: parent
                            anchors.margins: Theme.space.sm
                            clip: true

                            AppTextArea {
                                id: argsText
                                objectName: "approvalArgsText_" + qaKey
                                text: shownArgs
                                readOnly: true
                                wrapMode: TextEdit.WrapAnywhere
                                color: Theme.colors.textSecondary
                                selectedTextColor: Theme.colors.bgBase
                                selectionColor: Theme.colors.brandBright
                                font.family: Theme.monoFonts[0]
                                font.pixelSize: Theme.fontSize.codeSm
                                leftPadding: Theme.space.xl
                                rightPadding: Theme.space.xl
                                topPadding: Theme.space.lg
                                bottomPadding: Theme.space.lg
                                backgroundColor: "transparent"
                                borderColor: "transparent"
                                focusBorderColor: Theme.colors.borderBrandFaint
                                normalBorderWidth: 0
                                focusedBorderWidth: 1
                                backgroundRadius: Theme.radius.xs
                                Accessible.name: "Approval arguments"
                            }
                        }
                    }

                    AppButton {
                        id: argsToggle
                        objectName: "approvalArgsToggle_" + qaKey
                        visible: prettyArgs !== "" && argsLong
                        checkable: true
                        text: checked ? "Show less" : "Show full args"
                        variant: "ghost"
                        compact: true
                        contentAlignment: Text.AlignLeft
                        toolTipText: checked ? "Collapse approval arguments" : "Expand approval arguments"
                        Layout.fillWidth: true
                    }

                    Rectangle {
                        objectName: "approvalError_" + qaKey
                        visible: String(model.error || "") !== ""
                        Layout.fillWidth: true
                        implicitHeight: errorLabel.implicitHeight + Theme.space.md * 2
                        color: Theme.colors.errorBg
                        radius: Theme.radius.sm
                        border.width: 1
                        border.color: Theme.colors.error

                        Label {
                            id: errorLabel
                            anchors.left: parent.left
                            anchors.right: parent.right
                            anchors.verticalCenter: parent.verticalCenter
                            anchors.margins: Theme.space.md
                            text: model.error || ""
                            color: Theme.colors.error
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            wrapMode: Text.Wrap
                        }
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Theme.space.md

                        AppButton {
                            objectName: "approvalDeny_" + qaKey
                            text: "Deny"
                            variant: "danger"
                            enabled: !model.approving
                            toolTipText: "Deny approval"
                            Layout.preferredWidth: 88
                            onClicked: root.approve(approvalId, false)
                        }

                        Label {
                            visible: model.approving
                            text: "Resolving..."
                            color: Theme.colors.textMuted
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                        }

                        Item { Layout.fillWidth: true }

                        AppButton {
                            objectName: "approvalAllow_" + qaKey
                            text: "Allow"
                            variant: "primary"
                            enabled: !model.approving
                            toolTipText: "Allow approval"
                            Layout.preferredWidth: 88
                            onClicked: root.approve(approvalId, true)
                        }
                    }
                }
            }
        }
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]/g, "_")
    }

    function prettyArgs(raw) {
        const s = String(raw || "").trim()
        if (!s) return ""
        try {
            return JSON.stringify(JSON.parse(s), null, 2)
        } catch (e) {
            return s
        }
    }
}
