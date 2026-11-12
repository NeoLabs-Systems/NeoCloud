// swift-tools-version: 5.9

import PackageDescription

let package = Package(
    name: "neocloud_background",
    platforms: [
        .iOS("13.0")
    ],
    products: [
        // Flutter requires SwiftPM product names without underscores.
        .library(name: "neocloud-background", targets: ["neocloud_background"])
    ],
    dependencies: [],
    targets: [
        .target(
            name: "neocloud_background",
            dependencies: [],
            resources: [
                .process("PrivacyInfo.xcprivacy")
            ]
        )
    ]
)
