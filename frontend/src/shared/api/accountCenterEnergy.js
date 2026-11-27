import { apiRequest } from './client.js';

/** @typedef {{teamId?: string, member?: string, from?: string, to?: string, limit?: number, offset?: number}} EnergyFilters */
/** @typedef {{username: string, displayName: string, role: 'founder' | 'developer' | 'manager' | 'member', teamId?: string}} EnergySubject */
/** @typedef {{id: string, name: string, archivedAt: string | null}} EnergyTeam */
/** @typedef {{actualCost: string, unit: 'energy', requestCount: number, reconciledCount: number, pendingCount: number}} EnergyTotals */
/** @typedef {{available: boolean, amount: string | null, unit: 'energy', reason?: 'wallet_unavailable' | 'wallet_error' | 'wallet_stale' | 'no_active_team', updatedAt?: string}} EnergyBalance */
/** @typedef {{limit: number, offset: number, hasMore: boolean, nextOffset?: number}} EnergyPage */
/** @typedef {{team: EnergyTeam | null, consumption: EnergyTotals | null, balance: EnergyBalance}} EnergyTeamSummary */
/** @typedef {EnergyFilters & {from: string, to: string, timeZone: string}} EnergyAppliedFilters */
/** @typedef {EnergyTeamSummary & {filter: EnergyAppliedFilters, rechargeUrl: string}} EnergySummary */
/** @typedef {EnergySummary & {subject: EnergySubject, canViewAllTeams: boolean}} AccountOverview */
/** @typedef {EnergyTotals & {username: string, rank: number}} EnergyRankingEntry */
/** @typedef {EnergyPage & {team: EnergyTeam, filter: EnergyAppliedFilters, entries: EnergyRankingEntry[], emptyReason?: string}} EnergyRanking */
/** @typedef {{requestId: string, owner: string, actorUsername?: string, at: string, feature: string, model: string, status: 'reconciled' | 'pending', httpStatus: number, actualCost: string | null, reconciledAt?: string}} EnergyLedgerEntry */
/** @typedef {EnergyPage & {team: EnergyTeam, filter: EnergyAppliedFilters, unit: 'energy', entries: EnergyLedgerEntry[]}} EnergyLedger */
/** @typedef {EnergyPage & {filter: EnergyAppliedFilters, teams: EnergyTeamSummary[], rechargeUrl: string}} EnergyTeams */
/** @typedef {{signal?: AbortSignal}} EnergyRequestOptions */

export const ENERGY_CONSOLE_URL = 'http://newapi.yzcm.site/';
export const ENERGY_TIME_ZONE = 'Asia/Shanghai';
export const ENERGY_RANKING_PAGE_SIZE = 8;
export const ENERGY_LEDGER_PAGE_SIZE = 20;
const ENERGY_MAX_OFFSET = 10000;
const datePattern = /^([1-9]\d{3}-\d{2}-\d{2})(?:T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2}))?$/;

function energyDate(value) {
  const date = datePattern.exec(value)?.[1];
  const calendar = date && new Date(`${date}T00:00:00Z`);
  const timestamp = Date.parse(value.length === 10 ? `${value}T00:00:00+08:00` : value);
  if (!calendar || !Number.isFinite(timestamp) || calendar.toISOString().slice(0, 10) !== date) {
    throw new Error('请输入有效的筛选日期');
  }
  return timestamp;
}

/** @param {EnergyFilters} filters @returns {EnergyFilters} */
export function normalizeEnergyFilters(filters = {}) {
  const { teamId = '', member = '', from = '', to = '', limit, offset } = filters;
  const normalized = {};
  for (const [key, value, max] of [['teamId', teamId, 64], ['member', member, 32]]) {
    if (typeof value !== 'string' || new TextEncoder().encode(value.trim()).length > max) throw new Error('团队或成员筛选条件无效');
    if (value.trim()) normalized[key] = value.trim();
  }
  const start = from ? energyDate(from) : null;
  const end = to ? energyDate(to) : null;
  if (start !== null && end !== null && (start >= end || end - start > 366 * 86400000)) {
    throw new Error('筛选日期必须按顺序填写，且区间最多 366 天');
  }
  if (from) normalized.from = from;
  if (to) normalized.to = to;
  if (limit !== undefined) {
    if (!Number.isInteger(limit) || limit < 1 || limit > 100) throw new Error('分页大小无效');
    normalized.limit = limit;
  }
  if (offset !== undefined) {
    if (!Number.isInteger(offset) || offset < 0 || offset > ENERGY_MAX_OFFSET) throw new Error('分页位置无效');
    normalized.offset = offset;
  }
  return normalized;
}

/** @param {string | undefined} value */
export function trustedEnergyConsole(value) {
  return value === ENERGY_CONSOLE_URL ? value : '';
}

function pathFor(resource, filter) {
  const query = new URLSearchParams(filter).toString();
  return `/api/account-center/${resource}${query ? `?${query}` : ''}`;
}

function energyRequest(resource, filter, options) {
  return apiRequest(pathFor(resource, filter), { ...options, suppressGlobalError: true });
}

/** @param {EnergyFilters} [filters] @param {EnergyRequestOptions} [options] @returns {Promise<AccountOverview>} */
export async function getAccountOverview(filters, options) {
  return energyRequest('overview', normalizeEnergyFilters(filters), options);
}

/** @param {EnergyFilters} filters @param {EnergyRequestOptions} [options] @returns {Promise<EnergyTeams>} */
export async function getEnergyTeams({ from, to, limit, offset }, options) {
  return energyRequest('energy/teams', normalizeEnergyFilters({ from, to, limit, offset }), options);
}

/** @param {EnergyFilters} filters @param {EnergyRequestOptions} [options] @returns {Promise<EnergyRanking>} */
export async function getEnergyRanking(filters, options) {
  return energyRequest('energy/ranking', normalizeEnergyFilters(filters), options);
}

function ledgerFilter(filter, subject) {
  return subject.role === 'member' ? { ...filter, member: subject.username } : filter;
}

/** @param {EnergyFilters} filters @param {EnergySubject} subject @param {EnergyRequestOptions & {rankingOffset?: number}} [options] */
export async function loadEnergyDashboard(filters, subject, { rankingOffset = 0, signal } = {}) {
  const { limit = ENERGY_LEDGER_PAGE_SIZE, offset = 0, ...criteria } = normalizeEnergyFilters(filters);
  /** @type {[EnergySummary, EnergyLedger, EnergyRanking]} */
  const [summary, ledger, ranking] = await Promise.all([
    energyRequest('energy', criteria, { signal }),
    energyRequest('energy/ledger', ledgerFilter({ ...criteria, limit, offset }, subject), { signal }),
    getEnergyRanking({ ...criteria, limit: ENERGY_RANKING_PAGE_SIZE, offset: rankingOffset }, { signal })
  ]);
  return { summary, ledger, ranking };
}

function csvPageMetadata(headers) {
  if (headers.get('Content-Type')?.split(';')[0] !== 'text/csv') throw new Error('未收到有效的 CSV 账单');
  const integerHeader = name => {
    const value = headers.get(`X-Energy-${name}`);
    if (value === null || !/^\d{1,5}$/.test(value)) throw new Error('无法确认 CSV 导出的分页范围');
    return Number(value);
  };
  const limit = integerHeader('Page-Limit');
  const offset = integerHeader('Page-Offset');
  normalizeEnergyFilters({ limit, offset });
  const hasMore = headers.get('X-Energy-Has-More');
  if (hasMore !== 'true' && hasMore !== 'false') throw new Error('无法确认 CSV 导出的分页范围');
  const nextOffset = headers.has('X-Energy-Next-Offset') ? integerHeader('Next-Offset') : undefined;
  if (nextOffset !== undefined && (hasMore !== 'true' || nextOffset !== offset + limit || nextOffset > ENERGY_MAX_OFFSET)) {
    throw new Error('无法确认 CSV 导出的分页范围');
  }
  return { limit, offset, hasMore: hasMore === 'true', nextOffset };
}

/** @param {EnergyFilters} filters @param {EnergySubject} subject @param {EnergyRequestOptions} [options] @returns {Promise<{blob: Blob, page: EnergyPage}>} */
export async function exportEnergyLedgerPage(filters, subject, options) {
  const filter = ledgerFilter(normalizeEnergyFilters({ limit: ENERGY_LEDGER_PAGE_SIZE, offset: 0, ...filters }), subject);
  let page;
  const blob = await energyRequest('energy/ledger', { ...filter, format: 'csv' }, {
    ...options, responseType: 'blob', onResponseHeaders: headers => { page = csvPageMetadata(headers); }
  });
  if (!page || page.limit !== filter.limit || page.offset !== filter.offset) throw new Error('返回的 CSV 账单分页与当前页不一致');
  return { blob, page };
}
