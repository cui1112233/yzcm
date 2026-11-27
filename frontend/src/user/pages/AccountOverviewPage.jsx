import { Button } from 'antd';
import { useState } from 'react';
import { ENERGY_RANKING_PAGE_SIZE, getAccountOverview, getEnergyRanking } from '../../shared/api/accountCenterEnergy.js';
import { Link } from '../../shared/components/Link.jsx';
import { MemberIdentity, PageHeader, Panel, RoleBadge } from './accountCenterShared.jsx';
import {
  canSelectEnergyTeam, energyRoleLabel, energyTime, EnergyMetrics, EnergyQueryState, EnergyRanking,
  EnergyRecharge, EnergyTeamScope, useEnergyQuery
} from './accountCenterEnergyShared.jsx';

export default function AccountOverviewPage() {
  const [teamId, setTeamId] = useState('');
  const overview = useEnergyQuery('account', signal => getAccountOverview({}, { signal }));
  const current = overview.data;
  const canSelect = current && canSelectEnergyTeam(current);
  const selectedId = canSelect ? teamId : '';
  const period = current && { from: current.filter.from, to: current.filter.to };
  const selected = useEnergyQuery(selectedId || null, signal => getAccountOverview({ ...period, teamId: selectedId }, { signal }));
  const scope = selectedId ? selected.data : current;
  const rankingTeamId = scope?.team?.id;
  const ranking = useEnergyQuery(rankingTeamId || null, signal => getEnergyRanking({
    ...period, teamId: rankingTeamId, limit: ENERGY_RANKING_PAGE_SIZE
  }, { signal }));
  const usageHref = selectedId ? `/usage?${new URLSearchParams({ teamId: selectedId })}` : '/usage';

  return <div className="account-center-page energy-page">
    <PageHeader title="账户总览" actions={<Button onClick={() => { overview.retry(); selected.retry(); ranking.retry(); }}>刷新总览</Button>} />
    <EnergyQueryState query={overview} label="账户总览" />
    {current && <>
      <div className="energy-overview-columns">
        <Panel title="当前账号" action={<RoleBadge canonicalRole={energyRoleLabel(current.subject.role)} />}>
          <MemberIdentity member={current.subject} />
          <div className="ac-side-summary"><span>所属团队</span><strong>{current.team?.name || '未加入团队'}</strong></div>
        </Panel>
        <Panel title="补充能量"><EnergyRecharge url={current.rechargeUrl} /></Panel>
      </div>
      <EnergyQueryState query={selected} label="所选团队" />
      {scope && <>
        {scope.team && <p className="energy-hint">{scope.team.name} · {energyTime(scope.filter.from)} 至 {energyTime(scope.filter.to)}（不含结束时间）</p>}
        <EnergyMetrics data={scope} />
        {scope.team && <Panel title="团队成员能量消耗排行" action={<Link href={usageHref} className="ac-text-link">查看能量与账单</Link>}>
          <EnergyQueryState query={ranking} label="成员排行" />
          {ranking.data && <EnergyRanking ranking={ranking.data} />}
        </Panel>}
      </>}
      {canSelect && <EnergyTeamScope currentTeamId={current.team?.id} selectedTeamId={selectedId}
        onSelect={setTeamId} from={period.from} to={period.to} />}
    </>}
  </div>;
}
