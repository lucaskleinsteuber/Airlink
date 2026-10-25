import Foundation
import Testing
@testable import AirLinkCore

private func directDevice(_ id: String = "laptop", kind: String = "engine", online: Bool = true, paired: Bool = true, canReceive: Bool = true) throws -> Device {
    let data = try JSONSerialization.data(withJSONObject: ["id": id, "name": "Windows laptop", "kind": kind, "online": online, "paired": paired, "canReceive": canReceive, "canSend": true, "autoAccept": false, "fingerprint": String(repeating: "a", count: 64)])
    return try JSONDecoder().decode(Device.self, from: data)
}

@Test func directDropPinsItsOwnRecipientAndSourcesRatherThanTheEditableDraft() throws {
    var recipient = try directDevice()
    let dropped = URL(fileURLWithPath: "/tmp/actual drop.bin")
    let unrelated = URL(fileURLWithPath: "/tmp/unrelated draft.bin")
    var draft = FileSendSelection(); draft.append([unrelated])
    let batch = try DirectSendBatch(device: recipient, urls: [dropped])
    recipient.id = "another-device"; recipient.name = "Changed target"; recipient.fingerprint = String(repeating: "b", count: 64)
    #expect(batch.deviceID == "laptop")
    #expect(batch.deviceName == "Windows laptop")
    #expect(batch.fingerprint == String(repeating: "a", count: 64))
    #expect(batch.urls == [dropped])
    #expect(draft.urls == [unrelated])
    let payload = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(batch)) as? [String: Any])
    #expect(Set(payload.keys) == ["deviceId", "paths", "requestId"])
    #expect(payload["deviceId"] as? String == "laptop")
    #expect(payload["paths"] as? [String] == [dropped.path])
    let requestID = try #require(payload["requestId"] as? String)
    #expect(requestID.count == 32 && requestID.allSatisfy(\.isHexDigit))
    let retriedPayload = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(batch)) as? [String: Any])
    #expect(retriedPayload["requestId"] as? String == requestID)
    let next = try DirectSendBatch(device: recipient, urls: [dropped])
    #expect(batch.id != next.id)
    #expect(batch.requestID != next.requestID)
}

@Test func directDropDeduplicatesLocalPathsInOrderWithoutResolvingSourcesOrFoldingCase() throws {
    let source = URL(fileURLWithPath: "/tmp/folder/../a.bin")
    let same = URL(fileURLWithPath: "/tmp/a.bin")
    let differentCase = URL(fileURLWithPath: "/tmp/A.bin")
    let folder = URL(fileURLWithPath: "/tmp/empty folder", isDirectory: true)
    let symbolicName = URL(fileURLWithPath: "/tmp/not-an-existing-symbolic-name.bin")
    let batch = try DirectSendBatch(device: directDevice(), urls: [source, same, folder, differentCase, symbolicName])
    #expect(batch.paths == [same.path, folder.path, differentCase.path, symbolicName.path])
    #expect(batch.urls[1].hasDirectoryPath)
}

@Test func directDropRejectsTheWholeInvalidBatchRatherThanSilentlyDiscardingEntries() throws {
    let recipient = try directDevice()
    #expect(throws: DirectSendValidationError.emptyFiles) { try DirectSendBatch(device: recipient, urls: []) }
    for remote in ["https://example.invalid/file.bin", "file://server/share/file.bin", "file:relative", "file:///tmp/invalid%00file", "file:///tmp/good.bin?query=1", "file:///tmp/good.bin#page", "file://user@localhost/tmp/good.bin"] {
        let url = try #require(URL(string: remote))
        #expect(throws: DirectSendValidationError.nonLocalFile) {
            try DirectSendBatch(device: recipient, urls: [URL(fileURLWithPath: "/tmp/good.bin"), url])
        }
    }
}

@Test func directDropRequiresCurrentDesktopPermissionButAllowsAnExplicitOfflineQueue() throws {
    let files = [URL(fileURLWithPath: "/tmp/file.bin")]
    for invalid in [try directDevice(paired: false), try directDevice(canReceive: false), try directDevice(kind: "browser"), try directDevice("")] {
        #expect(throws: DirectSendValidationError.invalidRecipient) { try DirectSendBatch(device: invalid, urls: files) }
    }
    let offline = try DirectSendBatch(device: directDevice(online: false), urls: files)
    #expect(offline.deviceID == "laptop")
}

@Test func firstRecipientSuggestionRequiresOneEligibleOnlineDesktopAndNoExistingIntent() throws {
    let eligible = try directDevice()
    func suggest(_ devices: [Device], selected: String? = nil, explicit: Bool = false, draft: Bool = false, preparing: Bool = false) -> String? {
        RecipientSelection.defaultDeviceID(devices: devices, selectedID: selected, hasExplicitSelection: explicit, hasDraft: draft, isPreparing: preparing)
    }
    #expect(suggest([]) == nil)
    #expect(suggest([eligible]) == "laptop")
    #expect(suggest([eligible, try directDevice("second")]) == nil)
    for excluded in [try directDevice(online: false), try directDevice(paired: false), try directDevice(canReceive: false), try directDevice(kind: "browser"), try directDevice("")] {
        #expect(suggest([excluded]) == nil)
        #expect(suggest([eligible, excluded]) == "laptop")
    }
    #expect(suggest([eligible], selected: "unavailable-explicit-target") == nil)
    #expect(suggest([eligible], explicit: true) == nil)
    #expect(suggest([eligible], draft: true) == nil)
    #expect(suggest([eligible], preparing: true) == nil)
}
