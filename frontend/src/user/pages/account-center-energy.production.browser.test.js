import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { extname, resolve, sep } from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';

const dist = process.env.ENERGY_TASK3_PRODUCTION_DIST;
const browser = [process.env.CHROME_PATH, 'C:/Program Files/Google/Chrome/Application/chrome.exe',
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', '/usr/bin/chromium'].find(p => p && existsSync(p));

function fixture() {
  const scenario = new URLSearchParams(location.search).get('scenario');
  const teams = [{ id: 'amber', name: '琥珀团队' }, { id: 'blue', name: '蓝色团队' }];
  let team = scenario === 'assigned' ? null : teams[0];
  let wallet = '10.0000000000';
  let directoryWallet;
  let delayTeams = false;
  let failTeams = false;
  let releaseTeams;
  let delayedSignal;
  const requests = [];
  const realFetch = window.fetch;
  localStorage.setItem('auth_token', 'energy-fixture');
  localStorage.setItem('auth_username', 'founder');

  window.fetch = async (path, options) => {
    const url = new URL(path, location.href);
    if (!url.pathname.startsWith('/api/')) return realFetch(path, options);
    const resource = url.pathname.replace('/api/account-center/', '');
    const member = url.searchParams.get('member');
    requests.push({ resource, member, from: url.searchParams.get('from'), to: url.searchParams.get('to'), offset: url.searchParams.get('offset') });
    const filter = { teamId: team?.id, member, from: '2026-11-01T00:00:00+08:00', to: '2026-12-01T00:00:00+08:00', timeZone: 'Asia/Shanghai' };
    const offset = Number(url.searchParams.get('offset')) || 0;
    const limit = Number(url.searchParams.get('limit')) || 12;
    const page = { limit, offset, hasMore: offset === 0, nextOffset: offset === 0 ? limit : undefined };
    const summary = (entry = team, amberBalance = wallet) => ({
      team: entry,
      balance: entry ? { available: true, amount: entry.id === 'amber' ? amberBalance : '3.00' } : { available: false, reason: 'no_active_team' },
      consumption: entry && { actualCost: member ? entry.id === 'amber' ? '1.25' : '0.00' : '15.00',
        reconciledCount: member && entry.id === 'blue' ? 0 : 1, requestCount: 1, pendingCount: 0 }
    });
    switch (resource) {
    case '/api/login/session': return Response.json({ username: 'founder', canonicalRole: '开发创始', capabilities: [] });
    case '/api/account-impersonation/current': return Response.json({ actorUsername: 'founder', active: false, canSwitch: false });
    case 'overview': return Response.json({ ...summary(), filter, canViewAllTeams: true, subject: { username: 'founder', role: 'founder', teamId: team?.id } });
    case 'energy': return Response.json({ ...summary(), filter });
    case 'energy/ranking':
    case 'energy/ledger': return Response.json({ ...page, team, filter, entries: [] });
    case 'energy/teams': {
      const result = Response.json({ ...page, filter, teams: teams.map(entry => summary(entry, directoryWallet || wallet)) });
      if (failTeams) return Response.json({ error: '团队目录暂时失败' }, { status: 503 });
      if (delayTeams) {
        delayedSignal = options.signal;
        await new Promise(resolve => { releaseTeams = resolve; });
      }
      return result;
    }
    default: return Response.json({});
    }
  };

  const screen = () => document.querySelector('.energy-page')?.textContent || '';
  const label = value => value.replace(/\s/g, '');
  const button = value => [...document.querySelectorAll('button')].find(item => label(item.textContent) === label(value));
  const card = name => [...document.querySelectorAll('.energy-team')].find(item => item.textContent.includes(name))?.textContent;
  const metric = () => document.querySelector('.energy-metrics')?.textContent;
  const sleep = (milliseconds = 20) => new Promise(resolve => setTimeout(resolve, milliseconds));
  async function waitFor(check, message) {
    for (let i = 0; i < 80; i++) {
      if (check()) return;
      await sleep();
    }
    throw new Error(`${message} (${screen().slice(0, 420)})`);
  }
  function expect(condition, message) {
    if (!condition) throw new Error(message);
  }
  function fill(name, value) {
    const field = [...document.querySelectorAll('.energy-filter label')].find(item => item.textContent === name)?.querySelector('input');
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(field, value);
    field.dispatchEvent(new Event('input', { bubbles: true }));
    field.dispatchEvent(new Event('change', { bubbles: true }));
  }
  function requestedAll(since) {
    return ['overview', 'energy/teams', 'energy', 'energy/ledger', 'energy/ranking']
      .every(resource => requests.slice(since).some(item => item.resource === resource));
  }

  (async () => {
    await waitFor(() => button('刷新账单') && card('琥珀团队'), '初始目录未加载');
    if (scenario === 'assigned') {
      expect(screen().includes('当前账号没有所属团队'), '未分配团队的初始状态错误');
      team = teams[0];
      const start = requests.length;
      button('刷新账单').click();
      await waitFor(() => requestedAll(start) && metric()?.includes('10.0000000000 能量') && screen().includes('账单明细'), '刷新后未加载新分配团队');
    } else if (scenario === 'refresh') {
      await waitFor(() => metric()?.includes(wallet), '初始余额未加载');
      const start = requests.length;
      wallet = '22.0000000000';
      directoryWallet = '21.99';
      button('刷新账单').click();
      await waitFor(() => metric()?.includes(wallet) && card('琥珀团队')?.includes(wallet), '同团队的顶部和卡片金额没有同步刷新');
      expect(requestedAll(start), '刷新未重新校验全部接口');
      directoryWallet = undefined;

      // Complete an older team request after the newer refresh has rendered.
      delayTeams = true;
      wallet = '31.00';
      button('刷新账单').click();
      await waitFor(() => releaseTeams, '第一次刷新没有请求目录');
      expect(!screen().includes('22.0000000000') && !screen().includes('31.00'), '刷新进行中显示了旧余额或部分新余额');
      delayTeams = false;
      wallet = '44.00';
      button('刷新账单').click();
      await waitFor(() => metric()?.includes(wallet) && card('琥珀团队')?.includes(wallet), '第二次刷新未完整落地');
      expect(delayedSignal.aborted, '旧目录请求未取消');
      releaseTeams();
      await sleep(90);
      expect(!screen().includes('31.00') && card('琥珀团队')?.includes('44.00'), '旧响应覆盖了新刷新结果');

      failTeams = true;
      button('刷新账单').click();
      await waitFor(() => screen().includes('团队目录暂时失败'), '团队目录错误未清晰显示');
      expect(!screen().includes('44.00'), '目录失败后还把旧余额显示为有效值');
    } else {
      await waitFor(() => card('琥珀团队')?.includes('15.00') && screen().includes('账单明细'), '初始团队消耗未加载');
      document.querySelector('nav[aria-label="团队分页"] button:last-of-type').click();
      await waitFor(() => requests.some(item => item.resource === 'energy/teams' && item.offset === '12') && card('琥珀团队'), '团队第二页未加载');
      fill('成员账号', 'peer');
      fill('开始日期', '2026-11-02');
      fill('结束日期（不含当天）', '2026-11-27');
      document.querySelector('.energy-filter').requestSubmit();
      await waitFor(() => metric()?.includes('1.25 能量') && card('琥珀团队')?.includes('该成员已核账消耗 1.25 能量'), '团队卡没有使用已应用的成员筛选');
      expect(card('蓝色团队')?.includes('暂无已核账消耗') && !card('蓝色团队')?.includes('15.00'), '无该成员消耗的团队显示了团队总额');
      expect(card('琥珀团队')?.includes('10.0000000000') && card('蓝色团队')?.includes('3.00 能量') && !screen().includes('13.00 能量'), '筛选改写或合计了团队钱包');
      const lastDirectory = requests.filter(item => item.resource === 'energy/teams').at(-1);
      expect(lastDirectory.member === 'peer' && lastDirectory.from === '2026-11-02' && lastDirectory.to === '2026-11-27' && lastDirectory.offset === '0', '逐队请求未带完整筛选或未重置分页');
      button('重置筛选').click();
      await waitFor(() => card('琥珀团队')?.includes('团队已核账消耗 15.00 能量') && card('蓝色团队')?.includes('团队已核账消耗 15.00 能量'), '清除筛选后未恢复团队金额');
      expect(requests.filter(item => item.resource === 'energy/teams').at(-1).member === null, '重置后仍带成员筛选');
    }
    document.documentElement.dataset.energyResult = 'passed';
  })().catch(error => { document.documentElement.dataset.energyError = error.message; });
}

for (const scenario of ['refresh', 'assigned', 'member-filter']) {
  test(`production energy billing ${scenario}`, { skip: !dist || !browser }, async () => {
    const root = resolve(dist);
    const index = (await readFile(resolve(root, 'index.html'), 'utf8')).replace('<head>', `<head><script>(${fixture.toString()})()</script>`);
    const server = createServer(async (request, response) => {
      const { pathname } = new URL(request.url, 'http://127.0.0.1');
      if (pathname === '/usage') return response.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }).end(index);
      const file = resolve(root, `.${pathname}`);
      const mime = { '.js': 'text/javascript', '.css': 'text/css', '.png': 'image/png' }[extname(file)];
      if (!file.startsWith(root + sep) || !mime) return response.writeHead(404).end();
      try { response.writeHead(200, { 'Content-Type': mime }).end(await readFile(file)); }
      catch { response.end(); }
    });
    await new Promise(done => server.listen(0, '127.0.0.1', done));
    const profile = await mkdtemp(resolve(tmpdir(), 'v88-energy-prod-'));
    try {
      const { stdout, stderr } = await promisify(execFile)(browser, ['--headless=new', '--no-first-run', '--disable-gpu',
        '--disable-background-networking', '--no-proxy-server', '--host-resolver-rules=EXCLUDE 127.0.0.1, MAP * ~NOTFOUND',
        '--virtual-time-budget=18000', '--user-data-dir=' + profile, '--dump-dom',
        `http://127.0.0.1:${server.address().port}/usage?scenario=${scenario}`], { windowsHide: true, maxBuffer: 5_000_000, timeout: 50000 });
      assert.ok(stdout.includes('data-energy-result="passed"'), stdout.match(/data-energy-error="([^"]*)"/)?.[1] || stderr.slice(-1000) || 'Chrome scenario did not finish');
    } finally {
      server.close();
      await rm(profile, { recursive: true, force: true });
    }
  });
}
