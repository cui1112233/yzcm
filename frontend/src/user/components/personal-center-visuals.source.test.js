import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const assetRoot = path.join(root, 'user/assets/personal-center');
const read = relative => fs.readFileSync(path.join(root, relative), 'utf8');

test('uses committed brand assets instead of CSS or inline SVG stand-ins', () => {
  for (const asset of [
    'role-founder.png',
    'role-developer.png',
    'role-manager.png',
    'role-member.png',
    'energy-space.webp',
    'brand-energy-mark.webp'
  ]) {
    assert.equal(fs.existsSync(path.join(assetRoot, asset)), true, `${asset} must be committed`);
  }

  const roleIconSource = read('user/components/RoleEnergyIcon.jsx');
  assert.match(roleIconSource, /<img/);
  assert.doesNotMatch(roleIconSource, /<svg|linearGradient|clipPath/);
});

test('provides reusable visual primitives and the nine approved page-state contracts', () => {
  const visuals = read('user/components/PersonalCenterVisuals.jsx');
  const css = read('shared/styles/account-center-pixel-parity.css');
  for (const exportedName of ['EnergyScene', 'GlassPanel', 'MetricTile', 'DesignedEmptyState']) {
    assert.match(visuals, new RegExp(`export function ${exportedName}`));
  }
  for (const state of [
    'overview',
    'profile',
    'security',
    'team',
    'platform-consumption',
    'api-manager',
    'api-modal',
    'api-founder',
    'accounts'
  ]) {
    assert.match(css, new RegExp(`\\.pc-${state}`), `${state} visual contract is required`);
  }
  assert.match(css, /prefers-reduced-motion/);
});
