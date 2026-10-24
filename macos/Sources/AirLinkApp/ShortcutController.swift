import AppKit
import AirLinkCore
import Carbon
import SwiftUI

@MainActor final class ShortcutController {
    private var hotKey: EventHotKeyRef?
    private var eventHandler: EventHandlerRef?
    private var nextID: UInt32 = 1
    private var desired = Shortcut(enabled: false)
    private(set) var suspended = false
    var actual: Shortcut { hotKey == nil ? Shortcut(enabled: false, keyCode: desired.keyCode, modifiers: desired.modifiers) : desired }

    init() {
        var spec = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed))
        InstallEventHandler(GetApplicationEventTarget(), { _, event, _ in
            var identifier = EventHotKeyID()
            guard let event, GetEventParameter(event, EventParamName(kEventParamDirectObject), EventParamType(typeEventHotKeyID), nil, MemoryLayout<EventHotKeyID>.size, nil, &identifier) == noErr,
                  identifier.signature == OSType(0x414C4E4B) else { return OSStatus(eventNotHandledErr) }
            Task { @MainActor in AppModel.shared.chooseFiles() }
            return noErr
        }, 1, &spec, nil, &eventHandler)
    }
    func apply(_ shortcut: Shortcut) throws {
        guard shortcut.isValid else { throw NativeError.unavailable(AppModel.shared.text("Die Tastenkombination benötigt ⌘ oder ⌃.", "The shortcut requires Command or Control.")) }
        if suspended { desired = shortcut; return }
        if shortcut == actual { desired = shortcut; return }
        if !shortcut.enabled {
            if let hotKey { UnregisterEventHotKey(hotKey) }
            hotKey = nil; desired = shortcut
            return
        }
        guard eventHandler != nil else { throw NativeError.unavailable(AppModel.shared.text("Die systemweite Tastenkombination ist nicht verfügbar.", "The system shortcut handler is unavailable.")) }
        var candidate: EventHotKeyRef?
        let id = EventHotKeyID(signature: OSType(0x414C4E4B), id: nextID)
        nextID &+= 1
        let result = RegisterEventHotKey(shortcut.keyCode, shortcut.modifiers, id, GetApplicationEventTarget(), 0, &candidate)
        guard result == noErr, let candidate else {
            let message = hotKey != nil
                ? AppModel.shared.text("Diese Tastenkombination ist nicht verfügbar. Die bisherige Kombination bleibt erhalten.", "This shortcut is unavailable. The previous shortcut remains registered.")
                : AppModel.shared.text("Diese Tastenkombination ist nicht verfügbar. Aktuell ist keine globale Kombination registriert.", "This shortcut is unavailable. No global shortcut is currently registered.")
            throw NativeError.unavailable(message)
        }
        // Register first: a conflict never removes the previous working shortcut.
        if let hotKey { UnregisterEventHotKey(hotKey) }
        hotKey = candidate; desired = shortcut
    }
    func suspend() {
        guard !suspended else { return }
        suspended = true
        if let hotKey { UnregisterEventHotKey(hotKey) }
        hotKey = nil
    }
    func resume() throws {
        guard suspended else { return }
        suspended = false
        try apply(desired)
    }
    func stop() {
        if let hotKey { UnregisterEventHotKey(hotKey) }; hotKey = nil
        if let eventHandler { RemoveEventHandler(eventHandler) }; eventHandler = nil
    }
}

enum ShortcutLabel {
    static func string(_ shortcut: Shortcut) -> String {
        let special: [UInt32: String] = [36: "↩", 48: "⇥", 49: "␣", 51: "⌫", 53: "⎋", 76: "⌤", 115: "↖", 116: "⇞", 117: "⌦", 119: "↘", 121: "⇟", 123: "←", 124: "→", 125: "↓", 126: "↑", 122: "F1", 120: "F2", 99: "F3", 118: "F4", 96: "F5", 97: "F6", 98: "F7", 100: "F8", 101: "F9", 109: "F10", 103: "F11", 111: "F12"]
        var label = ""
        if shortcut.modifiers & 4096 != 0 { label += "⌃" }
        if shortcut.modifiers & 2048 != 0 { label += "⌥" }
        if shortcut.modifiers & 512 != 0 { label += "⇧" }
        if shortcut.modifiers & 256 != 0 { label += "⌘" }
        return label + (special[shortcut.keyCode] ?? keyName(shortcut.keyCode))
    }
    private static func keyName(_ code: UInt32) -> String {
        guard code <= UInt32(UInt16.max) else { return "Key \(code)" }
        let source = TISCopyCurrentKeyboardLayoutInputSource().takeRetainedValue()
        guard let raw = TISGetInputSourceProperty(source, kTISPropertyUnicodeKeyLayoutData) else { return "Key \(code)" }
        let data = unsafeBitCast(raw, to: CFData.self)
        guard let bytes = CFDataGetBytePtr(data) else { return "Key \(code)" }
        let layout = UnsafeRawPointer(bytes).assumingMemoryBound(to: UCKeyboardLayout.self)
        var dead: UInt32 = 0, length = 0
        var characters = [UniChar](repeating: 0, count: 8)
        let result = UCKeyTranslate(layout, UInt16(code), UInt16(kUCKeyActionDisplay), 0, UInt32(LMGetKbdType()), OptionBits(kUCKeyTranslateNoDeadKeysBit), &dead, 8, &length, &characters)
        guard result == noErr, length > 0 else { return "Key \(code)" }
        return String(utf16CodeUnits: characters, count: length).uppercased()
    }
}

struct ShortcutRecorder: NSViewRepresentable {
    var captured: (Shortcut) -> Void
    var cancelled: () -> Void
    var invalid: () -> Void
    func makeNSView(context: Context) -> ShortcutCaptureView {
        let view = ShortcutCaptureView(); view.captured = captured; view.cancelled = cancelled; view.invalid = invalid
        DispatchQueue.main.async { view.window?.makeFirstResponder(view) }
        return view
    }
    func updateNSView(_ view: ShortcutCaptureView, context: Context) { view.captured = captured; view.cancelled = cancelled; view.invalid = invalid }
}

final class ShortcutCaptureView: NSView {
    var captured: ((Shortcut) -> Void)?
    var cancelled: (() -> Void)?
    var invalid: (() -> Void)?
    override var acceptsFirstResponder: Bool { true }
    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        guard window?.firstResponder === self else { return false }
        keyDown(with: event); return true
    }
    override func resignFirstResponder() -> Bool {
        let resigned = super.resignFirstResponder()
        if resigned { DispatchQueue.main.async { [weak self] in self?.cancelled?() } }
        return resigned
    }
    override func keyDown(with event: NSEvent) {
        guard window?.firstResponder === self else { return }
        if event.keyCode == 53 && event.modifierFlags.intersection([.command, .control, .option, .shift]).isEmpty { cancelled?(); return }
        var bits: UInt32 = 0
        if event.modifierFlags.contains(.command) { bits |= 256 }
        if event.modifierFlags.contains(.control) { bits |= 4096 }
        if event.modifierFlags.contains(.shift) { bits |= 512 }
        if event.modifierFlags.contains(.option) { bits |= 2048 }
        let shortcut = Shortcut(keyCode: UInt32(event.keyCode), modifiers: bits)
        if shortcut.isValid { captured?(shortcut) } else { invalid?() }
    }
}
