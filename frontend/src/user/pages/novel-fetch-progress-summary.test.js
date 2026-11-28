import test from 'node:test';
import assert from 'node:assert/strict';
import { summarizeWorkshopProgress } from './novel-fetch-progress-summary.js';

const row = (versions = {}, selected = ['ai2']) => ({
  aiStatus: 'done', status: 'waiting_classification', versions,
  meta: { processSnapshot: { selected_versions: selected } }
});
test('old AI1 cannot complete the requested missing AI2', () => {
  assert.equal(summarizeWorkshopProgress([row({ ai1: '旧正文' })]).aiDone, 0);
});
test('only all requested ready AI versions complete this round', () => {
  assert.equal(summarizeWorkshopProgress([row({ ai1: '旧正文', ai2: { state: 'ready' } })]).aiDone, 1);
  assert.equal(summarizeWorkshopProgress([row({ ai2: { state: 'failed', text: '残留' } })]).aiDone, 0);
  assert.equal(summarizeWorkshopProgress([row({ ai2: '正文' }, ['ai2', 'ai3'])]).aiDone, 0);
});
test('explicit empty selection and original-only rounds require no AI', () => {
  const result = summarizeWorkshopProgress([row({ ai1: '旧正文' }, []), row({}, ['original'])]);
  assert.equal(result.aiDone, 0);
  assert.equal(result.aiRequired, 0);
});
test('durable body references count but original and unsupported versions never count', () => {
  const item = row({ original_raw: { state: 'ready' } });
  item.bodyRefs = { ai2: { state: 'ready', charCount: 50 } };
  assert.equal(summarizeWorkshopProgress([item]).aiDone, 1);
  assert.equal(summarizeWorkshopProgress([{ ...item, versions: { ai2: '旧内联正文' }, bodyRefs: { ai2: { state: 'releasable', charCount: 50 } } }]).aiDone, 0);
  assert.equal(summarizeWorkshopProgress([row({ ai6: '正文' }, ['ai6'])]).aiRequired, 0);
});
test('historical rows retain their existing ready AI semantics', () => {
  assert.equal(summarizeWorkshopProgress([{ aiStatus: 'done', versions: { ai1: '正文' } }]).aiDone, 1);
  assert.equal(summarizeWorkshopProgress([{ aiStatus: 'done', versions: { original: '原文' } }]).aiDone, 0);
  assert.equal(summarizeWorkshopProgress([{ aiStatus: 'done', versions: {}, ai_files: '旧字段不是数组' }]).aiDone, 0);
});

test('round failure counts exclude stale AI and upload failures', () => {
  const originalOnly = { ...row({}, []), status: 'completed', aiStatus: 'failed', siteSubmitStatus: 'failed' };
  originalOnly.meta.processSnapshot.auto_submit = false;
  assert.equal(summarizeWorkshopProgress([originalOnly]).failed, 0);
  const readyAI2 = { ...row({ ai1: { state: 'failed' }, ai2: '新正文' }), status: 'completed', aiStatus: 'failed' };
  assert.equal(summarizeWorkshopProgress([readyAI2]).failed, 0);
  assert.equal(summarizeWorkshopProgress([{ ...readyAI2, status: 'failed' }]).failed, 1);
  assert.equal(summarizeWorkshopProgress([{ ...readyAI2, versions: { ai2: { state: 'failed' } } }]).failed, 1);
  assert.equal(summarizeWorkshopProgress([{ ...originalOnly, originalStatus: 'failed' }]).failed, 1);
  originalOnly.meta.processSnapshot.auto_submit = true;
  assert.equal(summarizeWorkshopProgress([originalOnly]).failed, 1);
  assert.equal(summarizeWorkshopProgress([{ aiStatus: 'failed' }]).failed, 1);
});
