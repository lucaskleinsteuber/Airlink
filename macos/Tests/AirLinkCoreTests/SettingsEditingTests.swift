import Foundation
import Testing
@testable import AirLinkCore

@Test func savingOneSettingsCategoryKeepsNewerUneditedValues() {
    let baseline = HostSettings(name: "Mac", receiveDirectory: "/tmp/initial", notifications: false)
    var draft = baseline
    draft.name = "My MacBook"
    var latest = baseline
    latest.receiveDirectory = "/Volumes/Incoming"
    latest.notifications = true
    latest.receiveLimitBytesPerSecond = 8 * 1_048_576
    latest.organization = Organization(detectDuplicates: true)
    latest.shortcut = Shortcut(enabled: false)
    latest.setupCompleted = true
    let saved = SettingsEditing.merge(baseline: baseline, draft: draft, latest: latest)
    #expect(saved.name == "My MacBook")
    #expect(saved.receiveDirectory == latest.receiveDirectory)
    #expect(saved.notifications && saved.setupCompleted)
    #expect(saved.organization == latest.organization)
    #expect(saved.shortcut == latest.shortcut)
    #expect(saved.receiveLimitBytesPerSecond == latest.receiveLimitBytesPerSecond)
    #expect(draft.receiveDirectory == baseline.receiveDirectory)
}

@Test func explicitUnlimitedAndDisabledChoicesOverrideTheirBaseline() {
    let baseline = HostSettings(name: "Mac", receiveDirectory: "/tmp/received", notifications: true, sendLimitBytesPerSecond: 1024)
    var draft = baseline
    draft.sendLimitBytesPerSecond = 0
    draft.notifications = false
    var latest = baseline
    latest.sendLimitBytesPerSecond = 8 * 1_048_576
    latest.appearance = "dark"
    latest.language = "en"
    let saved = SettingsEditing.merge(baseline: baseline, draft: draft, latest: latest)
    #expect(saved.sendLimitBytesPerSecond == 0 && !saved.notifications)
    #expect(saved.appearance == "dark" && saved.language == "en")
    #expect(SettingsEditing.merge(baseline: baseline, draft: baseline, latest: latest) == latest)
}

@Test func setupReviewKeepsItsExplicitNotificationChoiceInItsDraft() {
    var draft = SetupDraft(name: "Mac", receiveDirectory: "/tmp/received", notifications: true)
    let snapshot = draft
    draft.notifications = false
    #expect(snapshot.notifications)
    #expect(!draft.notifications)
    #expect(draft.validName && draft.validFolder)
}

@Test func togglingOrganizationAndShortcutOptionsKeepsNewerRulesAndKeyChord() {
    let baseline = HostSettings(name: "Mac", receiveDirectory: "/tmp/received")
    var draft = baseline
    draft.organization.detectDuplicates = true
    draft.shortcut.enabled = false
    var latest = baseline
    latest.organization.rules = [SortRule(id: "images", extensions: ["png"], folder: "Pictures")]
    latest.organization.sortEnabled = true
    latest.shortcut.keyCode = 40
    latest.shortcut.modifiers = 256
    let saved = SettingsEditing.merge(baseline: baseline, draft: draft, latest: latest)
    #expect(saved.organization.detectDuplicates)
    #expect(saved.organization.sortEnabled && saved.organization.rules == latest.organization.rules)
    #expect(!saved.shortcut.enabled)
    #expect(saved.shortcut.keyCode == 40 && saved.shortcut.modifiers == 256)
    var recorded = baseline
    recorded.shortcut.keyCode = 12
    let chord = SettingsEditing.merge(baseline: baseline, draft: recorded, latest: latest)
    #expect(chord.shortcut.keyCode == 12 && chord.shortcut.modifiers == baseline.shortcut.modifiers)
}
