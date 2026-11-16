export type PixelBuffer = { width: number; height: number; data: Uint8ClampedArray };
export type GlassRect = { left: number; top: number; width: number; height: number };

/** Sample the actual wallpaper. The center stays clear; only the rounded rim refracts. */
export function refractGlass(source: PixelBuffer, rect: GlassRect, scale: number, rimOnly = false): PixelBuffer {
  const width = Math.max(1, Math.round(rect.width * scale));
  const height = Math.max(1, Math.round(rect.height * scale));
  const data = new Uint8ClampedArray(width * height * 4);
  const radius = Math.min(28 * scale, height / 2, width / 2), band = 12 * scale;
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
    const px = x - width / 2, py = y - height / 2;
    const qx = Math.abs(px) - (width / 2 - radius), qy = Math.abs(py) - (height / 2 - radius);
    const ax = Math.max(qx, 0), ay = Math.max(qy, 0), len = Math.hypot(ax, ay);
    const depth = -(len + Math.min(Math.max(qx, qy), 0) - radius);
    if (depth < 0) continue;
    if (rimOnly && depth >= band) continue;
    let nx = 0, ny = 0;
    if (len) { nx = ax / len * Math.sign(px); ny = ay / len * Math.sign(py); }
    else if (qx > qy) nx = Math.sign(px); else ny = Math.sign(py);
    const bend = depth < band ? Math.pow(1 - depth / band, 2) * 11 * scale : 0;
    const sx = Math.max(0, Math.min(source.width - 1, Math.round(rect.left * scale + x - nx * bend)));
    const sy = Math.max(0, Math.min(source.height - 1, Math.round(rect.top * scale + y - ny * bend)));
    const from = (sy * source.width + sx) * 4, to = (y * width + x) * 4;
    const glint = depth < 2 * scale ? (1 - depth / (2 * scale)) * .36 : 0;
    for (let c = 0; c < 3; c++) data[to + c] = source.data[from + c] * (1 - glint) + 255 * glint;
    data[to + 3] = 255;
  }
  return { width, height, data };
}
