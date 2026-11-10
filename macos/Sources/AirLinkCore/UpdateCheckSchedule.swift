import Foundation

public enum UpdateCheckSchedule {
    public static func isDue(enabled: Bool, lastAttempt: Date?, now: Date = Date()) -> Bool {
        guard enabled else { return false }
        guard let lastAttempt else { return true }
        return now.timeIntervalSince(lastAttempt) >= 86_400
    }
}
