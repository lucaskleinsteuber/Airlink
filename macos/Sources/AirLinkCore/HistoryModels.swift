import Foundation

public struct Shortcut: Codable, Equatable, Sendable {
    public var enabled: Bool
    public var keyCode: UInt32
    public var modifiers: UInt32
    public init(enabled: Bool = true, keyCode: UInt32 = 37, modifiers: UInt32 = 768) {
        self.enabled = enabled; self.keyCode = keyCode; self.modifiers = modifiers
    }
    public var isValid: Bool {
        guard enabled else { return true }
        let allowed: UInt32 = 256 | 512 | 2048 | 4096
        return keyCode <= 127 && ![54, 55, 56, 57, 58, 59, 60, 61, 62, 63].contains(keyCode)
            && modifiers & (256 | 4096) != 0 && modifiers & ~allowed == 0
    }
}

public enum RateLimit {
    public static let maximum: Int64 = 1 << 40
    public static func isValid(_ value: Int64) -> Bool { value == 0 || (1024...maximum).contains(value) }
    public static func parse(_ input: String, unit: Int64, enabled: Bool) -> Int64? {
        if !enabled { return 0 }
        guard unit == 1024 || unit == 1_048_576,
              let value = Double(input.trimmingCharacters(in: .whitespacesAndNewlines).replacingOccurrences(of: ",", with: ".")), value.isFinite else { return nil }
        let bytes = value * Double(unit)
        guard bytes >= 1024 && bytes <= Double(maximum) else { return nil }
        return Int64(bytes.rounded())
    }
}

public struct History: Codable, Sendable {
    public var transfers: [Transfer]
    public init(transfers: [Transfer] = []) { self.transfers = transfers }
    public var entries: [HistoryEntry] {
        transfers.map { HistoryEntry(transfer: $0) }.sorted { $0.createdAt > $1.createdAt }
    }
}

public struct HistoryEntry: Identifiable, Sendable {
    public var transfer: Transfer
    public init(transfer: Transfer) { self.transfer = transfer }
    public var id: String { "transfer-" + transfer.id }
    public var state: String { transfer.state }
    public var direction: String { transfer.direction }
    public var createdAt: String { transfer.createdAt }
    public var title: String { transfer.title }
    public var deviceNames: String { transfer.deviceName }
    public var deviceIDs: [String] { [transfer.deviceId] }
    public var files: [TransferFile] { transfer.files }
    public var isTerminal: Bool { ["completed", "failed", "cancelled"].contains(state) }
}

public struct HistoryFilter: Codable, Equatable, Sendable {
    public var query = ""
    public var state = ""
    public var direction = ""
    public var deviceId = ""
    public var limit = 100
    public init() {}
    public func matches(_ entry: HistoryEntry) -> Bool {
        if !state.isEmpty && entry.state != state { return false }
        if !direction.isEmpty && entry.direction != direction { return false }
        if !deviceId.isEmpty && !entry.deviceIDs.contains(deviceId) { return false }
        let search = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return search.isEmpty || entry.deviceNames.localizedCaseInsensitiveContains(search) || entry.files.contains { $0.path.localizedCaseInsensitiveContains(search) }
    }
}

public struct Statistics: Codable, Sendable {
    public var sentBytes: Int64
    public var receivedBytes: Int64
    public var completedTransfers: Int64
    public var failedTransfers: Int64
    public var cancelledTransfers: Int64
    public var activeTransfers: Int64
    public var trafficSentBytes: Int64
    public var trafficReceivedBytes: Int64
    public var skippedFiles: Int64?
    public var skippedBytes: Int64?
    public var days: [DayStatistics]?
}

public struct DayStatistics: Codable, Sendable {
    public var date: String
    public var sentBytes: Int64
    public var receivedBytes: Int64
    public var completedTransfers: Int64
}

public enum ServiceSelection {
    public static func urls(modern: [URL], legacyPaths: [String]) -> [URL] {
        let files = modern.filter(\.isFileURL)
        let candidates = files.isEmpty ? legacyPaths.filter { $0.hasPrefix("/") }.map { URL(fileURLWithPath: $0) } : files
        var seen = Set<String>()
        return candidates.filter { $0.isFileURL && seen.insert($0.standardizedFileURL.path).inserted }
    }
}
