import AirLinkCore
import SwiftUI

struct HistoryView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var tab = 0
    @State private var filter = HistoryFilter()
    @State private var history = History()
    @State private var knownDevices: [String: String] = [:]
    @State private var loading = false
    @State private var loadID = UUID()
    @State private var error: String?
    @State private var confirmClear = false
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                Text(model.text("Verlauf und Statistik", "History and statistics")).font(.title2.weight(.semibold))
                Spacer()
                Button(model.text("Fertig", "Done")) { dismiss() }
            }
            Picker("", selection: $tab) {
                Text(model.text("Verlauf", "History")).tag(0)
                Text(model.text("Statistik", "Statistics")).tag(1)
            }.pickerStyle(.segmented)
            if tab == 0 {
                TextField(model.text("Datei- oder Gerätenamen suchen", "Search file or device names"), text: $filter.query).textFieldStyle(.roundedBorder)
                HStack {
                    Picker(model.text("Status", "State"), selection: $filter.state) {
                        Text(model.text("Alle", "All")).tag("")
                        ForEach(["offered", "queued", "running", "paused", "reconnecting", "verifying", "completed", "failed", "cancelled"], id: \.self) { Text(model.stateLabel($0)).tag($0) }
                    }
                    Picker(model.text("Richtung", "Direction"), selection: $filter.direction) {
                        Text(model.text("Alle", "All")).tag("")
                        Text(model.text("Senden", "Send")).tag("send")
                        Text(model.text("Empfangen", "Receive")).tag("receive")
                    }
                }
                HStack {
                    Picker(model.text("Gerät", "Device"), selection: $filter.deviceId) {
                        Text(model.text("Alle", "All")).tag("")
                        ForEach(knownDevices.keys.sorted(), id: \.self) { Text(knownDevices[$0] ?? $0).tag($0) }
                    }
                    Picker(model.text("Einträge", "Entries"), selection: $filter.limit) { Text("100").tag(100); Text("250").tag(250); Text("500").tag(500) }.frame(width: 155)
                    Button { Task { await load() } } label: { Image(systemName: "arrow.clockwise") }.disabled(loading || !model.isReady)
                }
                if loading { ProgressView().controlSize(.small) }
                if let error { Text(error).foregroundStyle(.red).font(.callout).textSelection(.enabled) }
                ScrollView {
                    LazyVStack(spacing: 10) {
                        if history.entries.isEmpty { ContentUnavailableView(model.text("Keine passenden Einträge", "No matching entries"), systemImage: "clock") }
                        ForEach(history.entries.filter(filter.matches)) { entry in
                            TransferRow(transfer: entry.transfer).environmentObject(model)
                        }
                    }
                }.frame(minHeight: 220, maxHeight: 390)
                HStack {
                    Text(model.text("Dateien bleiben erhalten.", "Files remain unchanged.")).font(.caption).foregroundStyle(.secondary)
                    Spacer()
                    Button(model.text("Beendeten Verlauf löschen …", "Clear finished history …"), role: .destructive) { confirmClear = true }.disabled(!model.isReady || model.removingHistory)
                }
            } else {
                StatisticsView().environmentObject(model).frame(minHeight: 330, maxHeight: 490)
            }
        }.padding(26).frame(width: 740)
        .task(id: filter) { try? await Task.sleep(for: .milliseconds(200)); if !Task.isCancelled { await load() } }
        .onChange(of: model.removingHistory) { _, removing in if !removing { Task { await load() } } }
        .confirmationDialog(model.text("Alle beendeten Einträge dieses Hosts löschen?", "Clear all finished entries on this host?"), isPresented: $confirmClear) {
            Button(model.text("Beendeten Verlauf löschen", "Clear finished history"), role: .destructive) {
                Task {
                    do {
                        try await model.clearHistory()
                        knownDevices = Dictionary(uniqueKeysWithValues: (model.status?.devices ?? []).map { ($0.id, $0.name) })
                        if !filter.deviceId.isEmpty && knownDevices[filter.deviceId] == nil { filter.deviceId = "" }
                        await load()
                    }
                    catch { self.error = error.localizedDescription }
                }
            }
        } message: {
            Text(model.text("Entfernt nur Metadaten abgeschlossener, fehlgeschlagener und abgebrochener Einträge. Originale, empfangene Dateien, Zwischenablagetexte und aktive Vorgänge bleiben erhalten. Lebenslange Datenzähler bleiben bestehen.", "Removes metadata only for completed, failed, and cancelled entries. Originals, received files, clipboard text, and active work remain. Lifetime traffic counters are preserved."))
        }
    }
    private func load() async {
        loading = true
        let requestID = UUID()
        loadID = requestID
        let requested = filter
        do {
            let result = try await model.history(requested)
            guard !Task.isCancelled, loadID == requestID, filter == requested else { return }
            history = result; error = nil
            for device in model.status?.devices ?? [] { knownDevices[device.id] = device.name }
            for transfer in result.transfers { knownDevices[transfer.deviceId] = transfer.deviceName }
        } catch { if !Task.isCancelled, loadID == requestID { self.error = error.localizedDescription } }
        if loadID == requestID, filter == requested { loading = false }
    }
}

struct StatisticsView: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        ScrollView {
            if let stats = model.status?.statistics {
                VStack(alignment: .leading, spacing: 18) {
                    Text(model.text("Bestätigte Dateiübertragungen", "Confirmed file transfers")).font(.headline)
                    LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 12) {
                        metric(model.text("Gesendet", "Sent"), TransferFormatting.bytes(stats.sentBytes))
                        metric(model.text("Empfangen", "Received"), TransferFormatting.bytes(stats.receivedBytes))
                        metric(model.text("Abgeschlossen", "Completed"), String(stats.completedTransfers))
                        metric(model.text("Aktiv", "Active"), String(stats.activeTransfers))
                        metric(model.text("Fehlgeschlagen", "Failed"), String(stats.failedTransfers))
                        metric(model.text("Abgebrochen", "Cancelled"), String(stats.cancelledTransfers))
                    }
                    Text(model.text("Nutzdaten · Lebenslang", "Payload traffic · lifetime")).font(.headline)
                    HStack { metric(model.text("Gesendet", "Sent"), TransferFormatting.bytes(stats.trafficSentBytes)); metric(model.text("Empfangen", "Received"), TransferFormatting.bytes(stats.trafficReceivedBytes)) }
                    Text(model.text("Verlaufssummen ändern sich beim Löschen von Verlaufseinträgen. Lebenslange Nutzdatenzähler bleiben erhalten.", "History totals change when history is cleared. Lifetime payload counters remain.")).font(.caption).foregroundStyle(.secondary)
                    if let days = stats.days, !days.isEmpty {
                        Text(model.text("Verlauf pro Tag · letzte Aktivität, UTC", "History by day · last activity, UTC")).font(.headline)
                        ForEach(days, id: \.date) { day in
                            VStack(alignment: .leading, spacing: 4) {
                                Text(day.date).font(.callout.weight(.medium))
                                Text("↑ \(TransferFormatting.bytes(day.sentBytes))   ↓ \(TransferFormatting.bytes(day.receivedBytes))   · \(day.completedTransfers) " + model.text("abgeschlossen", "completed")).font(.caption).foregroundStyle(.secondary)
                            }.padding(.vertical, 5)
                        }
                    }
                }
            } else { ContentUnavailableView(model.text("Statistik noch nicht verfügbar", "Statistics not available yet"), systemImage: "chart.bar") }
        }
    }
    private func metric(_ title: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 5) { Text(title).font(.caption).foregroundStyle(.secondary); Text(value).font(.title3.weight(.medium)).monospacedDigit() }.padding(12).frame(maxWidth: .infinity, alignment: .leading).background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 10))
    }
}

extension AppModel {
    func stateLabel(_ state: String) -> String {
        let labels: [String: (String, String)] = ["offered": ("Wartet auf Annahme", "Awaiting acceptance"), "queued": ("In Warteschlange", "Queued"), "running": ("Wird übertragen", "Transferring"), "paused": ("Pausiert", "Paused"), "reconnecting": ("Verbindet erneut", "Reconnecting"), "verifying": ("Wird geprüft", "Verifying"), "completed": ("Abgeschlossen", "Completed"), "failed": ("Fehlgeschlagen", "Failed"), "cancelled": ("Abgebrochen", "Cancelled")]
        let label = labels[state] ?? (state, state)
        return text(label.0, label.1)
    }
}
