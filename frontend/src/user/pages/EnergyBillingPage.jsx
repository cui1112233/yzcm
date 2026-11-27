import { Alert, Button, Input, Tag } from 'antd';
import { useEffect, useRef, useState } from 'react';
import {
  ENERGY_LEDGER_PAGE_SIZE, ENERGY_TEAM_PAGE_SIZE, exportEnergyLedgerPage, getAccountOverview, getEnergyTeams,
  loadEnergyDashboard, normalizeEnergyFilters
} from '../../shared/api/accountCenterEnergy.js';
import { DesignedEmptyState, GlassPanel } from '../components/PersonalCenterVisuals.jsx';
import { FEATURE_LABELS, PageHeader, Panel, RoleBadge } from './accountCenterShared.jsx';
import {
  canSelectEnergyTeam, energyAmount, energyRoleLabel, energyTime, EnergyMetrics, EnergyPager, EnergyQueryState,
  EnergyRanking, EnergyRecharge, EnergyTeamCards, useEnergyQuery
} from './accountCenterEnergyShared.jsx';

const blankFilters = { from: '', to: '', member: '' };

function teamFromLocation() {
  const teamId = new URLSearchParams(globalThis.location?.search).get('teamId') || '';
  try { return normalizeEnergyFilters({ teamId }).teamId || ''; }
  catch { return ''; }
}

export function EnergyLedgerTable({ ledger, subject }) {
  const ownOnly = subject.role === 'member';
  const entries = ownOnly ? ledger.entries.filter(entry => entry.owner === subject.username) : ledger.entries;
  const showActor = !ownOnly && entries.some(entry => entry.actorUsername);
  if (!entries.length) return <p className="ac-empty">当前页暂无账单</p>;
  return <div className="energy-table" role="region" aria-label="账单明细表" tabIndex={0}>
    <table>
      <thead><tr><th scope="col">请求时间</th><th scope="col">所属账号</th><th scope="col">功能</th><th scope="col">模型</th>
        <th scope="col">已核账能量</th><th scope="col">核账状态</th>{showActor && <th scope="col">实际操作者</th>}</tr></thead>
      <tbody>{entries.map(entry => <tr key={entry.requestId}>
        <td>{energyTime(entry.at)}</td><td>{entry.owner}</td><td>{Object.hasOwn(FEATURE_LABELS, entry.feature) ? FEATURE_LABELS[entry.feature] : entry.feature || '—'}</td><td>{entry.model || '—'}</td>
        <td>{entry.status === 'reconciled' ? energyAmount(entry.actualCost) : '—'}</td>
        <td>{entry.status === 'reconciled' ? <Tag>已核账</Tag> : <Tag color="gold">待核账</Tag>}</td>
        {showActor && <td>{entry.actorUsername || '—'}</td>}
      </tr>)}</tbody>
    </table>
  </div>;
}

function EnergyFiltersForm({ ownOnly, applied, onApply }) {
  const [draft, setDraft] = useState(() => ({ ...blankFilters, ...applied }));
  const [error, setError] = useState('');
  const field = name => ({ value: draft[name], onChange: event => setDraft({ ...draft, [name]: event.target.value }) });
  function apply(event) {
    event.preventDefault();
    try {
      onApply(normalizeEnergyFilters(draft));
      setError('');
    } catch (invalid) { setError(invalid.message); }
  }
  return <Panel title="筛选账单" className="pc-platform-filters">
    <form onSubmit={apply} className="energy-filter">
      <label>开始日期<Input type="date" {...field('from')} /></label>
      <label>结束日期（不含当天）<Input type="date" {...field('to')} /></label>
      <label>成员账号<Input {...field('member')} maxLength={32} placeholder="全部成员" /></label>
      <Button type="primary" htmlType="submit">应用筛选</Button>
      <Button onClick={() => { setDraft(blankFilters); setError(''); onApply({}); }}>重置筛选</Button>
    </form>
    <p className="energy-hint">日期按北京时间计算。日期留空时使用本月范围。{ownOnly && ' 成员筛选适用于汇总和排行，明细与导出始终只包含本人。'}</p>
    {error && <Alert showIcon type="error" message={error} />}
  </Panel>;
}

function downloadLedger(blob, page) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = `energy-ledger-page-${Math.floor(page.offset / page.limit) + 1}.csv`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function LedgerPanel({ ledger, filters, subject, onPage }) {
  const [download, setDownload] = useState({});
  const exportRequest = useRef(null);
  useEffect(() => () => exportRequest.current?.abort(), []);
  async function exportPage() {
    if (exportRequest.current) return;
    const request = new AbortController();
    exportRequest.current = request;
    setDownload({ loading: true });
    try {
      const { blob, page } = await exportEnergyLedgerPage(filters, subject, { signal: request.signal });
      if (request.signal.aborted) return;
      downloadLedger(blob, page);
      setDownload({ page });
    } catch (error) {
      if (!request.signal.aborted) setDownload({ error: error.message });
    } finally { exportRequest.current = null; }
  }
  const exportedPage = download.page;
  return <Panel title="账单明细" className="pc-platform-ledger" action={<Button disabled={!ledger.entries.length} loading={download.loading} onClick={exportPage}>导出当前页 CSV</Button>}>
    <p className="energy-hint">CSV 仅导出当前页，每页最多 {ledger.limit} 条。{ledger.hasMore ? '后面还有记录。' : '当前页后暂无更多记录。'}</p>
    {download.error && <Alert showIcon type="error" message="CSV 导出失败" description={download.error} />}
    {exportedPage && <p role="status">已导出第 {Math.floor(exportedPage.offset / exportedPage.limit) + 1} 页，每页最多 {exportedPage.limit} 条。
      {exportedPage.hasMore ? '后面还有记录。' : '当前页后暂无更多记录。'}</p>}
    <EnergyLedgerTable ledger={ledger} subject={subject} />
    <EnergyPager page={ledger} onPage={onPage} label="账单" />
  </Panel>;
}

export default function EnergyBillingPage() {
  const overview = useEnergyQuery('account', signal => getAccountOverview({}, { signal }));
  const current = overview.data;
  const [teamId, setTeamId] = useState(teamFromLocation);
  const [filteredTeamId, setFilteredTeamId] = useState(null);
  const [applied, setApplied] = useState({});
  const [ledgerOffset, setLedgerOffset] = useState(0);
  const [rankingOffset, setRankingOffset] = useState(0);
  const [teamOffset, setTeamOffset] = useState(0);
  const selectTeam = current && canSelectEnergyTeam(current);
  const currentTeamId = current?.team?.id || '';
  const criteria = current && { from: current.filter.from, to: current.filter.to, ...applied };
  const directoryKey = selectTeam ? JSON.stringify([current.subject.username, criteria, teamOffset]) : null;
  const directory = useEnergyQuery(directoryKey, signal => getEnergyTeams({ ...criteria, limit: ENERGY_TEAM_PAGE_SIZE, offset: teamOffset }, { signal }));
  const requestedTeamId = (applied.member ? filteredTeamId ?? teamId : teamId) || currentTeamId;
  const teams = directory.data?.teams;
  const listed = teams?.some(({ team }) => team.id === requestedTeamId);
  const fallbackTeamId = teams?.some(({ team }) => team.id === currentTeamId) ? currentTeamId : teams?.[0]?.team.id;
  const fullDirectory = directory.data?.offset === 0 && !directory.data.hasMore;
  const missingFromDirectory = fullDirectory && !listed;
  let scope = currentTeamId;
  if (selectTeam) {
    scope = teams?.length && requestedTeamId ? (missingFromDirectory ? fallbackTeamId : requestedTeamId) : '';
  }
  const filters = { ...criteria, teamId: scope, limit: ENERGY_LEDGER_PAGE_SIZE, offset: ledgerOffset };
  const dashboardKey = scope ? JSON.stringify([current.subject.role, current.subject.username, filters, rankingOffset]) : null;
  const dashboard = useEnergyQuery(dashboardKey, signal => loadEnergyDashboard(filters, current.subject, { signal, rankingOffset }));
  const ready = dashboard.data;
  const detailForbidden = dashboard.error?.status === 403;
  // A paginated directory can prove a team is present. A missing row needs a detail check unless the whole directory was returned.
  const replaceMissingTeam = selectTeam && !applied.member && teamId && !listed && fallbackTeamId &&
    (missingFromDirectory || detailForbidden && scope !== fallbackTeamId);

  useEffect(() => {
    if (replaceMissingTeam) {
      setTeamId(fallbackTeamId === currentTeamId ? '' : fallbackTeamId);
      setLedgerOffset(0);
      setRankingOffset(0);
    }
  }, [replaceMissingTeam, fallbackTeamId, currentTeamId]);

  const canSyncSelection = current && (!selectTeam || directory.data && (!scope || listed || missingFromDirectory || ready));
  const selectedTeamId = scope === currentTeamId ? '' : scope;
  useEffect(() => {
    if (!canSyncSelection) return;
    const url = new URL(window.location.href);
    const target = selectTeam ? selectedTeamId : '';
    if ((url.searchParams.get('teamId') || '') === target) return;
    if (target) url.searchParams.set('teamId', target);
    else url.searchParams.delete('teamId');
    window.history.replaceState(window.history.state, '', url);
  }, [canSyncSelection, selectTeam, selectedTeamId]);

  function resetPages() { setLedgerOffset(0); setRankingOffset(0); }
  function changeTeam(id) {
    if (applied.member) setFilteredTeamId(id);
    else setTeamId(id);
    if ((id || currentTeamId) !== scope) resetPages();
  }
  function applyFilters(next) {
    if (next.member !== applied.member) setFilteredTeamId(null);
    setApplied(next);
    resetPages();
    setTeamOffset(0);
  }

  return <div className={`account-center-page energy-page pc-platform-consumption ${selectTeam ? 'is-platform-scope' : 'is-team-scope'}`}>
    <PageHeader title={selectTeam ? '全平台团队消耗' : '能量与账单'} subtitle={selectTeam ? '查看各团队余额与能量使用情况' : '查看所属团队余额与真实核账记录'} actions={<Button onClick={overview.retry}>刷新账单</Button>} />
    <EnergyQueryState query={overview} label="账号与团队" />
    {current && <>
      <Panel title={ready?.summary.team?.name || (scope === current.team?.id ? current.team?.name : '') || '团队能量'}
        action={<RoleBadge canonicalRole={energyRoleLabel(current.subject.role)} />}>
        <EnergyRecharge url={current.rechargeUrl} />
      </Panel>
      {(selectTeam || scope) && <EnergyFiltersForm ownOnly={current.subject.role === 'member'} applied={applied} onApply={applyFilters} />}
      {selectTeam && <div className="pc-platform-teams"><EnergyTeamCards currentTeamId={currentTeamId} selectedTeamId={selectedTeamId}
        onSelect={changeTeam} teams={directory.data} onPage={setTeamOffset} member={criteria.member} activeSummary={ready?.summary}>
        <EnergyQueryState query={directory} label="团队目录" />
      </EnergyTeamCards></div>}
      {selectTeam && detailForbidden && !replaceMissingTeam
        ? <Alert showIcon type="warning" message="所选团队账单无法查看"
          description={applied.member ? '请从团队卡片切换团队，或清除成员筛选。' : '请从团队卡片切换团队。'} />
        : <EnergyQueryState query={dashboard} label="所选团队账单" />}
      {!scope && !currentTeamId && (!selectTeam || teams?.length > 0) && <>
        <p className="ac-empty">{selectTeam ? '当前账号没有所属团队，请选择一个团队查看账单' : '当前账号没有可查询的所属团队'}</p>
        <div className="pc-platform-metrics"><EnergyMetrics data={current} /></div>
      </>}
      {ready && <>
        <p className="energy-hint">{energyTime(ready.summary.filter.from)} 至 {energyTime(ready.summary.filter.to)}（不含结束时间）</p>
        <div className="pc-platform-metrics"><EnergyMetrics data={ready.summary} member={criteria.member} /></div>
        <div className="pc-platform-analysis">
          <GlassPanel className="pc-platform-trend" title={selectTeam ? '各团队消耗趋势' : '团队消耗趋势'} icon="trend">
            <DesignedEmptyState icon="trend" title="趋势数据暂未接通" description="当前接口提供真实汇总、排行与账单，时间序列接通后会在这里显示。" />
          </GlassPanel>
          <Panel title="团队成员能量消耗排行" className="pc-platform-ranking">
            <EnergyRanking ranking={ready.ranking} />
            <EnergyPager page={ready.ranking} onPage={setRankingOffset} label="排行" />
          </Panel>
        </div>
        <LedgerPanel key={dashboardKey} ledger={ready.ledger} filters={filters} subject={current.subject} onPage={setLedgerOffset} />
      </>}
    </>}
  </div>;
}
