const expectedImpersonationHeader = 'X-Qiantie-Expected-Impersonation';
let verifiedImpersonation = null;

export function rememberVerifiedImpersonation(identity, token) {
  verifiedImpersonation = token && identity?.username && identity?.impersonationId
    ? Object.freeze({ username: identity.username, impersonationId: identity.impersonationId, token })
    : null;
}

export function readVerifiedImpersonation(token) {
  return verifiedImpersonation?.token === token ? verifiedImpersonation : null;
}

export function setImpersonationExpectation(headers, path, method, token) {
  headers.delete(expectedImpersonationHeader);
  if (typeof path !== 'string' || !path.startsWith('/api/')) return;
  if (/^\/api\/(?:login|account-impersonation)(?:[/?]|$)/.test(path)) return;
  if (['GET', 'HEAD', 'OPTIONS'].includes(method.toUpperCase())) return;
  const id = readVerifiedImpersonation(token)?.impersonationId;
  if (id) headers.set(expectedImpersonationHeader, id);
}
