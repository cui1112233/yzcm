import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { getAccountImpersonation, startAccountImpersonation, stopAccountImpersonation } from '../api/accountImpersonation.js';

async function loadLocalModule(file) {
  const result = await build({
    entryPoints: [fileURLToPath(new URL(file, import.meta.url))],
    platform: 'node', packages: 'external', format: 'cjs', bundle: true, write: false,
    jsx: 'automatic', loader: { '.css': 'empty' }
  });
  const exports = {};
  const module = { exports };
  new Function('require', 'module', 'exports', result.outputFiles[0].text)(createRequire(import.meta.url), module, exports);
  return module.exports;
}

test('reads, starts and stops the server-managed switch with the login session cookies', async () => {
  const requests = [];
  const active = { active: true, canSwitch: false, subject: { username: 'member' } };
  const inactive = { active: false, canSwitch: true };
  const request = async (path, options) => {
    requests.push([path, options]);
    return options.method === 'DELETE' ? inactive : active;
  };

  assert.deepEqual(await getAccountImpersonation(undefined, request), active);
  assert.deepEqual(await startAccountImpersonation('member', request), active);
  assert.deepEqual(await stopAccountImpersonation(request), inactive);
  assert.deepEqual(requests.map(([path, options]) => [path, options.method, options.credentials]), [
    ['/api/account-impersonation/current', 'GET', 'same-origin'],
    ['/api/account-impersonation', 'POST', 'same-origin'],
    ['/api/account-impersonation/current', 'DELETE', 'same-origin']
  ]);
  assert.deepEqual(JSON.parse(requests[1][1].body), { username: 'member' });
});

test('both personal-center layouts keep the active account and an exit control visible', async () => {
  const { AccountCenterLayout } = await loadLocalModule('./AccountCenterLayout.jsx');
  for (const pathname of ['/profile', '/security', '/usage']) {
    const markup = renderToStaticMarkup(createElement(AccountCenterLayout, {
      pathname, username: 'member', account: { role: 'member' },
      impersonation: { active: true, canSwitch: false, subject: { username: 'member' } }
    }));
    assert.match(markup, /正在以 member 操作/);
    assert.match(markup, /退出切换/);
    assert.doesNotMatch(markup, /切换账号/);
  }
});

test('the switch entry is controlled by server capability across personal-center pages', async () => {
  const { AccountCenterLayout } = await loadLocalModule('./AccountCenterLayout.jsx');
  for (const pathname of ['/profile', '/security']) {
    const render = canSwitch => renderToStaticMarkup(createElement(AccountCenterLayout, {
      pathname, username: 'someone', account: { role: 'dev' }, impersonation: { active: false, canSwitch }
    }));
    assert.match(render(true), /切换账号/);
    assert.doesNotMatch(render(false), /切换账号/);
  }
});

test('local drafts use the verified subject while the login session remains with the actor', async () => {
  const { getCurrentActorUsername, getCurrentUsername, setEffectiveImpersonationAccount } = await loadLocalModule('../api/auth.js');
  const fields = new Map([['auth_token', 'actor-session'], ['auth_username', 'actor']]);
  const oldStorage = globalThis.localStorage;
  globalThis.localStorage = { getItem: key => fields.get(key) || null };
  try {
    assert.equal(getCurrentUsername(), 'actor');
    setEffectiveImpersonationAccount('subject', 'actor-session');
    assert.equal(getCurrentUsername(), 'subject');
    assert.equal(getCurrentActorUsername(), 'actor');
    assert.equal(fields.get('auth_token'), 'actor-session');
    setEffectiveImpersonationAccount('');
    assert.equal(getCurrentUsername(), 'actor');
    fields.set('auth_token', 'new-session');
    setEffectiveImpersonationAccount('subject', 'actor-session');
    assert.equal(getCurrentUsername(), 'actor');
  } finally {
    if (oldStorage === undefined) delete globalThis.localStorage;
    else globalThis.localStorage = oldStorage;
  }
});
