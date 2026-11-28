import test from 'node:test';
import assert from 'node:assert/strict';
import { novelFetchAIFormPatch, novelFetchReadbackLabel } from './novel-fetch-ai-form.js';

test('both temperature controls update the same effective rewrite value', () => {
  assert.equal(novelFetchAIFormPatch({ ai: { temperature: 0.2 } }, { rewrite: { anchor_line_count: 5 } }).rewrite.temperature, 0.2);
  assert.equal(novelFetchAIFormPatch({ rewrite: { temperature: 0.6 } }, { ai: { top_p: 0.9 } }).ai.temperature, 0.6);
});
test('thinking controls explicitly disable their opposite without losing other fields', () => {
  const patch = novelFetchAIFormPatch({ ai: { disable_thinking: true } }, { ai: { enable_thinking: true, max_tokens: 8192 } });
  assert.equal(patch.ai.enable_thinking, false);
  assert.equal(patch.ai.max_tokens, 8192);
});
test('upload acceptance never claims remote content verification', () => {
  assert.equal(novelFetchReadbackLabel({ status: 'submitted', verified: false, readback_status: 'pending' }), '已接收，待核对');
  assert.equal(novelFetchReadbackLabel({ status: 'submitted', verified: true, readback_status: 'confirmed' }), '列表已确认');
  assert.equal(novelFetchReadbackLabel({ content_verified: true }), '正文已核对');
  assert.equal(novelFetchReadbackLabel({ status: 'skipped', reason: 'no_body' }), '待核对');
});
