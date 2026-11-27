import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const read = relative => fs.readFileSync(path.join(root, relative), 'utf8');

test('all personal-center routes use one shared energy-glass shell', () => {
  const layout = read('shared/layouts/AccountCenterLayout.jsx');
  assert.match(layout, /RoleEnergyIcon/);
  assert.match(layout, /account-center-energy-scene/);
  assert.match(layout, /account-center-topbar/);
  assert.doesNotMatch(layout, /if\s*\(pathname === ['"]\/profile['"]\)/);
  assert.doesNotMatch(layout, /profile-glass-workspace/);
});

test('profile and security pages do not show a fabricated security score', () => {
  const profile = read('user/pages/ProfilePage.jsx');
  const security = read('user/pages/SecurityPage.jsx');
  assert.doesNotMatch(profile, /安全评分|securityScore/);
  assert.doesNotMatch(security, /安全评分|percent=\{score\}/);
});

test('the visual system provides glass energy motion and reduced-motion fallback', () => {
  const css = read('shared/styles/account-center-visual-rebuild.css');
  assert.match(css, /\.account-center-energy-scene/);
  assert.match(css, /\.role-energy-icon/);
  assert.match(css, /backdrop-filter:\s*blur/);
  assert.match(css, /@media\s*\(prefers-reduced-motion:\s*reduce\)/);
});

test('all seven approved pages stay inside the common account-center page contract', () => {
  for (const file of [
    'user/pages/AccountOverviewPage.jsx',
    'user/pages/ProfilePage.jsx',
    'user/pages/EnergyBillingPage.jsx',
    'user/pages/SecurityPage.jsx',
    'user/pages/TeamPage.jsx',
    'user/pages/ApiConfigPage.jsx',
    'user/pages/AccountRolePage.jsx'
  ]) {
    assert.match(read(file), /account-center-page|AccountRolePage/, `${file} must use the shared page contract`);
  }
});
