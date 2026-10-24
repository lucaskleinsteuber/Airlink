import Foundation
import Testing
@testable import AirLinkCore

@Test func absentOrganizationDefaultsOffWithoutInventingRules() throws {
    let fixture = Data(#"{"name":"Mac","receiveDirectory":"/tmp/received","appearance":"system","language":"system","notifications":false}"#.utf8)
    let settings = try JSONDecoder().decode(HostSettings.self, from: fixture)
    #expect(settings.organization == Organization())
    #expect(!settings.organization.detectDuplicates)
    #expect(!settings.organization.sortEnabled)
    #expect(settings.organization.rules.isEmpty)
    let partial = try JSONDecoder().decode(Organization.self, from: Data(#"{"rules":null}"#.utf8))
    #expect(partial == Organization())
}

@Test func rulePresetsAreEditableOrderedDataWithoutEnablingFeatures() throws {
    var organization = Organization(rules: Organization.presets)
    #expect(!organization.detectDuplicates && !organization.sortEnabled)
    #expect(organization.isValid)
    #expect(organization.rules.map(\.folder) == ["Images", "Videos", "Audio", "Documents", "Archives"])
    organization.rules[0].folder = "My photos"
    let encoded = try JSONEncoder().encode(organization)
    #expect(try JSONDecoder().decode(Organization.self, from: encoded) == organization)
    #expect(Organization.presets[0].folder == "Images")
}

@Test func rulesNormalizeExtensionsAndRejectUnsafeRelativeFolders() {
    #expect(SortRule.parseExtensions(".PNG, jpg; png\n.HEIC") == ["png", "jpg", "heic"])
    #expect(SortRule.safeFolder("Images/2026"))
    for unsafe in ["", "/Users/test", "../Images", "Images/../Other", "Images//Other", "Images/", "C:\\Files", ".airlink-partials", "Images/.AIRLINK-PARTIALS", "Images\nOther", "CON.txt", "Images/LPT1", "Images/trailing.", "Images/trailing ", "a?b"] {
        #expect(!SortRule.safeFolder(unsafe))
    }
    #expect(!SortRule(extensions: ["tar.gz"], folder: "Archives").isValid)
    #expect(!SortRule(extensions: ["../jpg"], folder: "Images").isValid)
    #expect(!SortRule(extensions: ["📷"], folder: "Images").isValid)
    #expect(!SortRule(extensions: ["c++"], folder: "Documents").isValid)
    #expect(!SortRule(extensions: [String(repeating: "a", count: 33)], folder: "Images").isValid)
    #expect(!SortRule(id: String(repeating: "a", count: 129), extensions: ["png"], folder: "Images").isValid)
    #expect(!SortRule(extensions: Array(repeating: "png", count: 101), folder: "Images").isValid)
    #expect(!Organization(sortEnabled: true).isValid)
    #expect(!Organization(rules: [SortRule(id: "a", extensions: ["jpg"], folder: "Images"), SortRule(id: "b", extensions: ["png"], folder: "images/Other")]).isValid)
}

@Test func firstMatchingRulePreservesFoldersAndValueSnapshot() {
    var organization = Organization(sortEnabled: true, rules: [SortRule(id: "first", extensions: [".PNG"], folder: "First"), SortRule(id: "second", extensions: ["png"], folder: "Second")])
    let snapshot = organization
    #expect(snapshot.mappedPath(for: "photo.PNG") == "First/photo.PNG")
    #expect(snapshot.mappedPath(for: "Folder/photo.PNG") == "Folder/photo.PNG")
    #expect(snapshot.mappedPath(for: "folder.png", directory: true) == "folder.png")
    #expect(snapshot.mappedPath(for: "notes.txt") == "notes.txt")
    organization.rules[0].folder = "Changed"
    #expect(snapshot.mappedPath(for: "photo.PNG") == "First/photo.PNG")
    organization.sortEnabled = false
    #expect(organization.mappedPath(for: "photo.PNG") == "photo.PNG")
}

private func duplicateTransfer() throws -> Transfer {
    try JSONDecoder().decode(Transfer.self, from: Data(#"{"id":"transfer","deviceId":"sender","deviceName":"Phone","direction":"receive","state":"waitingDuplicate","files":[{"id":"file","path":"photo.png","size":1099511627776,"received":0,"completed":false,"destinationPath":"Images/photo.png","duplicateState":"pending","duplicateCandidates":[{"id":"candidate","path":"/Users/test/Received/older-name.png","size":1099511627776,"sha256":"abc"}]}],"totalBytes":1099511627776,"completedBytes":0,"skippedBytes":0,"bytesPerSecond":0,"createdAt":"2026-10-24T00:00:00Z","updatedAt":"2026-10-24T00:00:01Z","destination":"/Users/test/Received/new"}"#.utf8))
}

@Test func duplicateAttentionIsIncomingOnlyAndCannotResolvePausedWork() throws {
    var transfer = try duplicateTransfer()
    #expect(transfer.isActive && transfer.canResolveDuplicates)
    #expect(transfer.pendingDuplicateFiles.count == 1)
    #expect(transfer.files[0].path == "photo.png")
    #expect(transfer.files[0].destinationPath == "Images/photo.png")
    #expect(transfer.files[0].duplicateCandidates?.first?.size == 1_099_511_627_776)
    transfer.state = "paused"
    #expect(!transfer.canResolveDuplicates)
    #expect(transfer.pendingDuplicateFiles.count == 1)
    transfer.state = "checkingDuplicate"
    #expect(transfer.isActive && !transfer.canResolveDuplicates)
    transfer.state = "waitingDuplicate"; transfer.direction = "send"
    #expect(transfer.pendingDuplicateFiles.isEmpty)
    #expect(!transfer.canResolveDuplicates)
}

@Test func mixedSkippedProgressKeepsConfirmedNetworkBytesSeparate() throws {
    var transfer = try duplicateTransfer()
    transfer.state = "running"
    transfer.completedBytes = 549_755_813_888
    transfer.skippedBytes = 274_877_906_944
    #expect(transfer.fractionCompleted == 0.75)
    #expect(transfer.completedBytes == 549_755_813_888)
    #expect(transfer.accountedBytes == 824_633_720_832)
    transfer.completedBytes = Int64.max; transfer.skippedBytes = Int64.max
    #expect(transfer.accountedBytes == Int64.max)
    #expect(transfer.fractionCompleted == 1)
    transfer.files[0].skipped = true; transfer.files[0].completed = true
    transfer.files[0].duplicateState = "skipped"
    #expect(!transfer.hasReceivedContent)
    #expect(transfer.pendingDuplicateFiles.isEmpty)
}

@Test func oldTransferAndStatsDecodeWithNoDuplicateNetworkInflation() throws {
    let old = Data(#"{"id":"old","deviceId":"d","deviceName":"Old","direction":"receive","state":"running","files":[],"totalBytes":100,"completedBytes":25,"bytesPerSecond":0,"createdAt":"","updatedAt":""}"#.utf8)
    let transfer = try JSONDecoder().decode(Transfer.self, from: old)
    #expect(transfer.avoidedBytes == 0 && transfer.fractionCompleted == 0.25)
    let stats = Data(#"{"sentBytes":0,"receivedBytes":12345,"completedTransfers":2,"failedTransfers":0,"cancelledTransfers":0,"activeTransfers":0,"relayedBytes":0,"relayDeliveries":0,"trafficSentBytes":0,"trafficReceivedBytes":12345,"skippedFiles":4,"skippedBytes":1099511627776,"days":[]}"#.utf8)
    let decoded = try JSONDecoder().decode(Statistics.self, from: stats)
    #expect(decoded.skippedBytes == 1_099_511_627_776)
    #expect(decoded.skippedFiles == 4)
    #expect(decoded.receivedBytes == 12345 && decoded.trafficReceivedBytes == 12345)
}
