import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "Theme.js" as Theme

// MarkdownBlocks — Column of block components (para, code, heading, list, table, quote, hr)
Column {
    id: root
    spacing: Theme.space.md

    property var blocks: []  // list of block dicts from TranscriptModel.BlocksRole

    Repeater {
        model: root.blocks
        delegate: Loader {
            width: root.width
            sourceComponent: {
                const block = modelData
                switch (block.type) {
                    case "para": return paraBlock
                    case "code": return codeBlock
                    case "heading": return headingBlock
                    case "list": return listBlock
                    case "table": return tableBlock
                    case "quote": return quoteBlock
                    case "hr": return hrBlock
                    default: return null
                }
            }
            property var blockData: modelData
        }
    }

    // Paragraph
    Component {
        id: paraBlock
        Label {
            width: parent.width
            text: blockData.content
            textFormat: Text.RichText
            font.family: Theme.uiFonts[0]
            font.pixelSize: Theme.fontSize.body
            color: Theme.colors.textPrimary
            wrapMode: Text.Wrap
            lineHeight: 1.55  // --lh-prose
        }
    }

    // Code block (monospace, raised surface, lang badge, copy button, horizontal scroll)
    Component {
        id: codeBlock
        Rectangle {
            width: parent.width
            implicitHeight: codeColumn.height
            color: "#0a1012"  // --syn-bg
            radius: Theme.radius.sm
            border.width: 1
            border.color: Theme.colors.borderSubtle

            ColumnLayout {
                id: codeColumn
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.top: parent.top
                spacing: 0

                // Header: lang badge + copy button
                Rectangle {
                    Layout.fillWidth: true
                    height: 32
                    color: "transparent"

                    RowLayout {
                        anchors.fill: parent
                        anchors.leftMargin: Theme.space.md
                        anchors.rightMargin: Theme.space.md
                        spacing: Theme.space.md

                        Label {
                            text: blockData.lang || "text"
                            font.family: Theme.monoFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            font.weight: Theme.fontWeight.medium
                            color: Theme.colors.textMuted
                            Layout.fillWidth: true
                        }

                        Button {
                            text: "Copy"
                            font.family: Theme.uiFonts[0]
                            font.pixelSize: Theme.fontSize.micro
                            font.weight: Theme.fontWeight.medium
                            flat: true
                            Layout.preferredWidth: 60
                            Layout.preferredHeight: 24

                            background: Rectangle {
                                color: parent.hovered ? Theme.colors.stateHover : "transparent"
                                border.width: 1
                                border.color: Theme.colors.borderSubtle
                                radius: Theme.radius.xs
                            }

                            contentItem: Text {
                                text: parent.text
                                font: parent.font
                                color: Theme.colors.textSecondary
                                horizontalAlignment: Text.AlignHCenter
                                verticalAlignment: Text.AlignVCenter
                            }

                            onClicked: {
                                clipboardHelper.copyText(blockData.source)
                            }
                        }
                    }
                }

                // Divider
                Rectangle {
                    Layout.fillWidth: true
                    height: 1
                    color: Theme.colors.borderSubtle
                }

                // Code content (scrollable horizontally)
                ScrollView {
                    Layout.fillWidth: true
                    Layout.preferredHeight: Math.min(codeText.contentHeight + Theme.space.md * 2, 600)
                    ScrollBar.vertical.policy: ScrollBar.AsNeeded
                    ScrollBar.horizontal.policy: ScrollBar.AsNeeded
                    clip: true

                    Label {
                        id: codeText
                        text: highlightedCode(blockData.lang, blockData.source)
                        textFormat: Text.RichText
                        font.family: Theme.monoFonts[0]
                        font.pixelSize: Theme.fontSize.code
                        color: "#c7d2d0"  // --syn-text
                        wrapMode: Text.NoWrap
                        lineHeight: 1.5  // --lh-code
                        padding: Theme.space.md
                    }
                }
            }
        }
    }

    // Heading
    Component {
        id: headingBlock
        Label {
            width: parent.width
            text: blockData.content
            textFormat: Text.RichText
            font.family: Theme.uiFonts[0]
            font.pixelSize: {
                switch (blockData.level) {
                    case 1: return Theme.fontSize.h1
                    case 2: return Theme.fontSize.h2
                    case 3: return Theme.fontSize.h3
                    default: return Theme.fontSize.body
                }
            }
            font.weight: {
                return blockData.level <= 2 ? Theme.fontWeight.semibold : Theme.fontWeight.medium
            }
            color: Theme.colors.textPrimary
            wrapMode: Text.Wrap
        }
    }

    // List (bullet or ordered)
    Component {
        id: listBlock
        Column {
            width: parent.width
            spacing: Theme.space.sm

            Repeater {
                model: blockData.items
                delegate: Row {
                    width: parent.width
                    spacing: Theme.space.sm

                    Label {
                        text: blockData.ordered ? (index + 1) + "." : "•"
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textSecondary
                        width: 24
                        horizontalAlignment: Text.AlignRight
                    }

                    Label {
                        width: parent.width - 24 - Theme.space.sm
                        text: modelData
                        textFormat: Text.RichText
                        font.family: Theme.uiFonts[0]
                        font.pixelSize: Theme.fontSize.body
                        color: Theme.colors.textPrimary
                        wrapMode: Text.Wrap
                    }
                }
            }
        }
    }

    // Table (basic grid)
    Component {
        id: tableBlock
        Rectangle {
            width: parent.width
            implicitHeight: tableColumn.height
            color: "transparent"
            border.width: 1
            border.color: Theme.colors.borderSubtle
            radius: Theme.radius.sm

            Column {
                id: tableColumn
                anchors.fill: parent
                spacing: 0

                Repeater {
                    model: blockData.rows
                    delegate: Rectangle {
                        width: parent.width
                        height: tableRow.height
                        color: index === 0 ? Theme.colors.bgInset : "transparent"

                        Row {
                            id: tableRow
                            anchors.fill: parent
                            spacing: 0

                            Repeater {
                                model: modelData
                                delegate: Rectangle {
                                    width: tableColumn.width / modelData.length
                                    height: cellLabel.contentHeight + Theme.space.md * 2
                                    color: "transparent"
                                    border.width: index === 0 ? 0 : 1
                                    border.color: Theme.colors.borderSubtle

                                    Label {
                                        id: cellLabel
                                        anchors.fill: parent
                                        anchors.margins: Theme.space.md
                                        text: modelData
                                        textFormat: Text.RichText
                                        font.family: Theme.uiFonts[0]
                                        font.pixelSize: Theme.fontSize.bodySm
                                        font.weight: index === 0 && parent.parent.parent.parent.index === 0 ? Theme.fontWeight.semibold : Theme.fontWeight.regular
                                        color: Theme.colors.textPrimary
                                        wrapMode: Text.Wrap
                                        verticalAlignment: Text.AlignVCenter
                                    }
                                }
                            }
                        }

                        Rectangle {
                            anchors.bottom: parent.bottom
                            width: parent.width
                            height: 1
                            color: Theme.colors.borderSubtle
                            visible: index < blockData.rows.length - 1
                        }
                    }
                }
            }
        }
    }

    // Quote
    Component {
        id: quoteBlock
        Rectangle {
            width: parent.width
            implicitHeight: quoteLabel.contentHeight + Theme.space.md * 2
            color: Theme.colors.bgInset
            radius: Theme.radius.sm
            border.width: 0
            border.color: "transparent"

            Rectangle {
                anchors.left: parent.left
                anchors.top: parent.top
                anchors.bottom: parent.bottom
                width: 3
                color: Theme.colors.borderBrand
            }

            Label {
                id: quoteLabel
                anchors.fill: parent
                anchors.leftMargin: Theme.space.lg
                anchors.rightMargin: Theme.space.md
                anchors.topMargin: Theme.space.md
                anchors.bottomMargin: Theme.space.md
                text: blockData.content
                textFormat: Text.RichText
                font.family: Theme.uiFonts[0]
                font.pixelSize: Theme.fontSize.body
                font.italic: true
                color: Theme.colors.textSecondary
                wrapMode: Text.Wrap
            }
        }
    }

    // Horizontal rule
    Component {
        id: hrBlock
        Rectangle {
            width: parent.width
            height: 1
            color: Theme.colors.borderSubtle
        }
    }

    // Helper: syntax highlighting (Python → Qt via context property)
    function highlightedCode(lang, source) {
        // Call Python highlight function via context property
        return highlighter.highlight(lang, source)
    }
}
