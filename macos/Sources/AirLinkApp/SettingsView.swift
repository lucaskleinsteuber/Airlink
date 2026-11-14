import AppKit
import AirLinkCore
import ServiceManagement
import SwiftUI

/// Apple-style settings: changes apply at once, no Save button.
struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        TabView {
            GeneralSettings().tabItem { Label(model.text("Allgemein", "General"), systemImage: "gearshape") }
            DeviceSettingsList().tabItem { Label(model.text("Geräte", "Devices"), systemImage: "desktopcomputer") }
            AdvancedSettings().tabItem { Label(model.text("Erweitert", "Advanced"), systemImage: "slider.horizontal.3") }
        }
        .frame(width: 560, height: 470)
    }
}

private struct GeneralSettings: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.scenePhase) private var scenePhase
    @State private var name = ""
    @FocusState private var editingName: Bool
    var body: some View {
        Form {
            Section {
                TextField(model.text("Name dieses Macs", "Name of this Mac"), text: $name)
                    .focused($editingName)
                    .onSubmit(saveName)
                    .onChange(of: editingName) { _, editing in if !editing { saveName() } }
                LabeledContent(model.text("Empfangene Dateien", "Received files")) {
                    HStack {
                        Text(((model.status?.settings.receiveDirectory ?? "") as NSString).abbreviatingWithTildeInPath).lineLimit(1).truncationMode(.middle).foregroundStyle(.secondary)
                        Button(model.text("Ändern …", "Change …")) { model.chooseReceiveFolder() }
                    }
                }
            } footer: {
                Text(model.text("Unter diesem Namen sehen dich deine anderen Computer.", "Your other computers see you under this name.")).font(.caption).foregroundStyle(.secondary)
            }
            Section {
                Toggle(model.text("Beim Anmelden starten", "Open at login"), isOn: Binding(get: { model.launchAtLogin }, set: { model.setLaunchAtLogin($0) }))
                if model.launchNeedsApproval {
                    Button(model.text("In den Systemeinstellungen erlauben …", "Allow in System Settings …")) { SMAppService.openSystemSettingsLoginItems() }
                }
                Toggle(model.text("Mitteilungen", "Notifications"), isOn: Binding(get: { model.status?.settings.notifications ?? true }, set: { value in
                    if value { model.requestNotifications() }
                    model.updateSettings { $0.notifications = value }
                }))
                Picker(model.text("Sprache", "Language"), selection: Binding(get: { model.status?.settings.language ?? "system" }, set: { value in model.updateSettings { $0.language = value } })) {
                    Text(model.text("Wie das System", "System")).tag("system")
                    Text("Deutsch").tag("de")
                    Text("English").tag("en")
                }
            } footer: {
                Text(model.text("AirLink läuft nach dem Schließen des Fensters in der Menüleiste weiter, damit dich deine Computer erreichen.", "After you close the window, AirLink keeps running in the menu bar so your computers can reach you.")).font(.caption).foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .disabled(!model.isReady)
        .onAppear { name = model.status?.settings.name ?? ""; model.refreshLoginItem() }
        .onChange(of: model.status?.settings.name) { _, value in if !editingName, let value { name = value } }
        .onChange(of: scenePhase) { _, phase in if phase == .active { model.refreshLoginItem() } }
    }
    private func saveName() {
        let value = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let current = model.status?.settings.name, value != current else { return }
        guard SetupDraft(name: value, receiveDirectory: "/").validName else { name = current; return }
        model.updateSettings { $0.name = value }
    }
}

private struct DeviceSettingsList: View {
    @EnvironmentObject var model: AppModel
    @State private var removing: Device?
    var body: some View {
        Form {
            if model.pairedDevices.isEmpty {
                Text(model.text("Noch kein Computer verbunden. Öffne AirLink auf deinem anderen Computer und klick ihn im AirLink-Fenster an.", "No computer connected yet. Open AirLink on your other computer and click it in the AirLink window.")).foregroundStyle(.secondary)
            }
            ForEach(model.pairedDevices) { device in
                Section {
                    toggle(model.text("Dateien ohne Nachfrage annehmen", "Accept files without asking"), device, \.autoAccept).disabled(!device.canSend)
                    toggle(model.text("Zwischenablage teilen", "Share clipboard"), device, \.canClipboard)
                    toggle(model.text("Darf mir Dateien senden", "May send me files"), device, \.canSend)
                    toggle(model.text("Darf Dateien von mir empfangen", "May receive my files"), device, \.canReceive)
                    Button(model.text("Entfernen …", "Remove …"), role: .destructive) { removing = device }
                } header: {
                    Label {
                        Text(device.name) + Text(device.online ? "" : model.text(" · nicht erreichbar", " · not reachable")).foregroundColor(.secondary)
                    } icon: { Image(systemName: DeviceSymbol.name(device)) }
                }
            }
        }
        .formStyle(.grouped)
        .disabled(!model.isReady)
        .confirmationDialog(model.text("\(removing?.name ?? "") entfernen?", "Remove \(removing?.name ?? "")?"), isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } })) {
            Button(model.text("Entfernen", "Remove"), role: .destructive) { if let removing { model.remove(removing) }; removing = nil }
        } message: { Text(model.text("Ihr könnt euch danach keine Dateien mehr schicken, bis ihr euch neu verbindet.", "You can no longer send each other files until you connect again.")) }
    }
    private func toggle(_ title: String, _ device: Device, _ key: WritableKeyPath<Device, Bool>) -> some View {
        Toggle(title, isOn: Binding(get: { device[keyPath: key] }, set: { value in var changed = device; changed[keyPath: key] = value; model.update(changed) }))
    }
}

private struct AdvancedSettings: View {
    @EnvironmentObject var model: AppModel
    @ObservedObject private var updater = AirLinkUpdater.shared
    @State private var sendLimit = ""
    @State private var receiveLimit = ""
    @State private var recording = false
    var body: some View {
        Form {
            Section {
                limitRow(model.text("Senden begrenzen", "Limit sending"), text: $sendLimit, current: model.status?.settings.sendLimitBytesPerSecond ?? 0) { value in model.updateSettings { $0.sendLimitBytesPerSecond = value } }
                limitRow(model.text("Empfangen begrenzen", "Limit receiving"), text: $receiveLimit, current: model.status?.settings.receiveLimitBytesPerSecond ?? 0) { value in model.updateSettings { $0.receiveLimitBytesPerSecond = value } }
            } header: { Text(model.text("Geschwindigkeit", "Speed")) } footer: {
                Text(model.text("Leer lassen für volle Geschwindigkeit. Angabe in MB pro Sekunde.", "Leave empty for full speed. In MB per second.")).font(.caption).foregroundStyle(.secondary)
            }
            Section {
                Toggle(model.text("Tastenkürzel „Dateien senden“", "Shortcut “Send Files”"), isOn: Binding(get: { model.status?.settings.shortcut.enabled ?? false }, set: { value in model.updateSettings { $0.shortcut.enabled = value } }))
                if let shortcut = model.status?.settings.shortcut, shortcut.enabled {
                    LabeledContent(model.text("Kombination", "Keys")) {
                        HStack {
                            Text(ShortcutLabel.string(shortcut)).font(.body.monospaced())
                            Button(recording ? model.text("Abbrechen", "Cancel") : model.text("Ändern …", "Change …")) { setRecording(!recording) }
                        }
                    }
                    if recording {
                        ShortcutRecorder(captured: { value in var next = value; next.enabled = true; setRecording(false); model.updateSettings { $0.shortcut = next } },
                                         cancelled: { setRecording(false) },
                                         invalid: { model.shortcutError = model.text("Bitte ⌘ oder ⌃ zusammen mit einer Taste drücken.", "Press a key together with Command or Control.") })
                            .frame(height: 34)
                            .overlay(Text(model.text("Jetzt Tasten drücken · Esc bricht ab", "Press keys now · Esc cancels")).font(.caption).allowsHitTesting(false))
                    }
                }
                if let issue = model.shortcutError { Text(issue).font(.caption).foregroundStyle(.red) }
            } header: { Text(model.text("Tastenkürzel", "Shortcut")) } footer: {
                Text(model.text("Öffnet von überall die Dateiauswahl zum Senden. Im Finder geht es auch per Rechtsklick → Dienste → Mit AirLink senden.", "Opens the file picker from anywhere. In Finder you can also right-click → Services → Send with AirLink.")).font(.caption).foregroundStyle(.secondary)
            }
            Section(model.text("Updates", "Updates")) {
                LabeledContent(model.text("Version", "Version"), value: model.version)
                Toggle(model.text("Automatisch nach Updates suchen", "Check for updates automatically"), isOn: Binding(get: { model.status?.settings.automaticUpdateChecks ?? false }, set: { value in model.updateSettings { $0.automaticUpdateChecks = value } }))
                Button(model.text("Jetzt nach Updates suchen …", "Check Now …")) { updater.checkForUpdates() }.disabled(!updater.canCheckForUpdates)
            }
            Section(model.text("Verbindung", "Connection")) {
                LabeledContent(model.text("Adresse dieses Macs", "This Mac's address"), value: model.localAddresses.joined(separator: ", "))
                LabeledContent(model.text("Fingerabdruck", "Fingerprint")) {
                    Text(PeerConnectionDraft(fingerprint: model.status?.fingerprint ?? "").comparisonFingerprint).font(.caption.monospaced()).textSelection(.enabled).multilineTextAlignment(.trailing)
                }
                Button(model.text("Diagnosebericht sichern …", "Save Diagnostic Report …")) { Task { await model.exportDiagnostics() } }
            }
        }
        .formStyle(.grouped)
        .disabled(!model.isReady)
        .onAppear(perform: load)
        .onDisappear { setRecording(false) }
    }
    private func load() {
        func format(_ value: Int64) -> String { value == 0 ? "" : String(format: "%g", Double(value) / 1_000_000) }
        sendLimit = format(model.status?.settings.sendLimitBytesPerSecond ?? 0)
        receiveLimit = format(model.status?.settings.receiveLimitBytesPerSecond ?? 0)
    }
    private func limitRow(_ title: String, text: Binding<String>, current: Int64, apply: @escaping (Int64) -> Void) -> some View {
        LabeledContent(title) {
            HStack(spacing: 6) {
                TextField("", text: text, prompt: Text(model.text("unbegrenzt", "unlimited"))).frame(width: 100).multilineTextAlignment(.trailing)
                    .onSubmit {
                        let raw = text.wrappedValue.replacingOccurrences(of: ",", with: ".").trimmingCharacters(in: .whitespaces)
                        if raw.isEmpty { apply(0); return }
                        guard let mb = Double(raw), mb > 0, mb <= 1_000_000 else { load(); return }
                        apply(max(1024, Int64(mb * 1_000_000)))
                    }
                Text("MB/s").foregroundStyle(.secondary)
            }
        }
    }
    private func setRecording(_ value: Bool) {
        guard recording != value else { return }
        recording = value; model.captureShortcut(value)
    }
}
