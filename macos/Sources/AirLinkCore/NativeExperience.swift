import Foundation

/// The address is a connection hint. Only the paired certificate identity
/// establishes that an approved request reached the intended installation.
public struct PeerConnectionDraft: Equatable, Sendable {
    public var address: String
    public var fingerprint: String
    public var automaticReceive: Bool
    public init(address: String = "", fingerprint: String = "", automaticReceive: Bool = false) {
        self.address = address; self.fingerprint = fingerprint
        self.automaticReceive = automaticReceive
    }
    public init(device: Device) {
        self.init(address: device.address ?? "", fingerprint: device.fingerprint ?? "")
    }
    public var normalizedAddress: String {
        var value = address.trimmingCharacters(in: .whitespacesAndNewlines)
        if value.hasSuffix("/") { value.removeLast() }
        if var parts = URLComponents(string: value), parts.scheme?.lowercased() == "https" {
            parts.scheme = "https"
            return parts.string ?? value
        }
        return value
    }
    public var normalizedFingerprint: String { fingerprint.filter { !$0.isWhitespace && $0 != ":" }.lowercased() }
    public var isValid: Bool {
        guard let parts = URLComponents(string: normalizedAddress), parts.scheme?.lowercased() == "https",
              let host = parts.host, !host.isEmpty, parts.user == nil, parts.password == nil,
              parts.query == nil, parts.fragment == nil, parts.path.isEmpty else { return false }
        return normalizedFingerprint.utf8.count == 64 && normalizedFingerprint.utf8.allSatisfy { (48...57).contains($0) || (97...102).contains($0) }
    }
    public func pairedDevice(in devices: [Device]) -> Device? {
        guard isValid else { return nil }
        return devices.first { $0.paired && $0.kind == "engine" && $0.fingerprint?.lowercased() == normalizedFingerprint }
    }
    public var comparisonFingerprint: String {
        let characters = Array(normalizedFingerprint)
        return stride(from: 0, to: characters.count, by: 8).map { String(characters[$0..<min($0 + 8, characters.count)]) }.joined(separator: " ")
    }
}

public struct PeerConnectionAttempt: Sendable {
    public var requestID: String
    public var draft: PeerConnectionDraft
    public var requestedAt: Date
    public init(requestID: String, draft: PeerConnectionDraft, requestedAt: Date = Date()) {
        self.requestID = requestID; self.draft = draft; self.requestedAt = requestedAt
    }
    public func isExpired(at date: Date = Date()) -> Bool { date.timeIntervalSince(requestedAt) >= 5 * 60 }
    public func matches(_ other: PeerConnectionDraft) -> Bool { draft.normalizedFingerprint == other.normalizedFingerprint && draft.normalizedAddress == other.normalizedAddress }
}

/// Private host-only result paths returned after checking the received entries.
public struct ReceivedTransfer: Decodable, Sendable {
    public var directory: String
    public var items: [ReceivedItem]
    public func item(fileID: String) -> ReceivedItem? { items.first { $0.fileId == fileID } }
    public var singleItem: ReceivedItem? { items.count == 1 ? items.first : nil }
}

public struct ReceivedItem: Decodable, Identifiable, Sendable {
    public var fileId: String
    public var path: String
    public var directory: Bool
    public var id: String { fileId }
}

public extension Transfer {
    var canAcceptIncoming: Bool { direction == "receive" && state == "offered" }
    func canAcceptAndTrust(device: Device?) -> Bool {
        guard canAcceptIncoming, let device else { return false }
        return device.id == deviceId && device.paired && device.canSend
    }
    /// Display identities only. The actual reveal action asks the helper for
    /// current validated paths instead of constructing a path from this list.
    var receivedResultEntries: [TransferFile] {
        guard direction == "receive", state == "completed" else { return [] }
        let regular = files.filter { $0.directory != true }
        if !regular.isEmpty && !regular.contains(where: { $0.completed && !$0.isSkipped }) { return [] }
        let actual = files.filter { $0.completed && !$0.isSkipped }
        func publishedPath(_ file: TransferFile) -> String {
            guard let destination = file.destinationPath, !destination.isEmpty else { return file.path }
            return destination
        }
        let directories = actual.filter { $0.directory == true }.map { publishedPath($0) + "/" }
        return actual.filter { file in !directories.contains { publishedPath(file).hasPrefix($0) } }
    }
}
