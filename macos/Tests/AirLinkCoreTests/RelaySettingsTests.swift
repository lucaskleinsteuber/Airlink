import Foundation
import Testing
@testable import AirLinkCore

@Test func shortcutRequiresIntentionalModifiersAndPreservesDisabledChoice() {
    #expect(Shortcut().isValid)
    #expect(Shortcut(keyCode: 0, modifiers: 4096 | 2048).isValid)
    #expect(!Shortcut(keyCode: 37, modifiers: 0).isValid)
    #expect(!Shortcut(keyCode: 37, modifiers: 512).isValid)
    #expect(!Shortcut(keyCode: 37, modifiers: 256 | 1).isValid)
    #expect(!Shortcut(keyCode: 55, modifiers: 256).isValid)
    #expect(!Shortcut(keyCode: 200, modifiers: 256).isValid)
    #expect(Shortcut(enabled: false, keyCode: 37, modifiers: 768).isValid)
}

@Test func oldSettingsMigrateLimitsAndShortcutWithoutLosingExplicitDisable() throws {
    let base = #"{"name":"Mac","receiveDirectory":"/tmp/received","appearance":"system","language":"system","notifications":false}"#
    let old = try JSONDecoder().decode(HostSettings.self, from: Data(base.utf8))
    #expect(old.shortcut == Shortcut())
    #expect(old.sendLimitBytesPerSecond == 0)
    #expect(old.receiveLimitBytesPerSecond == 0)
    var object = try #require(JSONSerialization.jsonObject(with: Data(base.utf8)) as? [String: Any])
    object["shortcut"] = ["enabled": false, "keyCode": 37, "modifiers": 768]
    let disabled = try JSONDecoder().decode(HostSettings.self, from: JSONSerialization.data(withJSONObject: object))
    #expect(!disabled.shortcut.enabled)
    let wire = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(old)) as? [String: Any])
    #expect((wire["shortcut"] as? [String: Any])?["enabled"] as? Bool == true)
    #expect(wire["sendLimitBytesPerSecond"] as? Int == 0)
}

@Test func rateLimitsUseBinaryUnitsAndRejectInvalidNumbers() {
    #expect(RateLimit.parse("garbage", unit: 1024, enabled: false) == 0)
    #expect(RateLimit.parse("1,5", unit: 1024, enabled: true) == 1536)
    #expect(RateLimit.parse("0.5", unit: 1_048_576, enabled: true) == 524288)
    #expect(RateLimit.parse("0.5", unit: 1024, enabled: true) == nil)
    #expect(RateLimit.parse("nan", unit: 1024, enabled: true) == nil)
    #expect(RateLimit.parse("1073741825", unit: 1024, enabled: true) == nil)
    #expect(RateLimit.isValid(0))
    #expect(RateLimit.isValid(1024))
    #expect(RateLimit.isValid(1 << 40))
    #expect(!RateLimit.isValid(1023))
}

private func relayFixture() throws -> Relay {
    try JSONDecoder().decode(Relay.self, from: Data(#"{"id":"relay","senderId":"sender","senderName":"Phone","receiverId":"receiver","receiverName":"Tablet","state":"running","files":[{"id":"file","path":"Reports/Annual.pdf","size":1099511627776,"received":0,"completed":false}],"totalBytes":1099511627776,"deliveredBytes":549755813888,"bytesPerSecond":125000000,"createdAt":"2026-10-24T12:00:00Z","updatedAt":"2026-10-24T12:01:00Z"}"#.utf8))
}

@Test func relayByteCountsAndTerminalStateDoNotClaimSavedFile() throws {
    var relay = try relayFixture()
    #expect(relay.fractionDelivered == 0.5)
    #expect(relay.isActive)
    relay.deliveredBytes = Int64.max
    #expect(relay.fractionDelivered == 1)
    relay.state = "delivered"; relay.totalBytes = 0
    #expect(relay.fractionDelivered == 1)
    #expect(!relay.isActive)
    #expect(HistoryEntry(relay: relay).isTerminal)
    #expect(HistoryEntry(relay: relay).direction == "relay")
}

@Test func historyFiltersRelayParticipantsNamesPathsAndTerminalScope() throws {
    let entry = HistoryEntry(relay: try relayFixture())
    var filter = HistoryFilter()
    filter.query = "annual.PDF"
    #expect(filter.matches(entry))
    filter.query = "PHONE"; filter.deviceId = "receiver"; filter.direction = "relay"
    #expect(filter.matches(entry))
    filter.deviceId = "other"
    #expect(!filter.matches(entry))
    filter.deviceId = ""; filter.state = "delivered"
    #expect(!filter.matches(entry))
    #expect(!entry.isTerminal)
}

@Test func statisticsPreserveSeparate64BitTransportAndFileCounters() throws {
    let fixture = Data(#"{"sentBytes":1099511627776,"receivedBytes":549755813888,"completedTransfers":3,"failedTransfers":1,"cancelledTransfers":2,"activeTransfers":4,"relayedBytes":2199023255552,"relayDeliveries":6,"trafficSentBytes":4398046511104,"trafficReceivedBytes":8796093022208,"days":[{"date":"2026-10-24","sentBytes":1099511627776,"receivedBytes":0,"relayedBytes":2199023255552,"completedTransfers":3}]}"#.utf8)
    let stats = try JSONDecoder().decode(Statistics.self, from: fixture)
    #expect(stats.relayedBytes == 2_199_023_255_552)
    #expect(stats.sentBytes == 1_099_511_627_776)
    #expect(stats.trafficReceivedBytes == 8_796_093_022_208)
    #expect(stats.days?.first?.completedTransfers == 3)
}

@Test func finderServicePrefersFileURLsAndSafelyFallsBackToLegacyPaths() {
    let a = URL(fileURLWithPath: "/Users/test/Original.txt")
    #expect(ServiceSelection.urls(modern: [a, a], legacyPaths: ["/Users/test/Other.txt"]) == [a])
    #expect(ServiceSelection.urls(modern: [], legacyPaths: ["relative.txt", "/Users/test/Original.txt"]) == [a])
    #expect(ServiceSelection.urls(modern: [URL(string: "https://example.com")!], legacyPaths: [a.path]) == [a])
}
