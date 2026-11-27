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
  assert.match(layout, /<EnergyScene/);
  assert.match(layout, /pc-topbar/);
  assert.match(layout, /pc-sidebar/);
  assert.doesNotMatch(layout, /if\s*\(pathname === ['"]\/profile['"]\)/);
  assert.doesNotMatch(layout, /profile-glass-workspace/);
});

test('profile and security pages do not show a fabricated security score', () => {
  const profile = read('user/pages/ProfilePage.jsx');
  const security = read('user/pages/SecurityPage.jsx');
  assert.doesNotMatch(profile, /安全评分|securityScore/);
  assert.doesNotMatch(security, /安全评分|percent=\{score\}/);
});

test('profile matches the approved identity, basic information, ownership and visibility structure', () => {
  const profile = read('user/pages/ProfilePage.jsx');
  for (const region of ['pc-profile-identity', 'pc-profile-basic', 'pc-profile-ownership', 'pc-profile-visibility']) {
    assert.match(profile, new RegExp(region));
  }
  assert.match(profile, /RoleEnergyIcon/);
  assert.doesNotMatch(profile, /资料完整度|联合治理权限|ac-profile-cards-grid/);
});

test('security matches authentication, recovery, devices and activity without fake controls', () => {
  const security = read('user/pages/SecurityPage.jsx');
  for (const region of ['pc-security-auth', 'pc-security-recovery', 'pc-security-devices', 'pc-security-activity']) {
    assert.match(security, new RegExp(region));
  }
  assert.match(security, /修改密码/);
  assert.match(security, /忘记账号|忘记密码/);
  assert.doesNotMatch(security, /安全评分/);
});

test('the visual system provides glass energy motion and reduced-motion fallback', () => {
  const css = read('shared/styles/account-center-pixel-parity.css');
  assert.match(css, /\.pc-energy-scene/);
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

test('team governance uses the approved 团队管理 title consistently', () => {
  const team = read('user/pages/TeamPage.jsx');
  assert.match(team, /title="团队管理"/);
  assert.doesNotMatch(team, /title="组员管理"/);
});
