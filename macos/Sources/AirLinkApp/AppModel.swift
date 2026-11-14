import AppKit
import AirLinkCore
import Combine
import ServiceManagement
import UniformTypeIdentifiers
import UserNotifications

enum HelperLifecycle { case stopped, starting, ready, failed, stopping }

/// Files that still need a recipient ("Wohin senden?").
struct PendingSend: Identifiable {
    let id = UUID()
    let urls: [URL]
}

/// The one sheet the main window shows; a pairing request replaces any other.
enum AppSheet: Identifiable {
    case pairing(String)
    case addDevice(byAddress: Bool)
    case help
    case send(PendingSend)
    case device(String)
    var id: String {
        switch self {
        case .pairing(let id): return "pairing-" + id
        case .addDevice: return "add"
        case .help: return "help"
        case .send(let pending): return "send-" + pending.id.uuidString
        case .device(let id): return "device-" + id
        }
    }
}

/// What a device tile shows under its name.
struct DeviceActivity: Equatable {
    var label: String
    var progress: Double?
    var busy = false
}

@MainActor final class AppModel: ObservableObject {
    static let shared = AppModel()

    @Published var status: HostStatus?
    @Published var error: String?
    @Published var engineError: String?
    @Published private(set) var lifecycle = HelperLifecycle.stopped
    @Published var sheet: AppSheet?
    @Published private(set) var preparing: Set<String> = []
    @Published private(set) var clipboardTexts: [String: String] = [:]
    @Published var registeredShortcut = Shortcut(enabled: false)
    @Published var shortcutError: String?
    @Published var shortcutRecording = false
    @Published private(set) var historyRevision = UUID()
    @Published private(set) var launchAtLogin = false
    @Published private(set) var launchNeedsApproval = false

    /// Demo mode shows sample computers without starting the service; it is
    /// used for screenshots and never touches real data or the network.
    let demo: Bool
    var shortcutController: ShortcutController?
    var openWindow: ((String) -> Void)?
    private let client = DaemonClient()
    private var fallback: Task<Void, Never>?
    private var listening: Task<Void, Never>?
    private var lifecycleGeneration = UUID()
    private var refreshing = false
    private var refreshAgain = false
    private var lastStatusData: Data?
    private var notified = Set<String>()
    private var shownPairings = Set<String>()
    private var lastShortcutAttempt: Shortcut?
    private var windowVisible = false

    init(demo: Bool = CommandLine.arguments.contains("--demo")) {
        self.demo = demo
        refreshLoginItem()
    }

    // MARK: Derived state

    var english: Bool { LanguagePreference.usesEnglish(status?.settings.language ?? "system") }
    func text(_ de: String, _ en: String) -> String { english ? en : de }
    var isReady: Bool { lifecycle == .ready && engineError == nil && status != nil }
    var setupComplete: Bool { status?.settings.setupCompleted == true }
    var version: String { status?.version ?? Bundle.main.object(forInfoDictionaryKey: "AirLinkDisplayVersion") as? String ?? Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "" }
    var transfers: [Transfer] { status?.transfers ?? [] }
    var activeTransfers: [Transfer] { transfers.filter(\.isActive) }

    var pairedDevices: [Device] {
        (status?.devices ?? []).filter { $0.paired && $0.kind == "engine" }.sorted {
            if $0.online != $1.online { return $0.online }
            let order = $0.name.localizedStandardCompare($1.name)
            return order == .orderedSame ? $0.id < $1.id : order == .orderedAscending
        }
    }
    var nearbyDevices: [Device] {
        (status?.devices ?? []).filter { !$0.paired && $0.kind == "engine" && $0.address != nil }
            .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }
    /// Active transfers first, then the newest finished ones.
    var recentTransfers: [Transfer] {
        let newer: (Transfer, Transfer) -> Bool = { $0.updatedAt == $1.updatedAt ? $0.id > $1.id : $0.updatedAt > $1.updatedAt }
        let active = transfers.filter(\.isActive).sorted(by: newer)
        let done = transfers.filter { !$0.isActive }.sorted(by: newer)
        return active + done.prefix(max(0, 5 - active.count))
    }
    var incomingRequests: [Transfer] { transfers.filter(\.canAcceptIncoming) }
    var receivedClipboard: [ClipboardMessage] {
        (status?.clipboard ?? []).filter { $0.direction == "receive" && clipboardTexts[$0.id] != nil }.sorted { $0.createdAt > $1.createdAt }
    }
    func pairing(_ id: String) -> PairingSession? { status?.pairingSessions?.first { $0.id == id } }
    private var openPairingSheet: PairingSession? {
        if case .pairing(let id) = sheet { return pairing(id) }
        return nil
    }
    var localAddresses: [String] {
        (status?.urls ?? []).compactMap { URLComponents(string: $0)?.host }.filter { $0 != "localhost" && !$0.contains(":") }
    }

    func activity(for device: Device) -> DeviceActivity {
        guard device.paired else {
            if let session = status?.pairingSessions?.first(where: { $0.isOpen && ($0.peerId == device.id || $0.peerName == device.name) }), session.state != "paired" {
                return DeviceActivity(label: text("Verbindet …", "Connecting …"), busy: true)
            }
            if device.outdated == true { return DeviceActivity(label: text("Braucht Update", "Needs update")) }
            return DeviceActivity(label: text("Zum Verbinden klicken", "Click to connect"))
        }
        if preparing.contains(device.id) { return DeviceActivity(label: text("Wird vorbereitet …", "Preparing …"), busy: true) }
        let mine = transfers.filter { $0.deviceId == device.id && $0.isActive }
        if !mine.isEmpty {
            let total = mine.reduce(Int64(0)) { $0 + max(0, $1.totalBytes) }
            let done = mine.reduce(Int64(0)) { $0 + min($1.accountedBytes, max(0, $1.totalBytes)) }
            let fraction = total > 0 ? min(1, Double(done) / Double(total)) : 0
            let percent = Int((fraction * 100).rounded(.down))
            if let running = mine.first(where: { $0.state == "running" }) {
                return DeviceActivity(label: running.direction == "send" ? text("Sendet … \(percent) %", "Sending … \(percent)%") : text("Empfängt … \(percent) %", "Receiving … \(percent)%"), progress: fraction, busy: true)
            }
            let first = mine[0]
            switch first.state {
            case "paused": return DeviceActivity(label: text("Angehalten", "Paused"), progress: fraction)
            case "offered": return DeviceActivity(label: first.direction == "send" ? text("Wartet auf Annahme", "Waiting for approval") : text("Möchte senden", "Wants to send"), progress: nil, busy: first.direction == "send")
            default: return DeviceActivity(label: device.online ? text("Wartet …", "Waiting …") : text("Wartet auf Verbindung", "Waiting to connect"), progress: fraction > 0 ? fraction : nil, busy: true)
            }
        }
        return DeviceActivity(label: device.online ? text("Bereit", "Ready") : text("Nicht erreichbar", "Not reachable"))
    }

    // MARK: Lifecycle

    func start(receiveDirectory: URL? = nil) async {
        if demo {
            let arguments = CommandLine.arguments
            if let index = arguments.firstIndex(of: "--appearance"), arguments.count > index + 1 {
                NSApp.appearance = NSAppearance(named: arguments[index + 1] == "light" ? .aqua : .darkAqua)
            }
            let scenario = arguments.firstIndex(of: "--scenario").flatMap { arguments.count > $0 + 1 ? DemoScenario(rawValue: arguments[$0 + 1]) : nil }
            loadDemo(scenario ?? .home)
            return
        }
        guard lifecycle == .stopped || lifecycle == .failed else { return }
        let generation = UUID(); lifecycleGeneration = generation
        lifecycle = .starting; engineError = nil; lastStatusData = nil
        fallback?.cancel(); fallback = nil
        do {
            guard await client.stop() else { throw NativeError.unavailable(text("Der vorherige AirLink-Dienst ließ sich nicht beenden. Bitte noch einmal versuchen.", "The previous AirLink service could not be stopped. Please try again.")) }
            guard lifecycleGeneration == generation, lifecycle != .stopping else { return }
            let helper = Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/airlink")
            guard FileManager.default.isExecutableFile(atPath: helper.path) else {
                throw NativeError.unavailable(text("Ein Teil von AirLink fehlt. Bitte AirLink neu installieren.", "Part of AirLink is missing. Please reinstall AirLink."))
            }
            let data: URL
            if let custom = ProcessInfo.processInfo.environment["AIRLINK_DATA_DIR"], custom.hasPrefix("/") {
                data = URL(fileURLWithPath: custom, isDirectory: true)
            } else {
                data = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("AirLink", isDirectory: true)
            }
            try FileManager.default.createDirectory(at: data, withIntermediateDirectories: true)
            await client.onEvent { [weak self] _ in Task { @MainActor in await self?.refresh() } }
            try await client.start(executable: helper, dataDirectory: data, receiveDirectory: receiveDirectory)
            await refresh()
            guard lifecycleGeneration == generation, lifecycle == .ready else { return }
            // Events drive updates; this slow refresh only catches what has no
            // event, such as a nearby computer that went away.
            fallback = Task { [weak self] in
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(15))
                    guard !Task.isCancelled, let self, self.lifecycleGeneration == generation, self.lifecycle == .ready else { return }
                    await self.refresh()
                }
            }
            updatePairingWindow()
        } catch {
            guard lifecycleGeneration == generation else { return }
            engineError = error.localizedDescription; lifecycle = .failed
        }
    }

    @discardableResult func stop() async -> Bool {
        if demo { return true }
        lifecycle = .stopping; lifecycleGeneration = UUID()
        fallback?.cancel(); fallback = nil
        listening?.cancel(); listening = nil
        let exited = await client.stop()
        lifecycle = exited ? .stopped : .failed
        if !exited { engineError = text("Der AirLink-Dienst ließ sich nicht beenden. Bitte noch einmal versuchen.", "The AirLink service could not be stopped. Please try again.") }
        return exited
    }

    /// Coalesces events: a change during a running request reads once more.
    func refresh() async {
        guard !demo, lifecycle == .starting || lifecycle == .ready else { return }
        if refreshing { refreshAgain = true; return }
        refreshing = true
        defer { refreshing = false }
        repeat {
            refreshAgain = false
            await loadStatus()
        } while refreshAgain && lifecycle == .ready
    }

    private func loadStatus() async {
        let generation = lifecycleGeneration
        do {
            let data = try await client.request("status.summary")
            guard lifecycleGeneration == generation, lifecycle != .stopping else { return }
            if lastStatusData == data, lifecycle == .ready { return }
            lastStatusData = data
            apply(try JSONDecoder().decode(HostStatus.self, from: data))
        } catch {
            guard lifecycleGeneration == generation else { return }
            engineError = error.localizedDescription; lifecycle = .failed
        }
    }

    private func apply(_ next: HostStatus) {
        let previous = status
        status = next; engineError = nil
        let becameReady = lifecycle != .ready
        lifecycle = .ready
        if becameReady { updatePairingWindow() }
        AirLinkUpdater.shared.configure(automatic: next.settings.automaticUpdateChecks && next.settings.setupCompleted)
        applyShortcut(next.settings)
        observe(previous: previous, next: next)
    }

    private func applyShortcut(_ settings: HostSettings) {
        guard lastShortcutAttempt != settings.shortcut, let controller = shortcutController else { return }
        lastShortcutAttempt = settings.shortcut
        do { try controller.apply(settings.shortcut); registeredShortcut = controller.actual; if settings.shortcut.enabled { shortcutError = nil } }
        catch { registeredShortcut = controller.actual; shortcutError = error.localizedDescription }
    }

    /// Opens pairing requests at once and posts notifications for what
    /// happened while the window was in the background.
    private func observe(previous: HostStatus?, next: HostStatus) {
        for pair in next.pairingSessions ?? [] where pair.initiator != true && pair.canConfirm && shownPairings.insert(pair.id).inserted {
            if openPairingSheet?.isOpen != true { sheet = .pairing(pair.id) }
            showMainWindow()
            notify(id: "pair-" + pair.id, title: text("Verbindungsanfrage", "Connection request"), body: text("\(pair.peerName) möchte sich mit diesem Mac verbinden.", "\(pair.peerName) wants to connect to this Mac."))
        }
        if next.clipboard?.contains(where: { $0.direction == "receive" && clipboardTexts[$0.id] == nil }) == true {
            Task { await loadClipboard() }
        }
        guard let previous, next.settings.notifications else { return }
        func was(_ id: String, _ state: String) -> Bool { previous.transfers.contains { $0.id == id && $0.state == state } }
        for t in next.transfers where t.state == "completed" && !was(t.id, "completed") && previous.transfers.contains(where: { $0.id == t.id }) {
            if t.direction == "receive" {
                notify(id: "done-" + t.id, title: text("Von \(t.deviceName) empfangen", "Received from \(t.deviceName)"), body: describe(t), transferID: t.id)
            } else if !NSApp.isActive {
                notify(id: "done-" + t.id, title: text("An \(t.deviceName) gesendet", "Sent to \(t.deviceName)"), body: describe(t))
            }
        }
        for t in next.transfers where t.canAcceptIncoming && !was(t.id, "offered") {
            notify(id: "offer-" + t.id, title: text("\(t.deviceName) möchte Dateien senden", "\(t.deviceName) wants to send files"), body: describe(t))
        }
        for t in next.transfers where t.state == "failed" && !was(t.id, "failed") && previous.transfers.contains(where: { $0.id == t.id }) {
            notify(id: "failed-" + t.id, title: text("Übertragung fehlgeschlagen", "Transfer failed"), body: describe(t))
        }
    }

    /// "Notizen.pdf", "Fotos" for a folder with its content, or
    /// "a.txt und 2 weitere" for several separate files.
    func describe(_ t: Transfer) -> String {
        let name = t.title
        guard let first = t.files.first else { return name }
        if first.directory == true {
            let folder = first.path + "/"
            let separate = t.files.dropFirst().filter { !$0.path.hasPrefix(folder) }.count
            return separate == 0 ? name : text("\(name) und weitere", "\(name) and more")
        }
        let count = t.numberOfFiles
        return count > 1 ? text("\(name) und \(count - 1) weitere", "\(name) and \(count - 1) more") : name
    }

    // MARK: Sending

    func send(_ urls: [URL], to device: Device) {
        guard ensureReady() else { return }
        guard device.paired, device.canReceive else {
            error = text("\(device.name) darf von diesem Mac keine Dateien empfangen. Du kannst das in den Einstellungen unter „Geräte“ ändern.", "\(device.name) may not receive files from this Mac. You can change this in Settings under Devices.")
            return
        }
        let batch: DirectSendBatch
        do { batch = try DirectSendBatch(device: device, urls: urls) } catch { self.error = sendFailure(error); return }
        preparing.insert(device.id)
        Task {
            defer { preparing.remove(device.id) }
            do {
                try await prepareOffer(JSONEncoder().encode(batch))
                await refresh()
            } catch {
                self.error = text("Senden an \(device.name) hat nicht geklappt. ", "Sending to \(device.name) did not work. ") + error.localizedDescription
                await refresh()
            }
        }
    }

    /// Files from the Services menu, the Dock, the menu bar or the shortcut.
    func offer(_ urls: [URL]) {
        guard !urls.isEmpty else { return }
        showMainWindow()
        guard ensureReady() else { return }
        let receivers = pairedDevices.filter(\.canReceive)
        if receivers.count == 1 { send(urls, to: receivers[0]); return }
        if receivers.isEmpty {
            error = text("Verbinde zuerst deinen anderen Computer. Danach kannst du Dateien senden.", "Connect your other computer first. Then you can send files.")
            return
        }
        sheet = .send(PendingSend(urls: urls))
    }

    func chooseFiles(for device: Device? = nil) {
        showMainWindow()
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true; panel.canChooseFiles = true; panel.allowsMultipleSelection = true
        panel.prompt = text("Senden", "Send")
        panel.message = device.map { text("Wähle Dateien oder Ordner für \($0.name).", "Choose files or folders for \($0.name).") } ?? text("Wähle Dateien oder Ordner zum Senden.", "Choose files or folders to send.")
        panel.begin { [weak self] response in
            guard response == .OK else { return }
            let urls = panel.urls
            Task { @MainActor in
                guard let self else { return }
                if let device { self.send(urls, to: device) } else { self.offer(urls) }
            }
        }
    }

    /// The recipient is captured by the drop target before the files load.
    func receiveDrop(_ providers: [NSItemProvider], to device: Device) -> Bool {
        let files = providers.filter { $0.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) }
        guard !files.isEmpty, device.paired, device.canReceive else { return false }
        Task {
            do { send(try await loadDroppedFiles(files), to: device) }
            catch { self.error = text("Die abgelegten Dateien konnten nicht gelesen werden. Versuch es noch einmal oder klick auf den Computer, um sie auszuwählen.", "The dropped files could not be read. Try again or click the computer to choose them.") }
        }
        return true
    }

    private func loadDroppedFiles(_ providers: [NSItemProvider]) async throws -> [URL] {
        try await withThrowingTaskGroup(of: (Int, URL).self) { group in
            for (index, provider) in providers.enumerated() {
                group.addTask {
                    let url: URL = try await withCheckedThrowingContinuation { continuation in
                        provider.loadItem(forTypeIdentifier: UTType.fileURL.identifier, options: nil) { item, error in
                            let url = (item as? URL) ?? (item as? Data).flatMap { URL(dataRepresentation: $0, relativeTo: nil) }
                            if error == nil, let url, url.isFileURL { continuation.resume(returning: url) }
                            else { continuation.resume(throwing: NativeError.unavailable("drop")) }
                        }
                    }
                    return (index, url)
                }
            }
            var loaded: [(Int, URL)] = []
            for try await item in group { loaded.append(item) }
            return loaded.sorted { $0.0 < $1.0 }.map(\.1)
        }
    }

    private func prepareOffer(_ data: Data) async throws {
        var preparation = try JSONDecoder().decode(Preparation.self, from: await client.request("preparation.start", params: data))
        let id = preparation.id
        while preparation.state == "preparing" {
            try await Task.sleep(for: .milliseconds(120))
            try requireReady()
            preparation = try JSONDecoder().decode(Preparation.self, from: await client.request("preparation.status", params: JSONEncoder().encode(["id": id])))
        }
        guard preparation.state == "completed" else { throw NativeError.unavailable(preparation.error ?? text("Das Senden wurde abgebrochen.", "Sending was cancelled.")) }
    }

    private func sendFailure(_ failure: Error) -> String {
        guard let validation = failure as? DirectSendValidationError else { return failure.localizedDescription }
        switch validation {
        case .invalidRecipient: return text("Dieser Computer kann gerade keine Dateien empfangen.", "This computer cannot receive files right now.")
        case .emptyFiles: return text("Es wurden keine Dateien ausgewählt.", "No files were selected.")
        case .nonLocalFile: return text("Nur Dateien und Ordner auf diesem Mac können gesendet werden.", "Only files and folders on this Mac can be sent.")
        }
    }

    // MARK: Transfers

    func command(_ method: String, id: String) {
        guard ensureReady() else { return }
        Task {
            do { _ = try await client.request(method, params: JSONEncoder().encode(["id": id])) }
            catch { self.error = error.localizedDescription }
            await refresh()
        }
    }
    func pause(_ t: Transfer) { command("transfer.pause", id: t.id) }
    func resume(_ t: Transfer) { command("transfer.resume", id: t.id) }
    func cancel(_ t: Transfer) { command("transfer.cancel", id: t.id) }
    func accept(_ t: Transfer) { command("transfer.accept", id: t.id) }

    func reveal(_ transfer: Transfer) {
        Task {
            do {
                try requireReady()
                let response = try JSONDecoder().decode(ReceivedTransfer.self, from: await client.request("transfer.received", params: JSONEncoder().encode(["id": transfer.id])))
                if let item = response.singleItem, item.path.hasPrefix("/") {
                    if item.directory { try openFolder(item.path) }
                    else if FileManager.default.fileExists(atPath: item.path) { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: item.path)]) }
                    else { throw NativeError.unavailable(text("Die Datei ist nicht mehr da. Vielleicht wurde sie verschoben oder gelöscht.", "The file is no longer there. It may have been moved or deleted.")) }
                } else {
                    let urls = response.items.filter { $0.path.hasPrefix("/") && FileManager.default.fileExists(atPath: $0.path) }.map { URL(fileURLWithPath: $0.path) }
                    if urls.isEmpty { try openFolder(response.directory) } else { NSWorkspace.shared.activateFileViewerSelecting(urls) }
                }
            } catch { self.error = error.localizedDescription }
        }
    }

    func openReceiveFolder() {
        do { try requireReady(); guard let directory = status?.settings.receiveDirectory else { return }; try openFolder(directory) }
        catch { self.error = error.localizedDescription }
    }

    private func openFolder(_ path: String) throws {
        var isDirectory = ObjCBool(false)
        guard path.hasPrefix("/"), FileManager.default.fileExists(atPath: path, isDirectory: &isDirectory), isDirectory.boolValue else {
            throw NativeError.unavailable(text("Der Ordner ist nicht mehr da. Wähle in den Einstellungen einen Empfangsordner.", "The folder is no longer there. Choose a receive folder in Settings."))
        }
        NSWorkspace.shared.open(URL(fileURLWithPath: path, isDirectory: true))
    }

    func transferPage(cursor: String? = nil, deviceID: String? = nil) async throws -> TransferPage {
        struct Query: Encodable { var cursor: String?; var deviceId: String?; var limit = 50 }
        try requireReady()
        return try JSONDecoder().decode(TransferPage.self, from: await client.request("history.page", params: JSONEncoder().encode(Query(cursor: cursor, deviceId: deviceID))))
    }
    func removeFromHistory(_ t: Transfer) async throws {
        try requireReady()
        _ = try await client.request("history.remove", params: JSONEncoder().encode(["id": t.id]))
        historyRevision = UUID()
        await refresh()
    }
    func clearHistory() async throws {
        try requireReady()
        _ = try await client.request("history.clear")
        historyRevision = UUID()
        await refresh()
    }

    // MARK: Pairing

    func connect(_ device: Device) {
        guard ensureReady() else { return }
        if device.outdated == true {
            error = text("\(device.name) hat eine ältere AirLink-Version. Aktualisiere AirLink dort auf 0.13 oder neuer, dann klappt das Verbinden.", "\(device.name) runs an older AirLink. Update AirLink there to 0.13 or later, then connect again.")
            return
        }
        struct Start: Encodable { let url: String; let fingerprint: String; let name: String; let platform: String?; let form: String? }
        Task {
            do {
                let data = try await client.request("pairing.start", params: JSONEncoder().encode(Start(url: device.address ?? "", fingerprint: device.fingerprint ?? "", name: device.name, platform: device.platform, form: device.form)))
                let session = try JSONDecoder().decode(PairingSession.self, from: data)
                sheet = .pairing(session.id)
                await refresh()
            } catch { self.error = text("Verbinden hat nicht geklappt: ", "Could not connect: ") + error.localizedDescription }
        }
    }

    func connect(address: String) async throws {
        try requireReady()
        let data: Data
        do { data = try await client.request("pairing.start", params: JSONEncoder().encode(["address": address])) }
        catch { throw NativeError.unavailable(text("Diese Adresse sieht nicht richtig aus. Gib die IP-Adresse ein, die AirLink auf dem anderen Computer unter „Hilfe“ anzeigt, z. B. 192.168.178.22.", "This address does not look right. Enter the IP address AirLink shows under Help on the other computer, for example 192.168.178.22.")) }
        let session = try JSONDecoder().decode(PairingSession.self, from: data)
        sheet = .pairing(session.id)
        await refresh()
    }

    func confirmPairing(_ id: String, automatic: Bool) async throws {
        try requireReady()
        struct Confirmation: Encodable { let id: String; let automaticReceive: Bool }
        _ = try await client.request("pairing.confirm", params: JSONEncoder().encode(Confirmation(id: id, automaticReceive: automatic)))
        await refresh()
    }

    func cancelPairing(_ id: String) async {
        _ = try? await client.request("pairing.cancel", params: JSONEncoder().encode(["id": id]))
        await refresh()
    }

    /// Other computers may start pairing only while this window is open.
    func setMainWindowVisible(_ visible: Bool) {
        guard windowVisible != visible else { return }
        windowVisible = visible
        updatePairingWindow()
    }

    private func updatePairingWindow() {
        listening?.cancel(); listening = nil
        guard !demo, lifecycle == .ready else { return }
        if windowVisible {
            listening = Task { [weak self] in
                while !Task.isCancelled {
                    _ = try? await self?.client.request("pairing.listen")
                    try? await Task.sleep(for: .seconds(60))
                }
            }
        } else {
            Task { _ = try? await client.request("pairing.listen", params: Data(#"{"close":true}"#.utf8)) }
        }
    }

    // MARK: Devices and clipboard

    func update(_ device: Device) {
        guard ensureReady() else { return }
        Task {
            do { _ = try await client.request("device.update", params: JSONEncoder().encode(device)) }
            catch { self.error = error.localizedDescription }
            await refresh()
        }
    }
    func remove(_ device: Device) { command("device.revoke", id: device.id) }

    var clipboardText: String? {
        guard let value = NSPasteboard.general.string(forType: .string), ClipboardText.isValid(value) else { return nil }
        return value
    }
    func sendClipboard(to device: Device) {
        guard ensureReady() else { return }
        guard let value = clipboardText else {
            error = text("In der Zwischenablage ist kein Text. Kopiere zuerst einen Text (⌘C).", "There is no text on the clipboard. Copy some text first (⌘C).")
            return
        }
        guard device.canClipboard else {
            error = text("Zwischenablage teilen ist für \(device.name) ausgeschaltet. Du kannst es in den Einstellungen unter „Geräte“ einschalten.", "Clipboard sharing is off for \(device.name). You can turn it on in Settings under Devices.")
            return
        }
        Task {
            do { _ = try await client.request("clipboard.send", params: JSONEncoder().encode(["deviceId": device.id, "text": value])) }
            catch { self.error = text("Text konnte nicht gesendet werden: ", "Text could not be sent: ") + error.localizedDescription }
        }
    }
    private func loadClipboard() async {
        guard isReady, let data = try? await client.request("clipboard.list"),
              let messages = try? JSONDecoder().decode([ClipboardMessage].self, from: data) else { return }
        for message in messages where message.direction == "receive" && clipboardTexts[message.id] == nil {
            clipboardTexts[message.id] = message.text
            notify(id: "clip-" + message.id, title: text("Text von \(message.deviceName)", "Text from \(message.deviceName)"), body: String(message.text.prefix(120)))
        }
    }
    func copy(_ message: ClipboardMessage) {
        guard let value = clipboardTexts[message.id] else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
        dismiss(message)
    }
    func dismiss(_ message: ClipboardMessage) {
        clipboardTexts[message.id] = nil
        Task { _ = try? await client.request("clipboard.dismiss", params: JSONEncoder().encode(["id": message.id])); await refresh() }
    }

    // MARK: Settings

    func updateSettings(_ change: (inout HostSettings) -> Void) {
        guard ensureReady(), var settings = status?.settings else { return }
        change(&settings)
        Task {
            do { _ = try await saveSettings(settings) }
            catch { self.error = error.localizedDescription }
        }
    }
    @discardableResult func saveSettings(_ settings: HostSettings) async throws -> HostSettings {
        try requireReady()
        guard settings.shortcut.isValid, RateLimit.isValid(settings.sendLimitBytesPerSecond), RateLimit.isValid(settings.receiveLimitBytesPerSecond) else {
            throw NativeError.unavailable(text("Ungültige Tastenkombination oder Geschwindigkeitsgrenze.", "Invalid shortcut or speed limit."))
        }
        if let controller = shortcutController, settings.shortcut != registeredShortcut || settings.shortcut.enabled != registeredShortcut.enabled {
            do { try controller.apply(settings.shortcut); registeredShortcut = controller.actual; shortcutError = nil }
            catch { shortcutError = error.localizedDescription; throw error }
        }
        lastShortcutAttempt = settings.shortcut
        let confirmed = try JSONDecoder().decode(HostSettings.self, from: await client.request("settings.update", params: JSONEncoder().encode(settings)))
        await refresh()
        return confirmed
    }
    func chooseReceiveFolder() {
        let panel = NSOpenPanel()
        panel.canChooseFiles = false; panel.canChooseDirectories = true; panel.canCreateDirectories = true
        panel.prompt = text("Auswählen", "Choose")
        panel.message = text("Wohin sollen empfangene Dateien?", "Where should received files go?")
        if let current = status?.settings.receiveDirectory { panel.directoryURL = URL(fileURLWithPath: current, isDirectory: true) }
        panel.begin { [weak self] response in
            guard response == .OK, let url = panel.url else { return }
            Task { @MainActor in
                if self?.lifecycle == .failed { await self?.start(receiveDirectory: url) }
                else { self?.updateSettings { $0.receiveDirectory = url.path } }
            }
        }
    }
    func completeWelcome(name: String, folder: String, launchAtLogin: Bool, notifications: Bool) async throws {
        try requireReady()
        let draft = SetupDraft(name: name, receiveDirectory: folder, notifications: notifications)
        guard draft.validName, draft.validFolder else { throw NativeError.unavailable(text("Bitte einen Namen eingeben.", "Please enter a name.")) }
        guard var settings = status?.settings else { return }
        settings.name = draft.normalizedName; settings.receiveDirectory = draft.receiveDirectory
        settings.notifications = notifications; settings.setupCompleted = true
        if notifications { requestNotifications() }
        setLaunchAtLogin(launchAtLogin)
        try await saveSettings(settings)
    }
    func setLaunchAtLogin(_ enabled: Bool) {
        guard !demo else { return }
        do { if enabled { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() } }
        catch { if enabled { self.error = text("„Beim Anmelden starten“ ließ sich nicht einschalten: ", "Could not turn on Open at Login: ") + error.localizedDescription } }
        refreshLoginItem()
    }
    func refreshLoginItem() {
        let state = SMAppService.mainApp.status
        launchAtLogin = state == .enabled || state == .requiresApproval
        launchNeedsApproval = state == .requiresApproval
    }
    func captureShortcut(_ enabled: Bool) {
        shortcutRecording = enabled
        if enabled { shortcutController?.suspend() }
        else { do { try shortcutController?.resume() } catch { shortcutError = error.localizedDescription } }
        registeredShortcut = shortcutController?.actual ?? Shortcut(enabled: false)
    }
    func exportDiagnostics() async {
        do {
            let data: Data
            if isReady, !demo { data = try await client.request("diagnostics.export") }
            else { data = try JSONSerialization.data(withJSONObject: ["schemaVersion": 1, "version": version, "platform": "macos", "service": "unavailable", "privacy": "No raw logs or personal data included."]) }
            let panel = NSSavePanel()
            panel.title = text("Diagnosebericht sichern", "Save diagnostic report")
            panel.nameFieldStringValue = "AirLink-Diagnose.json"
            panel.allowedContentTypes = [.json]
            guard panel.runModal() == .OK, let url = panel.url else { return }
            let object = try JSONSerialization.jsonObject(with: data)
            try JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys]).write(to: url, options: .atomic)
        } catch { self.error = error.localizedDescription }
    }

    // MARK: Windows and notifications

    func showMainWindow() {
        NSApp.activate(ignoringOtherApps: true)
        if let window = NSApp.windows.first(where: { $0.identifier?.rawValue == "airlink-main" }) {
            window.makeKeyAndOrderFront(nil)
        } else {
            openWindow?("airlink-main")
        }
    }
    func showHistory() { NSApp.activate(ignoringOtherApps: true); openWindow?("airlink-history") }

    func requestNotifications() {
        guard Bundle.main.bundleURL.pathExtension == "app" else { return }
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
    }
    private func notify(id: String, title: String, body: String, transferID: String? = nil) {
        guard !demo, Bundle.main.bundleURL.pathExtension == "app", status?.settings.notifications == true, notified.insert(id).inserted else { return }
        let content = UNMutableNotificationContent()
        content.title = title; content.body = body; content.sound = .default
        if let transferID { content.userInfo = ["transferId": transferID]; content.categoryIdentifier = "received" }
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: id, content: content, trigger: nil))
    }
    func revealTransfer(id: String) {
        guard let transfer = transfers.first(where: { $0.id == id }) else { openReceiveFolder(); return }
        reveal(transfer)
    }

    private func requireReady() throws {
        guard isReady, !demo else { throw NativeError.unavailable(engineError ?? text("AirLink startet noch. Einen Moment bitte.", "AirLink is still starting. One moment please.")) }
    }
    private func ensureReady() -> Bool {
        if demo { return false }
        guard isReady else { error = engineError ?? text("AirLink startet noch. Einen Moment bitte.", "AirLink is still starting. One moment please."); return false }
        return true
    }

    // MARK: Demo

    func loadDemo(_ scenario: DemoScenario = .home) {
        status = DemoData.status(scenario, english: LanguagePreference.usesEnglish("system"))
        sheet = scenario == .pairing ? .pairing("demo-pair") : nil
        clipboardTexts = scenario == .home ? ["demo-clip": "https://example.com/notizen"] : [:]
        lifecycle = .ready; engineError = nil
    }
}
