import Foundation

/// Private JSON-lines transport. The helper has no network administrator endpoint.
public actor DaemonClient {
    private var process: Process?
    private var input: FileHandle?
    private var output: FileHandle?
    private var diagnostics: FileHandle?
    private var reader: Task<Void, Never>?
    private var buffer = Data()
    private var pending: [String: CheckedContinuation<Data, Error>] = [:]

    public init() {}

    public func start(executable: URL, dataDirectory: URL, receiveDirectory: URL? = nil, disableDiscovery: Bool = false) throws {
        guard process == nil else { return }
        let task = Process()
        let stdin = Pipe(), stdout = Pipe(), stderr = Pipe()
        task.executableURL = executable
        task.arguments = ["--stdio", "--data-dir", dataDirectory.path]
        if let receiveDirectory { task.arguments?.append(contentsOf: ["--receive-dir", receiveDirectory.path]) }
        if disableDiscovery { task.arguments?.append("--no-discovery") }
        task.standardInput = stdin; task.standardOutput = stdout; task.standardError = stderr
        // Drain diagnostic output so a full pipe can never block a transfer.
        stderr.fileHandleForReading.readabilityHandler = { handle in _ = handle.availableData }
        task.terminationHandler = { [weak self] _ in Task { await self?.closed() } }
        do { try task.run() } catch {
            stderr.fileHandleForReading.readabilityHandler = nil
            throw error
        }
        process = task; input = stdin.fileHandleForWriting; output = stdout.fileHandleForReading; diagnostics = stderr.fileHandleForReading
        let outputHandle = stdout.fileHandleForReading
        // One reader preserves byte order even if the helper splits a JSON line.
        reader = Task { [weak self] in
            do {
                for try await line in outputHandle.bytes.lines {
                    guard !Task.isCancelled else { return }
                    await self?.consume(Data((line + "\n").utf8))
                }
            } catch { await self?.closed() }
        }
    }

    public func request(_ method: String, params: Data? = nil) async throws -> Data {
        guard let process, process.isRunning, let input else { throw NativeError.unavailable("AirLink helper is not running.") }
        let id = UUID().uuidString
        var object: [String: Any] = ["id": id, "method": method]
        if let params { object["params"] = try JSONSerialization.jsonObject(with: params) }
        var line = try JSONSerialization.data(withJSONObject: object)
        line.append(10)
        return try await withCheckedThrowingContinuation { continuation in
            pending[id] = continuation
            do { try input.write(contentsOf: line) } catch {
                pending.removeValue(forKey: id)?.resume(throwing: error)
            }
            if method != "duplicate.resolve" {
                Task { [weak self] in
                    try? await Task.sleep(for: .seconds(20))
                    await self?.timeout(id)
                }
            }
        }
    }

    public func stop() async {
        let stopping = process
        if let stopping, stopping.isRunning { _ = try? await request("shutdown") }
        reader?.cancel(); reader = nil
        try? input?.close()
        input = nil
        if let stopping {
            // The ACK precedes the helper's deferred checkpoint/lock cleanup.
            // Wait off the actor and main thread before allowing app termination.
            await Task.detached(priority: .utility) { stopping.waitUntilExit() }.value
        }
        diagnostics?.readabilityHandler = nil
    }

    private func consume(_ bytes: Data) {
        guard !bytes.isEmpty else { return }
        buffer.append(bytes)
        while let end = buffer.firstIndex(of: 10) {
            let line = buffer.subdata(in: 0..<end)
            buffer.removeSubrange(0...end)
            guard let object = try? JSONSerialization.jsonObject(with: line) as? [String: Any],
                  let id = object["id"] as? String, let continuation = pending.removeValue(forKey: id) else { continue }
            if let error = object["error"] as? String, !error.isEmpty {
                continuation.resume(throwing: NativeError.unavailable(error))
            } else if let error = object["error"] as? [String: Any] {
                continuation.resume(throwing: NativeError.unavailable(error["message"] as? String ?? "Helper error"))
            } else {
                do {
                    let result = object["result"] ?? NSNull()
                    continuation.resume(returning: try JSONSerialization.data(withJSONObject: result, options: .fragmentsAllowed))
                } catch { continuation.resume(throwing: error) }
            }
        }
    }
    private func timeout(_ id: String) {
        pending.removeValue(forKey: id)?.resume(throwing: NativeError.unavailable("AirLink helper did not respond in time."))
    }
    private func closed() {
        let waiting = pending.values; pending.removeAll()
        for continuation in waiting { continuation.resume(throwing: NativeError.unavailable("AirLink helper stopped.")) }
        reader?.cancel(); reader = nil; diagnostics?.readabilityHandler = nil
        process = nil; input = nil
    }
}
