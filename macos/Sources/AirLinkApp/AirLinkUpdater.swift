import AppKit
import Combine
import AirLinkCore
import Sparkle

/// Manual installation with optional daily checks after a saved user opt-in.
@MainActor final class AirLinkUpdater: NSObject, ObservableObject, SPUUpdaterDelegate {
    static let shared = AirLinkUpdater()
    @Published private(set) var canCheckForUpdates = true
    private var started = false
    private var automatic = false
    private var dailyTask: Task<Void, Never>?
    private lazy var driver = AirLinkUpdateUserDriver(hostBundle: .main, delegate: nil)
    private lazy var updater = SPUUpdater(hostBundle: .main, applicationBundle: .main, userDriver: driver, delegate: self)

    func checkForUpdates() {
            do {
                try ensureStarted()
            } catch {
                let alert = NSAlert()
                alert.messageText = AppModel.shared.text("Updateprüfung nicht verfügbar", "Update checking is unavailable")
                alert.informativeText = error.localizedDescription
                alert.addButton(withTitle: "OK")
                alert.runModal()
                return
            }
        updater.checkForUpdates()
    }

    private func ensureStarted() throws {
        guard !started else { return }
        updater.automaticallyChecksForUpdates = false
        updater.automaticallyDownloadsUpdates = false
        try updater.start()
        started = true
        updater.publisher(for: \.canCheckForUpdates).assign(to: &$canCheckForUpdates)
    }
    func configure(automatic enabled: Bool) {
        automatic = enabled
        guard enabled else { dailyTask?.cancel(); dailyTask = nil; return }
        guard dailyTask == nil else { return }
        dailyTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(5))
            while !Task.isCancelled {
                guard let self else { return }
                let defaults = UserDefaults.standard
                let last = defaults.object(forKey: "AirLinkLastAutomaticUpdateCheck") as? Date
                if UpdateCheckSchedule.isDue(enabled: self.automatic, lastAttempt: last), self.canCheckForUpdates {
                    defaults.set(Date(), forKey: "AirLinkLastAutomaticUpdateCheck")
                    do { try self.ensureStarted(); self.updater.checkForUpdatesInBackground() }
                    catch { /* A background failure never interrupts local transfers. */ }
                }
                try? await Task.sleep(for: .seconds(60))
            }
        }
    }

    func updater(_ updater: SPUUpdater, mayPerform updateCheck: SPUUpdateCheck) throws {
        // Info.plist disables scheduling and automatic downloads. Also refuse
        // background requests if an old/user-modified preference enables them.
        guard updateCheck == .updates || automatic && updateCheck == .updatesInBackground else {
            throw NSError(domain: "AirLink.Updates", code: 1, userInfo: [
                NSLocalizedDescriptionKey: AppModel.shared.text("Updates werden nur auf Anfrage geprüft.", "Updates are checked only on request.")
            ])
        }
    }

    func updaterShouldPromptForPermissionToCheck(forUpdates updater: SPUUpdater) -> Bool { false }
    func allowedSystemProfileKeys(for updater: SPUUpdater) -> [String]? { [] }
    func allowedChannels(for updater: SPUUpdater) -> Set<String> { ["beta"] }
}

/// Sparkle still performs its standard download, verification and installation.
/// Only feed-loading errors gain context for the private GitHub distribution.
@MainActor private final class AirLinkUpdateUserDriver: SPUStandardUserDriver {
    override func showUpdaterError(_ error: Error, acknowledgement: @escaping () -> Void) {
        let original = error as NSError
        guard original.domain == SUSparkleErrorDomain, original.code == Int(SUError.appcastError.rawValue) else {
            super.showUpdaterError(error, acknowledgement: acknowledgement)
            return
        }
        var info = original.userInfo
        info[NSLocalizedDescriptionKey] = AppModel.shared.text("Updatequelle nicht erreichbar", "Update source unavailable")
        info[NSLocalizedRecoverySuggestionErrorKey] = AppModel.shared.text(
            "Prüfe deine Internetverbindung oder versuche es später erneut. Lokale Übertragungen funktionieren unabhängig von der Updatequelle.",
            "Check your internet connection or try again later. Local transfers work independently of the update source."
        )
        super.showUpdaterError(NSError(domain: original.domain, code: original.code, userInfo: info), acknowledgement: acknowledgement)
    }
}
