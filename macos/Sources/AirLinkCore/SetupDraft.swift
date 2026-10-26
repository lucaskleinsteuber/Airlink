import Foundation

public struct SetupDraft: Equatable, Sendable {
    public var name: String
    public var receiveDirectory: String
    public var notifications: Bool
    public init(name: String, receiveDirectory: String, notifications: Bool = false) { self.name = name; self.receiveDirectory = receiveDirectory; self.notifications = notifications }
    public var normalizedName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    public var validName: Bool {
        !normalizedName.isEmpty && normalizedName.utf8.count <= 120 &&
        !normalizedName.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains)
    }
    public var validFolder: Bool { receiveDirectory.hasPrefix("/") && !receiveDirectory.contains("\0") }
    public static func suggestedFolder(saved: String, downloads: URL) -> String {
        let old = downloads.appendingPathComponent("AirLink", isDirectory: true).standardizedFileURL.path
        if saved.isEmpty || URL(fileURLWithPath: saved).standardizedFileURL.path == old { return downloads.path }
        return saved
    }
    public static func suggestedName(saved: String, fallback: String) -> String {
        let value = saved.trimmingCharacters(in: .whitespacesAndNewlines)
        if value.isEmpty || value.lowercased().hasSuffix(".fritz.box") || UUID(uuidString: value) != nil { return fallback }
        return value
    }
}
