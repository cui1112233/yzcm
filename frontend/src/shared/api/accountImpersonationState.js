const expectedActorHeader = 'X-Qiantie-Expected-Actor';
const expectedImpersonationHeader = 'X-Qiantie-Expected-Impersonation';
let confirmedPageIdentity = null;
let pageIdentityGeneration = 0;
let awaitingReload = false;

function clearPageIdentity(reloadPending) {
  confirmedPageIdentity = null;
  pageIdentityGeneration++;
  awaitingReload = reloadPending;
}

export function blockPageIdentityUntilReload() {
  clearPageIdentity(true);
}

export function getPageIdentityGeneration() {
  return pageIdentityGeneration;
}

export function rememberVerifiedImpersonation(identity, token, generation = pageIdentityGeneration) {
  if (identity === null) {
    clearPageIdentity(false);
    return true;
  }
  if (awaitingReload || generation !== pageIdentityGeneration) return false;
  const { actorUsername, username, impersonationId } = identity || {};
  if (!token || !actorUsername || !username || !impersonationId) return false;
  if (confirmedPageIdentity) {
    // An old document keeps its original precondition until it is replaced.
    return confirmedPageIdentity.token === token && confirmedPageIdentity.actorUsername === actorUsername &&
      confirmedPageIdentity.impersonationId === impersonationId && confirmedPageIdentity.username === username;
  }
  confirmedPageIdentity = Object.freeze({ actorUsername, username, impersonationId, token });
  return true;
}

export function readVerifiedImpersonation() {
  return confirmedPageIdentity;
}

export function setImpersonationExpectation(headers, path, method, token) {
  headers.delete(expectedActorHeader);
  headers.delete(expectedImpersonationHeader);
  if (typeof path !== 'string' || !path.startsWith('/api/')) return;
  if (['GET', 'HEAD', 'OPTIONS'].includes(method.toUpperCase())) return;
  // Login and public account-recovery flows do not use a workspace identity.
  if (/^\/api\/(?:login|applications|account-recovery\/password\/(?:request|reset))(?:[/?]|$)/.test(path)) return;
  if (!token || !confirmedPageIdentity) {
    const error = new Error('请先确认当前页面账号，再重试');
    error.code = 'IMPERSONATION_UNCONFIRMED';
    throw error;
  }
  headers.set(expectedActorHeader, confirmedPageIdentity.actorUsername);
  headers.set(expectedImpersonationHeader, confirmedPageIdentity.impersonationId);
}
