import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtWebEngine
import "Theme.js" as Theme

Rectangle {
    id: root
    objectName: "browserTab"

    color: Theme.colors.bgRaised

    property string currentUrl: ""
    property bool hasPage: false
    readonly property var webView: browserLoader.item
    readonly property bool qaBrowserLoaded: browserLoader.item !== null
    readonly property bool qaEmptyStateVisible: browserEmptyState.visible
    readonly property string qaUrlText: addressField.text

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 44
            color: Theme.colors.bgRaised
            border.width: 1
            border.color: Theme.colors.borderHairline

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: Theme.space.md
                anchors.rightMargin: Theme.space.md
                spacing: Theme.space.xs

                AppButton {
                    objectName: "browserBackButton"
                    text: "<"
                    compact: true
                    enabled: root.webView && root.webView.canGoBack
                    toolTipText: "Back"
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 30
                    onClicked: if (root.webView) root.webView.goBack()
                }

                AppButton {
                    objectName: "browserForwardButton"
                    text: ">"
                    compact: true
                    enabled: root.webView && root.webView.canGoForward
                    toolTipText: "Forward"
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 30
                    onClicked: if (root.webView) root.webView.goForward()
                }

                AppButton {
                    objectName: "browserReloadButton"
                    text: root.webView && root.webView.loading ? "X" : "R"
                    compact: true
                    enabled: root.hasPage
                    toolTipText: root.webView && root.webView.loading ? "Stop" : "Reload"
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 30
                    onClicked: {
                        if (root.webView) {
                            if (root.webView.loading) root.webView.stop()
                            else root.webView.reload()
                        }
                    }
                }

                TextField {
                    id: addressField
                    objectName: "browserAddressField"
                    Layout.fillWidth: true
                    Layout.preferredHeight: 30
                    text: root.currentUrl
                    selectByMouse: true
                    font.family: Theme.monoFonts[0]
                    font.pixelSize: Theme.fontSize.codeSm
                    color: Theme.colors.textPrimary
                    placeholderText: "Enter a URL"
                    placeholderTextColor: Theme.colors.textGhost
                    Accessible.name: "Browser URL"
                    readonly property bool qaTextFits: !addressField.contentItem || !addressField.contentItem.text
                        || (addressField.contentItem.paintedWidth <= Math.max(0, width - leftPadding - rightPadding) + 0.5)

                    background: Rectangle {
                        color: Theme.colors.bgInset
                        radius: Theme.radius.sm
                        border.width: 1
                        border.color: addressField.activeFocus ? Theme.colors.borderBrand : Theme.colors.borderHairline
                    }

                    Keys.onReturnPressed: function(event) {
                        root.navigate()
                        event.accepted = true
                    }
                    Keys.onEnterPressed: function(event) {
                        root.navigate()
                        event.accepted = true
                    }
                }

                AppButton {
                    objectName: "browserGoButton"
                    text: "Go"
                    compact: true
                    variant: "secondary"
                    toolTipText: "Open URL"
                    Layout.preferredHeight: 30
                    onClicked: root.navigate()
                }

                AppButton {
                    objectName: "browserOpenExternalButton"
                    text: "^"
                    compact: true
                    enabled: root.hasPage
                    toolTipText: "Open externally"
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 30
                    onClicked: if (root.hasPage) Qt.openUrlExternally(root.currentUrl)
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 2
            color: Theme.colors.bgInset

            Rectangle {
                objectName: "browserProgressFill"
                anchors.left: parent.left
                anchors.top: parent.top
                anchors.bottom: parent.bottom
                width: root.webView && root.webView.loading
                    ? parent.width * Math.max(0, Math.min(100, root.webView.loadProgress)) / 100
                    : 0
                color: Theme.colors.brand
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.fillHeight: true
            color: Theme.colors.bgWell
            clip: true

            Loader {
                id: browserLoader
                objectName: "browserWebViewLoader"
                anchors.fill: parent
                active: root.hasPage
                onLoaded: {
                    if (item && root.hasPage) {
                        item.url = root.currentUrl
                    }
                }
                sourceComponent: WebEngineView {
                    objectName: "browserWebView"
                    backgroundColor: Theme.colors.bgWell

                    onUrlChanged: {
                        var next = String(url)
                        if (next !== "" && next !== root.currentUrl) {
                            root.setCurrentUrl(next)
                            addressField.text = next
                        }
                    }
                }
            }

            Rectangle {
                id: browserEmptyState
                objectName: "browserEmptyState"
                anchors.fill: parent
                visible: !root.hasPage
                color: Theme.colors.bgWell

                ColumnLayout {
                    anchors.centerIn: parent
                    width: Math.min(parent.width - Theme.space.xxxl * 2, 320)
                    spacing: Theme.space.sm

                    Label {
                        objectName: "browserEmptyTitle"
                        text: "No page open"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        font.weight: Theme.fontWeight.semibold
                        color: Theme.colors.textSecondary
                        horizontalAlignment: Text.AlignHCenter
                        Layout.fillWidth: true
                    }

                    Label {
                        objectName: "browserEmptyHint"
                        text: "Docs, PRs, localhost."
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.bodySm
                        color: Theme.colors.textMuted
                        horizontalAlignment: Text.AlignHCenter
                        wrapMode: Text.Wrap
                        Layout.fillWidth: true
                    }
                }
            }

            AppTag {
                objectName: "browserLoadingBadge"
                visible: root.webView && root.webView.loading
                anchors.top: parent.top
                anchors.right: parent.right
                anchors.topMargin: Theme.space.md
                anchors.rightMargin: Theme.space.md
                text: "Loading"
                backgroundColor: Theme.colors.bgOverlay
                borderColor: Theme.colors.borderHairline
                textColor: Theme.colors.textMuted
                minimumHeight: 24
                pill: false
            }
        }
    }

    onCurrentUrlChanged: {
        if (addressField.text !== root.currentUrl) {
            addressField.text = root.currentUrl
        }
        var nextHasPage = root.isPageUrl(root.currentUrl)
        if (root.hasPage !== nextHasPage) {
            root.hasPage = nextHasPage
        }
        if (root.webView && String(root.webView.url) !== root.currentUrl) {
            root.webView.url = root.currentUrl
        }
    }

    function navigate() {
        var next = normalizeUrl(addressField.text)
        if (next === "") return
        addressField.text = next
        if (next === root.currentUrl && root.webView) {
            root.webView.reload()
        } else {
            root.setCurrentUrl(next)
        }
    }

    function setCurrentUrl(next) {
        root.currentUrl = next
        root.hasPage = root.isPageUrl(next)
    }

    function isPageUrl(value) {
        return value !== "" && value !== "about:blank"
    }

    function normalizeUrl(value) {
        var text = String(value || "").trim()
        if (text === "") return ""
        if (text === "about:blank") return text
        if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(text)) return text
        if (/^localhost(:[0-9]+)?(\/|$)/i.test(text)) return "http://" + text
        if (/^[0-9]{1,3}(\.[0-9]{1,3}){3}(:[0-9]+)?(\/|$)/.test(text)) return "http://" + text
        return "https://" + text
    }
}
