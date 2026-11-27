import { Alert, Button, Skeleton } from 'antd';
import { useEffect, useMemo, useState } from 'react';
import { ENERGY_TEAM_PAGE_SIZE, ENERGY_TIME_ZONE, getEnergyTeams, trustedEnergyConsole } from '../../shared/api/accountCenterEnergy.js';
import { Panel } from './accountCenterShared.jsx';
import './account-center-energy.css';

const roleLabels = { founder: '开发创始', developer: '开发者', manager: '管理者', member: '组员' };
const timeFormatter = new Intl.DateTimeFormat('zh-CN', {
  timeZone: ENERGY_TIME_ZONE, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23'
});

export function energyRoleLabel(role) {
  return Object.hasOwn(roleLabels, role) ? roleLabels[role] : '暂无法获取';
}

export function canSelectEnergyTeam(overview) {
  return overview.canViewAllTeams === true && ['founder', 'developer'].includes(overview.subject.role);
}

export function energyTime(value) {
  const time = Date.parse(value);
  return Number.isFinite(time) ? timeFormatter.format(time) : '暂无记录';
}

export function useEnergyQuery(key, load) {
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState(null);
  const request = useMemo(() => ({ key, attempt }), [key, attempt]);
  useEffect(() => {
    if (request.key === null) return;
    const controller = new AbortController();
    Promise.resolve().then(() => load(controller.signal)).then(
      data => { if (!controller.signal.aborted) setResult({ request, data }); },
      error => { if (!controller.signal.aborted) setResult({ request, error }); }
    );
    return () => controller.abort();
    // The caller supplies a key covering every request parameter.
  }, [request]);
  const current = result?.request === request ? result : null;
  return { ...current, loading: key !== null && !current, retry: () => setAttempt(value => value + 1) };
}

export function EnergyQueryState({ query, label }) {
  if (query.loading) return <div role="status" aria-label={`正在读取${label}`}><Skeleton active paragraph={{ rows: 2 }} /></div>;
  if (query.error) return <Alert showIcon type="error" message={`${label}读取失败`} description={query.error.message}
    action={<Button onClick={query.retry}>重试</Button>} />;
  return null;
}

export function energyAmount(value) {
  return typeof value === 'string' && /^-?\d+(?:\.\d+)?$/.test(value) ? `${value} 能量` : '暂无法获取';
}

export function EnergyBalanceValue({ balance }) {
  const shown = balance?.available ? energyAmount(balance.amount) : '暂无法获取';
  const explanation = {
    wallet_unavailable: '暂无可用的 YZCM 余额数据', wallet_stale: 'YZCM 余额数据已过期',
    wallet_error: 'YZCM 余额查询失败', no_active_team: '当前账号没有可查询的所属团队'
  }[balance?.reason];
  return <div className="energy-value">
    <strong>{shown}</strong>
    {explanation && <small>{explanation}</small>}
    <small>余额更新时间 {energyTime(balance?.updatedAt)}</small>
  </div>;
}

export function EnergyRecharge({ url }) {
  const href = trustedEnergyConsole(url);
  const label = '前往 YZCM API 补充能量';
  return <div className="energy-recharge">
    {href ? <Button type="primary" href={href} target="_blank" rel="noreferrer">{label}</Button> : <Button disabled>{label}</Button>}
    {!href && <small>暂未配置可信的 YZCM API 控制台地址</small>}
    <small>1 能量 = 1 元 YZCM 可用额度</small>
  </div>;
}

function reconciledTotal(totals) {
  if (!totals) return '暂无法获取';
  if (totals.reconciledCount === 0) return totals.pendingCount ? '待核账' : '暂无已核账消耗';
  return energyAmount(totals.actualCost);
}

export function EnergyMetrics({ data, member }) {
  return <div className="energy-metrics">
    <Panel title="团队剩余能量"><EnergyBalanceValue balance={data.balance} /></Panel>
    <Panel title={member ? '所选成员已核账消耗' : '团队已核账消耗'}><div className="energy-value"><strong>{reconciledTotal(data.consumption)}</strong></div></Panel>
    <Panel title="待核账"><div className="energy-value"><strong>{data.consumption ? `${data.consumption.pendingCount} 笔` : '暂无记录'}</strong></div></Panel>
    <Panel title="模型请求"><div className="energy-value"><strong>{data.consumption ? `${data.consumption.requestCount} 次` : '暂无记录'}</strong></div></Panel>
  </div>;
}

export function EnergyRanking({ ranking }) {
  if (!ranking.entries.length) return <p className="ac-empty">
    {ranking.emptyReason === 'page_out_of_range' ? '当前页没有排行记录' : '当前时间范围暂无已核账消费排行'}
  </p>;
  return <div className="ac-ranking-list" aria-label="成员消费排行">
    {ranking.entries.map(entry => <div className="ac-ranking-row" key={entry.username}>
      <span aria-label={`第 ${entry.rank} 名`}>{entry.rank}</span><span>{entry.username}</span>
      <span className="ac-ranking-value"><strong>{energyAmount(entry.actualCost)}</strong>{entry.pendingCount > 0 && <small>{entry.pendingCount} 笔待核账</small>}</span>
    </div>)}
  </div>;
}

export function EnergyPager({ page, onPage, label }) {
  if (!page) return null;
  return <nav className="energy-pager" aria-label={`${label}分页`}>
    <Button size="small" disabled={page.offset === 0} onClick={() => onPage(Math.max(0, page.offset - page.limit))}>上一页</Button>
    <span>第 {Math.floor(page.offset / page.limit) + 1} 页</span>
    <Button size="small" disabled={page.nextOffset == null} onClick={() => onPage(page.nextOffset)}>下一页</Button>
    {page.hasMore && page.nextOffset == null && <small>已到达可查询页数上限，请缩小时间范围</small>}
  </nav>;
}

export function EnergyTeamScope({ currentTeamId, selectedTeamId, onSelect, from, to }) {
  const [paging, setPaging] = useState({});
  const dateKey = JSON.stringify([from, to]);
  const offset = paging.dateKey === dateKey ? paging.offset : 0;
  const teams = useEnergyQuery(JSON.stringify([dateKey, offset]), signal => getEnergyTeams({ from, to, limit: ENERGY_TEAM_PAGE_SIZE, offset }, { signal }));
  return <EnergyTeamCards currentTeamId={currentTeamId} selectedTeamId={selectedTeamId} onSelect={onSelect}
    teams={teams.data} onPage={next => setPaging({ dateKey, offset: next })}>
    <EnergyQueryState query={teams} label="团队余额与消耗" />
  </EnergyTeamCards>;
}

export function EnergyTeamCards({ currentTeamId, selectedTeamId, onSelect, teams, onPage, member, activeSummary, children }) {
  return <Panel title="各团队余额与消耗" action={<Button type={!selectedTeamId ? 'primary' : 'default'} disabled={!currentTeamId}
    aria-pressed={!selectedTeamId} onClick={() => onSelect('')}>我的团队</Button>}>
    {children}
    {teams?.teams.length === 0 && <p className="ac-empty">暂无可查看的团队</p>}
    {teams && <>
      <div className="energy-teams">
        {teams.teams.map(({ team, balance, consumption }) => <div className="energy-team" key={team.id}>
          <strong>{team.name}{team.archivedAt ? '（已归档）' : ''}</strong>
          <EnergyBalanceValue balance={activeSummary?.team?.id === team.id ? activeSummary.balance : balance} />
          <span>{member ? '该成员已核账消耗' : '团队已核账消耗'} {reconciledTotal(consumption)}</span>
          <Button size="small" type={team.id === (selectedTeamId || currentTeamId) ? 'primary' : 'default'}
            aria-pressed={team.id === (selectedTeamId || currentTeamId)} onClick={() => onSelect(team.id)}>查看 {team.name}</Button>
        </div>)}
      </div>
      <EnergyPager page={teams} onPage={onPage} label="团队" />
    </>}
  </Panel>;
}
