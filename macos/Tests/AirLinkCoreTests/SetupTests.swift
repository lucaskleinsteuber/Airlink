import Foundation
import Testing
@testable import AirLinkCore

@Test func setupCompletionIsExplicitAndSurvivesSettingsRoundTrip() throws {
    let legacy = Data(#"{"name":"Mac","receiveDirectory":"/tmp/receive","appearance":"system","language":"de","notifications":false}"#.utf8)
    var settings = try JSONDecoder().decode(HostSettings.self, from: legacy)
    #expect(!settings.setupCompleted)
    settings.name = "Mein MacBook"; settings.setupCompleted = true
    let restored = try JSONDecoder().decode(HostSettings.self, from: JSONEncoder().encode(settings))
    #expect(restored.setupCompleted && restored.name == "Mein MacBook")
    #expect(!restored.notifications)
}
@Test func setupSuggestsDownloadsWithoutChangingAnExistingCustomFolder() {
    let downloads = URL(fileURLWithPath: "/Users/example/Downloads", isDirectory: true)
    #expect(SetupDraft.suggestedFolder(saved: "/Users/example/Downloads/AirLink", downloads: downloads) == downloads.path)
    #expect(SetupDraft.suggestedFolder(saved: "/Volumes/External/Incoming", downloads: downloads) == "/Volumes/External/Incoming")
    #expect(SetupDraft.suggestedFolder(saved: "/Users/example/Documents/AirLink", downloads: downloads) == "/Users/example/Documents/AirLink")
    #expect(SetupDraft.suggestedName(saved: "12345678.fritz.box", fallback: "Mein Mac") == "Mein Mac")
    #expect(SetupDraft.suggestedName(saved: "Mein MacBook", fallback: "Mein Mac") == "Mein MacBook")
}
@Test func setupNameAndFolderValidationUseProtocolBounds() {
    #expect(SetupDraft(name: "  Mein MacBook  ", receiveDirectory: "/Users/example/Downloads").normalizedName == "Mein MacBook")
    #expect(!SetupDraft(name: "\n\t", receiveDirectory: "/tmp").validName)
    #expect(!SetupDraft(name: "Mac\nBook", receiveDirectory: "/tmp").validName)
    #expect(!SetupDraft(name: String(repeating: "😀", count: 31), receiveDirectory: "/tmp").validName)
    #expect(!SetupDraft(name: "Mac", receiveDirectory: "relative").validFolder)
}
