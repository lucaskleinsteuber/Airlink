import AppKit
import AirLinkCore
import SwiftUI

enum ThisMac {
    /// "macbook" on portables, "desktopcomputer" otherwise.
    static let symbol: String = {
        var size = 0
        sysctlbyname("hw.model", nil, &size, nil, 0)
        var buffer = [CChar](repeating: 0, count: max(1, size))
        sysctlbyname("hw.model", &buffer, &size, nil, 0)
        let model = String(cString: buffer).lowercased()
        return DeviceSymbol.name(platform: "macos", form: model.contains("book") ? "laptop" : "desktop")
    }()
}

/// Both computers show the same code; each side confirms that it matches.
struct PairingSheet: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let sessionID: String
    @State private var automatic = true
    @State private var working = false
    @State private var issue: String?

    private var session: PairingSession? { model.pairing(sessionID) }
    private var peer: String {
        guard let session, !session.peerName.isEmpty else { return model.text("dem anderen Computer", "the other computer") }
        return session.peerName
    }
    private var peerTitle: String { session.flatMap { $0.peerName.isEmpty ? nil : $0.peerName } ?? model.text("Anderer Computer", "Other computer") }

    var body: some View {
        VStack(spacing: 22) {
            header
            VStack(spacing: 10) {
                Text(title).font(.title2.weight(.semibold)).multilineTextAlignment(.center)
                content
            }
            if let issue { Text(issue).font(.callout).foregroundStyle(.red).multilineTextAlignment(.center) }
            buttons
        }
        .padding(.horizontal, 34).padding(.vertical, 30)
        .frame(width: 470)
        .interactiveDismissDisabled(working)
    }

    private var header: some View {
        HStack(spacing: 14) {
            icon(ThisMac.symbol)
            Group {
                if session?.state == "paired" {
                    Image(systemName: "checkmark.circle.fill").font(.title).foregroundStyle(.green).symbolEffectBounce()
                } else {
                    HStack(spacing: 5) { ForEach(0..<3) { _ in Circle().frame(width: 5, height: 5) } }.foregroundStyle(.tertiary)
                }
            }.frame(width: 44)
            icon(DeviceSymbol.name(platform: session?.platform, form: session?.form))
        }
    }
    private func icon(_ symbol: String) -> some View {
        Image(systemName: symbol).font(.system(size: 28)).symbolRenderingMode(.hierarchical).foregroundStyle(.tint)
            .frame(width: 68, height: 68).glassCircle(tint: Color.accentColor.opacity(0.10), interactive: false)
    }

    private var title: String {
        guard let session else { return model.text("Anfrage abgelaufen", "Request expired") }
        switch session.state {
        case "paired": return model.text("Verbunden!", "Connected!")
        case "starting": return model.text("Verbinden mit \(peerTitle)", "Connecting to \(peerTitle)")
        case "comparing", "finishing":
            return session.initiator == true ? model.text("Mit \(peerTitle) verbinden", "Connect to \(peerTitle)") : model.text("\(peerTitle) möchte sich verbinden", "\(peerTitle) wants to connect")
        default: return model.text("Verbinden hat nicht geklappt", "Could not connect")
        }
    }

    @ViewBuilder private var content: some View {
        if let session {
            switch session.state {
            case "starting":
                waiting(startingText(session))
            case "comparing" where !session.localConfirmed:
                Text(model.text("Steht auf \(peer) derselbe Code?", "Does \(peer) show the same code?")).foregroundStyle(.secondary)
                code(session.formattedCode)
                Toggle(model.text("Dateien von \(peerTitle) ohne Nachfrage annehmen", "Accept files from \(peerTitle) without asking"), isOn: $automatic)
                    .toggleStyle(.checkbox).padding(.top, 4)
                Text(model.text("Bestätige nur, wenn beide Codes gleich sind. So weißt du sicher, dass du mit deinem eigenen Computer verbunden bist.", "Confirm only if both codes match. That way you know for sure it is your own computer."))
                    .font(.caption).foregroundStyle(.secondary).multilineTextAlignment(.center).fixedSize(horizontal: false, vertical: true)
            case "comparing":
                code(session.formattedCode).opacity(0.5)
                waiting(model.text("Bestätige jetzt auch auf \(peer).", "Now confirm on \(peer) too."))
            case "finishing":
                waiting(model.text("Verbindung wird gespeichert …", "Saving the connection …"))
            case "paired":
                Text(model.text("Zieh jetzt Dateien auf \(peerTitle) – oder klick den Computer an, um Dateien auszuwählen.", "Now drag files onto \(peerTitle), or click the computer to choose files."))
                    .foregroundStyle(.secondary).multilineTextAlignment(.center).fixedSize(horizontal: false, vertical: true)
            default:
                Text(failureText(session)).foregroundStyle(.secondary).multilineTextAlignment(.center).fixedSize(horizontal: false, vertical: true)
            }
        } else {
            Text(model.text("Diese Anfrage gibt es nicht mehr. Starte das Verbinden einfach noch einmal.", "This request no longer exists. Simply start connecting again.")).foregroundStyle(.secondary).multilineTextAlignment(.center)
        }
    }

    private func code(_ value: String) -> some View {
        Text(value).font(.system(size: 44, weight: .semibold, design: .rounded)).monospacedDigit().tracking(2)
            .padding(.horizontal, 26).padding(.vertical, 12)
            .glassPanel(radius: 18)
            .textSelection(.enabled)
            .accessibilityLabel(model.text("Code ", "Code ") + value.map(String.init).joined(separator: " "))
    }
    private func waiting(_ message: String) -> some View {
        VStack(spacing: 12) {
            ProgressView().controlSize(.small)
            Text(message).foregroundStyle(.secondary).multilineTextAlignment(.center).fixedSize(horizontal: false, vertical: true)
        }.padding(.top, 4)
    }
    private func startingText(_ session: PairingSession) -> String {
        switch session.problem {
        case "closed":
            return model.text("Öffne AirLink auf \(peer). Sobald es dort offen ist, geht es hier von selbst weiter.", "Open AirLink on \(peer). As soon as it is open there, this continues by itself.")
        case "unreachable":
            return model.text("\(peerTitle) antwortet nicht. Ist AirLink dort geöffnet und seid ihr im selben Netzwerk? Falls dort eine Firewall aktiv ist (oft unter Linux): Klick stattdessen auf \(peer) auf diesen Mac – so herum klappt es.", "\(peerTitle) does not answer. Is AirLink open there and are you on the same network? If a firewall is active there (common on Linux), click this Mac on \(peer) instead – that direction works.")
        default:
            return model.text("Verbindung zu \(peer) wird aufgebaut …", "Contacting \(peer) …")
        }
    }
    private func failureText(_ session: PairingSession) -> String {
        switch session.problem {
        case "outdated": return model.text("\(peerTitle) hat eine ältere AirLink-Version. Aktualisiere AirLink dort auf 0.13 oder neuer.", "\(peerTitle) runs an older AirLink. Update AirLink there to 0.13 or later.")
        case "paired": return model.text("Ihr seid bereits verbunden.", "You are already connected.")
        case "self": return model.text("Das ist die Adresse dieses Macs. Gib die Adresse des anderen Computers ein.", "That is the address of this Mac. Enter the other computer's address.")
        case "identity": return model.text("Der Computer hat sich anders ausgewiesen als erwartet. Wähle ihn noch einmal aus der Liste.", "The computer identified itself differently than expected. Choose it from the list again.")
        default:
            if session.state == "cancelled" || session.state == "denied" { return model.text("Das Verbinden wurde abgebrochen.", "Connecting was cancelled.") }
            return model.text("Die Anfrage ist abgelaufen. Versuch es einfach noch einmal.", "The request expired. Simply try again.")
        }
    }

    @ViewBuilder private var buttons: some View {
        HStack(spacing: 12) {
            if let session, session.state == "comparing", !session.localConfirmed {
                Button(session.initiator == true ? model.text("Abbrechen", "Cancel") : model.text("Ablehnen", "Decline")) { cancel() }
                    .keyboardShortcut(.cancelAction).controlSize(.large)
                Button(model.text("Code stimmt", "Code Matches")) { confirm(session) }
                    .keyboardShortcut(.defaultAction).buttonStyle(.borderedProminent).controlSize(.large).disabled(working || !session.canConfirm)
            } else if let session, session.isOpen {
                Button(model.text("Abbrechen", "Cancel")) { cancel() }.keyboardShortcut(.cancelAction).controlSize(.large)
                    .disabled(session.localConfirmed && session.state != "comparing")
            } else {
                Button(session?.state == "paired" ? model.text("Fertig", "Done") : model.text("Schließen", "Close")) { dismiss() }
                    .keyboardShortcut(.defaultAction).buttonStyle(.borderedProminent).controlSize(.large)
            }
        }
    }
    private func confirm(_ session: PairingSession) {
        working = true; issue = nil
        Task {
            do { try await model.confirmPairing(session.id, automatic: automatic) }
            catch { issue = model.text("Bestätigen hat nicht geklappt. Die Anfrage ist vielleicht abgelaufen – versuch es noch einmal.", "Confirming did not work. The request may have expired – please try again.") }
            working = false
        }
    }
    private func cancel() {
        working = true
        Task { await model.cancelPairing(sessionID); working = false; dismiss() }
    }
}

private extension View {
    @ViewBuilder func symbolEffectBounce() -> some View {
        self.symbolEffect(.bounce, value: true)
    }
}

/// How to add a computer, the computers nearby and the address fallback.
struct AddDeviceSheet: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var byAddress: Bool
    @State private var address = ""
    @State private var working = false
    @State private var issue: String?
    init(byAddress: Bool) { _byAddress = State(initialValue: byAddress) }

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text(model.text("Computer hinzufügen", "Add a computer")).font(.title2.weight(.semibold))
            VStack(alignment: .leading, spacing: 12) {
                Step(number: 1, text: model.text("Installiere AirLink auf dem anderen Computer und öffne es.", "Install AirLink on the other computer and open it."))
                Step(number: 2, text: model.text("Beide Computer sind im selben Netzwerk (WLAN oder Kabel).", "Both computers are on the same network (Wi-Fi or cable)."))
                Step(number: 3, text: model.text("Klick den Computer hier an und vergleicht den Code.", "Click the computer here and compare the code."))
            }
            VStack(alignment: .leading, spacing: 8) {
                Text(model.text("In der Nähe", "Nearby")).font(.headline)
                VStack(spacing: 0) {
                    if model.nearbyDevices.isEmpty {
                        HStack(spacing: 10) {
                            ProgressView().controlSize(.small)
                            Text(model.text("Suche nach Computern mit AirLink …", "Looking for computers with AirLink …")).foregroundStyle(.secondary)
                            Spacer()
                        }.padding(14)
                    }
                    ForEach(Array(model.nearbyDevices.enumerated()), id: \.element.id) { index, device in
                        if index > 0 { Divider().padding(.leading, 52) }
                        HStack(spacing: 12) {
                            Image(systemName: DeviceSymbol.name(device)).font(.title2).symbolRenderingMode(.hierarchical).foregroundStyle(.tint).frame(width: 30)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(device.name).fontWeight(.medium)
                                if device.outdated == true { Text(model.text("Braucht AirLink 0.13 oder neuer", "Needs AirLink 0.13 or later")).font(.caption).foregroundStyle(.orange) }
                            }
                            Spacer()
                            Button(model.text("Verbinden", "Connect")) { model.connect(device) }.disabled(device.outdated == true)
                        }.padding(.horizontal, 14).padding(.vertical, 10)
                    }
                }.glassPanel(radius: 12)
            }
            DisclosureGroup(model.text("Nicht gefunden? Per IP-Adresse verbinden", "Not found? Connect by IP address"), isExpanded: $byAddress) {
                VStack(alignment: .leading, spacing: 10) {
                    Text(model.text("Gib die IP-Adresse des anderen Computers ein. AirLink zeigt sie dort unter Hilfe (?).", "Enter the other computer's IP address. AirLink shows it there under Help (?)."))
                        .font(.callout).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    HStack {
                        TextField(model.text("z. B. 192.168.178.22", "e.g. 192.168.178.22"), text: $address).textFieldStyle(.roundedBorder).onSubmit(connect)
                        Button(model.text("Verbinden", "Connect"), action: connect).disabled(address.trimmingCharacters(in: .whitespaces).isEmpty || working)
                    }
                    if let issue { Text(issue).font(.callout).foregroundStyle(.red).fixedSize(horizontal: false, vertical: true) }
                    if let own = model.localAddresses.first {
                        Text(model.text("Die Adresse dieses Macs: ", "This Mac's address: ") + own).font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
                    }
                }.padding(.top, 8)
            }
            HStack { Spacer(); Button(model.text("Fertig", "Done")) { dismiss() }.keyboardShortcut(.cancelAction).controlSize(.large) }
        }
        .padding(28).frame(width: 500)
    }
    private func connect() {
        let value = address.trimmingCharacters(in: .whitespaces)
        guard !value.isEmpty, !working else { return }
        working = true; issue = nil
        Task {
            do { try await model.connect(address: value) } catch { issue = error.localizedDescription }
            working = false
        }
    }
}

struct Step: View {
    let number: Int
    let text: String
    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text("\(number)").font(.callout.weight(.bold)).foregroundStyle(.white)
                .frame(width: 24, height: 24).background(Circle().fill(Color.accentColor))
                .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 5 }
            Text(text).fixedSize(horizontal: false, vertical: true)
        }
    }
}
