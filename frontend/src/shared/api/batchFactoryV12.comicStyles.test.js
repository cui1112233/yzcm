import test from 'node:test';
import assert from 'node:assert/strict';
import { createComicStyleMediaAPI } from './comicStyleRequests.js';

test('style queries preserve lite view, options and escaped IDs', async () => {
  const calls = [];
  const api = createComicStyleMediaAPI((...args) => calls.push(args), 'b/1', 'k', '水墨/A');
  await api.listPages({view:'lite',signal:'signal'});
  assert.equal(calls[0][0], '/api/batch-factory/v12/batches/b%2F1/books/k/comic-pages?view=lite&styleId=%E6%B0%B4%E5%A2%A8%2FA');
  assert.equal(calls[0][1].signal, 'signal');
  await api.generatePage('v/1', {imagePrompt:'shared',styleId:'wrong'});
  assert.equal(JSON.parse(calls[1][1].body).styleId, '水墨/A');
  await api.downloadCollection('c/1');
  assert.match(calls[2][0], /c%2F1\/download\?styleId=/);
  assert.equal(calls[2][1].responseType, 'blob');
});

test('captured A operations remain A when another client selects B', async () => {
  const calls = [];
  const request = (...args) => calls.push(args);
  const a = createComicStyleMediaAPI(request, 'b','k','A');
  const b = createComicStyleMediaAPI(request, 'b','k','B');
  assert.equal(calls.length,0);
  await b.listCollections(); await a.publish('c', {producerAdminName:'test'});
  assert.match(calls[0][0], /styleId=B$/);
  assert.equal(JSON.parse(calls[1][1].body).styleId,'A');
});
