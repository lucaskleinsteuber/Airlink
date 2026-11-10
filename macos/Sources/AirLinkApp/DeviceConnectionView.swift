import AirLinkCore
import SwiftUI

struct ConnectView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var browser = false
    @State private var manual = false
    @State private var requesting = false
    @State private var automatic = false
    @State private var verified = false
    @State private var issue: String?
    @State private var sentLegacy = false
    private var nearby: [Device] {
        (model.status?.devices ?? []).filter { !$0.paired && $0.kind == "engine" && $0.address != nil }
            .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }
    private var session: PairingSession? {
        (model.status?.pairingSessions ?? []).first { $0.id == model.selectedPairingSessionID }
    }
    private var openSessions: [PairingSession] {
        (model.status?.pairingSessions ?? []).filter { ["starting", "comparing", "finishing"].contains($0.state) }
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            HStack {
                Text(model.text("Gerät hinzufügen", "Add a device")).font(.title2.weight(.semibold))
                Spacer()
                Button(session?.localConfirmed == true ? model.text("Im Hintergrund warten", "Wait in background") : model.text("Fertig", "Done")) { dismiss() }.disabled(requesting)
            }
            if session == nil {
                Picker(model.text("Gerätetyp", "Device type"), selection: $browser) {
                    Text(model.text("Computer", "Computer")).tag(false)
                    Text(model.text("Handy oder Tablet", "Phone or tablet")).tag(true)
                }.pickerStyle(.segmented)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    if browser { LocalConnectionView(embedded: true).environmentObject(model) }
                    else if let session { comparison(session) }
                    else { selection }
                }.frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 3)
            }.frame(minHeight: 330, maxHeight: 520)
            if let issue { Text(issue).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
            if let session, !session.localConfirmed, ["starting", "comparing"].contains(session.state) {
                Button(model.text("Verbindungsanfrage abbrechen", "Cancel connection request")) {
                    requesting = true
                    Task {
                        do { try await model.cancelPairing(session.id); model.selectedPairingSessionID = nil }
                        catch { issue = error.localizedDescription }
                        requesting = false
                    }
                }.disabled(requesting)
            } else if let session, ["expired", "denied", "cancelled"].contains(session.state) {
                Button(model.text("Anderes Gerät wählen", "Choose another device")) { model.selectedPairingSessionID = nil; automatic = false; issue = nil }
            }
        }.padding(26).frame(width: 580).textFieldStyle(.roundedBorder)
            .onChange(of: session?.id) { _, _ in automatic = session?.automaticReceive ?? false }
            .onChange(of: session?.state) { _, state in if state == "paired", let session { model.selectedDeviceID = session.peerId; dismiss() } }
            .onChange(of: model.connectionDraft.normalizedAddress) { _, _ in verified = false }
            .onChange(of: model.connectionDraft.normalizedFingerprint) { _, _ in verified = false }
            .onChange(of: model.connectionDraft.pairedDevice(in: model.status?.devices ?? [])?.id) { _, id in if let id, sentLegacy { model.selectedDeviceID = id; dismiss() } }
            .interactiveDismissDisabled(requesting)
    }
    private var selection: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(model.text("Öffne AirLink auf beiden Computern und wähle das andere Gerät.", "Open AirLink on both computers and choose the other device.")).foregroundStyle(.secondary)
            ForEach(openSessions) { pending in
                Button { model.selectedPairingSessionID = pending.id } label: { Label(model.text("Verbindung mit \(pending.peerName) fortsetzen", "Continue connecting to \(pending.peerName)"), systemImage: "checkmark.shield") }.buttonStyle(.bordered)
            }
            if nearby.isEmpty { ContentUnavailableView(model.text("Noch kein Computer gefunden", "No computer found yet"), systemImage: "antenna.radiowaves.left.and.right", description: Text(model.text("Beide Geräte müssen im selben Netzwerk erreichbar sein.", "Both devices must be reachable on the same network."))) }
            ForEach(nearby) { device in
                Button {
                    requesting = true; issue = nil
                    Task { do { try await model.beginPairing(device) } catch { issue = error.localizedDescription }; requesting = false }
                } label: {
                    HStack(spacing: 14) {
                        Image(systemName: "desktopcomputer").font(.title2).frame(width: 32)
                        VStack(alignment: .leading, spacing: 4) { Text(device.name).fontWeight(.medium).lineLimit(2); Text(model.text("Im lokalen Netzwerk gefunden", "Found on your local network")).font(.caption).foregroundStyle(.secondary) }
                        Spacer(); Image(systemName: "chevron.right").foregroundStyle(.secondary)
                    }.padding(15).frame(maxWidth: .infinity, alignment: .leading).background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 10))
                }.buttonStyle(.plain).disabled(requesting || !model.isReady)
            }
            DisclosureGroup(model.text("Andere Verbindungsmöglichkeiten", "Other connection options"), isExpanded: $manual) {
                VStack(alignment: .leading, spacing: 12) {
                    Text(model.text("Für ältere Apps oder manuelle Verbindungen: Übernimm Adresse und Fingerabdruck aus „Dieses Gerät“ auf dem anderen Computer.", "For older apps or manual connections, copy the address and fingerprint from This device on the other computer.")).font(.callout).foregroundStyle(.secondary)
                    TextField(model.text("HTTPS-Adresse", "HTTPS address"), text: $model.connectionDraft.address)
                    TextField(model.text("Vollständiger SHA-256-Fingerabdruck", "Complete SHA-256 fingerprint"), text: $model.connectionDraft.fingerprint).font(.system(.callout, design: .monospaced))
                    Toggle(model.text("Ich habe den gesamten Fingerabdruck auf dem anderen Gerät verglichen.", "I compared the complete fingerprint on the other device."), isOn: $verified)
                    Toggle(model.text("Dateien dieses Geräts künftig automatisch annehmen", "Automatically receive files from this device in future"), isOn: $model.connectionDraft.automaticReceive)
                    Button(model.text("Verbindung anfragen", "Request connection")) {
                        var comparison = PeerVerification(); comparison.confirm(model.connectionDraft)
                        let draft = model.connectionDraft
                        requesting = true
                        Task { do { try await model.connect(draft, verification: comparison); sentLegacy = true } catch { issue = error.localizedDescription }; requesting = false }
                    }.disabled(!verified || !model.connectionDraft.isValid || requesting || !model.isReady)
                    if sentLegacy { Label(model.text("Jetzt auf dem anderen Computer bestätigen.", "Now approve on the other computer."), systemImage: "clock").font(.callout).foregroundStyle(.secondary) }
                }.padding(.top, 12)
            }
        }
    }
    @ViewBuilder private func comparison(_ session: PairingSession) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            Text(session.peerName.isEmpty ? model.text("Verbindung wird vorbereitet …", "Preparing connection …") : session.peerName).font(.title3.weight(.semibold))
            if session.state == "comparing" {
                Text(model.text("Vergleiche diesen Code auf beiden Geräten.", "Compare this code on both devices.")).foregroundStyle(.secondary)
                Text(session.formattedCode).font(.system(size: 38, weight: .medium, design: .monospaced)).frame(maxWidth: .infinity).padding(.vertical, 20)
                if session.localConfirmed {
                    HStack(spacing: 10) { ProgressView().controlSize(.small); Text(model.text("Warte auf die Bestätigung am anderen Gerät.", "Waiting for confirmation on the other device.")) }
                } else {
                    Text(model.text("Der Code ist fünf Minuten gültig. Bestätige nur, wenn er übereinstimmt.", "The code is valid for five minutes. Confirm only if it matches.")).font(.callout).foregroundStyle(.secondary)
                    Toggle(model.text("Dateien dieses Geräts künftig automatisch annehmen", "Automatically receive files from this device in future"), isOn: $automatic)
                    Text(model.text("Optional. Gilt nur für den Empfang auf diesem Mac.", "Optional. Applies only to receiving on this Mac.")).font(.caption).foregroundStyle(.secondary)
                    Button(model.text("Code stimmt überein", "Code matches")) {
                        requesting = true; issue = nil
                        Task { do { try await model.confirmPairing(session.id, automatic: automatic) } catch { issue = error.localizedDescription }; requesting = false }
                    }.buttonStyle(.borderedProminent).disabled(!session.canConfirm || requesting || !model.isReady)
                }
            } else if session.state == "starting" || session.state == "finishing" {
                HStack(spacing: 10) { ProgressView().controlSize(.small); Text(session.state == "starting" ? model.text("Der andere Computer wird erreicht …", "Contacting the other computer …") : model.text("Verbindung wird gespeichert …", "Saving connection …")) }
            } else if session.state == "paired" {
                Label(model.text("Verbunden. Du kannst jetzt Dateien senden.", "Connected. You can now send files."), systemImage: "checkmark.circle.fill").foregroundStyle(.green)
            } else { Label(session.error ?? model.text("Diese Anfrage ist abgelaufen oder wurde abgebrochen.", "This request expired or was cancelled."), systemImage: "exclamationmark.circle").foregroundStyle(.secondary) }
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
