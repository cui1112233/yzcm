import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';
import { isAccountCenterPath, normalizeAccountCenterReturnPath } from '../../shared/layouts/accountCenterNavigation.js';

const source = file => readFileSync(new URL(file, import.meta.url), 'utf8');

test('account overview and energy are canonical routes inside the existing account shell', () => {
  const app = source('../App.jsx');
  const layout = source('../../shared/layouts/AccountCenterLayout.jsx');
  assert.match(app, /'\/account': AccountOverviewPage/);
  assert.match(app, /'\/usage': EnergyBillingPage/);
  assert.match(app, /if \(pathname === '\/member'\) return <AccountOverviewRedirect/);
  assert.match(app, /history\.replaceState[\s\S]*?dispatchEvent\(new PopStateEvent\('popstate'\)\)/);
  assert.match(app, /addEventListener\('popstate', handlePopState\);\s+handlePopState\(\)/);
  assert.match(layout, /账户总览/);
  assert.match(layout, /能量与账单/);
  assert.match(source('../../shared/layouts/UserLayout.jsx'), /pushState\(\{\}, '', '\/account'\)/);
  for (const path of ['/account', '/member', '/usage']) {
    assert.equal(isAccountCenterPath(path), true);
    assert.equal(normalizeAccountCenterReturnPath(path, 'http://localhost'), '/');
  }
  for (const old of ['./MemberCenterPage.jsx', './UsageStatsPage.jsx']) assert.equal(existsSync(new URL(old, import.meta.url)), false);
  for (const text of [app, layout, source('./ProfilePage.jsx')]) assert.doesNotMatch(text, /会员中心|用量与制作|会员权益/);
});
