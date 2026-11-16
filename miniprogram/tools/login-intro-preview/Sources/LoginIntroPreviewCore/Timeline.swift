import CoreGraphics
import Foundation

public enum TimelineError: Error {
    case invalidOrder
    case invalidLayout
}

public struct IntroTimeline: Codable {
    public var crash: Double
    public var transitionStart: Double
    public var flashPeak: Double
    public var cardStart: Double
    public var settled: Double
    public var end: Double
    public var cardWidth: Double
    public var cardY: Double

    public init(
        crash: Double,
        transitionStart: Double,
        flashPeak: Double,
        cardStart: Double,
        settled: Double,
        end: Double,
        cardWidth: Double,
        cardY: Double
    ) {
        self.crash = crash
        self.transitionStart = transitionStart
        self.flashPeak = flashPeak
        self.cardStart = cardStart
        self.settled = settled
        self.end = end
        self.cardWidth = cardWidth
        self.cardY = cardY
    }

    public func validate(sourceDuration: Double) throws {
        let ordered = 0 < crash
            && crash < transitionStart
            && transitionStart < flashPeak
            && transitionStart < cardStart
            && abs(cardStart - flashPeak) <= 0.08
            && max(flashPeak, cardStart) < sourceDuration
            && cardStart < settled
            && settled < end
        guard ordered else { throw TimelineError.invalidOrder }
        guard cardWidth > 0, cardY >= 0 else { throw TimelineError.invalidLayout }
    }
}

public struct CardTransform {
    public let scaleX: CGFloat
    public let scaleY: CGFloat
    public let offsetY: CGFloat
}

public func flashOpacity(at seconds: Double, timeline: IntroTimeline) -> CGFloat {
    guard seconds >= timeline.transitionStart else { return 0 }
    if seconds <= timeline.flashPeak {
        let span = timeline.flashPeak - timeline.transitionStart
        let progress = max(0, min(1, (seconds - timeline.transitionStart) / span))
        return CGFloat(progress * progress * (3 - 2 * progress))
    }
    let fadeEnd = timeline.flashPeak + 0.18
    guard seconds < fadeEnd else { return 0 }
    let progress = max(0, min(1, (seconds - timeline.flashPeak) / 0.18))
    let eased = progress * progress * (3 - 2 * progress)
    return CGFloat(1 - eased)
}

public func cardTransform(at seconds: Double, timeline: IntroTimeline) -> CardTransform {
    guard seconds >= timeline.cardStart, seconds < timeline.settled else {
        return CardTransform(scaleX: 1, scaleY: 1, offsetY: 0)
    }
    let elapsed = seconds - timeline.cardStart
    let decay = exp(-7.2 * elapsed)
    let oscillation = cos(18 * elapsed)
    return CardTransform(
        scaleX: 1 + CGFloat(0.02 * decay * oscillation),
        scaleY: 1 - CGFloat(0.04 * decay * oscillation),
        offsetY: CGFloat(8 * decay * oscillation)
    )
}
