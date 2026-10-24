import AppKit
import AirLinkCore
import CoreImage.CIFilterBuiltins
import SwiftUI
import UniformTypeIdentifiers

struct MainView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.openWindow) private var openWindow
    @State private var dropTarget = false
    @State private var showLocalConnection = false
    @State private var editingDevice: Device?
    var body: some View {
        NavigationSplitView {
            List(selection: $model.selectedDeviceID) {
                Section(model.text("Geräte", "Devices")) {
                    ForEach(model.status?.devices.filter(\.paired) ?? []) { device in
                        HStack(spacing: 10) {
                            Image(systemName: device.kind == "browser" ? "globe" : "desktopcomputer").font(.title3).frame(width: 24)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(device.name).lineLimit(1)
                                Text(device.online ? model.text("Verbunden", "Connected") : model.text("Nicht erreichbar", "Unavailable")).font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            Circle().fill(device.online ? .green : .secondary).frame(width: 6, height: 6)
                        }
                        .padding(.vertical, 5).tag(device.id)
                        .contextMenu {
                            Button(model.text("Berechtigungen …", "Permissions …")) { editingDevice = device }
                        }
                    }
                    if model.status?.devices.filter(\.paired).isEmpty != false {
                        Text(model.text("Noch keine Geräte", "No devices yet")).foregroundStyle(.secondary)
                    }
                }
                let nearby = model.status?.devices.filter { !$0.paired } ?? []
                if !nearby.isEmpty {
                    Section(model.text("In der Nähe", "Nearby")) {
                        ForEach(nearby) { device in
                            Button { model.showConnection = true } label: { Label(device.name, systemImage: "antenna.radiowaves.left.and.right") }
                                .contextMenu { Text(device.address ?? "").textSelection(.enabled) }
                        }
                    }
                }
            }
            .navigationSplitViewColumnWidth(min: 200, ideal: 230, max: 290)
            .safeAreaInset(edge: .bottom) {
                Button { model.showConnection = true } label: { Label(model.text("Gerät verbinden", "Connect device"), systemImage: "plus") }
                    .buttonStyle(.plain).padding().frame(maxWidth: .infinity, alignment: .leading)
            }
        } detail: {
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    title
                    if let issue = model.engineError {
                        Label(issue, systemImage: "exclamationmark.triangle").foregroundStyle(.orange).textSelection(.enabled)
                    }
                    pairingRequests
                    dropArea
                    transfers
                }.padding(30).frame(maxWidth: 1000, alignment: .leading).frame(maxWidth: .infinity)
            }
            .background(Color(nsColor: .windowBackgroundColor))
            .toolbar {
                ToolbarItem { Button { showLocalConnection = true } label: { Label(model.text("Verbindung", "Connection"), systemImage: "qrcode") } }
                ToolbarItem { SettingsLink { Label(model.text("Einstellungen", "Settings"), systemImage: "gearshape") } }
            }
        }
        .sheet(isPresented: $model.showConnection) { ConnectView().environmentObject(model) }
        .sheet(isPresented: $showLocalConnection) { LocalConnectionView().environmentObject(model) }
        .sheet(item: $editingDevice) { device in DeviceSettingsView(device: device).environmentObject(model) }
        .alert(model.text("AirLink konnte die Aktion nicht ausführen", "AirLink could not complete the action"), isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) {
            Button("OK") { model.error = nil }
        } message: { Text(model.error ?? "") }
        .onAppear { model.openMainWindow = { openWindow(id: "airlink-main") } }
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
            VStack(alignment: .leading, spacing: 12) {
                Label(model.text("\(request.name) möchte sich verbinden", "\(request.name) wants to connect"), systemImage: "person.badge.key.fill").font(.headline)
                Text(model.text("Nur bestätigen, wenn du die Verbindung auf diesem Gerät gerade gestartet hast. Neue Übertragungen müssen zunächst einzeln angenommen werden.", "Approve only if you just initiated pairing on that device. Incoming transfers require approval by default.")).font(.callout).foregroundStyle(.secondary)
                HStack {
                    Button(model.text("Ablehnen", "Deny")) { model.command("pair.deny", params: ["id": request.id]) }
                    Button(model.text("Verbinden", "Approve")) { model.command("pair.approve", params: ["id": request.id]) }.buttonStyle(.borderedProminent)
                }
            }.padding(18).frame(maxWidth: .infinity, alignment: .leading).background(.blue.opacity(0.07), in: RoundedRectangle(cornerRadius: 14))
        }
    }
    private var dropArea: some View {
        VStack(spacing: 14) {
            Image(systemName: "arrow.up.doc").font(.system(size: 38, weight: .light)).foregroundStyle(.tint)
            Text(model.pendingURLs.isEmpty ? model.text("Dateien und Ordner hier ablegen", "Drop files and folders here") : model.text("\(model.pendingURLs.count) Objekte ausgewählt", "\(model.pendingURLs.count) items selected")).font(.title3.weight(.medium))
            Text(model.selectedDevice == nil ? model.text("Wähle links ein Gerät als Empfänger.", "Choose a device on the left.") : model.text("An \(model.selectedDevice!.name) senden", "Send to \(model.selectedDevice!.name)")).foregroundStyle(.secondary)
            if !model.pendingURLs.isEmpty {
                Text(model.pendingURLs.prefix(3).map(\.lastPathComponent).joined(separator: ", ")).font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
            HStack {
                Button(model.text("Dateien auswählen …", "Choose files …")) { model.chooseFiles() }
                if !model.pendingURLs.isEmpty {
                    Button(model.text("Leeren", "Clear")) { model.pendingURLs = [] }.buttonStyle(.borderless)
                    Button(model.text("Senden", "Send")) { model.send() }.buttonStyle(.borderedProminent)
                        .disabled(model.selectedDevice?.canReceive != true || model.engineError != nil)
                }
            }
        }
        .padding(30).frame(maxWidth: .infinity, minHeight: 240)
        .background(dropTarget ? Color.accentColor.opacity(0.09) : Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 18))
        .overlay(RoundedRectangle(cornerRadius: 18).strokeBorder(dropTarget ? Color.accentColor : Color.secondary.opacity(0.22), style: StrokeStyle(lineWidth: 1, dash: [6, 5])))
        .onDrop(of: [UTType.fileURL], isTargeted: $dropTarget) { providers in
            for provider in providers {
                provider.loadItem(forTypeIdentifier: UTType.fileURL.identifier, options: nil) { item, _ in
                    let url = (item as? URL) ?? (item as? Data).flatMap { URL(dataRepresentation: $0, relativeTo: nil) }
                    if let url { Task { @MainActor in model.add([url]) } }
                }
            }
            return !providers.isEmpty
        }
    }
    private var transfers: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(model.text("Übertragungen", "Transfers")).font(.title2.weight(.semibold))
            let list = model.status?.transfers.sorted { $0.createdAt > $1.createdAt } ?? []
            if list.isEmpty {
                Text(model.text("Hier erscheinen deine Übertragungen.", "Your transfers will appear here.")).foregroundStyle(.secondary).padding(.vertical, 14)
            }
            ForEach(list) { transfer in TransferRow(transfer: transfer).environmentObject(model) }
        }
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
                if transfer.state == "offered" && transfer.direction == "receive" {
                    Button(model.text("Annehmen", "Accept")) { model.command("transfer.accept", params: ["id": transfer.id]) }.buttonStyle(.borderedProminent)
                }
                if ["running", "queued", "reconnecting"].contains(transfer.state) {
                    Button { model.command("transfer.pause", params: ["id": transfer.id]) } label: { Image(systemName: "pause.fill") }.help(model.text("Pausieren", "Pause"))
                }
                if transfer.state == "paused" || transfer.state == "failed" {
                    Button { model.command("transfer.resume", params: ["id": transfer.id]) } label: { Image(systemName: "play.fill") }.help(model.text("Fortsetzen", "Resume"))
                }
                if transfer.isActive {
                    Button { model.command("transfer.cancel", params: ["id": transfer.id]) } label: { Image(systemName: "xmark") }.help(model.text("Abbrechen", "Cancel"))
                }
                if transfer.state == "completed", transfer.destination != nil {
                    Button { model.revealDestination(transfer) } label: { Image(systemName: "folder") }.help(model.text("Im Finder öffnen", "Open in Finder"))
                }
            }
            ProgressView(value: transfer.fractionCompleted).tint(transfer.state == "completed" ? .green : .accentColor)
            HStack {
                Text("\(TransferFormatting.bytes(transfer.completedBytes)) / \(TransferFormatting.bytes(transfer.totalBytes))")
                Spacer()
                if transfer.state == "running" { Text(TransferFormatting.speed(transfer.bytesPerSecond)) }
                Text(transfer.fractionCompleted, format: .percent.precision(.fractionLength(0)))
            }.font(.caption).monospacedDigit().foregroundStyle(.secondary)
            if let error = transfer.error, !error.isEmpty { Text(error).font(.caption).foregroundStyle(.red).textSelection(.enabled) }
        }.padding(16).background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 12))
    }
    private var stateLabel: String {
        let names: [String: (String, String)] = ["offered": ("Wartet auf Annahme", "Awaiting approval"), "queued": ("In Warteschlange", "Queued"), "running": ("Wird übertragen", "Transferring"), "paused": ("Pausiert", "Paused"), "reconnecting": ("Verbindung wird wiederhergestellt", "Reconnecting"), "verifying": ("Wird geprüft", "Verifying"), "completed": ("Abgeschlossen", "Completed"), "failed": ("Fehlgeschlagen", "Failed"), "cancelled": ("Abgebrochen", "Cancelled")]
        let pair = names[transfer.state] ?? (transfer.state, transfer.state)
        return model.text(pair.0, pair.1)
    }
}

struct ConnectView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var url = ""
    @State private var fingerprint = ""
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text(model.text("Gerät verbinden", "Connect a device")).font(.title2.weight(.semibold))
            Text(model.text("Öffne auf dem anderen Computer AirLink. Übernimm dessen Adresse und vergleiche den Zertifikat-Fingerabdruck direkt auf beiden Geräten.", "Open AirLink on the other computer. Enter its address and compare the certificate fingerprint directly on both devices.")).foregroundStyle(.secondary)
            TextField("https://192.168.1.10:…", text: $url)
            TextField("SHA-256", text: $fingerprint).font(.system(.body, design: .monospaced))
            if let devices = model.status?.devices.filter({ !$0.paired && $0.address != nil }), !devices.isEmpty {
                Text(model.text("Entdeckte Geräte", "Discovered devices")).font(.headline)
                ForEach(devices) { device in
                    Button(device.name) { url = device.address ?? ""; fingerprint = device.fingerprint ?? "" }.buttonStyle(.link)
                }
                Text(model.text("Auch bei automatisch erkannten Geräten den Fingerabdruck vergleichen.", "Compare the fingerprint even for discovered devices.")).font(.caption).foregroundStyle(.secondary)
            }
            Text(model.text("Danach die Anfrage am anderen Computer bestätigen. Ein Browser verbindet sich über den QR-Code dieser App.", "Then approve the request on the other computer. A browser connects using this app’s QR code.")).font(.caption).foregroundStyle(.secondary)
            HStack { Spacer(); Button(model.text("Abbrechen", "Cancel")) { dismiss() }; Button(model.text("Anfrage senden", "Request pairing")) { model.connect(url: url, fingerprint: fingerprint); dismiss() }.buttonStyle(.borderedProminent).disabled(!valid || model.status == nil || model.engineError != nil) }
        }.textFieldStyle(.roundedBorder).padding(28).frame(width: 510)
    }
    private var valid: Bool {
        guard let parsed = URL(string: url), parsed.scheme == "https", parsed.host != nil else { return false }
        let normalized = fingerprint.filter { !$0.isWhitespace && $0 != ":" }
        return normalized.count == 64 && normalized.allSatisfy(\.isHexDigit)
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
            Toggle(model.text("Eingehende Übertragungen automatisch annehmen", "Automatically accept incoming transfers"), isOn: $device.autoAccept)
                .disabled(!device.canSend)
            if let fingerprint = device.fingerprint { Text(fingerprint).font(.system(.caption, design: .monospaced)).foregroundStyle(.secondary).textSelection(.enabled) }
            HStack {
                Button(model.text("Vertrauen widerrufen …", "Revoke trust …"), role: .destructive) { confirmRevoke = true }
                Spacer(); Button(model.text("Abbrechen", "Cancel")) { dismiss() }
                Button(model.text("Speichern", "Save")) { model.update(device); dismiss() }.buttonStyle(.borderedProminent)
            }
        }.padding(28).frame(width: 540)
        .confirmationDialog(model.text("Vertrauen für \(device.name) widerrufen?", "Revoke trust for \(device.name)?"), isPresented: $confirmRevoke) {
            Button(model.text("Widerrufen", "Revoke"), role: .destructive) { model.command("device.revoke", params: ["id": device.id]); dismiss() }
        }
    }
}
