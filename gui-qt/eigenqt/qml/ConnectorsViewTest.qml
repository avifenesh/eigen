import QtQuick
import QtQuick.Controls
import QtQuick.Window

Window {
    id: root
    visible: true
    width: 1280
    height: 900
    title: "ConnectorsView Test"

    ConnectorsView {
        anchors.fill: parent
        connectorsModel: connectorsModel
    }
}
