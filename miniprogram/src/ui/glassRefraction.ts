export type GlassSurfaceRect = {
  left: number;
  top: number;
  width: number;
  height: number;
  role?: 'primary' | 'secondary';
};

export type RefractionTheme = 'light' | 'dark';

export type RefractionOptions = {
  radius?: number;
  rim?: number;
  bend?: number;
  reduced?: boolean;
  role?: 'primary' | 'secondary';
};

export type RoundedRectSample = {
  inside: boolean;
  depth: number;
  normalX: number;
  normalY: number;
  bend: number;
  highlight: number;
};

export function glassBackingScale(pixelRatio: number) {
  if (!Number.isFinite(pixelRatio)) return 1;
  return Math.max(1, Math.min(1.25, pixelRatio));
}

export function glassOpticsForRole(role: GlassSurfaceRect['role']) {
  return role === 'primary'
    ? { radius: 30, rim: 13, bend: 12.5 }
    : { radius: 24, rim: 8, bend: 6 };
}

export function roundedRectSample(
  x: number,
  y: number,
  width: number,
  height: number,
  radius: number,
  rim: number,
  maxBend = 25,
): RoundedRectSample {
  const safeRadius = Math.max(0, Math.min(radius, width / 2, height / 2));
  const px = x - width / 2;
  const py = y - height / 2;
  const qx = Math.abs(px) - (width / 2 - safeRadius);
  const qy = Math.abs(py) - (height / 2 - safeRadius);
  const ax = Math.max(qx, 0);
  const ay = Math.max(qy, 0);
  const length = Math.hypot(ax, ay);
  const signedDistance = length + Math.min(Math.max(qx, qy), 0) - safeRadius;
  let normalX = 0;
  let normalY = 0;

  if (length > 0) {
    normalX = ax / length * Math.sign(px);
    normalY = ay / length * Math.sign(py);
  } else if (qx > qy) {
    normalX = Math.sign(px);
  } else {
    normalY = Math.sign(py);
  }

  const depth = -signedDistance;
  const bend = depth >= 0 && depth < rim
    ? Math.pow(1 - depth / rim, 2) * maxBend
    : 0;
  const highlight = depth >= 0 && depth < 4 ? (1 - depth / 4) * .45 : 0;
  return { inside: signedDistance <= 0, depth, normalX, normalY, bend, highlight };
}

export function refractRoundedSurface(
  source: Uint8ClampedArray,
  sourceWidth: number,
  sourceHeight: number,
  rect: GlassSurfaceRect,
  options: RefractionOptions = {},
) {
  const width = Math.max(1, Math.round(rect.width));
  const height = Math.max(1, Math.round(rect.height));
  const radius = Math.min(options.radius ?? 28, width / 2, height / 2);
  const rim = Math.max(1, Math.min(options.rim ?? 10, width / 4, height / 4));
  const maxBend = Math.max(0, options.bend ?? 25);
  const output = new Uint8ClampedArray(width * height * 4);

  if (options.reduced) return output;
  for (let y = 0; y < height; y += 1) {
    for (let x = 0; x < width; x += 1) {
      const sample = roundedRectSample(x + .5, y + .5, width, height, radius, rim, maxBend);
      if (!sample.inside || sample.depth >= rim) continue;
      const sourceX = Math.max(0, Math.min(sourceWidth - 1, Math.round(rect.left + x - sample.normalX * sample.bend)));
      const sourceY = Math.max(0, Math.min(sourceHeight - 1, Math.round(rect.top + y - sample.normalY * sample.bend)));
      const sourceOffset = (sourceY * sourceWidth + sourceX) * 4;
      const outputOffset = (y * width + x) * 4;
      for (let channel = 0; channel < 3; channel += 1) {
        output[outputOffset + channel] = source[sourceOffset + channel] * (1 - sample.highlight) + 255 * sample.highlight;
      }
      output[outputOffset + 3] = 255;
    }
  }
  return output;
}

export function sampleOrigin(rect: GlassSurfaceRect, scrollTop: number) {
  return { x: rect.left, y: rect.top - scrollTop };
}

export function visibleRefractionRects(rects: GlassSurfaceRect[], viewportHeight: number, limit = 4) {
  const unique = new Set<string>();
  return rects.filter(rect => {
    if (!rect || rect.width < 70 || rect.height < 42 || rect.height > viewportHeight) return false;
    if (rect.top >= viewportHeight || rect.top + rect.height <= 0) return false;
    const key = [rect.left, rect.top, rect.width, rect.height].map(value => Math.round(value)).join(':');
    if (unique.has(key)) return false;
    unique.add(key);
    return true;
  }).slice(0, limit);
}

export function roundedGlassPath(ctx: any, left: number, top: number, width: number, height: number, radius: number) {
  const r = Math.min(radius, width / 2, height / 2);
  ctx.beginPath();
  ctx.moveTo(left + r, top);
  ctx.lineTo(left + width - r, top);
  ctx.quadraticCurveTo(left + width, top, left + width, top + r);
  ctx.lineTo(left + width, top + height - r);
  ctx.quadraticCurveTo(left + width, top + height, left + width - r, top + height);
  ctx.lineTo(left + r, top + height);
  ctx.quadraticCurveTo(left, top + height, left, top + height - r);
  ctx.lineTo(left, top + r);
  ctx.quadraticCurveTo(left, top, left + r, top);
  ctx.closePath();
}

export function paintGlassWallpaper(ctx: any, width: number, height: number, theme: RefractionTheme, offsetX = 0) {
  const dark = theme === 'dark';
  const gradient = ctx.createLinearGradient(0, 0, width, height);
  const stops = dark
    ? [[0, '#173a4b'], [0.4, '#29576a'], [1, '#132c48']]
    : [[0, '#e5f3ed'], [0.42, '#c0e5e4'], [1, '#6eafc3']];
  stops.forEach(([position, color]) => gradient.addColorStop(position, color));
  ctx.fillStyle = gradient;
  ctx.fillRect(0, 0, width, height);

  ctx.save();
  ctx.translate(offsetX - width * 0.45, height * 0.18);
  ctx.rotate(-0.3);
  const colors = dark
    ? ['rgba(49,88,111,.64)', 'rgba(136,187,191,.68)', 'rgba(66,115,135,.54)']
    : ['rgba(119,165,184,.62)', 'rgba(239,255,243,.76)', 'rgba(150,203,209,.62)'];
  colors.forEach((color, index) => {
    ctx.beginPath();
    ctx.ellipse(width * 0.18 - index * width * 0.035, height * 0.72, width * (0.72 + index * 0.1), height * (0.78 + index * 0.04), 0, 0, Math.PI * 2);
    ctx.strokeStyle = color;
    ctx.lineWidth = Math.max(14, width * (0.062 - index * 0.005));
    ctx.stroke();
  });
  ctx.restore();

  const glow = ctx.createRadialGradient(width * 0.218, height * 0.107, 0, width * 0.218, height * 0.107, width * 0.9);
  glow.addColorStop(0, dark ? 'rgba(191,238,255,.08)' : 'rgba(255,255,255,.44)');
  glow.addColorStop(1, 'rgba(255,255,255,0)');
  ctx.fillStyle = glow;
  ctx.fillRect(0, 0, width, height);
}

export function drawBackgroundRefraction(
  ctx: any,
  source: any,
  rect: GlassSurfaceRect,
  options: RefractionOptions = {},
) {
  const { left, top, width, height } = rect;
  const radius = Math.min(options.radius ?? 28, width / 2, height / 2);
  const rim = Math.max(1, Math.min(options.rim ?? 10, Math.floor(width / 4), Math.floor(height / 4)));
  const maxBend = Math.max(0, options.bend ?? 25);
  ctx.save();
  roundedGlassPath(ctx, left, top, width, height, radius);
  ctx.clip();

  if (!options.reduced) {
    for (let index = 0; index < rim; index += 1) {
      const bend = Math.max(0, Math.round(maxBend * (1 - index / rim) ** 2));
      const horizontalWidth = Math.max(1, width - index * 2);
      ctx.drawImage(source, left + index, top + index + bend, horizontalWidth, 1, left + index, top + index, horizontalWidth, 1);
      ctx.drawImage(source, left + index, top + height - index - bend - 1, horizontalWidth, 1, left + index, top + height - index - 1, horizontalWidth, 1);

      const verticalHeight = Math.max(1, height - index * 2);
      ctx.drawImage(source, left + index + bend, top + index, 1, verticalHeight, left + index, top + index, 1, verticalHeight);
      ctx.drawImage(source, left + width - index - bend - 1, top + index, 1, verticalHeight, left + width - index - 1, top + index, 1, verticalHeight);
    }
  }

  const clearFill = options.role === 'secondary' ? 'rgba(235,248,252,.045)' : 'rgba(235,248,252,.025)';
  ctx.fillStyle = options.reduced ? 'rgba(235,248,252,.72)' : clearFill;
  ctx.fillRect(left, top, width, height);
  ctx.restore();

  if (options.reduced) {
    roundedGlassPath(ctx, left + 0.5, top + 0.5, width - 1, height - 1, Math.max(1, radius - 0.5));
    ctx.strokeStyle = 'rgba(235,250,255,.72)';
    ctx.lineWidth = 1;
    ctx.stroke();
  }
}
