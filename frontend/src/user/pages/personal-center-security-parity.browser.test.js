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

test('security page includes the approved compact account identity strip', { skip: !browser }, async t => {
  const bundle = await build({
    stdin: { resolveDir: path.dirname(fileURLToPath(import.meta.url)), loader: 'jsx', contents: `
      import React from 'react';
      import { createRoot } from 'react-dom/client';
      import SecurityPage from './SecurityPage.jsx';
      const member = { username: 'choushiyiguai', displayName: 'choushiyiguai', canonicalRole: '开发创始' };
      window.fetch = async input => {
        const url = new URL(input, 'http://localhost');
        if (url.pathname === '/api/member/security') return Response.json({ mfa: { enabled: true }, sessions: [], persistentSessions: [], otherSessionCount: 0 });
        if (url.pathname === '/api/account-recovery/passkeys') return Response.json({ passkeys: [] });
        if (url.pathname === '/api/account-recovery/email/status') return Response.json({ email: 'owner@yzcm.com', verified: true });
        if (url.pathname === '/api/member/me') return Response.json({ member, team: { name: '一战晟铭' } });
        if (url.pathname === '/api/member/notifications') return Response.json({ unread: 0, entries: [] });
        return Response.json({ error: 'unexpected request' }, { status: 404 });
      };
      createRoot(document.getElementById('root')).render(<SecurityPage />);
      const sleep = delay => new Promise(resolve => setTimeout(resolve, delay));
      (async () => {
        for (let i = 0; i < 50 && !document.querySelector('.pc-security-identity'); i++) await sleep(40);
        const strip = document.querySelector('.pc-security-identity');
        if (!strip) throw new Error('security identity strip is missing');
        if (!strip.textContent.includes('choushiyiguai') || !strip.textContent.includes('开发创始')) throw new Error('security identity is incomplete');
        if (!strip.querySelector('.role-energy-icon')) throw new Error('security role emblem is missing');
        document.body.dataset.result = 'passed';
      })().catch(error => { document.body.dataset.result = 'failed'; document.body.dataset.error = error.message; });
    ` }, bundle: true, jsx: 'automatic', write: false, format: 'iife', platform: 'browser', loader: { '.css': 'empty', '.png': 'dataurl', '.webp': 'dataurl' }
  });
  const dir = mkdtempSync(path.join(tmpdir(), 'v88-security-parity-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  writeFileSync(path.join(dir, 'test.js'), bundle.outputFiles[0].text);
  const html = path.join(dir, 'test.html');
  writeFileSync(html, '<meta charset="utf-8"><div id="root"></div><script src="test.js"></script>');
  const output = execFileSync(browser, ['--headless=new', '--disable-gpu', '--no-first-run', '--disable-background-networking',
    '--host-resolver-rules=MAP * ~NOTFOUND', '--virtual-time-budget=8000', '--user-data-dir=' + path.join(dir, 'profile'),
    '--dump-dom', pathToFileURL(html).href], { encoding: 'utf8', timeout: 60000, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 4000000 });
  assert.match(output, /data-result="passed"/, output.match(/data-error="([^"]*)"/)?.[1] || 'browser did not finish the security parity scenario');
});
