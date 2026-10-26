import Foundation
import Testing
@testable import AirLinkCore

@Test func privatePipeCorrelatesResponsesAndSurfacesErrors() async throws {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let helper = directory.appendingPathComponent("fake-helper")
    let script = #"""
    #!/bin/sh
    while IFS= read -r line; do
      id="${line#*\"id\":\"}"
      id="${id%%\"*}"
      case "$line" in
        *'"method":"bad"'*) printf '{"id":"%s","error":"permission denied"}\n' "$id" ;;
        *'"method":"shutdown"'*) printf '{"id":"%s","result":{}}\n' "$id"; sleep 0.1; printf done > "$3/shutdown-complete"; exit 0 ;;
        *) printf '{"id":"%s","result":' "$id"; printf '{"ok":true}}\n' ;;
      esac
    done
    """#
    try Data(script.utf8).write(to: helper)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: helper.path)
    let client = DaemonClient()
    try await client.start(executable: helper, dataDirectory: directory)
    let response = try await client.request("status")
    #expect((try JSONSerialization.jsonObject(with: response) as? [String: Bool])?["ok"] == true)
    do {
        _ = try await client.request("bad")
        Issue.record("The helper error should be propagated")
    } catch {
        #expect(error.localizedDescription == "permission denied")
    }
    await client.stop()
    #expect(FileManager.default.fileExists(atPath: directory.appendingPathComponent("shutdown-complete").path))
}

@Test(arguments: ["duplicate.resolve", "offer", "share.create"])
func slowFilesystemRPCAllowsOutOfOrderStatusResponse(method: String) async throws {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let helper = directory.appendingPathComponent("fake-parallel-helper")
    let marker = directory.appendingPathComponent("duplicate-started")
    let release = directory.appendingPathComponent("release-duplicate")
    let script = #"""
    #!/bin/sh
    while IFS= read -r line; do
      id="${line#*\"id\":\"}"
      id="${id%%\"*}"
      case "$line" in
        *'"method":"duplicate.resolve"'*|*'"method":"offer"'*|*'"method":"share.create"'*)
          printf started > "$3/duplicate-started"
          (while [ ! -f "$3/release-duplicate" ]; do sleep 0.01; done; printf '{"id":"%s","result":{"kind":"duplicate"}}\n' "$id") &
          ;;
        *'"method":"shutdown"'*) wait; printf '{"id":"%s","result":{}}\n' "$id"; exit 0 ;;
        *) printf '{"id":"%s","result":{"kind":"status"}}\n' "$id" ;;
      esac
    done
    """#
    try Data(script.utf8).write(to: helper)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: helper.path)
    let client = DaemonClient(ordinaryRequestTimeout: 0.4)
    try await client.start(executable: helper, dataDirectory: directory)
    let slow = Task { try await client.request(method) }
    do {
        for _ in 0..<1000 {
            if FileManager.default.fileExists(atPath: marker.path) { break }
            try await Task.sleep(for: .milliseconds(1))
        }
        #expect(FileManager.default.fileExists(atPath: marker.path))
        let fast = try await client.request("status")
        #expect((try JSONSerialization.jsonObject(with: fast) as? [String: String])?["kind"] == "status")
        // Each filesystem reply must outlive the ordinary timeout while an
        // unrelated status reply is still delivered through the same pipe.
        try await Task.sleep(for: .milliseconds(650))
        try Data().write(to: release)
        let result = try await slow.value
        #expect((try JSONSerialization.jsonObject(with: result) as? [String: String])?["kind"] == "duplicate")
        await client.stop()
    } catch {
        try? Data().write(to: release)
        _ = try? await slow.value
        await client.stop()
        throw error
    }
}

@Test func ordinaryRequestTimeoutLeavesTheLiveHelperUsable() async throws {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let helper = directory.appendingPathComponent("fake-timeout-helper")
    let script = #"""
    #!/bin/sh
    while IFS= read -r line; do
      id="${line#*\"id\":\"}"
      id="${id%%\"*}"
      case "$line" in
        *'"method":"unanswered"'*) : ;;
        *'"method":"shutdown"'*) printf '{"id":"%s","result":{}}\n' "$id"; exit 0 ;;
        *) printf '{"id":"%s","result":{"ok":true}}\n' "$id" ;;
      esac
    done
    """#
    try Data(script.utf8).write(to: helper)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: helper.path)
    // Leave scheduling headroom when the background suite runs alongside
    // archive/signature work. The unanswered call still exercises a real timeout.
    let client = DaemonClient(ordinaryRequestTimeout: 1.5)
    try await client.start(executable: helper, dataDirectory: directory)
    do {
        _ = try await client.request("status")
        do {
            _ = try await client.request("unanswered")
            Issue.record("An ordinary unanswered request must time out.")
        } catch { #expect(error.localizedDescription.contains("did not respond in time")) }
        let response = try await client.request("status")
        #expect((try JSONSerialization.jsonObject(with: response) as? [String: Bool])?["ok"] == true)
        #expect(await client.stop())
    } catch {
        _ = await client.stop()
        throw error
    }
}
