import AppKit
import AirLinkCore
import ServiceManagement
import SwiftUI

private enum SettingsPane: String, CaseIterable {
    case general, receive, transfer, shortcuts, updates, about
    var icon: String { switch self { case .general: "gearshape"; case .receive: "tray.and.arrow.down"; case .transfer: "speedometer"; case .shortcuts: "keyboard"; case .updates: "arrow.triangle.2.circlepath"; case .about: "info.circle" } }
    var titles: (String, String) { switch self { case .general: ("Allgemein", "General"); case .receive: ("Empfangen", "Receiving"); case .transfer: ("Übertragungen", "Transfers"); case .shortcuts: ("Kurzbefehle", "Shortcuts"); case .updates: ("Updates", "Updates"); case .about: ("Info", "About") } }
}

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.scenePhase) private var scenePhase
    @State private var settings = HostSettings()
    @State private var baseline = HostSettings()
    @State private var loaded = false
    @State private var pane = SettingsPane.general
    @State private var saved = false
    @State private var loginEnabled = SMAppService.mainApp.status == .enabled
    @State private var loginNeedsApproval = SMAppService.mainApp.status == .requiresApproval
    @State private var recording = false
    @State private var advancedOrganization = false
    @ObservedObject private var updater = AirLinkUpdater.shared
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
    private var candidate: HostSettings {
        var result = settings
        if let sendLimit { result.sendLimitBytesPerSecond = sendLimit }
        if let receiveLimit { result.receiveLimitBytesPerSecond = receiveLimit }
        return result
    }
    private var dirty: Bool { loaded && (candidate != baseline || sendLimit == nil || receiveLimit == nil) }
    private var validName: Bool { SetupDraft(name: settings.name, receiveDirectory: settings.receiveDirectory).validName }
    private var validationIssue: String? {
        if !validName { return model.text("Prüfe den Gerätenamen unter Allgemein.", "Check the device name under General.") }
        if !SetupDraft(name: settings.name, receiveDirectory: settings.receiveDirectory).validFolder { return model.text("Wähle einen gültigen Zielordner unter Empfangen.", "Choose a valid destination under Receiving.") }
        if sendLimit == nil || receiveLimit == nil { return model.text("Prüfe die Geschwindigkeitslimits unter Übertragung.", "Check the speed limits under Transfers.") }
        if !settings.shortcut.isValid { return model.text("Prüfe die Tastenkombination unter Kurzbefehle.", "Check the keyboard shortcut under Shortcuts.") }
        if !settings.organization.isValid { return model.text("Prüfe die Sortierregeln unter Empfangen.", "Check the organization rules under Receiving.") }
        return nil
    }
    var body: some View {
        HStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 5) {
                Text("AirLink").font(.title3.weight(.semibold)).padding(.horizontal, 10).padding(.bottom, 18)
                ForEach(SettingsPane.allCases, id: \.self) { item in
                    Button { setRecording(false); pane = item } label: {
                        Label(model.text(item.titles.0, item.titles.1), systemImage: item.icon)
                            .frame(maxWidth: .infinity, alignment: .leading).padding(10)
                            .background(pane == item ? Color.accentColor.opacity(0.13) : .clear, in: RoundedRectangle(cornerRadius: 8))
                    }.buttonStyle(.plain).disabled(saving)
                }
                Spacer()
                Text(model.version).font(.caption).foregroundStyle(.secondary).padding(10)
            }.padding(14).frame(width: 175).background(Color(nsColor: .underPageBackgroundColor))
            Divider()
            VStack(alignment: .leading, spacing: 0) {
                Text(model.text(pane.titles.0, pane.titles.1)).font(.title2.weight(.semibold)).padding(24)
        Form {
            if pane == .general {
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
                Text(model.text("Der Start beim Anmelden wird sofort übernommen.", "Launch at login changes apply immediately.")).font(.caption).foregroundStyle(.secondary)
                if loginNeedsApproval {
                    Button(model.text("Anmeldeobjekt in Systemeinstellungen erlauben …", "Approve login item in System Settings …")) { SMAppService.openSystemSettingsLoginItems() }
                }
            }
            }
            if pane == .receive {
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
            DisclosureGroup(model.text("Erweiterte Dateiorganisation", "Advanced file organization"), isExpanded: $advancedOrganization) {
                OrganizationSection(organization: $settings.organization).environmentObject(model)
            }
            }
            if pane == .transfer {
            Section(model.text("Geschwindigkeit", "Speed")) {
                RateLimitEditor(title: model.text("Senden begrenzen", "Limit sending"), enabled: $sendEnabled, amount: $sendAmount, unit: $sendUnit)
                RateLimitEditor(title: model.text("Empfangen begrenzen", "Limit receiving"), enabled: $receiveEnabled, amount: $receiveAmount, unit: $receiveUnit)
                Text(model.text("Gilt pro Richtung gemeinsam für Dateien, Freigaben und Browser-Relays. Ohne Limit wird die verfügbare Verbindung genutzt.", "Each limit applies across files, shares, and browser relays in that direction. Unlimited uses the available connection.")).font(.caption).foregroundStyle(.secondary)
                if sendLimit == nil || receiveLimit == nil { Text(model.text("Ein Limit muss zwischen 1 KiB/s und 1 TiB/s liegen.", "A limit must be between 1 KiB/s and 1 TiB/s.")).font(.caption).foregroundStyle(.red) }
            }
            }
            if pane == .shortcuts {
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
            }
            if pane == .updates {
                Section(model.text("Beta-Updates", "Beta updates")) {
                    LabeledContent(model.text("Installierte Version", "Installed version"), value: model.version)
                    Toggle(model.text("Über neue Betas informieren", "Notify about new betas"), isOn: $settings.automaticUpdateChecks)
                    Text(model.text("Optional, höchstens einmal pro 24 Stunden. Installiert wird erst auf deinen Klick. Ohne diese Option prüft AirLink nur auf Anfrage.", "Optional, at most once every 24 hours. Updates install only when you choose. Without this option, checks happen only on request.")).font(.caption).foregroundStyle(.secondary)
                    Button(model.text("Nach Updates suchen …", "Check for Updates …")) { updater.checkForUpdates() }.disabled(!updater.canCheckForUpdates)
                    Link(model.text("Betas auf GitHub", "Betas on GitHub"), destination: URL(string: "https://github.com/lucaskleinsteuber/Airlink/releases")!)
                }
            }
            if pane == .about {
                Section("AirLink") {
                    LabeledContent(model.text("Version", "Version"), value: model.version)
                    Text(model.text("Dateien direkt in deinem Netzwerk teilen. Die Übertragung ist verschlüsselt und wird auf Dateiintegrität geprüft.", "Share files directly on your network. Transfers are encrypted and checked for file integrity.")).foregroundStyle(.secondary)
                    Link(model.text("Entwicklungsstände herunterladen …", "Download development builds …"), destination: URL(string: "https://github.com/lucaskleinsteuber/Airlink/releases")!)
                    Text("Copyright © 2026 Lucas Kleinsteuber · MIT").font(.caption).foregroundStyle(.secondary)
                }
            }
            if !model.isReady { HelperAvailabilityView().environmentObject(model) }
        }
        .formStyle(.grouped).padding(8).disabled(saving)
                Divider()
                VStack(alignment: .leading, spacing: 10) {
                    if let settingsError { Text(settingsError).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
                    if loaded, let validationIssue { Text(validationIssue).font(.caption).foregroundStyle(.red) }
                    HStack {
                        Text(dirty ? model.text("Ungespeicherte Änderungen", "Unsaved changes") : saved ? model.text("Gespeichert", "Saved") : model.text("Änderungen mit Speichern übernehmen", "Choose Save to apply changes")).font(.caption).foregroundStyle(.secondary)
                        Spacer()
                        if dirty { Button(model.text("Verwerfen", "Discard")) { setRecording(false); loadSettings(); settingsError = nil; saved = false }.disabled(saving) }
                        Button(saving ? model.text("Wird gespeichert …", "Saving …") : model.text("Speichern", "Save")) { save() }.buttonStyle(.borderedProminent)
                            .disabled(!model.isReady || !dirty || validationIssue != nil || recording || saving)
                    }
                }.padding(20)
            }.frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .onAppear { if let current = model.status?.settings { synchronizeSettings(current) }; refreshLogin() }
        .onDisappear { setRecording(false) }
        .onChange(of: scenePhase) { _, phase in if phase == .active { refreshLogin() } else { setRecording(false) } }
        .onChange(of: model.status?.id) { _, _ in if !loaded { loadSettings() } }
        .onChange(of: model.status?.settings) { _, current in if let current { synchronizeSettings(current) } }
        .onChange(of: model.isReady) { _, ready in if !ready { setRecording(false) } }
        .onChange(of: model.recoveredReceiveFolder) { _, folder in if let folder { settings.receiveDirectory = folder.path } }
        .onChange(of: settings.shortcut.enabled) { _, enabled in if !enabled { setRecording(false) } }
    }
    private func setRecording(_ value: Bool) {
        guard recording != value else { return }
        recording = value; model.captureShortcut(value)
    }
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
    private func loadSettings(_ confirmed: HostSettings? = nil) {
        guard let current = confirmed ?? model.status?.settings else { return }
        settings = current
        baseline = current; loaded = true
        sendEnabled = current.sendLimitBytesPerSecond != 0; receiveEnabled = current.receiveLimitBytesPerSecond != 0
        sendUnit = 1_048_576; receiveUnit = 1_048_576
        sendAmount = sendEnabled ? String(Double(current.sendLimitBytesPerSecond) / Double(sendUnit)) : "1"
        receiveAmount = receiveEnabled ? String(Double(current.receiveLimitBytesPerSecond) / Double(receiveUnit)) : "1"
    }
    private func synchronizeSettings(_ current: HostSettings) {
        guard !saving else { return }
        guard loaded, dirty else { loadSettings(current); return }
        let adoptSend = sendLimit == baseline.sendLimitBytesPerSecond
        let adoptReceive = receiveLimit == baseline.receiveLimitBytesPerSecond
        settings = SettingsEditing.merge(baseline: baseline, draft: candidate, latest: current)
        baseline = current
        // Keep raw invalid/in-progress edits; only untouched rate fields follow
        // external changes such as another settings surface or setup completion.
        if adoptSend {
            sendEnabled = current.sendLimitBytesPerSecond != 0
            sendAmount = sendEnabled ? String(Double(current.sendLimitBytesPerSecond) / Double(sendUnit)) : "1"
        }
        if adoptReceive {
            receiveEnabled = current.receiveLimitBytesPerSecond != 0
            receiveAmount = receiveEnabled ? String(Double(current.receiveLimitBytesPerSecond) / Double(receiveUnit)) : "1"
        }
    }
    private func save() {
        guard let sendLimit, let receiveLimit else { return }
        guard let latest = model.status?.settings else { return }
        var draft = settings; draft.sendLimitBytesPerSecond = sendLimit; draft.receiveLimitBytesPerSecond = receiveLimit
        draft = SettingsEditing.merge(baseline: baseline, draft: draft, latest: latest)
        saving = true; settingsError = nil
        Task {
            do { let confirmed = try await model.saveSettings(draft); loadSettings(confirmed); saved = true }
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
