import assert from 'node:assert/strict';
import test from 'node:test';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, 'ApiConfigPage.jsx'), 'utf8');

test('平台预设卡没有启用开关：填了 Key 就按生效处理', () => {
  assert.match(source, /enabled: !clearing/);
  assert.match(source, /else if \(clearing\) payload\.clearCredential = true/);
  assert.doesNotMatch(source, /togglePreset\(/);
  assert.doesNotMatch(source, /<Switch/);
});

test('预设卡不再有锁定遮罩与“先关闭开关”提示', () => {
  assert.doesNotMatch(source, /ac-platform-preset-lock/);
  assert.doesNotMatch(source, /请先关闭开关/);
});

test('未保存时输入框保持可编辑，保存中才禁用', () => {
  const settingsSource = fs.readFileSync(path.join(here, 'ApiIntegrationSettings.jsx'), 'utf8');
  assert.match(settingsSource, /disabled=\{saving\}/);
  assert.doesNotMatch(settingsSource, /disabled=\{!model\.hasCredential\}/);
});

test('文案明确说明清空 Key 等于停用，不依赖额外开关', () => {
  const settingsSource = fs.readFileSync(path.join(here, 'ApiIntegrationSettings.jsx'), 'utf8');
  assert.match(settingsSource, /填写 Key 后立即生效并进入模型下拉/);
  assert.match(settingsSource, /清空 Key 即从下拉移除/);
});

test('目录模型不再向三组传启用回调', () => {
  assert.doesNotMatch(source, /onToggleModelEnabled|modelTogglingId/);
});

test('role-aware page keeps full controls separate from restricted and member modes', () => {
  assert.match(source, /projectApiConfigCapabilities\(config\)/);
  assert.match(source, /policy\.mode === 'full'/);
  assert.match(source, /policy\.mode === 'restricted'/);
  assert.match(source, /policy\.mode !== 'none'\s*\?/);
});

test('restricted Key writes never send enabled or address fields', () => {
  assert.match(source, /restrictedCredentialPatch\(/);
  assert.doesNotMatch(source, /restrictedCredentialPatch\([^\n]+enabled/);
});

test('restricted Key saving and errors are keyed per model while full mode keeps its existing scalar state', () => {
  const settingsSource = fs.readFileSync(path.join(here, 'ApiIntegrationSettings.jsx'), 'utf8');
  const restricted = settingsSource.slice(settingsSource.indexOf('function RestrictedApiIntegrationSettings'), settingsSource.indexOf('export default function ApiIntegrationSettings'));
  const full = settingsSource.slice(settingsSource.indexOf('function FullApiIntegrationSettings'));
  assert.match(restricted, /keyRequests\[model\.id\]/);
  assert.match(restricted, /setCatalogKeyRequestState\(current, model\.id/);
  assert.match(restricted, /role="alert"/);
  assert.doesNotMatch(restricted, /catalogModelSavingId === model\.id/);
  assert.match(full, /catalogModelSavingId === model\.id/);
});

test('restricted async completions are guarded by component epoch and preserve newer drafts', () => {
  const settingsSource = fs.readFileSync(path.join(here, 'ApiIntegrationSettings.jsx'), 'utf8');
  const restricted = settingsSource.slice(settingsSource.indexOf('function RestrictedApiIntegrationSettings'), settingsSource.indexOf('export default function ApiIntegrationSettings'));
  assert.match(restricted, /mountedRef/);
  assert.match(restricted, /epochRef/);
  assert.match(restricted, /revisionRef/);
  assert.match(restricted, /catalogKeySubmissionStatus/);
  assert.match(restricted, /isActive/);
  assert.match(restricted, /if \(status\.clearTouched\) touchedRef\.current\.delete\(model\.id\)/);
});

test('restricted selection saves only display selection or kind and model id', () => {
  assert.match(source, /executeYZCMSelectionOperations\(/);
  assert.match(source, /create: createManagedModel/);
  assert.match(source, /update: updateManagedModel/);
  assert.doesNotMatch(source, /deleteManagedModel/);
});

test('full mode keeps every catalog model Key directly editable on its model card', () => {
  const settingsSource = fs.readFileSync(path.join(here, 'ApiIntegrationSettings.jsx'), 'utf8');
  const card = settingsSource.slice(settingsSource.indexOf('function renderCatalogCard'), settingsSource.indexOf('function buildGroupMerge'));
  assert.match(card, /className="api-channel-preset-line api-catalog-credential"/);
  assert.doesNotMatch(card, /!userManaged\s*\?/);
  assert.match(card, /userManaged\s*\?\s*\(/);
});
