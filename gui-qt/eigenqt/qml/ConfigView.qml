import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// Config view — editable ~/.eigen/config.json as a typed form.
// Tabbed categories (General/Models/Behavior/Integrations/Advanced) per Svelte's pattern.
// Each field renders by its shape: boolean toggle, select for closed sets, chips for multi-select, or text input.
// Every change persists through SetConfig RPC; invalid values are rejected with a toast and the field reverts.
Rectangle {
    id: root
    color: Theme.colors.bgBase

    property var configModel: null      // ConfigModel from Python
    property var ruleChainsModel: null  // RuleChainsModel from Python

    // Tab state
    readonly property var tabOrder: ["General", "Models", "Behavior", "Integrations", "Advanced"]
    property string activeTab: "General"
    // Function-call bindings are NOT reactive to model resets (the recurring
    // QML footgun) — Config data lands AFTER first render, so tabFields()
    // bindings froze on the initial empty model → black content pane.
    property var currentTabs: []
    property var currentTabFields
    property int configFieldCount: 0
    property int ruleChainCount: 0
    readonly property bool qaEmptyStateVisible: configEmptyState.visible
    onActiveTabChanged: currentTabFields = tabFields()
    onConfigModelChanged: {
        syncModelCounts()
        syncValuesFromModel()
        currentTabs = availableTabs()
        currentTabFields = tabFields()
        syncActiveModels()
    }
    onRuleChainsModelChanged: {
        syncModelCounts()
        syncActiveModels()
    }
    onVisibleChanged: syncActiveModels()

    // Key → tab mapping (matches Svelte Config.svelte)
    property var keyTab: ({
        model: "General", perm: "General", input_mode: "General", effort: "General",
        theme: "General", nerd_font: "General", max_tokens: "General",
        judge_model: "Models", dream_model: "Models", route_model: "Models",
        route: "Models", route_providers: "Models",
        dream_on_idle: "Behavior", dream_batch: "Behavior", idle_minutes: "Behavior",
        front_window_min: "Behavior", stall_idle_min: "Behavior",
        tts_cmd: "Integrations", notify_cmd: "Integrations", telegram_token: "Integrations",
        observe: "Advanced", local_background: "Advanced", daemon_timeout: "Advanced"
    })

    // Saving state (per key)
    property var saving: ({})
    property var ruleChainSaving: ({})
    readonly property int qaConfigSavingCount: Object.keys(saving || {}).length
    readonly property int qaRuleChainSavingCount: Object.keys(ruleChainSaving || {}).length

    // Working values (optimistic update before commit)
    property var values
    property string actionError: ""

    // Visual QA hook: lets the screenshot harness prove dropdown popups.
    property string qaOpenCombo: ""

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // CONFIG PATH (pinned header)
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 36
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline

            Label {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xxxl
                anchors.rightMargin: Theme.space.xxxl
                text: root.configModel ? root.configModel.config_path : ""
                font.family: Theme.monoFonts[0]
                font.pixelSize: 11
                color: Theme.colors.textFaint
                verticalAlignment: Text.AlignVCenter
                elide: Text.ElideMiddle
            }
        }

        // TABS STRIP
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 40
            color: Theme.colors.bgBase
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xxxl
                spacing: 0

                Repeater {
                    model: root.currentTabs || []
                    delegate: AppButton {
                        id: tabButton
                        objectName: "configTab_" + modelData
                        text: modelData
                        variant: "ghost"
                        selected: root.activeTab === modelData
                        compact: true
                        toolTipText: "Show " + modelData + " settings"
                        Layout.fillWidth: false
                        Layout.preferredWidth: Math.ceil(tabButton.implicitWidth + Theme.space.xs)
                        Layout.minimumWidth: Layout.preferredWidth
                        Layout.preferredHeight: 40
                        onClicked: root.activeTab = modelData
                    }
                }

                Item {
                    Layout.fillWidth: true
                    Layout.preferredHeight: 1
                }
            }
        }

        Rectangle {
            objectName: "configActionError"
            visible: root.actionError !== ""
            Layout.fillWidth: true
            Layout.preferredHeight: visible ? Math.max(40, configActionErrorRow.implicitHeight + Theme.space.md) : 0
            color: Theme.colors.errorBg
            border.width: 1
            border.color: Theme.colors.error
            clip: true

            RowLayout {
                id: configActionErrorRow
                anchors.fill: parent
                anchors.leftMargin: Theme.space.xxxl
                anchors.rightMargin: Theme.space.xxxl
                spacing: Theme.space.md

                Label {
                    text: root.actionError
                    font.family: Theme.uiFonts[0]
                    font.pixelSize: Theme.fontSize.label
                    color: Theme.colors.error
                    wrapMode: Text.WrapAnywhere
                    Layout.fillWidth: true
                }

                AppButton {
                    objectName: "configDismissErrorButton"
                    text: "X"
                    variant: "ghost"
                    compact: true
                    toolTipText: "Dismiss error"
                    Layout.preferredWidth: 28
                    Layout.preferredHeight: 28
                    onClicked: root.actionError = ""
                }
            }
        }

        // TAB CONTENT
        Flickable {
            objectName: "configFlick"
            Layout.fillWidth: true
            Layout.fillHeight: true
            contentWidth: width
            contentHeight: contentColumn.implicitHeight
            clip: true

            ColumnLayout {
                id: contentColumn
                width: Math.min(parent.width - Theme.space.xxxxl, 880)
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: Theme.space.lg

                Item { height: Theme.space.lg }

                Rectangle {
                    objectName: "configLoadError"
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
                            text: "Could not load config"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.body
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textPrimary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            objectName: "configLoadErrorText"
                            text: root.configLoadError()
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.textMuted
                            elide: Text.ElideRight
                            Layout.maximumWidth: 720
                            Layout.alignment: Qt.AlignHCenter
                        }

                        AppButton {
                            objectName: "configLoadErrorRetry"
                            text: "Retry"
                            compact: true
                            toolTipText: "Retry loading config"
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: root.retryLoad()
                        }
                    }
                }

                Rectangle {
                    objectName: "configRefreshErrorBanner"
                    visible: root.hasRefreshLoadError()
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? Math.max(44, configRefreshErrorRow.implicitHeight + Theme.space.md) : 0
                    color: Theme.colors.errorBg
                    border.width: visible ? 1 : 0
                    border.color: Theme.colors.error
                    radius: Theme.radius.md
                    clip: true

                    RowLayout {
                        id: configRefreshErrorRow
                        anchors.fill: parent
                        anchors.leftMargin: Theme.space.lg
                        anchors.rightMargin: Theme.space.lg
                        spacing: Theme.space.md

                        Label {
                            objectName: "configRefreshErrorText"
                            text: root.configLoadError()
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.label
                            color: Theme.colors.error
                            wrapMode: Text.WrapAnywhere
                            Layout.fillWidth: true
                        }

                        AppButton {
                            objectName: "configRefreshErrorRetry"
                            text: "Retry"
                            compact: true
                            toolTipText: "Retry loading config"
                            Layout.preferredWidth: 64
                            Layout.preferredHeight: 28
                            onClicked: root.retryLoad()
                        }
                    }
                }

                // RULE CHAINS EDITOR (Models tab only)
                Rectangle {
                    readonly property bool active: root.activeTab === "Models" && root.ruleChainsModel
                    visible: true
                    enabled: active
                    opacity: active ? 1 : 0
                    clip: true
                    Layout.fillWidth: true
                    Layout.preferredHeight: active ? implicitHeight : 0
                    Layout.minimumHeight: active ? 260 : 0
                    Layout.maximumHeight: active ? implicitHeight : 0
                    implicitHeight: Math.max(260, ruleChainsContent.implicitHeight + Theme.space.xxxl)
                    color: "transparent"

                    ColumnLayout {
                        id: ruleChainsContent
                        anchors.fill: parent
                        spacing: Theme.space.lg

                        // Header
                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: Theme.space.sm

                            Label {
                                text: "Model fallback chains"
                                font.family: Theme.uiFonts[0]
                                font.pixelSize: Theme.fontSize.h3
                                font.weight: Theme.fontWeight.semibold
                                color: Theme.colors.textPrimary
                            }

                            Label {
                                text: "Each role tries its models in order — the first reachable one answers, and a quota/billing failure falls through to the next (the drained provider is frozen for the day). If the whole chain is exhausted, the request fails."
                                font.pixelSize: Theme.fontSize.label
                                color: Theme.colors.textMuted
                                wrapMode: Text.WordWrap
                                Layout.fillWidth: true
                            }
                        }

                        // Role chains list
                        Repeater {
                            model: root.ruleChainsModel
                            delegate: Rectangle {
                                id: chainDelegate
                                // Capture the outer Repeater's roles once —
                                // nested Repeaters re-bind `model`, so
                                // parent.parent chains to reach the outer
                                // model were fragile and threw on undefined.
                                required property var model
                                readonly property bool chainSaving: !!root.ruleChainSaving[model.roleName]
                                Layout.fillWidth: true
                                Layout.preferredHeight: implicitHeight
                                Layout.minimumHeight: 96
                                implicitHeight: Math.max(96, chainRow.implicitHeight + Theme.space.xxxl)
                                color: Theme.colors.bgRaised
                                border.width: 1
                                border.color: Theme.colors.borderHairline
                                radius: Theme.radius.md

                                RowLayout {
                                    id: chainRow
                                    anchors.fill: parent
                                    anchors.margins: Theme.space.lg
                                    spacing: Theme.space.xxxl

                                    // Role info (left)
                                    ColumnLayout {
                                        Layout.preferredWidth: 180
                                        Layout.alignment: Qt.AlignTop
                                        spacing: Theme.space.xs

                                        RowLayout {
                                            spacing: Theme.space.sm

                                            Label {
                                                text: model.roleName
                                                font.family: Theme.monoFonts[0]
                                                font.pixelSize: Theme.fontSize.bodySm
                                                font.weight: Theme.fontWeight.semibold
                                                color: Theme.colors.textPrimary
                                            }

                                            AppTag {
                                                visible: model.custom
                                                text: "custom"
                                                backgroundColor: Theme.colors.stateSelected
                                                borderColor: Theme.colors.borderBrandFaint
                                                textColor: Theme.colors.brand
                                                fontPixelSize: 10
                                                fontWeight: Theme.fontWeight.semibold
                                                minimumHeight: 18
                                                pill: false
                                            }
                                        }

                                        Label {
                                            text: model.desc
                                            font.pixelSize: Theme.fontSize.label
                                            color: Theme.colors.textMuted
                                            wrapMode: Text.WordWrap
                                            Layout.fillWidth: true
                                        }
                                    }

                                    // Chain (right)
                                    ColumnLayout {
                                        Layout.fillWidth: true
                                        spacing: Theme.space.sm

                                        // Chain display (ordered list)
                                        ColumnLayout {
                                            visible: model.chain && model.chain.length > 0
                                            Layout.fillWidth: true
                                            spacing: Theme.space.xs

                                            Repeater {
                                                model: (chainDelegate.model && chainDelegate.model.chain) ? chainDelegate.model.chain : []
                                                delegate: Rectangle {
                                                    Layout.fillWidth: true
                                                    implicitHeight: 32
                                                    color: Theme.colors.bgBase
                                                    border.width: 1
                                                    border.color: Theme.colors.borderSubtle
                                                    radius: Theme.radius.sm

                                                    RowLayout {
                                                        anchors.fill: parent
                                                        anchors.margins: Theme.space.sm
                                                        spacing: Theme.space.sm

                                                        Label {
                                                            text: (index + 1).toString()
                                                            font.family: Theme.monoFonts[0]
                                                            font.pixelSize: 11
                                                            color: Theme.colors.textFaint
                                                            Layout.preferredWidth: 20
                                                        }

                                                        Label {
                                                            text: modelData
                                                            font.family: Theme.monoFonts[0]
                                                            font.pixelSize: Theme.fontSize.bodySm
                                                            color: Theme.colors.textPrimary
                                                            Layout.fillWidth: true
                                                        }

                                                        AppButton {
                                                            objectName: "configChainMoveUp_" + chainDelegate.model.roleName + "_" + index
                                                            text: "↑"
                                                            toolTipText: "Move " + modelData + " earlier"
                                                            variant: "ghost"
                                                            enabled: !chainDelegate.chainSaving && index > 0
                                                            leftPadding: Theme.space.sm
                                                            rightPadding: Theme.space.sm
                                                            Layout.preferredWidth: 28
                                                            Layout.preferredHeight: 24
                                                            onClicked: moveChainItem(chainDelegate.model.roleName, index, -1)
                                                        }

                                                        AppButton {
                                                            objectName: "configChainMoveDown_" + chainDelegate.model.roleName + "_" + index
                                                            text: "↓"
                                                            toolTipText: "Move " + modelData + " later"
                                                            variant: "ghost"
                                                            enabled: !chainDelegate.chainSaving && index < (chainDelegate.model.chain.length - 1)
                                                            leftPadding: Theme.space.sm
                                                            rightPadding: Theme.space.sm
                                                            Layout.preferredWidth: 28
                                                            Layout.preferredHeight: 24
                                                            onClicked: moveChainItem(chainDelegate.model.roleName, index, 1)
                                                        }

                                                        AppButton {
                                                            objectName: "configChainRemove_" + chainDelegate.model.roleName + "_" + index
                                                            text: "✕"
                                                            toolTipText: "Remove " + modelData
                                                            variant: "danger"
                                                            enabled: !chainDelegate.chainSaving
                                                            leftPadding: Theme.space.sm
                                                            rightPadding: Theme.space.sm
                                                            Layout.preferredWidth: 28
                                                            Layout.preferredHeight: 24
                                                            onClicked: removeChainItem(chainDelegate.model.roleName, index)
                                                        }
                                                    }
                                                }
                                            }
                                        }

                                        Label {
                                            visible: !model.chain || model.chain.length === 0
                                            text: "(default)"
                                            font.pixelSize: Theme.fontSize.label
                                            color: Theme.colors.textFaint
                                        }

                                        // Add controls
                                        RowLayout {
                                            Layout.fillWidth: true
                                            spacing: Theme.space.sm

                                            AppComboBox {
                                                id: addModelCombo
                                                objectName: "configAddModelCombo_" + chainDelegate.model.roleName
                                                qaPopupOpen: root.qaOpenCombo === ("configAddModelCombo_" + chainDelegate.model.roleName)
                                                Layout.fillWidth: true
                                                Layout.preferredHeight: 32
                                                enabled: !chainDelegate.chainSaving
                                                model: availableModelsForRole(chainDelegate.model.roleName, chainDelegate.model.chain, chainDelegate.model.models)
                                                displayText: currentIndex >= 0 ? currentText : "Add model…"
                                                accessibleName: "Model to add to " + chainDelegate.model.roleName
                                                toolTipText: "Choose a model to add"
                                            }

                                            AppButton {
                                                objectName: "configAddModelButton_" + chainDelegate.model.roleName
                                                text: "Add"
                                                variant: "primary"
                                                enabled: !chainDelegate.chainSaving && addModelCombo.currentIndex >= 0
                                                onClicked: {
                                                    if (addModelCombo.currentIndex >= 0) {
                                                        addChainItem(chainDelegate.model.roleName, addModelCombo.currentText)
                                                        addModelCombo.currentIndex = -1
                                                    }
                                                }
                                            }

                                            AppButton {
                                                objectName: "configResetChain_" + chainDelegate.model.roleName
                                                text: "Reset"
                                                enabled: !chainDelegate.chainSaving && model.custom
                                                onClicked: resetChain(chainDelegate.model.roleName)
                                            }

                                            RowLayout {
                                                objectName: "configChainSaving_" + chainDelegate.model.roleName
                                                visible: chainDelegate.chainSaving
                                                spacing: Theme.space.xs
                                                Layout.preferredHeight: visible ? 24 : 0

                                                Rectangle {
                                                    Layout.preferredWidth: 10
                                                    Layout.preferredHeight: 10
                                                    radius: 5
                                                    color: Theme.colors.working
                                                }

                                                Label {
                                                    text: "saving"
                                                    font.pixelSize: Theme.fontSize.label
                                                    color: Theme.colors.working
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }

                // CONFIG FIELDS (current tab)
                Repeater {
                    model: root.currentTabFields || []
                    delegate: Rectangle {
                        id: fieldDelegate
                        // Capture the outer Repeater's field dict explicitly.
                        // Nested Repeaters also expose modelData, so every
                        // inner scope must use these field* properties.
                        required property var modelData
                        readonly property string fieldKey: modelData.key || ""
                        readonly property string fieldDesc: modelData.desc || ""
                        readonly property string fieldValue: modelData.value || ""
                        readonly property var fieldOptions: modelData.options || []
                        readonly property bool fieldMulti: modelData.multi || false
                        readonly property bool fieldAllowEmpty: modelData.allowEmpty || false
                        Layout.fillWidth: true
                        Layout.preferredHeight: implicitHeight
                        Layout.minimumHeight: 72
                        implicitHeight: Math.max(72, fieldContent.implicitHeight + Theme.space.xxxl)
                        color: Theme.colors.bgRaised
                        border.width: 1
                        border.color: Theme.colors.borderHairline
                        radius: Theme.radius.md

                        function fieldSpec() {
                            return {
                                key: fieldKey,
                                desc: fieldDesc,
                                value: fieldValue,
                                options: fieldOptions,
                                multi: fieldMulti,
                                allowEmpty: fieldAllowEmpty
                            }
                        }

                        RowLayout {
                            id: fieldContent
                            anchors.fill: parent
                            anchors.margins: Theme.space.lg
                            spacing: Theme.space.xxxl

                            // Field info (left)
                            ColumnLayout {
                                Layout.fillWidth: true
                                Layout.alignment: Qt.AlignTop
                                spacing: Theme.space.xs

                                Label {
                                    text: fieldDelegate.fieldKey
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.textPrimary
                                }

                                Label {
                                    text: fieldDelegate.fieldDesc
                                    font.pixelSize: Theme.fontSize.label
                                    color: Theme.colors.textMuted
                                    wrapMode: Text.WordWrap
                                    Layout.fillWidth: true
                                }
                            }

                            // Control (right)
                            RowLayout {
                                Layout.alignment: Qt.AlignTop
                                Layout.preferredWidth: 240
                                spacing: Theme.space.sm

                                // Saving indicator
                                Row {
                                    visible: root.saving[fieldDelegate.fieldKey] || false
                                    spacing: Theme.space.xs

                                    Rectangle {
                                        width: 12
                                        height: 12
                                        radius: 6
                                        color: "transparent"
                                        border.width: 2
                                        border.color: Theme.colors.workingBg
                                        anchors.verticalCenter: parent.verticalCenter

                                        Rectangle {
                                            anchors.centerIn: parent
                                            width: 8
                                            height: 8
                                            radius: 4
                                            color: Theme.colors.working

                                            SequentialAnimation on opacity {
                                                running: Theme.continuousMotion
                                                loops: Animation.Infinite
                                                NumberAnimation { from: 1.0; to: 0.3; duration: 600 }
                                                NumberAnimation { from: 0.3; to: 1.0; duration: 600 }
                                            }
                                        }
                                    }

                                    Label {
                                        text: "saving…"
                                        font.pixelSize: Theme.fontSize.label
                                        color: Theme.colors.working
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                }

                                // TOGGLE (boolean)
                                AppSwitch {
                                    id: boolToggle
                                    visible: !!(!root.saving[fieldDelegate.fieldKey] && isBool(fieldDelegate.fieldOptions))
                                    objectName: "configBoolToggle_" + fieldDelegate.fieldKey
                                    Layout.preferredWidth: 44
                                    Layout.preferredHeight: 24
                                    checked: root.values[fieldDelegate.fieldKey] === "true"
                                    accessibleName: fieldDelegate.fieldKey + " setting"
                                    toolTipText: (checked ? "Turn off " : "Turn on ") + fieldDelegate.fieldKey
                                    onClicked: commitConfig(fieldDelegate.fieldKey, checked ? "true" : "false")
                                }

                                // CHIPS (multi-select)
                                Flow {
                                    visible: !!(!root.saving[fieldDelegate.fieldKey] && fieldDelegate.fieldMulti && fieldDelegate.fieldOptions)
                                    Layout.fillWidth: true
                                    spacing: Theme.space.xs

                                    Repeater {
                                        model: fieldDelegate.fieldOptions || []
                                        delegate: AppButton {
                                            id: multiChip
                                            required property string modelData
                                            readonly property string optionValue: modelData
                                            objectName: "configMultiChip_" + fieldDelegate.fieldKey + "_" + optionValue
                                            text: optionValue
                                            selected: multiHas(fieldDelegate.fieldKey, optionValue)
                                            compact: true
                                            pill: true
                                            height: implicitHeight
                                            toolTipText: (multiHas(fieldDelegate.fieldKey, optionValue) ? "Remove " : "Add ") + optionValue
                                            onClicked: toggleMulti(fieldDelegate.fieldKey, optionValue)
                                        }
                                    }
                                }

                                // SELECT (single-select from options)
                                AppComboBox {
                                    visible: !!(!root.saving[fieldDelegate.fieldKey] && !fieldDelegate.fieldMulti && fieldDelegate.fieldOptions && fieldDelegate.fieldOptions.length > 0 && !isBool(fieldDelegate.fieldOptions))
                                    objectName: "configSelect_" + fieldDelegate.fieldKey
                                    qaPopupOpen: root.qaOpenCombo === ("configSelect_" + fieldDelegate.fieldKey)
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: 32
                                    model: buildSelectOptions(fieldDelegate.fieldSpec())
                                    currentIndex: findSelectIndex(fieldDelegate.fieldSpec())
                                    activationUpdatesCurrentIndex: false
                                    accessibleName: fieldDelegate.fieldKey
                                    toolTipText: "Change " + fieldDelegate.fieldKey
                                    onActivated: function(index) {
                                        if (index >= 0) {
                                            var opts = buildSelectOptions(fieldDelegate.fieldSpec())
                                            commitConfig(fieldDelegate.fieldKey, opts[index])
                                        }
                                    }
                                }

                                // TEXT INPUT (free-form)
                                AppTextField {
                                    visible: !root.saving[fieldDelegate.fieldKey] && (!fieldDelegate.fieldOptions || fieldDelegate.fieldOptions.length === 0)
                                    objectName: "configText_" + fieldDelegate.fieldKey
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: 32
                                    text: root.values[fieldDelegate.fieldKey] || ""
                                    placeholderText: "(unset)"
                                    backgroundColor: Theme.colors.bgRaised2
                                    borderColor: Theme.colors.borderSubtle
                                    focusBorderColor: Theme.colors.borderFocus
                                    onEditingFinished: {
                                        if (text !== fieldDelegate.fieldValue) {
                                            commitConfig(fieldDelegate.fieldKey, text)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }

                Rectangle {
                    id: configEmptyState
                    objectName: "configEmptyState"
                    visible: root.configModel !== null
                        && !root.hasInitialLoadError()
                        && root.configFieldCount === 0
                        && root.ruleChainCount === 0
                    Layout.fillWidth: true
                    Layout.preferredHeight: visible ? 260 : 0
                    color: "transparent"
                    clip: true

                    ColumnLayout {
                        anchors.centerIn: parent
                        spacing: Theme.space.lg

                        Label {
                            text: "⚙"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: 44
                            color: Theme.colors.textFaint
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            text: "No config fields loaded"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.h3
                            font.weight: Theme.fontWeight.semibold
                            color: Theme.colors.textSecondary
                            Layout.alignment: Qt.AlignHCenter
                        }

                        Label {
                            text: "Refresh after the daemon finishes starting."
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.bodySm
                            color: Theme.colors.textMuted
                            horizontalAlignment: Text.AlignHCenter
                            Layout.alignment: Qt.AlignHCenter
                        }

                        AppButton {
                            objectName: "configEmptyRefreshButton"
                            text: "Refresh"
                            variant: "primary"
                            toolTipText: "Reload config"
                            Layout.alignment: Qt.AlignHCenter
                            onClicked: root.retryLoad()
                        }
                    }
                }

                Item { height: Theme.space.xxxl }
            }
        }
    }

    // Initialize values from model on startup
    Component.onCompleted: {
        syncModelCounts()
        currentTabs = availableTabs()
        currentTabFields = tabFields()
        if (root.configModel) {
            syncValuesFromModel()
        }
        syncActiveModels()
    }

    Component.onDestruction: syncActiveModels(false)

    Connections {
        target: root.configModel
        function onModelReset() {
            root.syncModelCounts()
            syncValuesFromModel()
            root.currentTabs = root.availableTabs()
            root.currentTabFields = root.tabFields()
        }
    }

    Connections {
        target: root.configModel
        function onSet_config_done(key, stored_value, success, error_msg) {
            var newSaving = root.cloneMap(root.saving)
            delete newSaving[key]
            root.saving = newSaving

            if (success) {
                root.actionError = ""
                var newValues = root.cloneMap(root.values)
                newValues[key] = stored_value
                root.values = newValues
            } else {
                var reverted = root.cloneMap(root.values)
                reverted[key] = root.currentModelValue(key)
                root.values = reverted
                root.actionError = "Could not save " + key + ": " + (error_msg || "unknown error")
            }
        }
    }

    Connections {
        target: root.ruleChainsModel
        function onModelReset() {
            root.syncModelCounts()
        }
        function onSet_rule_chain_done(role_name, stored_chain, success, error_msg) {
            root.setRuleChainSaving(role_name, false)
            if (!success) {
                root.actionError = "Could not save " + role_name + " chain: " + (error_msg || "unknown error")
            } else {
                root.actionError = ""
            }
        }
    }

    // Helper functions
    function cloneMap(map) {
        var next = {}
        var source = map || {}
        for (var key in source) next[key] = source[key]
        return next
    }

    function syncActiveModels(activeOverride) {
        var active = activeOverride === undefined ? root.visible : activeOverride
        if (root.configModel && typeof root.configModel.set_active === "function") {
            root.configModel.set_active(active)
        }
        if (root.ruleChainsModel && typeof root.ruleChainsModel.set_active === "function") {
            root.ruleChainsModel.set_active(active)
        }
    }

    function syncModelCounts() {
        root.configFieldCount = root.configModel ? root.configModel.rowCount() : 0
        root.ruleChainCount = root.ruleChainsModel ? root.ruleChainsModel.rowCount() : 0
    }

    function configLoadError() {
        if (root.configModel && root.configModel.load_error) {
            return String(root.configModel.load_error)
        }
        if (root.ruleChainsModel && root.ruleChainsModel.load_error) {
            return String(root.ruleChainsModel.load_error)
        }
        return ""
    }

    function hasInitialLoadError() {
        return root.configLoadError() !== "" && root.configRowCount() === 0 && root.ruleChainRowCount() === 0
    }

    function hasRefreshLoadError() {
        return root.configLoadError() !== "" && !root.hasInitialLoadError()
    }

    function configRowCount() {
        return root.configFieldCount
    }

    function ruleChainRowCount() {
        return root.ruleChainCount
    }

    function retryLoad() {
        if (root.configModel && root.configModel.refresh) {
            root.configModel.refresh()
        }
        if (root.ruleChainsModel && root.ruleChainsModel.refresh) {
            root.ruleChainsModel.refresh()
        }
    }

    function setRuleChainSaving(roleName, active) {
        var next = root.cloneMap(root.ruleChainSaving)
        if (active) {
            next[roleName] = true
        } else {
            delete next[roleName]
        }
        root.ruleChainSaving = next
    }

    function isRuleChainSaving(roleName) {
        return !!root.ruleChainSaving[roleName]
    }

    function syncValuesFromModel() {
        if (!root.configModel) return
        var vals = {}
        for (var i = 0; i < root.configModel.rowCount(); i++) {
            var idx = root.configModel.index(i, 0)
            var key = root.configModel.data(idx, 257)  // KeyRole
            var value = root.configModel.data(idx, 259) || ""  // ValueRole
            vals[key] = value
        }
        root.values = vals
    }

    function currentModelValue(key) {
        if (!root.configModel) return ""
        for (var i = 0; i < root.configModel.rowCount(); i++) {
            var idx = root.configModel.index(i, 0)
            if (root.configModel.data(idx, 257) === key) {
                return root.configModel.data(idx, 259) || ""
            }
        }
        return ""
    }

    function availableTabs() {
        if (!root.configModel) return []
        var present = new Set()
        for (var i = 0; i < root.configModel.rowCount(); i++) {
            var idx = root.configModel.index(i, 0)
            var key = root.configModel.data(idx, 257)
            var tab = root.keyTab[key] || "Advanced"
            present.add(tab)
        }
        var tabs = []
        for (var j = 0; j < root.tabOrder.length; j++) {
            if (present.has(root.tabOrder[j])) {
                tabs.push(root.tabOrder[j])
            }
        }
        return tabs
    }

    function tabFields() {
        if (!root.configModel) return []
        var fields = []
        for (var i = 0; i < root.configModel.rowCount(); i++) {
            var idx = root.configModel.index(i, 0)
            var key = root.configModel.data(idx, 257)
            var tab = root.keyTab[key] || "Advanced"
            if (tab === root.activeTab) {
                fields.push({
                    key: key,
                    desc: root.configModel.data(idx, 258) || "",
                    value: root.configModel.data(idx, 259) || "",
                    options: root.configModel.data(idx, 260),  // array or null
                    multi: root.configModel.data(idx, 261) || false,
                    allowEmpty: root.configModel.data(idx, 262) || false
                })
            }
        }
        return fields
    }

    function isBool(options) {
        if (!options || options.length !== 2) return false
        return options.indexOf("true") >= 0 && options.indexOf("false") >= 0
    }

    function commitConfig(key, value) {
        if (root.saving[key]) {
            return
        }
        root.actionError = ""
        var newSaving = root.cloneMap(root.saving)
        newSaving[key] = true
        root.saving = newSaving

        var newValues = root.cloneMap(root.values)
        newValues[key] = value
        root.values = newValues

        root.configModel.set_config(key, value)
    }

    function multiHas(key, opt) {
        return multiParts(root.values[key]).indexOf(opt) >= 0
    }

    function toggleMulti(key, opt) {
        var parts = multiParts(root.values[key])
        var idx = parts.indexOf(opt)
        if (idx >= 0) {
            parts.splice(idx, 1)
        } else {
            parts.push(opt)
        }
        commitConfig(key, parts.join(" "))
    }

    function multiParts(value) {
        return (value || "").split(/[\s,]+/).filter(function(s) { return s !== "" })
    }

    function buildSelectOptions(field) {
        var opts = []
        if (!field || !field.options) return opts  // options is null for free-text fields
        if (field.allowEmpty) {
            opts.push("")
        }
        // Add current value if it's not in options (custom value)
        if (field.value && field.options.indexOf(field.value) < 0 && (!field.allowEmpty || field.value !== "")) {
            opts.push(field.value)
        }
        for (var i = 0; i < field.options.length; i++) {
            opts.push(field.options[i])
        }
        return opts
    }

    function findSelectIndex(field) {
        var opts = buildSelectOptions(field)
        return opts.indexOf(root.values[field.key] || "")
    }

    // Rule chains helpers
    function availableModelsForRole(roleName, chain, models) {
        if (!models) return []
        var inChain = new Set(chain || [])
        var available = []
        for (var i = 0; i < models.length; i++) {
            if (!inChain.has(models[i])) {
                available.push(models[i])
            }
        }
        return available
    }

    function moveChainItem(roleName, index, delta) {
        if (!root.ruleChainsModel) return
        if (root.isRuleChainSaving(roleName)) return
        for (var i = 0; i < root.ruleChainsModel.rowCount(); i++) {
            var idx = root.ruleChainsModel.index(i, 0)
            var rn = root.ruleChainsModel.data(idx, 257)  // RoleNameRole
            if (rn === roleName) {
                var chain = root.ruleChainsModel.data(idx, 259) || []  // ChainRole
                var j = index + delta
                if (j < 0 || j >= chain.length) return
                var newChain = chain.slice()
                var tmp = newChain[index]
                newChain[index] = newChain[j]
                newChain[j] = tmp
                root.setRuleChainSaving(roleName, true)
                root.ruleChainsModel.set_rule_chain(roleName, newChain)
                break
            }
        }
    }

    function removeChainItem(roleName, index) {
        if (!root.ruleChainsModel) return
        if (root.isRuleChainSaving(roleName)) return
        for (var i = 0; i < root.ruleChainsModel.rowCount(); i++) {
            var idx = root.ruleChainsModel.index(i, 0)
            var rn = root.ruleChainsModel.data(idx, 257)
            if (rn === roleName) {
                var chain = root.ruleChainsModel.data(idx, 259) || []
                var newChain = chain.filter(function(_, k) { return k !== index })
                root.setRuleChainSaving(roleName, true)
                root.ruleChainsModel.set_rule_chain(roleName, newChain)
                break
            }
        }
    }

    function addChainItem(roleName, model) {
        if (!root.ruleChainsModel || !model) return
        if (root.isRuleChainSaving(roleName)) return
        for (var i = 0; i < root.ruleChainsModel.rowCount(); i++) {
            var idx = root.ruleChainsModel.index(i, 0)
            var rn = root.ruleChainsModel.data(idx, 257)
            if (rn === roleName) {
                var chain = root.ruleChainsModel.data(idx, 259) || []
                if (chain.indexOf(model) >= 0) {
                    root.actionError = model + " is already in the " + roleName + " chain"
                    return
                }
                var newChain = chain.slice()
                newChain.push(model)
                root.setRuleChainSaving(roleName, true)
                root.ruleChainsModel.set_rule_chain(roleName, newChain)
                break
            }
        }
    }

    function resetChain(roleName) {
        if (!root.ruleChainsModel) return
        if (root.isRuleChainSaving(roleName)) return
        root.setRuleChainSaving(roleName, true)
        root.ruleChainsModel.set_rule_chain(roleName, [])
    }
}
