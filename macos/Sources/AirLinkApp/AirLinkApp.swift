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
            SettingsView().environmentObject(model).frame(width: 520)
                .preferredColorScheme(model.colorSchemeName == "dark" ? .dark : model.colorSchemeName == "light" ? .light : nil)
        }
        MenuBarExtra("AirLink", systemImage: model.activeTransfers.isEmpty ? "arrow.left.arrow.right" : "arrow.up.arrow.down.circle.fill") {
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
            if model.activeTransfers.isEmpty {
                Label(model.text("Bereit zum Teilen", "Ready to share"), systemImage: "checkmark.circle").foregroundStyle(.secondary)
            } else {
                ForEach(model.activeTransfers.prefix(4)) { transfer in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(transfer.title).lineLimit(1)
                        ProgressView(value: transfer.fractionCompleted)
                        Text("\(transfer.deviceName) · \(TransferFormatting.speed(transfer.bytesPerSecond))").font(.caption).foregroundStyle(.secondary)
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
    private var hotKey: EventHotKeyRef?
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.servicesProvider = services
        var spec = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed))
        InstallEventHandler(GetApplicationEventTarget(), { _, _, _ in
            Task { @MainActor in AppModel.shared.chooseFiles() }
            return noErr
        }, 1, &spec, nil, nil)
        let identifier = EventHotKeyID(signature: OSType(0x414C4E4B), id: 1)
        let hotKeyStatus = RegisterEventHotKey(UInt32(kVK_ANSI_L), UInt32(cmdKey | shiftKey), identifier, GetApplicationEventTarget(), 0, &hotKey)
        if hotKeyStatus != noErr {
            AppModel.shared.error = AppModel.shared.text("⇧⌘L konnte nicht registriert werden. Möglicherweise verwendet eine andere App diese Tastenkombination.", "⇧⌘L could not be registered. Another application may already use this shortcut.")
        }
        Task { await AppModel.shared.start() }
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        if !AppModel.shared.activeTransfers.isEmpty {
            let alert = NSAlert()
            alert.messageText = AppModel.shared.text("Aktive Übertragungen anhalten?", "Stop active transfers?")
            alert.informativeText = AppModel.shared.text("AirLink speichert den Fortschritt beim Beenden. Mit geöffneter App können Übertragungen im Hintergrund weiterlaufen.", "AirLink saves progress when quitting. Leave the app running to continue transfers in the background.")
            alert.addButton(withTitle: AppModel.shared.text("Beenden", "Quit")); alert.addButton(withTitle: AppModel.shared.text("Abbrechen", "Cancel"))
            if alert.runModal() != .alertFirstButtonReturn { return .terminateCancel }
        }
        Task { await AppModel.shared.stop(); NSApp.reply(toApplicationShouldTerminate: true) }
        return .terminateLater
    }
}

@MainActor final class FileService: NSObject {
    @objc func sendFiles(_ pasteboard: NSPasteboard, userData: String?, error: AutoreleasingUnsafeMutablePointer<NSString?>) {
        let urls = pasteboard.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
        AppModel.shared.add(urls)
    }
}
