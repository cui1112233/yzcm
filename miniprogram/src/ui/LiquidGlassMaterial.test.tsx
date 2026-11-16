import { expect, test, vi } from 'vitest';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
vi.mock('@tarojs/components', () => ({ View: 'View', Image: 'Image' }));
import LiquidGlassMaterial from './LiquidGlassMaterial';
const nodes = (tree: any): any[] => !tree || typeof tree !== 'object' ? [] : [tree, ...[tree.props?.children].flat(Infinity).flatMap(nodes)];
test('glass only owns the wallpaper, leaving card optics attached to cards', () => {
  const tree = nodes(LiquidGlassMaterial({ theme: 'light', reduced: false }));
  expect(tree.filter(n => n.type === 'Image')).toHaveLength(1);
  expect(tree.find(n => n.type === 'Image')?.props.mode).toBe('scaleToFill');
  expect(tree.every(n => n.type !== 'Canvas')).toBe(true);
  expect(tree.every(n => !n.props.onTouchStart && !n.props.onTouchMove)).toBe(true);
});
test('reduced transparency removes decorative layers', () => {
  expect(nodes(LiquidGlassMaterial({ theme: 'dark', reduced: true }))).toHaveLength(1);
});
test('ships distinct 1.0.11 PNG wallpapers and renders the selected theme', () => {
  const light = resolve(__dirname, '../assets/glass/wallpaper-light.png');
  const dark = resolve(__dirname, '../assets/glass/wallpaper-dark.png');
  for (const asset of [light, dark]) {
    expect(existsSync(asset)).toBe(true);
    const bytes = readFileSync(asset);
    expect(bytes.subarray(1, 4).toString()).toBe('PNG');
  }
  expect(readFileSync(light)).not.toEqual(readFileSync(dark));

  const lightImage = nodes(LiquidGlassMaterial({ theme: 'light', reduced: false })).find(n => n.type === 'Image');
  const darkImage = nodes(LiquidGlassMaterial({ theme: 'dark', reduced: false })).find(n => n.type === 'Image');
  expect(lightImage.props.src).toContain('wallpaper-light.png');
  expect(darkImage.props.src).toContain('wallpaper-dark.png');
});
