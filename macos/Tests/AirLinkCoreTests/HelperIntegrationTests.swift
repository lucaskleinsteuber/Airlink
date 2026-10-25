import Foundation
import Testing
@testable import AirLinkCore

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
        #expect(status.version == "0.9.0")
        #expect(status.chunkSize == 8 * 1024 * 1024)
        #expect(status.fingerprint.count == 64)
        #expect(!status.urls.isEmpty)
        #expect(status.setupURLs?.isEmpty == false)
        var settings = status.settings
        settings.receiveDirectory = directory.appendingPathComponent("custom-received").path
        settings.appearance = "dark"; settings.language = "en"; settings.notifications = false
        settings.sendLimitBytesPerSecond = 4 * 1024 * 1024
        settings.receiveLimitBytesPerSecond = 8 * 1024 * 1024
        settings.shortcut = Shortcut(enabled: false)
        settings.organization = Organization(detectDuplicates: true, sortEnabled: true, rules: [SortRule(id: "test-images", extensions: ["png"], folder: "Images")])
        let response = try await client.request("settings.update", params: JSONEncoder().encode(settings))
        #expect(try JSONDecoder().decode(HostSettings.self, from: response) == settings)
        let updated = try JSONDecoder().decode(HostStatus.self, from: await client.request("status"))
        #expect(updated.settings == settings)
        #expect(updated.statistics != nil)
        #expect(updated.relays?.isEmpty == true)
        let history = try JSONDecoder().decode(History.self, from: await client.request("history.list", params: JSONEncoder().encode(HistoryFilter())))
        #expect(history.entries.isEmpty)
        let original = directory.appendingPathComponent("original.txt")
        let contents = Data("Native share integration".utf8)
        try contents.write(to: original)
        let request = ShareCreate(name: "Native integration", paths: [original.path], expiresAt: ShareDate.string(Date().addingTimeInterval(86400)), maxDownloads: 2)
        let share = try JSONDecoder().decode(Share.self, from: await client.request("share.create", params: JSONEncoder().encode(request)))
        #expect(share.totalBytes == Int64(contents.count))
        #expect(share.maxDownloads == 2)
        #expect(URL(string: share.url ?? "")?.fragment?.isEmpty == false)
        #expect(try Data(contentsOf: original) == contents)
        _ = try await client.request("share.revoke", params: JSONEncoder().encode(["id": share.id]))
        let revoked = try JSONDecoder().decode(HostStatus.self, from: await client.request("status"))
        #expect(revoked.shares?.first(where: { $0.id == share.id })?.revoked == true)
        _ = try await client.request("history.clear")
        #expect(try Data(contentsOf: original) == contents)
        let cleared = try JSONDecoder().decode(HostStatus.self, from: await client.request("status"))
        #expect(cleared.shares?.contains(where: { $0.id == share.id }) == true)
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
        #expect(first.version == "0.9.0" && second.version == "0.9.0")
        let address = try #require(second.urls.last(where: { $0.contains("localhost:") }))
        struct Requested: Decodable { var requestId: String }
        let requested = try JSONDecoder().decode(Requested.self, from: await source.request("peer.connectTrusted", params: JSONEncoder().encode(["url": address, "fingerprint": second.fingerprint])))
        _ = try await receiver.request("pair.approveTrusted", params: JSONEncoder().encode(["id": requested.requestId]))
        let deadline = Date().addingTimeInterval(15)
        var paired = false
        var pairedPeer: Device?
        while Date() < deadline {
            let current = try JSONDecoder().decode(HostStatus.self, from: await source.request("status"))
            if let peer = current.devices.first(where: { $0.id == second.id && $0.paired }) {
                #expect(peer.autoAccept)
                #expect(!peer.canClipboard && !peer.canRelay)
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
        _ = await source.stop(); _ = await receiver.stop()
    } catch {
        _ = await source.stop(); _ = await receiver.stop()
        throw error
    }
}
