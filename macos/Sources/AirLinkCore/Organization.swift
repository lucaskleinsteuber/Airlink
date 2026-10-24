import Foundation

public struct Organization: Codable, Equatable, Sendable {
    public var detectDuplicates: Bool
    public var sortEnabled: Bool
    public var rules: [SortRule]
    public init(detectDuplicates: Bool = false, sortEnabled: Bool = false, rules: [SortRule] = []) {
        self.detectDuplicates = detectDuplicates; self.sortEnabled = sortEnabled; self.rules = rules
    }
    enum CodingKeys: String, CodingKey { case detectDuplicates, sortEnabled, rules }
    public init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        detectDuplicates = try values.decodeIfPresent(Bool.self, forKey: .detectDuplicates) ?? false
        sortEnabled = try values.decodeIfPresent(Bool.self, forKey: .sortEnabled) ?? false
        rules = try values.decodeIfPresent([SortRule].self, forKey: .rules) ?? []
    }
    public var isValid: Bool {
        guard rules.count <= 100, !sortEnabled || !rules.isEmpty, rules.allSatisfy(\.isValid), Set(rules.map(\.id)).count == rules.count else { return false }
        var folders: [String: String] = [:]
        for rule in rules {
            let parts = rule.folder.split(separator: "/")
            for count in 1...parts.count {
                let prefix = parts.prefix(count).joined(separator: "/")
                let key = prefix.lowercased()
                if let previous = folders[key], previous != prefix { return false }
                folders[key] = prefix
            }
        }
        return true
    }
    public func mappedPath(for original: String, directory: Bool = false) -> String {
        guard sortEnabled, isValid, !directory, !original.contains("/") else { return original }
        let ext = original.lastIndex(of: ".").map { String(original[original.index(after: $0)...]).lowercased() } ?? ""
        guard let rule = rules.first(where: { $0.extensions.map(SortRule.normalizedExtension).contains(ext) }) else { return original }
        return rule.folder + "/" + original
    }
    public static var presets: [SortRule] { [
        SortRule(id: "images", extensions: ["jpg", "jpeg", "png", "heic", "gif", "webp", "tif", "tiff", "avif"], folder: "Images"),
        SortRule(id: "videos", extensions: ["mp4", "mov", "m4v", "mkv", "avi", "webm"], folder: "Videos"),
        SortRule(id: "audio", extensions: ["mp3", "m4a", "aac", "flac", "wav", "aiff", "ogg"], folder: "Audio"),
        SortRule(id: "documents", extensions: ["pdf", "txt", "md", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "pages", "numbers", "key"], folder: "Documents"),
        SortRule(id: "archives", extensions: ["zip", "7z", "tar", "gz", "bz2", "xz", "rar"], folder: "Archives")
    ] }
}

public struct SortRule: Codable, Equatable, Identifiable, Sendable {
    public var id: String
    public var extensions: [String]
    public var folder: String
    public init(id: String = UUID().uuidString, extensions: [String] = [], folder: String = "") {
        self.id = id; self.extensions = extensions; self.folder = folder
    }
    public static func normalizedExtension(_ value: String) -> String {
        var result = value.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if result.hasPrefix(".") { result.removeFirst() }
        return result
    }
    public static func parseExtensions(_ value: String) -> [String] {
        var seen = Set<String>()
        return value.split(whereSeparator: { $0 == "," || $0 == ";" || $0.isWhitespace }).map { normalizedExtension(String($0)) }.filter { !$0.isEmpty && seen.insert($0).inserted }
    }
    public var isValid: Bool {
        !id.isEmpty && id.utf8.count <= 128 && !extensions.isEmpty && extensions.count <= 100 && extensions.allSatisfy {
            let ext = Self.normalizedExtension($0)
            return !ext.isEmpty && ext.utf8.count <= 32 && ext.utf8.allSatisfy { (97...122).contains($0) || (48...57).contains($0) || $0 == 45 || $0 == 95 }
        } && Self.safeFolder(folder)
    }
    public static func safeFolder(_ folder: String) -> Bool {
        guard !folder.isEmpty, folder.utf8.count <= 512, !folder.hasPrefix("/"), !folder.contains(where: { "\\:<>\"|?*".contains($0) }), !folder.unicodeScalars.contains(where: { CharacterSet.controlCharacters.contains($0) }) else { return false }
        return folder.split(separator: "/", omittingEmptySubsequences: false).allSatisfy { part in
            guard !part.isEmpty, part != ".", part != "..", !part.hasSuffix("."), !part.hasSuffix(" "), part.lowercased() != ".airlink-partials" else { return false }
            let stem = part.split(separator: ".", omittingEmptySubsequences: false).first?.uppercased() ?? ""
            if ["CON", "PRN", "AUX", "NUL"].contains(stem) { return false }
            if stem.count == 4, stem.hasPrefix("COM") || stem.hasPrefix("LPT"), let last = stem.last, ("1"..."9").contains(String(last)) { return false }
            return true
        }
    }
}

public struct DuplicateCandidate: Codable, Identifiable, Sendable {
    public var id: String
    public var path: String
    public var size: Int64
    public var sha256: String
}
