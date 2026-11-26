import test from 'node:test';
import assert from 'node:assert/strict';
import { changeAdminAccount, getAccountAdminDirectory, getAccountAdminAudit } from './accountAdmin.js';

test('uses account-center directory, audit and encoded account actions', async () => {
  const requests = [];
  const request = async (...args) => requests.push(args);
  await getAccountAdminDirectory(request);
  await getAccountAdminAudit('team/a', request);
  await changeAdminAccount('a/b', 'purge_preflight', request);
  assert.deepEqual(requests, [
    ['/api/account-center/admin/teams', { suppressGlobalError: true }],
    ['/api/account-center/admin/audit?teamId=team%2Fa', { suppressGlobalError: true }],
    ['/api/account-center/admin/accounts/a%2Fb/purge_preflight', { method: 'POST', suppressGlobalError: true }]
  ]);
});
