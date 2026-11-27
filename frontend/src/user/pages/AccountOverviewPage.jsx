import { Button } from 'antd';
import { useState } from 'react';
import { ENERGY_RANKING_PAGE_SIZE, getAccountOverview, getEnergyRanking } from '../../shared/api/accountCenterEnergy.js';
import { Link } from '../../shared/components/Link.jsx';
import { DesignedEmptyState, GlassPanel } from '../components/PersonalCenterVisuals.jsx';
import RoleEnergyIcon from '../components/RoleEnergyIcon.jsx';
import { MemberIdentity, PageHeader } from './accountCenterShared.jsx';
import {
  canSelectEnergyTeam, energyAmount, energyRoleLabel, energyTime, EnergyBalanceValue, EnergyQueryState,
  EnergyRanking, EnergyRecharge, EnergyTeamScope, useEnergyQuery
} from './accountCenterEnergyShared.jsx';

function consumedEnergy(consumption) {
  if (!consumption) return '暂不可用';
  if (!consumption.reconciledCount) return consumption.pendingCount ? '待核账' : '暂无消耗';
  return energyAmount(consumption.actualCost);
}

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

  return <div className="account-center-page energy-page pc-overview">
    <PageHeader title="账户总览" actions={<Button onClick={() => { overview.retry(); selected.retry(); ranking.retry(); }}>刷新总览</Button>} />
    <EnergyQueryState query={overview} label="账户总览" />
    {current && <>
      <div className="pc-overview-hero">
        <GlassPanel className="pc-overview-identity" title="一战晟铭">
          <div className="pc-overview-identity-main"><MemberIdentity member={current.subject} /><RoleEnergyIcon role={energyRoleLabel(current.subject.role)} size={42} /></div>
          <dl><div><dt>所属团队</dt><dd>{current.team?.name || '未加入团队'}</dd></div><div><dt>当前身份</dt><dd>{energyRoleLabel(current.subject.role)}</dd></div></dl>
        </GlassPanel>
        <GlassPanel className="pc-overview-balance" title="可用能量" icon="energy">
          <div className="pc-overview-balance-main"><EnergyBalanceValue balance={scope?.balance || current.balance} /><small>1 能量 = 1 元 YZCM 可用额度</small></div>
          <dl className="pc-overview-balance-stats">
            <div><dt>团队本月消耗</dt><dd>{consumedEnergy(scope?.consumption)}</dd></div>
            <div><dt>当前范围请求</dt><dd>{scope?.consumption ? `${scope.consumption.requestCount} 次` : '暂不可用'}</dd></div>
            <div><dt>待核账</dt><dd>{scope?.consumption ? `${scope.consumption.pendingCount} 笔` : '暂不可用'}</dd></div>
          </dl>
        </GlassPanel>
      </div>

      <EnergyQueryState query={selected} label="所选团队" />
      {scope && <>
        {scope.team ? <p className="energy-hint">{scope.team.name} · {energyTime(scope.filter.from)} 至 {energyTime(scope.filter.to)}（不含结束时间）</p> : null}
        <div className="pc-overview-dashboard">
          <GlassPanel className="pc-overview-trend" title="能量消耗趋势" icon="trend">
            <DesignedEmptyState icon="trend" title="趋势数据暂未接通" description="当前接口只返回汇总和账单数据，接通时间序列后会在这里显示真实趋势。" />
          </GlassPanel>
          <GlassPanel className="pc-overview-ranking" title="成员能量消耗排行榜（本月）" icon="ranking" actions={<Link href={usageHref} className="ac-text-link">查看账单</Link>}>
            {scope.team ? <><EnergyQueryState query={ranking} label="成员排行" />{ranking.data ? <EnergyRanking ranking={ranking.data} /> : null}</> : <DesignedEmptyState icon="team" title="暂无所属团队" description="加入团队后才能查看成员能量消耗排行。" />}
          </GlassPanel>
        </div>
      </>}

      <div className="pc-overview-footer">
        <GlassPanel className="pc-overview-connection" title="YZCM 账户连接" icon="layers"><EnergyRecharge url={current.rechargeUrl} /></GlassPanel>
        <GlassPanel className="pc-overview-models" title="模型可用情况" icon="models"><DesignedEmptyState icon="models" title="从 API 配置读取" description="模型可用状态由真实 API 配置决定。" action={<Link href="/api-config" className="ac-text-link">管理模型与端点</Link>} /></GlassPanel>
        <GlassPanel className="pc-overview-bills" title="最近账单（已核账）" icon="billing"><DesignedEmptyState icon="billing" title="在账单页查看明细" description="账户总览不复制账单数据，避免产生第二套记录。" action={<Link href={usageHref} className="ac-text-link">查看完整账单</Link>} /></GlassPanel>
      </div>

      {canSelect ? <EnergyTeamScope currentTeamId={current.team?.id} selectedTeamId={selectedId} onSelect={setTeamId} from={period.from} to={period.to} /> : null}
    </>}
  </div>;
}
