import { Alert, Button, Input, Tag } from 'antd';
import { useEffect, useRef, useState } from 'react';
import {
  ENERGY_LEDGER_PAGE_SIZE, exportEnergyLedgerPage, getAccountOverview, loadEnergyDashboard, normalizeEnergyFilters
} from '../../shared/api/accountCenterEnergy.js';
import { FEATURE_LABELS, PageHeader, Panel, RoleBadge } from './accountCenterShared.jsx';
import {
  canSelectEnergyTeam, energyAmount, energyRoleLabel, energyTime, EnergyMetrics, EnergyPager, EnergyQueryState,
  EnergyRanking, EnergyRecharge, EnergyTeamScope, useEnergyQuery
} from './accountCenterEnergyShared.jsx';

const blankFilters = { from: '', to: '', member: '' };

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
  return <Panel title="筛选账单">
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
  return <Panel title="账单明细" action={<Button disabled={!ledger.entries.length} loading={download.loading} onClick={exportPage}>导出当前页 CSV</Button>}>
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
  const [teamId, setTeamId] = useState(() => new URLSearchParams(globalThis.location?.search).get('teamId') || '');
  const [applied, setApplied] = useState({});
  const [ledgerOffset, setLedgerOffset] = useState(0);
  const [rankingOffset, setRankingOffset] = useState(0);
  const selectTeam = current && canSelectEnergyTeam(current);
  const scope = (selectTeam && teamId) || current?.team?.id;
  const criteria = current && { from: current.filter.from, to: current.filter.to, ...applied };
  const filters = { ...criteria, teamId: scope, limit: ENERGY_LEDGER_PAGE_SIZE, offset: ledgerOffset };
  const dashboardKey = scope ? JSON.stringify([current.subject.role, current.subject.username, filters, rankingOffset]) : null;
  const dashboard = useEnergyQuery(dashboardKey, signal => loadEnergyDashboard(filters, current.subject, { signal, rankingOffset }));
  function resetPages() { setLedgerOffset(0); setRankingOffset(0); }
  function changeTeam(id) {
    setTeamId(id);
    if ((id || current.team?.id) !== scope) {
      setApplied(({ member, ...dates }) => dates);
      resetPages();
    }
  }
  const ready = dashboard.data;

  return <div className="account-center-page energy-page">
    <PageHeader title="能量与账单" actions={<Button onClick={current ? dashboard.retry : overview.retry}>刷新账单</Button>} />
    <EnergyQueryState query={overview} label="账号与团队" />
    {current && <>
      <Panel title={ready?.summary.team?.name || (scope === current.team?.id ? current.team?.name : '') || '团队能量'}
        action={<RoleBadge canonicalRole={energyRoleLabel(current.subject.role)} />}>
        <EnergyRecharge url={current.rechargeUrl} />
      </Panel>
      {selectTeam && <EnergyTeamScope currentTeamId={current.team?.id} selectedTeamId={teamId}
        onSelect={changeTeam} from={criteria.from} to={criteria.to} />}
      {scope && <EnergyFiltersForm key={scope} ownOnly={current.subject.role === 'member'} applied={applied} onApply={next => { setApplied(next); resetPages(); }} />}
      <EnergyQueryState query={dashboard} label="能量与账单" />
      {!scope && <><p className="ac-empty">{selectTeam ? '当前账号没有所属团队，请选择一个团队查看账单' : '当前账号没有可查询的所属团队'}</p><EnergyMetrics data={current} /></>}
      {ready && <>
        <p className="energy-hint">{energyTime(ready.summary.filter.from)} 至 {energyTime(ready.summary.filter.to)}（不含结束时间）</p>
        <EnergyMetrics data={ready.summary} member={criteria.member} />
        <Panel title="团队成员能量消耗排行">
          <EnergyRanking ranking={ready.ranking} />
          <EnergyPager page={ready.ranking} onPage={setRankingOffset} label="排行" />
        </Panel>
        <LedgerPanel key={dashboardKey} ledger={ready.ledger} filters={filters} subject={current.subject} onPage={setLedgerOffset} />
      </>}
    </>}
  </div>;
}
