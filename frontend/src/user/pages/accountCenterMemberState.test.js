import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

const pageURL = name => new URL(name, import.meta.url);
const pageSource = name => readFileSync(pageURL(name), 'utf8');

test('profile and member center use formal role and quota in each visible identity and quota section', () => {
  const profile = pageSource('./ProfilePage.jsx');
  const memberCenter = pageSource('./MemberCenterPage.jsx');
  assert.equal((profile.match(/<RoleBadge canonicalRole=\{member\.canonicalRole\}/g) || []).length, 2);
  assert.equal((profile.match(/mergeProfileMember\(current\.member, result\.member\)/g) || []).length, 2);
  assert.match(profile, /title="会员权益"[^\n]*<strong>\{membershipRole\}/);
  assert.match(memberCenter, /<RoleBadge canonicalRole=\{self\.canonicalRole\}/);
  assert.match(memberCenter, /当前身份<\/dt><dd>\{membershipRole\}/);
  for (const page of [profile, memberCenter]) {
    assert.match(page, /readMonthlyTokenLimit\(/);
    assert.match(page, /暂不可用/);
    assert.doesNotMatch(page, /(?:member|self)\.role\s*(?:===|\?)/);
  }
});

test('four canonical role labels remain visible after profile and avatar changes', async () => {
  const bundle = await build({
    entryPoints: [fileURLToPath(pageURL('./accountCenterShared.jsx'))],
    platform: 'node', packages: 'external', format: 'cjs', bundle: true, write: false, jsx: 'automatic'
  });
  const module = { exports: {} };
  new Function('require', 'module', 'exports', bundle.outputFiles[0].text)(createRequire(import.meta.url), module, module.exports);
  const { RoleBadge } = module.exports;
  const { canonicalRoleLabel, mergeProfileMember } = await import('./accountCenterMemberState.js');
  for (const [canonicalRole, role] of [['开发创始','dev'], ['开发者','dev'], ['管理者','manager'], ['组员','member']]) {
    const current = { canonicalRole, role, monthlyTokenLimit: null, monthlyTokenLimitStatus: 'unavailable' };
    const saved = mergeProfileMember(current, { role: 'dev', displayName: '已保存' });
    const uploaded = mergeProfileMember(saved, { role: 'manager', avatarUrl: '/avatar.png', monthlyTokenLimit: 2500 });
    for (const member of [current, saved, uploaded]) {
      assert.equal(canonicalRoleLabel(member.canonicalRole), canonicalRole);
      assert.match(renderToStaticMarkup(createElement(RoleBadge, { canonicalRole: member.canonicalRole })), new RegExp(`>${canonicalRole}</span>`));
      assert.equal(member.monthlyTokenLimitStatus, 'unavailable');
      assert.equal(member.monthlyTokenLimit, null);
    }
    assert.equal(uploaded.displayName, '已保存');
    assert.equal(uploaded.avatarUrl, '/avatar.png');
  }
  assert.equal(canonicalRoleLabel('dev'), '暂不可用');
});

test('unknown quota is distinct from a confirmed finite quota or confirmed unlimited quota', async () => {
  const { readMonthlyTokenLimit } = await import('./accountCenterMemberState.js');
  for (const member of [
    { monthlyTokenLimit: null }, { monthlyTokenLimit: 2500 },
    { monthlyTokenLimit: null, monthlyTokenLimitStatus: 'unavailable' },
    { monthlyTokenLimit: 2500, monthlyTokenLimitStatus: 'unavailable' },
    { monthlyTokenLimit: 2500, monthlyTokenLimitStatus: 'unlimited' }
  ]) assert.deepEqual(readMonthlyTokenLimit(member), { status: 'unavailable', limit: null });
  assert.deepEqual(readMonthlyTokenLimit({ monthlyTokenLimit: null, monthlyTokenLimitStatus: 'unlimited' }), { status: 'unlimited', limit: null });
  assert.deepEqual(readMonthlyTokenLimit({ monthlyTokenLimit: 0, monthlyTokenLimitStatus: 'limited' }), { status: 'limited', limit: 0 });
});
