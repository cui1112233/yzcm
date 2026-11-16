import { expect, test } from 'vitest';
import { readFileSync } from 'node:fs';
import { visibleGlassRects, glassVersion, liquidEnabled, glassSelectors } from './liquidGlass';
test('the 1.0.11 public shell enables liquid glass without leaking into login or business pages', () => {
  expect(liquidEnabled('mp-page native-login-page')).toBe(false);
  expect(liquidEnabled('mp-page mp-profile-page')).toBe(true);
  expect(liquidEnabled('mp-page mp-primary-page mp-functions-page')).toBe(true);
  expect(liquidEnabled('mp-page mp-profile-detail-page')).toBe(true);
  expect(liquidEnabled('mp-page mp-studio')).toBe(false);
  expect(liquidEnabled('mp-page mp-detail-page')).toBe(false);
});
test('only distinct, visible, bounded surfaces are sampled', () => {
  const rect = { left: 12, top: 20, width: 320, height: 90 };
  const result = visibleGlassRects([rect, rect, { ...rect, top: 900 }, { ...rect, height: 3000 }, ...Array.from({ length: 30 }, (_, i) => ({ ...rect, top: 30 + i }))], 800);
  expect(result.length).toBeLessThanOrEqual(8);
  expect(result.filter(r => r.top === 20)).toHaveLength(1);
  expect(result.every(r => r.height < 800 && r.top < 800)).toBe(true);
});
test('scroll, navigation and theme invalidation reject in-flight old results', () => {
  const version = glassVersion();
  const beforeScroll = version.current(); version.invalidate();
  expect(version.valid(beforeScroll)).toBe(false);
  expect(version.valid(version.current())).toBe(true);
  const beforeHide = version.current(); version.invalidate();
  expect(version.valid(beforeHide)).toBe(false);
});
test('public glass styles never reference repeated lens PNGs', () => {
  const source = readFileSync(new URL('./liquidGlass.scss', import.meta.url), 'utf8');
  expect(source).not.toContain('lens-light.png');
  expect(source).not.toContain('lens-dark.png');
});
test('the 1.0.11 renderer keeps the original public-card selector set', () => {
  expect(glassSelectors).toContain('.tools-entry');
  expect(glassSelectors).toContain('.mp-card');
  expect(glassSelectors).toContain('.mp-profile-section');
});
test('shared styles define clear centers and edge-focused optical tokens', () => {
  const source = readFileSync(new URL('./liquidGlass.scss', import.meta.url), 'utf8');
  expect(source).toContain('.mp-card,.tools-entry,.tools-section-01 .tools-entry,.tools-glass-feature,');
  expect(source).toContain('--liquid-center-primary:');
  expect(source).toContain('--liquid-center-secondary:');
  expect(source).toContain('--liquid-center-list:');
  expect(source).toContain('--liquid-inner-rim:');
  expect(source).toContain('--liquid-edge-highlight:');
  expect(source).toContain('--liquid-short-shadow:');
});
test('the public shell keeps the dynamic optical layer fixed behind interactive content', () => {
  const source = readFileSync(new URL('./liquidGlass.scss', import.meta.url), 'utf8');
  expect(source).toContain('.liquid-wall{position:fixed;inset:0;width:100vw;height:100vh;z-index:-1;');
  expect(source).toContain('.dynamic-glass-layer{position:fixed;inset:0;');
  expect(source).toContain('.dynamic-glass-canvas{width:100%;height:100%}');
  expect(source).toContain('&.liquid-reduced');
  expect(source).toMatch(/\.liquid-reduced[\s\S]*\.dynamic-glass-layer\{display:none/);
});
test('public material uses restrained center tint and one shared edge', () => {
  const source = readFileSync(new URL('./liquidGlass.scss', import.meta.url), 'utf8');
  expect(source).toContain('background:var(--liquid-center-primary)!important');
  expect(source).toContain('background:var(--liquid-center-secondary)!important');
  expect(source).toContain('box-shadow:inset 0 1px 0 var(--liquid-edge-highlight),inset 0 0 0 1px var(--liquid-inner-rim),0 4px 12px var(--liquid-short-shadow)!important');
  expect(source).not.toContain('backdrop-filter:blur(20px)');
});
test('function copy uses the cached 1.0.11 text treatment', () => {
  const shared = readFileSync(new URL('./liquidGlass.scss', import.meta.url), 'utf8');
  const functions = readFileSync(new URL('../pages/functions/functions-glass.scss', import.meta.url), 'utf8');
  expect(shared).toContain('.tools-entry-copy,.tools-section-heading{background:transparent!important}');
  expect(shared).toContain('.tools-entry-copy{text-shadow:0 1px 7px rgba(12,35,50,.22)}');
  expect(functions).toContain('.tools-section-title{font-size:24rpx}.tools-section-caption{font-size:22rpx;line-height:1.5}');
  expect(functions).toContain('.tools-entry-description,.tools-section-01 .tools-entry-description{font-size:22rpx');
});
