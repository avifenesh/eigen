import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Session control strip (model, effort, perm, title, goal)
Rectangle {
    id: root
    Layout.fillWidth: true
    Layout.preferredHeight: 52
    color: Theme.colors.bgWell
    border.width: 1
    border.color: Theme.colors.borderHairline

    property var sessionState  // SessionStateModel instance

    RowLayout {
        anchors.fill: parent
        anchors.margins: Theme.space.lg
        spacing: Theme.space.lg

        // Model badge (clickable → dropdown)
        AppComboBox {
            id: modelCombo
            objectName: "sessionModelCombo"
            model: sessionState ? sessionState.catalog : []
            fallbackText: sessionState ? sessionState.model : ""
            accessibleName: "Model"
            toolTipText: "Model"
            Layout.preferredWidth: 220
            activationUpdatesCurrentIndex: false
            currentIndex: {
                if (!sessionState || !sessionState.catalog) return -1
                return sessionState.catalog.indexOf(sessionState.model)
            }

            onActivated: function(index) {
                if (sessionState && sessionState.catalog && index >= 0) {
                    sessionState.setModel(sessionState.catalog[index])
                }
            }
        }

        // Perm toggle (gated ↔ auto)
        AppComboBox {
            id: permCombo
            objectName: "sessionPermCombo"
            model: ["gated", "auto"]
            accessibleName: "Permission mode"
            toolTipText: "Permission mode"
            Layout.preferredWidth: 112
            activationUpdatesCurrentIndex: false
            currentIndex: {
                if (!sessionState) return 0
                return sessionState.perm === "auto" ? 1 : 0
            }

            onActivated: function(index) {
                if (sessionState) {
                    sessionState.setPerm(index === 1 ? "auto" : "gated")
                }
            }
        }

        // Effort selector (if model supports it)
        Loader {
            active: sessionState && sessionState.effortLevels && sessionState.effortLevels.length > 0
            Layout.preferredWidth: active ? 120 : 0
            Layout.preferredHeight: active ? 32 : 0
            sourceComponent: AppComboBox {
                objectName: "sessionEffortCombo"
                model: sessionState ? sessionState.effortLevels : []
                fallbackText: sessionState ? sessionState.effort : ""
                accessibleName: "Reasoning effort"
                toolTipText: "Reasoning effort"
                activationUpdatesCurrentIndex: false
                currentIndex: {
                    if (!sessionState || !sessionState.effortLevels) return -1
                    return sessionState.effortLevels.indexOf(sessionState.effort)
                }

                onActivated: function(index) {
                    if (sessionState && sessionState.effortLevels && index >= 0) {
                        sessionState.setEffort(sessionState.effortLevels[index])
                    }
                }
            }
        }

        // Live search selector (only for providers that expose the mode).
        Loader {
            active: sessionState && sessionState.search !== ""
            Layout.preferredWidth: active ? 112 : 0
            Layout.preferredHeight: active ? 32 : 0
            sourceComponent: AppComboBox {
                objectName: "sessionSearchCombo"
                model: ["off", "auto", "on"]
                fallbackText: sessionState ? sessionState.search : ""
                accessibleName: "Live search"
                toolTipText: "Live search"
                activationUpdatesCurrentIndex: false
                currentIndex: {
                    if (!sessionState) return -1
                    return ["off", "auto", "on"].indexOf(sessionState.search)
                }

                onActivated: function(index) {
                    if (sessionState && index >= 0) {
                        sessionState.setSearch(["off", "auto", "on"][index])
                    }
                }
            }
        }

        // Fast/priority service tier. Kept compact so the title still breathes.
        Loader {
            active: sessionState && sessionState.fastOk
            Layout.preferredWidth: active ? 80 : 0
            Layout.preferredHeight: active ? 32 : 0
            sourceComponent: RowLayout {
                spacing: Theme.space.sm

                Label {
                    text: "fast"
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.micro
                    font.weight: Theme.fontWeight.semibold
                    color: sessionState && sessionState.fast ? Theme.colors.brandBright : Theme.colors.textMuted
                    verticalAlignment: Text.AlignVCenter
                    Layout.preferredWidth: 26
                    elide: Text.ElideRight
                }

                AppSwitch {
                    objectName: "sessionFastSwitch"
                    checked: !!(sessionState && sessionState.fast)
                    accessibleName: "Fast tier"
                    toolTipText: "Fast tier"
                    onClicked: {
                        if (sessionState) {
                            sessionState.setFast(!sessionState.fast)
                        }
                    }
                }
            }
        }

        // Title (double-click to rename)
        TextField {
            id: titleField
            objectName: "sessionTitleField"
            text: sessionState ? sessionState.title : ""
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.h3
            font.weight: Theme.fontWeight.semibold
            color: Theme.colors.textPrimary
            Layout.fillWidth: true
            readOnly: true
            focusPolicy: Qt.StrongFocus
            selectByMouse: !readOnly
            Accessible.name: "Session title"
            readonly property bool qaEditing: !readOnly
            readonly property bool qaTextFits: !titleField.contentItem || !titleField.contentItem.text
                || (titleField.contentItem.paintedWidth <= Math.max(0, width - leftPadding - rightPadding) + 0.5)
            readonly property string qaText: text

            function beginEdit() {
                if (!sessionState) {
                    return
                }
                readOnly = false
                forceActiveFocus()
                selectAll()
            }

            background: Rectangle {
                color: titleField.activeFocus ? Theme.colors.surfaceRaised : "transparent"
                radius: Theme.radius.sm
                border.width: titleField.activeFocus ? 1 : 0
                border.color: Theme.colors.borderBrand
            }

            onEditingFinished: {
                if (sessionState && text !== sessionState.title) {
                    sessionState.setTitle(text)
                }
                readOnly = true
                deselect()
            }

            Keys.onPressed: function(event) {
                if (event.key === Qt.Key_F2 && titleField.readOnly) {
                    titleField.beginEdit()
                    event.accepted = true
                }
            }

            MouseArea {
                anchors.fill: parent
                enabled: titleField.readOnly
                onDoubleClicked: titleField.beginEdit()
            }
        }

        // Goal display (compact)
        Label {
            text: sessionState && sessionState.goal ? sessionState.goal : ""
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.bodySm
            color: Theme.colors.textMuted
            elide: Text.ElideRight
            Layout.maximumWidth: 200
            visible: text.length > 0
        }
    }
}
