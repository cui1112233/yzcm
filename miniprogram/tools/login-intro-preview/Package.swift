// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "LoginIntroPreview",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "LoginIntroPreview", targets: ["LoginIntroPreview"]),
        .executable(name: "LoginIntroPreviewChecks", targets: ["LoginIntroPreviewChecks"])
    ],
    targets: [
        .target(name: "LoginIntroPreviewCore"),
        .executableTarget(name: "LoginIntroPreview", dependencies: ["LoginIntroPreviewCore"]),
        .executableTarget(
            name: "LoginIntroPreviewChecks",
            dependencies: ["LoginIntroPreviewCore"],
            path: "Tests/LoginIntroPreviewChecks"
        )
    ]
)
