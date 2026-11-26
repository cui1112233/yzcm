import assert from 'node:assert/strict';
import test from 'node:test';
import { createAutosaveController } from './scriptEditorDrafts.js';

test('409冲突保留本地剧本并要求选择恢复方式', async () => {
  const ctl = createAutosaveController({ delay: 0, save: async () => { const error = new Error('conflict'); error.status = 409; throw error; } });
  ctl.edit({ content: '本地新稿', revision: 2 });
  await ctl.flush();
  assert.equal(ctl.state().draft.content, '本地新稿');
  assert.equal(ctl.state().status, 'conflict');
});

test('连续输入只保存最后一份并接收服务端版本号', async () => {
  const saved = [];
  const ctl = createAutosaveController({ delay: 0, save: async draft => { saved.push(draft.content); return { ...draft, revision: 3 }; } });
  ctl.edit({ content: '一', revision: 1 });
  ctl.edit({ content: '二', revision: 1 });
  await ctl.flush();
  assert.deepEqual(saved, ['二']);
  assert.equal(ctl.state().draft.revision, 3);
  assert.equal(ctl.state().status, 'saved');
});
