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

test('account overview exposes every region in approved reference 01 without mock values', () => {
  const overview = source('./AccountOverviewPage.jsx');
  for (const region of [
    'pc-overview-identity',
    'pc-overview-balance',
    'pc-overview-trend',
    'pc-overview-ranking',
    'pc-overview-connection',
    'pc-overview-models',
    'pc-overview-bills'
  ]) assert.match(overview, new RegExp(region));
  assert.match(overview, /DesignedEmptyState/);
  assert.doesNotMatch(overview, /328\.60|96\.40|24\.80|6\.20/);
});

test('energy page exposes the all-team dashboard regions in approved reference 05', () => {
  const energy = source('./EnergyBillingPage.jsx');
  for (const region of [
    'pc-platform-consumption',
    'pc-platform-metrics',
    'pc-platform-trend',
    'pc-platform-ranking',
    'pc-platform-teams',
    'pc-platform-ledger'
  ]) assert.match(energy, new RegExp(region));
  assert.match(energy, /全平台团队消耗/);
  assert.doesNotMatch(energy, /486\.20|32\.80|186\.40/);
});
