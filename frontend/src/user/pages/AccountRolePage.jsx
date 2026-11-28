import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Empty, Form, Input, Modal, Select, Space, Table, Tag, Typography } from 'antd';
import { Activity, Bolt, CircleCheck, Clock3, Info, RefreshCw, Search, Trophy, UserRound, UsersRound } from 'lucide-react';
import { changeAdminAccount, createUngroupedAdminAccount, getAccountAdminAudit, getAccountAdminDirectory } from '../../shared/api/accountAdmin.js';
import { changeAdminTeam, createAdminTeam, createAdminTeamAccount } from '../../shared/api/teamAdmin.js';
import { startAccountImpersonation } from '../../shared/api/accountImpersonation.js';
import { reloadForAccountSwitch } from '../../shared/layouts/AccountCenterLayout.jsx';
import { PageHeader, RoleBadge } from './accountCenterShared.jsx';

const ROLE_LABELS = {
  founder: '开发创始',
  developer: '开发者',
  manager: '管理者',
  member: '组员'
};

const ACTION_LABELS = {
  create_team: '新建团队',
  create_manager: '添加管理者',
  create_member: '添加组员',
  archive_team: '归档团队',
  restore_team: '恢复团队',
  view_audit: '操作记录',
  grant_team_create: '授予建团队权限',
  revoke_team_create: '撤销建团队权限',
  set_member: '设为组员',
  set_manager: '设为管理者',
  set_developer: '设为开发者',
  enable: '启用账号',
  disable: '停用账号',
  archive: '归档账号',
  restore: '恢复归档账号',
  purge_preflight: '检查永久删除条件',
  switch_account: '切换账号'
};

const CHECK_LABELS = {
  archive: '归档状态',
  balance: '余额',
  in_flight_jobs: '在途任务',
  pending_reconciliation: '待核账记录',
  retention: '留存要求'
};

const CHECK_STATUSES = { clear: '通过', blocked: '未通过', unavailable: '暂不可用' };
const USERNAME_RULES = [
  { required: true },
  { pattern: /^[A-Za-z0-9][A-Za-z0-9._-]{1,31}$/, message: '账号须为 2–32 位字母、数字、点、下划线或短横线，并以字母或数字开头' }
];

function AccountFields({ manager = false }) {
  return <>
    <Form.Item name={manager ? 'managerUsername' : 'username'} label={manager ? '管理者账号' : '登录账号'} rules={USERNAME_RULES}>
      <Input autoComplete="off" />
    </Form.Item>
    <Form.Item name="displayName" label="显示名称" rules={[{ max: 191 }]}><Input /></Form.Item>
    <Form.Item name="password" label="初始密码" rules={[{ required: true, min: 8, max: 128 }]}>
      <Input.Password autoComplete="new-password" />
    </Form.Item>
  </>;
}

export default function AccountRolePage({ title = '账号与角色', teamsOnly = false }) {
  const [directory, setDirectory] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [pending, setPending] = useState(null);
  const [saving, setSaving] = useState(false);
  const [dialogError, setDialogError] = useState('');
  const [preflight, setPreflight] = useState(null);
  const [audit, setAudit] = useState(null);
  const [teamAudit, setTeamAudit] = useState([]);
  const [query, setQuery] = useState('');
  const [teamFilter, setTeamFilter] = useState('all');
  const [roleFilter, setRoleFilter] = useState('all');
  const [statusFilter, setStatusFilter] = useState('all');
  const [form] = Form.useForm();

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setDirectory(await getAccountAdminDirectory());
    } catch (failure) {
      setDirectory(null);
      setError(failure.message || '账号与团队读取失败');
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => { void load(); }, [load]);
  useEffect(() => {
    const team = directory?.teams?.find(item => !item.archivedAt) || directory?.teams?.[0];
    if (!teamsOnly || !team?.id) { setTeamAudit([]); return; }
    let active = true;
    getAccountAdminAudit(team.id).then(result => { if (active) setTeamAudit((result?.events || []).slice(0, 5)); }).catch(() => { if (active) setTeamAudit([]); });
    return () => { active = false; };
  }, [directory, teamsOnly]);

  function openAction(action, target) {
    if (!ACTION_LABELS[action]) return;
    form.resetFields();
    setDialogError('');
    setPending({ action, target });
  }

  async function submitAction(values) {
    if (!pending || saving) return;
    const { action, target } = pending;
    setSaving(true);
    setDialogError('');
    try {
      if (action === 'create_team') {
        await createAdminTeam(values);
      } else if (action === 'create_manager' || action === 'create_member') {
        const input = { ...values, role: action === 'create_manager' ? 'manager' : 'member' };
        if (target) await createAdminTeamAccount(target.id, input);
        else await createUngroupedAdminAccount(input);
      } else if (action === 'view_audit') {
        const result = await getAccountAdminAudit(target?.id);
        setAudit({ events: result.events, team: target?.name });
      } else if (action === 'purge_preflight') {
        setPreflight({ username: target.username, ...await changeAdminAccount(target.username, action) });
      } else if (action === 'switch_account') {
        await startAccountImpersonation(target.username);
        reloadForAccountSwitch();
        return;
      } else if (target.username) {
        await changeAdminAccount(target.username, action);
      } else {
        await changeAdminTeam(target.id, action);
      }
      setPending(null);
      if (action !== 'view_audit' && action !== 'purge_preflight') await load();
    } catch (failure) {
      setDialogError(failure.message || '操作失败');
    } finally {
      setSaving(false);
    }
  }

  function actionButton(action, target) {
    return ACTION_LABELS[action] ? <Button key={action} size="small" disabled={loading || saving} onClick={() => openAction(action, target)}>
      {ACTION_LABELS[action]}
    </Button> : null;
  }

  const unavailableValue = value => value ?? '暂不可用';
  const columns = [
    { title: '登录账号', dataIndex: 'username' },
    { title: '显示名称', dataIndex: 'displayName' },
    { title: '角色', dataIndex: 'role', render: role => <RoleBadge canonicalRole={ROLE_LABELS[role] || role} compact /> },
    { title: '状态', render: (_, account) => <Space direction="vertical" size={0}>
      {account.archivedAt ? '已归档' : account.active ? '启用' : '停用'}
      {account.historical && <Tag>历史归属</Tag>}
      {account.joinedAt && account.historical && <Typography.Text type="secondary">
        {new Date(account.joinedAt).toLocaleDateString()}{account.endedAt ? ` – ${new Date(account.endedAt).toLocaleDateString()}` : ''}
      </Typography.Text>}
      {account.restoreConflict && <Typography.Text type="warning">
        {account.currentTeamName ? `现属 ${account.currentTeamName}，无法恢复此归属` : '已有其他现行团队，无法恢复此归属'}
      </Typography.Text>}
    </Space> },
    { title: '余额', dataIndex: 'balance', render: unavailableValue },
    { title: '本月消费', dataIndex: 'monthConsumption', render: unavailableValue },
    { title: '操作', render: (_, account) => <Space wrap>{account.actions.map(action => actionButton(action, account))}</Space> }
  ];

  const createTeam = pending?.action === 'create_team';
  const createAccount = pending?.action === 'create_member' || pending?.action === 'create_manager';
  const hasForm = createTeam || createAccount;
  const normalizedQuery = query.trim().toLowerCase();
  const teams = directory?.teams || [];
  const ungrouped = directory ? directory.ungrouped : [];
  const allAccounts = [...teams.flatMap(team => team.accounts || []), ...ungrouped];
  const activeAccounts = allAccounts.filter(account => account.active && !account.archivedAt).length;
  const pendingAccounts = allAccounts.filter(account => !account.active || account.archivedAt).length;
  const monthConsumption = allAccounts.reduce((total, account) => total + (Number(account.monthConsumption) || 0), 0);
  const availableEnergy = allAccounts.reduce((total, account) => total + (Number(account.balance) || 0), 0);
  const primaryTeam = teams.find(team => !team.archivedAt) || teams[0];
  const primaryAccounts = primaryTeam?.accounts || [];
  const consumptionRanking = [...primaryAccounts].filter(account => !account.archivedAt).sort((left, right) => (Number(right.monthConsumption) || 0) - (Number(left.monthConsumption) || 0)).slice(0, 5);
  const matchesFilters = (account, teamName = '') => {
    const matchesQuery = !normalizedQuery || [account.username, account.displayName, ROLE_LABELS[account.role], teamName].some(value => String(value || '').toLowerCase().includes(normalizedQuery));
    const matchesRole = roleFilter === 'all' || account.role === roleFilter;
    const state = account.archivedAt ? 'archived' : account.active ? 'active' : 'disabled';
    return matchesQuery && matchesRole && (statusFilter === 'all' || statusFilter === state);
  };
  const filteredTeams = directory ? directory.teams.map(team => ({
    ...team,
    accounts: (team.accounts || []).filter(account => matchesFilters(account, team.name))
  })).filter(team => (teamFilter === 'all' || team.id === teamFilter) && (!normalizedQuery || team.name.toLowerCase().includes(normalizedQuery) || team.accounts.length)) : [];
  const filteredUngrouped = ungrouped.filter(account => matchesFilters(account));
  const teamMembers = primaryAccounts.filter(account => matchesFilters(account, primaryTeam?.name));
  const teamActions = primaryTeam?.actions || [];

  return <section className={`account-center-page account-role-page ${teamsOnly ? 'pc-team' : 'pc-accounts'}`}>
    <PageHeader title={title} subtitle={teamsOnly ? '管理成员、权限与团队能量使用' : '按团队管理账号、身份与权限'} actions={<Button icon={<RefreshCw size={15} />} onClick={load} loading={loading}>刷新</Button>} />
    {error && <Alert type="error" showIcon message={error} />}
    {directory && teamsOnly && <>
      <section className="pc-team-hero">
        <div className="pc-team-hero-name"><UsersRound size={28} /><span><small>团队</small><strong>{primaryTeam?.name || '暂无团队'}</strong></span></div>
        <div><Bolt size={22} /><span><small>可用能量</small><strong>{availableEnergy || '—'}</strong></span></div>
        <div><Activity size={22} /><span><small>本月消耗</small><strong>{monthConsumption || '—'}</strong></span></div>
        <div><UserRound size={22} /><span><small>成员</small><strong>{primaryAccounts.length}</strong></span></div>
        <Space>{teamActions.filter(action => ['create_member', 'create_manager', 'view_audit'].includes(action)).map(action => actionButton(action, primaryTeam))}</Space>
      </section>
      <div className="pc-team-workspace">
        <section className="pc-team-members">
          <header><h2>团队成员</h2><div className="pc-team-member-filters">
            <Input prefix={<Search size={16} />} allowClear value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索成员姓名或账号" />
            <Select value={roleFilter} onChange={setRoleFilter} options={[{ value: 'all', label: '全部身份' }, ...Object.entries(ROLE_LABELS).map(([value, label]) => ({ value, label }))]} />
            <Select value={statusFilter} onChange={setStatusFilter} options={[{ value: 'all', label: '全部状态' }, { value: 'active', label: '启用中' }, { value: 'disabled', label: '已停用' }, { value: 'archived', label: '已归档' }]} />
          </div></header>
          <Table rowKey={account => account.membershipId || account.username} columns={columns} dataSource={teamMembers} pagination={{ pageSize: 6, hideOnSinglePage: true }} scroll={{ x: 820 }} />
        </section>
        <aside className="pc-team-side">
          <section className="pc-team-ranking"><h2><Trophy size={20} />成员能量排行</h2>
            {consumptionRanking.length ? <ol>{consumptionRanking.map((account, index) => <li key={account.membershipId || account.username}><em>{index + 1}</em><span><strong>{account.displayName || account.username}</strong><small>@{account.username}</small></span><b>{Number(account.monthConsumption) || 0}</b></li>)}</ol> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无真实消耗记录" />}
          </section>
          <section className="pc-team-audit"><h2><Clock3 size={20} />最近管理操作</h2>
            {teamAudit.length ? <ol>{teamAudit.map(event => <li key={event.ID}><time>{event.CreatedAt ? new Date(event.CreatedAt).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : '—'}</time><span><strong>{event.ActorUsername || '系统'}</strong>{ACTION_LABELS[event.Action] || event.Action}{event.SubjectUsername ? ` · ${event.SubjectUsername}` : ''}</span></li>)}</ol> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无管理记录" />}
          </section>
        </aside>
      </div>
    </>}
    {directory && !teamsOnly && <>
      <div className="pc-accounts-notice"><Info size={17} />账号切换、密码重置与权限调整都会被系统记录，用于安全审计与合规追溯。</div>
      <div className="pc-accounts-metrics">
        <div><UsersRound size={28} /><span><small>团队</small><strong>{teams.length}</strong></span></div>
        <div><UserRound size={28} /><span><small>账号</small><strong>{allAccounts.length}</strong></span></div>
        <div><CircleCheck size={28} /><span><small>已启用</small><strong>{activeAccounts}</strong></span></div>
        <div><Clock3 size={28} /><span><small>待处理</small><strong>{pendingAccounts}</strong></span></div>
      </div>
      <div className="pc-accounts-filters">
        <Input prefix={<Search size={16} />} allowClear value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索账号（用户名 / 显示名称）" />
        <Select value={teamFilter} onChange={setTeamFilter} options={[{ value: 'all', label: '全部团队' }, ...teams.map(team => ({ value: team.id, label: team.name }))]} />
        <Select value={roleFilter} onChange={setRoleFilter} options={[{ value: 'all', label: '全部身份' }, ...Object.entries(ROLE_LABELS).map(([value, label]) => ({ value, label }))]} />
        <Select value={statusFilter} onChange={setStatusFilter} options={[{ value: 'all', label: '全部状态' }, { value: 'active', label: '已启用' }, { value: 'disabled', label: '已停用' }, { value: 'archived', label: '已归档' }]} />
        <Button onClick={() => { setQuery(''); setTeamFilter('all'); setRoleFilter('all'); setStatusFilter('all'); }}>重置</Button>
        <Space>{directory.actions.filter(action => ['create_team', 'create_member', 'create_manager'].includes(action)).map(action => actionButton(action))}</Space>
      </div>
      <div className="pc-accounts-directory">
        {filteredTeams.map((team, index) => <details className="pc-accounts-team" key={team.id} open={index === 0}>
          <summary><UsersRound size={22} /><strong>{team.name}</strong><span>团队负责人：{team.managerUsername || '—'}</span><span><Bolt size={17} />剩余能量：{(team.accounts || []).reduce((sum, account) => sum + (Number(account.balance) || 0), 0) || '—'}</span><span><Activity size={17} />本月消耗：{(team.accounts || []).reduce((sum, account) => sum + (Number(account.monthConsumption) || 0), 0) || '—'}</span></summary>
          <Table rowKey={account => account.membershipId || account.username} columns={columns} dataSource={team.accounts} pagination={false} scroll={{ x: 920 }} />
        </details>)}
        {filteredUngrouped.length > 0 && <details className="pc-accounts-team" open><summary><UsersRound size={22} /><strong>未分组账号</strong><span>{filteredUngrouped.length} 个账号</span></summary><Table rowKey="username" columns={columns} dataSource={filteredUngrouped} pagination={false} scroll={{ x: 920 }} /></details>}
        {filteredTeams.length === 0 && filteredUngrouped.length === 0 && <Empty description={normalizedQuery ? '没有找到匹配的账号' : '暂无可管理账号'} />}
      </div>
    </>}
    <Modal title={ACTION_LABELS[pending?.action]} open={Boolean(pending)}
      onCancel={() => { if (!saving) setPending(null); }}
      onOk={() => hasForm ? form.submit() : submitAction()}
      okText="确认" cancelText="取消" confirmLoading={saving} destroyOnClose>
      {hasForm ? <Form form={form} layout="vertical" onFinish={submitAction} preserve={false}>
        {createTeam && <Form.Item name="name" label="团队名称" rules={[{ required: true, max: 60 }]}><Input /></Form.Item>}
        <AccountFields manager={createTeam} />
      </Form> : pending?.target && <p>{pending.target.username ? `@${pending.target.username}` : pending.target.name}</p>}
      {dialogError && <Alert type="error" showIcon message={dialogError} />}
    </Modal>
    <Modal title={preflight ? `永久删除条件 · @${preflight.username}` : '永久删除条件'}
      open={Boolean(preflight)} onCancel={() => setPreflight(null)} footer={<Button onClick={() => setPreflight(null)}>关闭</Button>}>
      {preflight && <>
        <Alert type={preflight.allowed ? 'success' : 'warning'} showIcon message={preflight.allowed ? '删除条件已通过' : '当前无法永久删除'} />
        <ul>{preflight.checks.map(check => <li key={check.name}>{CHECK_LABELS[check.name] || check.name}　{CHECK_STATUSES[check.status] || check.status}</li>)}</ul>
      </>}
    </Modal>
    <Modal title={audit?.team ? `操作记录 · ${audit.team}` : '操作记录'} width={760} open={Boolean(audit)}
      onCancel={() => setAudit(null)} footer={<Button onClick={() => setAudit(null)}>关闭</Button>}>
      <Table size="small" rowKey="ID" dataSource={audit?.events || []} columns={[
        { title: '操作人', dataIndex: 'ActorUsername' },
        { title: '目标账号', dataIndex: 'SubjectUsername' },
        { title: '操作', dataIndex: 'Action', render: action => ACTION_LABELS[action] || action },
        { title: '时间', dataIndex: 'CreatedAt', render: value => new Date(value).toLocaleString() }
      ]} />
    </Modal>
  </section>;
}
