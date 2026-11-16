import AVFoundation
import CoreGraphics
import CoreVideo
import Foundation
import ImageIO

public enum RenderError: Error {
    case finalCardMissing
    case finalCardUnreadable
    case writerSetupFailed
    case writerStartFailed
    case pixelBufferUnavailable
    case contextCreationFailed
    case sourceFrameUnavailable
    case appendFailed
    case writerFailed(String)
}

public struct RenderReceipt: Codable {
    public let duration: Double
    public let width: Int
    public let height: Int
    public let frameRate: Double
    public let hasAudio: Bool
    public let codec: String

    public init(
        duration: Double,
        width: Int,
        height: Int,
        frameRate: Double,
        hasAudio: Bool,
        codec: String
    ) {
        self.duration = duration
        self.width = width
        self.height = height
        self.frameRate = frameRate
        self.hasAudio = hasAudio
        self.codec = codec
    }
}

public enum PreviewRenderer {
    public static func render(
        source: URL,
        finalCard: URL,
        timeline: IntroTimeline,
        output: URL,
        size: CGSize = CGSize(width: 540, height: 960),
        frameRate: Int = 24
    ) throws -> RenderReceipt {
        let facts = try VideoInspection.inspect(url: source)
        try timeline.validate(sourceDuration: facts.duration)
        guard FileManager.default.fileExists(atPath: finalCard.path) else {
            throw RenderError.finalCardMissing
        }
        guard let imageSource = CGImageSourceCreateWithURL(finalCard as CFURL, nil),
              let finalCardImage = CGImageSourceCreateImageAtIndex(imageSource, 0, nil) else {
            throw RenderError.finalCardUnreadable
        }

        let manager = FileManager.default
        try manager.createDirectory(at: output.deletingLastPathComponent(), withIntermediateDirectories: true)
        let previous = output.deletingPathExtension().appendingPathExtension("previous.mp4")
        if manager.fileExists(atPath: previous.path) {
            try manager.removeItem(at: previous)
        }
        if manager.fileExists(atPath: output.path) {
            try manager.moveItem(at: output, to: previous)
        }

        do {
            let receipt = try renderNewFile(
                source: source,
                finalCardImage: finalCardImage,
                timeline: timeline,
                output: output,
                width: Int(size.width),
                height: Int(size.height),
                frameRate: frameRate
            )
            if manager.fileExists(atPath: previous.path) {
                try manager.removeItem(at: previous)
            }
            return receipt
        } catch {
            if manager.fileExists(atPath: output.path) {
                try? manager.removeItem(at: output)
            }
            if manager.fileExists(atPath: previous.path) {
                try? manager.moveItem(at: previous, to: output)
            }
            throw error
        }
    }

    private static func renderNewFile(
        source: URL,
        finalCardImage: CGImage,
        timeline: IntroTimeline,
        output: URL,
        width: Int,
        height: Int,
        frameRate: Int
    ) throws -> RenderReceipt {
        let writer = try AVAssetWriter(outputURL: output, fileType: .mp4)
        let settings: [String: Any] = [
            AVVideoCodecKey: AVVideoCodecType.h264,
            AVVideoWidthKey: width,
            AVVideoHeightKey: height,
            AVVideoCompressionPropertiesKey: [
                AVVideoAverageBitRateKey: 2_400_000,
                AVVideoExpectedSourceFrameRateKey: frameRate,
                AVVideoMaxKeyFrameIntervalKey: frameRate * 2,
                AVVideoProfileLevelKey: AVVideoProfileLevelH264BaselineAutoLevel,
                AVVideoAllowFrameReorderingKey: false,
                AVVideoH264EntropyModeKey: AVVideoH264EntropyModeCAVLC
            ]
        ]
        let input = AVAssetWriterInput(mediaType: .video, outputSettings: settings)
        input.expectsMediaDataInRealTime = false
        guard writer.canAdd(input) else { throw RenderError.writerSetupFailed }
        writer.add(input)

        let adaptor = AVAssetWriterInputPixelBufferAdaptor(
            assetWriterInput: input,
            sourcePixelBufferAttributes: [
                kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA,
                kCVPixelBufferWidthKey as String: width,
                kCVPixelBufferHeightKey as String: height,
                kCVPixelBufferCGImageCompatibilityKey as String: true,
                kCVPixelBufferCGBitmapContextCompatibilityKey as String: true
            ]
        )
        guard writer.startWriting() else { throw RenderError.writerStartFailed }
        writer.startSession(atSourceTime: .zero)
        guard let pool = adaptor.pixelBufferPool else { throw RenderError.pixelBufferUnavailable }

        let generator = AVAssetImageGenerator(asset: AVURLAsset(url: source))
        generator.appliesPreferredTrackTransform = true
        generator.requestedTimeToleranceBefore = CMTime(value: 1, timescale: 48)
        generator.requestedTimeToleranceAfter = CMTime(value: 1, timescale: 48)

        let frameCount = Int(ceil(timeline.end * Double(frameRate)))
        for index in 0 ..< frameCount {
            while !input.isReadyForMoreMediaData {
                Thread.sleep(forTimeInterval: 0.002)
            }
            var buffer: CVPixelBuffer?
            guard CVPixelBufferPoolCreatePixelBuffer(nil, pool, &buffer) == kCVReturnSuccess,
                  let pixelBuffer = buffer else {
                throw RenderError.pixelBufferUnavailable
            }

            let seconds = Double(index) / Double(frameRate)
            var sourceImage: CGImage?
            if seconds < timeline.cardStart {
                let sourceTime = CMTime(seconds: seconds, preferredTimescale: 600)
                guard let image = try? generator.copyCGImage(at: sourceTime, actualTime: nil) else {
                    throw RenderError.sourceFrameUnavailable
                }
                sourceImage = image
            }

            try drawFrame(
                sourceImage: sourceImage,
                finalCardImage: finalCardImage,
                seconds: seconds,
                timeline: timeline,
                into: pixelBuffer,
                width: width,
                height: height
            )
            let presentationTime = CMTime(value: CMTimeValue(index), timescale: CMTimeScale(frameRate))
            guard adaptor.append(pixelBuffer, withPresentationTime: presentationTime) else {
                throw RenderError.appendFailed
            }
        }

        input.markAsFinished()
        let semaphore = DispatchSemaphore(value: 0)
        writer.finishWriting { semaphore.signal() }
        semaphore.wait()
        guard writer.status == .completed else {
            throw RenderError.writerFailed(writer.error?.localizedDescription ?? "unknown writer error")
        }

        return RenderReceipt(
            duration: Double(frameCount) / Double(frameRate),
            width: width,
            height: height,
            frameRate: Double(frameRate),
            hasAudio: false,
            codec: "H.264"
        )
    }

    private static func drawFrame(
        sourceImage: CGImage?,
        finalCardImage: CGImage,
        seconds: Double,
        timeline: IntroTimeline,
        into pixelBuffer: CVPixelBuffer,
        width: Int,
        height: Int
    ) throws {
        CVPixelBufferLockBaseAddress(pixelBuffer, [])
        defer { CVPixelBufferUnlockBaseAddress(pixelBuffer, []) }
        guard let baseAddress = CVPixelBufferGetBaseAddress(pixelBuffer),
              let context = CGContext(
                  data: baseAddress,
                  width: width,
                  height: height,
                  bitsPerComponent: 8,
                  bytesPerRow: CVPixelBufferGetBytesPerRow(pixelBuffer),
                  space: CGColorSpaceCreateDeviceRGB(),
                  bitmapInfo: CGImageAlphaInfo.premultipliedFirst.rawValue
                      | CGBitmapInfo.byteOrder32Little.rawValue
              ) else {
            throw RenderError.contextCreationFailed
        }

        context.translateBy(x: 0, y: CGFloat(height))
        context.scaleBy(x: 1, y: -1)
        context.interpolationQuality = .high
        let canvas = CGRect(x: 0, y: 0, width: width, height: height)

        if seconds < timeline.cardStart {
            guard let sourceImage else { throw RenderError.sourceFrameUnavailable }
            let amplitude: CGFloat
            if seconds >= timeline.transitionStart {
                let progress = min(1, (seconds - timeline.transitionStart) / max(0.001, timeline.cardStart - timeline.transitionStart))
                amplitude = CGFloat(3 * sin(progress * 8 * .pi))
            } else {
                amplitude = 0
            }
            drawTopLeftImage(
                sourceImage,
                in: canvas.offsetBy(dx: amplitude, dy: -amplitude * 0.55),
                context: context
            )
        } else {
            context.setFillColor(CGColor(gray: 1, alpha: 1))
            context.fill(canvas)
            drawFinalCard(
                finalCardImage,
                seconds: seconds,
                timeline: timeline,
                context: context,
                canvasWidth: CGFloat(width)
            )
            drawImpactStars(seconds: seconds, timeline: timeline, context: context)
        }

        let opacity = flashOpacity(at: seconds, timeline: timeline)
        if opacity > 0 {
            context.setFillColor(CGColor(gray: 1, alpha: opacity))
            context.fill(canvas)
        }
    }

    private static func drawFinalCard(
        _ image: CGImage,
        seconds: Double,
        timeline: IntroTimeline,
        context: CGContext,
        canvasWidth: CGFloat
    ) {
        let transform = cardTransform(at: seconds, timeline: timeline)
        let baseWidth = CGFloat(timeline.cardWidth)
        let baseHeight = baseWidth * CGFloat(image.height) / CGFloat(image.width)
        let width = baseWidth * transform.scaleX
        let height = baseHeight * transform.scaleY
        let rect = CGRect(
            x: (canvasWidth - width) / 2,
            y: CGFloat(timeline.cardY) + (baseHeight - height) / 2 + transform.offsetY,
            width: width,
            height: height
        )
        drawTopLeftImage(image, in: rect, context: context)
    }

    private static func drawTopLeftImage(_ image: CGImage, in rect: CGRect, context: CGContext) {
        context.saveGState()
        context.translateBy(x: 0, y: rect.minY * 2 + rect.height)
        context.scaleBy(x: 1, y: -1)
        context.draw(image, in: rect)
        context.restoreGState()
    }

    private static func drawImpactStars(
        seconds: Double,
        timeline: IntroTimeline,
        context: CGContext
    ) {
        guard seconds >= timeline.cardStart, seconds < timeline.settled else { return }
        let progress = CGFloat((seconds - timeline.cardStart) / (timeline.settled - timeline.cardStart))
        let alpha = max(0, 1 - progress)
        let stars: [(CGPoint, CGFloat, CGColor)] = [
            (CGPoint(x: 74, y: 330), 11, CGColor(red: 1, green: 0.57, blue: 0.70, alpha: 1)),
            (CGPoint(x: 466, y: 350), 9, CGColor(red: 0.43, green: 0.70, blue: 1, alpha: 1)),
            (CGPoint(x: 92, y: 570), 7, CGColor(red: 1, green: 0.80, blue: 0.30, alpha: 1)),
            (CGPoint(x: 454, y: 592), 8, CGColor(red: 1, green: 0.67, blue: 0.78, alpha: 1))
        ]
        for (center, radius, color) in stars {
            drawStar(center: center, radius: radius * (0.8 + 0.4 * alpha), color: color.copy(alpha: alpha) ?? color, context: context)
        }
    }

    private static func drawStar(
        center: CGPoint,
        radius: CGFloat,
        color: CGColor,
        context: CGContext
    ) {
        let path = CGMutablePath()
        for index in 0 ..< 8 {
            let angle = -CGFloat.pi / 2 + CGFloat(index) * CGFloat.pi / 4
            let pointRadius = index.isMultiple(of: 2) ? radius : radius * 0.28
            let point = CGPoint(
                x: center.x + cos(angle) * pointRadius,
                y: center.y + sin(angle) * pointRadius
            )
            if index == 0 { path.move(to: point) } else { path.addLine(to: point) }
        }
        path.closeSubpath()
        context.addPath(path)
        context.setFillColor(color)
        context.fillPath()
    }
}
