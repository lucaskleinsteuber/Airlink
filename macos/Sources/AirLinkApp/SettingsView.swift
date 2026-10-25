import AppKit
import AirLinkCore
import ServiceManagement
import SwiftUI

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.scenePhase) private var scenePhase
    @State private var settings = HostSettings()
    @State private var loginEnabled = SMAppService.mainApp.status == .enabled
    @State private var loginNeedsApproval = SMAppService.mainApp.status == .requiresApproval
    @State private var recording = false
    @State private var settingsError: String?
    @State private var saving = false
    @State private var sendEnabled = false
    @State private var receiveEnabled = false
    @State private var sendAmount = "1"
    @State private var receiveAmount = "1"
    @State private var sendUnit: Int64 = 1_048_576
    @State private var receiveUnit: Int64 = 1_048_576
    private var sendLimit: Int64? { RateLimit.parse(sendAmount, unit: sendUnit, enabled: sendEnabled) }
    private var receiveLimit: Int64? { RateLimit.parse(receiveAmount, unit: receiveUnit, enabled: receiveEnabled) }
    var body: some View {
        Form {
            Section(model.text("Allgemein", "General")) {
                TextField(model.text("Gerätename", "Device name"), text: $settings.name)
                Text(model.text("So erscheint dieser Mac auf deinen anderen Geräten.", "This is how this Mac appears on your other devices.")).font(.caption).foregroundStyle(.secondary)
                Picker(model.text("Darstellung", "Appearance"), selection: $settings.appearance) {
                    Text(model.text("System", "System")).tag("system"); Text(model.text("Hell", "Light")).tag("light"); Text(model.text("Dunkel", "Dark")).tag("dark")
                }
                Picker(model.text("Sprache", "Language"), selection: $settings.language) {
                    Text(model.text("System", "System")).tag("system")
                    Text("Deutsch").tag("de")
                    Text("English").tag("en")
                }
                Toggle(model.text("Benachrichtigungen", "Notifications"), isOn: Binding(get: { settings.notifications }, set: { enabled in settings.notifications = enabled; if enabled { model.notificationPermission() } }))
                if settings.notifications {
                    Button(model.text("Benachrichtigungen im System erlauben …", "Allow system notifications …")) { model.notificationPermission() }
                }
                Toggle(model.text("Beim Anmelden starten", "Launch at login"), isOn: Binding(get: { loginEnabled }, set: setLogin))
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
                Text(model.text("Neue Dateien landen direkt im gewählten Ordner. Gleichnamige Dateien erhalten einen Namenszusatz. Bisherige Transfers behalten ihren Speicherort.", "New files arrive directly in the chosen folder. Name collisions receive a suffix. Previous transfers keep their existing locations.")).font(.caption).foregroundStyle(.secondary)
                Button(model.text("Einrichtung erneut öffnen …", "Open setup again …")) { model.showSetup = true; model.showMainWindow() }
            }
            OrganizationSection(organization: $settings.organization).environmentObject(model)
            Section(model.text("Geschwindigkeit", "Speed")) {
                RateLimitEditor(title: model.text("Senden begrenzen", "Limit sending"), enabled: $sendEnabled, amount: $sendAmount, unit: $sendUnit)
                RateLimitEditor(title: model.text("Empfangen begrenzen", "Limit receiving"), enabled: $receiveEnabled, amount: $receiveAmount, unit: $receiveUnit)
                Text(model.text("Gilt pro Richtung gemeinsam für Dateien, Freigaben und Browser-Relays. Ohne Limit wird die verfügbare Verbindung genutzt.", "Each limit applies across files, shares, and browser relays in that direction. Unlimited uses the available connection.")).font(.caption).foregroundStyle(.secondary)
                if sendLimit == nil || receiveLimit == nil { Text(model.text("Ein Limit muss zwischen 1 KiB/s und 1 TiB/s liegen.", "A limit must be between 1 KiB/s and 1 TiB/s.")).font(.caption).foregroundStyle(.red) }
            }
            Section(model.text("Schnell senden", "Quick send")) {
                Toggle(model.text("Globale Tastenkombination", "Global shortcut"), isOn: $settings.shortcut.enabled)
                HStack {
                    Text(ShortcutLabel.string(settings.shortcut)).font(.system(.body, design: .monospaced))
                    Spacer()
                    Button(recording ? model.text("Abbrechen", "Cancel") : model.text("Aufnehmen …", "Record …")) { setRecording(!recording) }.disabled(!settings.shortcut.enabled || saving || !model.isReady)
                    Button(model.text("Standard", "Default")) { setRecording(false); settings.shortcut = Shortcut() }.disabled(saving || !model.isReady)
                }
                if recording {
                    ShortcutRecorder(captured: { shortcut in settings.shortcut = shortcut; setRecording(false) }, cancelled: { setRecording(false) }, invalid: { settingsError = model.text("Bitte ⌘ oder ⌃ zusammen mit einer Taste drücken.", "Press a key together with Command or Control.") })
                        .frame(height: 44).overlay(Text(model.text("Jetzt Tastenkombination drücken · Esc bricht ab", "Press shortcut now · Esc cancels")).font(.caption).allowsHitTesting(false))
                        .background(.blue.opacity(0.08), in: RoundedRectangle(cornerRadius: 8))
                }
                Text(model.shortcutRecording ? model.text("Während der Aufnahme vorübergehend ausgesetzt", "Temporarily suspended while recording") : model.registeredShortcut.enabled ? model.text("Aktiv: ", "Active: ") + ShortcutLabel.string(model.registeredShortcut) : model.text("Aktuell deaktiviert", "Currently disabled")).font(.caption).foregroundStyle(.secondary)
                if let issue = model.shortcutError {
                    Text(issue).font(.caption).foregroundStyle(.red)
                    Button(model.text("Hinweis schließen", "Dismiss notice")) { model.shortcutError = nil }.font(.caption)
                }
                Text(model.text("⌘ oder ⌃ ist erforderlich; ⇧ und ⌥ sind optional. Die neue Kombination wird erst beim Speichern aktiviert.", "Command or Control is required; Shift and Option are optional. A new shortcut activates only when saved.")).font(.caption).foregroundStyle(.secondary)
                Text(model.text("Im Finder: Rechtsklick → Dienste → Mit AirLink senden. Die App läuft beim Schließen des Fensters in der Menüleiste weiter.", "In Finder: right-click → Services → Send with AirLink. Closing the window keeps the app running in the menu bar.")).font(.caption).foregroundStyle(.secondary)
            }
            if !model.isReady { HelperAvailabilityView().environmentObject(model) }
            if let settingsError { Text(settingsError).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
            HStack { Text("AirLink \(model.version)").font(.caption).foregroundStyle(.secondary); Spacer(); Button(saving ? model.text("Wird gespeichert …", "Saving …") : model.text("Speichern", "Save")) { save() }.buttonStyle(.borderedProminent).disabled(!model.isReady || settings.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || sendLimit == nil || receiveLimit == nil || recording || saving || !settings.shortcut.isValid || !settings.organization.isValid) }
        }
        .formStyle(.grouped).padding(8).disabled(saving)
        .onAppear { loadSettings(); refreshLogin() }
        .onDisappear { setRecording(false) }
        .onChange(of: scenePhase) { _, phase in if phase == .active { refreshLogin() } else { setRecording(false) } }
        .onChange(of: model.status?.id) { _, _ in if settings.name.isEmpty { loadSettings() } }
        .onChange(of: model.isReady) { _, ready in if !ready { setRecording(false) } }
        .onChange(of: model.recoveredReceiveFolder) { _, folder in if let folder { settings.receiveDirectory = folder.path } }
        .onChange(of: settings.shortcut.enabled) { _, enabled in if !enabled { setRecording(false) } }
    }
    private func setRecording(_ value: Bool) { recording = value; model.captureShortcut(value) }
    private func refreshLogin() {
        let status = SMAppService.mainApp.status
        loginEnabled = status == .enabled || status == .requiresApproval
        loginNeedsApproval = status == .requiresApproval
    }
    private func setLogin(_ enabled: Bool) {
        do {
            if enabled { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
            refreshLogin()
        } catch { settingsError = error.localizedDescription; refreshLogin() }
    }
    private func loadSettings() {
        guard let current = model.status?.settings else { return }
        settings = current
        sendEnabled = current.sendLimitBytesPerSecond != 0; receiveEnabled = current.receiveLimitBytesPerSecond != 0
        sendUnit = 1_048_576; receiveUnit = 1_048_576
        sendAmount = sendEnabled ? String(Double(current.sendLimitBytesPerSecond) / Double(sendUnit)) : "1"
        receiveAmount = receiveEnabled ? String(Double(current.receiveLimitBytesPerSecond) / Double(receiveUnit)) : "1"
    }
    private func save() {
        guard let sendLimit, let receiveLimit else { return }
        var draft = settings; draft.sendLimitBytesPerSecond = sendLimit; draft.receiveLimitBytesPerSecond = receiveLimit
        saving = true; settingsError = nil
        Task {
            do { try await model.saveSettings(draft); settings = draft }
            catch { settingsError = error.localizedDescription }
            saving = false
        }
    }
}

private struct RateLimitEditor: View {
    let title: String
    @Binding var enabled: Bool
    @Binding var amount: String
    @Binding var unit: Int64
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Toggle(title, isOn: $enabled)
            if enabled {
                HStack {
                    TextField("1", text: $amount).textFieldStyle(.roundedBorder).frame(width: 110)
                    Picker("", selection: $unit) { Text("KiB/s").tag(Int64(1024)); Text("MiB/s").tag(Int64(1_048_576)) }.labelsHidden().frame(width: 110)
                }
            }
        }
    }
}
