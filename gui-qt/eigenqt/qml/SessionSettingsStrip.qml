import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Session control strip (model, effort, perm, title, goal)
Rectangle {
    id: root
    Layout.fillWidth: true
    Layout.preferredHeight: compactControls
        ? Math.max(100, compactControlsFlow.height + Theme.space.lg * 2)
        : 52
    Layout.minimumHeight: Layout.preferredHeight
    color: Theme.colors.bgWell
    border.width: 1
    border.color: Theme.colors.borderHairline

    property var sessionState  // SessionStateModel instance
    readonly property bool compactControls: width > 0 && width < 760
    readonly property real compactModelWidth: Math.max(
        120,
        Math.min(200, width - Theme.space.lg * 2 - 104 - Theme.space.md)
    )

    Component {
        id: modelComboComponent

        AppComboBox {
            objectName: "sessionModelCombo"
            model: root.sessionState ? root.sessionState.catalog : []
            fallbackText: root.sessionState ? root.sessionState.model : ""
            accessibleName: "Model"
            toolTipText: "Model"
            activationUpdatesCurrentIndex: false
            currentIndex: {
                if (!root.sessionState || !root.sessionState.catalog) return -1
                return root.sessionState.catalog.indexOf(root.sessionState.model)
            }

            onActivated: function(index) {
                if (root.sessionState && root.sessionState.catalog && index >= 0) {
                    root.sessionState.setModel(root.sessionState.catalog[index])
                }
            }
        }
    }

    Component {
        id: permComboComponent

        AppComboBox {
            objectName: "sessionPermCombo"
            model: ["gated", "auto"]
            accessibleName: "Permission mode"
            toolTipText: "Permission mode"
            activationUpdatesCurrentIndex: false
            currentIndex: {
                if (!root.sessionState) return 0
                return root.sessionState.perm === "auto" ? 1 : 0
            }

            onActivated: function(index) {
                if (root.sessionState) {
                    root.sessionState.setPerm(index === 1 ? "auto" : "gated")
                }
            }
        }
    }

    Component {
        id: effortComboComponent

        AppComboBox {
            objectName: "sessionEffortCombo"
            model: root.sessionState ? root.sessionState.effortLevels : []
            fallbackText: root.sessionState ? root.sessionState.effort : ""
            accessibleName: "Reasoning effort"
            toolTipText: "Reasoning effort"
            activationUpdatesCurrentIndex: false
            currentIndex: {
                if (!root.sessionState || !root.sessionState.effortLevels) return -1
                return root.sessionState.effortLevels.indexOf(root.sessionState.effort)
            }

            onActivated: function(index) {
                if (root.sessionState && root.sessionState.effortLevels && index >= 0) {
                    root.sessionState.setEffort(root.sessionState.effortLevels[index])
                }
            }
        }
    }

    Component {
        id: searchComboComponent

        AppComboBox {
            objectName: "sessionSearchCombo"
            model: ["off", "auto", "on"]
            fallbackText: root.sessionState ? root.sessionState.search : ""
            accessibleName: "Live search"
            toolTipText: "Live search"
            activationUpdatesCurrentIndex: false
            currentIndex: {
                if (!root.sessionState) return -1
                return ["off", "auto", "on"].indexOf(root.sessionState.search)
            }

            onActivated: function(index) {
                if (root.sessionState && index >= 0) {
                    root.sessionState.setSearch(["off", "auto", "on"][index])
                }
            }
        }
    }

    Component {
        id: fastControlComponent

        RowLayout {
            spacing: Theme.space.sm

            Label {
                objectName: "sessionFastLabel"
                text: "Fast"
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.micro
                font.weight: Theme.fontWeight.semibold
                color: root.sessionState && root.sessionState.fast ? Theme.colors.brandBright : Theme.colors.textMuted
                verticalAlignment: Text.AlignVCenter
                Layout.preferredWidth: 28
                elide: Text.ElideRight
            }

            AppSwitch {
                objectName: "sessionFastSwitch"
                checked: !!(root.sessionState && root.sessionState.fast)
                accessibleName: "Fast mode"
                toolTipText: "Prioritize lower latency"
                onClicked: {
                    if (root.sessionState) {
                        root.sessionState.setFast(!root.sessionState.fast)
                    }
                }
            }
        }
    }

    RowLayout {
        visible: !root.compactControls
        anchors.fill: parent
        anchors.margins: Theme.space.lg
        spacing: Theme.space.lg

        Loader {
            active: !root.compactControls
            Layout.preferredWidth: 220
            Layout.preferredHeight: 32
            sourceComponent: modelComboComponent
        }

        Loader {
            active: !root.compactControls
            Layout.preferredWidth: 112
            Layout.preferredHeight: 32
            sourceComponent: permComboComponent
        }

        Loader {
            active: !root.compactControls && root.sessionState && root.sessionState.effortLevels && root.sessionState.effortLevels.length > 0
            Layout.preferredWidth: active ? 120 : 0
            Layout.preferredHeight: active ? 32 : 0
            sourceComponent: effortComboComponent
        }

        Loader {
            active: !root.compactControls && root.sessionState && root.sessionState.search !== ""
            Layout.preferredWidth: active ? 112 : 0
            Layout.preferredHeight: active ? 32 : 0
            sourceComponent: searchComboComponent
        }

        Loader {
            active: !root.compactControls && root.sessionState && root.sessionState.fastOk
            Layout.preferredWidth: active ? 80 : 0
            Layout.preferredHeight: active ? 32 : 0
            sourceComponent: fastControlComponent
        }

        // Title (double-click to rename)
        AppTextField {
            id: titleField
            objectName: "sessionTitleField"
            text: sessionState ? sessionState.title : ""
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.h3
            font.weight: Theme.fontWeight.semibold
            backgroundColor: activeFocus ? Theme.colors.surfaceRaised : "transparent"
            borderColor: "transparent"
            focusBorderColor: Theme.colors.borderFocus
            normalBorderWidth: 0
            focusedBorderWidth: activeFocus ? 1 : 0
            Layout.fillWidth: true
            Layout.minimumWidth: 0
            readOnly: true
            visible: !root.compactControls
            focusPolicy: Qt.StrongFocus
            selectByMouse: !readOnly
            Accessible.name: "Session title"
            readonly property bool qaEditing: !readOnly

            function beginEdit() {
                if (!sessionState) {
                    return
                }
                readOnly = false
                forceActiveFocus()
                selectAll()
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
                && !root.compactControls
        }
    }

    Flow {
        id: compactControlsFlow
        visible: root.compactControls
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: Theme.space.lg
        height: childrenRect.height
        spacing: Theme.space.md

        Loader {
            active: root.compactControls
            width: root.compactModelWidth
            height: 32
            sourceComponent: modelComboComponent
        }

        Loader {
            active: root.compactControls
            width: 104
            height: 32
            sourceComponent: permComboComponent
        }

        Loader {
            active: root.compactControls && root.sessionState && root.sessionState.effortLevels && root.sessionState.effortLevels.length > 0
            visible: active
            width: active ? 120 : 0
            height: active ? 32 : 0
            sourceComponent: effortComboComponent
        }

        Loader {
            active: root.compactControls && root.sessionState && root.sessionState.search !== ""
            visible: active
            width: active ? 104 : 0
            height: active ? 32 : 0
            sourceComponent: searchComboComponent
        }

        Loader {
            active: root.compactControls && root.sessionState && root.sessionState.fastOk
            visible: active
            width: active ? 80 : 0
            height: active ? 32 : 0
            sourceComponent: fastControlComponent
        }
    }
}
