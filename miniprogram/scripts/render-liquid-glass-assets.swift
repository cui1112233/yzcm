import AppKit

extension NSColor {
  convenience init(hex: Int, alpha: CGFloat = 1) {
    self.init(
      calibratedRed: CGFloat((hex >> 16) & 255) / 255,
      green: CGFloat((hex >> 8) & 255) / 255,
      blue: CGFloat(hex & 255) / 255,
      alpha: alpha
    )
  }
}

struct Palette {
  let top: NSColor
  let middle: NSColor
  let bottom: NSColor
  let ribbon: NSColor
  let shadow: NSColor
  let highlight: NSColor
}

let wallpaperSize = NSSize(width: 390, height: 844)

func png(_ size: NSSize, output: URL, draw: () -> Void) throws {
  guard let representation = NSBitmapImageRep(
    bitmapDataPlanes: nil,
    pixelsWide: Int(size.width),
    pixelsHigh: Int(size.height),
    bitsPerSample: 8,
    samplesPerPixel: 4,
    hasAlpha: true,
    isPlanar: false,
    colorSpaceName: .deviceRGB,
    bitmapFormat: [],
    bytesPerRow: 0,
    bitsPerPixel: 0
  ), let context = NSGraphicsContext(bitmapImageRep: representation) else {
    throw NSError(domain: "LiquidGlassAssets", code: 1)
  }

  NSGraphicsContext.saveGraphicsState()
  NSGraphicsContext.current = context
  context.cgContext.clear(CGRect(origin: .zero, size: size))
  draw()
  context.flushGraphics()
  NSGraphicsContext.restoreGraphicsState()

  guard let data = representation.representation(using: .png, properties: [:]) else {
    throw NSError(domain: "LiquidGlassAssets", code: 2)
  }
  try data.write(to: output)
}

func renderApprovedWallpaper(_ image: NSImage, darkening: CGFloat, to output: URL) throws {
  try png(wallpaperSize, output: output) {
    let sourceSize = image.size
    let scale = max(wallpaperSize.width / sourceSize.width, wallpaperSize.height / sourceSize.height)
    let drawSize = NSSize(width: sourceSize.width * scale, height: sourceSize.height * scale)
    let drawRect = NSRect(
      x: (wallpaperSize.width - drawSize.width) / 2,
      y: (wallpaperSize.height - drawSize.height) / 2,
      width: drawSize.width,
      height: drawSize.height
    )
    NSGraphicsContext.current?.imageInterpolation = .high
    image.draw(in: drawRect, from: .zero, operation: .sourceOver, fraction: 1)
    if darkening > 0 {
      NSColor.black.withAlphaComponent(darkening).setFill()
      NSBezierPath(rect: NSRect(origin: .zero, size: wallpaperSize)).fill()
    }
  }
}

func renderLens(_ palette: Palette, to output: URL) throws {
  try png(NSSize(width: 240, height: 160), output: output) {
    let context = NSGraphicsContext.current!.cgContext
    context.setBlendMode(.normal)
    context.setAlpha(1)
    context.setLineWidth(38)
    context.setStrokeColor(palette.ribbon.withAlphaComponent(0.42).cgColor)
    context.strokeEllipse(in: CGRect(x: -100, y: -90, width: 300, height: 280))

    context.setLineWidth(9)
    context.setStrokeColor(palette.highlight.withAlphaComponent(0.66).cgColor)
    context.strokeEllipse(in: CGRect(x: -88, y: -78, width: 276, height: 256))

    context.setLineWidth(8)
    context.setStrokeColor(palette.shadow.withAlphaComponent(0.34).cgColor)
    context.strokeEllipse(in: CGRect(x: -110, y: -100, width: 324, height: 304))
  }
}

guard CommandLine.arguments.count == 3 else {
  fputs("usage: swift render-liquid-glass-assets.swift <master-image> <output-directory>\n", stderr)
  exit(2)
}

let masterImageURL = URL(fileURLWithPath: CommandLine.arguments[1])
guard let masterImage = NSImage(contentsOf: masterImageURL), masterImage.size.width > 0, masterImage.size.height > 0 else {
  fputs("unable to load approved wallpaper master\n", stderr)
  exit(3)
}
let directory = URL(fileURLWithPath: CommandLine.arguments[2], isDirectory: true)
try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)

let light = Palette(
  top: .init(hex: 0xF5FAF8),
  middle: .init(hex: 0xD1EEF2),
  bottom: .init(hex: 0x80B7C2),
  ribbon: .init(hex: 0xE2F8F1),
  shadow: .init(hex: 0x286883),
  highlight: .white
)
let dark = Palette(
  top: .init(hex: 0x193746),
  middle: .init(hex: 0x285668),
  bottom: .init(hex: 0x122F46),
  ribbon: .init(hex: 0xA5DEE4),
  shadow: .init(hex: 0x071F30),
  highlight: .init(hex: 0xE0FFFF)
)

try renderApprovedWallpaper(masterImage, darkening: 0, to: directory.appendingPathComponent("wallpaper-light.png"))
try renderApprovedWallpaper(masterImage, darkening: 0.34, to: directory.appendingPathComponent("wallpaper-dark.png"))
try renderLens(light, to: directory.appendingPathComponent("lens-light.png"))
try renderLens(dark, to: directory.appendingPathComponent("lens-dark.png"))
