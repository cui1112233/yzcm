import { expect, test } from 'vitest';
import { compile } from 'sass';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import postcss from 'postcss';
import selectorParser from 'postcss-selector-parser';

const supportedPseudos = new Set([':before', ':after', '::before', '::after']);

const stylesheetPaths = [
  '../app.scss',
  'system.scss',
  'liquidGlass.scss',
  '../pages/module-detail/fetch-studio.scss',
];

test('shared WeChat styles avoid unsupported :not selectors', () => {
  for (const path of stylesheetPaths) {
    const css = compile(fileURLToPath(new URL(path, import.meta.url)), {
      silenceDeprecations: ['legacy-js-api'],
    }).css;
    expect(css, path).not.toContain(':not(');
  }
});

function walkWxss(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return walkWxss(path);
    return entry.isFile() && entry.name.endsWith('.wxss') ? [path] : [];
  });
}

test('generated WXSS contains no unsupported selector pseudo-classes', () => {
  const incompatible: string[] = [];
  for (const path of walkWxss(fileURLToPath(new URL('../../dist/', import.meta.url)))) {
    postcss.parse(readFileSync(path, 'utf8')).walkRules(rule => {
      selectorParser(selectors => selectors.walkPseudos(pseudo => {
        if (!supportedPseudos.has(pseudo.value)) incompatible.push(`${path}: ${pseudo.value}`);
      })).processSync(rule.selector);
    });
  }
  expect(incompatible).toEqual([]);
});
