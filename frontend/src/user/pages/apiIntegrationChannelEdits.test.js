import assert from 'node:assert/strict';
import test from 'node:test';
import { channelEdits, mergeChannelEdit } from './apiIntegrationChannelEdits.js';

test('a rename sends only the stable ID and changed name', () => {
  const old = [{ id: 'a', name: 'A', model: 'm', hasApiKey: true }, { id: 'b', name: 'B' }];
  assert.deepEqual(channelEdits(old, [{ ...old[0], name: 'new' }, old[1]]), [{ id: 'a', name: 'new' }]);
  assert.deepEqual(channelEdits(old, [old[1]]), []);
});
test('key change and explicit clear replace each other in pending edits', () => {
  assert.deepEqual(mergeChannelEdit({ id: 'a', clearApiKey: true }, { id: 'a', apiKey: 'synthetic' }), { id: 'a', apiKey: 'synthetic' });
  assert.deepEqual(mergeChannelEdit({ id: 'a', apiKey: 'synthetic' }, { id: 'a', clearApiKey: true }), { id: 'a', clearApiKey: true });
});

test('preview writes use stable channel IDs and keep omitted fields', async () => {
  const { applyPreviewChannelEdits } = await import('./apiIntegrationChannelEdits.js');
  const old = [{ id: 'a', name: 'A', fullUrl: true, model: 'saved', hasApiKey: true, params: { keep: 1, change: 2 } }, { id: 'b', name: 'B' }];
  const after = applyPreviewChannelEdits(old, { customChannels: [{ id: 'a', name: 'renamed', params: { change: 3 }, apiKey: '  ' }] });
  assert.deepEqual(after, [{ ...old[0], name: 'renamed', params: { keep: 1, change: 3 } }, old[1]]);
  assert.deepEqual(applyPreviewChannelEdits(after, { deletedChannelIds: ['a'] }), [old[1]]);
  assert.deepEqual(applyPreviewChannelEdits(after, { customChannels: [] }), after);
  assert.deepEqual(applyPreviewChannelEdits(after, { customChannels: [{ id: 'a', params: null }] }), after);
  const cleared = applyPreviewChannelEdits(after, { customChannels: [{ id: 'a', clearApiKey: true, params: {} }] });
  assert.equal(cleared[0].hasApiKey, false);
  assert.equal('params' in cleared[0], false);
  assert.equal('apiKey' in cleared[0], false);
  assert.equal(old[0].name, 'A');
});
