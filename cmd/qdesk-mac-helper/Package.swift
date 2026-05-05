// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "qdesk-mac-helper",
    platforms: [.macOS(.v14)],
    targets: [
        .executableTarget(
            name: "Helper",
            path: "Sources/Helper"
        ),
        .testTarget(
            name: "HelperTests",
            dependencies: ["Helper"],
            path: "Tests/HelperTests"
        ),
    ]
)
