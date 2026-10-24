import AppKit
import AirLinkCore
import Combine
import ServiceManagement
import UserNotifications

@MainActor final class AppModel: ObservableObject {
    static let shared = AppModel()
    @Published var status: HostStatus?
    @Published var selectedDeviceID: String?
    @Published var error: String?
    @Published var engineError: String?
    @Published var pendingURLs: [URL] = []
    @Published var showConnection = false
    @Published var registeredShortcut = Shortcut(enabled: false)
    @Published var shortcutError: String?
    @Published var shortcutRecording = false
    var shortcutController: ShortcutController?
    var openMainWindow: (() -> Void)?
    private let client = DaemonClient()
    private var polling: Task<Void, Never>?
    private var didStart = false
    private var refreshing = false
    private var notified = Set<String>()
    private var lastShortcutAttempt: Shortcut?

    var english: Bool { LanguagePreference.usesEnglish(status?.settings.language ?? "system") }
    func text(_ de: String, _ en: String) -> String { english ? en : de }
    var selectedDevice: Device? { status?.devices.first { $0.id == selectedDeviceID } }
    var activeTransfers: [Transfer] { status?.transfers.filter(\.isActive) ?? [] }
    var activeRelays: [Relay] { status?.relays?.filter(\.isActive) ?? [] }
    var colorSchemeName: String { status?.settings.appearance ?? "system" }
    var version: String { status?.version ?? Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "" }
    var shares: [Share] { status?.shares ?? [] }
    var clipboardMessages: [ClipboardMessage] { status?.clipboard ?? [] }

    func start() async {
        guard !didStart else { return }; didStart = true
        do {
            let helper = Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/airlink")
            guard FileManager.default.isExecutableFile(atPath: helper.path) else {
                throw NativeError.unavailable(text("Der AirLink-Helfer fehlt. Bitte die vollständige App verwenden.", "AirLink helper is missing. Please use the complete app bundle."))
            }
            let data = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("AirLink", isDirectory: true)
            try FileManager.default.createDirectory(at: data, withIntermediateDirectories: true)
            try await client.start(executable: helper, dataDirectory: data)
            await refresh()
            polling = Task { [weak self] in
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(1))
                    guard !Task.isCancelled else { return }
                    await self?.refresh()
                }
            }
        } catch { engineError = error.localizedDescription }
    }
    func stop() async { polling?.cancel(); await client.stop() }
    func refresh() async {
        guard !refreshing else { return }
        refreshing = true
        defer { refreshing = false }
        do {
            let data = try await client.request("status")
            let next = try JSONDecoder().decode(HostStatus.self, from: data)
            let previous = status
            status = next; engineError = nil
            if lastShortcutAttempt != next.settings.shortcut, let controller = shortcutController {
                lastShortcutAttempt = next.settings.shortcut
                do { try controller.apply(next.settings.shortcut); registeredShortcut = controller.actual; if next.settings.shortcut.enabled { shortcutError = nil } }
                catch {
                    registeredShortcut = controller.actual; shortcutError = error.localizedDescription
                    var rollback = next.settings; rollback.shortcut = controller.actual
                    Task { [weak self] in
                        do { _ = try await self?.client.request("settings.update", params: JSONEncoder().encode(rollback)) }
                        catch { self?.shortcutError = error.localizedDescription }
                    }
                }
            }
            if next.settings.notifications, let previous {
                for transfer in next.transfers where transfer.state == "completed" && previous.transfers.contains(where: { $0.id == transfer.id && $0.state != "completed" }) {
                    let files = transfer.files.filter { $0.directory != true }
                    let allSkipped = !files.isEmpty && files.allSatisfy(\.isSkipped)
                    notify(id: transfer.id, title: allSkipped ? text("Ohne neue Dateikopie abgeschlossen", "Finished without a new file copy") : text("Übertragung abgeschlossen", "Transfer completed"), body: transfer.title)
                }
                for pair in next.pairRequests where !previous.pairRequests.contains(where: { $0.id == pair.id }) {
                    notify(id: pair.id, title: text("Verbindungsanfrage", "Pairing request"), body: pair.name)
                }
                for transfer in next.transfers where transfer.direction == "receive" {
                    for file in transfer.pendingDuplicateFiles {
                        let wasPending = previous.transfers.first(where: { $0.id == transfer.id })?.pendingDuplicateFiles.contains(where: { $0.id == file.id }) == true
                        if !wasPending {
                            notify(id: "duplicate-\(transfer.id)-\(file.id)", title: text("Duplikat benötigt deine Entscheidung", "Duplicate needs your decision"), body: file.path)
                        }
                    }
                }
            }
        } catch { engineError = error.localizedDescription }
    }
    func command(_ method: String, params: [String: String]) {
        perform(method, data: try? JSONEncoder().encode(params))
    }
    func perform(_ method: String, data: Data?) {
        Task {
            do { _ = try await client.request(method, params: data); await refresh() }
            catch { self.error = error.localizedDescription }
        }
    }
    func update(_ device: Device) { perform("device.update", data: try? JSONEncoder().encode(device)) }
    func update(_ settings: HostSettings) { Task { do { try await saveSettings(settings) } catch { self.error = error.localizedDescription } } }
    func saveSettings(_ settings: HostSettings) async throws {
        guard settings.shortcut.isValid, RateLimit.isValid(settings.sendLimitBytesPerSecond), RateLimit.isValid(settings.receiveLimitBytesPerSecond) else { throw NativeError.unavailable(text("Ungültige Tastenkombination oder Geschwindigkeitsgrenze.", "Invalid shortcut or speed limit.")) }
        guard settings.organization.isValid else { throw NativeError.unavailable(text("Bitte gültige Dateiendungen und sichere relative Ordner für die Sortierregeln verwenden.", "Use valid extensions and safe relative folders for sorting rules.")) }
        guard let controller = shortcutController else { throw NativeError.unavailable(text("Die Tastenkombination ist noch nicht bereit.", "The shortcut handler is not ready yet.")) }
        let previous = registeredShortcut
        do {
            try controller.apply(settings.shortcut)
            registeredShortcut = controller.actual
            _ = try await client.request("settings.update", params: JSONEncoder().encode(settings))
            shortcutError = nil; lastShortcutAttempt = settings.shortcut
            await refresh()
        } catch {
            try? controller.apply(previous)
            registeredShortcut = controller.actual
            shortcutError = error.localizedDescription
            throw error
        }
    }
    func captureShortcut(_ enabled: Bool) {
        shortcutRecording = enabled
        if enabled { shortcutController?.suspend() }
        else {
            do { try shortcutController?.resume() }
            catch { shortcutError = error.localizedDescription }
        }
        registeredShortcut = shortcutController?.actual ?? Shortcut(enabled: false)
    }
    func history(_ filter: HistoryFilter) async throws -> History {
        try JSONDecoder().decode(History.self, from: await client.request("history.list", params: JSONEncoder().encode(filter)))
    }
    func clearHistory() async throws {
        _ = try await client.request("history.clear")
        await refresh()
    }
    func resolveDuplicate(transferID: String, fileID: String, action: String, candidateID: String? = nil) async throws {
        guard let transfer = status?.transfers.first(where: { $0.id == transferID }), transfer.canResolveDuplicates,
              let file = transfer.pendingDuplicateFiles.first(where: { $0.id == fileID }), action == "keep" || action == "skip" else {
            throw NativeError.unavailable(text("Die Übertragung benötigt eine aktuelle, nicht pausierte Duplikatentscheidung.", "Refresh or resume the transfer before resolving this duplicate."))
        }
        if action == "skip", file.duplicateCandidates?.contains(where: { $0.id == candidateID }) != true {
            throw NativeError.unavailable(text("Die vorhandene Datei muss erneut geprüft werden.", "The existing candidate needs to be checked again."))
        }
        var params = ["transferId": transferID, "fileId": fileID, "action": action]
        if let candidateID { params["candidateId"] = candidateID }
        _ = try await client.request("duplicate.resolve", params: JSONEncoder().encode(params))
        await refresh()
    }
    func revealDuplicate(_ candidateID: String) async throws {
        struct CandidatePath: Decodable { var path: String }
        let response = try await client.request("duplicate.path", params: JSONEncoder().encode(["candidateId": candidateID]))
        let path = try JSONDecoder().decode(CandidatePath.self, from: response).path
        guard path.hasPrefix("/"), FileManager.default.fileExists(atPath: path) else {
            throw NativeError.unavailable(text("Die vorhandene Datei ist nicht mehr verfügbar.", "The existing file is no longer available."))
        }
        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
    }
    func createShare(_ request: ShareCreate) async throws -> Share {
        guard request.hasValidSource else { throw NativeError.unavailable(text("Bitte Dateien oder einen Ordner auswählen.", "Please choose files or a folder.")) }
        let result = try await client.request("share.create", params: JSONEncoder().encode(request))
        let share = try JSONDecoder().decode(Share.self, from: result)
        await refresh()
        return share
    }
    func sendClipboard(_ text: String, to deviceID: String) async throws {
        guard ClipboardText.isValid(text) else { throw NativeError.unavailable(self.text("Text muss 1 bis 65.536 UTF-8-Bytes enthalten.", "Text must contain 1 to 65,536 UTF-8 bytes.")) }
        guard status?.devices.contains(where: { $0.id == deviceID && $0.paired && $0.canClipboard }) == true else {
            throw NativeError.unavailable(self.text("Zwischenablage ist für dieses Gerät nicht freigegeben.", "Clipboard sharing is not enabled for this device."))
        }
        let request = ["deviceId": deviceID, "text": text]
        _ = try await client.request("clipboard.send", params: JSONEncoder().encode(request))
        await refresh()
    }
    func connect(url: String, fingerprint: String) {
        command("peer.connect", params: ["url": url.trimmingCharacters(in: .whitespacesAndNewlines), "fingerprint": fingerprint.replacingOccurrences(of: " ", with: "").replacingOccurrences(of: ":", with: "").lowercased()])
    }
    func add(_ urls: [URL]) {
        pendingURLs += urls.filter { url in !pendingURLs.contains(url) }
        showMainWindow()
    }
    func send() {
        guard let id = selectedDeviceID, !pendingURLs.isEmpty else { return }
        struct Offer: Encodable { var deviceId: String; var paths: [String] }
        let data = try? JSONEncoder().encode(Offer(deviceId: id, paths: pendingURLs.map(\.path)))
        Task {
            do { _ = try await client.request("offer", params: data); pendingURLs = []; await refresh() }
            catch { self.error = error.localizedDescription }
        }
    }
    func chooseFiles() {
        showMainWindow()
        let panel = NSOpenPanel(); panel.canChooseDirectories = true; panel.canChooseFiles = true; panel.allowsMultipleSelection = true
        panel.begin { response in if response == .OK { Task { @MainActor in self.add(panel.urls) } } }
    }
    func showMainWindow() {
        NSApp.activate(ignoringOtherApps: true)
        if let window = NSApp.windows.first(where: { $0.identifier?.rawValue == "airlink-main" || $0.title == "AirLink" }) {
            window.makeKeyAndOrderFront(nil)
        } else {
            openMainWindow?()
        }
    }
    func notificationPermission() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
    }
    private func notify(id: String, title: String, body: String) {
        guard notified.insert(id).inserted else { return }
        let content = UNMutableNotificationContent(); content.title = title; content.body = body; content.sound = .default
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: id, content: content, trigger: nil))
    }
    func revealDestination(_ transfer: Transfer) {
        if transfer.hasReceivedContent, let destination = transfer.destination, FileManager.default.fileExists(atPath: destination) {
            NSWorkspace.shared.open(URL(fileURLWithPath: destination))
        }
    }
}
