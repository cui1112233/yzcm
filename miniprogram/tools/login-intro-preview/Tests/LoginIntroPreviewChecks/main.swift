import CoreGraphics
import Foundation
import LoginIntroPreviewCore

private func require(_ condition: @autoclosure () -> Bool, _ message: String) {
    guard condition() else {
        FileHandle.standardError.write(Data("CHECK FAILED: \(message)\n".utf8))
        exit(1)
    }
}

private func checkTimeline() throws {
    let valid = IntroTimeline(
        crash: 2.1,
        transitionStart: 4.30,
        flashPeak: 4.42,
        cardStart: 4.40,
        settled: 4.76,
        end: 5.50,
        cardWidth: 520,
        cardY: 245
    )
    try valid.validate(sourceDuration: 5.042)

    var invalid = valid
    invalid.cardStart = 4.1
    do {
        try invalid.validate(sourceDuration: 5.042)
        require(false, "unordered timeline must fail validation")
    } catch TimelineError.invalidOrder {
        // Expected.
    }

    require(flashOpacity(at: 4.20, timeline: valid) == 0, "flash must be absent before transition")
    require(flashOpacity(at: valid.flashPeak, timeline: valid) >= 0.95, "flash peak must hide the cut")
    require(flashOpacity(at: 4.64, timeline: valid) == 0, "flash must clear quickly")

    let impact = cardTransform(at: valid.cardStart, timeline: valid)
    require(impact.scaleY < 1, "card must squash at impact")
    let final = cardTransform(at: valid.settled, timeline: valid)
    require(abs(final.scaleX - 1) < 0.0001, "card width must settle")
    require(abs(final.scaleY - 1) < 0.0001, "card height must settle")
    require(abs(final.offsetY) < 0.0001, "card offset must settle")
}

private func checkInspectionErrors() {
    do {
        _ = try VideoInspection.inspect(url: URL(fileURLWithPath: "/missing/source.mp4"))
        require(false, "missing source must fail inspection")
    } catch InspectionError.sourceMissing {
        // Expected.
    } catch {
        require(false, "missing source returned the wrong error")
    }
}

try checkTimeline()
checkInspectionErrors()
print("PASS TimelineChecks")
print("PASS InspectionChecks")
