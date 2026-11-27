import test from 'node:test';
import assert from 'node:assert/strict';
import { prepareCreationSources } from './batchFactoryCreationSources.js';

const groups = [{ platformId: '15', platformName: '书城', inputText: '738579 一本书\n738580 另一本', sourceTextByBookId: { '738579': { data: '已有正文', rawData: '完整原文' } }, sourceMetadataByBookId: { '738579': { gender: '女频' } } }];

test('automatic creation preserves known sources and never waits for missing originals', async () => {
  let calls = 0;
  const result = await prepareCreationSources({ groups, deferSources: true }, () => { calls++; return new Promise(() => {}); });
  assert.equal(calls, 0);
  assert.equal(result.sourcesByGroup['15']['738579'].rawData, '完整原文');
  assert.equal(result.sourceMetadataByGroup['15']['738579'].gender, '女频');
  assert.equal(result.fetchedCount, 1);
  assert.deepEqual(result.failedPlatforms, []);
  assert.equal(groups[0].sourceTextByBookId['738580'], undefined);
});

test('manual prefetch still reports the failed city without losing supplied text', async () => {
  const result = await prepareCreationSources({ groups, deferSources: false }, async payload => {
    assert.deepEqual(payload.bookIds, ['738580']);
    throw new Error('timeout');
  });
  assert.deepEqual(result.failedPlatforms, ['书城']);
  assert.match(result.failedReasons[0], /timeout/);
  assert.equal(result.sourcesByGroup['15']['738579'].data, '已有正文');
});
