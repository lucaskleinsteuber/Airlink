import AirLinkCore
import SwiftUI

struct RemoveTransferButton: View {
    @EnvironmentObject var model: AppModel
    let transfer: Transfer
    @State private var issue: String?
    var body: some View {
        VStack(alignment: .trailing, spacing: 5) {
            Button {
                issue = nil
                Task {
                    do { try await model.removeFromHistory([transfer]) }
                    catch { issue = error.localizedDescription }
                }
            } label: { Label(model.text("Aus Liste entfernen", "Remove from list"), systemImage: "xmark.circle") }
                .buttonStyle(.borderless)
                .disabled(!model.isReady || model.removingHistory)
                .help(model.text("Nur diesen Eintrag entfernen. Die Dateien bleiben erhalten.", "Remove this entry only. Files are kept."))
            if let issue { Text(issue).font(.caption).foregroundStyle(.red).textSelection(.enabled) }
        }
    }
}

struct RemoveFinishedTransfersButton: View {
    @EnvironmentObject var model: AppModel
    let transfers: [Transfer]
    @State private var selected: [Transfer] = []
    @State private var confirm = false
    var body: some View {
        Button(model.text("Fertige Einträge entfernen …", "Remove finished entries …")) {
            selected = transfers.filter(\.canRemoveFromHistory)
            confirm = !selected.isEmpty
        }.buttonStyle(.link).disabled(!model.isReady || model.removingHistory || TransferHistorySelection.removableIDs(in: transfers).isEmpty)
            .confirmationDialog(model.text("\(selected.count) fertige Einträge entfernen?", "Remove \(selected.count) finished entries?"), isPresented: $confirm) {
                Button(model.text("Aus Liste entfernen", "Remove from list")) {
                    let captured = selected
                    Task {
                        do { try await model.removeFromHistory(captured) }
                        catch { model.error = error.localizedDescription }
                    }
                }
            } message: {
                Text(model.text("Entfernt die ausgewählten abgeschlossenen, fehlgeschlagenen und abgebrochenen Übertragungen aus dem Verlauf. Deine Dateien bleiben erhalten. Laufende und später fertig werdende Übertragungen bleiben in der Liste.", "Removes the selected completed, failed and cancelled transfers from history. Your files are kept. Active transfers and transfers finishing later stay in the list."))
            }
    }
}
