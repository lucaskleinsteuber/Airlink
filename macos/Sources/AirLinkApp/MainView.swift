import AppKit
import AirLinkCore
import CoreImage.CIFilterBuiltins
import SwiftUI
import UniformTypeIdentifiers

struct MainView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.openWindow) private var openWindow
    @State private var dropTarget = false
    @State private var dropDeviceID: String?
    @State private var showLocalConnection = false
    @State private var editingDevice: Device?
    @State private var showShares = false
    @State private var showShareCreator = false
    @State private var showClipboard = false
    @State private var showHistory = false
    @State private var showRelays = false
    var body: some View {
        NavigationSplitView {
            List(selection: $model.selectedDeviceID) {
                Section(model.text("Geräte", "Devices")) {
                    ForEach(pairedDevices) { device in deviceRow(device) }
                    if model.status?.devices.filter(\.paired).isEmpty != false {
                        Text(model.text("Noch keine Geräte", "No devices yet")).foregroundStyle(.secondary)
                    }
                }
                let nearby = model.status?.devices.filter { !$0.paired } ?? []
                if !nearby.isEmpty {
                    Section(model.text("In der Nähe", "Nearby")) {
                        ForEach(nearby) { device in
                            Button { model.showConnection(to: device) } label: { Label(device.name, systemImage: "antenna.radiowaves.left.and.right") }.disabled(!model.isReady)
                                .contextMenu { Text(device.address ?? "").textSelection(.enabled) }
                        }
                    }
                }
            }
            .navigationSplitViewColumnWidth(min: 270, ideal: 310, max: 460)
            .safeAreaInset(edge: .bottom) {
                VStack(alignment: .leading, spacing: 14) {
                    Button { model.showConnection = true } label: { Label(model.text("Gerät hinzufügen", "Add device"), systemImage: "plus.circle") }.disabled(!model.isReady || !model.setupComplete)
                    Divider()
                    Button { showHistory = true } label: { Label(model.text("Übertragungen", "Transfers"), systemImage: "clock") }.disabled(!model.isReady)
                    Menu {
                        Button("Quick Share") { showShares = true }
                        Button(model.text("Zwischenablage", "Clipboard")) { showClipboard = true }
                        Button(model.text("Browser-Relays", "Browser relays")) { showRelays = true }
                    } label: { Label(model.text("Weitere Funktionen", "More features"), systemImage: "ellipsis.circle") }.disabled(!model.isReady || !model.setupComplete)
                    SettingsLink { Label(model.text("Einstellungen", "Settings"), systemImage: "gearshape") }
                }.buttonStyle(.plain).padding(18).frame(maxWidth: .infinity, alignment: .leading)
            }
        } detail: {
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    title
                    if !model.isReady { HelperAvailabilityView().environmentObject(model) }
                    if model.setupRequired {
                        VStack(alignment: .leading, spacing: 14) {
                            Text(model.text("Willkommen bei AirLink", "Welcome to AirLink")).font(.title2.weight(.semibold))
                            Text(model.text("Gib diesem Mac einen verständlichen Namen und lege fest, wo empfangene Dateien landen.", "Give this Mac a recognizable name and choose where received files arrive.")).foregroundStyle(.secondary)
                            Button(model.text("AirLink einrichten", "Set up AirLink")) { model.showSetup = true }.buttonStyle(.borderedProminent)
                        }.padding(24).frame(maxWidth: .infinity, alignment: .leading).background(Color.accentColor.opacity(0.06), in: RoundedRectangle(cornerRadius: 16))
                    } else {
                    pairingRequests
                    failedDirectSends
                    dropArea
                    }
                    transfers
                }.padding(30).frame(maxWidth: 1000, alignment: .leading).frame(maxWidth: .infinity)
            }
            .background(Color(nsColor: .windowBackgroundColor))
            .toolbar {
                ToolbarItem { Button { model.openReceiveDirectory() } label: { Label(model.text("Empfangsordner", "Receive folder"), systemImage: "tray.and.arrow.down") }.labelStyle(.titleAndIcon).disabled(!model.isReady) }
                ToolbarItem { Button { showLocalConnection = true } label: { Label(model.text("Verbindung", "Connection"), systemImage: "qrcode") }.disabled(!model.isReady) }
            }
        }
        .sheet(isPresented: $model.showConnection) { ConnectView().environmentObject(model) }
        .sheet(isPresented: $model.showSetup) { SetupView().environmentObject(model) }
        .sheet(isPresented: $showLocalConnection) { LocalConnectionView().environmentObject(model) }
        .sheet(item: $editingDevice) { device in DeviceSettingsView(device: device).environmentObject(model) }
        .sheet(isPresented: $showShares) { ShareListView().environmentObject(model) }
        .sheet(isPresented: $showShareCreator) { ShareCreateView(initialPaths: model.pendingURLs).environmentObject(model) }
        .sheet(isPresented: $showClipboard) { ClipboardView().environmentObject(model) }
        .sheet(isPresented: $showHistory) { HistoryView().environmentObject(model) }
        .sheet(isPresented: $showRelays) { RelayOverview().environmentObject(model) }
        .alert(model.text("AirLink konnte die Aktion nicht ausführen", "AirLink could not complete the action"), isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) {
            Button("OK") { model.error = nil }
        } message: { Text(model.error ?? "") }
        .onAppear { model.openMainWindow = { openWindow(id: "airlink-main") } }
    }
    private var pairedDevices: [Device] {
        (model.status?.devices.filter(\.paired) ?? []).sorted {
            if $0.online != $1.online { return $0.online }
            let order = $0.name.localizedStandardCompare($1.name)
            return order == .orderedSame ? $0.id < $1.id : order == .orderedAscending
        }
    }
    private var directRecipient: Device? {
        guard let device = model.selectedDevice, device.paired, device.kind == "engine", device.canReceive else { return nil }
        return device
    }
    private var dropHint: String {
        if let recipient = directRecipient {
            return recipient.online ? model.text("Ablegen startet diese Sendung direkt. Empfangsbestätigungen richten sich nach den Geräteberechtigungen.", "Dropping starts this send directly. Receive approval follows the device permissions.") : model.text("Das Gerät ist offline. AirLink bereitet die Sendung vor und wartet auf die Verbindung.", "The device is offline. AirLink prepares the send and waits for a connection.")
        }
        if model.selectedDevice?.kind == "browser" {
            return model.text("Abgelegte Dateien werden ausgewählt. Mit Senden anschließend im Browser zum Download bereitstellen.", "Dropped files are selected first. Choose Send to make them available for download in the browser.")
        }
        if model.selectedDevice?.canReceive == false {
            return model.text("Dieses Gerät darf nicht empfangen. Wähle ein anderes Gerät oder ändere seine Berechtigungen.", "This device is not allowed to receive. Choose another device or change its permissions.")
        }
        return model.text("Wähle links ein Gerät. Oder lege Dateien direkt auf einem gekoppelten Computer ab.", "Choose a device on the left, or drop files directly onto a paired computer.")
    }
    private func deviceRow(_ device: Device) -> some View {
        let direct = device.kind == "engine" && device.canReceive
        return HStack(spacing: 10) {
            Image(systemName: device.kind == "browser" ? "globe" : "desktopcomputer").font(.title3).frame(width: 24)
            VStack(alignment: .leading, spacing: 2) {
                Text(device.name).fontWeight(.medium).lineLimit(2).fixedSize(horizontal: false, vertical: true).help(device.name)
                Text(dropDeviceID == device.id && direct ? model.text("Ablegen → direkt senden", "Drop → send directly") : device.online ? model.text("Verbunden", "Connected") : model.text("Nicht erreichbar", "Unavailable")).font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            Circle().fill(device.online ? .green : .secondary).frame(width: 6, height: 6)
        }
        .padding(.vertical, 9).tag(device.id)
        .background(dropDeviceID == device.id && direct ? Color.accentColor.opacity(0.13) : .clear, in: RoundedRectangle(cornerRadius: 8))
        .contextMenu { Button(model.text("Berechtigungen …", "Permissions …")) { editingDevice = device } }
        .onDrop(of: [UTType.fileURL], isTargeted: Binding(get: { dropDeviceID == device.id }, set: { entered in
            if entered { dropDeviceID = device.id }
            else if dropDeviceID == device.id { dropDeviceID = nil }
        })) { providers in
            guard direct else { return false }
            return model.receiveDrop(providers, to: device)
        }
    }
    @ViewBuilder private var failedDirectSends: some View {
        ForEach(model.failedDirectSends) { failed in
            VStack(alignment: .leading, spacing: 10) {
                Label(model.text("Sendung an \(failed.batch.deviceName) nicht bestätigt", "Send to \(failed.batch.deviceName) not confirmed"), systemImage: "exclamationmark.triangle").font(.headline).foregroundStyle(.orange)
                Text(failed.batch.urls.prefix(3).map(\.lastPathComponent).joined(separator: ", ")).font(.caption).foregroundStyle(.secondary)
                Text(failed.message).font(.callout).textSelection(.enabled)
                HStack {
                    Button(model.text("Erneut versuchen", "Retry")) { model.retryDirectSend(failed) }.buttonStyle(.borderedProminent).disabled(!model.isReady || model.sendingFiles)
                    Button(model.text("Verwerfen", "Dismiss")) { model.dismissDirectSend(failed.id) }.buttonStyle(.borderless).disabled(model.sendingFiles)
                }
            }.padding(16).frame(maxWidth: .infinity, alignment: .leading).background(.orange.opacity(0.07), in: RoundedRectangle(cornerRadius: 12))
        }
    }
    private var title: some View {
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 6) {
                Text(model.selectedDevice?.name ?? model.text("Einfach teilen.", "Share with ease.")).font(.largeTitle.weight(.semibold))
                Text(model.text("Dateien bleiben in deinem Netzwerk.", "Your files stay on your network.")).foregroundStyle(.secondary)
            }
            Spacer()
            if let device = model.selectedDevice {
                Button { editingDevice = device } label: { Image(systemName: "slider.horizontal.3") }.buttonStyle(.borderless)
            }
        }
    }
    @ViewBuilder private var pairingRequests: some View {
        ForEach(model.status?.pairRequests ?? []) { request in
            PairingApprovalView(request: request).environmentObject(model)
        }
    }
    private var dropArea: some View {
        VStack(spacing: 14) {
            Image(systemName: "arrow.up.doc").font(.system(size: 38, weight: .light)).foregroundStyle(.tint)
            Text(model.preparingDirectSend ? model.text("Sendung wird vorbereitet …", "Preparing send …") : directRecipient.map { model.text("Hier ablegen → an \($0.name) senden", "Drop here → send to \($0.name)") } ?? model.text("Dateien und Ordner hier ablegen", "Drop files and folders here")).font(.title3.weight(.medium))
            Text(dropHint).font(.callout).foregroundStyle(.secondary)
            if model.preparingDirectSend { ProgressView().controlSize(.small) }
            if let notice = model.sendNotice { Text(notice).font(.caption).foregroundStyle(.secondary) }
            if !model.pendingURLs.isEmpty {
                Text(model.text("Auswahl zum manuellen Senden: ", "Selection for manual send: ") + model.pendingURLs.prefix(3).map(\.lastPathComponent).joined(separator: ", ")).font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
            HStack {
                Button(model.text("Dateien auswählen …", "Choose files …")) { model.chooseFiles() }
                if !model.pendingURLs.isEmpty {
                    Button(model.text("Leeren", "Clear")) { model.pendingURLs = [] }.buttonStyle(.borderless)
                    Button(model.text("Freigabelink …", "Share link …")) { showShareCreator = true }.disabled(!model.isReady)
                    Button(model.sendingFiles ? model.text("Wird gesendet …", "Sending …") : model.text("Senden", "Send")) { model.send() }.buttonStyle(.borderedProminent)
                        .disabled(model.selectedDevice?.canReceive != true || !model.isReady || model.sendingFiles)
                }
            }
        }
        .padding(30).frame(maxWidth: .infinity, minHeight: 240)
        .background(dropTarget ? Color.accentColor.opacity(0.09) : Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 18))
        .overlay(RoundedRectangle(cornerRadius: 18).strokeBorder(dropTarget ? Color.accentColor : Color.secondary.opacity(0.22), style: StrokeStyle(lineWidth: 1, dash: [6, 5])))
        .onDrop(of: [UTType.fileURL], isTargeted: $dropTarget) { providers in
            model.receiveDrop(providers, to: directRecipient)
        }
    }
    private var transfers: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(model.text("Übertragungen", "Transfers")).font(.title2.weight(.semibold))
            let all = model.status?.transfers.sorted { $0.createdAt == $1.createdAt ? $0.id < $1.id : $0.createdAt > $1.createdAt } ?? []
            let lastActivity = Dictionary(uniqueKeysWithValues: all.map { ($0.id, ShareDate.parse($0.updatedAt) ?? ShareDate.parse($0.createdAt) ?? .distantPast) })
            let recent = all.filter { !$0.isActive }.sorted {
                let left = lastActivity[$0.id] ?? .distantPast
                let right = lastActivity[$1.id] ?? .distantPast
                return left == right ? $0.id < $1.id : left > right
            }
            let list = all.filter(\.isActive) + Array(recent.prefix(5))
            if list.isEmpty {
                Text(model.text("Hier erscheinen deine Übertragungen.", "Your transfers will appear here.")).foregroundStyle(.secondary).padding(.vertical, 14)
            }
            ForEach(list) { transfer in TransferRow(transfer: transfer).environmentObject(model) }
            if all.count > list.count { Button(model.text("Gesamten Verlauf öffnen", "Open full history")) { showHistory = true }.buttonStyle(.link) }
        }
    }
}

struct HelperAvailabilityView: View {
    @EnvironmentObject var model: AppModel
    var compact = false
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if model.lifecycle == .starting || model.lifecycle == .stopping {
                HStack(spacing: 10) {
                    ProgressView().controlSize(.small)
                    Text(model.lifecycle == .starting ? model.text("AirLink wird gestartet …", "Starting AirLink …") : model.text("AirLink wird beendet …", "Stopping AirLink …"))
                }.foregroundStyle(.secondary)
            } else {
                Label(model.text("AirLink ist nicht bereit", "AirLink is unavailable"), systemImage: "exclamationmark.triangle").foregroundStyle(.orange)
                if let issue = model.engineError {
                    Text(issue).font(.caption).foregroundStyle(.secondary).lineLimit(compact ? 2 : 4).textSelection(.enabled)
                    if !compact {
                        DisclosureGroup(model.text("Fehlerdetails", "Error details")) {
                            ScrollView { Text(issue).font(.caption).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading) }.frame(maxHeight: 140)
                        }.font(.caption)
                    }
                }
                if model.canRetry {
                    Button(model.text("Erneut versuchen", "Retry")) { Task { await model.start() } }
                    if !compact { Button(model.text("Empfangsordner auswählen und erneut versuchen …", "Choose receive folder and retry …")) { model.chooseRecoveryFolder() } }
                }
            }
        }.frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct TransferRow: View {
    @EnvironmentObject var model: AppModel
    let transfer: Transfer
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                Image(systemName: transfer.direction == "receive" ? "arrow.down.circle" : "arrow.up.circle").font(.title2).foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: 3) {
                    Text(transfer.title + (transfer.files.count > 1 ? " +\(transfer.files.count - 1)" : "")).lineLimit(1).font(.headline)
                    Text(transfer.deviceName + " · " + stateLabel).font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                if ["running", "queued", "reconnecting", "checkingDuplicate", "waitingDuplicate"].contains(transfer.state) {
                    Button { model.command("transfer.pause", params: ["id": transfer.id]) } label: { Image(systemName: "pause.fill") }.help(model.text("Pausieren", "Pause"))
                }
                if transfer.state == "paused" || transfer.state == "failed" {
                    Button { model.command("transfer.resume", params: ["id": transfer.id]) } label: { Image(systemName: "play.fill") }.help(model.text("Fortsetzen", "Resume"))
                }
                if transfer.isActive {
                    Button { model.command("transfer.cancel", params: ["id": transfer.id]) } label: { Image(systemName: "xmark") }.help(model.text("Abbrechen", "Cancel"))
                }
            }.disabled(!model.isReady)
            if transfer.canAcceptIncoming { IncomingApprovalView(transfer: transfer).environmentObject(model) }
            if !transfer.receivedResultEntries.isEmpty { ReceivedResultView(transfer: transfer).environmentObject(model) }
            ProgressView(value: transfer.fractionCompleted).tint(transfer.state == "completed" ? .green : .accentColor)
            HStack {
                Text("\(TransferFormatting.bytes(transfer.accountedBytes)) / \(TransferFormatting.bytes(transfer.totalBytes))")
                Spacer()
                if transfer.state == "running" { Text(TransferFormatting.speed(transfer.bytesPerSecond)) }
                Text(transfer.fractionCompleted, format: .percent.precision(.fractionLength(0)))
            }.font(.caption).monospacedDigit().foregroundStyle(.secondary)
            if transfer.avoidedBytes > 0 {
                Text(TransferFormatting.bytes(transfer.completedBytes) + model.text(" bestätigt · ", " confirmed · ") + TransferFormatting.bytes(transfer.avoidedBytes) + model.text(" durch Überspringen vermieden", " avoided by skipping")).font(.caption).foregroundStyle(.secondary)
            }
            if transfer.direction == "receive" {
                ForEach(transfer.pendingDuplicateFiles) { file in
                    DuplicateAttentionView(transfer: transfer, file: file).environmentObject(model)
                }
                let organized = transfer.files.filter { $0.isSkipped || ($0.destinationPath != nil && $0.destinationPath != $0.path) }
                if !organized.isEmpty {
                    DisclosureGroup(model.text("Empfangsdateien", "Received files")) {
                        ForEach(organized) { file in
                            if file.isSkipped {
                                Label(file.path + model.text(" · übersprungen, keine neue Datei", " · skipped, no new file"), systemImage: "forward.end").font(.caption).foregroundStyle(.secondary)
                            } else if let mapped = file.destinationPath {
                                Text(file.path + " → " + mapped).font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
                            }
                        }
                    }
                }
            } else {
                if ["checkingDuplicate", "waitingDuplicate"].contains(transfer.state) {
                    Text(model.text("Der Empfänger prüft vorhandene Inhalte oder benötigt eine Entscheidung.", "The recipient is checking existing contents or needs a decision.")).font(.caption).foregroundStyle(.secondary)
                }
                let skipped = transfer.files.filter(\.isSkipped)
                if !skipped.isEmpty {
                    DisclosureGroup(model.text("Vom Empfänger übersprungen", "Skipped by recipient")) {
                        ForEach(skipped) { file in Text(file.path).font(.caption).foregroundStyle(.secondary) }
                    }
                }
            }
            if let error = transfer.error, !error.isEmpty { Text(error).font(.caption).foregroundStyle(.red).textSelection(.enabled) }
        }.padding(16).background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 12))
    }
    private var stateLabel: String {
        let names: [String: (String, String)] = ["offered": ("Wartet auf Annahme", "Awaiting approval"), "queued": ("In Warteschlange", "Queued"), "running": ("Wird übertragen", "Transferring"), "paused": ("Pausiert", "Paused"), "reconnecting": ("Verbindung wird wiederhergestellt", "Reconnecting"), "verifying": ("Wird geprüft", "Verifying"), "checkingDuplicate": ("Vorhandene Inhalte werden geprüft", "Checking existing contents"), "waitingDuplicate": ("Wartet auf Duplikatentscheidung", "Awaiting duplicate decision"), "completed": ("Abgeschlossen", "Completed"), "failed": ("Fehlgeschlagen", "Failed"), "cancelled": ("Abgebrochen", "Cancelled")]
        let pair = names[transfer.state] ?? (transfer.state, transfer.state)
        return model.text(pair.0, pair.1)
    }
}

struct ConnectView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var requesting = false
    @State private var issue: String?
    @State private var sentRequest = false
    private var pending: PeerConnectionAttempt? {
        guard let pending = model.pendingConnection, pending.matches(model.connectionDraft) else { return nil }
        return pending
    }
    private var pairedDeviceID: String? { model.connectionDraft.pairedDevice(in: model.status?.devices ?? [])?.id }
    private var waiting: Bool { pending != nil && pending?.isExpired() == false }
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text(model.text("Gerät verbinden", "Connect a device")).font(.title2.weight(.semibold))
            Text(model.text("Öffne auf dem anderen Computer AirLink. Übernimm dessen Adresse und vergleiche den Zertifikat-Fingerabdruck direkt auf beiden Geräten.", "Open AirLink on the other computer. Enter its address and compare the certificate fingerprint directly on both devices.")).foregroundStyle(.secondary)
            TextField("https://192.168.1.10:…", text: $model.connectionDraft.address).disabled(requesting || waiting)
            TextField("SHA-256", text: $model.connectionDraft.fingerprint).font(.system(.body, design: .monospaced)).disabled(requesting || waiting)
            if !model.connectionDraft.fingerprint.isEmpty { Text(model.connectionDraft.comparisonFingerprint).font(.system(.caption, design: .monospaced)).foregroundStyle(.secondary).textSelection(.enabled).fixedSize(horizontal: false, vertical: true) }
            if let devices = model.status?.devices.filter({ !$0.paired && $0.address != nil }), !devices.isEmpty {
                Text(model.text("Entdeckte Geräte", "Discovered devices")).font(.headline)
                ForEach(devices) { device in
                    Button(device.name) { model.connectionDraft = PeerConnectionDraft(device: device); issue = nil }.buttonStyle(.link).disabled(requesting || waiting)
                }
                Text(model.text("Auch bei automatisch erkannten Geräten den Fingerabdruck vergleichen.", "Compare the fingerprint even for discovered devices.")).font(.caption).foregroundStyle(.secondary)
            }
            Toggle(model.text("Dateien von diesem Gerät automatisch empfangen", "Automatically receive files from this device"), isOn: $model.connectionDraft.automaticReceive).disabled(requesting || waiting)
            Text(model.text("Optional: Nach der Kopplung werden Dateien dieses Geräts auf diesem Mac ohne weitere Annahme empfangen. Für andere Geräte bleibt die Bestätigung erforderlich.", "Optional: after pairing, files from this device arrive on this Mac without another acceptance prompt. Other devices still need approval.")).font(.caption).foregroundStyle(.secondary)
            if let pending {
                TimelineView(.periodic(from: .now, by: 1)) { context in
                    if pending.isExpired(at: context.date) {
                        Label(model.text("Noch keine Kopplung bestätigt. Prüfe das andere Gerät und versuche es erneut.", "Pairing has not been confirmed. Check the other device and try again."), systemImage: "clock.badge.exclamationmark").font(.callout).foregroundStyle(.secondary)
                    } else {
                        HStack(spacing: 10) { ProgressView().controlSize(.small); Text(model.text("Anfrage gesendet · jetzt auf dem anderen Gerät bestätigen.", "Request sent · approve it on the other device.")) }.font(.callout)
                    }
                }
            } else if pairedDeviceID != nil {
                Label(model.text("Gerät verbunden", "Device connected"), systemImage: "checkmark.circle.fill").foregroundStyle(.green)
            } else {
                Text(model.text("Einmal auf dem anderen Computer bestätigen. Danach bleibt das Gerät gespeichert. Browsergeräte verbinden sich über den QR-Code.", "Approve once on the other computer. The device remains saved afterward. Browser clients connect through the QR code.")).font(.caption).foregroundStyle(.secondary)
            }
            if let issue { Text(issue).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
            HStack {
                Spacer()
                Button(pending != nil ? model.text("Im Hintergrund warten", "Wait in background") : model.text("Fertig", "Done")) { dismiss() }.disabled(requesting)
                if pairedDeviceID == nil {
                    Button(requesting ? model.text("Wird angefragt …", "Requesting …") : pending != nil ? model.text("Erneut versuchen", "Try again") : model.text("Anfrage senden", "Request pairing")) { request() }.buttonStyle(.borderedProminent).disabled(!model.connectionDraft.isValid || !model.isReady || requesting || (pending != nil && pending?.isExpired() == false))
                }
            }
        }.textFieldStyle(.roundedBorder).padding(28).frame(width: 510)
            .onAppear { if pending != nil { sentRequest = true } }
            .onChange(of: pairedDeviceID) { _, id in if let id, sentRequest { model.selectedDeviceID = id; dismiss() } }
            .interactiveDismissDisabled(requesting)
    }
    private func request() {
        guard !requesting else { return }
        requesting = true; issue = nil
        Task {
            do { try await model.connect(model.connectionDraft); sentRequest = true; if let id = pairedDeviceID { model.selectedDeviceID = id; dismiss() } }
            catch { issue = error.localizedDescription }
            requesting = false
        }
    }
}

struct LocalConnectionView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var selectedURL = ""
    @State private var setupQR = true
    private var addresses: [String] { setupQR ? (model.status?.setupURLs ?? model.status?.urls ?? []) : (model.status?.urls ?? []) }
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack { Text(model.text("Mit AirLink verbinden", "Connect to AirLink")).font(.title2.weight(.semibold)); Spacer(); Button(model.text("Fertig", "Done")) { dismiss() } }
            Text(model.text("Auf dem anderen Gerät zuerst das Zertifikat einrichten. Danach laufen Kopplung und Übertragungen über HTTPS.", "First set up the certificate on the other device. Pairing and transfers then use HTTPS.")).foregroundStyle(.secondary)
            Picker(model.text("QR-Code", "QR code"), selection: $setupQR) {
                Text(model.text("Einrichtung", "Setup")).tag(true)
                Text(model.text("Übertragen", "Transfer")).tag(false)
            }.pickerStyle(.segmented)
            if let image = qrCode(selectedURL) { Image(nsImage: image).interpolation(.none).resizable().frame(width: 180, height: 180).padding(16).background(.white, in: RoundedRectangle(cornerRadius: 16)).frame(maxWidth: .infinity) }
            Picker(model.text("Adresse", "Address"), selection: $selectedURL) { ForEach(addresses, id: \.self) { Text($0).tag($0) } }
            HStack { Text(selectedURL).textSelection(.enabled).font(.system(.callout, design: .monospaced)); Spacer(); Button { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(selectedURL, forType: .string) } label: { Image(systemName: "doc.on.doc") } }
            Text(model.text("Die HTTP-Einrichtungsseite enthält nur das öffentliche Zertifikat. Den SHA-256-Fingerabdruck direkt mit dieser Anzeige vergleichen, bevor du das Zertifikat installierst. Anschließend die HTTPS-Adresse öffnen und die Verbindungsanfrage hier bestätigen.", "The HTTP setup page contains only the public certificate. Compare its SHA-256 fingerprint directly with this display before installing the certificate. Then open the HTTPS address and approve the pairing request here.")).font(.callout).foregroundStyle(.secondary)
            ForEach(model.status?.urls.filter { !($0.contains("localhost")) } ?? [], id: \.self) { url in
                HStack { Text(url).font(.system(.caption, design: .monospaced)).textSelection(.enabled); Spacer(); Button { NSPasteboard.general.clearContents(); NSPasteboard.general.setString(url, forType: .string) } label: { Image(systemName: "doc.on.doc") } }
            }
            Text(model.status?.fingerprint ?? "").font(.system(.caption, design: .monospaced)).textSelection(.enabled)
            if let path = model.status?.certificatePath {
                Button(model.text("Zertifikat im Finder zeigen", "Reveal certificate in Finder")) { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)]) }
            }
        }.padding(28).frame(width: 540).onAppear { selectedURL = addresses.first ?? "" }
            .onChange(of: setupQR) { _, _ in selectedURL = addresses.first ?? "" }
    }
    private func qrCode(_ string: String) -> NSImage? {
        guard !string.isEmpty else { return nil }
        let filter = CIFilter.qrCodeGenerator(); filter.message = Data(string.utf8)
        guard let output = filter.outputImage?.transformed(by: CGAffineTransform(scaleX: 8, y: 8)), let cg = CIContext().createCGImage(output, from: output.extent) else { return nil }
        return NSImage(cgImage: cg, size: NSSize(width: output.extent.width, height: output.extent.height))
    }
}

struct DeviceSettingsView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State var device: Device
    @State private var confirmRevoke = false
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text(device.name).font(.title2.weight(.semibold))
            Toggle(model.text("Darf Dateien an diesen Mac senden", "May send files to this Mac"), isOn: $device.canSend)
            Toggle(model.text("Darf Dateien von diesem Mac empfangen", "May receive files from this Mac"), isOn: $device.canReceive)
            Toggle(model.text("Darf Zwischenablagetext austauschen", "May exchange clipboard text"), isOn: $device.canClipboard)
            if device.kind == "browser" {
                Toggle(model.text("Darf Dateien zwischen Browsern vermitteln lassen", "May use browser-to-browser relay"), isOn: $device.canRelay)
                Text(model.text("Für Senden oder Empfangen gelten zusätzlich die jeweiligen Dateiberechtigungen. Neue Relay-Angebote benötigen die Bestätigung im Empfänger-Browser.", "Sending and receiving also require the corresponding file permission. New relay offers need acceptance in the recipient browser.")).font(.caption).foregroundStyle(.secondary)
            }
            Toggle(model.text("Eingehende Übertragungen automatisch annehmen", "Automatically accept incoming transfers"), isOn: $device.autoAccept)
                .disabled(!device.canSend)
            if let fingerprint = device.fingerprint { Text(fingerprint).font(.system(.caption, design: .monospaced)).foregroundStyle(.secondary).textSelection(.enabled) }
            HStack {
                Button(model.text("Vertrauen widerrufen …", "Revoke trust …"), role: .destructive) { confirmRevoke = true }
                Spacer(); Button(model.text("Abbrechen", "Cancel")) { dismiss() }
                Button(model.text("Speichern", "Save")) { model.update(device); dismiss() }.buttonStyle(.borderedProminent).disabled(!model.isReady)
            }
        }.padding(28).frame(width: 540)
        .confirmationDialog(model.text("Vertrauen für \(device.name) widerrufen?", "Revoke trust for \(device.name)?"), isPresented: $confirmRevoke) {
            Button(model.text("Widerrufen", "Revoke"), role: .destructive) { model.command("device.revoke", params: ["id": device.id]); dismiss() }.disabled(!model.isReady)
        }
    }
}
