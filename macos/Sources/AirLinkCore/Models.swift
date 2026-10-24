import Foundation

public struct TransferFile: Codable, Identifiable, Sendable {
    public var id: String
    public var path: String
    public var size: Int64
    public var modified: Int64?
    public var directory: Bool?
    public var sha256: String?
    public var received: Int64
    public var completed: Bool
}

public struct Transfer: Codable, Identifiable, Sendable {
    public var id: String
    public var deviceId: String
    public var deviceName: String
    public var direction: String
    public var state: String
    public var files: [TransferFile]
    public var totalBytes: Int64
    public var completedBytes: Int64
    public var bytesPerSecond: Double
    public var createdAt: String
    public var updatedAt: String
    public var error: String?
    public var destination: String?

    public var fractionCompleted: Double {
        if state == "completed" { return 1 }
        guard totalBytes > 0 else { return 0 }
        return min(1, max(0, Double(completedBytes) / Double(totalBytes)))
    }
    public var isActive: Bool { ["offered", "queued", "running", "paused", "reconnecting", "verifying"].contains(state) }
    public var title: String { files.first?.path.components(separatedBy: "/").last ?? "Transfer" }
}

public struct Device: Codable, Identifiable, Sendable {
    public var id: String
    public var name: String
    public var kind: String
    public var address: String?
    public var fingerprint: String?
    public var paired: Bool
    public var online: Bool
    public var canSend: Bool
    public var canReceive: Bool
    public var autoAccept: Bool
    public var lastSeen: String?
}

public struct PairRequest: Codable, Identifiable, Sendable {
    public var id: String
    public var name: String
    public var kind: String
    public var createdAt: String
}

public struct HostSettings: Codable, Equatable, Sendable {
    public var name: String
    public var receiveDirectory: String
    public var appearance: String
    public var language: String
    public var notifications: Bool
    public init(name: String = "", receiveDirectory: String = "", appearance: String = "system", language: String = "de", notifications: Bool = true) {
        self.name = name; self.receiveDirectory = receiveDirectory; self.appearance = appearance; self.language = language; self.notifications = notifications
    }
}

public struct HostStatus: Codable, Sendable {
    public var version: String
    public var id: String
    public var name: String
    public var urls: [String]
    public var setupURLs: [String]?
    public var fingerprint: String
    public var certificatePath: String?
    public var chunkSize: Int64
    public var devices: [Device]
    public var transfers: [Transfer]
    public var pairRequests: [PairRequest]
    public var settings: HostSettings
}

public enum NativeError: LocalizedError {
    case unavailable(String)
    public var errorDescription: String? { switch self { case .unavailable(let message): return message } }
}

public enum TransferFormatting {
    public static func bytes(_ value: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: max(0, value), countStyle: .file)
    }
    public static func speed(_ value: Double) -> String {
        let safe = value.isFinite ? min(Double(Int64.max / 2), max(0, value)) : 0
        return bytes(Int64(safe)) + "/s"
    }
}

public enum LanguagePreference {
    public static func usesEnglish(_ setting: String, preferredLanguages: [String] = Locale.preferredLanguages) -> Bool {
        if setting == "de" { return false }
        if setting == "en" { return true }
        return !(preferredLanguages.first?.lowercased().hasPrefix("de") ?? false)
    }
}
