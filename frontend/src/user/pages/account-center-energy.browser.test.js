import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'esbuild';

const browser = [process.env.CHROME_PATH, 'C:/Program Files/Google/Chrome/Application/chrome.exe',
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', '/usr/bin/chromium'].find(p => p && existsSync(p));

test('account pages render scoped energy, preserve amount precision, and export only the selected ledger page', { skip: !browser }, async t => {
  const bundle = await build({
    stdin: { resolveDir: path.dirname(fileURLToPath(import.meta.url)), loader: 'jsx', contents: `
      import React from 'react';
      import { createRoot } from 'react-dom/client';
      import Overview from './AccountOverviewPage.jsx';
      import Billing from './EnergyBillingPage.jsx';

      const amber = { id: 'amber', name: '琥珀团队', archivedAt: null };
      const blue = { id: 'blue', name: '蓝色团队', archivedAt: null };
      const amount = '9007199254740993.12345678';
      const subject = { username: 'me', displayName: '测试身份', role: 'member', teamId: 'amber' };
      const ownTime = '2026-11-01T12:03:00Z';
      const filter = { from: '2026-10-31T16:00:00Z', to: '2026-11-30T16:00:00Z', timeZone: 'Asia/Shanghai' };
      const unavailable = { available: false, amount: null, reason: 'wallet_unavailable' };
      const reconciled = { actualCost: amount, requestCount: 12, reconciledCount: 11, pendingCount: 1 };
      const pending = { actualCost: '0.00000000', requestCount: 1, reconciledCount: 0, pendingCount: 1 };
      const requests = [];
      let mode = 'normal';
      let rechargeUrl = '';
      let downloaded = '';
      const anchorClick = HTMLAnchorElement.prototype.click;
      HTMLAnchorElement.prototype.click = function () {
        if (this.href.startsWith('blob:')) downloaded = this.download;
        else anchorClick.call(this);
      };
      window.fetch = async path => {
        const url = new URL(path, 'http://localhost');
        requests.push(url);
        const params = url.searchParams;
        const offset = Number(params.get('offset') || 0);
        const limit = Number(params.get('limit') || 20);
        const team = params.get('teamId') === 'blue' ? blue : amber;
        const canViewAllTeams = ['founder', 'developer'].includes(subject.role);
        const balance = team === blue ? { available: true, amount: '4.20', updatedAt: ownTime } :
          mode === 'stale' ? { ...unavailable, reason: 'wallet_stale', updatedAt: ownTime } : unavailable;
        const summary = { team, filter, balance, consumption: mode === 'pending' ? pending : reconciled, rechargeUrl };
        const page = { limit, offset, hasMore: offset === 0, ...(offset === 0 ? { nextOffset: limit } : {}) };
        switch (url.pathname) {
        case '/api/account-center/overview':
          return Response.json({ ...summary, subject, canViewAllTeams });
        case '/api/account-center/energy':
          if (mode === 'failure') return Response.json({ error: '账单服务暂时不可用' }, { status: 503 });
          return Response.json(summary);
        case '/api/account-center/energy/teams':
          return Response.json({ ...page, filter, teams: [
            { team: amber, balance: { available: true, amount: '6.30', updatedAt: ownTime }, consumption: reconciled },
            { team: blue, balance: { available: true, amount: '4.20', updatedAt: ownTime }, consumption: reconciled }
          ] });
        case '/api/account-center/energy/ranking':
          return Response.json({ ...page, team, filter,
            entries: mode === 'pending' ? [] : [{ username: '可见排行同伴', rank: 1, ...reconciled }],
            emptyReason: mode === 'pending' ? 'no_reconciled_consumption' : undefined });
        case '/api/account-center/energy/ledger':
          if (params.get('format') === 'csv') return new Response('actualCost\\n' + amount, { headers: {
            'content-type': 'text/csv; charset=utf-8', 'X-Energy-Page-Limit': String(limit), 'X-Energy-Page-Offset': String(offset),
            'X-Energy-Has-More': String(page.hasMore)
          } });
          return Response.json({ ...page, team, filter, entries: [
            { at: ownTime, owner: 'me', actorUsername: '本人操作者不外显', feature: 'novel-creation', model: offset ? '第二页模型' : '我的模型',
              requestId: 'one', status: mode === 'pending' ? 'pending' : 'reconciled', actualCost: mode === 'pending' ? null : amount },
            { at: ownTime, owner: '同伴明细不可见', actorUsername: '其他操作者', feature: 'image', model: '他人模型',
              requestId: 'two', status: 'reconciled', actualCost: '123.40' }
          ] });
        default: return Response.json({ error: '意外请求 ' + path }, { status: 404 });
        }
      };

      const root = createRoot(document.getElementById('root'));
      const sleep = delay => new Promise(resolve => setTimeout(resolve, delay));
      const text = () => document.getElementById('root').textContent;
      const button = label => [...document.querySelectorAll('button')].find(node => node.textContent.replace(/\\s/g, '') === label.replace(/\\s/g, ''));
      const check = (value, message) => { if (!value) throw Error(message); };
      async function waitFor(predicate, message) {
        for (let attempt = 0; attempt < 30; attempt++) {
          if (predicate()) return;
          await sleep(40);
        }
        throw Error(message + ' [' + text().slice(0, 400) + ']');
      }
      function fill(label, value) {
        const input = [...document.querySelectorAll('label')].find(node => node.textContent === label).querySelector('input');
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, value);
        input.dispatchEvent(new Event('input', { bubbles: true }));
        input.dispatchEvent(new Event('change', { bubbles: true }));
      }

      (async () => {
        for (const [role, label] of [['founder', '开发创始'], ['developer', '开发者'], ['manager', '管理者'], ['member', '组员']]) {
          subject.role = role;
          const start = requests.length;
          root.render(<Overview key={role} />);
          await waitFor(() => text().includes(label) && text().includes('可见排行同伴'), label + '未见同队排行');
          check(text().includes('琥珀团队') && text().includes('暂无法获取') && text().includes('1 笔'), '总览漏掉团队或待核账状态');
          check(button('前往 YZCM API 补充能量')?.disabled, '无 URL 时仍可补充能量');
          if (['founder', 'developer'].includes(role)) {
            await waitFor(() => button('查看 蓝色团队'), label + '缺少团队切换');
            check(text().includes('6.30 能量') && text().includes('4.20 能量') && !text().includes('10.50 能量'), '余额未按团队独立展示');
            button('查看 蓝色团队').click();
            await waitFor(() => requests.slice(start).some(u => u.pathname.endsWith('/ranking') && u.searchParams.get('teamId') === 'blue') &&
              [...document.querySelectorAll('a')].some(a => a.getAttribute('href') === '/usage?teamId=blue'), '团队选中后排行和账单链接范围不一致');
          } else {
            check(!requests.slice(start).some(u => u.pathname.endsWith('/teams')), label + '访问了团队目录');
            check(!text().includes('蓝色团队'), label + '看到其他团队');
          }
        }

        rechargeUrl = 'http://newapi.yzcm.site/';
        root.render(<Billing key="member-billing" />);
        await waitFor(() => text().includes('账单明细') && text().includes(amount), '账单丢掉精确小数');
        check(text().includes('组员') && text().includes('可见排行同伴'), '账单漏掉身份或排行');
        check(!text().includes('同伴明细不可见') && !text().includes('实际操作者'), '组员账单泄露他人记录或操作者');
        check([...document.querySelectorAll('a')].some(a => a.href === rechargeUrl), '可信补充能量链接未显示');

        fill('开始日期', '2026-11-02');
        fill('结束日期（不含当天）', '2026-11-28');
        fill('成员账号', 'peer');
        document.querySelector('form').requestSubmit();
        await waitFor(() => requests.some(u => u.pathname.endsWith('/ranking') && u.searchParams.get('member') === 'peer') && button('导出当前页 CSV'), '筛选未传入查询');
        for (const suffix of ['/energy', '/ranking', '/ledger']) {
          const request = requests.filter(u => u.pathname.endsWith(suffix)).at(-1);
          check(request.searchParams.get('from') === '2026-11-02' && request.searchParams.get('to') === '2026-11-28', '汇总、排行和账单筛选日期不一致');
          check(request.searchParams.get('member') === (suffix === '/ledger' ? 'me' : 'peer'), '筛选越过组员明细范围');
        }
        document.querySelector('nav[aria-label="账单分页"] button:last-of-type').click();
        await waitFor(() => text().includes('第二页模型'), '下一页未请求当前筛选范围');
        button('导出当前页 CSV').click();
        await waitFor(() => text().includes('已导出第 2 页') && downloaded === 'energy-ledger-page-2.csv', 'CSV 未按返回页信息提示');
        const csvRequest = requests.at(-1);
        check(csvRequest.searchParams.get('offset') === '20' && csvRequest.searchParams.get('format') === 'csv' &&
          csvRequest.searchParams.get('member') === 'me' && csvRequest.searchParams.get('from') === '2026-11-02', 'CSV 未使用与账单相同的页和过滤条件');

        mode = 'pending';
        button('重置筛选').click();
        await waitFor(() => text().includes('当前时间范围暂无已核账消费排行') && text().includes('账单明细'), '待核账被充作排行');
        check(!text().includes('0.00000000 能量') && text().includes('待核账'), '待核账被展示为实际零费用');
        mode = 'failure';
        button('刷新账单').click();
        await waitFor(() => text().includes('账单服务暂时不可用') && button('重试'), '错误没有独立提示');
        mode = 'stale';
        button('重试').click();
        await waitFor(() => text().includes('YZCM 余额数据已过期') && text().includes('余额更新时间 2026/11/01 20:03'), '过期余额缺少状态或最后更新');

        subject.role = 'manager';
        root.render(<Billing key="manager-billing" />);
        await waitFor(() => text().includes('管理者') && text().includes('其他操作者'), '管理者看不到授权团队账单或实际操作者');
        document.body.dataset.result = 'passed';
      })().catch(error => { document.body.dataset.result = 'failed'; document.body.dataset.error = error.message; });
    ` },
    bundle: true, jsx: 'automatic', write: false, format: 'iife', platform: 'browser', loader: { '.css': 'empty', '.png': 'dataurl', '.webp': 'dataurl' }
  });
  const dir = mkdtempSync(path.join(tmpdir(), 'v88-energy-page-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  writeFileSync(path.join(dir, 'test.js'), bundle.outputFiles[0].text);
  const html = path.join(dir, 'test.html');
  writeFileSync(html, '<meta charset="utf-8"><div id="root"></div><script src="test.js"></script>');
  const output = execFileSync(browser, ['--headless=new', '--disable-gpu', '--no-first-run', '--disable-background-networking',
    '--host-resolver-rules=MAP * ~NOTFOUND', '--virtual-time-budget=16000', '--user-data-dir=' + path.join(dir, 'profile'),
    '--dump-dom', pathToFileURL(html).href], { encoding: 'utf8', timeout: 60000, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 4000000 });
  assert.ok(/data-result="passed"/.test(output), output.match(/data-error="([^"]*)"/)?.[1] || 'browser did not finish the scenario');
});
