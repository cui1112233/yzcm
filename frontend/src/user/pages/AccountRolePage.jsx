import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Card, Empty, Form, Input, Modal, Space, Table, Tag, Typography } from 'antd';
import { Activity, RefreshCw, Search, UsersRound, WalletCards } from 'lucide-react';
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
  const [query, setQuery] = useState('');
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
    { title: '角色', dataIndex: 'role', render: role => <RoleBadge canonicalRole={role} compact /> },
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
  const monthConsumption = allAccounts.reduce((total, account) => total + (Number(account.monthConsumption) || 0), 0);
  const filteredTeams = directory ? directory.teams.map(team => ({
    ...team,
    accounts: normalizedQuery
      ? (team.accounts || []).filter(account => [account.username, account.displayName, ROLE_LABELS[account.role], team.name].some(value => String(value || '').toLowerCase().includes(normalizedQuery)))
      : (team.accounts || [])
  })).filter(team => !normalizedQuery || team.name.toLowerCase().includes(normalizedQuery) || team.accounts.length) : [];
  const filteredUngrouped = normalizedQuery
    ? ungrouped.filter(account => [account.username, account.displayName, ROLE_LABELS[account.role]].some(value => String(value || '').toLowerCase().includes(normalizedQuery)))
    : ungrouped;

  return <section className={`account-center-page account-role-page ${teamsOnly ? 'pc-team' : 'pc-accounts'}`}>
    <PageHeader title={title} subtitle={teamsOnly ? '管理团队、成员与团队能量使用范围' : '按团队查看账号、身份与能量消耗'} actions={<Button icon={<RefreshCw size={15} />} onClick={load} loading={loading}>刷新</Button>} />
    <div className="pc-governance-summary">
      <div><span><UsersRound size={20} /></span><small>团队数量</small><strong>{teams.length}</strong></div>
      <div><span><Activity size={20} /></span><small>启用账号</small><strong>{activeAccounts}</strong></div>
      <div><span><WalletCards size={20} /></span><small>本月总消耗</small><strong>{monthConsumption || '—'}</strong></div>
    </div>
    <div className="pc-governance-filter">
      <Input prefix={<Search size={16} />} allowClear value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索团队、账号、名称或身份" />
      <div className="pc-governance-actions"><Space wrap>{directory?.actions.filter(action => !teamsOnly || !['create_manager', 'create_member'].includes(action)).map(action => actionButton(action))}</Space></div>
    </div>
    {error && <Alert type="error" showIcon message={error} />}
    {directory && <div className="pc-governance-directory">
      {filteredTeams.map(team => <Card className="account-role-team-card" key={team.id}
        title={<Space>{team.name}{team.archivedAt && <Tag>已归档</Tag>}</Space>}
        extra={<Space wrap>{team.actions.map(action => actionButton(action, team))}</Space>}>
        <Typography.Paragraph type="secondary">负责人 @{team.managerUsername}</Typography.Paragraph>
        <Table rowKey={account => account.membershipId || account.username} columns={columns} dataSource={team.accounts} pagination={false} scroll={{ x: 820 }} />
      </Card>)}
      {!teamsOnly && filteredUngrouped.length > 0 && <Card className="account-role-team-card" title="未分组">
        <Table rowKey="username" columns={columns} dataSource={filteredUngrouped} pagination={false} scroll={{ x: 820 }} />
      </Card>}
      {filteredTeams.length === 0 && (teamsOnly || filteredUngrouped.length === 0) && <Empty description={normalizedQuery ? '没有找到匹配的团队或账号' : '暂无可管理的团队'} />}
    </div>}
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
