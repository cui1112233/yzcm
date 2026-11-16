import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { beforeAll, describe, expect, test } from 'vitest';

const execFileAsync = promisify(execFile);
const lightAsset = fileURLToPath(new URL('../src/assets/glass/wallpaper-light.webp', import.meta.url));
const darkAsset = fileURLToPath(new URL('../src/assets/glass/wallpaper-dark.webp', import.meta.url));

let wallpaperModule: Partial<typeof import('./prepare-glass-wallpaper.mjs')> = {};

beforeAll(async () => {
  try {
    wallpaperModule = await import('./prepare-glass-wallpaper.mjs');
  } catch {
    // The old generator depends on an undeclared sharp installation.
  }
});

describe('Material Study wallpaper generator', () => {
  test.each([
    [false, ['#e5f3ed', '#c0e5e4', '#6eafc3'], ['#6fa7bb', '#edfff1', '#86c7cf', '#386a9b'], '#ffffff70'],
    [true, ['#173a4b', '#29576a', '#132c48'], ['#478b97', '#a0d6d2', '#2b667f', '#193d62'], '#bfeeff14'],
  ] as const)('reproduces the exact %s theme geometry and palette', (dark, gradient, curves, glow) => {
    expect(typeof wallpaperModule.materialStudySvg).toBe('function');
    const svg = wallpaperModule.materialStudySvg!(dark);

    expect(svg).toContain('width="780" height="1500" viewBox="0 0 780 1500"');
    expect(svg).toContain('transform="translate(150 0) rotate(-21.772396 0 0)"');
    expect(svg.match(/<ellipse /g)).toHaveLength(4);
    expect(svg).toContain('cx="170" cy="160" r="700"');
    expect(svg).toContain(glow);
    gradient.forEach(color => expect(svg).toContain(color));
    curves.forEach(color => expect(svg).toContain(color));
  });

  test('writes 780 by 1500 WebP assets for both themes', async () => {
    expect(typeof wallpaperModule.writeMaterialStudyWallpapers).toBe('function');
    await wallpaperModule.writeMaterialStudyWallpapers!();
    const [light, dark] = await Promise.all([lightAsset, darkAsset].map(async asset => {
      const [{ stdout: dimensions }, { stdout: kind }] = await Promise.all([
        execFileAsync('/usr/bin/sips', ['-g', 'pixelWidth', '-g', 'pixelHeight', asset]),
        execFileAsync('/usr/bin/file', ['-b', asset]),
      ]);
      return { dimensions, kind };
    }));

    for (const asset of [light, dark]) {
      expect(asset.kind.toLowerCase()).toContain('web/p image');
      expect(asset.dimensions).toContain('pixelWidth: 780');
      expect(asset.dimensions).toContain('pixelHeight: 1500');
    }
  }, 15_000);
});
