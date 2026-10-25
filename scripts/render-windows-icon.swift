// Headless, code-authored Windows rendition of Assets/AirLink.svg.
// Run on macOS: swift scripts/render-windows-icon.swift [output.ico] [preview.png]
// No application windows, screen access, or generated artwork are involved.
import Foundation
import AppKit
import CoreGraphics

let output = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "windows/AirLink.Windows/Assets/AirLink.ico"
let preview = CommandLine.arguments.count > 2 ? CommandLine.arguments[2] : "build/windows-icon.png"
func render(_ size: Int) throws -> Data {
    let colorSpace = CGColorSpace(name: CGColorSpace.sRGB)!
    let context = CGContext(data: nil, width: size, height: size, bitsPerComponent: 8, bytesPerRow: 0,
                            space: colorSpace, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    context.scaleBy(x: CGFloat(size) / 1024, y: CGFloat(size) / 1024)
    context.translateBy(x: 0, y: 1024)
    context.scaleBy(x: 1, y: -1)
    context.saveGState()
    context.addPath(CGPath(roundedRect: CGRect(x: 48, y: 48, width: 928, height: 928), cornerWidth: 218, cornerHeight: 218, transform: nil))
    context.clip()
    let colors = [CGColor(srgbRed: 41/255, green: 148/255, blue: 251/255, alpha: 1), CGColor(srgbRed: 18/255, green: 87/255, blue: 214/255, alpha: 1)] as CFArray
    context.drawLinearGradient(CGGradient(colorsSpace: colorSpace, colors: colors, locations: [0, 1])!, start: CGPoint(x: 512, y: 48), end: CGPoint(x: 512, y: 976), options: [])
    context.restoreGState()
    context.translateBy(x: 512, y: 512)
    context.rotate(by: -35 * .pi / 180)
    context.translateBy(x: -512, y: -512)
    context.setStrokeColor(CGColor(gray: 1, alpha: 1))
    context.setLineWidth(76)
    context.setLineCap(.round)
    context.setLineJoin(.round)
    let path = CGMutablePath()
    path.move(to: CGPoint(x: 492, y: 390)); path.addLine(to: CGPoint(x: 364, y: 390))
    path.addCurve(to: CGPoint(x: 242, y: 512), control1: CGPoint(x: 296.62, y: 390), control2: CGPoint(x: 242, y: 444.62))
    path.addCurve(to: CGPoint(x: 364, y: 634), control1: CGPoint(x: 242, y: 579.38), control2: CGPoint(x: 296.62, y: 634))
    path.addLine(to: CGPoint(x: 444, y: 634))
    path.move(to: CGPoint(x: 532, y: 634)); path.addLine(to: CGPoint(x: 660, y: 634))
    path.addCurve(to: CGPoint(x: 782, y: 512), control1: CGPoint(x: 727.38, y: 634), control2: CGPoint(x: 782, y: 579.38))
    path.addCurve(to: CGPoint(x: 660, y: 390), control1: CGPoint(x: 782, y: 444.62), control2: CGPoint(x: 727.38, y: 390))
    path.addLine(to: CGPoint(x: 580, y: 390))
    path.move(to: CGPoint(x: 426, y: 512)); path.addLine(to: CGPoint(x: 598, y: 512))
    context.addPath(path); context.strokePath()
    let bitmap = NSBitmapImageRep(cgImage: context.makeImage()!)
    return bitmap.representation(using: .png, properties: [:])!
}
func appendLE<T: FixedWidthInteger>(_ value: T, to data: inout Data) {
    var encoded = value.littleEndian
    withUnsafeBytes(of: &encoded) { data.append(contentsOf: $0) }
}
let sizes = [16, 24, 32, 48, 64, 128, 256]
let images = try sizes.map(render)
var icon = Data()
appendLE(UInt16(0), to: &icon); appendLE(UInt16(1), to: &icon); appendLE(UInt16(sizes.count), to: &icon)
var offset = UInt32(6 + 16 * sizes.count)
for (size, png) in zip(sizes, images) {
    icon.append(size == 256 ? 0 : UInt8(size)); icon.append(size == 256 ? 0 : UInt8(size))
    icon.append(contentsOf: [0, 0]); appendLE(UInt16(1), to: &icon); appendLE(UInt16(32), to: &icon)
    appendLE(UInt32(png.count), to: &icon); appendLE(offset, to: &icon); offset += UInt32(png.count)
}
for png in images { icon.append(png) }
try FileManager.default.createDirectory(atPath: (output as NSString).deletingLastPathComponent, withIntermediateDirectories: true)
try icon.write(to: URL(fileURLWithPath: output), options: .atomic)
try FileManager.default.createDirectory(atPath: (preview as NSString).deletingLastPathComponent, withIntermediateDirectories: true)
try render(512).write(to: URL(fileURLWithPath: preview), options: .atomic)
print("Wrote \(output) (\(sizes.count) PNG icon sizes), preview \(preview)")
