import Foundation
import Testing
@testable import AirLinkCore

private func desktopTransfer(id: String, device: String = "arch", state: String = "completed", date: String = "2026-11-10T00:00:00Z") throws -> Transfer {
    let json: [String: Any] = ["id": id, "deviceId": device, "deviceName": "Arch laptop", "state": state, "direction": "receive", "files": [], "totalBytes": 1024, "completedBytes": 512, "bytesPerSecond": 256, "createdAt": date, "updatedAt": date]
    return try JSONDecoder().decode(Transfer.self, from: JSONSerialization.data(withJSONObject: json))
}

@Test func recipientViewKeepsAllActiveWorkAndOnlyThreeRecentResults() throws {
    var items: [Transfer] = []
    for i in 0..<10 { items.append(try desktopTransfer(id: "t\(i)", date: "2026-11-10T00:00:\(String(format: "%02d", i))Z")) }
    items.append(try desktopTransfer(id: "paused", state: "paused"))
    items.append(try desktopTransfer(id: "running", state: "running"))
    items.append(try desktopTransfer(id: "other", device: "other"))
    let visible = DesktopTransferSelection.visible(items, deviceID: "arch")
    #expect(visible.count == 5)
    #expect(Set(visible.filter(\.isActive).map(\.id)) == ["paused", "running"])
    #expect(visible.filter { !$0.isActive }.map(\.id) == ["t9", "t8", "t7"])
    #expect(DesktopTransferSelection.visible(items, deviceID: "missing").isEmpty)
}

@Test func remainingTimeIsOnlyShownForMeasurablePayloadWork() throws {
    var value = try desktopTransfer(id: "t", state: "running")
    #expect(DesktopTransferSelection.remainingSeconds(value) == 2)
    value.state = "verifying"
    #expect(DesktopTransferSelection.remainingSeconds(value) == nil)
    value.state = "running"; value.bytesPerSecond = .infinity
    #expect(DesktopTransferSelection.remainingSeconds(value) == nil)
}

@Test func pairingCodePreservesLeadingZeroAndCannotBeConfirmedTwice() throws {
    let json = #"{"id":"pair","peerId":"arch","peerName":"Arch","state":"comparing","comparisonCode":"00123456","expiresAt":"2026-11-10T00:05:00Z","localConfirmed":false,"remoteConfirmed":false,"automaticReceive":false}"#
    var p = try JSONDecoder().decode(PairingSession.self, from: Data(json.utf8))
    #expect(p.formattedCode == "0012 3456")
    #expect(p.canConfirm && !p.automaticReceive)
    p.localConfirmed = true
    #expect(!p.canConfirm)
    p.localConfirmed = false; p.state = "expired"
    #expect(!p.canConfirm)
}

@Test func updateChecksRequireOptInAndAtLeastOneDayBetweenAttempts() {
    let now = Date(timeIntervalSince1970: 1_000_000)
    #expect(!UpdateCheckSchedule.isDue(enabled: false, lastAttempt: nil, now: now))
    #expect(UpdateCheckSchedule.isDue(enabled: true, lastAttempt: nil, now: now))
    #expect(!UpdateCheckSchedule.isDue(enabled: true, lastAttempt: now.addingTimeInterval(-86_399), now: now))
    #expect(UpdateCheckSchedule.isDue(enabled: true, lastAttempt: now.addingTimeInterval(-86_400), now: now))
    #expect(!UpdateCheckSchedule.isDue(enabled: true, lastAttempt: now.addingTimeInterval(60), now: now))
}

@Test func updatePreferenceMergesOnlyItsDeliberateChange() {
    let old = HostSettings(name: "Mac", receiveDirectory: "/tmp/receive")
    var draft = old; draft.automaticUpdateChecks = true
    var latest = old; latest.name = "Renamed Mac"; latest.organization.detectDuplicates = true
    let merged = SettingsEditing.merge(baseline: old, draft: draft, latest: latest)
    #expect(merged.automaticUpdateChecks)
    #expect(merged.name == "Renamed Mac" && merged.organization.detectDuplicates)
}

@Test func compactSummaryDoesNotInventAReceivedFileFromItsPreview() throws {
    let json = #"{"id":"t","deviceId":"arch","deviceName":"Arch","state":"completed","direction":"receive","files":[{"id":"folder","path":"folder","directory":true,"size":0,"received":0,"completed":true}],"totalBytes":1024,"completedBytes":0,"skippedBytes":1024,"bytesPerSecond":0,"createdAt":"","updatedAt":"","summary":true,"fileCount":10000,"hasContent":false}"#
    let transfer = try JSONDecoder().decode(Transfer.self, from: Data(json.utf8))
    #expect(transfer.numberOfFiles == 10000)
    #expect(!transfer.hasReceivedContent)
}
