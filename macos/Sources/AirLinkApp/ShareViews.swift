import AppKit
import AirLinkCore
import CoreImage.CIFilterBuiltins
import SwiftUI

struct ShareListView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var showCreator = false
    @State private var selectedShare: Share?
    @State private var revokeShare: Share?
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                Text("Quick Share").font(.title2.weight(.semibold))
                Spacer()
                Button(model.text("Neue Freigabe …", "New share …")) { showCreator = true }.buttonStyle(.borderedProminent).disabled(model.status == nil || model.engineError != nil)
                Button(model.text("Fertig", "Done")) { dismiss() }
            }
            Text(model.text("Freigabelinks funktionieren im lokalen Netzwerk. Die Originaldateien bleiben an ihrem Platz.", "Share links work on your local network. Original files stay in place.")).foregroundStyle(.secondary)
            if model.shares.isEmpty {
                ContentUnavailableView(model.text("Noch keine Freigaben", "No shares yet"), systemImage: "link", description: Text(model.text("Dateien auswählen und einen Freigabelink erstellen.", "Choose files to create a share link."))).frame(minHeight: 200)
            } else {
                ScrollView {
                    LazyVStack(spacing: 12) {
                        ForEach(model.shares.sorted { $0.createdAt > $1.createdAt }) { share in
                            VStack(alignment: .leading, spacing: 9) {
                                HStack {
                                    Label(share.name, systemImage: share.passwordProtected ? "lock.fill" : "link").font(.headline).lineLimit(1)
                                    Spacer()
                                    Text(shareState(share)).font(.caption).foregroundStyle(.secondary)
                                }
                                HStack {
                                    Text(TransferFormatting.bytes(share.totalBytes))
                                    Text("·")
                                    Text(share.maxDownloads == 0 ? model.text("\(share.downloads) Downloads", "\(share.downloads) downloads") : "\(share.downloads) / \(share.maxDownloads) " + model.text("Downloads", "downloads"))
                                }.font(.caption).foregroundStyle(.secondary)
                                if let expiry = share.expirationDate {
                                    Text(model.text("Läuft ab: ", "Expires: ") + expiry.formatted(date: .abbreviated, time: .shortened)).font(.caption).foregroundStyle(.secondary)
                                }
                                HStack {
                                    Button(model.text("Link und QR-Code", "Link and QR code")) { selectedShare = share }.disabled(share.url == nil || share.revoked || share.isExpired())
                                    Spacer()
                                    if !share.revoked {
                                        Button(model.text("Widerrufen …", "Revoke …"), role: .destructive) { revokeShare = share }
                                    }
                                }
                            }.padding(16).background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 12))
                        }
                    }
                }.frame(minHeight: 240, maxHeight: 440)
            }
        }.padding(28).frame(width: 620)
        .sheet(isPresented: $showCreator) { ShareCreateView(initialPaths: model.pendingURLs).environmentObject(model) }
        .sheet(item: $selectedShare) { share in ShareLinkView(share: share).environmentObject(model) }
        .confirmationDialog(model.text("Freigabe widerrufen? Laufende Downloads werden beendet.", "Revoke this share? Active downloads will stop."), isPresented: Binding(get: { revokeShare != nil }, set: { if !$0 { revokeShare = nil } })) {
            Button(model.text("Widerrufen", "Revoke"), role: .destructive) {
                if let share = revokeShare { model.command("share.revoke", params: ["id": share.id]) }
                revokeShare = nil
            }
        }
    }
    private func shareState(_ share: Share) -> String {
        if share.revoked { return model.text("Widerrufen", "Revoked") }
        if share.isExpired() { return model.text("Abgelaufen", "Expired") }
        if share.exhausted { return model.text("Downloadlimit erreicht", "Download limit reached") }
        return model.text("Aktiv", "Active")
    }
}

struct ShareCreateView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var paths: [URL]
    @State private var name = ""
    @State private var password = ""
    @State private var neverExpires = false
    @State private var expiration = Date().addingTimeInterval(24 * 60 * 60)
    @State private var unlimited = true
    @State private var maximum: Int64 = 1
    @State private var creating = false
    @State private var error: String?
    @State private var created: Share?
    init(initialPaths: [URL]) { _paths = State(initialValue: initialPaths) }
    var body: some View {
        if let created {
            ShareLinkView(share: created).environmentObject(model)
        } else {
            VStack(alignment: .leading, spacing: 18) {
                Text(model.text("Freigabelink erstellen", "Create a share link")).font(.title2.weight(.semibold))
                Form {
                    LabeledContent(model.text("Dateien", "Files")) {
                        VStack(alignment: .trailing, spacing: 8) {
                            Text(paths.isEmpty ? model.text("Keine Auswahl", "Nothing selected") : paths.map(\.lastPathComponent).joined(separator: ", ")).lineLimit(3).foregroundStyle(.secondary)
                            Button(model.text("Dateien oder Ordner auswählen …", "Choose files or folders …")) { chooseSources() }
                        }
                    }
                    TextField(model.text("Name (optional)", "Name (optional)"), text: $name)
                    SecureField(model.text("Passwort (optional)", "Password (optional)"), text: $password)
                    Toggle(model.text("Kein Ablaufdatum", "Never expires"), isOn: $neverExpires)
                    if !neverExpires { DatePicker(model.text("Gültig bis", "Expires"), selection: $expiration, in: Date()..., displayedComponents: [.date, .hourAndMinute]) }
                    Toggle(model.text("Unbegrenzte Downloads", "Unlimited downloads"), isOn: $unlimited)
                    if !unlimited {
                        TextField(model.text("Downloadlimit", "Download limit"), value: $maximum, format: .number).textFieldStyle(.roundedBorder)
                    }
                }.formStyle(.grouped).disabled(creating)
                Text(model.text("Empfänger benötigen das Zertifikat dieses Macs. Ein Download zählt pro Sitzung; Fortsetzen verbraucht kein zusätzliches Kontingent.", "Recipients need to trust this Mac’s certificate. A download counts once per session; resuming does not consume another allowance.")).font(.caption).foregroundStyle(.secondary)
                if let error { Text(error).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
                HStack {
                    Spacer()
                    Button(model.text("Abbrechen", "Cancel")) { dismiss() }.disabled(creating)
                    Button(creating ? model.text("Wird erstellt …", "Creating …") : model.text("Link erstellen", "Create link")) { create() }.buttonStyle(.borderedProminent).disabled(!valid || creating || model.engineError != nil)
                }
            }.padding(28).frame(width: 580)
        }
    }
    private var valid: Bool { !paths.isEmpty && model.status != nil && (neverExpires || expiration > Date()) && (unlimited || maximum > 0) }
    private func chooseSources() {
        let panel = NSOpenPanel(); panel.canChooseFiles = true; panel.canChooseDirectories = true; panel.allowsMultipleSelection = true
        panel.begin { response in if response == .OK { paths = panel.urls } }
    }
    private func create() {
        creating = true; error = nil
        let request = ShareCreate(name: name, paths: paths.map(\.path), password: password.isEmpty ? nil : password, expiresAt: neverExpires ? nil : ShareDate.string(expiration), maxDownloads: unlimited ? 0 : maximum)
        Task {
            do { created = try await model.createShare(request); password = "" }
            catch { self.error = error.localizedDescription }
            creating = false
        }
    }
}

struct ShareLinkView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let share: Share
    @State private var copied = false
    private var current: Share { model.shares.first { $0.id == share.id } ?? share }
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack { Text(current.name).font(.title2.weight(.semibold)); Spacer(); Button(model.text("Fertig", "Done")) { dismiss() } }
            if current.revoked || current.isExpired() {
                Label(model.text("Diese Freigabe ist nicht mehr aktiv.", "This share is no longer active."), systemImage: "link.badge.plus").foregroundStyle(.secondary)
            } else if let link = current.url {
                if let image = qr(link) { Image(nsImage: image).resizable().interpolation(.none).frame(width: 190, height: 190).padding(14).background(.white, in: RoundedRectangle(cornerRadius: 14)).frame(maxWidth: .infinity) }
                Text(link).font(.system(.callout, design: .monospaced)).textSelection(.enabled)
                Button(copied ? model.text("Link kopiert", "Link copied") : model.text("Link kopieren", "Copy link")) {
                    NSPasteboard.general.clearContents(); NSPasteboard.general.setString(link, forType: .string); copied = true
                }.buttonStyle(.borderedProminent)
            }
            Text(model.text("Nur im lokalen Netzwerk erreichbar. Teile den vollständigen Link nur mit den vorgesehenen Empfängern. Ein Passwort wird getrennt weitergegeben.", "Available on your local network only. Share the complete link only with its intended recipients. Send any password separately.")).font(.callout).foregroundStyle(.secondary)
        }.padding(28).frame(width: 560)
    }
    private func qr(_ link: String) -> NSImage? {
        let filter = CIFilter.qrCodeGenerator(); filter.message = Data(link.utf8)
        guard let output = filter.outputImage?.transformed(by: CGAffineTransform(scaleX: 8, y: 8)), let cg = CIContext().createCGImage(output, from: output.extent) else { return nil }
        return NSImage(cgImage: cg, size: NSSize(width: output.extent.width, height: output.extent.height))
    }
}
