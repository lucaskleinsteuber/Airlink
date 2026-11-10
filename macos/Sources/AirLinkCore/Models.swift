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
    public var destinationPath: String?
    public var skipped: Bool?
    public var duplicateState: String?
    public var duplicateCandidates: [DuplicateCandidate]?
    public var isSkipped: Bool { skipped ?? false }
}

public struct TransferProblem: Codable, Sendable {
 public var code, titleDE, titleEN, recoveryDE, recoveryEN: String
}

public struct Transfer: Codable, Identifiable, Sendable {
 public var problem: TransferProblem?
 public var retryable: Bool?
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
    public var skippedBytes: Int64?
    public var summary: Bool?
    public var fileCount: Int?
    public var hasContent: Bool?
    public var avoidedBytes: Int64 { max(0, skippedBytes ?? 0) }
    public var accountedBytes: Int64 {
        let confirmed = max(0, completedBytes)
        return confirmed > Int64.max - avoidedBytes ? Int64.max : confirmed + avoidedBytes
    }

    public var fractionCompleted: Double {
        if state == "completed" { return 1 }
        guard totalBytes > 0 else { return 0 }
        return min(1, max(0, Double(accountedBytes) / Double(totalBytes)))
    }
    public var isActive: Bool { ["offered", "queued", "running", "paused", "reconnecting", "verifying", "checkingDuplicate", "waitingDuplicate"].contains(state) }
    public var pendingDuplicateFiles: [TransferFile] { direction == "receive" ? files.filter { $0.duplicateState == "pending" && !$0.isSkipped } : [] }
    public var canResolveDuplicates: Bool { direction == "receive" && ["waitingDuplicate", "running", "queued"].contains(state) }
    public var hasReceivedContent: Bool {
        guard direction == "receive" else { return false }
        if summary == true { return hasContent == true }
        return files.contains { $0.directory == true || ($0.completed && !$0.isSkipped) }
    }
    public var numberOfFiles: Int { fileCount ?? files.count }
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
    private var storedCanClipboard: Bool?
    private var storedCanRelay: Bool?
    public var canClipboard: Bool {
        get { storedCanClipboard ?? false }
        set { storedCanClipboard = newValue }
    }
    public var canRelay: Bool {
        get { storedCanRelay ?? false }
        set { storedCanRelay = newValue }
    }
    public var autoAccept: Bool
    public var lastSeen: String?

    enum CodingKeys: String, CodingKey {
        case id, name, kind, address, fingerprint, paired, online, canSend, canReceive, autoAccept, lastSeen
        case storedCanClipboard = "canClipboard"
        case storedCanRelay = "canRelay"
    }
}

public struct PairRequest: Codable, Identifiable, Sendable {
    public var id: String
    public var name: String
    public var kind: String
    public var createdAt: String
}

public struct HostSettings: Codable, Equatable, Sendable {
    public var setupCompleted: Bool
    public var name: String
    public var receiveDirectory: String
    public var appearance: String
    public var language: String
    public var notifications: Bool
    public var automaticUpdateChecks: Bool = false
    public var sendLimitBytesPerSecond: Int64
    public var receiveLimitBytesPerSecond: Int64
    public var shortcut: Shortcut
    public var organization: Organization
    public init(name: String = "", receiveDirectory: String = "", appearance: String = "system", language: String = "de", notifications: Bool = true, sendLimitBytesPerSecond: Int64 = 0, receiveLimitBytesPerSecond: Int64 = 0, shortcut: Shortcut = Shortcut(), organization: Organization = Organization(), setupCompleted: Bool = false) {
        self.setupCompleted = setupCompleted
        self.name = name; self.receiveDirectory = receiveDirectory; self.appearance = appearance; self.language = language; self.notifications = notifications
        self.sendLimitBytesPerSecond = sendLimitBytesPerSecond; self.receiveLimitBytesPerSecond = receiveLimitBytesPerSecond; self.shortcut = shortcut
        self.organization = organization
    }
    enum CodingKeys: String, CodingKey {
        case name, receiveDirectory, appearance, language, notifications, automaticUpdateChecks, sendLimitBytesPerSecond, receiveLimitBytesPerSecond, shortcut, organization, setupCompleted
    }
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        setupCompleted = try c.decodeIfPresent(Bool.self, forKey: .setupCompleted) ?? false
        name = try c.decode(String.self, forKey: .name)
        receiveDirectory = try c.decode(String.self, forKey: .receiveDirectory)
        appearance = try c.decode(String.self, forKey: .appearance)
        language = try c.decode(String.self, forKey: .language)
        notifications = try c.decode(Bool.self, forKey: .notifications)
        automaticUpdateChecks = try c.decodeIfPresent(Bool.self, forKey: .automaticUpdateChecks) ?? false
        sendLimitBytesPerSecond = try c.decodeIfPresent(Int64.self, forKey: .sendLimitBytesPerSecond) ?? 0
        receiveLimitBytesPerSecond = try c.decodeIfPresent(Int64.self, forKey: .receiveLimitBytesPerSecond) ?? 0
        shortcut = try c.decodeIfPresent(Shortcut.self, forKey: .shortcut) ?? Shortcut()
        organization = try c.decodeIfPresent(Organization.self, forKey: .organization) ?? Organization()
    }
}

public struct HostStatus: Codable, Sendable {
    public var summary: Bool?
    public var activeTransferCount: Int?
    public var activeRelayCount: Int?
    public var capabilities: [String]?
    public var pairingSessions: [PairingSession]?
    public var preparations: [Preparation]?
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
    public var shares: [Share]?
    public var clipboard: [ClipboardMessage]?
    public var clipboardAllowed: Bool?
    public var currentDeviceId: String?
    public var relayDevices: [Device]?
    public var relays: [Relay]?
    public var statistics: Statistics?
}

public struct Share: Codable, Identifiable, Sendable {
    public var id: String
    public var name: String
    public var url: String?
    public var files: [TransferFile]
    public var totalBytes: Int64
    public var createdAt: String
    public var expiresAt: String?
    public var maxDownloads: Int64
    public var downloads: Int64
    public var passwordProtected: Bool
    public var revoked: Bool

    public var expirationDate: Date? { expiresAt.flatMap(ShareDate.parse) }
    public func isExpired(at now: Date = Date()) -> Bool { expirationDate.map { $0 <= now } ?? false }
    public var exhausted: Bool { maxDownloads > 0 && downloads >= maxDownloads }
}

public struct ShareCreate: Codable, Sendable {
    public var name: String
    public var paths: [String]?
    public var transferId: String?
    public var password: String?
    public var expiresAt: String?
    public var maxDownloads: Int64
    public init(name: String, paths: [String]? = nil, transferId: String? = nil, password: String? = nil, expiresAt: String? = nil, maxDownloads: Int64 = 0) {
        self.name = name; self.paths = paths; self.transferId = transferId; self.password = password; self.expiresAt = expiresAt; self.maxDownloads = maxDownloads
    }
    public var hasValidSource: Bool {
        let local = paths?.isEmpty == false
        let transfer = transferId?.isEmpty == false
        return local != transfer
    }
}

public struct ClipboardMessage: Codable, Identifiable, Sendable {
    public var id: String
    public var deviceId: String
    public var deviceName: String
    public var direction: String
    public var text: String
    public var createdAt: String
}

public enum ClipboardText {
    public static let maximumUTF8Bytes = 64 * 1024
    public static func isValid(_ text: String) -> Bool { !text.isEmpty && text.utf8.count <= maximumUTF8Bytes }
}

public enum ShareDate {
    public static func parse(_ string: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        if let date = formatter.date(from: string) { return date }
        formatter.formatOptions.insert(.withFractionalSeconds)
        return formatter.date(from: string)
    }
    public static func string(_ date: Date) -> String { ISO8601DateFormatter().string(from: date) }
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
