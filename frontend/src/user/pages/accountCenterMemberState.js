const CANONICAL_ROLES = ['开发创始', '开发者', '管理者', '组员'];

export function canonicalRoleLabel(role) {
  return CANONICAL_ROLES.includes(role) ? role : '暂不可用';
}

export function readMonthlyTokenLimit(member) {
  const limit = member?.monthlyTokenLimit;
  switch (member?.monthlyTokenLimitStatus) {
    case 'limited':
      if (Number.isFinite(limit) && limit >= 0) return { status: 'limited', limit };
      break;
    case 'unlimited':
      if (limit === null) return { status: 'unlimited', limit: null };
      break;
  }
  return { status: 'unavailable', limit: null };
}

export function mergeProfileMember(current, updated) {
  const quota = readMonthlyTokenLimit(updated?.monthlyTokenLimitStatus ? updated : current);
  return {
    ...current,
    ...updated,
    canonicalRole: CANONICAL_ROLES.includes(updated?.canonicalRole) ? updated.canonicalRole : current?.canonicalRole,
    capabilities: Array.isArray(updated?.capabilities) ? updated.capabilities : current?.capabilities,
    monthlyTokenLimit: quota.limit,
    monthlyTokenLimitStatus: quota.status
  };
}
