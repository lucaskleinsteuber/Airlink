import Foundation
import Testing
@testable import AirLinkCore

private func expectedHelperVersion() throws -> String {
    if let version = ProcessInfo.processInfo.environment["AIRLINK_TEST_VERSION"] { return version }
    let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    return try String(contentsOf: root.appendingPathComponent("VERSION"), encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)
}

@Test(.enabled(if: ProcessInfo.processInfo.environment["AIRLINK_TEST_HELPER"] != nil, "Set AIRLINK_TEST_HELPER to run the real helper integration test"))
func actualHelperStatusAndSettingsUseNativeContract() async throws {
    let path = try #require(ProcessInfo.processInfo.environment["AIRLINK_TEST_HELPER"])
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent("AirLink-native-test-" + UUID().uuidString, isDirectory: true)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let client = DaemonClient()
    try await client.start(executable: URL(fileURLWithPath: path), dataDirectory: directory.appendingPathComponent("state"), receiveDirectory: directory.appendingPathComponent("received"), disableDiscovery: true)
    do {
        let data = try await client.request("status")
        let status = try JSONDecoder().decode(HostStatus.self, from: data)
        #expect(status.version == (try expectedHelperVersion()))
        #expect(status.chunkSize == 8 * 1024 * 1024)
        #expect(status.fingerprint.count == 64)
        #expect(!status.urls.isEmpty)
        var settings = status.settings
        settings.receiveDirectory = directory.appendingPathComponent("custom-received").path
        settings.appearance = "dark"; settings.language = "en"; settings.notifications = false
        settings.sendLimitBytesPerSecond = 4 * 1024 * 1024
        settings.receiveLimitBytesPerSecond = 8 * 1024 * 1024
        settings.shortcut = Shortcut(enabled: false)
        let response = try await client.request("settings.update", params: JSONEncoder().encode(settings))
        #expect(try JSONDecoder().decode(HostSettings.self, from: response) == settings)
        let updated = try JSONDecoder().decode(HostStatus.self, from: await client.request("status"))
        #expect(updated.settings == settings)
        #expect(updated.statistics != nil)
        let history = try JSONDecoder().decode(History.self, from: await client.request("history.list", params: JSONEncoder().encode(HistoryFilter())))
        #expect(history.entries.isEmpty)
        _ = try await client.request("history.clear")
        for removed in ["share.list", "relay.list", "duplicate.resolve", "connection.qr", "peer.connect", "pair.approve"] {
            await #expect(throws: (any Error).self) { _ = try await client.request(removed) }
        }
        await client.stop()
    } catch {
        await client.stop()
        throw error
    }
}

@Test(.enabled(if: ProcessInfo.processInfo.environment["AIRLINK_TEST_HELPER"] != nil, "Set AIRLINK_TEST_HELPER to run the real helper integration test"))
func actualNativeTrustedPairAndReceivedResultUsePrivateContract() async throws {
    let path = try #require(ProcessInfo.processInfo.environment["AIRLINK_TEST_HELPER"])
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent("AirLink-native-receive-" + UUID().uuidString, isDirectory: true)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let source = DaemonClient(), receiver = DaemonClient()
    do {
        try await source.start(executable: URL(fileURLWithPath: path), dataDirectory: directory.appendingPathComponent("source-state"), receiveDirectory: directory.appendingPathComponent("source-files"), disableDiscovery: true)
        try await receiver.start(executable: URL(fileURLWithPath: path), dataDirectory: directory.appendingPathComponent("receiver-state"), receiveDirectory: directory.appendingPathComponent("receiver-files"), disableDiscovery: true)
        let first = try JSONDecoder().decode(HostStatus.self, from: await source.request("status"))
        let second = try JSONDecoder().decode(HostStatus.self, from: await receiver.request("status"))
        let expectedVersion = try expectedHelperVersion()
        #expect(first.version == expectedVersion && second.version == expectedVersion)
        let address = try #require(second.urls.last(where: { $0.contains("localhost:") }))
        // Comparison-code pairing: both helpers show the same code and both
        // sides confirm, each choosing automatic receipt for its direction.
        struct Confirmation: Encodable { let id: String; let automaticReceive: Bool }
        _ = try await receiver.request("pairing.listen")
        let started = try JSONDecoder().decode(PairingSession.self, from: await source.request("pairing.start", params: JSONEncoder().encode(["url": address, "fingerprint": second.fingerprint])))
        let deadline = Date().addingTimeInterval(15)
        var remoteSession: PairingSession?
        while Date() < deadline, remoteSession == nil {
            let local = try JSONDecoder().decode(PairingSession.self, from: await source.request("pairing.status", params: JSONEncoder().encode(["id": started.id])))
            if local.state == "comparing", let code = local.comparisonCode {
                let summary = try JSONDecoder().decode(HostStatus.self, from: await receiver.request("status.summary"))
                remoteSession = summary.pairingSessions?.first { $0.comparisonCode == code }
            }
            if remoteSession == nil { try await Task.sleep(for: .milliseconds(50)) }
        }
        let responder = try #require(remoteSession)
        _ = try await source.request("pairing.confirm", params: JSONEncoder().encode(Confirmation(id: started.id, automaticReceive: true)))
        _ = try await receiver.request("pairing.confirm", params: JSONEncoder().encode(Confirmation(id: responder.id, automaticReceive: true)))
        var paired = false
        var pairedPeer: Device?
        while Date() < deadline {
            let current = try JSONDecoder().decode(HostStatus.self, from: await source.request("status"))
            if let peer = current.devices.first(where: { $0.id == second.id && $0.paired }) {
                #expect(peer.autoAccept)
                // Your own computers share the clipboard by default; it can be
                // turned off per device.
                #expect(peer.canClipboard)
                paired = true; pairedPeer = peer; break
            }
            try await Task.sleep(for: .milliseconds(50))
        }
        #expect(paired)
        let original = directory.appendingPathComponent("visible-received-file.txt")
        let contents = Data("Native private receive result contract\n".utf8)
        try contents.write(to: original)
        let peer = try #require(pairedPeer)
        let batch = try DirectSendBatch(device: peer, urls: [original])
        let parameters = try JSONEncoder().encode(batch)
        let offered = try JSONDecoder().decode(Transfer.self, from: await source.request("offer", params: parameters))
        let replayed = try JSONDecoder().decode(Transfer.self, from: await source.request("offer", params: parameters))
        #expect(replayed.id == offered.id)
        var completed: Transfer?
        while Date() < deadline {
            let current = try JSONDecoder().decode(HostStatus.self, from: await receiver.request("status"))
            if let owner = current.devices.first(where: { $0.id == first.id }) { #expect(owner.autoAccept) }
            if let received = current.transfers.first(where: { $0.deviceId == first.id && $0.files.contains(where: { $0.id == offered.files.first?.id }) && $0.state == "completed" }) {
                completed = received; break
            }
            try await Task.sleep(for: .milliseconds(50))
        }
        let received = try #require(completed)
        let result = try JSONDecoder().decode(ReceivedTransfer.self, from: await receiver.request("transfer.received", params: JSONEncoder().encode(["id": received.id])))
        let item = try #require(result.singleItem)
        #expect(item.fileId == offered.files.first?.id)
        #expect(!item.directory)
        #expect(URL(fileURLWithPath: item.path).lastPathComponent == original.lastPathComponent)
        #expect(try Data(contentsOf: URL(fileURLWithPath: item.path)) == contents)
        #expect(try Data(contentsOf: original) == contents)
        #expect(received.receivedResultEntries.map(\.id) == [item.fileId])
        let completedReplay = try JSONDecoder().decode(Transfer.self, from: await source.request("offer", params: parameters))
        #expect(completedReplay.id == offered.id)
        let sourceStatus = try JSONDecoder().decode(HostStatus.self, from: await source.request("status"))
        #expect(sourceStatus.transfers.count == 1)
        let completionDeadline = Date().addingTimeInterval(10)
        var sent: Transfer?
        while Date() < completionDeadline {
            let current = try JSONDecoder().decode(HostStatus.self, from: await source.request("status"))
            if let transfer = current.transfers.first(where: { $0.id == offered.id && $0.state == "completed" }) { sent = transfer; break }
            try await Task.sleep(for: .milliseconds(50))
        }
        _ = try #require(sent)
        _ = try await receiver.request("history.remove", params: JSONEncoder().encode(["id": received.id]))
        _ = try await source.request("history.remove", params: JSONEncoder().encode(["id": offered.id]))
        let removed = try JSONDecoder().decode(HostStatus.self, from: await receiver.request("status"))
        #expect(!removed.transfers.contains(where: { $0.id == received.id }))
        #expect(try Data(contentsOf: original) == contents)
        #expect(try Data(contentsOf: URL(fileURLWithPath: item.path)) == contents)
        _ = await receiver.stop()
        try await receiver.start(executable: URL(fileURLWithPath: path), dataDirectory: directory.appendingPathComponent("receiver-state"), receiveDirectory: directory.appendingPathComponent("receiver-files"), disableDiscovery: true)
        let restarted = try JSONDecoder().decode(HostStatus.self, from: await receiver.request("status"))
        #expect(!restarted.transfers.contains(where: { $0.id == received.id }))
        #expect(try Data(contentsOf: URL(fileURLWithPath: item.path)) == contents)
        _ = await source.stop(); _ = await receiver.stop()
    } catch {
        _ = await source.stop(); _ = await receiver.stop()
        throw error
    }
}
