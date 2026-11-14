import AirLinkCore
import SwiftUI

struct IncomingApprovalView: View {
    @EnvironmentObject var model: AppModel
    let transfer: Transfer
    @State private var automatic = false
    @State private var working = false
    @State private var issue: String?
    private var device: Device? { model.status?.devices.first { $0.id == transfer.deviceId } }
    private var mayTrust: Bool { transfer.canAcceptAndTrust(device: device) }
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if mayTrust && device?.autoAccept != true {
                Toggle(model.text("Von \(transfer.deviceName) künftig automatisch empfangen", "Automatically receive from \(transfer.deviceName) from now on"), isOn: $automatic)
                Text(model.text("Optional: Dieses gekoppelte Gerät darf weitere Dateien ohne erneute Annahme senden. Andere Geräte bleiben davon unberührt.", "Optional: this paired device may send more files without another acceptance prompt. Other devices are unaffected.")).font(.caption).foregroundStyle(.secondary)
            }
            HStack {
                if working { ProgressView().controlSize(.small) }
                Spacer()
                Button(automatic ? model.text("Annehmen und künftig automatisch", "Accept and receive automatically") : model.text("Annehmen", "Accept")) { accept() }.buttonStyle(.borderedProminent)
                    .disabled(!mayTrust || !model.isReady || working)
            }
            if !mayTrust { Text(model.text("Zum Empfangen muss dieses Gerät gekoppelt sein und senden dürfen.", "The sender must be paired and have permission to send.")).font(.caption).foregroundStyle(.secondary) }
            if let issue { Text(issue).font(.caption).foregroundStyle(.red).textSelection(.enabled) }
        }.padding(12).background(.blue.opacity(0.05), in: RoundedRectangle(cornerRadius: 10)).disabled(working)
    }
    private func accept() {
        guard !working else { return }
        working = true; issue = nil
        Task {
            do { try await model.acceptIncoming(transfer.id, automatic: automatic) }
            catch { issue = error.localizedDescription }
            working = false
        }
    }
}

struct ReceivedResultView: View {
    @EnvironmentObject var model: AppModel
    let transfer: Transfer
    @State private var working = false
    @State private var issue: String?
    private var entries: [TransferFile] { transfer.receivedResultEntries }
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 12) {
                Label(entries.count == 1 ? model.text("Empfangen und geprüft", "Received and verified") : model.text("\(entries.count) Objekte empfangen und geprüft", "\(entries.count) items received and verified"), systemImage: "checkmark.circle.fill").font(.callout).foregroundStyle(.green)
                Spacer()
                if working { ProgressView().controlSize(.small) }
                Button(primaryTitle) { reveal() }.buttonStyle(.bordered).disabled(!model.isReady || working)
            }
            if let path = displayedLocation {
                Text(model.text("Gespeichert in: ", "Saved in: ") + (path as NSString).abbreviatingWithTildeInPath).font(.caption).foregroundStyle(.secondary).lineLimit(2).truncationMode(.middle).textSelection(.enabled)
            }
            if entries.count > 1 {
                DisclosureGroup(model.text("Empfangene Dateien und Ordner", "Received files and folders")) {
                    ForEach(entries) { file in
                        HStack {
                            Label(file.path, systemImage: file.directory == true ? "folder" : "doc").lineLimit(2)
                            Spacer()
                            Button(file.directory == true ? model.text("Ordner öffnen", "Open folder") : model.text("Datei zeigen", "Show file")) { reveal(fileID: file.id) }.disabled(!model.isReady || working)
                        }.font(.caption)
                    }
                }.font(.caption)
            }
            if let issue { Text(issue).font(.caption).foregroundStyle(.red).textSelection(.enabled) }
        }.padding(12).background(.green.opacity(0.05), in: RoundedRectangle(cornerRadius: 10))
    }
    private var primaryTitle: String {
        if entries.count == 1 { return entries[0].directory == true ? model.text("Ordner öffnen", "Open folder") : model.text("Datei zeigen", "Show file") }
        return model.text("Empfangsordner öffnen", "Open receive folder")
    }
    private var displayedLocation: String? {
        guard let root = transfer.destination else { return nil }
        guard entries.count == 1 else { return root }
        let file = entries[0]
        let destination = URL(fileURLWithPath: root).appendingPathComponent(file.destinationPath ?? file.path)
        return file.directory == true ? destination.path : destination.deletingLastPathComponent().path
    }
    private func reveal(fileID: String? = nil) {
        guard !working else { return }
        working = true; issue = nil
        Task {
            do { try await model.revealReceived(transfer, fileID: fileID) }
            catch { issue = error.localizedDescription }
            working = false
        }
    }
}
