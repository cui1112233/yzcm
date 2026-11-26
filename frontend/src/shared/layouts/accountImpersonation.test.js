import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { getAccountImpersonation, startAccountImpersonation, stopAccountImpersonation } from '../api/accountImpersonation.js';

async function loadLocalModule(file, companion) {
  const result = await build({
    ...(companion ? { stdin: { contents: `export * from ${JSON.stringify(file)};\nexport * from ${JSON.stringify(companion)};`, resolveDir: fileURLToPath(new URL('.', import.meta.url)) } }
      : { entryPoints: [fileURLToPath(new URL(file, import.meta.url))] }),
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
  let header;
  globalThis.fetch = async (_path, options) => {
    header = new Headers(options.headers).get('X-Qiantie-Expected-Impersonation');
    return new Response('event: ready\ndata: {}\n\n');
  };
  try {
    setVerifiedImpersonation({ username: 'subject', impersonationId: 'stream-switch' });
    await streamAgentChatMessage('session', { content: 'a test' });
    assert.equal(header, 'stream-switch');
  } finally {
    if (previousStorage === undefined) delete globalThis.localStorage;
    else globalThis.localStorage = previousStorage;
    globalThis.fetch = previousFetch;
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
    setEffectiveImpersonationAccount({username: 'subject', impersonationId: 'switch-1'}, 'actor-session');
    assert.equal(getCurrentUsername(), 'subject');
    assert.equal(getCurrentActorUsername(), 'actor');
    assert.equal(fields.get('auth_token'), 'actor-session');
    setEffectiveImpersonationAccount(null);
    assert.equal(getCurrentUsername(), 'actor');
    fields.set('auth_token', 'new-session');
    setEffectiveImpersonationAccount({username: 'subject', impersonationId: 'switch-1'}, 'actor-session');
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
    setEffectiveImpersonationAccount({username: 'member-one', impersonationId: 'switch-1'});
    assert.equal(confirmation().read(), undefined);
    const firstKey = await confirmation().authorize(() => true);
    assert.notEqual(firstKey, actorKey);
    setEffectiveImpersonationAccount({username: 'member-two', impersonationId: 'switch-2'});
    assert.equal(confirmation().read(), undefined);
    const secondKey = await confirmation().authorize(() => true);
    assert.notEqual(secondKey, firstKey);
    setEffectiveImpersonationAccount(null);
    assert.equal(confirmation().read(), actorKey);
    setEffectiveImpersonationAccount({username: 'member-one', impersonationId: 'switch-3'});
    assert.equal(confirmation().read(), firstKey);
  } finally {
    if (previousStorage === undefined) delete globalThis.localStorage;
    else globalThis.localStorage = previousStorage;
    if (previousWindow === undefined) delete globalThis.window;
    else globalThis.window = previousWindow;
  }
});
