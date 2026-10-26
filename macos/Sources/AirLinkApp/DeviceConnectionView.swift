import AirLinkCore
import SwiftUI

struct ConnectView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var browser = false
    @State private var reviewing = false
    @State private var manual = false
    @State private var requesting = false
    @State private var verification = PeerVerification()
    @State private var issue: String?
    @State private var sentRequest = false
    private var nearby: [Device] {
        (model.status?.devices ?? []).filter { !$0.paired && $0.kind == "engine" && $0.address != nil }
            .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }
    private var pending: PeerConnectionAttempt? {
        guard let attempt = model.pendingConnection, attempt.matches(model.connectionDraft) else { return nil }
        return attempt
    }
    private var waiting: Bool { pending?.isExpired() == false }
    private var pairedID: String? { model.connectionDraft.pairedDevice(in: model.status?.devices ?? [])?.id }
    private var peerName: String {
        (model.status?.devices ?? []).first { $0.fingerprint?.lowercased() == model.connectionDraft.normalizedFingerprint }?.name
            ?? model.text("Anderer Computer", "Other computer")
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                Text(model.text("Gerät hinzufügen", "Add a device")).font(.title2.weight(.semibold))
                Spacer()
                Button(waiting ? model.text("Im Hintergrund warten", "Wait in background") : model.text("Fertig", "Done")) { dismiss() }.disabled(requesting)
            }
            Picker(model.text("Gerätetyp", "Device type"), selection: $browser) {
                Text(model.text("Mac oder Windows-PC", "Mac or Windows PC")).tag(false)
                Text(model.text("Handy oder Tablet", "Phone or tablet")).tag(true)
            }.pickerStyle(.segmented).disabled(requesting || waiting)
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    if browser { LocalConnectionView(embedded: true).environmentObject(model) }
                    else if reviewing { review }
                    else { deviceSelection }
                }.frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 4)
            }.frame(minHeight: 330, maxHeight: 530)
            if let issue { Text(issue).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
            if !browser {
                Divider()
                HStack {
                    if reviewing && !waiting && !requesting {
                        Button(model.text("Anderes Gerät wählen", "Choose another device")) { reviewing = false; verification.reset(); issue = nil }
                    }
                    Spacer()
                    if reviewing && pairedID == nil {
                        Button(requesting ? model.text("Wird verbunden …", "Connecting …") : model.text("Verbinden", "Connect")) { request() }
                            .buttonStyle(.borderedProminent)
                            .disabled(!verification.matches(model.connectionDraft) || !model.isReady || requesting || waiting)
                    }
                }
            }
        }.textFieldStyle(.roundedBorder).padding(26).frame(width: 580)
            .onAppear {
                reviewing = model.connectionDraft.isValid || pending != nil
                manual = nearby.isEmpty
                if pending != nil { sentRequest = true }
            }
            .onChange(of: model.connectionDraft.normalizedAddress) { _, _ in verification.reset() }
            .onChange(of: model.connectionDraft.normalizedFingerprint) { _, _ in verification.reset() }
            .onChange(of: pairedID) { _, id in
                if let id, sentRequest { model.selectedDeviceID = id; dismiss() }
            }
            .interactiveDismissDisabled(requesting)
    }
    private var deviceSelection: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(model.text("1. Wähle den anderen Computer", "1. Choose the other computer")).font(.headline)
            Text(model.text("Öffne AirLink auf beiden Geräten und verbinde sie mit demselben Netzwerk. Du musst ein Gerät nur einmal hinzufügen.", "Open AirLink on both devices and connect them to the same network. Each device only needs to be added once.")).foregroundStyle(.secondary)
            if nearby.isEmpty {
                Label(model.text("Noch kein Computer gefunden", "No computer found yet"), systemImage: "antenna.radiowaves.left.and.right")
                    .foregroundStyle(.secondary).padding(.vertical, 16)
            }
            ForEach(nearby) { device in
                Button {
                    model.connectionDraft = PeerConnectionDraft(device: device)
                    verification.reset(); issue = nil; reviewing = true
                } label: {
                    HStack(spacing: 12) {
                        Image(systemName: "desktopcomputer").font(.title2).frame(width: 34)
                        VStack(alignment: .leading, spacing: 4) {
                            Text(device.name).fontWeight(.medium).lineLimit(2)
                            Text(model.text("Im selben Netzwerk gefunden", "Found on your network")).font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Image(systemName: "chevron.right").foregroundStyle(.secondary)
                    }.padding(16).frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 12))
                }.buttonStyle(.plain).disabled(!model.isReady)
            }
            DisclosureGroup(model.text("Nicht gefunden? Manuell verbinden", "Not found? Connect manually"), isExpanded: $manual) {
                VStack(alignment: .leading, spacing: 12) {
                    Text(model.text("Öffne auf dem anderen Computer „Dieses Gerät“. Übernimm seine HTTPS-Adresse und den vollständigen Fingerabdruck.", "Open This device on the other computer. Copy its HTTPS address and complete fingerprint.")).font(.callout).foregroundStyle(.secondary)
                    TextField(model.text("HTTPS-Adresse des anderen Computers", "Other computer’s HTTPS address"), text: $model.connectionDraft.address)
                    TextField(model.text("Vollständiger SHA-256-Fingerabdruck", "Complete SHA-256 fingerprint"), text: $model.connectionDraft.fingerprint).font(.system(.callout, design: .monospaced))
                    Button(model.text("Weiter", "Continue")) { verification.reset(); issue = nil; reviewing = true }
                        .disabled(!model.connectionDraft.isValid || !model.isReady)
                }.padding(.top, 12)
            }
        }
    }
    private var review: some View {
        VStack(alignment: .leading, spacing: 16) {
            Label(peerName, systemImage: "desktopcomputer").font(.title3.weight(.semibold))
            Text(model.text("2. Vergleiche einmal die Geräteidentität", "2. Compare the device identity once")).font(.headline)
            Text(model.text("Öffne auf \(peerName) „Dieses Gerät“ und vergleiche den gesamten Fingerabdruck mit dieser Anzeige.", "Open This device on \(peerName) and compare the complete fingerprint with this display.")).foregroundStyle(.secondary)
            Text(model.connectionDraft.comparisonFingerprint).font(.system(.body, design: .monospaced)).textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading).padding(16)
                .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 12))
            Toggle(model.text("Der Fingerabdruck stimmt überein", "The fingerprint matches"), isOn: Binding(
                get: { verification.matches(model.connectionDraft) },
                set: { if $0 { verification.confirm(model.connectionDraft) } else { verification.reset() } }
            )).disabled(requesting || waiting)
            Divider()
            Toggle(model.text("Dateien von diesem Gerät automatisch empfangen", "Automatically receive files from this device"), isOn: $model.connectionDraft.automaticReceive)
                .disabled(requesting || waiting)
            Text(model.text("Optional. Dieser Mac nimmt künftige Dateien dieses Geräts ohne Rückfrage an. Du kannst das später direkt beim Gerät ändern.", "Optional. This Mac will accept future files from this device without asking. You can change this on the device card later.")).font(.caption).foregroundStyle(.secondary)
            if let pending {
                TimelineView(.periodic(from: .now, by: 1)) { context in
                    if pending.isExpired(at: context.date) {
                        Label(model.text("Noch nicht bestätigt. Prüfe den anderen Computer und versuche es erneut.", "Not confirmed yet. Check the other computer and try again."), systemImage: "clock.badge.exclamationmark").foregroundStyle(.orange)
                    } else {
                        HStack(spacing: 10) {
                            ProgressView().controlSize(.small)
                            Text(model.text("3. Jetzt auf \(peerName) bestätigen", "3. Now approve on \(peerName)")).fontWeight(.medium)
                        }
                    }
                }.font(.callout).padding(.vertical, 8)
            } else {
                Text(model.text("Nach „Verbinden“ bestätigst du einmal auf dem anderen Computer. Danach erscheint er unter „Vertraute Geräte“.", "After choosing Connect, approve once on the other computer. It will then appear under Trusted devices.")).font(.callout).foregroundStyle(.secondary)
            }
        }
    }
    private func request() {
        let draft = model.connectionDraft
        guard !requesting, verification.matches(draft) else { return }
        requesting = true; issue = nil
        let compared = verification
        Task {
            do {
                try await model.connect(draft, verification: compared)
                sentRequest = true
                if let id = pairedID { model.selectedDeviceID = id; dismiss() }
            } catch { issue = error.localizedDescription }
            requesting = false
        }
    }
}

struct TrustedDeviceSummary: View {
    @EnvironmentObject var model: AppModel
    let deviceID: String
    var onManage: () -> Void
    @State private var updating = false
    @State private var issue: String?
    private var device: Device? { model.status?.devices.first { $0.id == deviceID && $0.paired } }
    var body: some View {
        if let device {
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    Label(model.text("Vertrautes Gerät", "Trusted device"), systemImage: "checkmark.shield").font(.headline)
                    Spacer()
                    Button(model.text("Geräteeinstellungen …", "Device settings …"), action: onManage).buttonStyle(.link)
                }
                if device.canSend {
                    Toggle(model.text("Von \(device.name) automatisch empfangen", "Automatically receive from \(device.name)"), isOn: Binding(
                        get: { device.autoAccept },
                        set: { value in
                            let id = device.id
                            updating = true; issue = nil
                            Task {
                                do { try await model.setAutomaticReceive(value, deviceID: id) }
                                catch { issue = error.localizedDescription }
                                updating = false
                            }
                        }
                    )).disabled(updating || !model.isReady)
                    Text(device.autoAccept ? model.text("Neue Dateien dieses Geräts kommen ohne erneute Annahme an.", "New files from this device arrive without another acceptance prompt.") : model.text("Du bestätigst eingehende Dateien dieses Geräts einzeln.", "You approve incoming files from this device individually.")).font(.caption).foregroundStyle(.secondary)
                } else { Text(model.text("Dieses Gerät darf derzeit keine Dateien an diesen Mac senden.", "This device is currently not allowed to send files to this Mac.")).font(.callout).foregroundStyle(.secondary) }
                if let issue { Text(issue).font(.caption).foregroundStyle(.red).textSelection(.enabled) }
            }.padding(18).frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 14))
        }
    }
}
