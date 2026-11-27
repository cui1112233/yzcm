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

test('profile page opens as the approved inline editable card instead of a read-only panel and modal', { skip: !browser }, async t => {
  const bundle = await build({
    stdin: { resolveDir: path.dirname(fileURLToPath(import.meta.url)), loader: 'jsx', contents: `
      import React from 'react';
      import { createRoot } from 'react-dom/client';
      import ProfilePage from './ProfilePage.jsx';

      const member = {
        username: 'choushiyiguai', displayName: 'choushiyiguai', canonicalRole: 'founder',
        phone: '13800006720', email: 'owner@yzcm.com', teamTitle: '开发创始',
        bio: '专注于 YZCM 团队的 AI 能力建设与协作效率提升。', createdAt: '2024-05-14T02:27:00Z'
      };
      window.fetch = async input => {
        const url = new URL(input, 'http://localhost');
        if (url.pathname === '/api/member/me') return Response.json({ member, team: { name: '一战晟铭' } });
        if (url.pathname === '/api/member/security') return Response.json({ mfa: { enabled: true } });
        if (url.pathname === '/api/member/notifications') return Response.json({ unread: 0, entries: [] });
        if (url.pathname === '/api/account-recovery/email/status') return Response.json({ email: member.email, verified: true, deliveryConfigured: true });
        return Response.json({ error: 'unexpected request' }, { status: 404 });
      };

      createRoot(document.getElementById('root')).render(<ProfilePage />);
      const sleep = delay => new Promise(resolve => setTimeout(resolve, delay));
      (async () => {
        for (let i = 0; i < 50 && !document.querySelector('.pc-profile-inline-form'); i++) await sleep(40);
        const card = document.querySelector('.pc-profile-inline-form');
        if (!card) throw new Error('approved inline form is missing');
        const labels = [...card.querySelectorAll('label')].map(node => node.textContent.trim());
        for (const label of ['显示名称', '手机号码', '邮箱地址', '团队职务', '个人简介']) {
          if (!labels.includes(label)) throw new Error('missing inline field ' + label);
        }
        const actions = [...card.querySelectorAll('button')].map(node => node.textContent.replace(/\\s/g, ''));
        if (!actions.includes('保存资料') || !actions.includes('取消修改')) throw new Error('approved inline actions are missing');
        if (document.querySelector('.pg-edit-modal')) throw new Error('profile still depends on a modal editor');
        if (!document.querySelector('.pc-profile-name .role-energy-icon')) throw new Error('role emblem is not beside the profile name');
        document.body.dataset.result = 'passed';
      })().catch(error => { document.body.dataset.result = 'failed'; document.body.dataset.error = error.message; });
    ` },
    bundle: true, jsx: 'automatic', write: false, format: 'iife', platform: 'browser', loader: { '.css': 'empty', '.png': 'dataurl', '.webp': 'dataurl' }
  });
  const dir = mkdtempSync(path.join(tmpdir(), 'v88-profile-parity-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  writeFileSync(path.join(dir, 'test.js'), bundle.outputFiles[0].text);
  const html = path.join(dir, 'test.html');
  writeFileSync(html, '<meta charset="utf-8"><div id="root"></div><script src="test.js"></script>');
  const output = execFileSync(browser, ['--headless=new', '--disable-gpu', '--no-first-run', '--disable-background-networking',
    '--host-resolver-rules=MAP * ~NOTFOUND', '--virtual-time-budget=8000', '--user-data-dir=' + path.join(dir, 'profile'),
    '--dump-dom', pathToFileURL(html).href], { encoding: 'utf8', timeout: 60000, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 4000000 });
  assert.match(output, /data-result="passed"/, output.match(/data-error="([^"]*)"/)?.[1] || 'browser did not finish the profile parity scenario');
});
