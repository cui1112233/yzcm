import { expect, test } from 'vitest';
import { refractGlass } from './glassOptics';
import { readFileSync } from 'node:fs';

const background = { width: 160, height: 160, data: new Uint8ClampedArray(160 * 160 * 4) };
for (let y = 0; y < 160; y++) for (let x = 0; x < 160; x++) {
  const i = (y * 160 + x) * 4;
  background.data.set([x, y, 90, 255], i);
}
test('preserves the background through the clear center', () => {
  const image = refractGlass(background, { left: 20, top: 30, width: 100, height: 60 }, 1);
  expect([...image.data.slice((30 * 100 + 50) * 4, (30 * 100 + 50) * 4 + 4)]).toEqual([70, 60, 90, 255]);
});
test('clips rounded corners and bends the background near the rim', () => {
  const image = refractGlass(background, { left: 20, top: 30, width: 100, height: 60 }, 1);
  expect(image.data[3]).toBe(0);
  const i = (30 * 100 + 4) * 4;
  expect(image.data[i]).toBeGreaterThan(24);
  expect(image.data[i + 3]).toBe(255);
});
test('scrolling changes the sampled background and clips outside the viewport safely', () => {
  const rect = { left: 20, top: 10, width: 100, height: 60 };
  const image = refractGlass(background, rect, 1);
  expect(image.data[(30 * 100 + 50) * 4 + 1]).toBe(40);
  const clipped = refractGlass(background, { ...rect, top: -100 }, 1);
  expect(clipped.data[(30 * 100 + 50) * 4 + 1]).toBe(0);
});
test('the native rim overlay leaves the center transparent while the page scrolls', () => {
  const image = refractGlass(background, { left: 20, top: 30, width: 100, height: 60 }, 1, true);
  expect(image.data[(30 * 100 + 50) * 4 + 3]).toBe(0);
  expect(image.data[(30 * 100 + 4) * 4 + 3]).toBe(255);
});

test('feature-page styles do not add a second white mask or optical edge', () => {
  const source = readFileSync(new URL('./functions-glass.scss', import.meta.url), 'utf8');
  expect(source).not.toMatch(/\.tools-entry-copy\{[^}]*linear-gradient/);
  expect(source).not.toMatch(/\.tools-entry-script[^\{]*::before/);
  expect(source).not.toMatch(/\.tools-glass-feature\{[^}]*box-shadow/);
});
