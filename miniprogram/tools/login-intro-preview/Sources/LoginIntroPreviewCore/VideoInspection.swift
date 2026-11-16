import AVFoundation
import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

public enum InspectionError: Error {
    case sourceMissing
    case videoTrackMissing
    case imageGenerationFailed
    case imageWriteFailed
}

public struct VideoFacts: Codable {
    public let duration: Double
    public let width: Int
    public let height: Int
    public let frameRate: Double
    public let hasAudio: Bool
}

public enum VideoInspection {
    public static func inspect(url: URL) throws -> VideoFacts {
        guard FileManager.default.fileExists(atPath: url.path) else {
            throw InspectionError.sourceMissing
        }

        let asset = AVURLAsset(url: url)
        guard let track = asset.tracks(withMediaType: .video).first else {
            throw InspectionError.videoTrackMissing
        }
        let transformed = track.naturalSize.applying(track.preferredTransform)
        return VideoFacts(
            duration: CMTimeGetSeconds(asset.duration),
            width: Int(abs(transformed.width).rounded()),
            height: Int(abs(transformed.height).rounded()),
            frameRate: Double(track.nominalFrameRate),
            hasAudio: !asset.tracks(withMediaType: .audio).isEmpty
        )
    }

    public static func extractFrames(
        source: URL,
        times: [Double],
        outputDirectory: URL
    ) throws -> [URL] {
        guard FileManager.default.fileExists(atPath: source.path) else {
            throw InspectionError.sourceMissing
        }
        try FileManager.default.createDirectory(
            at: outputDirectory,
            withIntermediateDirectories: true
        )

        let asset = AVURLAsset(url: source)
        let generator = AVAssetImageGenerator(asset: asset)
        generator.appliesPreferredTrackTransform = true
        generator.requestedTimeToleranceBefore = .zero
        generator.requestedTimeToleranceAfter = .zero

        return try times.map { seconds in
            let time = CMTime(seconds: seconds, preferredTimescale: 600)
            guard let image = try? generator.copyCGImage(at: time, actualTime: nil) else {
                throw InspectionError.imageGenerationFailed
            }
            let milliseconds = Int((seconds * 1_000).rounded())
            let output = outputDirectory.appendingPathComponent(
                String(format: "frame-%06d.png", milliseconds)
            )
            try writePNG(image, to: output)
            return output
        }
    }

    private static func writePNG(_ image: CGImage, to url: URL) throws {
        guard let destination = CGImageDestinationCreateWithURL(
            url as CFURL,
            UTType.png.identifier as CFString,
            1,
            nil
        ) else {
            throw InspectionError.imageWriteFailed
        }
        CGImageDestinationAddImage(destination, image, nil)
        guard CGImageDestinationFinalize(destination) else {
            throw InspectionError.imageWriteFailed
        }
    }
}
