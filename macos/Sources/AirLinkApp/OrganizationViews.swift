import AirLinkCore
import SwiftUI

struct OrganizationSection: View {
    @EnvironmentObject var model: AppModel
    @Binding var organization: Organization
    @State private var editRules = false
    var body: some View {
        Section(model.text("Neue Dateien organisieren", "Organize new incoming files")) {
            Toggle(model.text("Inhaltsgleiche Dateien erkennen", "Detect identical file contents"), isOn: $organization.detectDuplicates)
            Text(model.text("Nur geprüfte, von AirLink empfangene Dateien werden verglichen. Ein Treffer benötigt immer deine Entscheidung: weitere Kopie oder neue Datei überspringen. Vorhandene Dateien werden nicht gelöscht.", "Only verified AirLink received files are compared. Each match needs your decision: receive another copy or skip the new file. Existing files are not deleted.")).font(.caption).foregroundStyle(.secondary)
            Toggle(model.text("Einzelne Dateien nach Dateiendung sortieren", "Sort individual files by extension"), isOn: $organization.sortEnabled)
            HStack {
                Text(model.text("\(organization.rules.count) geordnete Regeln", "\(organization.rules.count) ordered rules")).font(.callout).foregroundStyle(.secondary)
                Spacer()
                Button(model.text("Regeln und Vorschläge …", "Rules and presets …")) { editRules = true }
            }
            if organization.sortEnabled && organization.rules.isEmpty {
                Text(model.text("Eine Regel hinzufügen, damit neue Einzeldateien einsortiert werden können.", "Add a rule to classify new individual files.")).font(.caption).foregroundStyle(.secondary)
            }
            Text(model.text("Beides ist standardmäßig aus. Änderungen gelten erst für neue eingehende Angebote. Laufende und abgeschlossene Dateien werden nicht verschoben. Übertragene Ordner bleiben mit ihrer gesamten Struktur zusammen.", "Both features default to off. Changes apply only to new incoming offers. Active and completed files are not moved. Transferred folders keep their complete structure together.")).font(.caption).foregroundStyle(.secondary)
        }
        .sheet(isPresented: $editRules) { OrganizationRulesView(rules: $organization.rules).environmentObject(model) }
    }
}

struct OrganizationRulesView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @Binding var rules: [SortRule]
    @State private var draft: [SortRule] = []
    private var valid: Bool { Organization(rules: draft).isValid }
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(model.text("Sortierregeln", "Sorting rules")).font(.title2.weight(.semibold))
            Text(model.text("Die erste passende Regel gewinnt. Nur einzelne Dateien auf der obersten Ebene werden einsortiert; Ordnerhierarchien bleiben unverändert. Die Ordner sind relativ zum neuen Empfangsziel der jeweiligen Übertragung.", "The first matching rule wins. Only individual top-level files are classified; folder hierarchies remain unchanged. Folders are relative to each transfer’s new receive destination.")).font(.callout).foregroundStyle(.secondary)
            HStack {
                Button(model.text("Vorschläge hinzufügen", "Add presets")) {
                    let ids = Set(draft.map(\.id)); draft += Organization.presets.filter { !ids.contains($0.id) }.prefix(max(0, 100 - draft.count))
                }.disabled(draft.count >= 100)
                Button(model.text("Neue Regel", "New rule")) { draft.append(SortRule()) }.disabled(draft.count >= 100)
                Spacer()
                Text(model.text("Images · Videos · Audio · Documents · Archives", "Images · Videos · Audio · Documents · Archives")).font(.caption).foregroundStyle(.secondary)
            }
            ScrollView {
                LazyVStack(spacing: 12) {
                    if draft.isEmpty { ContentUnavailableView(model.text("Noch keine Regeln", "No rules yet"), systemImage: "folder.badge.gearshape", description: Text(model.text("Vorschläge ansehen oder eigene Regeln hinzufügen. Sortieren wird dadurch nicht eingeschaltet.", "Add editable presets or your own rules. This does not enable sorting."))) }
                    ForEach($draft) { $rule in
                        SortRuleRow(rule: $rule, first: draft.first?.id == rule.id, last: draft.last?.id == rule.id, up: { move(rule.id, by: -1) }, down: { move(rule.id, by: 1) }, remove: { draft.removeAll { $0.id == rule.id } }).environmentObject(model)
                    }
                }
            }.frame(minHeight: 220, maxHeight: 390)
            if !valid { Text(model.text("Jede Regel benötigt Dateiendungen und einen sicheren relativen Ordner. Keine absoluten Pfade, leeren Abschnitte, „..“ oder „.airlink-partials“.", "Each rule needs extensions and a safe relative folder. Absolute paths, empty segments, '..', and '.airlink-partials' are not allowed.")).font(.caption).foregroundStyle(.red) }
            Text(model.text("Bis zu 100 Regeln. Dateiendungen: a–z, 0–9, _ oder -, höchstens 32 Zeichen pro Endung.", "Up to 100 rules. Extensions use a–z, 0–9, _ or -, with at most 32 characters each.")).font(.caption).foregroundStyle(.secondary)
            HStack {
                Text(model.text("Vorschläge bleiben editierbar. Speichern in den Einstellungen übernimmt die Änderungen.", "Presets remain editable. Save Settings to apply the changes.")).font(.caption).foregroundStyle(.secondary)
                Spacer()
                Button(model.text("Abbrechen", "Cancel")) { dismiss() }
                Button(model.text("Übernehmen", "Apply")) { rules = draft; dismiss() }.buttonStyle(.borderedProminent).disabled(!valid)
            }
        }.padding(26).frame(width: 760).onAppear { draft = rules }
    }
    private func move(_ id: String, by offset: Int) {
        guard let index = draft.firstIndex(where: { $0.id == id }), draft.indices.contains(index + offset) else { return }
        draft.swapAt(index, index + offset)
    }
}

private struct SortRuleRow: View {
    @EnvironmentObject var model: AppModel
    @Binding var rule: SortRule
    let first: Bool
    let last: Bool
    var up: () -> Void
    var down: () -> Void
    var remove: () -> Void
    @State private var extensionsText: String
    init(rule: Binding<SortRule>, first: Bool, last: Bool, up: @escaping () -> Void, down: @escaping () -> Void, remove: @escaping () -> Void) {
        _rule = rule; self.first = first; self.last = last; self.up = up; self.down = down; self.remove = remove
        _extensionsText = State(initialValue: rule.wrappedValue.extensions.joined(separator: ", "))
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                TextField(model.text("Dateiendungen, z. B. jpg, png", "Extensions, e.g. jpg, png"), text: $extensionsText)
                    .onChange(of: extensionsText) { _, value in rule.extensions = SortRule.parseExtensions(value) }
                Image(systemName: "arrow.right").foregroundStyle(.secondary)
                TextField(model.text("Relativer Ordner", "Relative folder"), text: $rule.folder)
            }.textFieldStyle(.roundedBorder)
            HStack {
                if !rule.isValid { Text(model.text("Regel vervollständigen", "Complete this rule")).font(.caption).foregroundStyle(.red) }
                Spacer()
                Button(action: up) { Image(systemName: "arrow.up") }.disabled(first).help(model.text("Früher prüfen", "Match earlier"))
                Button(action: down) { Image(systemName: "arrow.down") }.disabled(last).help(model.text("Später prüfen", "Match later"))
                Button(role: .destructive, action: remove) { Image(systemName: "trash") }.help(model.text("Regel entfernen", "Remove rule"))
            }
        }.padding(13).background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 10))
    }
}

struct DuplicateAttentionView: View {
    @EnvironmentObject var model: AppModel
    let transfer: Transfer
    let file: TransferFile
    @State private var working = false
    @State private var revalidating = false
    @State private var error: String?
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label(model.text("Inhaltsgleiche Datei gefunden", "Identical file contents found"), systemImage: "doc.on.doc").font(.callout.weight(.semibold))
            if working {
                HStack { ProgressView().controlSize(.small); Text(revalidating ? model.text("Vorhandene Datei wird erneut geprüft. Große Dateien können länger benötigen.", "Rechecking the existing file. Large files can take longer.") : model.text("Aktion wird verarbeitet …", "Processing action …")).font(.caption).foregroundStyle(.secondary) }
            }
            Text(file.path).font(.callout).textSelection(.enabled)
            ForEach(file.duplicateCandidates ?? []) { candidate in
                VStack(alignment: .leading, spacing: 7) {
                    Text(candidate.path).font(.system(.caption, design: .monospaced)).textSelection(.enabled)
                    HStack {
                        Text(TransferFormatting.bytes(candidate.size)).font(.caption).foregroundStyle(.secondary)
                        Spacer()
                        Button(model.text("Vorhandene Datei zeigen", "Show existing file")) {
                            working = true; revalidating = false; error = nil
                            Task { do { try await model.revealDuplicate(candidate.id) } catch { self.error = error.localizedDescription }; working = false }
                        }.disabled(working)
                        Button(model.text("Neue Datei überspringen", "Skip new file")) { resolve("skip", candidate: candidate.id) }.disabled(working || !transfer.canResolveDuplicates)
                    }
                }
            }
            HStack {
                Text(model.text("Die vorhandene Datei bleibt unverändert. Überspringen erhält keine neue Dateikopie.", "The existing file stays unchanged. Skipping does not create a new copy.")).font(.caption).foregroundStyle(.secondary)
                Spacer()
                Button(model.text("Weitere Kopie empfangen", "Receive another copy")) { resolve("keep") }.disabled(working || !transfer.canResolveDuplicates)
            }
            if !transfer.canResolveDuplicates { Text(model.text("Zum Entscheiden die Übertragung fortsetzen und die Prüfung abwarten.", "Resume the transfer and wait for checking before choosing.")).font(.caption).foregroundStyle(.secondary) }
            if let error { Text(error).font(.caption).foregroundStyle(.red).textSelection(.enabled) }
        }.padding(13).background(.orange.opacity(0.07), in: RoundedRectangle(cornerRadius: 10))
    }
    private func resolve(_ action: String, candidate: String? = nil) {
        working = true; revalidating = action == "skip"; error = nil
        Task {
            do { try await model.resolveDuplicate(transferID: transfer.id, fileID: file.id, action: action, candidateID: candidate) }
            catch { self.error = error.localizedDescription }
            working = false
        }
    }
}
