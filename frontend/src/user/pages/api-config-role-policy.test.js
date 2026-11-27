import assert from 'node:assert/strict';
import test from 'node:test';

import { loadApiConfigPage } from './api-config-initial-load.js';
import { projectApiConfigCapabilities } from './api-config-role-policy.js';

test('projects only complete server capability sets into full or restricted modes', () => {
  assert.deepEqual(projectApiConfigCapabilities({
    canConfigureAPI: true,
    canAddCustomModel: true,
    canEditAddress: true,
    catalogSource: 'full'
  }), {
    mode: 'full',
    canConfigureAPI: true,
    canAddCustomModel: true,
    canEditAddress: true,
    catalogSource: 'full'
  });
  assert.deepEqual(projectApiConfigCapabilities({
    canConfigureAPI: true,
    canAddCustomModel: false,
    canEditAddress: false,
    catalogSource: 'yzcm_saved'
  }), {
    mode: 'restricted',
    canConfigureAPI: true,
    canAddCustomModel: false,
    canEditAddress: false,
    catalogSource: 'yzcm_saved'
  });
});

test('fails closed for missing, contradictory, role-only and legacy-only claims', () => {
  const invalid = [
    null,
    {},
    { canManageApi: true },
    { canonicalRole: '开发创始' },
    { canConfigureAPI: true, catalogSource: 'full' },
    { canConfigureAPI: true, canAddCustomModel: true, canEditAddress: false, catalogSource: 'full' },
    { canConfigureAPI: true, canAddCustomModel: true, canEditAddress: false, catalogSource: 'yzcm_saved' },
    { canConfigureAPI: true, canAddCustomModel: false, canEditAddress: false, catalogSource: 'unknown' }
  ];
  for (const value of invalid) assert.equal(projectApiConfigCapabilities(value).mode, 'none');
  assert.equal(projectApiConfigCapabilities({ canConfigureAPI: false, canAddCustomModel: true, canEditAddress: true, catalogSource: 'full' }).mode, 'none');
});

test('member mode finishes initial load without requesting models or quotas', async () => {
  const calls = [];
  const config = { canConfigureAPI: false, canAddCustomModel: false, canEditAddress: false, catalogSource: 'none' };
  await loadApiConfigPage({
    getConfig: async () => { calls.push('config'); return config; },
    getMemberCenter: async () => { calls.push('member'); return { member: { canonicalRole: '组员' } }; },
    canManage: value => projectApiConfigCapabilities(value).mode !== 'none',
    listModels: async () => { calls.push('models'); return []; },
    getQuotas: async () => { calls.push('quotas'); return []; }
  }, {
    config() {}, center() {}, models() {}, quotas() {}, quotaError() {}, quotaLoading() {}, error() {}, ready() {}
  });
  await Promise.resolve();
  assert.deepEqual(calls.sort(), ['config', 'member']);
});
