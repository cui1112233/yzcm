import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

test('uses the same server-authorized team groups and account actions on both pages', () => {
  const accounts = readFileSync(new URL('./AccountRolePage.jsx', import.meta.url), 'utf8');
  const teams = readFileSync(new URL('./TeamPage.jsx', import.meta.url), 'utf8');
  for (const label of ['开发创始', '开发者', '管理者', '组员']) assert.ok(accounts.includes(label));
  assert.match(accounts, /directory\.teams\.map/);
  assert.match(accounts, /directory\.ungrouped/);
  assert.match(accounts, /account\.actions\.map/);
  assert.match(accounts, /canonicalRole=\{ROLE_LABELS\[role\] \|\| role\}/);
  assert.match(accounts, /team\.actions\.map/);
  assert.match(accounts, /value \?\? '暂不可用'/);
  assert.match(teams, /<AccountRolePage[^>]*teamsOnly/);
  assert.doesNotMatch(accounts, /reset_password|listGlobalAccounts|getMemberCenter/);
  assert.match(accounts, /getAccountAdminAudit\(target\?\.id\)/);
  assert.match(accounts, /account\.restoreConflict/);
  assert.match(accounts, /grant_team_create/);
});
