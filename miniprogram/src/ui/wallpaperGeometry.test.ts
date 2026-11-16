import { describe, expect, test } from 'vitest';
import { aspectFillSourceRect } from './wallpaperGeometry';

describe('aspectFillSourceRect', () => {
  test('keeps the full source when source and viewport ratios match', () => {
    expect(aspectFillSourceRect(390, 844, 390, 844)).toEqual({ sx: 0, sy: 0, sw: 390, sh: 844 });
  });

  test('centers the vertical crop for the approved portrait wallpaper', () => {
    const rect = aspectFillSourceRect(658, 1449, 390, 844);
    expect(rect.sx).toBe(0);
    expect(rect.sw).toBe(658);
    expect(rect.sy).toBeCloseTo(12.51, 1);
    expect(rect.sh).toBeCloseTo(1423.98, 1);
  });

  test('centers a tighter vertical crop for a landscape viewport', () => {
    const rect = aspectFillSourceRect(658, 1449, 844, 390);
    expect(rect.sx).toBe(0);
    expect(rect.sw).toBe(658);
    expect(rect.sy).toBeCloseTo(572.47, 1);
    expect(rect.sh).toBeCloseTo(304.05, 1);
  });

  test('returns a zero rectangle for invalid dimensions', () => {
    expect(aspectFillSourceRect(0, 1449, 390, 844)).toEqual({ sx: 0, sy: 0, sw: 0, sh: 0 });
    expect(aspectFillSourceRect(658, 1449, 390, Number.NaN)).toEqual({ sx: 0, sy: 0, sw: 0, sh: 0 });
  });
});
