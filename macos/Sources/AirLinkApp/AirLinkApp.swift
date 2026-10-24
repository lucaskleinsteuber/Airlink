import AppKit
import AirLinkCore
import Carbon
import SwiftUI

@main struct AirLinkApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var delegate
    @StateObject private var model = AppModel.shared
    var body: some Scene {
        Window("AirLink", id: "airlink-main") {
            MainView().environmentObject(model)
                .preferredColorScheme(model.colorSchemeName == "dark" ? .dark : model.colorSchemeName == "light" ? .light : nil)
                .frame(minWidth: 840, minHeight: 560)
        }
        .defaultSize(width: 1000, height: 700)
        .commands {
            CommandGroup(after: .newItem) {
                Button(model.text("Dateien senden …", "Send files …")) { model.chooseFiles() }.keyboardShortcut("o")
                Button(model.text("Gerät verbinden …", "Connect device …")) { model.showConnection = true; model.showMainWindow() }.keyboardShortcut("k")
            }
        }
        Settings {
            SettingsView().environmentObject(model).frame(width: 560, height: 680)
                .preferredColorScheme(model.colorSchemeName == "dark" ? .dark : model.colorSchemeName == "light" ? .light : nil)
        }
        MenuBarExtra("AirLink", systemImage: model.activeTransfers.isEmpty && model.activeRelays.isEmpty ? "arrow.left.arrow.right" : "arrow.up.arrow.down.circle.fill") {
            MenuContent().environmentObject(model)
                .preferredColorScheme(model.colorSchemeName == "dark" ? .dark : model.colorSchemeName == "light" ? .light : nil)
        }
        .menuBarExtraStyle(.window)
    }
}

struct MenuContent: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.openWindow) private var openWindow
    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack { Text("AirLink").font(.headline); Spacer(); Text(model.version).foregroundStyle(.secondary) }
            if model.activeTransfers.isEmpty && model.activeRelays.isEmpty {
                Label(model.text("Bereit zum Teilen", "Ready to share"), systemImage: "checkmark.circle").foregroundStyle(.secondary)
            } else {
                ForEach(model.activeTransfers.prefix(4)) { transfer in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(transfer.title).lineLimit(1)
                        ProgressView(value: transfer.fractionCompleted)
                        Text("\(transfer.deviceName) · \(TransferFormatting.speed(transfer.bytesPerSecond))").font(.caption).foregroundStyle(.secondary)
                    }
                }
                ForEach(model.activeRelays.prefix(2)) { relay in
                    VStack(alignment: .leading, spacing: 4) {
                        Label(relay.title, systemImage: "arrow.triangle.branch").lineLimit(1)
                        ProgressView(value: relay.fractionDelivered)
                        Text("\(relay.senderName) → \(relay.receiverName) · \(model.stateLabel(relay.state))").font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            Divider()
            Button(model.text("AirLink öffnen", "Open AirLink")) { openWindow(id: "airlink-main"); model.showMainWindow() }
            Button(model.text("Beenden", "Quit")) { NSApp.terminate(nil) }
        }.padding(18).frame(width: 300)
    }
}

@MainActor final class AppDelegate: NSObject, NSApplicationDelegate {
    private let services = FileService()
    private let shortcuts = ShortcutController()
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.servicesProvider = services
        AppModel.shared.shortcutController = shortcuts
        Task { await AppModel.shared.start() }
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        if !AppModel.shared.activeTransfers.isEmpty || !AppModel.shared.activeRelays.isEmpty {
            let alert = NSAlert()
            alert.messageText = AppModel.shared.text("Aktive Übertragungen anhalten?", "Stop active transfers?")
            alert.informativeText = AppModel.shared.text("AirLink speichert den bestätigten Stand. Browser-Relays benötigen beim Fortsetzen erneut erreichbare Quell-Tabs. Mit geöffneter App können Übertragungen weiterlaufen.", "AirLink saves confirmed progress. Browser relays need reachable source tabs to continue later. Leave the app running to continue transfers.")
            alert.addButton(withTitle: AppModel.shared.text("Beenden", "Quit")); alert.addButton(withTitle: AppModel.shared.text("Abbrechen", "Cancel"))
            if alert.runModal() != .alertFirstButtonReturn { return .terminateCancel }
        }
        shortcuts.stop()
        Task { await AppModel.shared.stop(); NSApp.reply(toApplicationShouldTerminate: true) }
        return .terminateLater
    }
    func applicationWillTerminate(_ notification: Notification) { shortcuts.stop() }
}

@MainActor final class FileService: NSObject {
    @objc func sendFiles(_ pasteboard: NSPasteboard, userData: String?, error: AutoreleasingUnsafeMutablePointer<NSString?>) {
        let modern = pasteboard.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
        let legacy = pasteboard.propertyList(forType: NSPasteboard.PasteboardType("NSFilenamesPboardType")) as? [String] ?? []
        AppModel.shared.add(ServiceSelection.urls(modern: modern, legacyPaths: legacy))
    }
}
