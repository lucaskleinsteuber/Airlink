// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "AirLinkMac",
    platforms: [.macOS(.v14)],
    products: [.executable(name: "AirLink", targets: ["AirLinkApp"])],
    dependencies: [.package(url: "https://github.com/sparkle-project/Sparkle", exact: "2.10.0")],
    targets: [
        .target(name: "AirLinkCore"),
        .executableTarget(
            name: "AirLinkApp",
            dependencies: ["AirLinkCore", .product(name: "Sparkle", package: "Sparkle")],
            linkerSettings: [.unsafeFlags(["-Xlinker", "-rpath", "-Xlinker", "@executable_path/../Frameworks"])]
        ),
        .testTarget(name: "AirLinkCoreTests", dependencies: ["AirLinkCore"])
    ],
    swiftLanguageModes: [.v5]
)
