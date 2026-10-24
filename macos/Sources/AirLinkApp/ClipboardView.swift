import AppKit
import AirLinkCore
import SwiftUI

struct ClipboardView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var target = ""
    @State private var draft = ""
    @State private var sending = false
    @State private var error: String?
    private var devices: [Device] { model.status?.devices.filter { $0.paired && $0.canClipboard } ?? [] }
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack { Text(model.text("Zwischenablage", "Clipboard")).font(.title2.weight(.semibold)); Spacer(); Button(model.text("Fertig", "Done")) { dismiss() } }
            Text(model.text("Text wird nur nach deiner Aktion gelesen, gesendet oder kopiert. Nachrichten bleiben bis zu einer Stunde in dieser Sitzung erhalten.", "Text is read, sent, or copied only after your action. Messages remain in this session for up to one hour.")).foregroundStyle(.secondary).font(.callout)
            if devices.isEmpty {
                Label(model.text("Zwischenablage in den Berechtigungen eines gekoppelten Geräts freigeben.", "Enable clipboard sharing in a paired device’s permissions."), systemImage: "lock").foregroundStyle(.secondary)
            } else {
                Picker(model.text("Empfänger", "Recipient"), selection: $target) { ForEach(devices) { Text($0.name).tag($0.id) } }
            }
            TextEditor(text: $draft).font(.body).frame(height: 125).padding(6).background(Color(nsColor: .textBackgroundColor), in: RoundedRectangle(cornerRadius: 9)).overlay(RoundedRectangle(cornerRadius: 9).stroke(.secondary.opacity(0.2)))
            HStack {
                Button(model.text("Einfügen", "Paste")) {
                    if let pasted = NSPasteboard.general.string(forType: .string) { draft = pasted }
                }
                Text("\(draft.utf8.count) / \(ClipboardText.maximumUTF8Bytes) UTF-8 bytes").font(.caption).foregroundStyle(draft.utf8.count > ClipboardText.maximumUTF8Bytes ? .red : .secondary)
                Spacer()
                Button(sending ? model.text("Wird gesendet …", "Sending …") : model.text("Senden", "Send")) { send() }.buttonStyle(.borderedProminent)
                    .disabled(!ClipboardText.isValid(draft) || !devices.contains(where: { $0.id == target }) || sending || model.engineError != nil)
            }
            if let error { Text(error).font(.callout).foregroundStyle(.red).textSelection(.enabled) }
            Divider()
            if model.clipboardMessages.isEmpty {
                Text(model.text("Noch keine Textnachrichten", "No text messages yet")).foregroundStyle(.secondary).padding(.vertical, 12)
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 12) {
                        ForEach(model.clipboardMessages.reversed()) { message in
                            VStack(alignment: .leading, spacing: 9) {
                                HStack {
                                    Label((message.direction == "receive" ? model.text("Von ", "From ") : model.text("An ", "To ")) + message.deviceName, systemImage: message.direction == "receive" ? "arrow.down.left" : "arrow.up.right").font(.caption).foregroundStyle(.secondary)
                                    Spacer()
                                    Button { model.command("clipboard.dismiss", params: ["id": message.id]) } label: { Image(systemName: "xmark") }.buttonStyle(.borderless).help(model.text("Verwerfen", "Dismiss"))
                                }
                                Text(message.text).textSelection(.enabled).lineLimit(5).frame(maxWidth: .infinity, alignment: .leading)
                                Button(model.text("Kopieren", "Copy")) {
                                    NSPasteboard.general.clearContents(); NSPasteboard.general.setString(message.text, forType: .string)
                                }
                            }.padding(14).frame(maxWidth: .infinity, alignment: .leading).background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 10))
                        }
                    }
                }.frame(maxHeight: 280)
            }
        }.padding(28).frame(width: 620)
        .onAppear { target = devices.contains(where: { $0.id == model.selectedDeviceID }) ? model.selectedDeviceID ?? "" : devices.first?.id ?? "" }
    }
    private func send() {
        sending = true; error = nil
        let text = draft, deviceID = target
        Task {
            do { try await model.sendClipboard(text, to: deviceID); if draft == text { draft = "" } }
            catch { self.error = error.localizedDescription }
            sending = false
        }
    }
}
