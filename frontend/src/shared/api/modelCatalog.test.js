import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// Resolve the browser's extensionless client import for the native Node runner.
const source = readFileSync(new URL('./modelCatalog.js', import.meta.url), 'utf8')
  .replace(/from '\.\/client(?:\.js)?'/, `from '${new URL('./client.js', import.meta.url).href}'`);
const modelCatalogModule = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
const { isModelKind, modelKindLabel, modelSelectOptions, searchSavedYZCMModels } = modelCatalogModule;

test('uses backend displayName for typed model choices', () => {
  assert.deepEqual(modelSelectOptions([
    { id: 'text-a', kind: 'text', displayName: '后台文本模型', name: '旧名' },
    { id: 'video-a', kind: 'video', name: '后台视频模型' }
  ], 'text'), [{ value: 'text-a', label: '后台文本模型' }]);
});

test('uses the available label fallback and skips entries without a usable ID', () => {
  assert.deepEqual(modelSelectOptions([
    { id: 'model-name', kind: 'image', name: '名称' },
    { id: 'model-id', kind: 'image', modelId: '模型标识' },
    { id: '   ', kind: 'image', displayName: '无 ID' },
    { id: 'text-a', kind: 'text', displayName: '其他类型' }
  ], 'image'), [
    { value: 'model-name', label: '名称' },
    { value: 'model-id', label: '模型标识' }
  ]);
});

test('treats audio as a supported managed model kind', () => {
  assert.equal(isModelKind('audio'), true);
  assert.equal(modelKindLabel('audio'), '音频');
});

test('treats ASR as a separate managed model kind', () => {
  assert.equal(isModelKind('asr'), true);
  assert.equal(modelKindLabel('asr'), '语音识别');
});

test('one shared catalog keeps successful model kinds and is reused by every dropdown', async () => {
  assert.equal(typeof modelCatalogModule.getAvailableModelCatalog, 'function');
  assert.equal(typeof modelCatalogModule.invalidateAvailableModelCatalog, 'function');
  modelCatalogModule.invalidateAvailableModelCatalog();
  const calls = [];
  const first = await modelCatalogModule.getAvailableModelCatalog({ loadKind: async kind => {
    calls.push(kind);
    if (kind === 'video') throw new Error('video temporarily unavailable');
    return [{ id: `${kind}-configured`, kind, enabled: true }];
  } });
  assert.deepEqual(first.models.map(model => model.id), ['text-configured', 'image-configured', 'asr-configured']);
  assert.deepEqual(first.failedKinds, ['video']);

  const second = await modelCatalogModule.getAvailableModelCatalog({ loadKind: async kind => {
    calls.push(kind);
    return [{ id: `${kind}-configured`, kind }];
  } });
  assert.deepEqual(second.failedKinds, []);
  assert.deepEqual(calls, ['text', 'image', 'video', 'asr', 'video']);
});

test('requests only relevant kinds and shares simultaneous requests', async () => {
  modelCatalogModule.invalidateAvailableModelCatalog();
  const calls = [];
  const loadKind = async kind => { calls.push(kind); return [{ id: kind, kind }]; };
  await Promise.all([
    modelCatalogModule.getAvailableModelCatalog({ kinds: ['text', 'image'], loadKind }),
    modelCatalogModule.getAvailableModelCatalog({ kinds: ['text'], loadKind })
  ]);
  assert.deepEqual(calls, ['text', 'image']);
});

test('failures retain readable reasons without caching a failed request', async () => {
  modelCatalogModule.invalidateAvailableModelCatalog();
  const result = await modelCatalogModule.getAvailableModelCatalog({ kinds: ['video'], loadKind: async () => { throw Object.assign(new Error('unauthorized'), {status:401}); } });
  assert.match(modelCatalogModule.describeModelCatalogFailure(result), /视频：登录已失效/);
  assert.equal(modelCatalogModule.peekAvailableModelCatalog(['video']), null);
});

test('saved YZCM search sends only supported filters and preserves page metadata', async () => {
  const calls = [];
  const expected = {
    status: 'available', reason: '', stale: true, catalogUpdatedAt: '2026-11-27T12:00:00Z',
    snapshotId: '55', hasMore: true, nextOffset: 60,
    models: [{ kind: 'text', modelId: 'deepseek-chat', displayName: 'DeepSeek' }]
  };
  const page = await searchSavedYZCMModels({ kind: 'text', q: '深度 探索', limit: 20, offset: 40, snapshotId: '55', ignored: 'no' }, async (path, options) => {
    calls.push({ path, options });
    return expected;
  });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].path, '/api/config/models/yzcm?kind=text&q=%E6%B7%B1%E5%BA%A6+%E6%8E%A2%E7%B4%A2&limit=20&offset=40&snapshotId=55');
  assert.deepEqual(calls[0].options, { suppressGlobalError: true });
  assert.deepEqual(page, expected);
});

test('saved YZCM search restarts at page one once after a snapshot conflict', async () => {
  const paths = [];
  const page = await searchSavedYZCMModels({ kind: 'video', q: 'seed', limit: 10, offset: 20, snapshotId: '99' }, async path => {
    paths.push(path);
    if (paths.length === 1) throw Object.assign(new Error('changed'), { status: 409 });
    return { status: 'available', stale: false, models: [], hasMore: false, offset: 0, limit: 10 };
  });
  assert.equal(paths.length, 2);
  assert.match(paths[0], /offset=20/);
  assert.match(paths[0], /snapshotId=99/);
  assert.match(paths[1], /offset=0/);
  assert.doesNotMatch(paths[1], /snapshotId=/);
  assert.equal(page.offset, 0);
});
