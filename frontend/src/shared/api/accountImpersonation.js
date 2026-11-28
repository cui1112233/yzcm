import { apiRequest } from './client.js';
import { blockPageIdentityUntilReload } from './accountImpersonationState.js';

const path = '/api/account-impersonation';
const options = { credentials: 'same-origin', suppressGlobalError: true };

export function getAccountImpersonation(signal, request = apiRequest) {
  return request(`${path}/current`, { ...options, method: 'GET', signal, cache: 'no-store' });
}

export function getAccountImpersonationTargets(request = apiRequest) {
  return request(`${path}/targets`, { ...options, method: 'GET', cache: 'no-store' });
}

export async function startAccountImpersonation(username, request = apiRequest) {
  const result = await request(path, { ...options, method: 'POST', body: JSON.stringify({ username }) });
  blockPageIdentityUntilReload();
  return result;
}

export async function stopAccountImpersonation(request = apiRequest) {
  const result = await request(`${path}/current`, { ...options, method: 'DELETE' });
  blockPageIdentityUntilReload();
  return result;
}
