import test from 'node:test';
import assert from 'node:assert/strict';

import { API_TIMEOUT_CODE, DEFAULT_API_TIMEOUT_MS, isApiTimeoutError, apiRequest, getToken, isSessionAuthFailure, setToken, setVerifiedImpersonation } from './client.js';

function responseWithHeader(value) {
  return {
    headers: {
      get(name) {
        return name.toLowerCase() === 'x-qiantie-auth-failure' ? value : null;
      }
    }
  };
}

test('recognizes the primary application session-expired response', () => {
  assert.equal(isSessionAuthFailure(responseWithHeader('session')), true);
});

test('does not treat downstream bridge 401 responses as session expiry', () => {
  assert.equal(isSessionAuthFailure(responseWithHeader(null)), false);
  assert.equal(isSessionAuthFailure(responseWithHeader('bridge')), false);
});

test('reads and clears the Go application session token from local storage', () => {
  const previousStorage = globalThis.localStorage;
  const values = new Map([['auth_token', 'go-session-token']]);
  globalThis.localStorage = {
    getItem(key) { return values.get(key) || null; },
    setItem(key, value) { values.set(key, value); },
    removeItem(key) { values.delete(key); }
  };
  try {
    assert.equal(getToken(), 'go-session-token');
    values.delete('auth_token');
    assert.equal(getToken(), '');
    setToken('go-session-token-2');
    assert.equal(getToken(), 'go-session-token-2');
    setToken('');
    assert.equal(getToken(), '');
  } finally {
    globalThis.localStorage = previousStorage;
  }
});

test('business writes use the tab-confirmed actor and explicit switch state even if another tab changes the token', async () => {
  const storage = new Map([['auth_token', 'actor-session'], ['expectedActor', 'spoofed-local-actor'], ['impersonationId', 'spoofed-local-id']]);
  const sent = [];
  const previousStorage = globalThis.localStorage;
  const previousFetch = globalThis.fetch;
  globalThis.localStorage = { getItem: key => storage.get(key) || null, setItem: (key, value) => storage.set(key, value), removeItem: key => storage.delete(key) };
  globalThis.fetch = (path, init) => {
    sent.push([path, init.headers.get('X-Qiantie-Expected-Actor'), init.headers.get('X-Qiantie-Expected-Impersonation'), init.headers.get('Authorization')]);
    return Promise.resolve(new Response('{}', { headers: { 'Content-Type': 'application/json' } }));
  };
  try {
    setVerifiedImpersonation({ actorUsername: 'actor', username: 'actor', impersonationId: 'none' }, 'actor-session');
    await apiRequest('/api/business', { method: 'POST', headers: { 'X-Qiantie-Expected-Actor': 'spoofed-actor', 'X-Qiantie-Expected-Impersonation': 'spoofed-id' } });
    assert.equal(setVerifiedImpersonation({ actorUsername: 'actor', username: 'subject', impersonationId: 'other-tab-switch' }), false);
    storage.set('auth_token', 'other-tab-session');
    assert.equal(setVerifiedImpersonation({ actorUsername: 'other-actor', username: 'other-actor', impersonationId: 'none' }), false);
    await apiRequest('/api/business', { method: 'PATCH' });
    storage.set('auth_token', 'actor-session');
    setVerifiedImpersonation(null);
    setVerifiedImpersonation({ actorUsername: 'actor', username: 'subject', impersonationId: 'verified-id' }, 'actor-session');
    await apiRequest('/api/business', { method: 'POST' });
    storage.set('auth_token', 'other-tab-session');
    await apiRequest('/api/business', { method: 'PATCH' });
    await apiRequest('/api/business');
    await apiRequest('/api/account-impersonation/current', { method: 'DELETE' });
    assert.deepEqual(sent, [
      ['/api/business', 'actor', 'none', 'Bearer actor-session'],
      ['/api/business', 'actor', 'none', 'Bearer other-tab-session'],
      ['/api/business', 'actor', 'verified-id', 'Bearer actor-session'],
      ['/api/business', 'actor', 'verified-id', 'Bearer other-tab-session'],
      ['/api/business', null, null, 'Bearer other-tab-session'],
      ['/api/account-impersonation/current', 'actor', 'verified-id', 'Bearer other-tab-session']
    ]);
  } finally {
    globalThis.localStorage = previousStorage;
    globalThis.fetch = previousFetch;
  }
});

test('apiRequest defaults to a bounded timeout so a hung request can never stay pending', async () => {
  const previousStorage = globalThis.localStorage;
  const previousFetch = globalThis.fetch;
  globalThis.localStorage = { getItem() { return ''; } };
  globalThis.fetch = (_path, init) => new Promise((_resolve, reject) => {
    init.signal.addEventListener('abort', () => reject(Object.assign(new Error('aborted'), { name: 'AbortError' })));
  });
  try {
    assert.equal(DEFAULT_API_TIMEOUT_MS, 60000);
    await assert.rejects(
      apiRequest('/api/hangs-forever', { timeoutMs: 20, silent: true, suppressGlobalError: true }),
      error => {
        assert.equal(error.code, API_TIMEOUT_CODE);
        assert.equal(error.name, 'TimeoutError');
        assert.equal(error.timeoutMs, 20);
        assert.equal(error.source, '/api/hangs-forever');
        assert.equal(isApiTimeoutError(error), true);
        return true;
      }
    );
  } finally {
    globalThis.localStorage = previousStorage;
    globalThis.fetch = previousFetch;
  }
});

test('apiRequest keeps caller cancellation separate from timeout errors', async () => {
  const previousStorage = globalThis.localStorage;
  const previousFetch = globalThis.fetch;
  globalThis.localStorage = { getItem() { return ''; } };
  globalThis.fetch = (_path, init) => new Promise((_resolve, reject) => {
    init.signal.addEventListener('abort', () => reject(Object.assign(new Error('aborted'), { name: 'AbortError' })));
  });
  const controller = new AbortController();
  try {
    const pending = apiRequest('/api/cancelled', { signal: controller.signal, timeoutMs: 0, silent: true, suppressGlobalError: true });
    controller.abort();
    await assert.rejects(pending, error => {
      assert.equal(isApiTimeoutError(error), false);
      assert.equal(error.name, 'AbortError');
      return true;
    });
  } finally {
    globalThis.localStorage = previousStorage;
    globalThis.fetch = previousFetch;
  }
});

test('apiRequest exposes the stable server error code next to the legacy message', async () => {
  const previousStorage = globalThis.localStorage;
  const previousFetch = globalThis.fetch;
  globalThis.localStorage = { getItem() { return ''; } };
  globalThis.fetch = async () => new Response(
    JSON.stringify({ error: '图片模型未配置', code: 'IMAGE_MODEL_NOT_CONFIGURED' }),
    { status: 503, headers: { 'Content-Type': 'application/json' } }
  );
  try {
    await assert.rejects(
      apiRequest('/api/agent/images', { silent: true }),
      error => {
        assert.equal(error.message, '图片模型未配置');
        assert.equal(error.code, 'IMAGE_MODEL_NOT_CONFIGURED');
        assert.equal(error.status, 503);
        return true;
      }
    );
  } finally {
    globalThis.localStorage = previousStorage;
    globalThis.fetch = previousFetch;
  }
});
