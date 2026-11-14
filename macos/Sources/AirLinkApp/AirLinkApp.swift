import AppKit
import AirLinkCore
import Carbon
import SwiftUI
import UserNotifications

@main struct AirLinkApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var delegate
    @StateObject private var model = AppModel.shared
    @StateObject private var updater = AirLinkUpdater.shared
    var body: some Scene {
        Window("AirLink", id: "airlink-main") {
            HomeView().environmentObject(model)
        }
        .defaultSize(width: 700, height: 640)
        .commands {
            CommandGroup(replacing: .appInfo) {
                Button(model.text("Über AirLink", "About AirLink")) {
                    NSApp.orderFrontStandardAboutPanel(options: [.applicationVersion: model.version])
                }
            }
            CommandGroup(after: .appInfo) {
                Button(model.text("Nach Updates suchen …", "Check for Updates …")) { updater.checkForUpdates() }
                    .disabled(!updater.canCheckForUpdates)
            }
            CommandGroup(replacing: .newItem) {
                Button(model.text("Dateien senden …", "Send Files …")) { model.chooseFiles() }.keyboardShortcut("o")
                Button(model.text("Computer hinzufügen …", "Add Computer …")) { model.showMainWindow(); model.sheet = .addDevice(byAddress: false) }.keyboardShortcut("k")
                Divider()
                Button(model.text("Empfangsordner öffnen", "Open Receive Folder")) { model.openReceiveFolder() }.keyboardShortcut("d", modifiers: [.command, .shift])
            }
            CommandGroup(replacing: .help) {
                Button(model.text("AirLink-Hilfe", "AirLink Help")) { model.showMainWindow(); model.sheet = .help }
            }
        }
        Window(model.text("Verlauf", "History"), id: "airlink-history") {
            HistoryView().environmentObject(model)
        }
        .defaultSize(width: 640, height: 560)
        Settings {
            SettingsView().environmentObject(model)
        }
        MenuBarExtra {
            MenuContent().environmentObject(model).environmentObject(updater)
        } label: {
            MenuBarLabel().environmentObject(model)
        }
        .menuBarExtraStyle(.window)
    }
}

/// The menu bar symbol. It exists for the whole app lifetime, so it also
/// provides the window opener when the main window was never shown.
struct MenuBarLabel: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.openWindow) private var openWindow
    var body: some View {
        Image(systemName: model.activeTransfers.isEmpty ? "arrow.left.arrow.right" : "arrow.up.arrow.down.circle.fill")
            .onAppear { if model.openWindow == nil { model.openWindow = { openWindow(id: $0) } } }
    }
}

/// The small window behind the menu bar symbol.
struct MenuContent: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.openWindow) private var openWindow
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("AirLink").font(.headline)
                Spacer()
                Text(model.isReady ? model.text("Bereit", "Ready") : model.text("Startet …", "Starting …")).font(.caption).foregroundStyle(.secondary)
            }
            if !model.activeTransfers.isEmpty {
                ForEach(model.activeTransfers.prefix(3)) { transfer in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(model.describe(transfer)).lineLimit(1).truncationMode(.middle)
                        ProgressView(value: transfer.fractionCompleted).controlSize(.small)
                        Text((transfer.direction == "send" ? model.text("An ", "To ") : model.text("Von ", "From ")) + transfer.deviceName + " · " + TransferFormatting.speed(transfer.bytesPerSecond)).font(.caption).foregroundStyle(.secondary)
                    }
                }
            } else if !model.pairedDevices.isEmpty {
                ForEach(model.pairedDevices.prefix(4)) { device in
                    Label(device.name, systemImage: DeviceSymbol.name(device)).foregroundStyle(device.online ? .primary : .secondary)
                }
            }
            Divider()
            Button(model.text("Dateien senden …", "Send Files …")) { model.chooseFiles() }.disabled(!model.isReady || model.pairedDevices.isEmpty)
            Button(model.text("AirLink öffnen", "Open AirLink")) { openWindow(id: "airlink-main"); model.showMainWindow() }
            Button(model.text("Beenden", "Quit")) { NSApp.terminate(nil) }
        }
        .buttonStyle(.plain)
        .padding(16).frame(width: 280)
    }
}

@MainActor final class AppDelegate: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    private let services = FileService()
    private let shortcuts = ShortcutController()
    private var terminating = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        if let index = CommandLine.arguments.firstIndex(of: "--snapshot"), CommandLine.arguments.count > index + 1 {
            let target = URL(fileURLWithPath: CommandLine.arguments[index + 1], isDirectory: true)
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { Snapshotter.run(to: target) }
            return
        }
        NSApp.servicesProvider = services
        AppModel.shared.shortcutController = shortcuts
        if Bundle.main.bundleURL.pathExtension == "app" {
            let center = UNUserNotificationCenter.current()
            center.delegate = self
            let reveal = UNNotificationAction(identifier: "reveal", title: AppModel.shared.text("Zeigen", "Show"), options: [.foreground])
            center.setNotificationCategories([UNNotificationCategory(identifier: "received", actions: [reveal], intentIdentifiers: [])])
        }
        // Started at login: stay in the menu bar instead of opening the window.
        if let event = NSAppleEventManager.shared().currentAppleEvent, event.eventID == kAEOpenApplication,
           event.paramDescriptor(forKeyword: keyAEPropData)?.enumCodeValue == keyAELaunchedAsLogInItem {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) {
                NSApp.windows.first { $0.identifier?.rawValue == "airlink-main" }?.close()
            }
        }
        Task { await AppModel.shared.start() }
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag { AppModel.shared.showMainWindow() }
        return true
    }
    /// Files dropped on the Dock symbol or opened with AirLink.
    func application(_ application: NSApplication, open urls: [URL]) {
        let files = urls.filter(\.isFileURL)
        guard !files.isEmpty else { return }
        AppModel.shared.offer(files)
    }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard !terminating else { return .terminateLater }
        let model = AppModel.shared
        if !model.activeTransfers.isEmpty {
            let alert = NSAlert()
            alert.messageText = model.text("AirLink beenden?", "Quit AirLink?")
            alert.informativeText = model.text("Laufende Übertragungen werden angehalten und beim nächsten Start dort fortgesetzt, wo sie aufgehört haben.", "Running transfers pause and continue where they stopped the next time AirLink starts.")
            alert.addButton(withTitle: model.text("Beenden", "Quit")); alert.addButton(withTitle: model.text("Abbrechen", "Cancel"))
            if alert.runModal() != .alertFirstButtonReturn { return .terminateCancel }
        }
        terminating = true
        Task {
            let stopped = await model.stop()
            if stopped { shortcuts.stop() }
            terminating = false
            NSApp.reply(toApplicationShouldTerminate: stopped)
        }
        return .terminateLater
    }
    func applicationWillTerminate(_ notification: Notification) { shortcuts.stop() }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        await MainActor.run { NSApp.isActive ? [] : [.banner, .sound] }
    }
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let transferID = response.notification.request.content.userInfo["transferId"] as? String
        let reveal = response.actionIdentifier == "reveal"
        await MainActor.run {
            if let transferID, reveal { AppModel.shared.revealTransfer(id: transferID) }
            else { AppModel.shared.showMainWindow() }
        }
    }
}

@MainActor final class FileService: NSObject {
    @objc func sendFiles(_ pasteboard: NSPasteboard, userData: String?, error: AutoreleasingUnsafeMutablePointer<NSString?>) {
        let modern = pasteboard.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
        let legacy = pasteboard.propertyList(forType: NSPasteboard.PasteboardType("NSFilenamesPboardType")) as? [String] ?? []
        AppModel.shared.offer(ServiceSelection.urls(modern: modern, legacyPaths: legacy))
    }
}
