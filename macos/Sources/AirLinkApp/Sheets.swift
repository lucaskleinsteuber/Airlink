import AppKit
import AirLinkCore
import SwiftUI

/// First start: a name and a folder, both prefilled. One button.
struct WelcomeView: View {
    @EnvironmentObject var model: AppModel
    @State private var name = ""
    @State private var folder = ""
    @State private var login = true
    @State private var notifications = true
    @State private var working = false
    @State private var issue: String?

    var body: some View {
        ScrollView {
            VStack(spacing: 26) {
                Image(nsImage: NSApp.applicationIconImage).resizable().frame(width: 104, height: 104).shadow(color: .black.opacity(0.15), radius: 12, y: 6)
                VStack(spacing: 10) {
                    Text(model.text("Willkommen bei AirLink", "Welcome to AirLink")).font(.largeTitle.weight(.bold))
                    Text(model.text("Schick Dateien direkt zwischen deinen Computern – Mac, Linux und Windows. Schnell, verschlüsselt und nur in deinem Netzwerk.", "Send files directly between your computers – Mac, Linux and Windows. Fast, encrypted and only on your network."))
                        .multilineTextAlignment(.center).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                }
                VStack(spacing: 0) {
                    row {
                        Text(model.text("Name dieses Macs", "Name of this Mac"))
                        Spacer()
                        TextField("", text: $name).textFieldStyle(.roundedBorder).frame(width: 220).multilineTextAlignment(.trailing)
                    }
                    Divider().padding(.leading, 16)
                    row {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(model.text("Empfangene Dateien", "Received files"))
                            Text((folder as NSString).abbreviatingWithTildeInPath).font(.caption).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                        }
                        Spacer()
                        Button(model.text("Ändern …", "Change …"), action: chooseFolder)
                    }
                    Divider().padding(.leading, 16)
                    row {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(model.text("Beim Anmelden starten", "Open at login"))
                            Text(model.text("Damit dich deine anderen Computer jederzeit erreichen.", "So your other computers can always reach you.")).font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Toggle("", isOn: $login).toggleStyle(.switch).labelsHidden()
                    }
                    Divider().padding(.leading, 16)
                    row {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(model.text("Mitteilungen", "Notifications"))
                            Text(model.text("Wenn Dateien ankommen oder jemand sich verbinden möchte.", "When files arrive or someone wants to connect.")).font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Toggle("", isOn: $notifications).toggleStyle(.switch).labelsHidden()
                    }
                }
                .glassPanel(radius: 14)
                if let issue { Text(issue).foregroundStyle(.red) }
                Button(action: finish) {
                    Text(model.text("Los geht’s", "Get Started")).frame(minWidth: 180)
                }
                .buttonStyle(.borderedProminent).controlSize(.extraLarge).keyboardShortcut(.defaultAction)
                .disabled(working || name.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            .padding(40).frame(maxWidth: 560).frame(maxWidth: .infinity)
        }
        .onAppear(perform: prefill)
    }
    private func row<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        HStack(spacing: 12) { content() }.padding(.horizontal, 16).padding(.vertical, 12)
    }
    private func prefill() {
        guard name.isEmpty, let settings = model.status?.settings else { return }
        let computer = Host.current().localizedName ?? ""
        name = settings.name.isEmpty || settings.name == "Mac" || settings.name == "AirLink" ? (computer.isEmpty ? settings.name : computer) : settings.name
        let downloads = FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask).first ?? FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Downloads")
        folder = SetupDraft.suggestedFolder(saved: settings.receiveDirectory, downloads: downloads)
        notifications = true
    }
    private func chooseFolder() {
        let panel = NSOpenPanel(); panel.canChooseFiles = false; panel.canChooseDirectories = true; panel.canCreateDirectories = true
        panel.directoryURL = URL(fileURLWithPath: folder, isDirectory: true)
        panel.begin { response in if response == .OK, let url = panel.url { folder = url.path } }
    }
    private func finish() {
        working = true; issue = nil
        Task {
            do { try await model.completeWelcome(name: name, folder: folder, launchAtLogin: login, notifications: notifications) }
            catch { issue = error.localizedDescription }
            working = false
        }
    }
}

/// "Where to?" for files that arrived without a recipient.
struct SendChooser: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let pending: PendingSend
    var body: some View {
        VStack(spacing: 20) {
            VStack(spacing: 6) {
                Text(model.text("Wohin senden?", "Send to?")).font(.title2.weight(.semibold))
                Text(summary).foregroundStyle(.secondary).lineLimit(2).truncationMode(.middle)
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 120, maximum: 140), spacing: 10)], spacing: 18) {
                ForEach(model.pairedDevices.filter(\.canReceive)) { device in
                    DeviceTile(kind: .paired(device), size: 78) { picked in
                        model.send(pending.urls, to: picked)
                        dismiss()
                    }
                }
            }
            Button(model.text("Abbrechen", "Cancel")) { dismiss() }.keyboardShortcut(.cancelAction).controlSize(.large)
        }
        .padding(28).frame(width: 480)
    }
    private var summary: String {
        let names = pending.urls.map(\.lastPathComponent)
        guard let first = names.first else { return "" }
        return names.count == 1 ? first : model.text("\(first) und \(names.count - 1) weitere", "\(first) and \(names.count - 1) more")
    }
}

/// What one paired computer may do, and removing it.
struct DeviceSettingsSheet: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let deviceID: String
    @State private var confirmRemove = false
    private var device: Device? { model.pairedDevices.first { $0.id == deviceID } }
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            if let device {
                HStack(spacing: 14) {
                    Image(systemName: DeviceSymbol.name(device)).font(.system(size: 24)).symbolRenderingMode(.hierarchical).foregroundStyle(.tint)
                        .frame(width: 56, height: 56).glassCircle(tint: Color.accentColor.opacity(0.1), interactive: false)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(device.name).font(.title2.weight(.semibold))
                        Text(device.online ? model.text("Verbunden", "Connected") : model.text("Gerade nicht erreichbar", "Not reachable right now")).foregroundStyle(.secondary)
                    }
                }
                DevicePermissions(device: device)
                HStack {
                    Button(model.text("Entfernen …", "Remove …"), role: .destructive) { confirmRemove = true }
                    Spacer()
                    Button(model.text("Fertig", "Done")) { dismiss() }.keyboardShortcut(.defaultAction).buttonStyle(.borderedProminent)
                }
            } else {
                Text(model.text("Dieser Computer ist nicht mehr verbunden.", "This computer is no longer connected.")).foregroundStyle(.secondary)
                HStack { Spacer(); Button(model.text("Schließen", "Close")) { dismiss() } }
            }
        }
        .padding(26).frame(width: 470)
        .confirmationDialog(model.text("\(device?.name ?? "") entfernen?", "Remove \(device?.name ?? "")?"), isPresented: $confirmRemove) {
            Button(model.text("Entfernen", "Remove"), role: .destructive) { if let device { model.remove(device) }; dismiss() }
        } message: {
            Text(model.text("Ihr könnt euch danach keine Dateien mehr schicken, bis ihr euch neu verbindet.", "You can no longer send each other files until you connect again."))
        }
    }
}

struct DevicePermissions: View {
    @EnvironmentObject var model: AppModel
    let device: Device
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            toggle(model.text("Dateien ohne Nachfrage annehmen", "Accept files without asking"), model.text("Aus: Du bestätigst jede Sendung einzeln.", "Off: you approve each transfer."), \.autoAccept).disabled(!device.canSend)
            Divider().padding(.leading, 14)
            toggle(model.text("Zwischenablage teilen", "Share clipboard"), model.text("Kopierten Text per Rechtsklick auf den Computer senden.", "Send copied text by right-clicking the computer."), \.canClipboard)
            Divider().padding(.leading, 14)
            toggle(model.text("Darf mir Dateien senden", "May send me files"), nil, \.canSend)
            Divider().padding(.leading, 14)
            toggle(model.text("Darf Dateien von mir empfangen", "May receive my files"), nil, \.canReceive)
        }.glassPanel(radius: 12)
    }
    private func toggle(_ title: String, _ detail: String?, _ key: WritableKeyPath<Device, Bool>) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                if let detail { Text(detail).font(.caption).foregroundStyle(.secondary) }
            }
            Spacer()
            Toggle("", isOn: Binding(get: { device[keyPath: key] }, set: { value in var changed = device; changed[keyPath: key] = value; model.update(changed) }))
                .toggleStyle(.switch).labelsHidden()
        }.padding(.horizontal, 14).padding(.vertical, 10)
    }
}

/// How AirLink works and what to do when something does not.
struct HelpView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                Text(model.text("So funktioniert AirLink", "How AirLink works")).font(.title2.weight(.semibold))
                VStack(alignment: .leading, spacing: 12) {
                    Step(number: 1, text: model.text("Installiere AirLink auf deinen Computern – Mac, Linux oder Windows.", "Install AirLink on your computers – Mac, Linux or Windows."))
                    Step(number: 2, text: model.text("Verbinde sie einmal: Computer anklicken, Code vergleichen, bestätigen.", "Connect them once: click the computer, compare the code, confirm."))
                    Step(number: 3, text: model.text("Zieh Dateien auf einen Computer. Sie landen dort direkt im Download-Ordner.", "Drag files onto a computer. They arrive directly in its Downloads folder."))
                }
                Text(model.text("Probleme lösen", "Troubleshooting")).font(.headline).padding(.top, 6)
                VStack(alignment: .leading, spacing: 10) {
                    help(model.text("Der andere Computer erscheint nicht", "The other computer does not appear"),
                         model.text("AirLink muss auf beiden Computern geöffnet sein, und beide müssen im selben Netzwerk sein. Gastnetze und manche Router trennen Geräte voneinander – dann hilft „Per IP-Adresse verbinden“ unter „Hinzufügen“.", "AirLink must be open on both computers, and both must be on the same network. Guest networks and some routers keep devices apart – then use “Connect by IP address” under “Add”."))
                    help(model.text("Verbinden klappt nicht (Linux-Firewall)", "Connecting does not work (Linux firewall)"),
                         model.text("Unter Linux blockiert oft eine Firewall eingehende Verbindungen. Starte das Verbinden dann auf dem Linux-Computer – so herum klappt es, und danach funktioniert das Senden in beide Richtungen. Oder gib AirLink dort frei: sudo ufw allow 43871/tcp", "On Linux a firewall often blocks incoming connections. Start connecting on the Linux computer instead – that direction works, and afterwards sending works both ways. Or allow AirLink there: sudo ufw allow 43871/tcp"))
                    help(model.text("Eine Übertragung hängt", "A transfer is stuck"),
                         model.text("AirLink macht automatisch weiter, sobald beide Computer wieder verbunden sind – auch nach einem Neustart. Was schon angekommen ist, wird nicht noch einmal gesendet.", "AirLink continues automatically as soon as both computers are connected again – even after a restart. What already arrived is not sent again."))
                    help(model.text("Wo landen meine Dateien?", "Where do my files go?"),
                         model.text("Im Empfangsordner, normalerweise „Downloads“. Ändern kannst du ihn in den Einstellungen.", "In the receive folder, usually Downloads. You can change it in Settings."))
                }
                if let own = model.localAddresses.first {
                    HStack {
                        Text(model.text("Adresse dieses Macs:", "This Mac's address:")).foregroundStyle(.secondary)
                        Text(own).fontWeight(.medium).textSelection(.enabled)
                    }.font(.callout).padding(.top, 4)
                }
                HStack {
                    Button(model.text("Empfangsordner öffnen", "Open Receive Folder")) { model.openReceiveFolder() }
                    Spacer()
                    Button(model.text("Fertig", "Done")) { dismiss() }.keyboardShortcut(.defaultAction).buttonStyle(.borderedProminent)
                }.padding(.top, 6)
            }.padding(28)
        }
        .frame(width: 520, height: 560)
    }
    private func help(_ title: String, _ text: String) -> some View {
        DisclosureGroup {
            Text(text).foregroundStyle(.secondary).textSelection(.enabled).fixedSize(horizontal: false, vertical: true).padding(.top, 6)
        } label: { Text(title).fontWeight(.medium) }
        .padding(12).glassPanel(radius: 10)
    }
}
