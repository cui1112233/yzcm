import assert from 'node:assert/strict';
import fs from 'node:fs';
import { createRequire } from 'node:module';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { getAccountImpersonation, getAccountImpersonationTargets, startAccountImpersonation, stopAccountImpersonation } from '../api/accountImpersonation.js';

async function loadLocalModule(file, ...companions) {
  const result = await build({
    ...(companions.length ? { stdin: { contents: [file, ...companions].map(module => `export * from ${JSON.stringify(module)};`).join('\n'), resolveDir: fileURLToPath(new URL('.', import.meta.url)) } }
      : { entryPoints: [fileURLToPath(new URL(file, import.meta.url))] }),
    platform: 'node', packages: 'external', format: 'cjs', bundle: true, write: false,
    jsx: 'automatic', loader: { '.css': 'empty', '.png': 'dataurl', '.webp': 'dataurl' }
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
  const targets = { groups: [{ teamId: 'one', teamName: '一战晟铭', accounts: [{ username: 'member', displayName: '组员甲', role: 'member' }] }] };
  const request = async (path, options) => {
    requests.push([path, options]);
    if (path.endsWith('/targets')) return targets;
    return options.method === 'DELETE' ? inactive : active;
  };

  assert.deepEqual(await getAccountImpersonation(undefined, request), active);
  assert.deepEqual(await getAccountImpersonationTargets(request), targets);
  assert.deepEqual(await startAccountImpersonation('member', request), active);
  assert.deepEqual(await stopAccountImpersonation(request), inactive);
  assert.deepEqual(requests.map(([path, options]) => [path, options.method, options.credentials]), [
    ['/api/account-impersonation/current', 'GET', 'same-origin'],
    ['/api/account-impersonation/targets', 'GET', 'same-origin'],
    ['/api/account-impersonation', 'POST', 'same-origin'],
    ['/api/account-impersonation/current', 'DELETE', 'same-origin']
  ]);
  assert.equal(requests[1][1].cache, 'no-store');
  assert.deepEqual(JSON.parse(requests[2][1].body), { username: 'member' });
});

test('account switcher opens with authorized accounts, supports search, and requires confirmation', () => {
  const switchPath = fileURLToPath(new URL('./AccountSwitchMenu.jsx', import.meta.url));
  assert.equal(fs.existsSync(switchPath), true, 'AccountSwitchMenu.jsx must exist');
  const source = fs.readFileSync(switchPath, 'utf8');
  assert.match(source, /getAccountImpersonationTargets/);
  assert.match(source, /placeholder="搜索组员名称或用户名"/);
  assert.match(source, /displayName[\s\S]*username|username[\s\S]*displayName/);
  assert.match(source, /确认切换/);
  assert.match(source, /无法读取可切换账号/);
  assert.doesNotMatch(source, /输入目标账号用户名/);
});

test('successful start and exit block old-page writes and discard a late status response', async () => {
  const api = await loadLocalModule('../api/accountImpersonation.js', '../api/client.js', '../api/accountImpersonationState.js');
  const originalStorage = globalThis.localStorage;
  const originalFetch = globalThis.fetch;
  globalThis.localStorage = { getItem: key => key === 'auth_token' ? 'actor-session' : null };
  let releaseStatus;
  let pending;
  try {
    for (const action of ['start', 'stop']) {
      const wasActive = action === 'stop';
      const oldStatus = { actorUsername: 'actor', active: wasActive, canSwitch: !wasActive, ...(wasActive ? { impersonationId: 'switch-one', subject: { username: 'subject' } } : {}) };
      const expectation = { actorUsername: 'actor', username: oldStatus.subject?.username || 'actor', impersonationId: oldStatus.impersonationId || 'none' };
      const writes = [];
      globalThis.fetch = (path, options) => {
        const response = payload => new Response(JSON.stringify(payload), { headers: { 'Content-Type': 'application/json' } });
        if (options.method === 'GET') return new Promise(resolve => { releaseStatus = () => resolve(response(oldStatus)); });
        const headerNames = ['X-Qiantie-Expected-Actor', 'X-Qiantie-Expected-Impersonation'];
        writes.push([path, ...headerNames.map(header => options.headers.get(header))]);
        return Promise.resolve(response({ active: !wasActive }));
      };
      api.setVerifiedImpersonation(null);
      api.setVerifiedImpersonation(expectation);
      const oldGeneration = api.getPageIdentityGeneration();
      pending = api.getAccountImpersonation();
      if (wasActive) await api.stopAccountImpersonation();
      else await api.startAccountImpersonation('subject');

      const write = () => api.apiRequest('/api/business', { method: 'POST' });
      await assert.rejects(write(), { code: 'IMPERSONATION_UNCONFIRMED' });
      releaseStatus();
      const lateStatus = await pending;
      assert.equal(lateStatus.active, wasActive);
      assert.equal(api.setVerifiedImpersonation(expectation, 'actor-session', oldGeneration), false);
      assert.equal(api.setVerifiedImpersonation(expectation, 'actor-session', api.getPageIdentityGeneration()), false);
      await assert.rejects(write(), { code: 'IMPERSONATION_UNCONFIRMED' });
      assert.deepEqual(writes, [[wasActive ? '/api/account-impersonation/current' : '/api/account-impersonation', 'actor', expectation.impersonationId]]);
    }
  } finally {
    releaseStatus?.();
    await pending?.catch(() => {});
    if (originalStorage === undefined) delete globalThis.localStorage;
    else globalThis.localStorage = originalStorage;
    globalThis.fetch = originalFetch;
  }
});

test('personal-center business content waits for a confirmed status and has a retry path', async () => {
  const { AccountCenterLayout } = await loadLocalModule('./AccountCenterLayout.jsx');
  for (const pathname of ['/profile', '/security']) {
    const render = (impersonation, impersonationError) => renderToStaticMarkup(createElement(AccountCenterLayout, {
      pathname, username: 'actor', impersonation, impersonationError, onRetryImpersonation: () => {}
    }, createElement('button', null, '业务动作')));
    assert.match(render(null), /正在确认账号切换状态/);
    assert.doesNotMatch(render(null), /业务动作/);
    assert.match(render(null, '暂时失败'), /重试/);
    assert.doesNotMatch(render(null, '暂时失败'), /业务动作/);
    assert.match(render({ active: false, canSwitch: false }), /业务动作/);
  }
});

test('the management shell waits for switch confirmation before rendering protected content', async () => {
  const { AdminLayout } = await loadLocalModule('./AdminLayout.jsx');
  const previousStorage = globalThis.localStorage;
  const previousWindow = globalThis.window;
  globalThis.localStorage = { getItem: key => key === 'auth_token' ? 'actor-session' : null };
  globalThis.window = { location: { pathname: '/admin' } };
  try {
    const markup = renderToStaticMarkup(createElement(AdminLayout, null, createElement('button', null, '后台业务动作')));
    assert.match(markup, /正在确认账号切换状态/);
    assert.doesNotMatch(markup, /后台业务动作/);
  } finally {
    if (previousStorage === undefined) delete globalThis.localStorage;
    else globalThis.localStorage = previousStorage;
    if (previousWindow === undefined) delete globalThis.window;
    else globalThis.window = previousWindow;
  }
});

test('the streaming chat write carries the same verified expectation', async () => {
  const { streamAgentChatMessage, setVerifiedImpersonation } = await loadLocalModule('../api/agentChat.js', '../api/client.js');
  const previousStorage = globalThis.localStorage;
  const previousFetch = globalThis.fetch;
  globalThis.localStorage = { getItem: key => key === 'auth_token' ? 'actor-session' : null };
  let expectation;
  globalThis.fetch = async (_path, options) => {
    const headers = new Headers(options.headers);
    expectation = [headers.get('X-Qiantie-Expected-Actor'), headers.get('X-Qiantie-Expected-Impersonation')];
    return new Response('event: ready\ndata: {}\n\n');
  };
  try {
    setVerifiedImpersonation({ actorUsername: 'actor', username: 'subject', impersonationId: 'stream-switch' });
    await streamAgentChatMessage('session', { content: 'a test' });
    assert.deepEqual(expectation, ['actor', 'stream-switch']);
  } finally {
    if (previousStorage === undefined) delete globalThis.localStorage;
    else globalThis.localStorage = previousStorage;
    globalThis.fetch = previousFetch;
  }
});

test('same-tab login and logout clear the page identity until the server confirms again', async () => {
  const { login, logout, apiRequest, setVerifiedImpersonation } = await loadLocalModule('../api/auth.js', '../api/client.js');
  const fields = new Map([['auth_token', 'actor-a-session'], ['auth_username', 'actor-a']]);
  const requests = [];
  const originalStorage = globalThis.localStorage;
  const originalFetch = globalThis.fetch;
  globalThis.localStorage = { getItem: key => fields.get(key) || null, setItem: (key, value) => fields.set(key, value), removeItem: key => fields.delete(key) };
  globalThis.fetch = async (path, options) => {
    const headers = new Headers(options.headers);
    requests.push([path, headers.get('X-Qiantie-Expected-Actor'), headers.get('X-Qiantie-Expected-Impersonation')]);
    return new Response(path === '/api/login' ? JSON.stringify({ username: 'actor-b', token: 'actor-b-session' }) : '{}', {
      headers: { 'Content-Type': 'application/json' }
    });
  };
  try {
    const write = () => apiRequest('/api/business', { method: 'POST' });
    setVerifiedImpersonation({ actorUsername: 'actor-a', username: 'actor-a', impersonationId: 'none' });
    await write();
    await login('actor-b', 'fixture-password');
    await assert.rejects(write(), { code: 'IMPERSONATION_UNCONFIRMED' });
    setVerifiedImpersonation({ actorUsername: 'actor-b', username: 'actor-b', impersonationId: 'none' });
    await write();
    await logout();
    await assert.rejects(write(), { code: 'IMPERSONATION_UNCONFIRMED' });
    assert.deepEqual(requests, [
      ['/api/business', 'actor-a', 'none'],
      ['/api/login', null, null],
      ['/api/business', 'actor-b', 'none'],
      ['/api/login/logout', null, null]
    ]);
    assert.equal(fields.has('expectedActor'), false);
    assert.equal(fields.has('expectedImpersonation'), false);
  } finally {
    if (originalStorage === undefined) delete globalThis.localStorage;
    else globalThis.localStorage = originalStorage;
    globalThis.fetch = originalFetch;
  }
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
    setEffectiveImpersonationAccount({actorUsername: 'actor', username: 'subject', impersonationId: 'switch-1'}, 'actor-session');
    assert.equal(getCurrentUsername(), 'subject');
    assert.equal(getCurrentActorUsername(), 'actor');
    assert.equal(fields.get('auth_token'), 'actor-session');
    setEffectiveImpersonationAccount(null);
    assert.equal(getCurrentUsername(), 'actor');
    fields.set('auth_token', 'new-session');
    setEffectiveImpersonationAccount({actorUsername: 'actor', username: 'subject', impersonationId: 'switch-1'}, 'actor-session');
    assert.equal(getCurrentUsername(), 'actor');
  } finally {
    if (oldStorage === undefined) delete globalThis.localStorage;
    else globalThis.localStorage = oldStorage;
  }
});

test('retry confirmations stay separate for the actor and each verified switch subject', async () => {
  const { textRetryConfirmation, setEffectiveImpersonationAccount } = await loadLocalModule(
    '../../user/pages/shuihuo/batchFactoryNovelListHelpers.js', '../api/auth.js'
  );
  const fields = new Map([['auth_token', 'actor-session'], ['auth_username', 'actor'], ['impersonationSubject', 'forged']]);
  const previousStorage = globalThis.localStorage;
  const previousWindow = globalThis.window;
  globalThis.localStorage = { getItem: key => fields.get(key) || null };
  globalThis.window = { localStorage: globalThis.localStorage, sessionStorage: Object.create(null) };
  try {
    const confirmation = () => textRetryConfirmation('batch', 'book', 'collection');
    const actorKey = await confirmation().authorize(() => true);
    setEffectiveImpersonationAccount({actorUsername: 'actor', username: 'member-one', impersonationId: 'switch-1'});
    assert.equal(confirmation().read(), undefined);
    const firstKey = await confirmation().authorize(() => true);
    assert.notEqual(firstKey, actorKey);
    setEffectiveImpersonationAccount(null);
    setEffectiveImpersonationAccount({actorUsername: 'actor', username: 'member-two', impersonationId: 'switch-2'});
    assert.equal(confirmation().read(), undefined);
    const secondKey = await confirmation().authorize(() => true);
    assert.notEqual(secondKey, firstKey);
    setEffectiveImpersonationAccount(null);
    assert.equal(confirmation().read(), actorKey);
    setEffectiveImpersonationAccount({actorUsername: 'actor', username: 'member-one', impersonationId: 'switch-3'});
    assert.equal(confirmation().read(), firstKey);
  } finally {
    if (previousStorage === undefined) delete globalThis.localStorage;
    else globalThis.localStorage = previousStorage;
    if (previousWindow === undefined) delete globalThis.window;
    else globalThis.window = previousWindow;
  }
});
