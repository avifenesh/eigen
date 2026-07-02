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
    property var currentTabFields: []
    onActiveTabChanged: currentTabFields = tabFields()

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

    // Working values (optimistic update before commit)
    property var values: ({})

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
                    model: availableTabs()
                    delegate: Button {
                        text: modelData
                        checkable: false
                        Layout.preferredHeight: 40

                        background: Rectangle {
                            color: root.activeTab === modelData ? Theme.colors.bgBase : "transparent"
                            border.width: root.activeTab === modelData ? 1 : 0
                            border.color: Theme.colors.borderHairline
                            Rectangle {
                                visible: root.activeTab === modelData
                                anchors.bottom: parent.bottom
                                anchors.left: parent.left
                                anchors.right: parent.right
                                height: 2
                                color: Theme.colors.brandBright
                            }
                        }

                        contentItem: Label {
                            text: parent.text
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: 13
                            font.weight: root.activeTab === modelData ? Theme.fontWeight.semibold : Theme.fontWeight.regular
                            color: root.activeTab === modelData ? Theme.colors.textPrimary : Theme.colors.textMuted
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                        }

                        onClicked: root.activeTab = modelData
                    }
                }
            }
        }

        // TAB CONTENT
        Flickable {
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

                // RULE CHAINS EDITOR (Models tab only)
                Rectangle {
                    visible: root.activeTab === "Models" && root.ruleChainsModel
                    Layout.fillWidth: true
                    implicitHeight: ruleChainsContent.implicitHeight + Theme.space.xxxl
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
                                Layout.fillWidth: true
                                implicitHeight: chainRow.implicitHeight + Theme.space.xxxl
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

                                            Rectangle {
                                                visible: model.custom
                                                implicitWidth: customLabel.implicitWidth + Theme.space.md
                                                implicitHeight: 18
                                                radius: Theme.radius.sm
                                                color: Theme.colors.stateSelected
                                                border.width: 1
                                                border.color: Theme.colors.borderBrandFaint

                                                Label {
                                                    id: customLabel
                                                    anchors.centerIn: parent
                                                    text: "custom"
                                                    font.pixelSize: 10
                                                    font.weight: Theme.fontWeight.semibold
                                                    color: Theme.colors.brand
                                                }
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

                                                        Button {
                                                            text: "←"
                                                            enabled: index > 0
                                                            implicitWidth: 28
                                                            implicitHeight: 24
                                                            onClicked: moveChainItem(chainDelegate.model.roleName, index, -1)

                                                            background: Rectangle {
                                                                color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                                                                radius: Theme.radius.sm
                                                                opacity: parent.enabled ? 1.0 : 0.4
                                                            }

                                                            contentItem: Label {
                                                                text: parent.text
                                                                font.pixelSize: 11
                                                                color: Theme.colors.textSecondary
                                                                horizontalAlignment: Text.AlignHCenter
                                                                verticalAlignment: Text.AlignVCenter
                                                            }
                                                        }

                                                        Button {
                                                            text: "→"
                                                            enabled: index < (chainDelegate.model.chain.length - 1)
                                                            implicitWidth: 28
                                                            implicitHeight: 24
                                                            onClicked: moveChainItem(chainDelegate.model.roleName, index, 1)

                                                            background: Rectangle {
                                                                color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                                                                radius: Theme.radius.sm
                                                                opacity: parent.enabled ? 1.0 : 0.4
                                                            }

                                                            contentItem: Label {
                                                                text: parent.text
                                                                font.pixelSize: 11
                                                                color: Theme.colors.textSecondary
                                                                horizontalAlignment: Text.AlignHCenter
                                                                verticalAlignment: Text.AlignVCenter
                                                            }
                                                        }

                                                        Button {
                                                            text: "✕"
                                                            implicitWidth: 28
                                                            implicitHeight: 24
                                                            onClicked: removeChainItem(chainDelegate.model.roleName, index)

                                                            background: Rectangle {
                                                                color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                                                                radius: Theme.radius.sm
                                                            }

                                                            contentItem: Label {
                                                                text: parent.text
                                                                font.pixelSize: 11
                                                                color: Theme.colors.error
                                                                horizontalAlignment: Text.AlignHCenter
                                                                verticalAlignment: Text.AlignVCenter
                                                            }
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

                                            ComboBox {
                                                id: addModelCombo
                                                Layout.fillWidth: true
                                                Layout.preferredHeight: 32
                                                model: availableModelsForRole(chainDelegate.model.roleName, chainDelegate.model.chain, chainDelegate.model.models)
                                                displayText: currentIndex >= 0 ? currentText : "Add model…"

                                                background: Rectangle {
                                                    color: Theme.colors.bgRaised
                                                    border.width: 1
                                                    border.color: addModelCombo.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                                                    radius: Theme.radius.sm
                                                }

                                                contentItem: Label {
                                                    text: parent.displayText
                                                    font.pixelSize: Theme.fontSize.bodySm
                                                    color: Theme.colors.textPrimary
                                                    verticalAlignment: Text.AlignVCenter
                                                    leftPadding: Theme.space.md
                                                }
                                            }

                                            Button {
                                                text: "Add"
                                                enabled: addModelCombo.currentIndex >= 0
                                                implicitHeight: 32
                                                onClicked: {
                                                    if (addModelCombo.currentIndex >= 0) {
                                                        addChainItem(chainDelegate.model.roleName, addModelCombo.currentText)
                                                        addModelCombo.currentIndex = -1
                                                    }
                                                }

                                                background: Rectangle {
                                                    color: parent.down ? Theme.colors.brandStrong : (parent.hovered ? Theme.colors.brand : Theme.colors.brandDim)
                                                    radius: Theme.radius.sm
                                                    opacity: parent.enabled ? 1.0 : 0.5
                                                }

                                                contentItem: Label {
                                                    text: parent.text
                                                    font.pixelSize: Theme.fontSize.bodySm
                                                    font.weight: Theme.fontWeight.medium
                                                    color: Theme.colors.textPrimary
                                                    horizontalAlignment: Text.AlignHCenter
                                                    verticalAlignment: Text.AlignVCenter
                                                }
                                            }

                                            Button {
                                                text: "Reset"
                                                enabled: model.custom
                                                implicitHeight: 32
                                                onClicked: resetChain(chainDelegate.model.roleName)

                                                background: Rectangle {
                                                    color: parent.down ? Theme.colors.stateActive : (parent.hovered ? Theme.colors.stateHover : "transparent")
                                                    border.width: 1
                                                    border.color: Theme.colors.borderSubtle
                                                    radius: Theme.radius.sm
                                                    opacity: parent.enabled ? 1.0 : 0.5
                                                }

                                                contentItem: Label {
                                                    text: parent.text
                                                    font.pixelSize: Theme.fontSize.bodySm
                                                    color: Theme.colors.textSecondary
                                                    horizontalAlignment: Text.AlignHCenter
                                                    verticalAlignment: Text.AlignVCenter
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
                    model: root.currentTabFields
                    delegate: Rectangle {
                        id: fieldDelegate
                        // Capture the field dict once — nested Repeaters
                        // rebind modelData, so inner scopes must reference
                        // fieldDelegate.field, never fieldContent.modelData
                        // (fieldContent is a RowLayout; it has no modelData).
                        property var field: modelData
                        Layout.fillWidth: true
                        implicitHeight: fieldContent.implicitHeight + Theme.space.xxxl
                        color: Theme.colors.bgRaised
                        border.width: 1
                        border.color: Theme.colors.borderHairline
                        radius: Theme.radius.md

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
                                    text: modelData.key
                                    font.family: Theme.monoFonts[0]
                                    font.pixelSize: Theme.fontSize.bodySm
                                    font.weight: Theme.fontWeight.semibold
                                    color: Theme.colors.textPrimary
                                }

                                Label {
                                    text: modelData.desc
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
                                    visible: root.saving[modelData.key] || false
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
                                                running: true
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
                                Button {
                                    visible: !!(!root.saving[modelData.key] && isBool(modelData.options))
                                    checkable: false
                                    Layout.preferredWidth: 44
                                    Layout.preferredHeight: 24

                                    background: Rectangle {
                                        color: (root.values[modelData.key] === "true") ? Theme.colors.brandDim : Theme.colors.bgInset
                                        border.width: 1
                                        border.color: (root.values[modelData.key] === "true") ? Theme.colors.brand : Theme.colors.borderSubtle
                                        radius: Theme.radius.full

                                        Rectangle {
                                            x: (root.values[modelData.key] === "true") ? parent.width - width - 2 : 2
                                            y: 2
                                            width: 18
                                            height: 18
                                            radius: 9
                                            color: (root.values[modelData.key] === "true") ? Theme.colors.brandBright : Theme.colors.textSecondary

                                            Behavior on x {
                                                NumberAnimation { duration: Theme.duration.fast; easing.type: Easing.OutCubic }
                                            }
                                        }
                                    }

                                    onClicked: commitConfig(modelData.key, (root.values[modelData.key] === "true") ? "false" : "true")
                                }

                                // CHIPS (multi-select)
                                Flow {
                                    visible: !!(!root.saving[modelData.key] && modelData.multi && modelData.options)
                                    Layout.fillWidth: true
                                    spacing: Theme.space.xs

                                    Repeater {
                                        model: modelData.options || []
                                        delegate: Button {
                                            text: modelData
                                            checkable: false
                                            implicitHeight: 24

                                            background: Rectangle {
                                                color: multiHas(fieldDelegate.field.key, modelData) ? Theme.colors.stateSelected : Theme.colors.bgRaised2
                                                border.width: 1
                                                border.color: multiHas(fieldDelegate.field.key, modelData) ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                                                radius: Theme.radius.full
                                            }

                                            contentItem: Label {
                                                text: parent.text
                                                font.pixelSize: Theme.fontSize.label
                                                font.weight: Theme.fontWeight.medium
                                                color: multiHas(fieldDelegate.field.key, modelData) ? Theme.colors.brandBright : Theme.colors.textMuted
                                                horizontalAlignment: Text.AlignHCenter
                                                verticalAlignment: Text.AlignVCenter
                                                leftPadding: Theme.space.md
                                                rightPadding: Theme.space.md
                                            }

                                            onClicked: toggleMulti(fieldDelegate.field.key, modelData)
                                        }
                                    }
                                }

                                // SELECT (single-select from options)
                                ComboBox {
                                    visible: !!(!root.saving[modelData.key] && !modelData.multi && modelData.options && modelData.options.length > 0 && !isBool(modelData.options))
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: 32
                                    model: buildSelectOptions(modelData)
                                    currentIndex: findSelectIndex(modelData)
                                    onActivated: function(index) {
                                        if (index >= 0) {
                                            var opts = buildSelectOptions(modelData)
                                            commitConfig(modelData.key, opts[index])
                                        }
                                    }

                                    background: Rectangle {
                                        color: Theme.colors.bgRaised2
                                        border.width: 1
                                        border.color: parent.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                                        radius: Theme.radius.sm
                                    }

                                    contentItem: Label {
                                        text: parent.displayText
                                        font.pixelSize: Theme.fontSize.bodySm
                                        color: Theme.colors.textPrimary
                                        verticalAlignment: Text.AlignVCenter
                                        leftPadding: Theme.space.md
                                        elide: Text.ElideRight
                                    }
                                }

                                // TEXT INPUT (free-form)
                                TextField {
                                    visible: !root.saving[modelData.key] && (!modelData.options || modelData.options.length === 0)
                                    Layout.fillWidth: true
                                    Layout.preferredHeight: 32
                                    text: root.values[modelData.key] || ""
                                    placeholderText: "(unset)"
                                    onEditingFinished: {
                                        if (text !== modelData.value) {
                                            commitConfig(modelData.key, text)
                                        }
                                    }

                                    background: Rectangle {
                                        color: Theme.colors.bgRaised2
                                        border.width: 1
                                        border.color: parent.activeFocus ? Theme.colors.borderBrandFaint : Theme.colors.borderSubtle
                                        radius: Theme.radius.sm
                                    }

                                    font.pixelSize: Theme.fontSize.bodySm
                                    color: Theme.colors.textPrimary
                                    leftPadding: Theme.space.md
                                    rightPadding: Theme.space.md
                                }
                            }
                        }
                    }
                }

                Item { height: Theme.space.xxxl }
            }
        }
    }

    // Initialize values from model on startup
    Component.onCompleted: {
        currentTabFields = tabFields()
        if (root.configModel) {
            syncValuesFromModel()
        }
    }

    Connections {
        target: root.configModel
        function onModelReset() {
            syncValuesFromModel()
            root.currentTabFields = root.tabFields()
        }
    }

    Connections {
        target: root.configModel
        function onSet_config_done(key, stored_value, success, error_msg) {
            var newSaving = root.saving
            delete newSaving[key]
            root.saving = newSaving

            if (success) {
                var newValues = root.values
                newValues[key] = stored_value
                root.values = newValues
                console.log(key + " saved")
            } else {
                console.error("SetConfig failed for " + key + ": " + error_msg)
            }
        }
    }

    Connections {
        target: root.ruleChainsModel
        function onSet_rule_chain_done(role_name, stored_chain, success, error_msg) {
            if (success) {
                console.log(role_name + " chain saved")
            } else {
                console.error("SetRuleChain failed for " + role_name + ": " + error_msg)
            }
        }
    }

    // Helper functions
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
        var newSaving = root.saving
        newSaving[key] = true
        root.saving = newSaving

        var newValues = root.values
        newValues[key] = value
        root.values = newValues

        root.configModel.set_config(key, value)
    }

    function multiHas(key, opt) {
        var val = root.values[key] || ""
        var parts = val.split(/\s+/).filter(function(s) { return s !== "" })
        return parts.indexOf(opt) >= 0
    }

    function toggleMulti(key, opt) {
        var val = root.values[key] || ""
        var parts = val.split(/\s+/).filter(function(s) { return s !== "" })
        var idx = parts.indexOf(opt)
        if (idx >= 0) {
            parts.splice(idx, 1)
        } else {
            parts.push(opt)
        }
        commitConfig(key, parts.join(" "))
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
                root.ruleChainsModel.set_rule_chain(roleName, newChain)
                break
            }
        }
    }

    function removeChainItem(roleName, index) {
        if (!root.ruleChainsModel) return
        for (var i = 0; i < root.ruleChainsModel.rowCount(); i++) {
            var idx = root.ruleChainsModel.index(i, 0)
            var rn = root.ruleChainsModel.data(idx, 257)
            if (rn === roleName) {
                var chain = root.ruleChainsModel.data(idx, 259) || []
                var newChain = chain.filter(function(_, k) { return k !== index })
                root.ruleChainsModel.set_rule_chain(roleName, newChain)
                break
            }
        }
    }

    function addChainItem(roleName, model) {
        if (!root.ruleChainsModel || !model) return
        for (var i = 0; i < root.ruleChainsModel.rowCount(); i++) {
            var idx = root.ruleChainsModel.index(i, 0)
            var rn = root.ruleChainsModel.data(idx, 257)
            if (rn === roleName) {
                var chain = root.ruleChainsModel.data(idx, 259) || []
                if (chain.indexOf(model) >= 0) {
                    console.error(model + " is already in the " + roleName + " chain")
                    return
                }
                var newChain = chain.slice()
                newChain.push(model)
                root.ruleChainsModel.set_rule_chain(roleName, newChain)
                break
            }
        }
    }

    function resetChain(roleName) {
        if (!root.ruleChainsModel) return
        root.ruleChainsModel.set_rule_chain(roleName, [])
    }
}
