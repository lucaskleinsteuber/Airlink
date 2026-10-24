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

public struct Relay: Codable, Identifiable, Sendable {
    public var id: String
    public var senderId: String
    public var senderName: String
    public var receiverId: String
    public var receiverName: String
    public var state: String
    public var files: [TransferFile]
    public var totalBytes: Int64
    public var deliveredBytes: Int64
    public var bytesPerSecond: Double
    public var createdAt: String
    public var updatedAt: String
    public var error: String?
    public var isActive: Bool { !["delivered", "failed", "cancelled"].contains(state) }
    public var fractionDelivered: Double {
        if state == "delivered" { return 1 }
        guard totalBytes > 0 else { return 0 }
        return min(1, max(0, Double(deliveredBytes) / Double(totalBytes)))
    }
    public var title: String { files.first?.path.components(separatedBy: "/").last ?? "Relay" }
}

public struct History: Codable, Sendable {
    public var transfers: [Transfer]
    public var relays: [Relay]
    public init(transfers: [Transfer] = [], relays: [Relay] = []) { self.transfers = transfers; self.relays = relays }
    public var entries: [HistoryEntry] {
        (transfers.map { HistoryEntry(transfer: $0) } + relays.map { HistoryEntry(relay: $0) }).sorted { $0.createdAt > $1.createdAt }
    }
}

public struct HistoryEntry: Identifiable, Sendable {
    public var transfer: Transfer?
    public var relay: Relay?
    public init(transfer: Transfer) { self.transfer = transfer; relay = nil }
    public init(relay: Relay) { transfer = nil; self.relay = relay }
    public var id: String { transfer.map { "transfer-" + $0.id } ?? "relay-" + (relay?.id ?? "") }
    public var state: String { transfer?.state ?? relay?.state ?? "" }
    public var direction: String { transfer?.direction ?? "relay" }
    public var createdAt: String { transfer?.createdAt ?? relay?.createdAt ?? "" }
    public var title: String { transfer?.title ?? relay?.title ?? "" }
    public var deviceNames: String { transfer?.deviceName ?? "\(relay?.senderName ?? "") → \(relay?.receiverName ?? "")" }
    public var deviceIDs: [String] { transfer.map { [$0.deviceId] } ?? [relay?.senderId ?? "", relay?.receiverId ?? ""] }
    public var files: [TransferFile] { transfer?.files ?? relay?.files ?? [] }
    public var isTerminal: Bool { ["completed", "failed", "cancelled", "delivered"].contains(state) }
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
    public var relayedBytes: Int64
    public var relayDeliveries: Int64
    public var trafficSentBytes: Int64
    public var trafficReceivedBytes: Int64
    public var days: [DayStatistics]?
}

public struct DayStatistics: Codable, Sendable {
    public var date: String
    public var sentBytes: Int64
    public var receivedBytes: Int64
    public var relayedBytes: Int64
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
