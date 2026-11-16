import { expect, test, vi } from 'vitest';
import {
  drawBackgroundRefraction,
  glassBackingScale,
  glassOpticsForRole,
  refractRoundedSurface,
  roundedRectSample,
  sampleOrigin,
  visibleRefractionRects,
} from './glassRefraction';

test('caps backing resolution below the web proof while preserving retina smoothing', () => {
  expect(glassBackingScale(1)).toBe(1);
  expect(glassBackingScale(2)).toBe(1.25);
  expect(glassBackingScale(3)).toBe(1.25);
});

test('uses the reference optical scale in CSS pixels instead of doubled backing pixels', () => {
  expect(glassOpticsForRole('primary')).toEqual({ radius: 30, rim: 13, bend: 12.5 });
  expect(glassOpticsForRole('secondary')).toEqual({ radius: 24, rim: 8, bend: 6 });
});

function context() {
  return Object.assign(
    Object.fromEntries([
      'save', 'restore', 'beginPath', 'moveTo', 'lineTo', 'quadraticCurveTo',
      'closePath', 'clip', 'drawImage', 'fillRect', 'stroke',
    ].map(key => [key, vi.fn()])),
    { fillStyle: '', strokeStyle: '', lineWidth: 0 },
  ) as any;
}

test('keeps the wallpaper coordinate continuous as the viewport scrolls', () => {
  expect(sampleOrigin({ left: 20, top: 260, width: 300, height: 100 }, 120))
    .toEqual({ x: 20, y: 140 });
});

test('models a clear center and a thick curved refractive rim', () => {
  const center = roundedRectSample(160, 50, 320, 100, 30, 26, 25);
  const edge = roundedRectSample(1, 50, 320, 100, 30, 26, 25);
  const corner = roundedRectSample(8, 8, 320, 100, 30, 26, 25);
  const outside = roundedRectSample(0, 0, 320, 100, 30, 26, 25);

  expect(center.inside).toBe(true);
  expect(center.bend).toBe(0);
  expect(edge.bend).toBeGreaterThan(20);
  expect(Math.abs(corner.normalX)).toBeGreaterThan(.3);
  expect(Math.abs(corner.normalY)).toBeGreaterThan(.3);
  expect(outside.inside).toBe(false);
});

test('renders a transparent center and a displaced two-dimensional curved rim', () => {
  const sourceWidth = 80;
  const sourceHeight = 60;
  const source = new Uint8ClampedArray(sourceWidth * sourceHeight * 4);
  for (let y = 0; y < sourceHeight; y += 1) {
    for (let x = 0; x < sourceWidth; x += 1) {
      const offset = (y * sourceWidth + x) * 4;
      source[offset] = x;
      source[offset + 1] = y;
      source[offset + 2] = 40;
      source[offset + 3] = 255;
    }
  }

  const result = refractRoundedSurface(source, sourceWidth, sourceHeight, {
    left: 20, top: 10, width: 40, height: 30,
  }, { radius: 10, rim: 8, bend: 7 });
  const pixel = (x: number, y: number) => result.slice((y * 40 + x) * 4, (y * 40 + x) * 4 + 4);

  expect([...pixel(20, 15)]).toEqual([0, 0, 0, 0]);
  expect(pixel(1, 15)[0]).toBeGreaterThan(21);
  expect(pixel(1, 15)[3]).toBe(255);
  expect([...pixel(0, 0)]).toEqual([0, 0, 0, 0]);
});

test('keeps at most four distinct visible bounded surfaces', () => {
  const base = { left: 10, top: 20, width: 300, height: 44 };
  const result = visibleRefractionRects([
    base,
    base,
    { ...base, top: -100 },
    { ...base, top: 900 },
    ...Array.from({ length: 12 }, (_, index) => ({ ...base, top: 30 + index * 50 })),
  ], 700);

  expect(result).toHaveLength(4);
  expect(result.filter(rect => rect.top === 20)).toHaveLength(1);
  expect(result.every(rect => rect.top < 700 && rect.top + rect.height > 0)).toBe(true);
});

test('samples displaced scanlines only inside the rounded optical rim', () => {
  const ctx = context();
  const source = {};
  drawBackgroundRefraction(
    ctx,
    source,
    { left: 20, top: 100, width: 300, height: 96 },
    { radius: 28, rim: 10, reduced: false },
  );

  expect(ctx.drawImage).toHaveBeenCalled();
  expect(ctx.clip).toHaveBeenCalledOnce();
  expect(ctx.restore).toHaveBeenCalledOnce();
  for (const call of ctx.drawImage.mock.calls) {
    const [, sx, sy, sw, sh, dx, dy, dw, dh] = call;
    expect(sx).toBeGreaterThanOrEqual(20);
    expect(sy).toBeGreaterThanOrEqual(100);
    expect(sx + sw).toBeLessThanOrEqual(320);
    expect(sy + sh).toBeLessThanOrEqual(196);
    expect(dx + dw).toBeLessThanOrEqual(320);
    expect(dy + dh).toBeLessThanOrEqual(196);
  }
});

test('reduced transparency keeps the surface but skips displaced sampling', () => {
  const ctx = context();
  drawBackgroundRefraction(
    ctx,
    {},
    { left: 20, top: 100, width: 300, height: 96 },
    { radius: 28, rim: 10, reduced: true },
  );

  expect(ctx.drawImage).not.toHaveBeenCalled();
  expect(ctx.fillRect).toHaveBeenCalledOnce();
  expect(ctx.stroke).toHaveBeenCalled();
});
