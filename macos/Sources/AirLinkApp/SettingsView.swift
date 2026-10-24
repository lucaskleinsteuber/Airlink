import AppKit
import AirLinkCore
import ServiceManagement
import SwiftUI

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @State private var settings = HostSettings()
    @State private var loginEnabled = SMAppService.mainApp.status == .enabled
    @State private var loginNeedsApproval = SMAppService.mainApp.status == .requiresApproval
    var body: some View {
        Form {
            Section(model.text("Allgemein", "General")) {
                TextField(model.text("Gerätename", "Device name"), text: $settings.name)
                Picker(model.text("Darstellung", "Appearance"), selection: $settings.appearance) {
                    Text(model.text("System", "System")).tag("system"); Text(model.text("Hell", "Light")).tag("light"); Text(model.text("Dunkel", "Dark")).tag("dark")
                }
                Picker(model.text("Sprache", "Language"), selection: $settings.language) {
                    Text(model.text("System", "System")).tag("system")
                    Text("Deutsch").tag("de")
                    Text("English").tag("en")
                }
                Toggle(model.text("Benachrichtigungen", "Notifications"), isOn: $settings.notifications)
                    .onChange(of: settings.notifications) { _, enabled in if enabled { model.notificationPermission() } }
                if settings.notifications {
                    Button(model.text("Benachrichtigungen im System erlauben …", "Allow system notifications …")) { model.notificationPermission() }
                }
                Toggle(model.text("Beim Anmelden starten", "Launch at login"), isOn: $loginEnabled)
                    .onChange(of: loginEnabled) { _, enabled in
                        do {
                            if enabled { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
                            loginNeedsApproval = SMAppService.mainApp.status == .requiresApproval
                        }
                        catch { model.error = error.localizedDescription; loginEnabled = SMAppService.mainApp.status == .enabled }
                    }
                if loginNeedsApproval {
                    Button(model.text("Anmeldeobjekt in Systemeinstellungen erlauben …", "Approve login item in System Settings …")) { SMAppService.openSystemSettingsLoginItems() }
                }
            }
            Section(model.text("Empfangen", "Receive")) {
                LabeledContent(model.text("Zielordner", "Destination")) {
                    VStack(alignment: .trailing, spacing: 8) {
                        Text(settings.receiveDirectory).lineLimit(2).truncationMode(.middle).font(.callout).textSelection(.enabled)
                        Button(model.text("Ordner auswählen …", "Choose folder …")) {
                            let panel = NSOpenPanel(); panel.canChooseFiles = false; panel.canChooseDirectories = true; panel.canCreateDirectories = true
                            panel.begin { response in if response == .OK, let url = panel.url { settings.receiveDirectory = url.path } }
                        }
                    }
                }
                Text(model.text("Originaldateien werden beim Senden nicht verschoben oder verändert.", "Sending never moves or changes your original files.")).font(.caption).foregroundStyle(.secondary)
            }
            Section {
                LabeledContent(model.text("Schnell senden", "Quick send"), value: "⇧⌘L")
                Text(model.text("Im Finder: Rechtsklick → Dienste → Mit AirLink senden. Die App läuft beim Schließen des Fensters in der Menüleiste weiter.", "In Finder: right-click → Services → Send with AirLink. Closing the window keeps the app running in the menu bar.")).font(.caption).foregroundStyle(.secondary)
            }
            HStack { Text("AirLink 0.1.0").font(.caption).foregroundStyle(.secondary); Spacer(); Button(model.text("Speichern", "Save")) { model.update(settings) }.buttonStyle(.borderedProminent).disabled(model.status == nil || settings.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) }
        }
        .formStyle(.grouped).padding(8)
        .onAppear { if let current = model.status?.settings { settings = current } }
    }
}
