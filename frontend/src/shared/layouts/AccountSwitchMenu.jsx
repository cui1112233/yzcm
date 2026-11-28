import { Alert, Avatar, Button, Dropdown, Empty, Input, Spin } from 'antd';
import { Search, Users } from 'lucide-react';
import { useMemo, useState } from 'react';
import { getAccountImpersonationTargets, startAccountImpersonation } from '../api/accountImpersonation.js';

const roleLabels = {
  founder: '开发创始',
  developer: '开发者',
  manager: '管理者',
  member: '组员'
};

function searchable(value) {
  return String(value || '').normalize('NFKC').trim().toLocaleLowerCase();
}

export function filterAccountSwitchGroups(groups, query) {
  const keyword = searchable(query);
  if (!keyword) return groups;
  return groups.map(group => ({
    ...group,
    accounts: (group.accounts || []).filter(account =>
      searchable(account.displayName).includes(keyword) || searchable(account.username).includes(keyword))
  })).filter(group => group.accounts.length > 0);
}

export default function AccountSwitchMenu({ status, onSwitched }) {
  const [open, setOpen] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [groups, setGroups] = useState([]);
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const visibleGroups = useMemo(() => filterAccountSwitchGroups(groups, query), [groups, query]);
  const visibleCount = visibleGroups.reduce((total, group) => total + group.accounts.length, 0);

  if (!status?.canSwitch || status.active) return null;

  async function loadTargets(force = false) {
    if (loading || loaded && !force) return;
    setLoading(true);
    setError('');
    try {
      const result = await getAccountImpersonationTargets();
      setGroups(Array.isArray(result?.groups) ? result.groups : []);
      setLoaded(true);
    } catch (failure) {
      setError(failure.message || '无法读取可切换账号');
    } finally {
      setLoading(false);
    }
  }

  function changeOpen(next) {
    if (busy) return;
    setOpen(next);
    if (next) void loadTargets();
    else {
      setQuery('');
      setSelected(null);
      setError('');
    }
  }

  async function confirmSwitch() {
    if (!selected || busy) return;
    setBusy(true);
    setError('');
    try {
      await startAccountImpersonation(selected.username);
      onSwitched?.();
    } catch (failure) {
      setError(failure.message || '账号切换失败');
      setBusy(false);
      setLoaded(false);
    }
  }

  const popup = <div className="pc-account-switch-menu" role="dialog" aria-label="选择要切换的账号">
    <header><span><Users size={17} />切换组员账号</span><small>{loaded ? `${groups.reduce((total, group) => total + group.accounts.length, 0)} 个可选账号` : '读取权限范围'}</small></header>
    <Input allowClear autoComplete="off" prefix={<Search size={15} />} placeholder="搜索组员名称或用户名" value={query} onChange={event => setQuery(event.target.value)} />
    <div className="pc-account-switch-results">
      {loading ? <div className="pc-account-switch-state"><Spin size="small" />正在读取可切换账号…</div> : null}
      {!loading && error ? <Alert type="error" showIcon message="无法读取可切换账号" description={error} action={<Button size="small" onClick={() => loadTargets(true)}>重试</Button>} /> : null}
      {!loading && !error && loaded && visibleCount === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={query ? '没有匹配的账号' : '暂无可切换账号'} /> : null}
      {!loading && !error ? visibleGroups.map(group => <section key={group.teamId || 'ungrouped'} className="pc-account-switch-group">
        <h3>{group.teamName || '未分组账号'}</h3>
        {(group.accounts || []).map(account => <button key={account.username} type="button" className={selected?.username === account.username ? 'selected' : ''} onClick={() => setSelected(account)}>
          <Avatar size={34} src={account.avatarUrl}>{String(account.displayName || account.username).slice(0, 1)}</Avatar>
          <span><strong>{account.displayName || account.username}</strong><small>@{account.username}</small></span>
          <em>{roleLabels[account.role] || account.role}</em>
        </button>)}
      </section>) : null}
    </div>
    <footer>
      <span>{selected ? `将切换到 ${selected.displayName || selected.username}` : '请选择一个账号'}</span>
      <Button type="primary" size="small" loading={busy} disabled={!selected} onClick={confirmSwitch}>确认切换</Button>
    </footer>
  </div>;

  return <Dropdown open={open} onOpenChange={changeOpen} trigger={['click']} menu={{ items: [] }} popupRender={() => popup} placement="bottomRight">
    <Button size="small" aria-expanded={open}>切换账号</Button>
  </Dropdown>;
}
