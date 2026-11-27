import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';

const app = fs.readFileSync(new URL('../App.jsx', import.meta.url), 'utf8');
const layout = fs.readFileSync(new URL('../../shared/layouts/UserLayout.jsx', import.meta.url), 'utf8');
const page = fs.readFileSync(new URL('./ScriptEditorPage.jsx', import.meta.url), 'utf8');

test('剧本编辑有独立入口和路由', () => {
  assert.match(layout, /label:\s*'剧本编辑'/);
  assert.match(layout, /href:\s*'\/script-editor'/);
  assert.match(app, /'\/script-editor':\s*ScriptEditorPage/);
});

test('剧本编辑包含导入预览且前端不硬编码系统提示词', () => {
  assert.match(page, /导入文档/);
  assert.match(page, /确认导入/);
  assert.doesNotMatch(page, /systemPrompt|系统提示词正文/);
});
