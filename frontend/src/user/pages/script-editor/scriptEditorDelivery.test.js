import assert from 'node:assert/strict';
import test from 'node:test';
import { validateDelivery } from './scriptEditorDelivery.js';

test('缺少正式版时阻止交付并列出集数', () => {
  assert.deepEqual(validateDelivery([{ episode: 1, preferred: true }, { episode: 2, preferred: false }]), { ok: false, missing: [2] });
});

test('每集都有正式版时允许导出', () => {
  assert.deepEqual(validateDelivery([{ episode: 1, preferred: true }, { episode: 1, preferred: false }, { episode: 2, preferred: true }]), { ok: true, missing: [] });
});
