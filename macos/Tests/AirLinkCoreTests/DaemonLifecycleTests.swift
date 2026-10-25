import Darwin
import Foundation
import Testing
@testable import AirLinkCore

private func lifecycleDirectory() throws -> URL {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent("AirLink-lifecycle-" + UUID().uuidString, isDirectory: true)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    return directory
}
private func lifecycleHelper(_ directory: URL, _ script: String) throws -> URL {
    let executable = directory.appendingPathComponent("fake-helper")
    try Data(("#!/bin/sh\n" + script).utf8).write(to: executable)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: executable.path)
    return executable
}
private let lifecycleReplies = #"""
while IFS= read -r line; do
  id=$(printf '%s\n' "$line" | /usr/bin/sed -nE 's/.*"id":"([^"]+)".*/\1/p')
  printf '{"id":"%s","result":{"ok":true}}\n' "$id"
  case "$line" in *'"method":"shutdown"'*) exit 0 ;; esac
done
"""#

@Test func failedHelperLaunchAndUnexpectedExitCanRetryWithoutChangingData() async throws {
    let directory = try lifecycleDirectory()
    defer { try? FileManager.default.removeItem(at: directory) }
    let preserved = directory.appendingPathComponent("preserved-state")
    try Data("identity and saved settings".utf8).write(to: preserved)
    let client = DaemonClient()
    do {
        try await client.start(executable: directory.appendingPathComponent("missing-helper"), dataDirectory: directory)
        Issue.record("A missing executable should fail launch.")
    } catch {}
    let executable = try lifecycleHelper(directory, #"""
    if [ ! -f "$3/attempted" ]; then
      touch "$3/attempted"
      IFS= read -r line
      printf '%s\n' 'Selected receive folder is unavailable.' >&2
      exit 23
    fi
    printf '%s\n' "$@" > "$3/arguments"
    """# + "\n" + lifecycleReplies)
    try await client.start(executable: executable, dataDirectory: directory)
    do {
        _ = try await client.request("status")
        Issue.record("A helper that exits should fail the pending request.")
    } catch {
        #expect(error.localizedDescription.contains("Selected receive folder is unavailable."))
    }
    #expect(await client.stop())
    try await client.start(executable: executable, dataDirectory: directory)
    let response = try await client.request("status")
    #expect((try JSONSerialization.jsonObject(with: response) as? [String: Bool])?["ok"] == true)
    let arguments = try String(contentsOf: directory.appendingPathComponent("arguments"), encoding: .utf8).split(separator: "\n").map(String.init)
    #expect(arguments == ["--stdio", "--data-dir", directory.path])
    #expect(try String(contentsOf: preserved, encoding: .utf8) == "identity and saved settings")
    #expect(await client.stop())
}

@Test(arguments: [false, true])
func helperIgnoringShutdownAndTerminateCannotHangQuit(acknowledges: Bool) async throws {
    let directory = try lifecycleDirectory()
    defer { try? FileManager.default.removeItem(at: directory) }
    let acknowledgement = acknowledges ? #"printf '{"id":"%s","result":{}}\n' "$id""# : ":"
    let executable = try lifecycleHelper(directory, #"""
    trap '' TERM
    printf '%s\n' "$$" > "$3/pid"
    while IFS= read -r line; do
      id=$(printf '%s\n' "$line" | /usr/bin/sed -nE 's/.*"id":"([^"]+)".*/\1/p')
      case "$line" in
        *'"method":"shutdown"'*)
          printf received > "$3/shutdown-requested"
          ACKNOWLEDGEMENT
          while :; do :; done ;;
        *) printf '{"id":"%s","result":{"ok":true}}\n' "$id" ;;
      esac
    done
    """#.replacingOccurrences(of: "ACKNOWLEDGEMENT", with: acknowledgement))
    let client = DaemonClient(shutdownPolicy: HelperShutdownPolicy(gracefulSeconds: 0.12, terminateSeconds: 0.12, killSeconds: 0.5))
    try await client.start(executable: executable, dataDirectory: directory)
    _ = try await client.request("status")
    let pid = try #require(Int32(String(contentsOf: directory.appendingPathComponent("pid"), encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)))
    let started = ContinuousClock.now
    async let first = client.stop()
    async let repeated = client.stop()
    let results = await (first, repeated)
    #expect(results.0 && results.1)
    #expect(started.duration(to: .now) < .seconds(2))
    #expect(FileManager.default.fileExists(atPath: directory.appendingPathComponent("shutdown-requested").path))
    #expect(Darwin.kill(pid, 0) == -1)
}

@Test func immediateStartupFailureReportsDiagnosticBeforeAnyRequestIsRead() async throws {
    let directory = try lifecycleDirectory()
    defer { try? FileManager.default.removeItem(at: directory) }
    let executable = try lifecycleHelper(directory, #"""
    printf '%s\n' 'AirLink is already running with this data directory.' >&2
    exit 17
    """#)
    for _ in 0..<5 {
        let client = DaemonClient()
        try await client.start(executable: executable, dataDirectory: directory)
        do {
            _ = try await client.request("status")
            Issue.record("An immediate helper startup failure must fail the request.")
        } catch {
            #expect(error.localizedDescription.contains("AirLink is already running with this data directory."))
        }
        #expect(await client.stop())
    }
}

@Test func blockedHelperInputDoesNotBlockBoundedShutdownOrLeavePendingRequest() async throws {
    let directory = try lifecycleDirectory()
    defer { try? FileManager.default.removeItem(at: directory) }
    let executable = try lifecycleHelper(directory, #"""
    trap '' TERM
    IFS= read -r line
    id=$(printf '%s\n' "$line" | /usr/bin/sed -nE 's/.*"id":"([^"]+)".*/\1/p')
    printf '{"id":"%s","result":{"ok":true}}\n' "$id"
    while :; do :; done
    """#)
    let client = DaemonClient(shutdownPolicy: HelperShutdownPolicy(gracefulSeconds: 0.12, terminateSeconds: 0.12, killSeconds: 0.5))
    try await client.start(executable: executable, dataDirectory: directory)
    _ = try await client.request("status")
    let params = try JSONEncoder().encode(["payload": String(repeating: "x", count: 2 * 1024 * 1024)])
    let blocked = Task { try await client.request("duplicate.resolve", params: params) }
    try await Task.sleep(for: .milliseconds(50))
    let started = ContinuousClock.now
    #expect(await client.stop())
    #expect(started.duration(to: .now) < .seconds(2))
    do {
        _ = try await blocked.value
        Issue.record("Stopping should fail a pending request even when its write is blocked.")
    } catch {
        #expect(error.localizedDescription.contains("stopping"))
    }
}

@Test func latePreviousProcessCallbacksCannotCloseRestartedConnection() async throws {
    let directory = try lifecycleDirectory()
    defer { try? FileManager.default.removeItem(at: directory) }
    let executable = try lifecycleHelper(directory, lifecycleReplies)
    let client = DaemonClient()
    for _ in 0..<3 {
        try await client.start(executable: executable, dataDirectory: directory)
        _ = try await client.request("status")
        #expect(await client.stop())
    }
    try await client.start(executable: executable, dataDirectory: directory)
    _ = try await client.request("status")
    // Previous termination callbacks drain asynchronously for 250 ms. Their
    // continuation must not fail requests or clear the new generation's pipes.
    try await Task.sleep(for: .milliseconds(400))
    let response = try await client.request("status")
    #expect((try JSONSerialization.jsonObject(with: response) as? [String: Bool])?["ok"] == true)
    #expect(await client.stop())
}

@Test func independentIdleHelpersCannotStarveEachOthersReplies() async throws {
    let firstDirectory = try lifecycleDirectory(), secondDirectory = try lifecycleDirectory()
    defer {
        try? FileManager.default.removeItem(at: firstDirectory)
        try? FileManager.default.removeItem(at: secondDirectory)
    }
    let firstExecutable = try lifecycleHelper(firstDirectory, lifecycleReplies)
    let secondExecutable = try lifecycleHelper(secondDirectory, lifecycleReplies)
    let first = DaemonClient(), second = DaemonClient()
    do {
        try await first.start(executable: firstExecutable, dataDirectory: firstDirectory)
        _ = try await first.request("status")
        // Leave the first pipe open and idle before starting the second reader.
        try await Task.sleep(for: .milliseconds(30))
        try await second.start(executable: secondExecutable, dataDirectory: secondDirectory)
        for _ in 0..<10 {
            async let one = first.request("status")
            async let two = second.request("status")
            for response in try await [one, two] {
                #expect((try JSONSerialization.jsonObject(with: response) as? [String: Bool])?["ok"] == true)
            }
        }
    } catch {
        _ = await first.stop(); _ = await second.stop()
        throw error
    }
    #expect(await first.stop()); #expect(await second.stop())
}

@Test(arguments: [false, true])
func malformedOrOversizedHelperRepliesFailWithoutWaitingForRequestTimeout(oversized: Bool) async throws {
    let directory = try lifecycleDirectory()
    defer { try? FileManager.default.removeItem(at: directory) }
    let response = oversized
        ? #"/usr/bin/head -c 16777217 /dev/zero | /usr/bin/tr '\000' x"#
        : #"printf 'not-json\n'"#
    let executable = try lifecycleHelper(directory, "IFS= read -r line\n" + response + "\n" + lifecycleReplies)
    let client = DaemonClient()
    try await client.start(executable: executable, dataDirectory: directory)
    let started = ContinuousClock.now
    do {
        _ = try await client.request("status")
        Issue.record("Invalid helper output should fail its pending request.")
    } catch {
        #expect(error.localizedDescription.contains(oversized ? "safe size limit" : "invalid response"))
        #expect(started.duration(to: .now) < .seconds(5))
    }
    #expect(await client.stop())
}

@Test func finalReplyIsDrainedBeforeCleanHelperExit() async throws {
    let directory = try lifecycleDirectory()
    defer { try? FileManager.default.removeItem(at: directory) }
    let executable = try lifecycleHelper(directory, #"""
    IFS= read -r line
    id=$(printf '%s\n' "$line" | /usr/bin/sed -nE 's/.*"id":"([^"]+)".*/\1/p')
    printf '{"id":"%s","result":{"ok":true}}\n' "$id"
    exit 0
    """#)
    let client = DaemonClient()
    try await client.start(executable: executable, dataDirectory: directory)
    let response = try await client.request("status")
    #expect((try JSONSerialization.jsonObject(with: response) as? [String: Bool])?["ok"] == true)
    #expect(await client.stop())
}

@Test func helperDiagnosticsAreBoundedAndRedactKeysAndCredentials() {
    let tail = HelperDiagnosticTail()
    tail.append(Data((0..<2000).map { "line \($0)\n" }.joined().utf8))
    tail.append(Data("-----BEGIN PRI".utf8))
    tail.append(Data("VATE KEY-----\nprivate-key-material\n-----END PRIVATE KEY-----\n".utf8))
    tail.append(Data((String(repeating: "x", count: 10000) + "\n").utf8))
    tail.append(Data("Authorization: Bearer credential-value\ntoken=private-token password=private-password\nFolder is unavailable".utf8))
    tail.finish()
    #expect(tail.text.utf8.count <= 8192)
    #expect(!tail.text.contains("private-key-material"))
    #expect(!tail.text.contains("PRIVATE KEY"))
    #expect(!tail.text.contains("credential-value"))
    #expect(!tail.text.contains("private-token"))
    #expect(!tail.text.contains("private-password"))
    #expect(tail.text.contains("Folder is unavailable"))
    #expect(tail.text.contains("[Long helper diagnostic omitted]"))
}

@Test func normalRetryKeepsPersistedReceiveFolderUnlessUserExplicitlySelectsOne() {
    let data = URL(fileURLWithPath: "/tmp/AirLink existing state", isDirectory: true)
    let selected = URL(fileURLWithPath: "/Volumes/Selected disk/New receipts", isDirectory: true)
    #expect(HelperLaunchArguments.build(dataDirectory: data, receiveDirectory: nil, disableDiscovery: false) == ["--stdio", "--data-dir", data.path])
    #expect(HelperLaunchArguments.build(dataDirectory: data, receiveDirectory: selected, disableDiscovery: true) == ["--stdio", "--data-dir", data.path, "--receive-dir", selected.path, "--no-discovery"])
}
