import AirLinkCore
import SwiftUI

struct DesktopTransfersView: View {
    @EnvironmentObject var model: AppModel
    @State private var history: [Transfer] = []
    @State private var cursor: String?
    @State private var loading = false
    @State private var issue: String?
    @State private var generation = UUID()
    private var rows: [Transfer] {
        DesktopTransferSelection.mergeHistory(history, summary: model.status?.transfers ?? [])
    }
    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 14) {
                HStack {
                    VStack(alignment: .leading, spacing: 7) {
                        Text(model.text("Übertragungen", "Transfers")).font(.largeTitle.weight(.semibold))
                        Text(model.text("Aktive Sendungen und Verlauf. Deine Dateien bleiben beim Entfernen erhalten.", "Active transfers and history. Removing an entry keeps your files.")).foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button { Task { await load(more: false) } } label: { Label(model.text("Aktualisieren", "Refresh"), systemImage: "arrow.clockwise") }.disabled(loading || !model.isReady)
                }
                if rows.isEmpty, !loading { ContentUnavailableView(model.text("Noch keine Übertragungen", "No transfers yet"), systemImage: "tray") }
                ForEach(rows) { TransferRow(transfer: $0).environmentObject(model) }
                if let issue { Text(issue).foregroundStyle(.red).font(.callout).textSelection(.enabled) }
                HStack {
                    if loading { ProgressView().controlSize(.small) }
                    if cursor != nil { Button(model.text("Weitere 50 laden", "Load 50 more")) { Task { await load(more: true) } }.disabled(loading) }
                    Spacer()
                    RemoveFinishedTransfersButton(transfers: rows).environmentObject(model)
                }
            }.padding(28).frame(maxWidth: 1000, alignment: .leading).frame(maxWidth: .infinity)
        }.task(id: model.historyRevision) { await load(more: false) }
    }
    private func load(more: Bool) async {
        guard model.isReady else { return }
        let requestID = UUID(); generation = requestID; loading = true
        do {
            let page = try await model.transferPage(cursor: more ? cursor : nil)
            guard generation == requestID, !Task.isCancelled else { return }
            if more { let existing = Set(history.map(\.id)); history += page.transfers.filter { !existing.contains($0.id) } }
            else { history = page.transfers }
            cursor = page.nextCursor; issue = nil
        } catch { if generation == requestID { issue = error.localizedDescription } }
        if generation == requestID { loading = false }
    }
}

struct TransferDetailsSheet: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let transfer: Transfer
    @State private var files: [TransferFile] = []
    @State private var next = 0
    @State private var loading = false
    @State private var issue: String?
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack { Text(transfer.title).font(.title2.weight(.semibold)); Spacer(); Button(model.text("Fertig", "Done")) { dismiss() } }
            Text(transfer.deviceName + " · " + model.stateLabel(transfer.state)).foregroundStyle(.secondary)
            if let error = transfer.error { Text(error).foregroundStyle(.red).textSelection(.enabled) }
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 15) {
                    ForEach(files) { file in
                        VStack(alignment: .leading, spacing: 6) {
                            Label(file.path, systemImage: file.directory == true ? "folder" : "doc").textSelection(.enabled)
                            if file.directory != true { Text(TransferFormatting.bytes(file.size)).font(.caption).foregroundStyle(.secondary) }
                            if file.isSkipped { Text(model.text("Übersprungen · keine neue Dateikopie", "Skipped · no new file copy")).font(.caption).foregroundStyle(.secondary) }
                            else if let destination = file.destinationPath, destination != file.path { Text(destination).font(.caption).foregroundStyle(.secondary).textSelection(.enabled) }
                            if file.duplicateState == "pending" { DuplicateAttentionView(transfer: transfer, file: file).environmentObject(model) }
                        }
                        Divider()
                    }
                }
            }.frame(minHeight: 240, maxHeight: 440)
            if let issue { Text(issue).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
            HStack {
                if next >= 0 { Button(model.text("Weitere Dateien laden", "Load more files")) { Task { await load() } }.disabled(loading) }
                if loading { ProgressView().controlSize(.small) }
                Spacer()
                if transfer.state == "completed", transfer.hasReceivedContent {
                    Button(model.text("Im Finder zeigen", "Show in Finder")) { Task { do { try await model.revealReceived(transfer) } catch { issue = error.localizedDescription } } }
                }
            }
        }.padding(26).frame(width: 680).task { await load() }
    }
    private func load() async {
        guard next >= 0, !loading else { return }
        loading = true
        do {
            let result = try await model.filePage(transferID: transfer.id, offset: next)
            guard !Task.isCancelled else { loading = false; return }
            files += result.files; next = result.nextOffset; issue = nil
        } catch { issue = error.localizedDescription }
        loading = false
    }
}

struct StatisticsSheet: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack { Text(model.text("Statistik", "Statistics")).font(.title2.weight(.semibold)); Spacer(); Button(model.text("Fertig", "Done")) { dismiss() } }
            StatisticsView().environmentObject(model)
        }.padding(26).frame(width: 680, height: 550)
            .onAppear { model.observeTool("statistics", active: true) }
            .onDisappear { model.observeTool("statistics", active: false) }
    }
}
