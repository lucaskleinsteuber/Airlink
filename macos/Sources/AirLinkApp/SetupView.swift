import AppKit
import AirLinkCore
import SwiftUI

struct SetupView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var draft = SetupDraft(name: "", receiveDirectory: "")
    @State private var step = 0
    @State private var saving = false
    @State private var issue: String?
    @State private var connectNext = true
    @FocusState private var nameFocused: Bool
    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            HStack {
                Image(systemName: step == 0 ? "laptopcomputer" : step == 1 ? "folder.fill" : "checkmark.seal").font(.system(size: 34, weight: .light)).foregroundStyle(.blue)
                Spacer()
                Text(model.text("Einrichtung", "Setup") + " · \(step + 1)/3").font(.callout).foregroundStyle(.secondary)
            }
            VStack(alignment: .leading, spacing: 10) {
                Text(step == 0 ? model.text("Wie heißt dieser Mac?", "What is this Mac called?") : step == 1 ? model.text("Wohin sollen Dateien?", "Where should files arrive?") : model.text("Bereit zum Teilen", "Ready to share")).font(.largeTitle.weight(.semibold))
                Text(step == 0 ? model.text("Mit diesem Namen erkennen deine anderen Geräte den Mac in AirLink.", "Your other devices will recognize this Mac by this name in AirLink.") : step == 1 ? model.text("Empfangene Dateien landen direkt in deinem gewählten Ordner. Bei gleichen Namen ergänzt AirLink eine Nummer.", "Received files arrive directly in your chosen folder. AirLink adds a number when a name is already in use.") : model.text("Prüfe deine Auswahl. Danach verbindest du den anderen Computer einmal und kannst Dateien senden.", "Review your choices. Then connect the other computer once to start sharing files.")).foregroundStyle(.secondary)
            }
            if step == 0 {
                VStack(alignment: .leading, spacing: 8) {
                    TextField(model.text("Zum Beispiel: Mein MacBook", "For example: My MacBook"), text: $draft.name).font(.title3).textFieldStyle(.roundedBorder).focused($nameFocused)
                    Text(model.text("Später jederzeit in den Einstellungen änderbar.", "You can change this later in Settings.")).font(.caption).foregroundStyle(.secondary)
                }
            } else if step == 1 {
                VStack(alignment: .leading, spacing: 14) {
                    HStack(spacing: 14) {
                        Image(systemName: "folder").font(.title)
                        VStack(alignment: .leading, spacing: 4) {
                            Text(URL(fileURLWithPath: draft.receiveDirectory).lastPathComponent).font(.headline)
                            Text((draft.receiveDirectory as NSString).abbreviatingWithTildeInPath).font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
                        }
                        Spacer()
                        Button(model.text("Ändern …", "Change …"), action: chooseFolder)
                    }.padding(18).background(Color.secondary.opacity(0.06), in: RoundedRectangle(cornerRadius: 12))
                    Text(model.text("Gerätename: ", "Device name: ") + draft.normalizedName).font(.callout)
                    Text(model.text("Bisher empfangene Dateien bleiben an ihrem bisherigen Speicherort.", "Previously received files keep their existing locations.")).font(.caption).foregroundStyle(.secondary)
                }
            } else {
                VStack(alignment: .leading, spacing: 16) {
                    LabeledContent(model.text("Gerätename", "Device name"), value: draft.normalizedName)
                    LabeledContent(model.text("Empfangsordner", "Receive folder")) {
                        Text((draft.receiveDirectory as NSString).abbreviatingWithTildeInPath).lineLimit(2).truncationMode(.middle).textSelection(.enabled)
                    }
                    Divider()
                    Toggle(model.text("Bei Empfang und Anfragen benachrichtigen", "Notify about received files and requests"), isOn: Binding(get: { draft.notifications }, set: { enabled in draft.notifications = enabled; if enabled { model.notificationPermission() } }))
                    if draft.notifications {
                        Button(model.text("Benachrichtigungen im System erlauben …", "Allow system notifications …")) { model.notificationPermission() }
                        Text(model.text("macOS benötigt dafür deine Freigabe. Du kannst sie auch später in den Einstellungen erteilen.", "macOS needs your permission for notifications. You can also allow them later in Settings.")).font(.caption).foregroundStyle(.secondary)
                    }
                    Toggle(model.text("Danach ein Gerät hinzufügen", "Add a device next"), isOn: $connectNext)
                    Text(model.text("Deine Auswahl lässt sich jederzeit in den geordneten Einstellungen ändern.", "You can change these choices in Settings at any time.")).font(.caption).foregroundStyle(.secondary)
                }.padding(18).background(Color.secondary.opacity(0.06), in: RoundedRectangle(cornerRadius: 12))
            }
            if let issue { Text(issue).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
            HStack {
                if step > 0 { Button(model.text("Zurück", "Back")) { step -= 1 } }
                Button(model.text("Später", "Later")) { dismiss() }.foregroundStyle(.secondary)
                Spacer()
                if saving { ProgressView().controlSize(.small) }
                Button(step < 2 ? model.text("Weiter", "Continue") : model.text("Einrichtung abschließen", "Finish setup")) {
                    if step < 2 { step += 1; nameFocused = false }
                    else { finish() }
                }.buttonStyle(.borderedProminent).keyboardShortcut(.defaultAction).disabled(!draft.validName || (step > 0 && !draft.validFolder))
            }
        }.padding(32).frame(width: 520).disabled(saving).interactiveDismissDisabled(saving)
            .onAppear {
                let downloads = FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask).first ?? FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Downloads")
                draft = SetupDraft(name: SetupDraft.suggestedName(saved: model.status?.settings.name ?? "", fallback: model.text("Mein Mac", "My Mac")), receiveDirectory: SetupDraft.suggestedFolder(saved: model.status?.settings.receiveDirectory ?? "", downloads: downloads), notifications: model.status?.settings.notifications ?? false)
                connectNext = model.status?.devices.contains(where: \.paired) != true
                nameFocused = true
            }
    }
    private func chooseFolder() {
        let panel = NSOpenPanel(); panel.canChooseFiles = false; panel.canChooseDirectories = true; panel.canCreateDirectories = true
        panel.directoryURL = URL(fileURLWithPath: draft.receiveDirectory)
        panel.begin { response in if response == .OK, let url = panel.url { draft.receiveDirectory = url.path } }
    }
    private func finish() {
        saving = true; issue = nil
        Task {
            do { try await model.completeSetup(draft, connectNext: connectNext); dismiss() }
            catch { issue = error.localizedDescription }
            saving = false
        }
    }
}
