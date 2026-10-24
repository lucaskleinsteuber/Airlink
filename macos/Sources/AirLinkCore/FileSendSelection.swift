import Foundation

public struct FileSendSubmission: Encodable, Sendable {
    fileprivate let id: UUID
    fileprivate let entryIDs: Set<UUID>
    public let deviceId: String
    public let paths: [String]
    enum CodingKeys: String, CodingKey { case deviceId, paths }
}

/// A submission owns a snapshot; later selections remain independent of its ACK.
public struct FileSendSelection: Sendable {
    private struct Entry: Sendable { var id = UUID(); var url: URL }
    private var entries: [Entry] = []
    private var submission: FileSendSubmission?
    public init() {}
    public var urls: [URL] { entries.map(\.url) }
    public var isSending: Bool { submission != nil }

    public mutating func append(_ urls: [URL]) {
        for url in urls where url.isFileURL && !entries.contains(where: { $0.url == url }) {
            entries.append(Entry(url: url))
        }
    }
    public mutating func replace(_ urls: [URL]) {
        let previous = entries
        entries = []
        for url in urls where url.isFileURL && !entries.contains(where: { $0.url == url }) {
            entries.append(previous.first(where: { $0.url == url }) ?? Entry(url: url))
        }
    }
    public mutating func begin(deviceID: String) -> FileSendSubmission? {
        guard submission == nil, !deviceID.isEmpty, !entries.isEmpty else { return nil }
        let next = FileSendSubmission(id: UUID(), entryIDs: Set(entries.map(\.id)), deviceId: deviceID, paths: entries.map { $0.url.path })
        submission = next
        return next
    }
    public mutating func acknowledge(_ completed: FileSendSubmission) {
        guard submission?.id == completed.id else { return }
        entries.removeAll { completed.entryIDs.contains($0.id) }
        submission = nil
    }
    public mutating func fail(_ failed: FileSendSubmission) {
        guard submission?.id == failed.id else { return }
        submission = nil
    }
}
