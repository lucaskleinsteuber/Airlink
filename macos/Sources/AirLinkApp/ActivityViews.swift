import AppKit
import AirLinkCore
import SwiftUI
import UniformTypeIdentifiers

/// Requests that need the user: files waiting for approval, received text.
struct Banners: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        let requests = model.incomingRequests.prefix(2), clips = model.receivedClipboard
        if !requests.isEmpty || !clips.isEmpty {
            VStack(spacing: 10) {
                ForEach(Array(requests)) { t in
                    Banner(symbol: "tray.and.arrow.down.fill", title: model.text("\(t.deviceName) möchte dir etwas senden", "\(t.deviceName) wants to send you something"),
                           detail: model.describe(t) + " · " + TransferFormatting.bytes(t.totalBytes)) {
                        Button(model.text("Ablehnen", "Decline")) { model.cancel(t) }
                        Button(model.text("Annehmen", "Accept")) { model.accept(t) }.buttonStyle(.borderedProminent)
                    }
                }
                if let clip = clips.first {
                    Banner(symbol: "doc.on.clipboard.fill", title: model.text("Text von \(clip.deviceName)", "Text from \(clip.deviceName)") + (clips.count > 1 ? model.text(" (+\(clips.count - 1) weitere)", " (+\(clips.count - 1) more)") : ""),
                           detail: model.clipboardTexts[clip.id] ?? "") {
                        Button { model.dismiss(clip) } label: { Image(systemName: "xmark") }.buttonStyle(.borderless).help(model.text("Verwerfen", "Dismiss"))
                        Button(model.text("Kopieren", "Copy")) { model.copy(clip) }.buttonStyle(.borderedProminent)
                    }
                }
            }
        }
    }
}

struct Banner<Actions: View>: View {
    let symbol: String
    let title: String
    let detail: String
    @ViewBuilder let actions: Actions
    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: symbol).font(.title2).foregroundStyle(.tint).frame(width: 30)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).fontWeight(.semibold).lineLimit(1)
                if !detail.isEmpty { Text(detail).font(.callout).foregroundStyle(.secondary).lineLimit(2).truncationMode(.tail) }
            }
            Spacer(minLength: 8)
            HStack(spacing: 8) { actions }
        }
        .padding(14)
        .glassPanel(radius: 14)
    }
}

/// The newest transfers below the computers.
struct RecentArea: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        let items = model.recentTransfers
        if !items.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                HStack(alignment: .firstTextBaseline) {
                    Text(model.text("Zuletzt", "Recent")).font(.headline)
                    Spacer()
                    Button(model.text("Alle anzeigen", "Show All")) { model.showHistory() }.buttonStyle(.link)
                }
                VStack(spacing: 0) {
                    ForEach(Array(items.enumerated()), id: \.element.id) { index, transfer in
                        if index > 0 { Divider().padding(.leading, 62) }
                        TransferRow(transfer: transfer)
                    }
                }
                .glassPanel(radius: 14)
            }
        }
    }
}

struct TransferRow: View {
    @EnvironmentObject var model: AppModel
    let transfer: Transfer
    var body: some View {
        HStack(spacing: 12) {
            FileIcon(transfer: transfer)
            VStack(alignment: .leading, spacing: 4) {
                Text(model.describe(transfer)).fontWeight(.medium).lineLimit(1).truncationMode(.middle)
                if transfer.state == "running" {
                    ProgressView(value: transfer.fractionCompleted).controlSize(.small).tint(.accentColor)
                }
                Text(detail).font(.caption).foregroundStyle(transfer.state == "failed" ? AnyShapeStyle(.red) : AnyShapeStyle(.secondary)).lineLimit(2).monospacedDigit()
            }
            Spacer(minLength: 8)
            actions
        }
        .padding(.horizontal, 14).padding(.vertical, 10)
        .contentShape(Rectangle())
    }

    private var peer: String {
        transfer.direction == "send" ? model.text("An \(transfer.deviceName)", "To \(transfer.deviceName)") : model.text("Von \(transfer.deviceName)", "From \(transfer.deviceName)")
    }
    private var detail: String {
        let size = TransferFormatting.bytes(transfer.totalBytes)
        let percent = Int((transfer.fractionCompleted * 100).rounded(.down))
        switch transfer.state {
        case "running":
            var parts = ["\(percent) %", TransferFormatting.speed(transfer.bytesPerSecond)]
            if let seconds = DesktopTransferSelection.remainingSeconds(transfer) { parts.append(remaining(seconds)) }
            return peer + " · " + parts.joined(separator: " · ")
        case "paused": return peer + " · " + model.text("Angehalten bei \(percent) %", "Paused at \(percent)%")
        case "offered": return transfer.direction == "send" ? peer + " · " + model.text("Wartet, bis \(transfer.deviceName) annimmt", "Waiting for \(transfer.deviceName) to accept") : peer + " · " + size
        case "queued", "reconnecting", "verifying": return peer + " · " + model.text("Wartet auf \(transfer.deviceName) … (\(percent) %)", "Waiting for \(transfer.deviceName) … (\(percent)%)")
        case "failed":
            if let problem = transfer.problem { return model.text(problem.titleDE + " " + problem.recoveryDE, problem.titleEN + " " + problem.recoveryEN) }
            return peer + " · " + model.text("Fehlgeschlagen", "Failed") + (transfer.error.map { ": " + $0 } ?? "")
        case "cancelled": return peer + " · " + model.text("Abgebrochen", "Cancelled")
        default: return peer + " · " + size + " · " + when
        }
    }
    private var when: String {
        let formatter = RelativeDateTimeFormatter()
        formatter.locale = Locale(identifier: model.english ? "en" : "de")
        formatter.unitsStyle = .full
        guard let date = ISO8601DateFormatter.flexible(transfer.updatedAt) else { return "" }
        return formatter.localizedString(for: min(date, Date()), relativeTo: Date())
    }
    private func remaining(_ seconds: Int) -> String {
        if seconds < 60 { return model.text("noch \(seconds) s", "\(seconds) s left") }
        if seconds < 3600 { return model.text("noch \(seconds / 60) min", "\(seconds / 60) min left") }
        return model.text("noch \(seconds / 3600) h \((seconds % 3600) / 60) min", "\(seconds / 3600) h \((seconds % 3600) / 60) min left")
    }
    @ViewBuilder private var actions: some View {
        HStack(spacing: 6) {
            switch transfer.state {
            case "running", "queued", "reconnecting", "verifying":
                iconButton("pause.fill", model.text("Anhalten", "Pause")) { model.pause(transfer) }
                iconButton("xmark", model.text("Abbrechen", "Cancel")) { model.cancel(transfer) }
            case "paused":
                iconButton("play.fill", model.text("Fortsetzen", "Resume")) { model.resume(transfer) }
                iconButton("xmark", model.text("Abbrechen", "Cancel")) { model.cancel(transfer) }
            case "offered":
                if transfer.direction == "receive" {
                    Button(model.text("Annehmen", "Accept")) { model.accept(transfer) }.buttonStyle(.borderedProminent).controlSize(.small)
                } else {
                    iconButton("xmark", model.text("Abbrechen", "Cancel")) { model.cancel(transfer) }
                }
            case "completed":
                if transfer.direction == "receive" && transfer.hasReceivedContent {
                    Button(model.text("Zeigen", "Show")) { model.reveal(transfer) }.buttonStyle(.bordered).controlSize(.small)
                        .help(model.text("Im Finder zeigen", "Show in Finder"))
                } else {
                    Image(systemName: "checkmark.circle.fill").foregroundStyle(.green).help(model.text("Fertig", "Done"))
                }
            case "failed":
                if transfer.retryable == true {
                    Button(model.text("Erneut versuchen", "Try Again")) { model.resume(transfer) }.buttonStyle(.bordered).controlSize(.small)
                }
            default:
                EmptyView()
            }
        }
    }
    private func iconButton(_ symbol: String, _ help: String, action: @escaping () -> Void) -> some View {
        Button(action: action) { Image(systemName: symbol).frame(width: 22, height: 22) }
            .buttonStyle(.borderless).help(help).accessibilityLabel(help)
    }
}

extension ISO8601DateFormatter {
    static func flexible(_ value: String) -> Date? {
        let precise = ISO8601DateFormatter(); precise.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return precise.date(from: value) ?? ISO8601DateFormatter().date(from: value)
    }
}

/// The Finder icon of the (first) file, with a small direction badge.
struct FileIcon: View {
    let transfer: Transfer
    var body: some View {
        Image(nsImage: icon).resizable().interpolation(.high).frame(width: 36, height: 36)
            .overlay(alignment: .bottomTrailing) {
                Image(systemName: transfer.direction == "send" ? "arrow.up.circle.fill" : "arrow.down.circle.fill")
                    .font(.system(size: 14, weight: .semibold))
                    .symbolRenderingMode(.palette)
                    .foregroundStyle(.white, transfer.direction == "send" ? Color.accentColor : Color.green)
                    .background(Circle().fill(Color(nsColor: .windowBackgroundColor)).padding(1))
                    .offset(x: 4, y: 3)
            }
            .padding(.trailing, 4)
    }
    private var icon: NSImage {
        guard let first = transfer.files.first else { return NSWorkspace.shared.icon(for: .data) }
        if first.directory == true { return NSWorkspace.shared.icon(for: .folder) }
        let ext = (first.path as NSString).pathExtension
        return NSWorkspace.shared.icon(for: UTType(filenameExtension: ext) ?? .data)
    }
}

/// All transfers, newest first, loaded page by page.
struct HistoryView: View {
    @EnvironmentObject var model: AppModel
    @State private var items: [Transfer] = []
    @State private var cursor: String?
    @State private var loading = false
    @State private var deviceID: String?
    @State private var confirmClear = false
    private var rows: [Transfer] {
        DesktopTransferSelection.mergeHistory(items, summary: model.transfers.filter { deviceID == nil || $0.deviceId == deviceID })
    }
    var body: some View {
        Group {
            if rows.isEmpty && !loading {
                ContentUnavailableView {
                    Label(model.text("Noch keine Übertragungen", "No Transfers Yet"), systemImage: "clock")
                } description: {
                    Text(model.text("Was du sendest und empfängst, erscheint hier.", "What you send and receive appears here."))
                }
            } else {
                List {
                    ForEach(rows) { transfer in
                        TransferRow(transfer: transfer)
                            .listRowInsets(EdgeInsets())
                            .contextMenu {
                                if transfer.canRemoveFromHistory {
                                    Button(model.text("Aus dem Verlauf entfernen", "Remove from History")) { remove(transfer) }
                                }
                            }
                    }
                    if cursor != nil {
                        Button(model.text("Ältere laden", "Load Older")) { Task { await load(more: true) } }.disabled(loading)
                    }
                }
                .listStyle(.inset)
            }
        }
        .frame(minWidth: 520, minHeight: 420)
        .navigationTitle(model.text("Verlauf", "History"))
        .toolbar {
            ToolbarItemGroup {
                Picker(model.text("Gerät", "Device"), selection: $deviceID) {
                    Text(model.text("Alle Computer", "All Computers")).tag(String?.none)
                    ForEach(model.pairedDevices) { Text($0.name).tag(Optional($0.id)) }
                }.pickerStyle(.menu)
                Button(model.text("Verlauf leeren …", "Clear History …")) { confirmClear = true }.disabled(rows.isEmpty)
            }
        }
        .confirmationDialog(model.text("Verlauf leeren?", "Clear history?"), isPresented: $confirmClear) {
            Button(model.text("Verlauf leeren", "Clear History"), role: .destructive) {
                Task { do { try await model.clearHistory() } catch { model.error = error.localizedDescription } }
            }
        } message: { Text(model.text("Nur die Liste wird geleert. Deine Dateien bleiben, wo sie sind.", "Only the list is cleared. Your files stay where they are.")) }
        .task(id: deviceID) { await load() }
        .onChange(of: model.historyRevision) { _, _ in Task { await load() } }
    }
    private func load(more: Bool = false) async {
        guard model.isReady, !loading else { return }
        loading = true
        defer { loading = false }
        do {
            let page = try await model.transferPage(cursor: more ? cursor : nil, deviceID: deviceID)
            items = more ? items + page.transfers : page.transfers
            cursor = page.nextCursor
        } catch { model.error = error.localizedDescription }
    }
    private func remove(_ transfer: Transfer) {
        Task {
            do { try await model.removeFromHistory(transfer); items.removeAll { $0.id == transfer.id } }
            catch { model.error = error.localizedDescription }
        }
    }
}
