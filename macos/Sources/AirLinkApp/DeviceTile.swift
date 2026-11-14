import AppKit
import AirLinkCore
import SwiftUI
import UniformTypeIdentifiers

enum DeviceSymbol {
    static func name(platform: String?, form: String?) -> String {
        switch (platform, form) {
        case ("macos", "laptop"): return available("macbook") ?? "laptopcomputer"
        case ("macos", _): return "desktopcomputer"
        case (_, "laptop"): return "laptopcomputer"
        case ("windows", _), ("linux", _): return available("pc") ?? "desktopcomputer"
        default: return "desktopcomputer"
        }
    }
    static func name(_ device: Device) -> String { name(platform: device.platform, form: device.form) }
    private static func available(_ symbol: String) -> String? {
        NSImage(systemSymbolName: symbol, accessibilityDescription: nil) == nil ? nil : symbol
    }
}

/// Liquid Glass on macOS 26 and later, a material bubble before.
struct GlassCircle: ViewModifier {
    var tint: Color?
    var interactive = true
    func body(content: Content) -> some View {
        if #available(macOS 26.0, *) {
            content.glassEffect(glass, in: Circle())
        } else {
            content
                .background(Circle().fill(.regularMaterial))
                .background(Circle().fill(tint ?? .clear))
                .shadow(color: .black.opacity(0.10), radius: 8, y: 3)
        }
    }
    @available(macOS 26.0, *)
    private var glass: Glass {
        var value = Glass.regular
        if let tint { value = value.tint(tint) }
        return interactive ? value.interactive() : value
    }
}

struct GlassPanel: ViewModifier {
    var radius: CGFloat = 16
    func body(content: Content) -> some View {
        if #available(macOS 26.0, *) {
            content.glassEffect(.regular, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
        } else {
            content.background(.regularMaterial, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
        }
    }
}

extension View {
    func glassCircle(tint: Color? = nil, interactive: Bool = true) -> some View { modifier(GlassCircle(tint: tint, interactive: interactive)) }
    func glassPanel(radius: CGFloat = 16) -> some View { modifier(GlassPanel(radius: radius)) }
}

/// A computer in the AirDrop-style grid: drop files on it or click it.
struct DeviceTile: View {
    enum Kind { case paired(Device), nearby(Device), add, searching }
    @EnvironmentObject var model: AppModel
    let kind: Kind
    var size: CGFloat = 96
    var onPick: ((Device) -> Void)?
    @State private var hovering = false
    @State private var targeted = false
    @State private var confirmRemove = false

    private var device: Device? {
        switch kind {
        case .paired(let d), .nearby(let d): return d
        default: return nil
        }
    }
    private var activity: DeviceActivity? { device.map { model.activity(for: $0) } }
    private var reachable: Bool {
        if case .paired(let d) = kind { return d.online }
        return true
    }

    var body: some View {
        VStack(spacing: 10) {
            ZStack {
                surface
                Image(systemName: symbol)
                    .font(.system(size: size * 0.40, weight: .regular))
                    .symbolRenderingMode(.hierarchical)
                    .foregroundStyle(symbolStyle)
                ProgressRing(progress: activity?.progress, busy: activity?.busy == true, diameter: size + 12)
            }
            .frame(width: size + 14, height: size + 14)
            .scaleEffect(targeted ? 1.1 : hovering ? 1.04 : 1)
            .animation(.spring(response: 0.32, dampingFraction: 0.68), value: targeted)
            .animation(.easeOut(duration: 0.15), value: hovering)
            VStack(spacing: 3) {
                Text(title).font(.callout.weight(.medium)).lineLimit(2).multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                Text(subtitle).font(.caption).foregroundStyle(activity?.busy == true ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary))
                    .lineLimit(1).monospacedDigit()
            }
        }
        .frame(width: size + 44)
        .opacity(reachable ? 1 : 0.62)
        .contentShape(Rectangle())
        .onTapGesture(perform: primary)
        .onHover { hovering = $0 }
        .onDrop(of: [UTType.fileURL], isTargeted: dropBinding) { providers in
            guard case .paired(let d) = kind else { return false }
            return model.receiveDrop(providers, to: d)
        }
        .contextMenu { menu }
        .help(help)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(title + ", " + subtitle)
        .accessibilityAddTraits(.isButton)
        .accessibilityAction(.default, primary)
        .confirmationDialog(model.text("\(title) entfernen?", "Remove \(title)?"), isPresented: $confirmRemove) {
            if let device { Button(model.text("Entfernen", "Remove"), role: .destructive) { model.remove(device) } }
        } message: {
            Text(model.text("Ihr könnt euch danach keine Dateien mehr schicken, bis ihr euch neu verbindet. Bereits gesendete Dateien bleiben erhalten.", "You can no longer send each other files until you connect again. Files already sent stay where they are."))
        }
    }

    @ViewBuilder private var surface: some View {
        switch kind {
        case .paired(let d):
            Circle().fill(.clear).frame(width: size, height: size)
                .glassCircle(tint: targeted ? Color.accentColor.opacity(0.35) : d.online ? Color.accentColor.opacity(0.10) : nil)
                .overlay(Circle().strokeBorder(Color.accentColor, lineWidth: targeted ? 3 : 0))
        case .nearby, .add, .searching:
            Circle().fill(Color.primary.opacity(0.03)).frame(width: size, height: size)
                .overlay(Circle().strokeBorder(style: StrokeStyle(lineWidth: 1.5, dash: [5, 4])).foregroundStyle(hovering ? Color.accentColor : Color.secondary.opacity(0.45)))
        }
    }
    private var symbol: String {
        switch kind {
        case .paired(let d), .nearby(let d): return DeviceSymbol.name(d)
        case .add: return "plus"
        case .searching: return "antenna.radiowaves.left.and.right"
        }
    }
    private var symbolStyle: AnyShapeStyle {
        switch kind {
        case .paired(let d): return d.online ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary)
        case .nearby: return hovering ? AnyShapeStyle(.tint) : AnyShapeStyle(.primary.opacity(0.75))
        default: return hovering ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary)
        }
    }
    private var title: String {
        switch kind {
        case .paired(let d), .nearby(let d): return d.name
        case .add: return model.text("Hinzufügen", "Add")
        case .searching: return model.text("Suche …", "Searching …")
        }
    }
    private var subtitle: String {
        switch kind {
        case .paired, .nearby: return activity?.label ?? ""
        case .add: return model.text("Anderer Computer", "Another computer")
        case .searching: return model.text("Im Netzwerk", "On your network")
        }
    }
    private var help: String {
        switch kind {
        case .paired(let d): return model.text("Klicken, um Dateien an \(d.name) zu senden – oder Dateien hierher ziehen.", "Click to send files to \(d.name), or drag files here.")
        case .nearby(let d): return model.text("Klicken, um dich mit \(d.name) zu verbinden. Ihr vergleicht dann einen Code.", "Click to connect to \(d.name). You then compare a code.")
        case .add: return model.text("Einen anderen Computer hinzufügen", "Add another computer")
        case .searching: return model.text("AirLink sucht im Netzwerk nach anderen Computern mit AirLink.", "AirLink is looking for other computers with AirLink on your network.")
        }
    }
    private var dropBinding: Binding<Bool> {
        Binding(get: { targeted }, set: { value in if case .paired = kind { targeted = value } })
    }
    private func primary() {
        switch kind {
        case .paired(let d): if let onPick { onPick(d) } else { model.chooseFiles(for: d) }
        case .nearby(let d): model.connect(d)
        case .add, .searching: model.sheet = .addDevice(byAddress: false)
        }
    }
    @ViewBuilder private var menu: some View {
        switch kind {
        case .paired(let d):
            Button(model.text("Dateien senden …", "Send Files …")) { model.chooseFiles(for: d) }
            Button(model.text("Zwischenablage senden", "Send Clipboard")) { model.sendClipboard(to: d) }.disabled(!d.canClipboard)
            Divider()
            Button(model.text("Einstellungen für \(d.name) …", "Settings for \(d.name) …")) { model.sheet = .device(d.id) }
            Button(model.text("Entfernen …", "Remove …"), role: .destructive) { confirmRemove = true }
        case .nearby(let d):
            Button(model.text("Verbinden", "Connect")) { model.connect(d) }
        case .add, .searching:
            Button(model.text("Computer hinzufügen …", "Add Computer …")) { model.sheet = .addDevice(byAddress: false) }
        }
    }
}

/// Progress around a tile; spins while the amount is not known yet.
struct ProgressRing: View {
    var progress: Double?
    var busy: Bool
    var diameter: CGFloat
    @State private var spin = false
    var body: some View {
        ZStack {
            if let progress {
                Circle().stroke(Color.accentColor.opacity(0.16), lineWidth: 4)
                Circle().trim(from: 0, to: max(0.02, progress))
                    .stroke(Color.accentColor, style: StrokeStyle(lineWidth: 4, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                    .animation(.easeOut(duration: 0.3), value: progress)
            } else if busy {
                Circle().trim(from: 0, to: 0.22)
                    .stroke(Color.accentColor, style: StrokeStyle(lineWidth: 4, lineCap: .round))
                    .rotationEffect(.degrees(spin ? 270 : -90))
                    .onAppear { withAnimation(.linear(duration: 1).repeatForever(autoreverses: false)) { spin = true } }
            }
        }
        .frame(width: diameter, height: diameter)
        .allowsHitTesting(false)
    }
}
