import { Alert, Avatar, Button, Input, Modal, Spin, message } from 'antd';
import { ChartNoAxesCombined, ChevronLeft, Crown, ExternalLink, KeyRound, LogOut, ShieldCheck, UserRound, UsersRound } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { getAccountImpersonation, startAccountImpersonation, stopAccountImpersonation } from '../api/accountImpersonation.js';
import { setEffectiveImpersonationAccount } from '../api/auth';
import { getToken } from '../api/client';
import { Link } from '../components/Link';
import '../styles/profile-grey-glass.css';

function roleLabel(role) {
  if (role === 'dev') return 'DEV';
  if (role === 'manager') return 'MANAGER';
  return 'MEMBER';
}

const switchChannel = 'qiantie:account-impersonation';

function openSwitchChannel() {
  try {
    return typeof BroadcastChannel === 'function' ? new BroadcastChannel(switchChannel) : null;
  } catch {
    return null;
  }
}

function reloadForAccountSwitch() {
  const channel = openSwitchChannel();
  channel?.postMessage('changed');
  channel?.close();
  window.location.reload();
}

export function useAccountImpersonation(enabled) {
  const token = enabled ? getToken() : '';
  const [snapshot, setSnapshot] = useState({ token: '', status: null, error: '' });
  const refreshRef = useRef(null);
  useEffect(() => {
    if (!token) return undefined;
    const channel = openSwitchChannel();
    let currentRequest;
    let currentId;
    let timer;

    function reload() {
      window.location.reload();
    }

    function onSessionChange(event) {
      if (currentId && event.key === 'auth_token' && event.oldValue === token) reload();
    }

    async function refresh() {
      clearTimeout(timer);
      currentRequest?.abort();
      const request = new AbortController();
      currentRequest = request;
      setSnapshot(previous => previous.token === token && previous.error ? { ...previous, error: '' } : previous);
      try {
        const next = await getAccountImpersonation(request.signal);
        if (request.signal.aborted || getToken() !== token) return;
        if (typeof next?.active !== 'boolean' || typeof next.canSwitch !== 'boolean' || (next.active && (!next.impersonationId || !next.subject?.username))) {
          throw new Error('无法确认账号切换状态');
        }
        const nextId = next.impersonationId || '';
        if (currentId !== undefined && currentId !== nextId) {
          // Keep the old write expectation until the old page is unloaded.
          reload();
          return;
        }
        setEffectiveImpersonationAccount(next.active ? { username: next.subject.username, impersonationId: nextId } : null, token);
        currentId = nextId;
        setSnapshot({ token, status: next, error: '' });
        if (next.active) timer = setTimeout(refresh, Math.min(30, next.expiresInSeconds || 1) * 1000);
      } catch {
        if (request.signal.aborted) return;
        if (getToken() !== token) {
          setSnapshot({ token: '', status: null, error: '' });
          return;
        }
        setSnapshot(previous => ({ token, status: previous.token === token ? previous.status : null, error: '无法确认账号切换状态' }));
        if (currentId) timer = setTimeout(refresh, 5000);
      }
    }

    refreshRef.current = refresh;
    channel?.addEventListener('message', reload);
    window.addEventListener('focus', refresh);
    window.addEventListener('storage', onSessionChange);
    void refresh();
    return () => {
      refreshRef.current = null;
      window.removeEventListener('focus', refresh);
      window.removeEventListener('storage', onSessionChange);
      channel?.close();
      clearTimeout(timer);
      currentRequest?.abort();
    };
  }, [token]);
  return {
    status: snapshot.token === token ? snapshot.status : null,
    error: snapshot.token === token ? snapshot.error : '',
    retry: () => refreshRef.current?.()
  };
}

export function AccountImpersonationPending({ error, onRetry }) {
  return <div style={{ padding: 24 }}>
    {error
      ? <Alert type="error" showIcon message={error} action={<Button aria-label="重试确认账号切换状态" onClick={onRetry}>重试</Button>} />
      : <div role="status" aria-live="polite"><Spin size="small" /> 正在确认账号切换状态</div>}
  </div>;
}

export function AccountImpersonationBanner({ status }) {
  const [busy, setBusy] = useState(false);
  if (!status?.active || !status.subject?.username) return null;

  async function exit() {
    setBusy(true);
    try {
      await stopAccountImpersonation();
      // Until reload replaces the old page, its in-flight actions still expect this switch.
      reloadForAccountSwitch();
    } catch (error) {
      message.error(error.message || '退出账号切换失败');
      setBusy(false);
    }
  }

  return <div role="region" aria-label="账号切换状态" style={{ position: 'fixed', top: 8, left: '50%', transform: 'translateX(-50%)', zIndex: 1100, maxWidth: 'calc(100vw - 16px)' }}>
    <Alert type="warning" showIcon message={`正在以 ${status.subject.username} 操作`} action={<Button size="small" loading={busy} onClick={exit}>退出切换</Button>} />
  </div>;
}

function AccountSwitchEntry({ status }) {
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  if (!status?.canSwitch || status.active) return null;

  function openDialog() {
    setTarget('');
    setError('');
    setOpen(true);
  }

  async function submit() {
    if (busy || !target.trim()) return;
    const token = getToken();
    setBusy(true);
    setError('');
    try {
      const next = await startAccountImpersonation(target.trim());
      setEffectiveImpersonationAccount({ username: next.subject?.username, impersonationId: next.impersonationId }, token);
      reloadForAccountSwitch();
    } catch (failure) {
      setError(failure.message || '账号切换失败');
      setBusy(false);
    }
  }

  return <>
    <Button size="small" onClick={openDialog}>切换账号</Button>
    <Modal title="切换账号" open={open} okText="切换" cancelText="取消" confirmLoading={busy} okButtonProps={{ disabled: !target.trim() }} onOk={submit} onCancel={() => { if (!busy) setOpen(false); }}>
      <Input autoComplete="off" aria-label="目标账号" placeholder="输入目标账号用户名" maxLength={32} value={target} onChange={event => setTarget(event.target.value)} onPressEnter={submit} />
      {error ? <Alert type="error" message={error} style={{ marginTop: 12 }} /> : null}
    </Modal>
  </>;
}

export function AccountCenterLayout({ account, username, avatar, pathname, onReturn, onLogout, impersonation, impersonationRequired = Boolean(username), impersonationError, onRetryImpersonation, children }) {
  const canManage = ['dev', 'manager'].includes(account?.role);
  const canAccessAdmin = account?.role === 'dev';
  const content = impersonationRequired && !impersonation
    ? <AccountImpersonationPending error={impersonationError} onRetry={onRetryImpersonation} />
    : children;

  // Roll out the new shell only to Profile; other account pages keep their layout.
  if (pathname === '/profile') {
    const entries = [
      { href: '/profile', label: '个人资料' },
      { href: '/member', label: '会员中心' },
      { href: '/security', label: '账号安全' },
      ...(canManage ? [{ href: '/advanced-team-admin', label: '联合治理' }, { href: '/api-config', label: 'API 配置' }] : []),
      { href: 'http://newapi.yzcm.site/', label: 'YZCM API', external: true },
      { href: '/usage', label: '用量与制作' },
      ...(canManage ? [{ href: '/team', label: '组员管理' }] : []),
      ...(canAccessAdmin ? [{ href: '/accounts', label: '账号与角色', developer: true }, { href: '/admin/presets', label: '管理后台', reload: true, developer: true }] : [])
    ];
    return <div className="account-center-workspace profile-glass-workspace">
      <header className="pg-shell-header">
        <button type="button" className="pg-brand" onClick={onReturn} aria-label="一战晟铭，返回工作台"><b aria-hidden="true">Y</b><span>一战晟铭</span></button>
        <div className="pg-shell-account"><Avatar size={32} src={account?.avatarUrl} style={{ backgroundColor: avatar?.background }}>{avatar?.emoji}</Avatar><span>{account?.displayName || username}</span><em>{roleLabel(account?.role)}</em><AccountSwitchEntry status={impersonation} /></div>
      </header>
      <AccountImpersonationBanner status={impersonation} />
      <nav className="pg-navigation" aria-label="个人中心导航">
        {entries.map(entry => entry.external
          ? <a key={entry.href} href={entry.href} target="_blank" rel="noreferrer" title="一战晟铭专属 API 控制台">{entry.label}<ExternalLink size={12} aria-hidden="true" /></a>
          : <Link key={entry.href} href={entry.href} reload={entry.reload} aria-current={pathname === entry.href ? 'page' : undefined} className={pathname === entry.href ? 'active' : ''}>{entry.label}{entry.developer ? <small>DEV</small> : null}</Link>)}
      </nav>
      <main className="account-center-main pg-main">{content}</main>
      <footer className="pg-shell-footer"><button type="button" onClick={onReturn} aria-label="返回工作台"><ChevronLeft size={16} />返回工作台</button><button type="button" onClick={onLogout}><LogOut size={16} />退出登录</button></footer>
    </div>;
  }

  return <div className="account-center-workspace">
    <AccountImpersonationBanner status={impersonation} />
    <aside className="account-center-sidebar" aria-label="个人中心导航">
      <div className="account-center-sidebar-head">
        <button type="button" className="account-center-return" aria-label="返回工作台" onClick={onReturn}><ChevronLeft size={19} /><span>返回工作台</span></button>
        <strong>个人中心</strong>
        <AccountSwitchEntry status={impersonation} />
      </div>
      <div className="account-center-sidebar-profile">
        <Avatar size={54} src={account?.avatarUrl} style={{ backgroundColor: avatar?.background }}>{avatar?.emoji}</Avatar>
        <div><strong>{account?.displayName || username}</strong><small>@{username}</small><em>{roleLabel(account?.role)}</em></div>
      </div>
      <nav className="account-center-sidebar-nav">
        <div className="account-center-sidebar-group">
          <Link href="/profile" className={pathname === '/profile' ? 'active' : ''} aria-current={pathname === '/profile' ? 'page' : undefined}><UserRound size={18} />个人资料</Link>
          <div className="account-center-sidebar-submenu"><Link href="/member" className={pathname === '/member' ? 'active' : ''} aria-current={pathname === '/member' ? 'page' : undefined}><Crown size={17} />会员中心</Link><Link href="/security" className={pathname === '/security' ? 'active' : ''} aria-current={pathname === '/security' ? 'page' : undefined}><ShieldCheck size={17} />账号安全</Link>{canManage ? <Link href="/advanced-team-admin" className={pathname === '/advanced-team-admin' ? 'active' : ''} aria-current={pathname === '/advanced-team-admin' ? 'page' : undefined}><UsersRound size={17} />联合治理</Link> : null}</div>
        </div>
        <div className="account-center-sidebar-group">{canManage ? <Link href="/api-config" className={pathname === '/api-config' ? 'active' : ''} aria-current={pathname === '/api-config' ? 'page' : undefined}><KeyRound size={17} />API 配置</Link> : null}<a href="http://newapi.yzcm.site/" target="_blank" rel="noreferrer" title="一战晟铭专属 API 控制台"><ExternalLink size={17} />YZCM API</a><Link href="/usage" className={pathname === '/usage' ? 'active' : ''} aria-current={pathname === '/usage' ? 'page' : undefined}><ChartNoAxesCombined size={17} />用量与制作</Link>{canManage ? <Link href="/team" className={pathname === '/team' ? 'active' : ''} aria-current={pathname === '/team' ? 'page' : undefined}><UsersRound size={17} />组员管理</Link> : null}</div>
        {canAccessAdmin ? <div className="account-center-sidebar-group account-center-sidebar-developer"><span>开发者工具</span><Link href="/accounts" className={pathname === '/accounts' ? 'active' : ''} aria-current={pathname === '/accounts' ? 'page' : undefined}><UsersRound size={17} />账号与角色</Link><Link href="/admin/presets" reload><ShieldCheck size={17} />管理后台</Link></div> : null}
      </nav>
      <button type="button" className="account-center-sidebar-logout" onClick={onLogout}><LogOut size={17} />退出登录</button>
    </aside>
    <main className="account-center-main">{content}</main>
  </div>;
}
