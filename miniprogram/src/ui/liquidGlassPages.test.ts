import { readFileSync } from 'node:fs';
import { expect, test } from 'vitest';

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8');

test('functions maps the reference script card to primary glass and other entries to secondary glass', () => {
  const source = read('../pages/functions/index.tsx');
  expect(source).toContain('item.key === "script" ? "liquid-glass--primary" : "liquid-glass--secondary"');
  expect(source).toContain('data-glass-role={item.key === "script" ? "primary" : "secondary"}');
  expect(source).toContain('tools-glass-feature liquid-glass--secondary');
});

test('tasks uses one glass list container instead of a glass rim on every row', () => {
  const source = read('../pages/tasks/index.tsx');
  expect(source).toContain('mp-task-list liquid-glass--list');
  expect(source).not.toContain('mp-task-card mp-task-card-new liquid-glass');
});

test('workspace keeps the hero artwork visible and marks explicit glass surfaces', () => {
  const component = read('../components/WorkspaceHomeHeader.tsx');
  const style = read('../components/WorkspaceHomeHeader.scss');
  const page = read('../pages/workspace/index.tsx');
  expect(component).toContain('home-status-card liquid-glass--primary');
  expect(component).toContain('data-glass-role="primary"');
  expect(page).toContain('mp-workspace-action liquid-glass--secondary');
  expect(style).not.toMatch(/\.home-status-card\{[^}]*position:sticky/);
  expect(style).not.toMatch(/\.home-status-card\{[^}]*margin:-/);
  expect(component).toContain('<Image className="home-hero-art"');
  expect(style).not.toMatch(/\.mp-liquid[^}]*\.home-hero-art\{[^}]*display\s*:\s*none/);
});

test('functions leaves card optics to the shared liquid material stack', () => {
  const style = read('../pages/functions/functions-glass.scss');
  expect(style).not.toMatch(/\.tools-entry::before/);
  expect(style).not.toMatch(/\.tools-entry\.liquid-glass--secondary::before/);
  expect(style).not.toMatch(/\.tools-entry[^\{]*\{[^}]*background:var\(--glass-fill\)/);
  expect(style).not.toMatch(/\.tools-entry[^\{]*\{[^}]*border:1rpx solid var\(--glass-edge\)/);
});

test('profile applies glass to grouped surfaces rather than every setting row', () => {
  const source = read('../pages/profile/index.tsx');
  expect(source).toContain('mp-profile-hero liquid-glass--primary');
  expect(source).toContain('mp-usage-card liquid-glass--secondary');
  expect(source).toContain('mp-profile-section liquid-glass--list');
  expect(source).not.toContain('mp-profile-item liquid-glass');
});

test('profile detail opts summary cards into secondary glass explicitly', () => {
  const source = read('../pages/profile-detail/index.tsx');
  expect(source).toContain('mp-card liquid-glass--secondary');
  expect(source).toContain('data-glass-role="secondary"');
});
