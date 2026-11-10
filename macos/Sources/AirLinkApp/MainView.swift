import AppKit
import AirLinkCore
import CoreImage.CIFilterBuiltins
import SwiftUI
import UniformTypeIdentifiers

struct MainView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.openWindow) private var openWindow
    @State private var navigation: String?
    @State private var dropTarget = false
    @State private var dropDeviceID: String?
    @State private var showLocalConnection = false
    @State private var editingDevice: Device?
    @State private var showShares = false
    @State private var showClipboard = false
    @State private var showRelays = false
    @State private var showStatistics = false
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
    var body: some View {
        NavigationSplitView {
            List(selection: $navigation) {
                Section(model.text("Geräte", "Devices")) {
                    ForEach(pairedDevices) { device in deviceRow(device).tag("device:" + device.id) }
                    if pairedDevices.isEmpty { Text(model.text("Noch keine vertrauten Geräte", "No trusted devices yet")).foregroundStyle(.secondary) }
                    Button { model.showConnection() } label: { Label(model.text("Gerät hinzufügen …", "Add device …"), systemImage: "plus.circle") }.disabled(!model.isReady || !model.setupComplete)
                }
                Section {
                    Label {
                        HStack { Text(model.text("Übertragungen", "Transfers")); Spacer(); if let count = model.status?.activeTransferCount, count > 0 { Text(count.formatted()).foregroundStyle(.secondary) } }
                    } icon: { Image(systemName: "arrow.up.arrow.down") }.tag("transfers")
                }
                let nearby = model.status?.devices.filter { !$0.paired && $0.kind == "engine" } ?? []
                if !nearby.isEmpty {
                    Section(model.text("In der Nähe", "Nearby")) {
                        ForEach(nearby) { device in
                            Button { model.showConnection(to: device) } label: { Label(device.name, systemImage: "desktopcomputer").lineLimit(2) }.disabled(!model.isReady || !model.setupComplete)
                        }
                    }
                }
            }
            .navigationSplitViewColumnWidth(min: 250, ideal: 290, max: 430)
            .safeAreaInset(edge: .bottom) {
                VStack(alignment: .leading, spacing: 14) {
                    Menu {
                        Button("Quick Share") { showShares = true }
                        Button(model.text("Zwischenablage", "Clipboard")) { showClipboard = true }
                        Button(model.text("Browser-Verbindungen", "Browser connections")) { showRelays = true }
                        Button(model.text("Statistik", "Statistics")) { showStatistics = true }
                    } label: { Label(model.text("Weitere Werkzeuge", "More tools"), systemImage: "ellipsis.circle") }.disabled(!model.isReady || !model.setupComplete)
                    SettingsLink { Label(model.text("Einstellungen", "Settings"), systemImage: "gearshape") }
                    Divider()
                    Text(model.status?.name ?? "AirLink").font(.callout.weight(.medium)).lineLimit(2)
                    Text(model.version + " Beta").font(.caption).foregroundStyle(.secondary)
                }.buttonStyle(.plain).padding(18).frame(maxWidth: .infinity, alignment: .leading)
            }
        } detail: {
            if navigation == "transfers" {
                DesktopTransfersView().environmentObject(model)
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 22) {
                        if !model.isReady { HelperAvailabilityView().environmentObject(model) }
                        connectionAttention
                        if model.setupRequired {
                            ContentUnavailableView {
                                Label(model.text("Willkommen bei AirLink", "Welcome to AirLink"), systemImage: "arrow.left.arrow.right")
                            } description: {
                                Text(model.text("Wähle einen Gerätenamen und den Empfangsordner.", "Choose a device name and receive folder."))
                            } actions: {
                                Button(model.text("AirLink einrichten", "Set up AirLink")) { model.showSetup = true }.buttonStyle(.borderedProminent)
                            }
                        } else {
                            if let device = model.selectedDevice {
                                HStack(alignment: .top) {
                                    VStack(alignment: .leading, spacing: 7) {
                                        Text(device.name).font(.largeTitle.weight(.semibold)).textSelection(.enabled)
                                        Label(device.online ? model.text("Im lokalen Netzwerk erreichbar", "Available on your local network") : model.text("Offline · Sendungen warten auf die Verbindung", "Offline · sends wait for a connection"), systemImage: device.online ? "circle.fill" : "circle").font(.callout).foregroundStyle(.secondary)
                                    }
                                    Spacer()
                                    Button { editingDevice = device } label: { Image(systemName: "slider.horizontal.3") }.help(model.text("Gerät verwalten", "Manage device"))
                                }
                            } else {
                                VStack(alignment: .leading, spacing: 8) {
                                    Text(model.text("An welches Gerät?", "Which device? ")).font(.largeTitle.weight(.semibold))
                                    Text(model.text("Wähle links ein Gerät oder füge deinen anderen Computer einmal hinzu.", "Choose a device on the left or add your other computer once.")).foregroundStyle(.secondary)
                                }
                            }
                            dropArea
                            failedDirectSends
                            if let id = model.selectedDevice?.id {
                                let transfers = DesktopTransferSelection.visible(model.status?.transfers ?? [], deviceID: id)
                                if !transfers.isEmpty {
                                    HStack { Text(model.text("Übertragungen mit diesem Gerät", "Transfers with this device")).font(.headline); Spacer(); Button(model.text("Alle Übertragungen", "All transfers")) { navigation = "transfers" }.buttonStyle(.link) }
                                    ForEach(transfers) { TransferRow(transfer: $0).environmentObject(model) }
                                }
                            }
                        }
                    }.padding(28).frame(maxWidth: 1000, alignment: .leading).frame(maxWidth: .infinity)
                }.background(Color(nsColor: .windowBackgroundColor))
            }
        }
        .toolbar {
            ToolbarItem { Button { model.openReceiveDirectory() } label: { Label(model.text("Empfangsordner", "Receive folder"), systemImage: "tray.and.arrow.down") }.disabled(!model.isReady) }
            ToolbarItem { Button { showLocalConnection = true } label: { Label(model.text("Dieses Gerät", "This device"), systemImage: "qrcode") }.disabled(!model.isReady) }
        }
        .sheet(isPresented: $model.showConnection) { ConnectView().environmentObject(model) }
        .sheet(isPresented: $model.showSetup, onDismiss: { if model.connectAfterSetup { model.connectAfterSetup = false; model.showConnection() } }) { SetupView().environmentObject(model) }
        .sheet(isPresented: $showLocalConnection) { LocalConnectionView().environmentObject(model) }
        .sheet(item: $editingDevice) { DeviceSettingsView(device: $0).environmentObject(model) }
        .sheet(isPresented: $showShares) { ShareListView().environmentObject(model).onAppear { model.observeTool("shares", active: true) }.onDisappear { model.observeTool("shares", active: false) } }
        .sheet(isPresented: $showClipboard) { ClipboardView().environmentObject(model).onAppear { model.observeTool("clipboard", active: true) }.onDisappear { model.observeTool("clipboard", active: false) } }
        .sheet(isPresented: $showRelays) { RelayOverview().environmentObject(model).onAppear { model.observeTool("relays", active: true) }.onDisappear { model.observeTool("relays", active: false) } }
        .sheet(isPresented: $showStatistics) { StatisticsSheet().environmentObject(model) }
        .alert(model.text("Aktion konnte nicht abgeschlossen werden", "Could not complete the action"), isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) { Button("OK") { model.error = nil } } message: { Text(model.error ?? "") }
        .onAppear { model.openMainWindow = { openWindow(id: "airlink-main") }; if let id = model.selectedDeviceID { navigation = "device:" + id } }
        .onChange(of: navigation) { _, value in if let value, value.hasPrefix("device:") { model.selectedDeviceID = String(value.dropFirst(7)) } }
        .onChange(of: model.selectedDeviceID) { _, id in if let id, navigation != "transfers" { navigation = "device:" + id } }
    }
    private func deviceRow(_ device: Device) -> some View {
        HStack(spacing: 10) {
            Image(systemName: device.kind == "browser" ? "globe" : "desktopcomputer").font(.title3).frame(width: 24)
            VStack(alignment: .leading, spacing: 3) {
                Text(device.name).fontWeight(.medium).lineLimit(2).fixedSize(horizontal: false, vertical: true)
                Text(device.online ? model.text("Erreichbar", "Available") : model.text("Nicht erreichbar", "Unavailable")).font(.caption).foregroundStyle(.secondary)
            }
            Spacer(minLength: 3)
        }.padding(.vertical, 7).help(device.name)
            .contextMenu { Button(model.text("Gerät verwalten …", "Manage device …")) { editingDevice = device } }
            .onDrop(of: [UTType.fileURL], isTargeted: Binding(get: { dropDeviceID == device.id }, set: { dropDeviceID = $0 ? device.id : nil })) { providers in
                guard device.kind == "engine", device.canReceive else { return false }; return model.receiveDrop(providers, to: device)
            }
    }
    @ViewBuilder private var connectionAttention: some View {
        if let pair = model.status?.pairingSessions?.first(where: \.needsAttention) {
            HStack { Label(model.text("Verbindung mit \(pair.peerName) bestätigen", "Confirm connection to \(pair.peerName)"), systemImage: "person.crop.circle.badge.checkmark"); Spacer(); Button(model.text("Code vergleichen", "Compare code")) { model.selectedPairingSessionID = pair.id; model.showConnection = true } }.padding(14).background(Color.accentColor.opacity(0.07), in: RoundedRectangle(cornerRadius: 10))
        }
        ForEach((model.status?.pairRequests ?? []).prefix(1)) { PairingApprovalView(request: $0).environmentObject(model) }
    }
    private var dropArea: some View {
        VStack(alignment: .leading, spacing: 15) {
            Label(model.sendingFiles ? model.text("Sendung wird vorbereitet …", "Preparing send …") : model.text("Dateien und Ordner senden", "Send files and folders"), systemImage: "arrow.up.doc").font(.title3.weight(.medium))
            Text(directRecipient.map { model.text("Hier ablegen oder auswählen, um an \($0.name) zu senden.", "Drop here or choose files to send to \($0.name).") } ?? model.text("Lege Dateien hier ab. Deine Auswahl bleibt bis zur Empfängerwahl erhalten.", "Drop files here. Your selection is kept until you choose a recipient.")).foregroundStyle(.secondary)
            if !model.pendingURLs.isEmpty { Text(model.pendingURLs.prefix(3).map(\.lastPathComponent).joined(separator: ", ")).font(.callout).lineLimit(2) }
            HStack {
                Button(model.text("Dateien auswählen …", "Choose files …")) { model.chooseFiles() }.buttonStyle(.borderedProminent).disabled(model.sendingFiles || !model.isReady)
                if !model.pendingURLs.isEmpty {
                    Button(model.text("Auswahl leeren", "Clear selection")) { model.pendingURLs = [] }.buttonStyle(.borderless)
                    Button(model.text("Auswahl senden", "Send selection")) { model.send() }.disabled(model.selectedDevice?.canReceive != true || model.sendingFiles || !model.isReady)
                }
                if let id = model.activePreparationID { ProgressView().controlSize(.small); Button(model.text("Abbrechen", "Cancel")) { model.command("preparation.cancel", params: ["id": id]) } }
            }
            if let notice = model.sendNotice { Text(notice).font(.caption).foregroundStyle(.secondary) }
        }.padding(25).frame(maxWidth: .infinity, minHeight: 170, alignment: .leading)
            .background(dropTarget ? Color.accentColor.opacity(0.08) : Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 14))
            .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(dropTarget ? Color.accentColor : Color.secondary.opacity(0.16)))
            .onDrop(of: [UTType.fileURL], isTargeted: $dropTarget) { model.receiveDrop($0, to: directRecipient) }
    }
    @ViewBuilder private var failedDirectSends: some View {
        ForEach(model.failedDirectSends) { failed in
            VStack(alignment: .leading, spacing: 8) {
                Label(model.text("Sendung an \(failed.batch.deviceName) benötigt eine Aktion", "Send to \(failed.batch.deviceName) needs attention"), systemImage: "exclamationmark.triangle").font(.headline)
                Text(failed.message).font(.callout).foregroundStyle(.secondary)
                HStack { Button(model.text("Erneut versuchen", "Retry")) { model.retryDirectSend(failed) }.disabled(model.sendingFiles || !model.isReady); Button(model.text("Aus Liste entfernen", "Dismiss")) { model.dismissDirectSend(failed.id) }.buttonStyle(.borderless) }
            }.padding(14).frame(maxWidth: .infinity, alignment: .leading).background(.orange.opacity(0.07), in: RoundedRectangle(cornerRadius: 10))
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
    @State private var details = false
    @State private var issue: String?
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: transfer.direction == "send" ? "arrow.up.doc" : "arrow.down.doc").font(.title3).foregroundStyle(.tint).frame(width: 24)
                VStack(alignment: .leading, spacing: 4) {
                    Text(transfer.title).font(.headline).lineLimit(2)
                    Text(transfer.deviceName + " · " + model.stateLabel(transfer.state)).font(.callout).foregroundStyle(.secondary)
                }
                Spacer(minLength: 8)
                if transfer.state == "completed", transfer.hasReceivedContent {
                    Button(model.text("Im Finder zeigen", "Show in Finder")) { Task { do { try await model.revealReceived(transfer) } catch { issue = error.localizedDescription } } }.buttonStyle(.borderless)
                }
                Menu {
                    Button(model.text("Details", "Details")) { details = true }
                    if transfer.state == "paused" || transfer.state == "reconnecting" || transfer.retryable == true {
                        Button(model.text("Fortsetzen", "Resume")) { model.command("transfer.resume", params: ["id": transfer.id]) }
                    } else if transfer.isActive {
                        Button(model.text("Pausieren", "Pause")) { model.command("transfer.pause", params: ["id": transfer.id]) }
                    }
                    if transfer.isActive { Button(model.text("Abbrechen", "Cancel")) { model.command("transfer.cancel", params: ["id": transfer.id]) } }
                    if transfer.canRemoveFromHistory {
                        Button(model.text("Aus Liste entfernen", "Remove from list")) { Task { do { try await model.removeFromHistory([transfer]) } catch { issue = error.localizedDescription } } }
                    }
                } label: { Image(systemName: "ellipsis.circle") }.menuStyle(.borderlessButton).fixedSize().disabled(!model.isReady)
            }
            if transfer.canAcceptIncoming { IncomingApprovalView(transfer: transfer).environmentObject(model) }
            if transfer.isActive {
                ProgressView(value: transfer.fractionCompleted)
                HStack {
                    Text(TransferFormatting.bytes(transfer.accountedBytes) + " / " + TransferFormatting.bytes(transfer.totalBytes))
                    Spacer()
                    if transfer.state == "running" {
                        Text(TransferFormatting.speed(transfer.bytesPerSecond))
                        if let seconds = DesktopTransferSelection.remainingSeconds(transfer) { Text(model.text("Noch \(seconds < 60 ? "\(seconds) s" : "\(seconds / 60) min")", "\(seconds < 60 ? "\(seconds) s" : "\(seconds / 60) min") left")) }
                    }
                }.font(.caption).monospacedDigit().foregroundStyle(.secondary)
            } else {
                Text(TransferFormatting.bytes(transfer.completedBytes) + (transfer.numberOfFiles > 1 ? model.text(" · \(transfer.numberOfFiles) Einträge", " · \(transfer.numberOfFiles) entries") : "")).font(.caption).foregroundStyle(.secondary)
            }
            if transfer.state == "waitingDuplicate" {
                Button(model.text("Duplikate prüfen …", "Review duplicates …")) { details = true }
            }
            if let problem = transfer.problem {
                Text(model.text(problem.titleDE, problem.titleEN)).font(.callout).foregroundStyle(.red)
                Text(model.text(problem.recoveryDE, problem.recoveryEN)).font(.caption).foregroundStyle(.secondary)
            } else if let error = transfer.error, !error.isEmpty { Text(error).font(.callout).foregroundStyle(.red).lineLimit(3) }
            if transfer.retryable == true {
                Button(model.text("Erneut versuchen", "Try again")) { model.command("transfer.resume", params: ["id": transfer.id]) }
            }
            if let issue { Text(issue).font(.callout).foregroundStyle(.red) }
        }.padding(16).background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 10))
            .sheet(isPresented: $details) { TransferDetailsSheet(transfer: transfer).environmentObject(model) }
    }
}

struct LocalConnectionView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var selectedURL = ""
    @State private var setupQR = true
    @State private var browserExpanded = false
    var embedded = false
    private var addresses: [String] { setupQR ? (model.status?.setupURLs ?? model.status?.urls ?? []) : (model.status?.urls ?? []) }
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            if !embedded { HStack { Text(model.text("Dieses Gerät", "This device")).font(.title2.weight(.semibold)); Spacer(); Button(model.text("Fertig", "Done")) { dismiss() } } }
            if embedded { browserSetup }
            else {
                identity
                Divider()
                DisclosureGroup(model.text("Handy oder Tablet über den Browser verbinden", "Connect a phone or tablet through its browser"), isExpanded: $browserExpanded) {
                    browserSetup.padding(.top, 16)
                }
            }
        }.padding(embedded ? 0 : 28).frame(width: embedded ? nil : 540).onAppear { selectedURL = addresses.first ?? "" }
            .onChange(of: setupQR) { _, _ in selectedURL = addresses.first ?? "" }
    }
    private var identity: some View {
        VStack(alignment: .leading, spacing: 14) {
            Label(model.status?.name ?? "AirLink", systemImage: "laptopcomputer").font(.title3.weight(.semibold))
            Text(model.text("Wähle diesen Namen auf dem anderen Computer in AirLink aus. Vergleiche dort den Fingerabdruck mit dieser Anzeige.", "Select this name in AirLink on the other computer. Compare the fingerprint there with this display.")).font(.callout).foregroundStyle(.secondary)
            Text(model.text("Fingerabdruck dieses Geräts", "This device’s fingerprint")).font(.caption.weight(.medium))
            Text(PeerConnectionDraft(fingerprint: model.status?.fingerprint ?? "").comparisonFingerprint)
                .font(.system(.body, design: .monospaced)).textSelection(.enabled)
                .padding(14).frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 10))
            Text(model.text("Adresse für eine manuelle Verbindung", "Address for a manual connection")).font(.caption.weight(.medium))
            ForEach(model.status?.urls.filter { !($0.contains("localhost")) } ?? [], id: \.self) { url in
                HStack {
                    Text(url).font(.system(.callout, design: .monospaced)).textSelection(.enabled)
                    Spacer()
                    copyButton(url)
                }
            }
        }
    }
    private var browserSetup: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(model.text("Mit \(model.status?.name ?? "AirLink") verbinden", "Connect to \(model.status?.name ?? "AirLink")")).font(.headline)
            Text(model.text("Auf dem anderen Gerät zuerst das Zertifikat einrichten. Danach laufen Kopplung und Übertragungen über HTTPS.", "First set up the certificate on the other device. Pairing and transfers then use HTTPS.")).foregroundStyle(.secondary)
            Picker(model.text("QR-Code", "QR code"), selection: $setupQR) {
                Text(model.text("Einrichtung", "Setup")).tag(true)
                Text(model.text("Übertragen", "Transfer")).tag(false)
            }.pickerStyle(.segmented)
            if let image = qrCode(selectedURL) { Image(nsImage: image).interpolation(.none).resizable().frame(width: 180, height: 180).padding(16).background(.white, in: RoundedRectangle(cornerRadius: 16)).frame(maxWidth: .infinity) }
            Picker(model.text("Adresse", "Address"), selection: $selectedURL) { ForEach(addresses, id: \.self) { Text($0).tag($0) } }
            HStack { Text(selectedURL).textSelection(.enabled).font(.system(.callout, design: .monospaced)); Spacer(); copyButton(selectedURL) }
            Text(model.text("Die HTTP-Einrichtungsseite enthält nur das öffentliche Zertifikat. Den SHA-256-Fingerabdruck direkt mit dieser Anzeige vergleichen, bevor du das Zertifikat installierst. Anschließend die HTTPS-Adresse öffnen und die Verbindungsanfrage hier bestätigen.", "The HTTP setup page contains only the public certificate. Compare its SHA-256 fingerprint directly with this display before installing the certificate. Then open the HTTPS address and approve the pairing request here.")).font(.callout).foregroundStyle(.secondary)
            Text(model.text("Fingerabdruck dieses Geräts", "This device’s fingerprint")).font(.caption.weight(.medium))
            Text(PeerConnectionDraft(fingerprint: model.status?.fingerprint ?? "").comparisonFingerprint).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
            if let path = model.status?.certificatePath {
                Button(model.text("Zertifikat im Finder zeigen", "Reveal certificate in Finder")) { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)]) }
            }
        }
    }
    private func copyButton(_ text: String) -> some View {
        Button {
            NSPasteboard.general.clearContents(); NSPasteboard.general.setString(text, forType: .string)
        } label: { Label(model.text("Adresse kopieren", "Copy address"), systemImage: "doc.on.doc") }.labelStyle(.iconOnly).help(model.text("Adresse kopieren", "Copy address")).disabled(text.isEmpty)
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
