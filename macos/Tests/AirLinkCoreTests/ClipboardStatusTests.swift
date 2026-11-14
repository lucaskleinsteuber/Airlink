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
    device.canClipboard = true
    let encoded = try JSONEncoder().encode(device)
    let object = try #require(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
    #expect(object["canClipboard"] as? Bool == true)
    #expect(object["storedCanClipboard"] == nil)
}

@Test func hostStatusDecodesWithoutOptionalClipboardAndStatisticsFields() throws {
    let fixture = Data(#"{"version":"0.13.0","id":"host","name":"Mac","urls":[],"fingerprint":"abc","chunkSize":8388608,"devices":[],"transfers":[],"settings":{"name":"Mac","receiveDirectory":"/tmp/received","appearance":"system","language":"system","notifications":false}}"#.utf8)
    let status = try JSONDecoder().decode(HostStatus.self, from: fixture)
    #expect(status.clipboard == nil)
    #expect(status.clipboardAllowed == nil)
    #expect(status.statistics == nil)
}
