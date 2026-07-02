import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Session control strip (model, effort, perm, title, goal)
Rectangle {
    id: root
    Layout.fillWidth: true
    Layout.preferredHeight: 48
    color: Theme.colors.bgWell
    border.width: 1
    border.color: Theme.colors.borderHairline

    property var sessionState  // SessionStateModel instance

    RowLayout {
        anchors.fill: parent
        anchors.margins: Theme.space.lg
        spacing: Theme.space.lg

        // Model badge (clickable → dropdown)
        ComboBox {
            id: modelCombo
            model: sessionState ? sessionState.catalog : []
            currentIndex: {
                if (!sessionState || !sessionState.catalog) return -1
                return sessionState.catalog.indexOf(sessionState.model)
            }
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.bodySm
            font.weight: Theme.fontWeight.medium

            onActivated: function(index) {
                if (sessionState && sessionState.catalog && index >= 0) {
                    sessionState.setModel(sessionState.catalog[index])
                }
            }

            background: Rectangle {
                color: modelCombo.hovered ? Theme.colors.stateHover : "transparent"
                border.width: 1
                border.color: Theme.colors.borderSubtle
                radius: Theme.radius.sm
                implicitWidth: 180
                implicitHeight: 32
            }

            contentItem: Label {
                text: sessionState ? sessionState.model : ""
                font: modelCombo.font
                color: Theme.colors.textPrimary
                verticalAlignment: Text.AlignVCenter
                leftPadding: Theme.space.md
            }

            delegate: ItemDelegate {
                width: parent.width
                text: modelData
                font: modelCombo.font
                highlighted: modelCombo.highlightedIndex === index

                background: Rectangle {
                    color: parent.highlighted ? Theme.colors.stateHover : "transparent"
                }

                contentItem: Label {
                    text: parent.text
                    font: parent.font
                    color: Theme.colors.textPrimary
                    verticalAlignment: Text.AlignVCenter
                }
            }
        }

        // Perm toggle (gated ↔ auto)
        ComboBox {
            id: permCombo
            model: ["gated", "auto"]
            currentIndex: {
                if (!sessionState) return 0
                return sessionState.perm === "auto" ? 1 : 0
            }
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.bodySm

            onActivated: function(index) {
                if (sessionState) {
                    sessionState.setPerm(index === 1 ? "auto" : "gated")
                }
            }

            background: Rectangle {
                color: permCombo.hovered ? Theme.colors.stateHover : "transparent"
                border.width: 1
                border.color: Theme.colors.borderSubtle
                radius: Theme.radius.sm
                implicitWidth: 100
                implicitHeight: 32
            }

            contentItem: Label {
                text: permCombo.currentText
                font: permCombo.font
                color: Theme.colors.textPrimary
                verticalAlignment: Text.AlignVCenter
                leftPadding: Theme.space.md
            }

            delegate: ItemDelegate {
                width: parent.width
                text: modelData
                font: permCombo.font
                highlighted: permCombo.highlightedIndex === index

                background: Rectangle {
                    color: parent.highlighted ? Theme.colors.stateHover : "transparent"
                }

                contentItem: Label {
                    text: parent.text
                    font: parent.font
                    color: Theme.colors.textPrimary
                    verticalAlignment: Text.AlignVCenter
                }
            }
        }

        // Effort selector (if model supports it)
        Loader {
            active: sessionState && sessionState.effortLevels && sessionState.effortLevels.length > 0
            sourceComponent: ComboBox {
                model: sessionState ? sessionState.effortLevels : []
                currentIndex: {
                    if (!sessionState || !sessionState.effortLevels) return -1
                    return sessionState.effortLevels.indexOf(sessionState.effort)
                }
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.bodySm

                onActivated: function(index) {
                    if (sessionState && sessionState.effortLevels && index >= 0) {
                        sessionState.setEffort(sessionState.effortLevels[index])
                    }
                }

                background: Rectangle {
                    color: parent.hovered ? Theme.colors.stateHover : "transparent"
                    border.width: 1
                    border.color: Theme.colors.borderSubtle
                    radius: Theme.radius.sm
                    implicitWidth: 100
                    implicitHeight: 32
                }

                contentItem: Label {
                    text: sessionState ? sessionState.effort : ""
                    font: parent.font
                    color: Theme.colors.textPrimary
                    verticalAlignment: Text.AlignVCenter
                    leftPadding: Theme.space.md
                }

                delegate: ItemDelegate {
                    width: parent.width
                    text: modelData
                    font: parent.font
                    highlighted: parent.highlightedIndex === index

                    background: Rectangle {
                        color: parent.highlighted ? Theme.colors.stateHover : "transparent"
                    }

                    contentItem: Label {
                        text: parent.text
                        font: parent.font
                        color: Theme.colors.textPrimary
                        verticalAlignment: Text.AlignVCenter
                    }
                }
            }
        }

        // Title (double-click to rename)
        TextField {
            id: titleField
            text: sessionState ? sessionState.title : ""
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.h3
            font.weight: Theme.fontWeight.semibold
            color: Theme.colors.textPrimary
            Layout.fillWidth: true
            readOnly: !activeFocus

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
            }

            MouseArea {
                anchors.fill: parent
                enabled: titleField.readOnly
                onDoubleClicked: {
                    titleField.readOnly = false
                    titleField.forceActiveFocus()
                    titleField.selectAll()
                }
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
