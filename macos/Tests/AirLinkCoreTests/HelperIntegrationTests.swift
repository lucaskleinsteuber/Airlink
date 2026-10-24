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
        #expect(status.version == "0.4.0")
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
