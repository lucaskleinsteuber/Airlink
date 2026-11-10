import Foundation

public struct PairingSession: Codable, Identifiable, Sendable {
    public var id: String
    public var peerId: String
    public var peerName: String
    public var state: String
    public var comparisonCode: String?
    public var expiresAt: String
    public var localConfirmed: Bool
    public var remoteConfirmed: Bool
    public var automaticReceive: Bool
    public var error: String?
    public var canConfirm: Bool { state == "comparing" && !localConfirmed && comparisonCode?.count == 8 }
    public var formattedCode: String {
        guard let code = comparisonCode, code.count == 8 else { return "" }
        return String(code.prefix(4)) + " " + code.suffix(4)
    }
    public var needsAttention: Bool { canConfirm }
}

public struct Preparation: Codable, Identifiable, Sendable {
    public var id: String
    public var requestId: String
    public var deviceId: String
    public var state: String
    public var transferId: String?
    public var error: String?
}

public struct TransferPage: Decodable, Sendable {
    public var transfers: [Transfer]
    public var nextCursor: String?
}

public struct TransferFilePage: Decodable, Sendable {
    public var files: [TransferFile]
    public var nextOffset: Int
    public var total: Int
}

public enum DesktopTransferSelection {
    private static func timestampKey(_ raw: String) -> String {
        guard raw.hasSuffix("Z"), raw.count >= 20 else { return raw }
        let parts = raw.dropLast().split(separator: ".", maxSplits: 1, omittingEmptySubsequences: false)
        let fraction = parts.count == 2 ? String(parts[1].prefix(9)) : ""
        return String(parts[0]) + fraction + String(repeating: "0", count: max(0, 9 - fraction.count))
    }
    public static func mergeHistory(_ history: [Transfer], summary: [Transfer]) -> [Transfer] {
        var rows: [String: Transfer] = [:]
        for item in history { rows[item.id] = item }
        for item in summary where item.isActive || rows[item.id] != nil {
            if let known = rows[item.id], timestampKey(item.updatedAt) < timestampKey(known.updatedAt) { continue }
            rows[item.id] = item
        }
        return rows.values.sorted {
            if $0.isActive != $1.isActive { return $0.isActive }
            let left = timestampKey($0.updatedAt), right = timestampKey($1.updatedAt)
            return left == right ? $0.id > $1.id : left > right
        }
    }
    public static func visible(_ transfers: [Transfer], deviceID: String?, recentLimit: Int = 3) -> [Transfer] {
        let matching = transfers.filter { deviceID == nil || $0.deviceId == deviceID }
        let newer: (Transfer, Transfer) -> Bool = { left, right in
            left.updatedAt == right.updatedAt ? left.id > right.id : left.updatedAt > right.updatedAt
        }
        return matching.filter(\.isActive).sorted(by: newer) + matching.filter { !$0.isActive }.sorted(by: newer).prefix(max(0, recentLimit))
    }
    public static func remainingSeconds(_ transfer: Transfer) -> Int? {
        guard transfer.state == "running", transfer.bytesPerSecond.isFinite, transfer.bytesPerSecond > 0 else { return nil }
        let left = max(0, Double(transfer.totalBytes) - Double(transfer.accountedBytes))
        let seconds = ceil(left / transfer.bytesPerSecond)
        guard seconds.isFinite, seconds <= 31_536_000 else { return nil }
        return Int(seconds)
    }
}
