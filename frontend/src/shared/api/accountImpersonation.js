import { apiRequest } from './client.js';

const path = '/api/account-impersonation';
const options = { credentials: 'same-origin', suppressGlobalError: true };

export function getAccountImpersonation(signal, request = apiRequest) {
  return request(`${path}/current`, { ...options, method: 'GET', signal, cache: 'no-store' });
}

export function startAccountImpersonation(username, request = apiRequest) {
  return request(path, { ...options, method: 'POST', body: JSON.stringify({ username }) });
}

export function stopAccountImpersonation(request = apiRequest) {
  return request(`${path}/current`, { ...options, method: 'DELETE' });
}
