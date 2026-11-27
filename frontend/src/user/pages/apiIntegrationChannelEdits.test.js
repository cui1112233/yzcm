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
