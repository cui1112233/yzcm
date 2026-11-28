import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const sourcePath = path.join(path.dirname(fileURLToPath(import.meta.url)), 'AccountCenterLayout.jsx');
const cssPath = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'styles', 'account-center-pixel-parity.css');
const mainPath = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', '..', 'user', 'main.jsx');

test('renders an integrated personal-center shell with a return action', () => {
  const source = fs.readFileSync(sourcePath, 'utf8');

  assert.match(source, /className="pc-shell"/);
  assert.match(source, /<EnergyScene/);
  assert.match(source, /aria-label="返回工作台"/);
  assert.match(source, /onClick=\{onReturn\}/);
  assert.doesNotMatch(source, /legacy-sidebar/);
  assert.doesNotMatch(source, /account-center-popover/);
});

test('shows the server role and gates navigation with server capabilities', () => {
  const source = fs.readFileSync(sourcePath, 'utf8');

  assert.match(source, /account\?\.canonicalRole/);
  for (const capability of ['team_governance', 'account_governance', 'platform_admin']) {
    assert.ok(source.includes(`capabilities.includes('${capability}')`));
  }
  assert.doesNotMatch(source, /account\?\.role/);
});

test('keeps account overview, profile, billing, and security navigation available on every center page', () => {
  const source = fs.readFileSync(sourcePath, 'utf8');

  assert.doesNotMatch(source, /const profileOpen/);
  assert.doesNotMatch(source, /profileOpen \?/);
  for (const label of ['账户总览', '个人资料', '能量与账单', '账号安全']) assert.match(source, new RegExp(label));
});

test('exposes the YZCM API console entry in the sidebar', () => {
  const source = fs.readFileSync(sourcePath, 'utf8');

  assert.match(source, /href=\{ENERGY_CONSOLE_URL\}/);
  assert.match(source, /target="_blank"/);
  assert.match(source, /YZCM API/);
});

test('uses the searchable authorized account switcher instead of free-text usernames', () => {
  const source = fs.readFileSync(sourcePath, 'utf8');

  assert.match(source, /<AccountSwitchMenu status=\{impersonation\} onSwitched=\{reloadForAccountSwitch\} \/>/);
  assert.doesNotMatch(source, /placeholder="输入目标账号用户名"/);
});

test('keeps sidebar actions visible while navigation and page content scroll independently', () => {
  const css = fs.readFileSync(cssPath, 'utf8');

  assert.match(css, /\.pc-sidebar\s*\{[^}]*overflow:\s*hidden/s);
  assert.match(css, /\.pc-sidebar-profile\s*\{[^}]*flex:\s*0\s+0\s+auto/s);
  assert.match(css, /\.pc-sidebar-nav\s*\{[^}]*flex:\s*1\s+1\s+auto[^}]*overflow-y:\s*auto/s);
  assert.match(css, /\.pc-sidebar-actions\s*\{[^}]*flex:\s*0\s+0\s+auto/s);
  assert.match(css, /\.pc-main\s*\{[^}]*overflow-y:\s*auto/s);
});

test('pixel-parity shell is the only account-center visual authority', () => {
  const css = fs.readFileSync(cssPath, 'utf8');
  const main = fs.readFileSync(mainPath, 'utf8');

  assert.doesNotMatch(css, /\.account-center-popover\s*\{\s*position:\s*fixed/);
  assert.doesNotMatch(css, /account-center-drawer-open .legacy-main[\s\S]*margin-left:\s*284px/);
  assert.match(css, /\.pc-shell\s*\{/);
  assert.match(css, /grid-template-columns:\s*220px minmax\(0, 1fr\)/);
  assert.match(main, /account-center-pixel-parity\.css/);
  assert.doesNotMatch(main, /account-center-visual-rebuild\.css/);
});
