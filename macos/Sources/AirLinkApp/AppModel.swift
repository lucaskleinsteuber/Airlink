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
    var openMainWindow: (() -> Void)?
    private let client = DaemonClient()
    private var polling: Task<Void, Never>?
    private var didStart = false
    private var refreshing = false
    private var notified = Set<String>()

    var english: Bool { LanguagePreference.usesEnglish(status?.settings.language ?? "system") }
    func text(_ de: String, _ en: String) -> String { english ? en : de }
    var selectedDevice: Device? { status?.devices.first { $0.id == selectedDeviceID } }
    var activeTransfers: [Transfer] { status?.transfers.filter(\.isActive) ?? [] }
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
            if next.settings.notifications, let previous {
                for transfer in next.transfers where transfer.state == "completed" && previous.transfers.contains(where: { $0.id == transfer.id && $0.state != "completed" }) {
                    notify(id: transfer.id, title: text("Übertragung abgeschlossen", "Transfer completed"), body: transfer.title)
                }
                for pair in next.pairRequests where !previous.pairRequests.contains(where: { $0.id == pair.id }) {
                    notify(id: pair.id, title: text("Verbindungsanfrage", "Pairing request"), body: pair.name)
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
    func update(_ settings: HostSettings) { perform("settings.update", data: try? JSONEncoder().encode(settings)) }
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
        if let destination = transfer.destination { NSWorkspace.shared.open(URL(fileURLWithPath: destination)) }
    }
}
