import QtQuick
import Quickshell
import Quickshell.Io

ShellRoot {
    id: app

    readonly property string home: Quickshell.env("HOME") || ""
    readonly property string cli: home + "/.local/bin/bifrost"
    property bool online: false
    property var files: []
    property string notice: "Ready. Files stay on your Arch computer."
    property string pairingURL: ""
    property string qrPath: ""

    function prettySize(bytes) {
        if (bytes < 1024) return bytes + " B"
        if (bytes < 1048576) return (bytes / 1024).toFixed(1) + " KiB"
        return (bytes / 1048576).toFixed(1) + " MiB"
    }
    function refreshFiles() {
        if (!listProc.running) listProc.running = true
    }
    function doDelete(id) {
        deleteProc.command = [app.cli, "delete", id]
        if (!deleteProc.running) deleteProc.running = true
    }
    function doDownload(id) {
        downloadProc.command = [app.cli, "download", id]
        if (!downloadProc.running) downloadProc.running = true
    }

    Process {
        id: healthProc
        command: [app.cli, "health"]
        running: true
        onExited: (exitCode, exitStatus) => { if (exitCode !== 0) app.online = false }
        stdout: StdioCollector {
            onStreamFinished: {
                try { app.online = JSON.parse(this.text).status === "ok" }
                catch (e) { app.online = false }
            }
        }
    }
    Process {
        id: listProc
        command: [app.cli, "list"]
        running: true
        stdout: StdioCollector {
            onStreamFinished: {
                try {
                    var items = JSON.parse(this.text)
                    app.files = Array.isArray(items) ? items : []
                } catch (e) { app.files = []; app.notice = "File list unavailable: start BIfrost engine." }
            }
        }
    }
    Process {
        id: uploadProc
        stdout: StdioCollector { onStreamFinished: { if (this.text.trim()) { app.notice = "File uploaded successfully"; app.refreshFiles() } } }
        stderr: SplitParser {
            onRead: data => {
                const line = String(data).trim()
                if (line.startsWith("PROGRESS ")) app.notice = "Transferring: " + line.substring(9) + "%"
                else if (line) app.notice = line
            }
        }
    }
    Process {
        id: downloadProc
        stdout: StdioCollector { onStreamFinished: { if (this.text.trim()) app.notice = "Saved to " + this.text.trim() } }
        stderr: SplitParser {
            onRead: data => {
                const line = String(data).trim()
                if (line.startsWith("PROGRESS ")) app.notice = "Transferring: " + line.substring(9) + "%"
                else if (line) app.notice = line
            }
        }
    }
    Process {
        id: deleteProc
        stdout: StdioCollector { onStreamFinished: { if (this.text.trim()) { app.notice = "File deleted"; app.refreshFiles() } } }
        stderr: StdioCollector { onStreamFinished: if (this.text.trim()) app.notice = this.text.trim() }
    }
    Process {
        id: pairProc
        command: [app.cli, "pair"]
        stdout: StdioCollector {
            onStreamFinished: {
                try {
                    const response = JSON.parse(this.text)
                    app.qrPath = ""
                    app.pairingURL = response.url || ""
                    app.qrPath = response.qr || ""
                    app.notice = "Invitation expires in 3 minutes; scan from iPhone Safari."
                } catch (e) { app.notice = "Could not generate pairing QR" }
            }
        }
        stderr: StdioCollector { onStreamFinished: if (this.text.trim()) app.notice = this.text.trim() }
    }
    Process {
        id: revokeProc
        command: [app.cli, "revoke"]
        stdout: StdioCollector {
            onStreamFinished: { app.notice = "All iPhone sessions revoked"; app.qrPath = ""; app.pairingURL = "" }
        }
    }
    Timer {
        interval: 4000
        running: true
        repeat: true
        onTriggered: {
            if (!healthProc.running) healthProc.running = true
            app.refreshFiles()
        }
    }

    Process {
        id: chooseProc
        command: ["zenity", "--file-selection", "--title=BIfrost: choose a file"]
        stdout: StdioCollector {
            onStreamFinished: {
                const file = this.text.trim()
                if (!file) return
                if (uploadProc.running) { app.notice = "Wait for the current upload"; return }
                app.notice = "Uploading selected file…"
                uploadProc.command = [app.cli, "upload", file]
                uploadProc.running = true
            }
        }
    }

    FloatingWindow {
        id: win
        title: "BIfrost · Native File Sharing"
        implicitWidth: 840
        implicitHeight: 690
        color: "#0b111b"
        visible: true

        Rectangle {
            anchors.fill: parent
            color: "#0b111b"

            Column {
                anchors.fill: parent
                anchors.margins: 25
                spacing: 16

                Row {
                    spacing: 14
                    Rectangle { width: 48; height: 48; radius: 13; color: "#183245"
                        Text { anchors.centerIn: parent; text: "❄"; color: "#93dbff"; font.pixelSize: 27 }
                    }
                    Column {
                        spacing: 3
                        Text { text: "BIFROST"; color: "#eef6ff"; font.pixelSize: 22; font.bold: true }
                        Text { text: "NATIVE FILE SHARING"; color: "#7f99b1"; font.pixelSize: 10; font.letterSpacing: 2 }
                    }
                }
                Rectangle { width: parent.width; height: 1; color: "#25364a" }

                Row {
                    width: parent.width
                    spacing: 16
                    Column {
                        width: parent.width - 195
                        spacing: 6
                        Text { text: "Share without boundaries."; color: "#edf4fe"; font.pixelSize: 26; font.bold: true }
                        Text { text: "Your Arch computer and iPhone, on the same Wi-Fi."; color: "#90a5ba"; font.pixelSize: 12 }
                    }
                    Rectangle {
                        width: 175; height: 40; radius: 10
                        color: app.online ? "#153a2f" : "#3b252b"
                        Text { anchors.centerIn: parent; text: app.online ? "● Engine online" : "● Engine offline"; color: app.online ? "#8ee6b6" : "#f7a8af"; font.pixelSize: 13 }
                    }
                }

                Row {
                    width: parent.width
                    height: 208
                    spacing: 15
                    Rectangle {
                        width: parent.width * .55 - 8
                        height: parent.height
                        radius: 15
                        color: "#142131"
                        border.color: "#2b4055"
                        Column {
                            anchors.fill: parent
                            anchors.margins: 20
                            spacing: 14
                            Text { text: "Send a file"; color: "#f0f6ff"; font.pixelSize: 19; font.bold: true }
                            Text { text: "Choose a file and store it locally.\nMaximum 512 MiB per file."; color: "#94a9bc"; font.pixelSize: 13; lineHeight: 1.4 }
                            Rectangle {
                                width: parent.width; height: 53; radius: 10
                                color: "#a7e1ff"
                                Text { anchors.centerIn: parent; text: "↥  Choose file"; color: "#092032"; font.pixelSize: 15; font.bold: true }
                                MouseArea { anchors.fill: parent; onClicked: { if (!chooseProc.running) chooseProc.running = true } }
                            }
                        }
                    }
                    Rectangle {
                        width: parent.width * .45 - 8
                        height: parent.height
                        radius: 15
                        color: "#142131"
                        border.color: "#2b4055"
                        Column {
                            anchors.fill: parent
                            anchors.margins: 17
                            spacing: 10
                            Text { text: "Pair iPhone"; color: "#f0f6ff"; font.pixelSize: 19; font.bold: true }
                            Text { text: "One-use invite · 3 minutes"; color: "#94a9bc"; font.pixelSize: 11 }
                            Row {
                                spacing: 10
                                Rectangle { width: 108; height: 110; radius: 7; color: "#fff"; visible: app.qrPath !== ""
                                    Image { anchors.fill: parent; anchors.margins: 6; fillMode: Image.PreserveAspectFit; cache: false; source: app.qrPath ? "file://" + app.qrPath : "" }
                                }
                                Column {
                                    spacing: 10
                                    Rectangle {
                                        width: app.qrPath ? 110 : 170; height: 43; radius: 9; color: "#294966"
                                        Text { anchors.centerIn: parent; text: "Generate QR"; color: "#e5f4ff"; font.pixelSize: 12; font.bold: true }
                                        MouseArea { anchors.fill: parent; onClicked: { if (!pairProc.running) pairProc.running = true } }
                                    }
                                    Rectangle {
                                        width: app.qrPath ? 110 : 170; height: 35; radius: 9; color: "#3a2936"
                                        Text { anchors.centerIn: parent; text: "Revoke devices"; color: "#ffc4d0"; font.pixelSize: 11 }
                                        MouseArea { anchors.fill: parent; onClicked: { if (!revokeProc.running) revokeProc.running = true } }
                                    }
                                    Rectangle {
                                        width: app.qrPath ? 110 : 170; height: 30; radius: 8; color: "#1e364a"
                                        Text { anchors.centerIn: parent; text: "Copy link"; color: "#d3ebf9"; font.pixelSize: 11 }
                                        MouseArea { anchors.fill: parent; onClicked: { if (app.pairingURL) Quickshell.clipboardText = app.pairingURL } }
                                    }
                                }
                            }
                        }
                    }
                }

                Rectangle {
                    width: parent.width; height: 52; radius: 10; color: "#172538"
                    Row {
                        anchors.fill: parent; anchors.margins: 12; spacing: 18
                        Text { anchors.verticalCenter: parent.verticalCenter; text: "Stored files   " + app.files.length; color: "#eaf5ff"; font.pixelSize: 16; font.bold: true }
                        Rectangle { width: 80; height: 28; radius: 8; color: "#284059"; anchors.verticalCenter: parent.verticalCenter
                            Text { anchors.centerIn: parent; text: "Refresh"; color: "#cce6fa"; font.pixelSize: 11 }
                            MouseArea { anchors.fill: parent; onClicked: app.refreshFiles() }
                        }
                        Rectangle { width: 110; height: 28; radius: 8; color: "#284059"; anchors.verticalCenter: parent.verticalCenter
                            Text { anchors.centerIn: parent; text: "Downloads folder"; color: "#cce6fa"; font.pixelSize: 10 }
                            MouseArea { anchors.fill: parent; onClicked: Quickshell.execDetached(["xdg-open", app.home + "/Downloads/BIfrost"]) }
                        }
                    }
                }
                ListView {
                    id: fileList
                    width: parent.width
                    height: 183
                    model: app.files
                    clip: true
                    spacing: 6
                    delegate: Rectangle {
                        required property var modelData
                        width: fileList.width
                        height: 49
                        radius: 9
                        color: "#142131"
                        Row {
                            anchors.fill: parent
                            anchors.margins: 10
                            spacing: 12
                            Column {
                                width: parent.width - 200
                                spacing: 3
                                Text { width: parent.width; elide: Text.ElideRight; text: modelData.name; color: "#f0f7ff"; font.pixelSize: 13 }
                                Text { text: app.prettySize(modelData.size); color: "#8ca8c1"; font.pixelSize: 10 }
                            }
                            Rectangle { width: 82; height: 28; radius: 7; color: "#24506c"; anchors.verticalCenter: parent.verticalCenter
                                Text { anchors.centerIn: parent; text: "Download"; color: "#eaf5ff"; font.pixelSize: 11 }
                                MouseArea { anchors.fill: parent; onClicked: app.doDownload(modelData.id) }
                            }
                            Rectangle { width: 65; height: 28; radius: 7; color: "#4a2936"; anchors.verticalCenter: parent.verticalCenter
                                Text { anchors.centerIn: parent; text: "Delete"; color: "#ffc6d3"; font.pixelSize: 11 }
                                MouseArea { anchors.fill: parent; onClicked: app.doDelete(modelData.id) }
                            }
                        }
                    }
                }
                Rectangle {
                    width: parent.width; height: 51; radius: 9; color: "#182738"
                    Text { anchors.fill: parent; anchors.margins: 13; text: app.notice; elide: Text.ElideRight; color: "#a3cfe8"; font.pixelSize: 12; verticalAlignment: Text.AlignVCenter }
                }
            }
        }
    }
}
