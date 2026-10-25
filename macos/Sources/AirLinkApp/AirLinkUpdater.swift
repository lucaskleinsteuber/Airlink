import AppKit
import Combine
import Sparkle

/// The updater is created and started only after an explicit menu action.
/// Local transfer startup does not initialize an update session or use a feed.
@MainActor final class AirLinkUpdater: NSObject, ObservableObject, SPUUpdaterDelegate {
    @Published private(set) var canCheckForUpdates = true
    private var started = false
    private lazy var driver = AirLinkUpdateUserDriver(hostBundle: .main, delegate: nil)
    private lazy var updater = SPUUpdater(hostBundle: .main, applicationBundle: .main, userDriver: driver, delegate: self)

    func checkForUpdates() {
        if !started {
            do {
                try updater.start()
                started = true
                updater.publisher(for: \.canCheckForUpdates).assign(to: &$canCheckForUpdates)
            } catch {
                let alert = NSAlert()
                alert.messageText = AppModel.shared.text("Updateprüfung nicht verfügbar", "Update checking is unavailable")
                alert.informativeText = error.localizedDescription
                alert.addButton(withTitle: "OK")
                alert.runModal()
                return
            }
        }
        updater.checkForUpdates()
    }

    func updater(_ updater: SPUUpdater, mayPerform updateCheck: SPUUpdateCheck) throws {
        // Info.plist disables scheduling and automatic downloads. Also refuse
        // background requests if an old/user-modified preference enables them.
        guard updateCheck == .updates else {
            throw NSError(domain: "AirLink.Updates", code: 1, userInfo: [
                NSLocalizedDescriptionKey: AppModel.shared.text("Updates werden nur auf Anfrage geprüft.", "Updates are checked only on request.")
            ])
        }
    }

    func updaterShouldPromptForPermissionToCheck(forUpdates updater: SPUUpdater) -> Bool { false }
    func allowedSystemProfileKeys(for updater: SPUUpdater) -> [String]? { [] }
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
            "Prüfe deine Internetverbindung. Solange das GitHub-Repository privat ist, kann AirLink dessen Update-Dateien nicht abrufen. Lokale Übertragungen funktionieren unabhängig davon.",
            "Check your internet connection. While the GitHub repository is private, AirLink cannot retrieve its update files. Local transfers work independently."
        )
        super.showUpdaterError(NSError(domain: original.domain, code: original.code, userInfo: info), acknowledgement: acknowledgement)
    }
}
