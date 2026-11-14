import AppKit
import AirLinkCore
import SwiftUI

/// The main window: welcome on first start, otherwise the computers and
/// what happened recently.
struct HomeView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        Group {
            if !model.isReady {
                StartupView()
            } else if !model.setupComplete {
                WelcomeView()
            } else {
                ScrollView {
                    VStack(alignment: .leading, spacing: 26) {
                        Banners()
                        DeviceArea()
                        RecentArea()
                    }
                    .padding(.horizontal, 30).padding(.top, 22).padding(.bottom, 30)
                    .frame(maxWidth: 820).frame(maxWidth: .infinity)
                }
            }
        }
        .frame(minWidth: 560, minHeight: 500)
        .background(Backdrop())
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                Button { model.sheet = .help } label: { Label(model.text("Hilfe", "Help"), systemImage: "questionmark.circle") }
                    .help(model.text("So funktioniert AirLink", "How AirLink works"))
                Button { model.showHistory() } label: { Label(model.text("Verlauf", "History"), systemImage: "clock") }
                    .help(model.text("Alle Übertragungen", "All transfers")).disabled(!model.isReady || !model.setupComplete)
                SettingsLink { Label(model.text("Einstellungen", "Settings"), systemImage: "gearshape") }
                    .help(model.text("Einstellungen", "Settings"))
            }
        }
        .navigationTitle("AirLink")
        .toolbarBackground(.hidden, for: .windowToolbar)
        .sheet(item: $model.sheet) { sheet in
            Group {
                switch sheet {
                case .pairing(let id): PairingSheet(sessionID: id)
                case .addDevice(let byAddress): AddDeviceSheet(byAddress: byAddress)
                case .help: HelpView()
                case .send(let pending): SendChooser(pending: pending)
                case .device(let id): DeviceSettingsSheet(deviceID: id)
                }
            }.environmentObject(model)
        }
        .alert(model.text("Das hat nicht geklappt", "That did not work"), isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) {
            Button("OK") { model.error = nil }
        } message: { Text(model.error ?? "") }
        .onAppear {
            model.openWindow = { openWindow(id: $0) }
            model.setMainWindowVisible(true)
        }
        .onDisappear { model.setMainWindowVisible(false) }
    }
}

private extension View {
    @ViewBuilder func softScrollEdge() -> some View {
        if #available(macOS 26.0, *) { self.scrollEdgeEffectStyle(.soft, for: .top) } else { self }
    }
}

/// A calm colored backdrop so the glass tiles have something to refract.
struct Backdrop: View {
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        ZStack {
            Color(nsColor: .windowBackgroundColor)
            RadialGradient(colors: [Color.accentColor.opacity(scheme == .dark ? 0.22 : 0.13), .clear], center: .init(x: 0.2, y: 0.0), startRadius: 10, endRadius: 520)
            RadialGradient(colors: [Color.purple.opacity(scheme == .dark ? 0.14 : 0.07), .clear], center: .init(x: 0.95, y: 0.15), startRadius: 10, endRadius: 420)
        }.ignoresSafeArea()
    }
}

/// The computers: paired ones first, then nearby ones and "Add".
struct DeviceArea: View {
    @EnvironmentObject var model: AppModel
    private let columns = [GridItem(.adaptive(minimum: 140, maximum: 170), spacing: 14, alignment: .top)]
    var body: some View {
        let paired = model.pairedDevices, nearby = model.nearbyDevices
        VStack(alignment: .leading, spacing: 22) {
            VStack(alignment: .leading, spacing: 6) {
                Text(paired.isEmpty ? model.text("Verbinde deinen anderen Computer", "Connect your other computer") : model.text("Zieh Dateien auf einen Computer", "Drag files onto a computer"))
                    .font(.title2.weight(.semibold))
                Text(paired.isEmpty
                     ? model.text("Öffne AirLink auf dem anderen Computer. Er erscheint dann hier – klick ihn an und vergleicht einen Code. Das musst du nur einmal machen.", "Open AirLink on the other computer. It then appears here – click it and compare a code. You only do this once.")
                     : model.text("Oder klick ihn an, um Dateien auszuwählen. Alles bleibt in deinem Netzwerk.", "Or click it to choose files. Everything stays on your network."))
                    .foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            }
            LazyVGrid(columns: columns, alignment: .leading, spacing: 22) {
                ForEach(paired) { DeviceTile(kind: .paired($0)) }
                ForEach(nearby) { DeviceTile(kind: .nearby($0)) }
                if paired.isEmpty && nearby.isEmpty { DeviceTile(kind: .searching) }
                DeviceTile(kind: .add)
            }
            if paired.isEmpty {
                Button(model.text("Nicht gefunden? Per IP-Adresse verbinden …", "Not found? Connect by IP address …")) { model.sheet = .addDevice(byAddress: true) }
                    .buttonStyle(.link)
            }
        }
    }
}

struct StartupView: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        VStack(spacing: 18) {
            if model.lifecycle == .failed {
                Image(systemName: "exclamationmark.triangle").font(.system(size: 44)).foregroundStyle(.orange)
                Text(model.text("AirLink konnte nicht starten", "AirLink could not start")).font(.title2.weight(.semibold))
                Text(friendly).multilineTextAlignment(.center).foregroundStyle(.secondary).frame(maxWidth: 420)
                HStack {
                    Button(model.text("Anderen Empfangsordner wählen …", "Choose Another Receive Folder …")) { model.chooseReceiveFolder() }
                    Button(model.text("Noch einmal versuchen", "Try Again")) { Task { await model.start() } }.buttonStyle(.borderedProminent)
                }
                if let detail = model.engineError {
                    DisclosureGroup(model.text("Technische Details", "Technical details")) {
                        Text(detail).font(.caption.monospaced()).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                    }.frame(maxWidth: 420).font(.caption)
                }
            } else {
                ProgressView().controlSize(.large)
                Text(model.text("AirLink startet …", "Starting AirLink …")).foregroundStyle(.secondary)
            }
        }.padding(40).frame(maxWidth: .infinity, maxHeight: .infinity)
    }
    private var friendly: String {
        let detail = model.engineError ?? ""
        if detail.contains("already running") { return model.text("AirLink läuft schon in einem anderen Fenster oder Benutzerkonto. Beende die andere AirLink-App und versuch es noch einmal.", "AirLink is already running elsewhere. Quit the other AirLink app and try again.") }
        if detail.localizedCaseInsensitiveContains("receive") || detail.localizedCaseInsensitiveContains("directory") { return model.text("Der Empfangsordner ist nicht erreichbar. Wähle einen anderen Ordner, z. B. „Downloads“.", "The receive folder is not available. Choose another folder, such as Downloads.") }
        return model.text("Ein Neustart von AirLink hilft meistens. Wenn nicht, sichere unter Einstellungen → Erweitert einen Diagnosebericht.", "Restarting AirLink usually helps. If not, save a diagnostic report under Settings → Advanced.")
    }
}
