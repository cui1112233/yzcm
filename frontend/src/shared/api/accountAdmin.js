import { apiRequest } from './client.js';

const adminPath = '/api/account-center/admin';

export function getAccountAdminDirectory(request = apiRequest) {
  return request(`${adminPath}/teams`, { suppressGlobalError: true });
}

export function getAccountAdminAudit(teamId = '', request = apiRequest) {
  const query = teamId ? `?${new URLSearchParams({ teamId })}` : '';
  return request(`${adminPath}/audit${query}`, { suppressGlobalError: true });
}

export function changeAdminAccount(username, action, request = apiRequest) {
  return request(`${adminPath}/accounts/${encodeURIComponent(username)}/${encodeURIComponent(action)}`, {
    method: 'POST',
    suppressGlobalError: true
  });
}

export function createUngroupedAdminAccount(input, request = apiRequest) {
  return request(`${adminPath}/accounts`, { method: 'POST', body: JSON.stringify(input), suppressGlobalError: true });
}

export function listGlobalAccounts(filters = {}) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== null && value !== '') query.set(key, String(value));
  }
  const suffix = query.size ? `?${query.toString()}` : '';
  return apiRequest(`/api/account-admin/accounts${suffix}`);
}
