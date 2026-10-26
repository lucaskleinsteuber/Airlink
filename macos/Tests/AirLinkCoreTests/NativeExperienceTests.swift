import Foundation
import Testing
@testable import AirLinkCore

private let peerFingerprint = String(repeating: "a", count: 64)
private func experienceDevice() throws -> Device {
    try JSONDecoder().decode(Device.self, from: Data("""
    {"id":"peer","name":"Windows laptop","kind":"engine","address":"https://192.168.1.3:43871","fingerprint":"\(peerFingerprint)","paired":false,"online":true,"canSend":true,"canReceive":true,"autoAccept":false}
    """.utf8))
}
private func experienceTransfer(files: String = #"[{"id":"f","path":"file.txt","size":3,"received":3,"completed":true}]"#) throws -> Transfer {
    try JSONDecoder().decode(Transfer.self, from: Data("""
    {"id":"transfer","deviceId":"peer","deviceName":"Windows laptop","direction":"receive","state":"completed","files":\(files),"totalBytes":3,"completedBytes":3,"bytesPerSecond":0,"createdAt":"","updatedAt":"","destination":"/Users/test/Downloads/AirLink/received"}
    """.utf8))
}

@Test func discoveredPeerPrefillsConnectionWithoutEnablingAutomaticReceipt() throws {
    let device = try experienceDevice()
    var draft = PeerConnectionDraft(device: device)
    #expect(draft.address == device.address)
    #expect(draft.fingerprint == peerFingerprint)
    #expect(draft.isValid)
    #expect(!draft.automaticReceive)
    draft.automaticReceive = true
    #expect(draft.isValid)
    #expect(!device.autoAccept)
}

@Test func connectionDraftNormalizesIdentityAndRejectsUnsafeAddressParts() {
    let draft = PeerConnectionDraft(address: "  https://192.168.1.3:43871/\n", fingerprint: Array(repeating: "AA", count: 32).joined(separator: ":"))
    #expect(draft.isValid)
    #expect(draft.normalizedAddress == "https://192.168.1.3:43871")
    #expect(draft.normalizedFingerprint == peerFingerprint)
    #expect(draft.comparisonFingerprint == Array(repeating: "aaaaaaaa", count: 8).joined(separator: " "))
    #expect(PeerConnectionDraft(address: "HTTPS://localhost:43871/", fingerprint: peerFingerprint).normalizedAddress == "https://localhost:43871")
    for address in ["http://192.168.1.3:43871", "https://user:password@192.168.1.3", "https://192.168.1.3/api", "https://192.168.1.3/?token=secret", "https://192.168.1.3/#fragment", "https://"] {
        #expect(!PeerConnectionDraft(address: address, fingerprint: peerFingerprint).isValid)
    }
    #expect(!PeerConnectionDraft(address: "https://localhost:43871", fingerprint: String(repeating: "ａ", count: 64)).isValid)
    #expect(!PeerConnectionDraft(address: "https://localhost:43871", fingerprint: String(repeating: "a", count: 63)).isValid)
}

@Test func pairingCompletionMatchesPairedCertificateIdentityRatherThanAddressAlone() throws {
    var device = try experienceDevice()
    let draft = PeerConnectionDraft(device: device)
    #expect(draft.pairedDevice(in: [device]) == nil)
    device.paired = true
    device.address = "https://192.168.1.99:43871"
    #expect(draft.pairedDevice(in: [device])?.id == "peer")
    device.fingerprint = String(repeating: "b", count: 64)
    #expect(draft.pairedDevice(in: [device]) == nil)
    device.fingerprint = peerFingerprint; device.kind = "browser"
    #expect(draft.pairedDevice(in: [device]) == nil)
}

@Test func pendingPairingKeepsItsExplicitPolicySnapshotAndExpiresWithoutApproval() {
    var draft = PeerConnectionDraft(address: "https://peer.local:43871/", fingerprint: peerFingerprint)
    let started = Date(timeIntervalSince1970: 1000)
    let attempt = PeerConnectionAttempt(requestID: "request", draft: draft, requestedAt: started)
    draft.automaticReceive = true
    #expect(!attempt.draft.automaticReceive)
    #expect(attempt.matches(draft))
    #expect(!attempt.isExpired(at: started.addingTimeInterval(299)))
    #expect(attempt.isExpired(at: started.addingTimeInterval(300)))
    #expect(PeerConnectionDraft().automaticReceive == false)
}

@Test func identityComparisonDoesNotCarryOverToAnotherAddressOrCertificate() {
    var draft = PeerConnectionDraft(address: "https://peer.local:43871", fingerprint: peerFingerprint)
    var verification = PeerVerification()
    #expect(!verification.matches(draft))
    verification.confirm(draft)
    #expect(verification.matches(draft))
    draft.automaticReceive = true
    #expect(verification.matches(draft))
    draft.address = "https://other.local:43871"
    #expect(!verification.matches(draft))
    draft.address = "https://peer.local:43871"
    draft.fingerprint = String(repeating: "b", count: 64)
    #expect(!verification.matches(draft))
    verification.reset()
    draft.fingerprint = peerFingerprint
    #expect(!verification.matches(draft))
}

@Test func comparisonRequiresCompleteValidIdentityAndAllowsEquivalentFormatting() {
    let draft = PeerConnectionDraft(address: "https://peer.local:43871/", fingerprint: peerFingerprint)
    var verification = PeerVerification()
    verification.confirm(draft)
    let formatted = PeerConnectionDraft(address: " HTTPS://peer.local:43871\n", fingerprint: Array(repeating: "AA", count: 32).joined(separator: ":"))
    #expect(verification.matches(formatted))
    verification.confirm(PeerConnectionDraft(address: draft.address, fingerprint: "aa"))
    #expect(!verification.matches(draft))
}

@Test func historyRemovalCapturesOnlyKnownFinishedStatesAndDistinctIDs() throws {
    let base = try experienceTransfer()
    var running = base; running.id = "active"; running.state = "running"
    var failed = base; failed.id = "failed"; failed.state = "failed"
    var cancelled = base; cancelled.id = "cancelled"; cancelled.state = "cancelled"
    var unknown = base; unknown.id = "future"; unknown.state = "new-state"
    let captured = TransferHistorySelection.removableIDs(in: [base, running, failed, base, cancelled, unknown])
    #expect(captured == ["transfer", "failed", "cancelled"])
    running.state = "completed"
    #expect(captured == ["transfer", "failed", "cancelled"])
    #expect(TransferHistorySelection.removableIDs(in: [running]) == ["active"])
    for state in ["offered", "queued", "paused", "reconnecting", "verifying", "checkingDuplicate", "waitingDuplicate", "ready", "delivered", ""] {
        var entry = base; entry.state = state
        #expect(!entry.canRemoveFromHistory)
    }
}

@Test func mutationRefreshDiscardsEarlierPollSuccessErrorAndCompletion() throws {
    var gate = StatusRefreshGate()
    let oldRequest = gate.begin()
    let oldPoll = try #require(oldRequest)
    let blocked = gate.begin()
    #expect(blocked == nil)
    let forcedRequest = gate.begin(force: true)
    let afterRemoval = try #require(forcedRequest)
    #expect(!gate.accepts(oldPoll))
    gate.finish(oldPoll)
    #expect(gate.accepts(afterRemoval))
    let stillBlocked = gate.begin()
    #expect(stillBlocked == nil)
    gate.finish(afterRemoval)
    let laterRequest = gate.begin()
    let later = try #require(laterRequest)
    #expect(!gate.accepts(afterRemoval))
    #expect(gate.accepts(later))
    gate.invalidate()
    #expect(!gate.accepts(later))
    let next = gate.begin()
    #expect(next != nil)
}

@Test func receivedDisplayEntriesPreserveFolderRootsAndExcludeSkippedPayload() throws {
    let files = #"[{"id":"album","path":"Album","size":0,"directory":true,"received":0,"completed":true},{"id":"child","path":"Album/photo.png","size":3,"received":3,"completed":true},{"id":"loose","path":"notes.txt","destinationPath":"Documents/notes.txt","size":3,"received":3,"completed":true},{"id":"skipped","path":"duplicate.txt","size":3,"received":0,"completed":true,"skipped":true}]"#
    let transfer = try experienceTransfer(files: files)
    #expect(transfer.receivedResultEntries.map(\.id) == ["album", "loose"])
    #expect(transfer.receivedResultEntries[1].destinationPath == "Documents/notes.txt")
    var sending = transfer; sending.direction = "send"
    #expect(sending.receivedResultEntries.isEmpty)
    var incomplete = transfer; incomplete.state = "running"
    #expect(incomplete.receivedResultEntries.isEmpty)
}

@Test func allSkippedTransferDoesNotOfferAnEmptyWrapperAsReceivedContent() throws {
    let files = #"[{"id":"album","path":"Album","size":0,"directory":true,"received":0,"completed":true},{"id":"skip","path":"Album/file.txt","size":3,"received":0,"completed":true,"skipped":true}]"#
    #expect(try experienceTransfer(files: files).receivedResultEntries.isEmpty)
    let emptyFolder = #"[{"id":"empty","path":"Empty","size":0,"directory":true,"received":0,"completed":true}]"#
    #expect(try experienceTransfer(files: emptyFolder).receivedResultEntries.map(\.id) == ["empty"])
}

@Test func receivedDisplayEntriesUsePublishedDestinationsAndExactFolderBoundaries() throws {
    let files = #"[{"id":"album","path":"Album","size":0,"directory":true,"received":0,"completed":true},{"id":"child","path":"Album/photo.png","size":3,"received":3,"completed":true},{"id":"sorted","path":"Album/notes.pdf","destinationPath":"Documents/notes.pdf","size":3,"received":3,"completed":true},{"id":"prefix","path":"Album-copy.txt","size":3,"received":3,"completed":true},{"id":"moved-inside","path":"loose.txt","destinationPath":"Album/loose.txt","size":3,"received":3,"completed":true}]"#
    #expect(try experienceTransfer(files: files).receivedResultEntries.map(\.id) == ["album", "sorted", "prefix"])
}

@Test func acceptingAndTrustingRequiresOfferedIncomingWorkAndItsCurrentPairedSender() throws {
    var transfer = try experienceTransfer(); transfer.state = "offered"
    var device = try experienceDevice()
    #expect(transfer.canAcceptIncoming)
    #expect(!transfer.canAcceptAndTrust(device: device))
    device.paired = true
    #expect(transfer.canAcceptAndTrust(device: device))
    device.canReceive = false
    #expect(transfer.canAcceptAndTrust(device: device))
    device.canSend = false
    #expect(!transfer.canAcceptAndTrust(device: device))
    device.canSend = true; device.id = "other"
    #expect(!transfer.canAcceptAndTrust(device: device))
    for state in ["paused", "running", "failed", "completed", "cancelled"] {
        transfer.state = state
        #expect(!transfer.canAcceptIncoming)
        #expect(!transfer.canAcceptAndTrust(device: device))
    }
}

@Test func privateReceivedDTOSelectsOnlyTheRequestedValidatedItem() throws {
    let encoded = Data(#"{"directory":"/Users/test/Downloads/AirLink/received","items":[{"fileId":"one","path":"/Users/test/Downloads/AirLink/received/a.txt","directory":false},{"fileId":"two","path":"/Users/test/Downloads/AirLink/received/Album","directory":true}]}"#.utf8)
    let result = try JSONDecoder().decode(ReceivedTransfer.self, from: encoded)
    #expect(result.singleItem == nil)
    #expect(result.item(fileID: "one")?.directory == false)
    #expect(result.item(fileID: "two")?.directory == true)
    #expect(result.item(fileID: "skipped-or-missing") == nil)
    let single = try JSONDecoder().decode(ReceivedTransfer.self, from: Data(#"{"directory":"/tmp/received","items":[{"fileId":"one","path":"/tmp/received/a.txt","directory":false}]}"#.utf8))
    #expect(single.singleItem?.fileId == "one")
}
