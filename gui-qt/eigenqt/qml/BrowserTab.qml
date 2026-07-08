import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtWebEngine
import "Theme.js" as Theme

Rectangle {
    id: root
    objectName: "browserTab"

    color: Theme.colors.bgRaised

    property string currentUrl: "about:blank"
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
                    enabled: webView.canGoBack
                    toolTipText: "Back"
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 30
                    onClicked: webView.goBack()
                }

                AppButton {
                    objectName: "browserForwardButton"
                    text: ">"
                    compact: true
                    enabled: webView.canGoForward
                    toolTipText: "Forward"
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 30
                    onClicked: webView.goForward()
                }

                AppButton {
                    objectName: "browserReloadButton"
                    text: webView.loading ? "X" : "R"
                    compact: true
                    toolTipText: webView.loading ? "Stop" : "Reload"
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 30
                    onClicked: {
                        if (webView.loading) webView.stop()
                        else webView.reload()
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
                    toolTipText: "Open externally"
                    Layout.preferredWidth: 32
                    Layout.preferredHeight: 30
                    onClicked: Qt.openUrlExternally(root.currentUrl)
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
                width: webView.loading ? parent.width * Math.max(0, Math.min(100, webView.loadProgress)) / 100 : 0
                color: Theme.colors.brand
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.fillHeight: true
            color: Theme.colors.bgWell
            clip: true

            WebEngineView {
                id: webView
                objectName: "browserWebView"
                anchors.fill: parent
                url: root.currentUrl
                backgroundColor: Theme.colors.bgWell

                onUrlChanged: {
                    var next = String(url)
                    if (next !== "" && next !== root.currentUrl) {
                        root.currentUrl = next
                        addressField.text = next
                    }
                }
            }
        }
    }

    function navigate() {
        var next = normalizeUrl(addressField.text)
        if (next === "") return
        root.currentUrl = next
        addressField.text = next
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
