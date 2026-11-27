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
  const initialParams = new URLSearchParams(location.search);
  const scenario = initialParams.get('scenario');
  const role = initialParams.get('role') === 'developer' ? 'developer' : 'founder';
  const canonicalRole = role === 'developer' ? '开发者' : '开发创始';
  const teams = [{ id: 'amber', name: '琥珀团队' }, { id: 'blue', name: '蓝色团队' }];
  let team = scenario === 'assigned' ? null : teams[0];
  let wallet = '10.0000000000';
  let directoryWallet;
  let delayTeams = false;
  let failTeams = false;
  let releaseTeams;
  let delayedSignal;
  let releaseAmber;
  let amberSignal;
  const requests = [];
  const realFetch = window.fetch;
  localStorage.setItem('auth_token', 'energy-fixture');
  localStorage.setItem('auth_username', role);

  window.fetch = async (path, options) => {
    const url = new URL(path, location.href);
    if (!url.pathname.startsWith('/api/')) return realFetch(path, options);
    const resource = url.pathname.replace('/api/account-center/', '');
    const member = url.searchParams.get('member');
    const teamId = url.searchParams.get('teamId');
    const target = teamId ? teams.find(entry => entry.id === teamId) : team;
    requests.push({ resource, teamId, member, from: url.searchParams.get('from'), to: url.searchParams.get('to'), offset: url.searchParams.get('offset') });
    const filter = { teamId: target?.id, member, from: '2026-11-01T00:00:00+08:00', to: '2026-12-01T00:00:00+08:00', timeZone: 'Asia/Shanghai' };
    const offset = Number(url.searchParams.get('offset')) || 0;
    const limit = Number(url.searchParams.get('limit')) || 12;
    const hasMore = offset === 0 && ['member-filter', 'missing-team', 'unlisted-team'].includes(scenario);
    const page = { limit, offset, hasMore, nextOffset: hasMore ? limit : undefined };
    function summary(entry = team, amberBalance = wallet) {
      const matchingMember = entry?.id === (scenario === 'cross-team' ? 'blue' : 'amber');
      return { team: entry,
        balance: entry ? { available: true, amount: entry.id === 'amber' ? amberBalance : '3.00' } : { available: false, reason: 'no_active_team' },
        consumption: entry && { actualCost: member ? matchingMember ? '1.25' : '0.00' : '15.00',
          reconciledCount: member && !matchingMember ? 0 : 1, requestCount: 1, pendingCount: 0 } };
    }
    if (['energy', 'energy/ranking', 'energy/ledger'].includes(resource)) {
      if (scenario === 'cross-team' && member && teamId === 'amber' && resource === 'energy/ledger') {
        amberSignal = options.signal;
        await new Promise(resolve => { releaseAmber = resolve; });
      }
      if (!target || scenario === 'cross-team' && member && teamId === 'amber') {
        return Response.json({ error: '无权访问所选团队' }, { status: 403 });
      }
    }
    switch (resource) {
    case '/api/login/session': return Response.json({ username: role, canonicalRole, capabilities: [] });
    case '/api/account-impersonation/current': return Response.json({ actorUsername: role, active: false, canSwitch: false });
    case 'overview': return Response.json({ ...summary(), filter, canViewAllTeams: true, subject: { username: role, role, teamId: team?.id } });
    case 'energy': return Response.json({ ...summary(target), filter });
    case 'energy/ranking':
    case 'energy/ledger': return Response.json({ ...page, team: target, filter, entries: [] });
    case 'energy/teams': {
      const matchingTeams = scenario === 'empty-filter' && member ? [] : scenario === 'unlisted-team' ? [teams[0]] : teams;
      const result = Response.json({ ...page, filter, teams: matchingTeams.map(entry => summary(entry, directoryWallet || wallet)) });
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
    await waitFor(() => button('刷新账单') && screen().includes(canonicalRole), '账单未显示正式身份');
    if (scenario === 'missing-team') {
      await waitFor(() => card('琥珀团队') && screen().includes('账单明细') && !new URLSearchParams(location.search).has('teamId'), '失效团队链接未回到我的团队');
      expect(new URLSearchParams(location.search).get('scenario') === 'missing-team' && location.hash === '#ledger', '修正团队地址时丢失了其他查询参数或锚点');
      expect(requests.some(item => item.resource === 'energy' && item.teamId === 'amber'), '失效地址未加载合法团队详情');
      document.documentElement.dataset.energyResult = 'passed';
      return;
    }
    if (scenario === 'unlisted-team') {
      await waitFor(() => card('琥珀团队') && metric()?.includes('3.00 能量') && screen().includes('账单明细'), '目录首屏之外的有效团队没有保留');
      expect(!card('蓝色团队') && new URLSearchParams(location.search).get('teamId') === 'blue', '将分页遗漏误当成失效团队链接');
      document.documentElement.dataset.energyResult = 'passed';
      return;
    }
    await waitFor(() => card('琥珀团队'), '初始目录未加载');
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
    } else if (scenario === 'cross-team') {
      await waitFor(() => screen().includes('账单明细'), '初始团队账单未加载');
      fill('成员账号', 'peer');
      document.querySelector('.energy-filter').requestSubmit();
      await waitFor(() => screen().includes('请从团队卡片切换团队') && card('蓝色团队')?.includes('该成员已核账消耗 1.25 能量') && releaseAmber, 'A 队拒绝后没有显示 B 队筛选结果和切换入口');
      expect(!screen().includes('账单明细') && !screen().includes('团队目录读取失败'), 'A 队权限错误没有与目录状态区分');
      button('查看 蓝色团队').click();
      await waitFor(() => new URLSearchParams(location.search).get('teamId') === 'blue' && metric()?.includes('1.25 能量') && screen().includes('账单明细'), '切换 B 队时未保留成员筛选或未同步地址');
      expect(amberSignal.aborted, '切换后未取消 A 队请求');
      expect(['energy', 'energy/ledger', 'energy/ranking'].every(resource => requests.some(item => item.resource === resource && item.teamId === 'blue' && item.member === 'peer')), '切换 B 队后详情端点缺少对应成员筛选');
      releaseAmber();
      await sleep(90);
      expect(metric()?.includes('1.25 能量') && !screen().includes('无权访问所选团队'), '晚到的 A 队失败覆盖了 B 队结果');
      button('重置筛选').click();
      await waitFor(() => !new URLSearchParams(location.search).has('teamId') && metric()?.includes('15.00 能量') && card('琥珀团队')?.includes('团队已核账消耗 15.00 能量'), '清除筛选后未回到 A 队并恢复全队金额');
    } else if (scenario === 'empty-filter') {
      await waitFor(() => screen().includes('账单明细'), '初始账单未加载');
      const start = requests.length;
      fill('成员账号', 'peer');
      document.querySelector('.energy-filter').requestSubmit();
      await waitFor(() => screen().includes('该筛选无可查看团队'), '筛选后的空目录没有明确提示');
      expect(!screen().includes('账单明细') && !screen().includes('读取失败'), '空目录误呈现详情或错误');
      expect(!requests.slice(start).some(item => item.resource === 'energy'), '空目录仍请求选中团队详情');
    } else if (scenario === 'directory-failure') {
      await waitFor(() => screen().includes('账单明细'), '初始账单未加载');
      failTeams = true;
      button('刷新账单').click();
      await waitFor(() => screen().includes('团队目录读取失败') && screen().includes('团队目录暂时失败'), '目录失败没有独立错误');
      expect(!screen().includes('账单明细') && !screen().includes('无权访问所选团队') && !metric(), '目录请求失败时显示了过期详情或权限错误');
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

const cases = [...['refresh', 'assigned', 'member-filter', 'unlisted-team'].map(scenario => ({ scenario, role: 'founder' })),
  ...['founder', 'developer'].flatMap(role => ['cross-team', 'missing-team', 'empty-filter', 'directory-failure'].map(scenario => ({ scenario, role })))];
for (const { scenario, role } of cases) {
  test(`production energy billing ${scenario} (${role})`, { skip: !dist || !browser }, async () => {
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
      const query = new URLSearchParams({ scenario, role });
      if (scenario === 'missing-team') query.set('teamId', 'missing');
      if (scenario === 'unlisted-team') query.set('teamId', 'blue');
      const { stdout, stderr } = await promisify(execFile)(browser, ['--headless=new', '--no-first-run', '--disable-gpu',
        '--disable-background-networking', '--no-proxy-server', '--host-resolver-rules=EXCLUDE 127.0.0.1, MAP * ~NOTFOUND',
        '--virtual-time-budget=18000', '--user-data-dir=' + profile, '--dump-dom',
        `http://127.0.0.1:${server.address().port}/usage?${query}#ledger`], { windowsHide: true, maxBuffer: 5_000_000, timeout: 50000 });
      assert.ok(stdout.includes('data-energy-result="passed"'), stdout.match(/data-energy-error="([^"]*)"/)?.[1] || stderr.slice(-1000) || 'Chrome scenario did not finish');
    } finally {
      server.close();
      await rm(profile, { recursive: true, force: true });
    }
  });
}
