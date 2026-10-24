import Foundation
import Testing
@testable import AirLinkCore

@Test func clipboardLimitCountsUTF8BytesInsteadOfCharacters() {
    #expect(!ClipboardText.isValid(""))
    #expect(ClipboardText.isValid(String(repeating: "a", count: 65_536)))
    #expect(!ClipboardText.isValid(String(repeating: "a", count: 65_537)))
    #expect(ClipboardText.isValid(String(repeating: "é", count: 32_768)))
    #expect(!ClipboardText.isValid(String(repeating: "é", count: 32_768) + "x"))
}

@Test func oldDeviceDefaultsClipboardPermissionToFalse() throws {
    let fixture = Data(#"{"id":"d","name":"Old peer","kind":"engine","paired":true,"online":false,"canSend":true,"canReceive":true,"autoAccept":false}"#.utf8)
    var device = try JSONDecoder().decode(Device.self, from: fixture)
    #expect(!device.canClipboard)
    #expect(!device.canRelay)
    device.canClipboard = true
    let encoded = try JSONEncoder().encode(device)
    let object = try #require(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
    #expect(object["canClipboard"] as? Bool == true)
    #expect(object["storedCanClipboard"] == nil)
}

@Test func oldHostStatusCanDecodeWithoutShareAndClipboardFields() throws {
    let fixture = Data(#"{"version":"0.1.0","id":"host","name":"Mac","urls":[],"fingerprint":"abc","chunkSize":8388608,"devices":[],"transfers":[],"pairRequests":[],"settings":{"name":"Mac","receiveDirectory":"/tmp/received","appearance":"system","language":"system","notifications":false}}"#.utf8)
    let status = try JSONDecoder().decode(HostStatus.self, from: fixture)
    #expect(status.shares == nil)
    #expect(status.clipboard == nil)
    #expect(status.clipboardAllowed == nil)
    #expect(status.relays == nil)
    #expect(status.statistics == nil)
}

@Test func sharePolicyPreserves64BitLimitsAndExactlyOneSource() throws {
    var request = ShareCreate(name: "Archive", paths: ["/Volumes/External/Archive"], maxDownloads: 4_294_967_296)
    #expect(request.hasValidSource)
    let encoded = try JSONEncoder().encode(request)
    #expect(try JSONDecoder().decode(ShareCreate.self, from: encoded).maxDownloads == 4_294_967_296)
    request.transferId = "transfer"
    #expect(!request.hasValidSource)
    request.paths = nil
    #expect(request.hasValidSource)
    request.transferId = nil
    #expect(!request.hasValidSource)
}

@Test func shareStateHandlesExpiryQuotaAndLargeByteCounts() throws {
    let fixture = Data(#"{"id":"s","name":"Archive","url":"https://192.168.1.2/s/s#secret","files":[],"totalBytes":1099511627776,"createdAt":"2026-10-24T00:00:00Z","expiresAt":"2026-10-25T00:00:00.123Z","maxDownloads":2,"downloads":2,"passwordProtected":true,"revoked":false}"#.utf8)
    let share = try JSONDecoder().decode(Share.self, from: fixture)
    #expect(share.totalBytes == 1_099_511_627_776)
    #expect(share.exhausted)
    let expiry = try #require(share.expirationDate)
    #expect(!share.isExpired(at: expiry.addingTimeInterval(-1)))
    #expect(share.isExpired(at: expiry))
    #expect(share.url?.hasSuffix("#secret") == true)
    #expect(ShareDate.parse(ShareDate.string(expiry)) != nil)
}
