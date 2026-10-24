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
        *'"method":"shutdown"'*) printf '{"id":"%s","result":{}}\n' "$id"; exit 0 ;;
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
}
