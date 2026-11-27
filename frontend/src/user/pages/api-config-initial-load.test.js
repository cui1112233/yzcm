import assert from 'node:assert/strict';
import test from 'node:test';
import { loadApiConfigPage, refreshApiConfigModels } from './api-config-initial-load.js';

const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};
const flush = () => new Promise(resolve => setImmediate(resolve));

function setup(overrides = {}) {
  const events = [];
  const config = { canManage: true, role: 'founder' };
  const dependencies = {
    getConfig: async () => config,
    getMemberCenter: async () => ({ member: { canonicalRole: 'founder' } }),
    canManage: value => value.canManage,
    listModels: async () => [{ id: 'configured-model', hasCredential: true }],
    getQuotas: async () => [],
    ...overrides
  };
  const callbacks = Object.fromEntries(['config', 'center', 'models', 'quotas', 'error', 'quotaError', 'quotaLoading', 'ready'].map(name => [name, (...args) => events.push([name, ...args])]));
  return { events, dependencies, callbacks };
}

test('valid role and models become ready while quota request is pending', async () => {
  const quota = deferred();
  const state = setup({ getQuotas: () => quota.promise });
  const ready = loadApiConfigPage(state.dependencies, state.callbacks);
  await flush();
  assert.equal(state.events.find(([name]) => name === 'config')[1].role, 'founder');
  assert.ok(state.events.some(([name]) => name === 'models'), 'a pending quota must not hide successful models');
  assert.equal(state.events.find(([name]) => name === 'models')[1][0].id, 'configured-model');
  assert.ok(state.events.some(([name]) => name === 'ready'));
  await ready;
  quota.reject(new Error('quota timeout'));
  await flush();
  assert.match(state.events.find(([name]) => name === 'quotaError')[1].message, /quota timeout/);
  assert.equal(state.events.filter(([name]) => name === 'models').length, 1);
});

test('configuration failure is explicit and never publishes fallback configuration or editable models', async () => {
  let reads = 0;
  const state = setup({ getConfig: async () => { throw new Error('configuration unavailable'); }, listModels: async () => { reads++; return []; } });
  await loadApiConfigPage(state.dependencies, state.callbacks);
  assert.equal(reads, 0);
  assert.equal(state.events.some(([name]) => name === 'config' || name === 'models'), false);
  assert.equal(state.events.find(([name]) => name === 'error')[1], 'config');
  assert.ok(state.events.some(([name]) => name === 'ready'));
});

test('slow member details do not delay authoritative config or models', async () => {
  const member = deferred();
  const state = setup({ getMemberCenter: () => member.promise });
  await loadApiConfigPage(state.dependencies, state.callbacks);
  assert.ok(state.events.some(([name]) => name === 'config'));
  assert.ok(state.events.some(([name]) => name === 'models'));
  assert.ok(state.events.some(([name]) => name === 'ready'));
  member.reject(new Error('member details unavailable'));
  await flush();
  assert.equal(state.events.find(([name]) => name === 'error')[1], 'member');
  assert.equal(state.events.filter(([name]) => name === 'config').length, 1);
});

test('model failure remains distinct from successful configuration and quota results', async () => {
  const state = setup({ listModels: async () => { throw new Error('catalog unavailable'); } });
  await loadApiConfigPage(state.dependencies, state.callbacks);
  assert.equal(state.events.find(([name]) => name === 'error')[1], 'models');
  assert.equal(state.events.some(([name]) => name === 'models'), false);
  assert.ok(state.events.some(([name]) => name === 'ready'));
});

test('managed accounts finish without requesting administrator models or quotas', async () => {
  const state = setup({ getConfig: async () => ({ canManage: false }), listModels: () => assert.fail('administrator models requested'), getQuotas: () => assert.fail('administrator quotas requested') });
  await loadApiConfigPage(state.dependencies, state.callbacks);
  assert.ok(state.events.some(([name]) => name === 'ready'));
  assert.equal(state.events.some(([name]) => name === 'error'), false);
});

test('post-save refresh publishes updated models and returns while quota remains pending', async () => {
  const quota = deferred();
  const state = setup({ getQuotas: () => quota.promise, listModels: async () => [{ id: 'saved-key-model', hasCredential: true }] });
  let returned = false;
  const refresh = refreshApiConfigModels(state.dependencies, state.callbacks).then(() => { returned = true; });
  await flush();
  assert.ok(state.events.some(([name]) => name === 'models'), 'saved models must refresh before quota finishes');
  assert.equal(state.events.find(([name]) => name === 'models')[1][0].hasCredential, true);
  assert.equal(returned, true, 'saving must finish without waiting for remote quota');
  await refresh;
  quota.reject(new Error('remote quota timeout'));
  await flush();
  assert.match(state.events.find(([name]) => name === 'quotaError')[1].message, /remote quota timeout/);
});

test('post-save model refresh failure rejects instead of claiming save completion', async () => {
  const state = setup({ listModels: async () => { throw new Error('updated catalog unavailable'); } });
  await assert.rejects(refreshApiConfigModels(state.dependencies, state.callbacks), /updated catalog unavailable/);
  assert.equal(state.events.some(([name]) => name === 'models'), false);
});
