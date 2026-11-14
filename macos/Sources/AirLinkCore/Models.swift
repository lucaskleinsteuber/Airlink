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
    public var isActive: Bool { ["offered", "queued", "running", "paused", "reconnecting", "verifying"].contains(state) }
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
    public var canClipboard: Bool {
        get { storedCanClipboard ?? false }
        set { storedCanClipboard = newValue }
    }
    public var autoAccept: Bool
    public var lastSeen: String?
    /// macos, linux or windows; laptop or desktop. Only chooses the symbol.
    public var platform: String?
    public var form: String?
    /// A nearby computer that runs an older, incompatible AirLink.
    public var outdated: Bool?

    enum CodingKeys: String, CodingKey {
        case id, name, kind, address, fingerprint, paired, online, canSend, canReceive, autoAccept, lastSeen, platform, form, outdated
        case storedCanClipboard = "canClipboard"
    }
    public init(id: String, name: String, kind: String = "engine", address: String? = nil, fingerprint: String? = nil, paired: Bool, online: Bool, canSend: Bool = true, canReceive: Bool = true, canClipboard: Bool = true, autoAccept: Bool = true, platform: String? = nil, form: String? = nil, outdated: Bool? = nil) {
        self.id = id; self.name = name; self.kind = kind; self.address = address; self.fingerprint = fingerprint
        self.paired = paired; self.online = online; self.canSend = canSend; self.canReceive = canReceive
        self.storedCanClipboard = canClipboard; self.autoAccept = autoAccept
        self.platform = platform; self.form = form; self.outdated = outdated
    }
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
    public init(name: String = "", receiveDirectory: String = "", appearance: String = "system", language: String = "de", notifications: Bool = true, sendLimitBytesPerSecond: Int64 = 0, receiveLimitBytesPerSecond: Int64 = 0, shortcut: Shortcut = Shortcut(), setupCompleted: Bool = false) {
        self.setupCompleted = setupCompleted
        self.name = name; self.receiveDirectory = receiveDirectory; self.appearance = appearance; self.language = language; self.notifications = notifications
        self.sendLimitBytesPerSecond = sendLimitBytesPerSecond; self.receiveLimitBytesPerSecond = receiveLimitBytesPerSecond; self.shortcut = shortcut
    }
    enum CodingKeys: String, CodingKey {
        case name, receiveDirectory, appearance, language, notifications, automaticUpdateChecks, sendLimitBytesPerSecond, receiveLimitBytesPerSecond, shortcut, setupCompleted
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
    }
}

public struct HostStatus: Codable, Sendable {
    public var summary: Bool?
    public var activeTransferCount: Int?
    public var capabilities: [String]?
    public var pairingSessions: [PairingSession]?
    public var preparations: [Preparation]?
    public var version: String
    public var id: String
    public var name: String
    public var urls: [String]
    public var fingerprint: String
    public var chunkSize: Int64
    public var devices: [Device]
    public var transfers: [Transfer]
    public var settings: HostSettings
    public var clipboard: [ClipboardMessage]?
    public var clipboardAllowed: Bool?
    public var currentDeviceId: String?
    public var statistics: Statistics?
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
