import Foundation

public enum DirectSendValidationError: LocalizedError, Sendable, Equatable {
    case invalidRecipient
    case emptyFiles
    case nonLocalFile

    public var errorDescription: String? {
        switch self {
        case .invalidRecipient: return "Choose a paired desktop device that is allowed to receive files."
        case .emptyFiles: return "Choose at least one file or folder to send."
        case .nonLocalFile: return "Only local files and folders can be sent."
        }
    }
}

/// A direct drop owns its recipient and sources independently of the editable draft.
/// Filesystem availability and source identity are checked by the transfer service.
public struct DirectSendBatch: Encodable, Identifiable, Sendable {
    public let id: UUID
    public let deviceID: String
    public let deviceName: String
    public let fingerprint: String?
    public let urls: [URL]
    public var paths: [String] { urls.map(\.path) }
    public var requestID: String { id.uuidString.replacingOccurrences(of: "-", with: "").lowercased() }

    public init(device: Device, urls: [URL]) throws {
        guard device.paired, device.kind == "engine", device.canReceive,
              !device.id.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            throw DirectSendValidationError.invalidRecipient
        }
        guard !urls.isEmpty else { throw DirectSendValidationError.emptyFiles }
        var unique: [URL] = [], seen = Set<String>()
        for url in urls {
            guard url.isFileURL, url.host == nil || url.host?.isEmpty == true || url.host?.lowercased() == "localhost",
                  url.user == nil, url.password == nil, url.query == nil, url.fragment == nil,
                  url.path.hasPrefix("/"), !url.path.utf8.contains(0),
                  url.absoluteString.range(of: "%00", options: .caseInsensitive) == nil else {
                throw DirectSendValidationError.nonLocalFile
            }
            // Preserve case and symlinks: resolving them here would change what
            // the user selected or merge distinct files on a case-sensitive disk.
            let normalized = url.standardizedFileURL
            if seen.insert(normalized.path).inserted { unique.append(normalized) }
        }
        self.id = UUID()
        self.deviceID = device.id
        self.deviceName = device.name
        self.fingerprint = device.fingerprint
        self.urls = unique
    }

    private enum CodingKeys: String, CodingKey { case deviceID = "deviceId", paths, requestID = "requestId" }
    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(deviceID, forKey: .deviceID)
        try container.encode(paths, forKey: .paths)
        try container.encode(requestID, forKey: .requestID)
    }
}

public enum RecipientSelection {
    /// Suggest only an unambiguous first recipient. An explicit or in-progress
    /// choice must never move to another device because a status snapshot changed.
    public static func defaultDeviceID(devices: [Device], selectedID: String?, hasExplicitSelection: Bool, hasDraft: Bool, isPreparing: Bool) -> String? {
        guard selectedID == nil || selectedID?.isEmpty == true,
              !hasExplicitSelection, !hasDraft, !isPreparing else { return nil }
        let eligible = devices.filter {
            $0.online && $0.paired && $0.kind == "engine" && $0.canReceive && !$0.id.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        }
        return eligible.count == 1 ? eligible[0].id : nil
    }
}
