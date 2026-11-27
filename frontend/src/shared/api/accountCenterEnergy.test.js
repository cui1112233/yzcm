import assert from 'node:assert/strict';
import test from 'node:test';
import {
  exportEnergyLedgerPage, getAccountOverview, getEnergyTeams, loadEnergyDashboard, normalizeEnergyFilters, trustedEnergyConsole
} from './accountCenterEnergy.js';

test('normalizes one bounded filter and accepts real calendar dates', () => {
  assert.deepEqual(normalizeEnergyFilters({ teamId: ' blue ', member: ' mei ', from: '2024-02-29', to: '2024-03-01', limit: 23, offset: 46, format: 'csv' }), {
    teamId: 'blue', member: 'mei', from: '2024-02-29', to: '2024-03-01', limit: 23, offset: 46
  });
  for (const filters of [
    { from: '2026-02-29' }, { from: '2026-03-01', to: '2026-03-01' }, { from: '2024-01-01', to: '2025-01-02' },
    { limit: 101 }, { offset: -1 }, { offset: 10001 }, { teamId: 'x'.repeat(65) }, { member: 'x'.repeat(33) }
  ]) assert.throws(() => normalizeEnergyFilters(filters), /筛选|日期|分页/);
});

test('shares dashboard criteria while pinning group-member ledger and CSV to the subject', async t => {
  const requests = [];
  const original = globalThis.localStorage;
  globalThis.localStorage = { getItem: () => 'test-session' };
  t.after(() => { if (original) globalThis.localStorage = original; else delete globalThis.localStorage; });
  const amount = '9007199254740993.00000001';
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    url = new URL(url, 'http://localhost');
    requests.push(url);
    assert.equal(options.headers.get('authorization'), 'Bearer test-session');
    if (url.searchParams.get('format') === 'csv') {
      return new Response('actualCost\n' + amount, { headers: {
        'Content-Type': 'text/csv; charset=utf-8', 'X-Energy-Page-Limit': '20', 'X-Energy-Page-Offset': '20',
        'X-Energy-Has-More': 'true', 'X-Energy-Next-Offset': '40'
      } });
    }
    return Response.json({ consumption: { actualCost: amount }, entries: [] });
  });
  await getAccountOverview();
  const filters = { from: '2026-11-01', to: '2026-12-01', teamId: 'team & 1', member: 'peer', limit: 20, offset: 20 };
  const subject = { username: 'member', role: 'member', teamId: 'team & 1' };
  const result = await loadEnergyDashboard(filters, subject);
  assert.equal(result.summary.consumption.actualCost, amount);
  const exported = await exportEnergyLedgerPage(filters, subject);
  assert.equal(await exported.blob.text(), 'actualCost\n' + amount);
  assert.deepEqual(exported.page, { limit: 20, offset: 20, hasMore: true, nextOffset: 40 });
  assert.deepEqual(requests.map(url => url.pathname), [
    '/api/account-center/overview', '/api/account-center/energy', '/api/account-center/energy/ledger',
    '/api/account-center/energy/ranking', '/api/account-center/energy/ledger'
  ]);
  for (const url of requests.slice(1)) {
    for (const key of ['teamId', 'from', 'to']) assert.equal(url.searchParams.get(key), filters[key]);
    assert.equal(url.searchParams.get('member'), url.pathname.endsWith('/ledger') ? subject.username : 'peer');
  }
  assert.equal(requests.at(-1).searchParams.get('offset'), '20');
});

test('recharge uses only the configured console URL', () => {
  assert.equal(trustedEnergyConsole('http://newapi.yzcm.site/'), 'http://newapi.yzcm.site/');
  for (const url of [undefined, '', 'javascript:alert(1)', 'http://newapi.yzcm.site.example/', 'http://newapi.yzcm.site/?redirect=elsewhere']) {
    assert.equal(trustedEnergyConsole(url), '');
  }
});

test('team cards send the same member and date filter without excluding other teams', async t => {
  const original = globalThis.localStorage;
  globalThis.localStorage = { getItem: () => 'test-session' };
  t.after(() => { if (original) globalThis.localStorage = original; else delete globalThis.localStorage; });
  t.mock.method(globalThis, 'fetch', async path => {
    const url = new URL(path, 'http://localhost');
    assert.equal(url.pathname, '/api/account-center/energy/teams');
    assert.deepEqual(Object.fromEntries(url.searchParams), { member: 'peer', from: '2026-11-02', to: '2026-11-27', limit: '12', offset: '0' });
    return Response.json({ teams: [] });
  });
  await getEnergyTeams({ teamId: 'only-the-active-team', member: ' peer ', from: '2026-11-02', to: '2026-11-27', limit: 12, offset: 0 });
});
