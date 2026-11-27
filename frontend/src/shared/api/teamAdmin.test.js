import test from 'node:test';
import assert from 'node:assert/strict';
import { changeAdminTeam, createAdminTeam, createAdminTeamAccount } from './teamAdmin.js';

test('uses account-center team mutations and encodes the team ID', async () => {
  const requests = [];
  const request = async (...args) => requests.push(args);
  await createAdminTeam({ name: '第一队' }, request);
  await createAdminTeamAccount('a/b', { username: 'worker', role: 'member' }, request);
  await changeAdminTeam('a/b', 'restore_team', request);
  assert.deepEqual(requests, [
    ['/api/account-center/admin/teams', { method: 'POST', body: '{"name":"第一队"}', suppressGlobalError: true }],
    ['/api/account-center/admin/teams/a%2Fb/accounts', { method: 'POST', body: '{"username":"worker","role":"member"}', suppressGlobalError: true }],
    ['/api/account-center/admin/teams/a%2Fb/restore_team', { method: 'POST', suppressGlobalError: true }]
  ]);
});
