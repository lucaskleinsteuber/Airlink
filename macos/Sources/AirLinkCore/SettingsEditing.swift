import Foundation

public enum SettingsEditing {
    /// Apply deliberate edits to the newest host snapshot. Opening setup or
    /// another settings surface must not revert fields this editor never changed.
    public static func merge(baseline: HostSettings, draft: HostSettings, latest: HostSettings) -> HostSettings {
        var result = latest
        if draft.name != baseline.name { result.name = draft.name }
        if draft.receiveDirectory != baseline.receiveDirectory { result.receiveDirectory = draft.receiveDirectory }
        if draft.appearance != baseline.appearance { result.appearance = draft.appearance }
        if draft.language != baseline.language { result.language = draft.language }
        if draft.notifications != baseline.notifications { result.notifications = draft.notifications }
        if draft.automaticUpdateChecks != baseline.automaticUpdateChecks { result.automaticUpdateChecks = draft.automaticUpdateChecks }
        if draft.sendLimitBytesPerSecond != baseline.sendLimitBytesPerSecond { result.sendLimitBytesPerSecond = draft.sendLimitBytesPerSecond }
        if draft.receiveLimitBytesPerSecond != baseline.receiveLimitBytesPerSecond { result.receiveLimitBytesPerSecond = draft.receiveLimitBytesPerSecond }
        if draft.shortcut.enabled != baseline.shortcut.enabled { result.shortcut.enabled = draft.shortcut.enabled }
        if draft.shortcut.keyCode != baseline.shortcut.keyCode || draft.shortcut.modifiers != baseline.shortcut.modifiers {
            result.shortcut.keyCode = draft.shortcut.keyCode
            result.shortcut.modifiers = draft.shortcut.modifiers
        }
        if draft.organization.detectDuplicates != baseline.organization.detectDuplicates { result.organization.detectDuplicates = draft.organization.detectDuplicates }
        if draft.organization.sortEnabled != baseline.organization.sortEnabled { result.organization.sortEnabled = draft.organization.sortEnabled }
        if draft.organization.rules != baseline.organization.rules { result.organization.rules = draft.organization.rules }
        if draft.setupCompleted != baseline.setupCompleted { result.setupCompleted = draft.setupCompleted }
        return result
    }
}
