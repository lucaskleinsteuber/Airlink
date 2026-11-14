import AppKit
import AirLinkCore
import SwiftUI

/// `AirLink --demo --snapshot <folder>` renders the main states as PNG files
/// (light and dark) from sample data, then quits. Used to review the design.
@MainActor enum Snapshotter {
    static func run(to directory: URL) {
        let model = AppModel.shared
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        for dark in [false, true] {
            let suffix = dark ? "dark" : "light"
            model.loadDemo(.welcome); model.sheet = nil
            render(HomeView(), "1-welcome-\(suffix)", CGSize(width: 700, height: 680), dark, directory)
            model.loadDemo(.empty); model.sheet = nil
            render(HomeView(), "2-empty-\(suffix)", CGSize(width: 700, height: 560), dark, directory)
            model.loadDemo(.home); model.sheet = nil
            render(HomeView(), "3-home-\(suffix)", CGSize(width: 700, height: 760), dark, directory)
            model.loadDemo(.pairing); model.sheet = nil
            render(PairingSheet(sessionID: "demo-pair"), "4-pairing-\(suffix)", nil, dark, directory)
            model.loadDemo(.home); model.sheet = nil
            render(AddDeviceSheet(byAddress: true), "5-add-\(suffix)", nil, dark, directory)
            render(SendChooser(pending: PendingSend(urls: [URL(fileURLWithPath: "/tmp/Urlaub.mov"), URL(fileURLWithPath: "/tmp/Notizen.pdf")])), "6-send-\(suffix)", nil, dark, directory)
            render(HelpView(), "7-help-\(suffix)", nil, dark, directory)
            render(SettingsView(), "8-settings-\(suffix)", nil, dark, directory)
        }
        NSApp.terminate(nil)
    }

    private static func render<V: View>(_ view: V, _ name: String, _ size: CGSize?, _ dark: Bool, _ directory: URL) {
        let content = view.environmentObject(AppModel.shared).background(Color(nsColor: .windowBackgroundColor))
        let host = NSHostingView(rootView: content)
        let fitted = size ?? host.fittingSize
        let window = NSWindow(contentRect: NSRect(origin: NSPoint(x: -20000, y: -20000), size: fitted), styleMask: [.titled], backing: .buffered, defer: false)
        window.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
        window.contentView = host
        window.orderFrontRegardless()
        RunLoop.main.run(until: Date().addingTimeInterval(0.8))
        if let rep = host.bitmapImageRepForCachingDisplay(in: host.bounds) {
            host.cacheDisplay(in: host.bounds, to: rep)
            try? rep.representation(using: .png, properties: [:])?.write(to: directory.appendingPathComponent(name + ".png"))
        }
        window.orderOut(nil)
    }
}
