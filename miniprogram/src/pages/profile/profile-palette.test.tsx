import { beforeEach, expect, test, vi } from 'vitest';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { compile } from 'sass';
import postcss from 'postcss';
import { fileURLToPath } from 'node:url';
import { readFileSync } from 'node:fs';

const h = vi.hoisted(() => ({ preference: 'dark' }));
vi.mock('@tarojs/taro', () => ({
  default: { getStorageSync: (key: string) => key === 'yzcm.mobile.theme' ? h.preference : false },
  useDidShow: () => {},
}));
vi.mock('@tarojs/components', () => ({ View: 'div', Text: 'span', Image: 'img', Button: 'button' }));
vi.mock('../../ui/DynamicGlassCanvas', () => ({ default: 'dynamic-glass-canvas' }));
import { ThemedPage } from '../../theme';

// JSDOM matches selectors, but its computed-style implementation ignores CSS
// specificity. Resolve matching declarations in cascade order explicitly.
const rules = postcss.parse(['../../app.scss', '../../ui/system.scss', '../../ui/liquidGlass.scss'].map(path => compile(fileURLToPath(new URL(path, import.meta.url)), { silenceDeprecations: ['legacy-js-api'] }).css).join('\n'));
function hasDeclaration(selectorFragment: string, property: string, value: string) {
  let found = false;
  rules.walkRules(rule => {
    if (!rule.selector.includes(selectorFragment)) return;
    rule.walkDecls(declaration => {
      if (declaration.prop === property && declaration.value === value) found = true;
    });
  });
  return found;
}
function hasDeclarationContaining(selectorFragment: string, property: string, valueFragment: string) {
  let found = false;
  rules.walkRules(rule => {
    if (!rule.selector.includes(selectorFragment)) return;
    rule.walkDecls(declaration => {
      if (declaration.prop === property && declaration.value.includes(valueFragment)) found = true;
    });
  });
  return found;
}
beforeEach(() => { vi.clearAllMocks(); });
test.each(['dark', 'light'])('saved %s preference keeps profile surfaces on the dynamic liquid material', preference => {
  h.preference = preference;
  const sample = createElement('div', {},
    ...['mp-card', 'mp-profile-hero', 'mp-profile-section', 'mp-usage-card'].map(className => createElement('div', { className, key: className },
      ...['mp-profile-name', 'mp-profile-item-title', 'mp-usage-title', 'mp-usage-number', 'mp-missing-data'].map(textClass => createElement('span', { className: textClass, key: textClass }, '账号资料')))));
  const html = renderToStaticMarkup(createElement(ThemedPage, { className: 'mp-page mp-profile-page' }, sample));
  expect(html).toContain(`mp-theme-${preference}`);
  expect(html).not.toContain('mp-profile-ordinary');
  expect(html).toContain('mp-liquid');
  expect(html).toContain('liquid-wall');
  expect(html).toContain('dynamic-glass-canvas');
});

test('service section always shows the transparency preference', () => {
  const source = readFileSync(new URL('./index.tsx', import.meta.url), 'utf8');
  expect(source).toContain('section.key === "service" ? <View className="liquid-preference">');
  expect(source).not.toContain('PUBLIC_LIQUID_GLASS_ENABLED');
  expect(source).toContain('降低透明度');
});

test('compiled liquid profile selectors use WeChat-compatible explicit classes', () => {
  let liquidRules = 0;
  rules.walkRules(rule => {
    if (!rule.selector.includes('mp-profile-hero') && !rule.selector.includes('mp-profile-section') && !rule.selector.includes('mp-usage-card')) return;
    expect(rule.selector).not.toContain(':not(');
    if (rule.selector.includes('mp-liquid')) liquidRules += 1;
  });
  expect(liquidRules).toBeGreaterThan(0);
});

test('profile cards never repeat fixed lens artwork', () => {
  expect(hasDeclarationContaining('.mp-theme.mp-liquid.mp-profile-page .mp-profile-hero::after', 'background-image', 'lens-dark.png')).toBe(false);
  expect(hasDeclarationContaining('.mp-theme.mp-liquid.mp-theme-light.mp-profile-page .mp-profile-hero::after', 'background-image', 'lens-light.png')).toBe(false);
  expect(hasDeclarationContaining('.mp-theme.mp-liquid.mp-profile-detail-page .mp-card::after', 'background-image', 'lens-dark.png')).toBe(false);
  expect(hasDeclarationContaining('.mp-theme.mp-liquid.mp-theme-light.mp-profile-detail-page .mp-card::after', 'background-image', 'lens-light.png')).toBe(false);
});
