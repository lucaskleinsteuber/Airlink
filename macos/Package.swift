// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "AirLinkMac",
    platforms: [.macOS(.v14)],
    products: [.executable(name: "AirLink", targets: ["AirLinkApp"])],
    targets: [
        .target(name: "AirLinkCore"),
        .executableTarget(name: "AirLinkApp", dependencies: ["AirLinkCore"]),
        .testTarget(name: "AirLinkCoreTests", dependencies: ["AirLinkCore"])
    ],
    swiftLanguageModes: [.v5]
)
