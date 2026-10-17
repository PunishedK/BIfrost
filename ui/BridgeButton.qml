import QtQuick

Rectangle {
        property string caption: ""
        property color base: "#203b50"
        property color ink: "#ecf7ff"
        signal triggered()

        radius: 9
        color: buttonMouse.containsMouse ? Qt.lighter(base, 1.17) : base
        border.color: "#3c5469"
        border.width: 1

        Text {
            anchors.centerIn: parent
            text: parent.caption
            color: parent.ink
            font.pixelSize: 12
            font.bold: true
        }

        MouseArea {
            id: buttonMouse
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: parent.triggered()
        }
    }
