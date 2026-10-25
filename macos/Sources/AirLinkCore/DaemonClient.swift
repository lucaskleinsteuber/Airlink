import Foundation
import Darwin

enum HelperLaunchArguments {
    static func build(dataDirectory: URL, receiveDirectory: URL?, disableDiscovery: Bool) -> [String] {
        var arguments = ["--stdio", "--data-dir", dataDirectory.path]
        if let receiveDirectory { arguments.append(contentsOf: ["--receive-dir", receiveDirectory.path]) }
        if disableDiscovery { arguments.append("--no-discovery") }
        return arguments
    }
}

public struct HelperShutdownPolicy: Sendable {
    public var gracefulSeconds: TimeInterval
    public var terminateSeconds: TimeInterval
    public var killSeconds: TimeInterval
    public init(gracefulSeconds: TimeInterval = 10, terminateSeconds: TimeInterval = 3, killSeconds: TimeInterval = 2) {
        self.gracefulSeconds = max(0, gracefulSeconds)
        self.terminateSeconds = max(0, terminateSeconds)
        self.killSeconds = max(0, killSeconds)
    }
}

/// Private JSON-lines transport. Each launch owns its pipes and callback generation.
public actor DaemonClient {
    private var process: Process?
    private var input: FileHandle?
    private var output: FileHandle?
    private var diagnostics: FileHandle?
    private var diagnosticTail: HelperDiagnosticTail?
    private var reader: HelperReplyReader?
    private var replyBuffer = Data()
    private var generation: UUID?
    private var channelClosed = false
    private var stopping = false
    private var failure: String?
    private var pending: [String: CheckedContinuation<Data, Error>] = [:]
    private let writer = DispatchQueue(label: "com.lucasfd3.airlink.helper-input", qos: .userInitiated)
    private let shutdownPolicy: HelperShutdownPolicy
    private let ordinaryRequestTimeout: TimeInterval

    public init(shutdownPolicy: HelperShutdownPolicy = HelperShutdownPolicy(), ordinaryRequestTimeout: TimeInterval = 20) {
        self.shutdownPolicy = shutdownPolicy
        self.ordinaryRequestTimeout = ordinaryRequestTimeout.isFinite && ordinaryRequestTimeout > 0 ? ordinaryRequestTimeout : 20
    }

    public func start(executable: URL, dataDirectory: URL, receiveDirectory: URL? = nil, disableDiscovery: Bool = false) throws {
        guard !stopping else { throw NativeError.unavailable("AirLink helper is stopping.") }
        guard process == nil else { throw NativeError.unavailable("Stop the previous AirLink helper before restarting it.") }
        let session = UUID(), task = Process()
        let stdin = Pipe(), stdout = Pipe(), stderr = Pipe(), tail = HelperDiagnosticTail()
        task.executableURL = executable
        task.arguments = HelperLaunchArguments.build(dataDirectory: dataDirectory, receiveDirectory: receiveDirectory, disableDiscovery: disableDiscovery)
        task.standardInput = stdin; task.standardOutput = stdout; task.standardError = stderr
        let diagnosticFD = stderr.fileHandleForReading.fileDescriptor
        let flags = fcntl(diagnosticFD, F_GETFL)
        let outputFD = stdout.fileHandleForReading.fileDescriptor
        let outputFlags = fcntl(outputFD, F_GETFL)
        guard flags != -1, fcntl(diagnosticFD, F_SETFL, flags | O_NONBLOCK) != -1,
              outputFlags != -1, fcntl(outputFD, F_SETFL, outputFlags | O_NONBLOCK) != -1,
              fcntl(stdin.fileHandleForWriting.fileDescriptor, F_SETNOSIGPIPE, 1) != -1 else {
            throw NSError(domain: NSPOSIXErrorDomain, code: Int(errno))
        }
        // Drain without queuing one actor task per diagnostic chunk during a flood.
        stderr.fileHandleForReading.readabilityHandler = { handle in if tail.drain(diagnosticFD) { handle.readabilityHandler = nil } }
        task.terminationHandler = { [weak self] ended in
            Task { await self?.terminated(session, status: ended.terminationStatus) }
        }
        do { try task.run() } catch {
            stderr.fileHandleForReading.readabilityHandler = nil
            task.terminationHandler = nil
            throw error
        }
        generation = session; failure = nil; channelClosed = false; replyBuffer = Data()
        process = task; input = stdin.fileHandleForWriting; output = stdout.fileHandleForReading
        diagnostics = stderr.fileHandleForReading; diagnosticTail = tail
        reader = HelperReplyReader(handle: stdout.fileHandleForReading) { [weak self] data, ended in
            await self?.consumeChunk(data, ended: ended, session: session)
        }
        reader?.start()
    }

    public func request(_ method: String, params: Data? = nil) async throws -> Data {
        if !stopping, let process, !process.isRunning, failure == nil {
            failure = stoppedMessage(status: process.terminationStatus)
            channelClosed = true
            failPending(failure!)
        }
        guard !stopping, !channelClosed, let process, process.isRunning, let input, let session = generation else {
            throw NativeError.unavailable(failure ?? "AirLink helper is not running.")
        }
        let id = UUID().uuidString
        var object: [String: Any] = ["id": id, "method": method]
        if let params { object["params"] = try JSONSerialization.jsonObject(with: params) }
        var line = try JSONSerialization.data(withJSONObject: object); line.append(10)
        let payload = line
        return try await withCheckedThrowingContinuation { continuation in
            pending[id] = continuation
            // A full input pipe must not block actor access to status or shutdown.
            writer.async { [weak self] in
                do { try input.write(contentsOf: payload) }
                catch { Task { await self?.writeFailed(id, session: session) } }
            }
            if !["duplicate.resolve", "offer", "share.create"].contains(method) {
                let timeout = ordinaryRequestTimeout
                Task { [weak self] in
                    try? await Task.sleep(for: .seconds(timeout))
                    await self?.timeout(id, session: session)
                }
            }
        }
    }

    @discardableResult public func stop() async -> Bool {
        if stopping {
            while stopping { try? await Task.sleep(for: .milliseconds(20)) }
            return process == nil
        }
        guard let task = process, let session = generation else { return true }
        stopping = true
        failPending("AirLink helper is stopping.")
        if let input {
            let shutdown = Data("{\"id\":\"shutdown-\(session.uuidString)\",\"method\":\"shutdown\"}\n".utf8)
            writer.async {
                try? input.write(contentsOf: shutdown)
                try? input.close()
            }
        }
        // Normal exit includes the helper's deferred checkpoint and lock cleanup.
        if !(await waitForExit(task, seconds: shutdownPolicy.gracefulSeconds)) {
            if task.isRunning { task.terminate() }
            if !(await waitForExit(task, seconds: shutdownPolicy.terminateSeconds)) {
                if task.isRunning { _ = Darwin.kill(task.processIdentifier, SIGKILL) }
                _ = await waitForExit(task, seconds: shutdownPolicy.killSeconds)
            }
        }
        let exited = !task.isRunning
        if generation == session {
            if exited { clear(session) }
            else { failure = "AirLink helper could not be stopped. Try quitting again."; channelClosed = true }
        }
        stopping = false
        return exited
    }

    private func waitForExit(_ task: Process, seconds: TimeInterval) async -> Bool {
        let deadline = ContinuousClock.now.advanced(by: .seconds(seconds))
        while task.isRunning && ContinuousClock.now < deadline {
            try? await Task.sleep(for: .milliseconds(20))
        }
        return !task.isRunning
    }

    private func consumeChunk(_ data: Data, ended: Bool, session: UUID) {
        guard generation == session, !channelClosed else { return }
        var start = data.startIndex
        for index in data.indices where data[index] == 10 {
            guard appendReply(data[start..<index], session: session) else { return }
            let line = replyBuffer; replyBuffer = Data()
            consume(line, session: session)
            guard !channelClosed else { return }
            start = data.index(after: index)
        }
        guard appendReply(data[start..<data.endIndex], session: session) else { return }
        if ended {
            // JSON-lines requires a final newline; never silently parse a partial response.
            if !replyBuffer.isEmpty { invalidReply(session, message: "AirLink helper returned an incomplete response.") }
            else { streamEnded(session) }
        }
    }
    private func appendReply(_ segment: Data.SubSequence, session: UUID) -> Bool {
        guard replyBuffer.count + segment.count <= 16 * 1024 * 1024 else {
            invalidReply(session, message: "AirLink helper response exceeded the safe size limit.")
            return false
        }
        replyBuffer.append(segment)
        return true
    }
    private func invalidReply(_ session: UUID, message: String) {
        guard generation == session else { return }
        failure = message; channelClosed = true; replyBuffer = Data()
        failPending(message); reader?.stop()
    }
    private func consume(_ line: Data, session: UUID) {
        guard generation == session, !channelClosed else { return }
        guard let object = try? JSONSerialization.jsonObject(with: line) as? [String: Any], let id = object["id"] as? String else {
            invalidReply(session, message: "AirLink helper returned an invalid response.")
            return
        }
        guard let continuation = pending.removeValue(forKey: id) else { return }
        if let error = object["error"] as? String, !error.isEmpty {
            continuation.resume(throwing: NativeError.unavailable(error))
        } else if let error = object["error"] as? [String: Any] {
            continuation.resume(throwing: NativeError.unavailable(error["message"] as? String ?? "Helper error"))
        } else {
            do {
                continuation.resume(returning: try JSONSerialization.data(withJSONObject: object["result"] ?? NSNull(), options: .fragmentsAllowed))
            } catch { continuation.resume(throwing: error) }
        }
    }
    private func writeFailed(_ id: String, session: UUID) {
        guard generation == session, pending[id] != nil else { return }
        failure = stoppedMessage(status: process?.isRunning == false ? process?.terminationStatus : nil)
        channelClosed = true
        failPending(failure!)
    }
    private func timeout(_ id: String, session: UUID) {
        guard generation == session else { return }
        pending.removeValue(forKey: id)?.resume(throwing: NativeError.unavailable("AirLink helper did not respond in time."))
    }
    private func terminated(_ session: UUID, status: Int32) async {
        guard generation == session else { return }
        // Drain replies written just before exit; descendants cannot hold failure open.
        try? await Task.sleep(for: .milliseconds(250))
        guard generation == session else { return }
        if !channelClosed { failure = stoppedMessage(status: status); channelClosed = true; failPending(failure!) }
        if !stopping { clear(session, preserveFailure: true) }
    }
    private func streamEnded(_ session: UUID) {
        guard generation == session else { return }
        failure = stoppedMessage(status: process?.isRunning == false ? process?.terminationStatus : nil)
        channelClosed = true; failPending(failure!)
        // Closed stdout with a live process still needs the bounded stop path.
        if process?.isRunning == false && !stopping { clear(session, preserveFailure: true) }
    }
    private func stoppedMessage(status: Int32?) -> String {
        let reason = status.map { "AirLink helper stopped (exit \($0))." } ?? "AirLink helper connection closed."
        if let diagnostics { diagnosticTail?.drain(diagnostics.fileDescriptor) }
        diagnosticTail?.finish()
        let detail = diagnosticTail?.text ?? ""
        return detail.isEmpty ? reason : reason + "\n" + detail
    }
    private func failPending(_ message: String) {
        let waiting = pending.values; pending.removeAll()
        for continuation in waiting { continuation.resume(throwing: NativeError.unavailable(message)) }
    }
    private func clear(_ session: UUID, preserveFailure: Bool = false) {
        guard generation == session else { return }
        reader?.stop(); reader = nil; replyBuffer = Data()
        process?.terminationHandler = nil
        diagnostics?.readabilityHandler = nil
        // The read source closes its own descriptor after queued callbacks finish.
        try? input?.close()
        if let diagnostics { diagnosticTail?.close(diagnostics) }
        process = nil; input = nil; output = nil; diagnostics = nil; diagnosticTail = nil
        generation = nil; channelClosed = false
        if !preserveFailure { failure = nil }
    }
}

/// One nonblocking stdout reader per helper. An idle pipe never occupies a Swift
/// executor or Foundation's shared asynchronous-byte reader. A single in-flight
/// batch provides bounded buffering and ordered delivery to the owning actor.
private final class HelperReplyReader: @unchecked Sendable {
    private let queue = DispatchQueue(label: "com.lucasfd3.airlink.helper-output", qos: .userInitiated)
    private let source: DispatchSourceRead
    private let descriptor: Int32
    private let receive: @Sendable (Data, Bool) async -> Void
    private var closed = false
    private var suspended = false

    init(handle: FileHandle, receive: @escaping @Sendable (Data, Bool) async -> Void) {
        descriptor = handle.fileDescriptor
        self.receive = receive
        source = DispatchSource.makeReadSource(fileDescriptor: descriptor, queue: queue)
        source.setEventHandler { [weak self] in self?.readAvailable() }
        source.setCancelHandler { try? handle.close() }
    }
    func start() { source.resume() }
    func stop() { queue.async { [self] in closeOnQueue() } }

    private func closeOnQueue() {
        guard !closed else { return }
        closed = true
        source.cancel()
        if suspended { suspended = false; source.resume() }
    }
    private func readAvailable() {
        guard !closed else { return }
        var batch = Data(), bytes = [UInt8](repeating: 0, count: 4096)
        var ended = false
        for _ in 0..<16 {
            let count = Darwin.read(descriptor, &bytes, bytes.count)
            if count > 0 { batch.append(contentsOf: bytes.prefix(count)) }
            else if count == 0 { ended = true; break }
            else if errno == EINTR { continue }
            else { ended = errno != EAGAIN && errno != EWOULDBLOCK; break }
        }
        guard !batch.isEmpty || ended else { return }
        suspended = true; source.suspend()
        Task { [self, batch, ended] in
            await receive(batch, ended)
            queue.async { [self] in
                if ended { closeOnQueue() }
                else if suspended && !closed { suspended = false; source.resume() }
            }
        }
    }
}

/// Thread-safe bounded diagnostic collection. Never retain PEM private keys.
final class HelperDiagnosticTail: @unchecked Sendable {
    private let lock = NSLock()
    private var lines: [String] = []
    private var partial = Data()
    private var oversized = false
    private var privateKey = false
    private var descriptorClosed = false
    func append(_ data: Data) {
        lock.lock(); defer { lock.unlock() }
        appendLocked(data)
    }
    @discardableResult func drain(_ descriptor: Int32) -> Bool {
        lock.lock(); defer { lock.unlock() }
        guard !descriptorClosed else { return true }
        var bytes = [UInt8](repeating: 0, count: 4096)
        // Nonblocking, bounded work even while a failing helper keeps writing.
        for _ in 0..<16 {
            let count = Darwin.read(descriptor, &bytes, bytes.count)
            if count == 0 { return true }
            guard count > 0 else { return false }
            appendLocked(Data(bytes.prefix(count)))
        }
        return false
    }
    func close(_ handle: FileHandle) {
        lock.lock(); defer { lock.unlock() }
        descriptorClosed = true
        try? handle.close()
    }
    private func appendLocked(_ data: Data) {
        for byte in data {
            if byte == 10 { finishLine() }
            else if partial.count < 2048 { partial.append(byte) }
            else { oversized = true }
        }
    }
    var text: String {
        lock.lock(); defer { lock.unlock() }
        return lines.joined(separator: "\n")
    }
    func finish() {
        lock.lock(); defer { lock.unlock() }
        if !partial.isEmpty { finishLine() }
    }
    private func finishLine() {
        let line = String(decoding: partial, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
        partial.removeAll(keepingCapacity: true)
        defer { oversized = false }
        if line.contains("-----BEGIN"), line.contains("PRIVATE KEY-----") { privateKey = true; return }
        if privateKey {
            if line.contains("-----END"), line.contains("PRIVATE KEY-----") { privateKey = false }
            return
        }
        guard !line.isEmpty else { return }
        let safe = oversized ? "[Long helper diagnostic omitted]" : line
            .replacingOccurrences(of: #"(?i)(bearer\s+)[a-z0-9._~+/=-]+"#, with: "$1[redacted]", options: .regularExpression)
            .replacingOccurrences(of: #"(?i)((?:token|password|secret)\s*[=:]\s*)\S+"#, with: "$1[redacted]", options: .regularExpression)
        lines.append(safe)
        while lines.joined(separator: "\n").utf8.count > 8192 { lines.removeFirst() }
    }
}
