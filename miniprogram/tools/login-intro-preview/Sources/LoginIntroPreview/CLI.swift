import Foundation
import LoginIntroPreviewCore

private enum CLIError: Error, CustomStringConvertible {
    case usage(String)

    var description: String {
        switch self {
        case let .usage(message): message
        }
    }
}

private func option(_ name: String, in arguments: [String]) throws -> String {
    guard let index = arguments.firstIndex(of: name), arguments.indices.contains(index + 1) else {
        throw CLIError.usage("Missing \(name)")
    }
    return arguments[index + 1]
}

private func run() throws {
    let arguments = Array(CommandLine.arguments.dropFirst())
    guard let command = arguments.first else {
        throw CLIError.usage("Use: LoginIntroPreview inspect --source <mp4> --output <directory>")
    }

    switch command {
    case "inspect":
        let source = URL(fileURLWithPath: try option("--source", in: arguments))
        let output = URL(fileURLWithPath: try option("--output", in: arguments), isDirectory: true)
        let facts = try VideoInspection.inspect(url: source)
        var times: [Double] = []
        var current = 0.0
        while current < facts.duration {
            times.append(current)
            current += 0.2
        }
        times.append(max(0, facts.duration - 0.001))
        _ = try VideoInspection.extractFrames(source: source, times: times, outputDirectory: output)
        let data = try JSONEncoder.pretty.encode(facts)
        try data.write(to: output.appendingPathComponent("inspection.json"), options: .atomic)
        print(String(decoding: data, as: UTF8.self))
    case "extract":
        let source = URL(fileURLWithPath: try option("--source", in: arguments))
        let output = URL(fileURLWithPath: try option("--output", in: arguments), isDirectory: true)
        guard let from = Double(try option("--from", in: arguments)),
              let to = Double(try option("--to", in: arguments)),
              let step = Double(try option("--step", in: arguments)),
              from >= 0, to >= from, step > 0 else {
            throw CLIError.usage("Invalid --from, --to, or --step value")
        }
        var times: [Double] = []
        var current = from
        while current <= to + 0.000_001 {
            times.append(current)
            current += step
        }
        let outputs = try VideoInspection.extractFrames(
            source: source,
            times: times,
            outputDirectory: output
        )
        print("Extracted \(outputs.count) frames")
    case "render":
        let source = URL(fileURLWithPath: try option("--source", in: arguments))
        let finalCard = URL(fileURLWithPath: try option("--final-card", in: arguments))
        let timelineURL = URL(fileURLWithPath: try option("--timeline", in: arguments))
        let output = URL(fileURLWithPath: try option("--output", in: arguments))
        let timeline = try JSONDecoder().decode(
            IntroTimeline.self,
            from: Data(contentsOf: timelineURL)
        )
        let receipt = try PreviewRenderer.render(
            source: source,
            finalCard: finalCard,
            timeline: timeline,
            output: output
        )
        print(String(decoding: try JSONEncoder.pretty.encode(receipt), as: UTF8.self))
    case "verify":
        let video = URL(fileURLWithPath: try option("--video", in: arguments))
        let timelineURL = URL(fileURLWithPath: try option("--timeline", in: arguments))
        let frames = URL(fileURLWithPath: try option("--frames", in: arguments), isDirectory: true)
        let receiptURL = URL(fileURLWithPath: try option("--receipt", in: arguments))
        let timeline = try JSONDecoder().decode(
            IntroTimeline.self,
            from: Data(contentsOf: timelineURL)
        )
        let facts = try VideoInspection.inspect(url: video)
        if let existing = try? FileManager.default.contentsOfDirectory(
            at: frames,
            includingPropertiesForKeys: nil
        ) {
            for item in existing where item.lastPathComponent.hasPrefix("frame-")
                && item.pathExtension.lowercased() == "png" {
                try FileManager.default.removeItem(at: item)
            }
        }
        let reviewTimes = [
            max(0, timeline.transitionStart - 0.08),
            timeline.flashPeak,
            timeline.cardStart + 0.08,
            timeline.settled,
            max(0, timeline.end - 0.08)
        ]
        _ = try VideoInspection.extractFrames(
            source: video,
            times: reviewTimes,
            outputDirectory: frames
        )
        let receipt = RenderReceipt(
            duration: facts.duration,
            width: facts.width,
            height: facts.height,
            frameRate: facts.frameRate,
            hasAudio: facts.hasAudio,
            codec: "H.264"
        )
        let data = try JSONEncoder.pretty.encode(receipt)
        try data.write(to: receiptURL, options: .atomic)
        print(String(decoding: data, as: UTF8.self))
    default:
        throw CLIError.usage("Unknown command: \(command)")
    }
}

private extension JSONEncoder {
    static var pretty: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        return encoder
    }
}

do {
    try run()
} catch {
    FileHandle.standardError.write(Data("ERROR: \(error)\n".utf8))
    exit(1)
}
