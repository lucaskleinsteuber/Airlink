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
        #expect(status.version == "0.1.0")
        #expect(status.chunkSize == 8 * 1024 * 1024)
        #expect(status.fingerprint.count == 64)
        #expect(!status.urls.isEmpty)
        #expect(status.setupURLs?.isEmpty == false)
        var settings = status.settings
        settings.receiveDirectory = directory.appendingPathComponent("custom-received").path
        settings.appearance = "dark"; settings.language = "en"; settings.notifications = false
        let response = try await client.request("settings.update", params: JSONEncoder().encode(settings))
        #expect(try JSONDecoder().decode(HostSettings.self, from: response) == settings)
        let updated = try JSONDecoder().decode(HostStatus.self, from: await client.request("status"))
        #expect(updated.settings == settings)
        await client.stop()
    } catch {
        await client.stop()
        throw error
    }
}
