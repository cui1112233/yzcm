import { apiRequest } from './client.js';

const adminTeamsPath = '/api/account-center/admin/teams';

export function createAdminTeam(input, request = apiRequest) {
  return request(adminTeamsPath, { method: 'POST', body: JSON.stringify(input), suppressGlobalError: true });
}

export function createAdminTeamAccount(teamId, input, request = apiRequest) {
  return request(`${adminTeamsPath}/${encodeURIComponent(teamId)}/accounts`, {
    method: 'POST',
    body: JSON.stringify(input),
    suppressGlobalError: true
  });
}

export function changeAdminTeam(teamId, action, request = apiRequest) {
  return request(`${adminTeamsPath}/${encodeURIComponent(teamId)}/${encodeURIComponent(action)}`, {
    method: 'POST',
    suppressGlobalError: true
  });
}
