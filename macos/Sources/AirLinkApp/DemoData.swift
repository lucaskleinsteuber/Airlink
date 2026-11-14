import AirLinkCore
import Foundation

/// Sample states for `--demo` and `--snapshot`: neutral names and
/// documentation addresses only, no service and no network.
enum DemoScenario: String, CaseIterable {
    case welcome, empty, home, pairing
}

enum DemoData {
    static func status(_ scenario: DemoScenario, english: Bool) -> HostStatus {
        let now = Date()
        func time(_ secondsAgo: Double) -> String { ISO8601DateFormatter().string(from: now.addingTimeInterval(-secondsAgo)) }
        let linux = #"{"id":"demo-linux","name":"Linux-PC","kind":"engine","address":"https://192.0.2.22:43871","fingerprint":"aa","paired":true,"online":true,"canSend":true,"canReceive":true,"canClipboard":true,"autoAccept":true,"platform":"linux","form":"laptop"}"#
        let windows = #"{"id":"demo-windows","name":"Windows-PC","kind":"engine","address":"https://192.0.2.30:43871","fingerprint":"bb","paired":true,"online":false,"canSend":true,"canReceive":true,"canClipboard":true,"autoAccept":true,"platform":"windows","form":"desktop"}"#
        let nearby = #"{"id":"demo-air","name":"MacBook Air","kind":"engine","address":"https://192.0.2.41:43871","fingerprint":"cc","paired":false,"online":true,"canSend":false,"canReceive":false,"autoAccept":false,"platform":"macos","form":"laptop"}"#
        var devices: [String] = []
        var transfers: [String] = []
        var pairing = ""
        var clipboard = ""
        switch scenario {
        case .welcome, .empty:
            devices = [nearby]
        case .home, .pairing:
            devices = [linux, windows] + (scenario == .home ? [nearby] : [])
            transfers = [
                #"{"id":"t1","deviceId":"demo-linux","deviceName":"Linux-PC","direction":"send","state":"running","files":[{"id":"f1","path":"Urlaub 2026.mov","size":2147483648,"received":0,"completed":false}],"totalBytes":2147483648,"completedBytes":1331439862,"bytesPerSecond":48234496,"createdAt":"\#(time(40))","updatedAt":"\#(time(0))","fileCount":1,"summary":true}"#,
                #"{"id":"t2","deviceId":"demo-linux","deviceName":"Linux-PC","direction":"receive","state":"completed","files":[{"id":"f2","path":"Notizen.pdf","size":482133,"received":482133,"completed":true}],"totalBytes":482133,"completedBytes":482133,"bytesPerSecond":0,"createdAt":"\#(time(300))","updatedAt":"\#(time(240))","fileCount":1,"summary":true,"hasContent":true}"#,
                #"{"id":"t3","deviceId":"demo-windows","deviceName":"Windows-PC","direction":"send","state":"completed","files":[{"id":"f3","path":"Fotos","size":0,"directory":true,"received":0,"completed":true},{"id":"f4","path":"Fotos/IMG_0001.heic","size":3311000,"received":3311000,"completed":true}],"totalBytes":734003200,"completedBytes":734003200,"bytesPerSecond":0,"createdAt":"\#(time(7300))","updatedAt":"\#(time(7200))","fileCount":214,"summary":true}"#
            ]
            if scenario == .home {
                clipboard = #"{"id":"demo-clip","deviceId":"demo-linux","deviceName":"Linux-PC","direction":"receive","text":"","createdAt":"\#(time(20))"}"#
            } else {
                pairing = #"{"id":"demo-pair","peerId":"demo-air","peerName":"MacBook Air","state":"comparing","comparisonCode":"48210937","expiresAt":"\#(time(-240))","localConfirmed":false,"remoteConfirmed":false,"automaticReceive":true,"initiator":false,"platform":"macos","form":"laptop"}"#
            }
        }
        let name = english ? "My Mac" : "Mein Mac"
        let json = """
        {"summary":true,"version":"0.13.0","id":"demo-self","name":"\(name)","urls":["https://192.0.2.10:43871","https://localhost:43871"],"fingerprint":"dd","chunkSize":0,
         "devices":[\(devices.joined(separator: ","))],"transfers":[\(transfers.joined(separator: ","))],
         "pairingSessions":[\(pairing)],"clipboard":[\(clipboard)],
         "settings":{"setupCompleted":\(scenario != .welcome),"name":"\(name)","receiveDirectory":"\(NSHomeDirectory())/Downloads","appearance":"system","language":"system","notifications":true,"automaticUpdateChecks":true,"sendLimitBytesPerSecond":0,"receiveLimitBytesPerSecond":0,"shortcut":{"enabled":false,"keyCode":37,"modifiers":768}}}
        """
        do { return try JSONDecoder().decode(HostStatus.self, from: Data(json.utf8)) }
        catch { fatalError("demo status: \(error)") }
    }
}
