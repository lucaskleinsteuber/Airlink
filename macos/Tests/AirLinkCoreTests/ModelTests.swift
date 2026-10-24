import Foundation
import Testing
@testable import AirLinkCore

@Test func largeTransferUses64BitCountsAndClampsProgress() throws {
    let data = Data(#"{"id":"t","deviceId":"d","deviceName":"Windows","direction":"receive","state":"running","files":[],"totalBytes":1099511627776,"completedBytes":549755813888,"bytesPerSecond":125000000,"createdAt":"","updatedAt":""}"#.utf8)
    var transfer = try JSONDecoder().decode(Transfer.self, from: data)
    #expect(transfer.fractionCompleted == 0.5)
    #expect(transfer.isActive)
    transfer.completedBytes = transfer.totalBytes + 10
    #expect(transfer.fractionCompleted == 1)
    transfer.state = "completed"
    transfer.totalBytes = 0
    #expect(transfer.fractionCompleted == 1)
    #expect(!transfer.isActive)
}

@Test func settingsRoundTripPreservesUserDestination() throws {
    let value = HostSettings(name: "Mac", receiveDirectory: "/Volumes/External/Received", appearance: "dark", language: "en", notifications: false)
    #expect(try JSONDecoder().decode(HostSettings.self, from: JSONEncoder().encode(value)) == value)
}

@Test func invalidSpeedCannotOverflowFormatting() {
    #expect(TransferFormatting.speed(.infinity) == TransferFormatting.speed(0))
    #expect(TransferFormatting.speed(-20) == TransferFormatting.speed(0))
    #expect(!TransferFormatting.speed(.greatestFiniteMagnitude).isEmpty)
}

@Test func systemLanguageUsesSupportedFallbackAndHonorsExplicitChoice() {
    #expect(!LanguagePreference.usesEnglish("system", preferredLanguages: ["de-DE"]))
    #expect(LanguagePreference.usesEnglish("system", preferredLanguages: ["en-US"]))
    #expect(LanguagePreference.usesEnglish("system", preferredLanguages: ["fr-FR"]))
    #expect(!LanguagePreference.usesEnglish("de", preferredLanguages: ["en-US"]))
    #expect(LanguagePreference.usesEnglish("en", preferredLanguages: ["de-DE"]))
}
