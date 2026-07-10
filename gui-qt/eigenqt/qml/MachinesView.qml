import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Machines view - local remote-host inventory with on-demand ssh drill-in.
Rectangle {
    id: root
    objectName: "machinesView"
    color: Theme.colors.bgBase

    property var machinesModel: null
    readonly property var machines: machinesModel ? machinesModel.machines || [] : []
    readonly property var selectedMachine: machinesModel ? machinesModel.selected_machine || ({}) : ({})
    readonly property var remoteSessions: machinesModel ? machinesModel.remote_sessions || [] : []
    readonly property bool hasSelection: String(selectedMachine.ssh || "") !== ""
    property bool copyCredentials: true
    readonly property int qaMachineCount: machinesModel ? machinesModel.machine_count : 0
    readonly property int qaRemoteCount: machinesModel ? machinesModel.remote_count : 0
    signal openSession(string sessionId)

    onMachinesModelChanged: syncActiveModel()
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
                        objectName: "machinesTitle"
                        text: "Machines"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.h3
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textPrimary
                    }

                    Label {
                        objectName: "machinesSummary"
                        text: summaryText()
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.micro
                        color: Theme.colors.textMuted
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }

                AppButton {
                    objectName: "machinesRefreshButton"
                    text: root.machinesModel && root.machinesModel.loading ? "Refreshing..." : "Refresh"
                    compact: true
                    toolTipText: "Refresh machines"
                    enabled: root.machinesModel && !root.machinesModel.loading
                    Layout.preferredWidth: Math.max(86, implicitWidth)
                    Layout.preferredHeight: 32
                    onClicked: if (root.machinesModel) root.machinesModel.refresh()
                }
            }
        }

        Flickable {
            id: machinesFlick
            objectName: "machinesFlick"
            Layout.fillWidth: true
            Layout.fillHeight: true
            contentWidth: width
            contentHeight: contentColumn.implicitHeight + Theme.space.xxl
            clip: true

            ColumnLayout {
                id: contentColumn
                width: Math.min(parent.width - Theme.space.xl * 2, 1080)
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: Theme.space.lg

                Item { Layout.preferredHeight: Theme.space.xl }

                Rectangle {
                    visible: root.machinesModel && root.machinesModel.loading && root.qaMachineCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 120 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    Label {
                        anchors.centerIn: parent
                        text: "Loading machines..."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textMuted
                    }
                }

                Rectangle {
                    objectName: "machinesLoadError"
                    visible: root.machinesModel && root.machinesModel.load_error !== "" && root.qaMachineCount === 0
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
                            text: "Could not load machines"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            objectName: "machinesLoadErrorText"
                            text: root.machinesModel ? root.machinesModel.load_error : ""
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            Layout.alignment: Qt.AlignHCenter
                        }

                        AppButton {
                            objectName: "machinesLoadErrorRetry"
                            text: "Retry"
                            compact: true
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: if (root.machinesModel) root.machinesModel.refresh()
                        }
                    }
                }

                RefreshErrorBanner {
                    objectName: "machinesRefreshErrorBanner"
                    visible: root.machinesModel && root.machinesModel.load_error !== "" && root.qaMachineCount > 0
                    message: root.machinesModel ? root.machinesModel.load_error : ""
                    textObjectName: "machinesRefreshErrorText"
                    retryObjectName: "machinesRefreshErrorRetry"
                    retryToolTipText: "Retry loading machines"
                    onRetry: if (root.machinesModel) root.machinesModel.refresh()
                }

                Rectangle {
                    visible: root.machinesModel && !root.machinesModel.loading && root.machinesModel.load_error === "" && root.qaMachineCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 132 : 0
                    radius: Theme.radius.md
                    color: Theme.colors.surfaceRaised
                    border.width: 1
                    border.color: Theme.colors.borderHairline

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.sm

                        Label {
                            text: "No machines yet"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            text: "eigen remote add"
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.codeSm
                            color: Theme.colors.textMuted
                            Layout.alignment: Qt.AlignHCenter
                        }
                    }
                }

                Item {
                    id: machinesContent
                    visible: root.qaMachineCount > 0
                    width: parent.width
                    height: stacked
                        ? machineGrid.height + Theme.space.lg + remotePanel.height
                        : Math.max(machineGrid.height, remotePanel.height)
                    Layout.fillWidth: true
                    Layout.preferredHeight: height
                    property bool stacked: width < 760

                    Flow {
                        id: machineGrid
                        x: 0
                        y: 0
                        width: machinesContent.stacked
                            ? parent.width
                            : Math.max(0, parent.width - remotePanel.width - Theme.space.lg)
                        height: childrenRect.height
                        spacing: Theme.space.md
                        property int columnCount: Math.max(1, Math.floor(width / 304))
                        property real cardWidth: Math.floor((width - (columnCount - 1) * spacing) / columnCount)

                        Repeater {
                            model: root.machines
                            delegate: Rectangle {
                                id: machineCard
                                readonly property var machine: modelData || ({})
                                readonly property string ssh: String(machine.ssh || "")
                                readonly property bool selected: root.hasSelection && String(root.selectedMachine.ssh || "") === ssh
                                readonly property bool qaTextFits: !machineNameLabel.truncated && !machineSshLabel.truncated && !machineDirBaseLabel.truncated
                                objectName: "machinesCard_" + root.safeObjectName(ssh || machine.name || index)
                                width: machineGrid.cardWidth
                                height: Math.max(152, machineColumn.implicitHeight + Theme.space.xl)
                                radius: Theme.radius.md
                                color: selected ? Theme.colors.stateSelected : (machineMouse.containsMouse ? Theme.colors.surfaceRaised2 : Theme.colors.surfaceRaised)
                                border.width: 1
                                border.color: selected ? Theme.colors.borderBrandFaint : Theme.colors.borderHairline

                                MouseArea {
                                    id: machineMouse
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: if (root.machinesModel) root.machinesModel.select_machine(machineCard.ssh)
                                }

                                ColumnLayout {
                                    id: machineColumn
                                    anchors.fill: parent
                                    anchors.margins: Theme.space.lg
                                    spacing: Theme.space.md

                                    RowLayout {
                                        Layout.fillWidth: true
                                        spacing: Theme.space.sm

                                        Label {
                                            id: machineNameLabel
                                            text: machine.name || machineCard.ssh || "unknown"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.bodySm
                                            font.weight: Theme.fontWeight.semibold
                                            color: Theme.colors.textPrimary
                                            elide: Text.ElideRight
                                            Layout.fillWidth: true
                                        }

                                        Flow {
                                            spacing: Theme.space.xs
                                            width: Math.min(144, implicitWidth)
                                            clip: true
                                            Layout.preferredWidth: width
                                            Layout.minimumWidth: 0
                                            Layout.maximumWidth: 144

                                            Repeater {
                                                model: root.machineTags(machine)
                                                delegate: AppTag {
                                                    text: String(modelData)
                                                    backgroundColor: modelData === "saved" ? Theme.colors.brandBg : Theme.colors.accentBg
                                                    borderColor: modelData === "saved" ? Theme.colors.borderBrandFaint : Theme.colors.borderAccentFaint
                                                    textColor: Theme.colors.textSecondary
                                                    minimumHeight: 20
                                                }
                                            }
                                        }
                                    }

                                    Label {
                                        id: machineSshLabel
                                        text: machineCard.ssh
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.codeSm
                                        color: Theme.colors.textSecondary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    GridLayout {
                                        visible: String(machine.addr || "") !== "" || String(machine.dir || "") !== ""
                                        Layout.fillWidth: true
                                        columns: 2
                                        columnSpacing: Theme.space.lg
                                        rowSpacing: Theme.space.xs

                                        Label {
                                            visible: String(machine.addr || "") !== ""
                                            text: "addr"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.micro
                                            font.capitalization: Font.AllUppercase
                                            color: Theme.colors.textFaint
                                        }

                                        Label {
                                            visible: String(machine.addr || "") !== ""
                                            text: machine.addr || ""
                                            font.family: Theme.monoFonts[0]
                                            font.pixelSize: Theme.fontSize.codeSm
                                            color: Theme.colors.textMuted
                                            elide: Text.ElideRight
                                            Layout.fillWidth: true
                                        }

                                        Label {
                                            visible: String(machine.dir || "") !== ""
                                            text: "dir"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.micro
                                            font.capitalization: Font.AllUppercase
                                            color: Theme.colors.textFaint
                                        }

                                        ColumnLayout {
                                            visible: String(machine.dir || "") !== ""
                                            Layout.fillWidth: true
                                            spacing: 1

                                            Label {
                                                id: machineDirBaseLabel
                                                text: root.baseName(machine.dir || "")
                                                font.family: Theme.uiFonts[0]
                                                font.pixelSize: Theme.fontSize.codeSm
                                                color: Theme.colors.textSecondary
                                                elide: Text.ElideRight
                                                Layout.fillWidth: true
                                            }

                                            Label {
                                                text: root.normalPath(machine.dir || "")
                                                font.family: Theme.monoFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textFaint
                                                wrapMode: Text.WrapAnywhere
                                                Layout.fillWidth: true
                                            }
                                        }
                                    }

                                    Flow {
                                        Layout.fillWidth: true
                                        spacing: Theme.space.xs

                                        Repeater {
                                            model: root.machineBadges(machine)
                                            delegate: AppTag {
                                                text: String(modelData)
                                                backgroundColor: Theme.colors.bgOverlay
                                                borderColor: Theme.colors.borderHairline
                                                textColor: Theme.colors.textSecondary
                                                minimumHeight: 21
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }

                    Rectangle {
                        id: remotePanel
                        objectName: "machinesRemotePanel"
                        width: Math.min(340, parent.width)
                        height: Math.max(284, remoteColumn.implicitHeight + Theme.space.xl)
                        x: machinesContent.stacked ? 0 : parent.width - width
                        y: machinesContent.stacked ? machineGrid.height + Theme.space.lg : 0
                        radius: Theme.radius.md
                        color: Theme.colors.surfaceRaised
                        border.width: 1
                        border.color: Theme.colors.borderHairline

                        ColumnLayout {
                            id: remoteColumn
                            anchors.fill: parent
                            anchors.margins: Theme.space.lg
                            spacing: Theme.space.md

                            RowLayout {
                                Layout.fillWidth: true
                                spacing: Theme.space.sm

                                ColumnLayout {
                                    Layout.fillWidth: true
                                    spacing: 2

                                    Label {
                                        id: remoteTitleLabel
                                        text: root.hasSelection ? (root.selectedMachine.name || root.selectedMachine.ssh) : "No host selected"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.body
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.textPrimary
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        objectName: "machinesSelectedSsh"
                                        visible: root.hasSelection
                                        text: root.selectedMachine.ssh || ""
                                        font.family: Theme.monoFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textMuted
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }
                                }

                                AppButton {
                                    visible: root.hasSelection
                                    text: "X"
                                    compact: true
                                    toolTipText: "Close remote sessions"
                                    Layout.preferredWidth: 28
                                    Layout.preferredHeight: 28
                                    onClicked: if (root.machinesModel) root.machinesModel.clear_selection()
                                }
                            }

                            Rectangle {
                                id: installPanel
                                objectName: "machinesInstallPanel"
                                visible: root.hasSelection && root.machinesModel
                                implicitHeight: visible ? 120 : 0
                                width: parent ? parent.width : 0
                                Layout.fillWidth: true
                                Layout.minimumHeight: visible ? 120 : 0
                                Layout.preferredHeight: visible ? 120 : 0
                                radius: Theme.radius.sm
                                color: Theme.colors.bgWell
                                border.width: 1
                                border.color: Theme.colors.borderHairline

                                Column {
                                    id: installColumn
                                    anchors.fill: parent
                                    anchors.margins: Theme.space.lg
                                    spacing: Theme.space.sm

                                    RowLayout {
                                        width: parent.width
                                        height: 28
                                        spacing: Theme.space.sm

                                        Label {
                                            text: "Eigen on this host"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.bodySm
                                            font.weight: Theme.fontWeight.semibold
                                            color: Theme.colors.textPrimary
                                            Layout.fillWidth: true
                                            elide: Text.ElideRight
                                        }

                                        AppButton {
                                            id: installButton
                                            objectName: "machinesInstallButton"
                                            text: root.machinesModel && root.machinesModel.installing ? "Installing..." : "Install / update"
                                            compact: true
                                            variant: "primary"
                                            toolTipText: "Install or update Eigen on this SSH host"
                                            enabled: root.machinesModel && !root.machinesModel.installing
                                            Layout.preferredWidth: Math.max(116, implicitWidth)
                                            Layout.preferredHeight: 28
                                            onClicked: if (root.machinesModel) root.machinesModel.install_machine(root.selectedMachine.ssh || "", credentialsSwitch.checked)
                                        }
                                    }

                                    Label {
                                        text: "Uses this SSH profile and installs to ~/.local/bin/eigen."
                                        width: parent.width
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textMuted
                                        wrapMode: Text.Wrap
                                    }

                                    RowLayout {
                                        width: parent.width
                                        height: 24
                                        spacing: Theme.space.sm

                                        Label {
                                            text: "Copy local daemon credentials"
                                            font.family: Theme.uiFonts[0]
                                            font.pixelSize: Theme.fontSize.micro
                                            color: Theme.colors.textSecondary
                                            Layout.fillWidth: true
                                            elide: Text.ElideRight
                                        }

                                        AppSwitch {
                                            id: credentialsSwitch
                                            objectName: "machinesCredentialsSwitch"
                                            checked: root.copyCredentials
                                            enabled: root.machinesModel && !root.machinesModel.installing
                                            accessibleName: "Copy local daemon credentials"
                                            toolTipText: "Copy available local daemon credentials to run remote sessions"
                                            onToggled: root.copyCredentials = checked
                                        }
                                    }
                                }
                            }

                            Rectangle {
                                objectName: "machinesInstallProgress"
                                visible: root.hasSelection && root.machinesModel && root.machinesModel.installing
                                width: parent ? parent.width : 0
                                Layout.fillWidth: true
                                Layout.preferredHeight: visible ? 40 : 0
                                radius: Theme.radius.sm
                                color: Theme.colors.workingBg
                                border.width: 1
                                border.color: Theme.colors.warn

                                RowLayout {
                                    anchors.fill: parent
                                    anchors.leftMargin: Theme.space.lg
                                    anchors.rightMargin: Theme.space.lg
                                    spacing: Theme.space.sm

                                    Rectangle {
                                        width: 7
                                        height: 7
                                        radius: 4
                                        color: Theme.colors.working
                                    }

                                    Label {
                                        text: "Installing Eigen over ssh..."
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.label
                                        color: Theme.colors.textSecondary
                                        Layout.fillWidth: true
                                    }
                                }
                            }

                            Rectangle {
                                objectName: "machinesInstallMessage"
                                visible: root.hasSelection && root.machinesModel && root.machinesModel.install_message !== ""
                                width: parent ? parent.width : 0
                                Layout.fillWidth: true
                                Layout.preferredHeight: visible ? Math.max(40, installMessageText.implicitHeight + Theme.space.lg * 2) : 0
                                radius: Theme.radius.sm
                                color: Theme.colors.successBg
                                border.width: 1
                                border.color: Theme.colors.success

                                Label {
                                    id: installMessageText
                                    anchors.fill: parent
                                    anchors.margins: Theme.space.lg
                                    text: root.machinesModel ? root.machinesModel.install_message : ""
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textSecondary
                                    wrapMode: Text.Wrap
                                    verticalAlignment: Text.AlignVCenter
                                }
                            }

                            Rectangle {
                                objectName: "machinesInstallError"
                                visible: root.hasSelection && root.machinesModel && root.machinesModel.install_error !== ""
                                width: parent ? parent.width : 0
                                Layout.fillWidth: true
                                Layout.preferredHeight: visible ? Math.max(64, installErrorText.implicitHeight + Theme.space.xxxl) : 0
                                radius: Theme.radius.sm
                                color: Theme.colors.errorBg
                                border.width: 1
                                border.color: Theme.colors.error

                                ColumnLayout {
                                    anchors.fill: parent
                                    anchors.margins: Theme.space.lg
                                    spacing: Theme.space.xs

                                    Label {
                                        text: "Could not install Eigen"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.label
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.error
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        id: installErrorText
                                        text: root.machinesModel ? root.machinesModel.install_error : ""
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.micro
                                        color: Theme.colors.textSecondary
                                        wrapMode: Text.Wrap
                                        Layout.fillWidth: true
                                    }
                                }
                            }

                            Rectangle {
                                visible: !root.hasSelection
                                Layout.fillWidth: true
                                Layout.preferredHeight: visible ? 104 : 0
                                radius: Theme.radius.sm
                                color: Theme.colors.bgWell
                                border.width: 1
                                border.color: Theme.colors.borderHairline

                                Label {
                                    anchors.centerIn: parent
                                    text: "Remote sessions"
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textMuted
                                }
                            }

                            RowLayout {
                                objectName: "machinesRemoteDialing"
                                visible: root.hasSelection && root.machinesModel && root.machinesModel.remote_loading
                                Layout.fillWidth: true
                                Layout.preferredHeight: visible ? 52 : 0
                                spacing: Theme.space.md

                                Rectangle {
                                    width: 14
                                    height: 14
                                    radius: 7
                                    color: "transparent"
                                    border.width: 2
                                    border.color: Theme.colors.brandBright
                                    Layout.alignment: Qt.AlignVCenter

                                    SequentialAnimation on rotation {
                                        running: Theme.continuousMotion && root.hasSelection && root.machinesModel && root.machinesModel.remote_loading
                                        loops: Animation.Infinite
                                        NumberAnimation { from: 0; to: 360; duration: 700 }
                                    }
                                }

                                Label {
                                    text: "Dialing over ssh..."
                                    font.family: Theme.uiFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textMuted
                                    Layout.fillWidth: true
                                }
                            }

                            Rectangle {
                                objectName: "machinesRemoteError"
                                visible: root.hasSelection && root.machinesModel && root.machinesModel.remote_error !== ""
                                Layout.fillWidth: true
                                Layout.preferredHeight: visible ? Math.max(86, remoteErrorText.implicitHeight + Theme.space.xxxxl) : 0
                                radius: Theme.radius.sm
                                color: Theme.colors.errorBg
                                border.width: 1
                                border.color: Theme.colors.error

                                ColumnLayout {
                                    anchors.fill: parent
                                    anchors.margins: Theme.space.lg
                                    spacing: Theme.space.sm

                                    Label {
                                        text: root.remoteNeedsInstall(root.machinesModel ? root.machinesModel.remote_error : "")
                                            ? "Eigen is not installed yet"
                                            : "Could not reach host"
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        font.weight: Theme.fontWeight.semibold
                                        color: Theme.colors.error
                                        Layout.fillWidth: true
                                    }

                                    Label {
                                        id: remoteErrorText
                                        objectName: "machinesRemoteErrorText"
                                        text: root.machinesModel ? root.machinesModel.remote_error : ""
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.label
                                        color: Theme.colors.textSecondary
                                        wrapMode: Text.Wrap
                                        Layout.fillWidth: true
                                    }

                                    RowLayout {
                                        Layout.fillWidth: true
                                        spacing: Theme.space.sm

                                        AppButton {
                                            objectName: "machinesRemoteInstallButton"
                                            text: "Install Eigen"
                                            compact: true
                                            variant: "primary"
                                            enabled: root.machinesModel && !root.machinesModel.installing
                                            onClicked: if (root.machinesModel) root.machinesModel.install_machine(root.selectedMachine.ssh || "", credentialsSwitch.checked)
                                        }

                                        AppButton {
                                            objectName: "machinesRemoteErrorRetry"
                                            text: "Retry"
                                            compact: true
                                            variant: "secondary"
                                            enabled: root.machinesModel && !root.machinesModel.installing
                                            onClicked: if (root.machinesModel) root.machinesModel.select_machine(root.selectedMachine.ssh || "")
                                        }
                                    }
                                }
                            }

                            Label {
                                visible: root.hasSelection && root.machinesModel && !root.machinesModel.remote_loading && root.machinesModel.remote_error === "" && root.qaRemoteCount === 0
                                text: "No active sessions on this host."
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.bodySm
                                color: Theme.colors.textMuted
                                Layout.fillWidth: true
                            }

                            ColumnLayout {
                                visible: root.hasSelection && root.qaRemoteCount > 0
                                Layout.fillWidth: true
                                spacing: Theme.space.sm

                                Label {
                                    text: String(root.qaRemoteCount) + " remote " + (root.qaRemoteCount === 1 ? "session" : "sessions")
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.micro
                                    color: Theme.colors.textMuted
                                    Layout.fillWidth: true
                                }

                                Repeater {
                                    model: root.remoteSessions
                                    delegate: Rectangle {
                                        readonly property var sessionInfo: modelData || ({})
                                        readonly property string sessionId: String(sessionInfo.id || "")
                                        readonly property bool qaTextFits: !sessionTitleLabel.truncated && !sessionDirLabel.truncated && openButton.qaTextFits
                                        objectName: "machinesRemoteRow_" + root.safeObjectName(sessionId || index)
                                        Layout.fillWidth: true
                                        implicitHeight: 54
                                        radius: Theme.radius.sm
                                        color: Theme.colors.bgWell
                                        border.width: 1
                                        border.color: Theme.colors.borderHairline

                                        RowLayout {
                                            anchors.fill: parent
                                            anchors.leftMargin: Theme.space.md
                                            anchors.rightMargin: Theme.space.md
                                            spacing: Theme.space.sm

                                            Rectangle {
                                                width: 7
                                                height: 7
                                                radius: 4
                                                color: root.sessionDot(sessionInfo.status || "")
                                                Layout.alignment: Qt.AlignVCenter
                                            }

                                            ColumnLayout {
                                                Layout.fillWidth: true
                                                spacing: 1

                                                Label {
                                                    id: sessionTitleLabel
                                                    text: sessionInfo.title || "untitled session"
                                                    font.family: Theme.uiFonts[0]
                                                    font.pixelSize: Theme.fontSize.bodySm
                                                    font.weight: Theme.fontWeight.medium
                                                    color: Theme.colors.textPrimary
                                                    elide: Text.ElideRight
                                                    Layout.fillWidth: true
                                                }

                                                Label {
                                                    id: sessionDirLabel
                                                    text: root.baseName(sessionInfo.dir || "")
                                                    font.family: Theme.uiFonts[0]
                                                    font.pixelSize: Theme.fontSize.micro
                                                    color: Theme.colors.textMuted
                                                    elide: Text.ElideRight
                                                    Layout.fillWidth: true
                                                }
                                            }

                                            Label {
                                                visible: String(sessionInfo.model || "") !== ""
                                                text: sessionInfo.model || ""
                                                font.family: Theme.monoFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textMuted
                                                elide: Text.ElideRight
                                                Layout.maximumWidth: 76
                                            }

                                            Label {
                                                text: String(sessionInfo.turns || 0)
                                                font.family: Theme.monoFonts[0]
                                                font.pixelSize: Theme.fontSize.micro
                                                color: Theme.colors.textGhost
                                                Layout.minimumWidth: 18
                                                horizontalAlignment: Text.AlignRight
                                            }

                                            AppButton {
                                                id: openButton
                                                objectName: "machinesRemoteOpen_" + root.safeObjectName(sessionId || index)
                                                text: "Open"
                                                compact: true
                                                variant: "primary"
                                                toolTipText: "Open remote session"
                                                Layout.preferredWidth: Math.max(64, implicitWidth)
                                                Layout.preferredHeight: 28
                                                onClicked: root.openSession(sessionId)
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }

                Item { Layout.preferredHeight: Theme.space.xl }
            }
        }
    }

    function syncActiveModel(activeOverride) {
        if (root.machinesModel && root.machinesModel.set_active) {
            root.machinesModel.set_active(activeOverride === undefined ? root.visible : activeOverride)
        }
    }

    function summaryText() {
        if (!root.machinesModel) return "no machines"
        if (root.qaMachineCount === 0 && root.machinesModel.loading) return "loading machines"
        return String(root.qaMachineCount) + " hosts / " + String(root.machinesModel.saved_count || 0)
            + " saved / " + String(root.machinesModel.detected_count || 0) + " detected"
    }

    function machineTags(machine) {
        var tags = []
        if (machine.saved) tags.push("saved")
        if (machine.detected) tags.push("detected")
        return tags
    }

    function machineBadges(machine) {
        var badges = []
        if (machine.model) badges.push(String(machine.model))
        if (machine.perm) badges.push(String(machine.perm))
        return badges
    }

    function normalPath(path) {
        return String(path || "").replace(/\\/g, "/")
    }

    function baseName(path) {
        var text = normalPath(path).replace(/\/+$/, "")
        if (text.length === 0) return "-"
        var parts = text.split("/")
        return parts[parts.length - 1] || text
    }

    function sessionDot(status) {
        if (status === "working") return Theme.colors.dotWorking
        if (status === "approval") return Theme.colors.dotWarn
        if (status === "error") return Theme.colors.dotError
        return Theme.colors.dotIdle
    }

    function remoteNeedsInstall(errorText) {
        var text = String(errorText || "").toLowerCase()
        return text.indexOf("no eigen") >= 0 || text.indexOf("eigen: not found") >= 0
            || text.indexOf("eigen: command not found") >= 0
    }

    function safeObjectName(value) {
        return String(value || "").replace(/[^A-Za-z0-9_]/g, "_")
    }
}
