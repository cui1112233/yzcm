import { Alert, Avatar, Button, Spin, message } from 'antd';
import { useEffect, useRef, useState } from 'react';
import { getAccountImpersonation, stopAccountImpersonation } from '../api/accountImpersonation.js';
import { ENERGY_CONSOLE_URL } from '../api/accountCenterEnergy.js';
import { getPageIdentityGeneration } from '../api/accountImpersonationState.js';
import { setEffectiveImpersonationAccount } from '../api/auth';
import { getToken } from '../api/client';
import { Link } from '../components/Link';
import PersonalCenterIcon from '../../user/components/PersonalCenterIcon.jsx';
import { EnergyScene } from '../../user/components/PersonalCenterVisuals.jsx';
import RoleEnergyIcon from '../../user/components/RoleEnergyIcon.jsx';
import brandLogoNeon from '../../user/assets/personal-center/brand-logo-neon.png';
import AccountSwitchMenu from './AccountSwitchMenu.jsx';

const switchChannel = 'qiantie:account-impersonation';
const PAGE_TITLES = {
  '/account': '账户总览',
  '/profile': '个人资料',
  '/usage': '能量与账单',
  '/security': '账号安全',
  '/team': '团队管理',
  '/api-config': 'API 配置',
  '/accounts': '账号与角色'
};

function openSwitchChannel() {
  try {
    return typeof BroadcastChannel === 'function' ? new BroadcastChannel(switchChannel) : null;
  } catch {
    return null;
  }
}

export function reloadForAccountSwitch() {
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
    let confirmedSwitch = false;
    let timer;

    function reload() {
      window.location.reload();
    }

    function onSessionChange(event) {
      if (event.key === 'auth_token' && event.oldValue === token) reload();
    }

    async function refresh() {
      clearTimeout(timer);
      currentRequest?.abort();
      const request = new AbortController();
      const generation = getPageIdentityGeneration();
      currentRequest = request;
      setSnapshot(previous => previous.token === token && previous.error ? { ...previous, error: '' } : previous);
      try {
        const next = await getAccountImpersonation(request.signal);
        if (request.signal.aborted || generation !== getPageIdentityGeneration() || getToken() !== token) return;
        if (!next?.actorUsername || typeof next.active !== 'boolean' || typeof next.canSwitch !== 'boolean' ||
          (next.active && (!next.impersonationId || !next.subject?.username))) {
          throw new Error('无法确认账号切换状态');
        }
        const accepted = setEffectiveImpersonationAccount({
          actorUsername: next.actorUsername,
          username: next.active ? next.subject.username : next.actorUsername,
          impersonationId: next.active ? next.impersonationId : 'none'
        }, token, generation);
        if (!accepted) {
          reload();
          return;
        }
        confirmedSwitch = next.active;
        setSnapshot({ token, status: next, error: '' });
        if (next.active) timer = setTimeout(refresh, Math.min(30, next.expiresInSeconds || 1) * 1000);
      } catch {
        if (request.signal.aborted || generation !== getPageIdentityGeneration()) return;
        if (getToken() !== token) {
          setSnapshot({ token: '', status: null, error: '' });
          return;
        }
        setSnapshot(previous => ({ token, status: previous.token === token ? previous.status : null, error: '无法确认账号切换状态' }));
        if (confirmedSwitch) timer = setTimeout(refresh, 5000);
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

export function AccountCenterLayout({ account, username, avatar, pathname, onReturn, onLogout, impersonation, impersonationRequired = Boolean(username), impersonationError, onRetryImpersonation, children }) {
  const capabilities = account?.capabilities || [];
  const canManage = capabilities.includes('team_governance');
  const canGovernAccounts = capabilities.includes('account_governance');
  const canAccessAdmin = capabilities.includes('platform_admin');
  const usageTitle = canGovernAccounts ? '全平台消耗' : '能量与账单';
  const content = impersonationRequired && !impersonation
    ? <AccountImpersonationPending error={impersonationError} onRetry={onRetryImpersonation} />
    : children;

  return <div className="pc-shell">
    <EnergyScene />
    <AccountImpersonationBanner status={impersonation} />
    <header className="pc-topbar">
      <button type="button" className="pc-brand" onClick={onReturn} aria-label="一战晟铭，返回工作台"><img src={brandLogoNeon} alt="" /><strong>一战晟铭</strong></button>
      <div className="pc-breadcrumb"><span>个人中心</span><b>·</b><strong>{pathname === '/usage' ? usageTitle : (PAGE_TITLES[pathname] || '个人中心')}</strong></div>
      <div className="pc-top-account"><Avatar size={34} src={account?.avatarUrl} style={{ backgroundColor: avatar?.background }}>{avatar?.emoji}</Avatar><strong>{account?.displayName || username}</strong><RoleEnergyIcon role={account?.canonicalRole} /><em>{account?.canonicalRole}</em><span className="pc-online"><i />当前身份：{account?.canonicalRole}</span><AccountSwitchMenu status={impersonation} onSwitched={reloadForAccountSwitch} /></div>
    </header>
    <aside className="pc-sidebar" aria-label="个人中心导航">
      <div className="pc-sidebar-profile">
        <Avatar size={92} src={account?.avatarUrl} style={{ backgroundColor: avatar?.background }}>{avatar?.emoji}</Avatar>
        <div><span className="pc-sidebar-name"><strong>{account?.displayName || username}</strong><RoleEnergyIcon role={account?.canonicalRole} size={26} /></span><small>@{username}</small><em>{account?.canonicalRole}</em></div>
      </div>
      <nav className="pc-sidebar-nav">
        <div className="pc-sidebar-group">
          <Link href="/account" className={pathname === '/account' ? 'active' : ''} aria-current={pathname === '/account' ? 'page' : undefined}><PersonalCenterIcon name="home" />账户总览</Link>
          <Link href="/profile" className={pathname === '/profile' ? 'active' : ''} aria-current={pathname === '/profile' ? 'page' : undefined}><PersonalCenterIcon name="profile" />个人资料</Link>
          <Link href="/usage" className={pathname === '/usage' ? 'active' : ''} aria-current={pathname === '/usage' ? 'page' : undefined}><PersonalCenterIcon name={canGovernAccounts ? 'trend' : 'layers'} />{usageTitle}</Link>
          <Link href="/security" className={pathname === '/security' ? 'active' : ''} aria-current={pathname === '/security' ? 'page' : undefined}><PersonalCenterIcon name="security" />账号安全</Link>
        </div>
        <div className="pc-sidebar-group">{canManage ? <Link href="/team" className={pathname === '/team' ? 'active' : ''} aria-current={pathname === '/team' ? 'page' : undefined}><PersonalCenterIcon name="team" />团队管理</Link> : null}{canManage ? <Link href="/api-config" className={pathname === '/api-config' ? 'active' : ''} aria-current={pathname === '/api-config' ? 'page' : undefined}><PersonalCenterIcon name="api" />API 配置</Link> : null}<a href={ENERGY_CONSOLE_URL} target="_blank" rel="noreferrer" title="一战晟铭专属 API 控制台"><PersonalCenterIcon name="external" />YZCM API</a></div>
        {canGovernAccounts || canAccessAdmin ? <div className="pc-sidebar-group pc-sidebar-developer">
          <span>开发者工具</span>
          {canGovernAccounts && <Link href="/accounts" className={pathname === '/accounts' ? 'active' : ''} aria-current={pathname === '/accounts' ? 'page' : undefined}><PersonalCenterIcon name="accounts" />账号与角色</Link>}
          {canAccessAdmin && <Link href="/admin/presets" reload><PersonalCenterIcon name="settings" />管理后台</Link>}
        </div> : null}
      </nav>
      <div className="pc-sidebar-actions"><button type="button" aria-label="返回工作台" onClick={onReturn}><PersonalCenterIcon name="home" />返回工作台</button><button type="button" className="pc-logout" aria-label="退出登录" onClick={onLogout}><PersonalCenterIcon name="lock" />退出登录</button></div>
    </aside>
    <main className="pc-main">{content}</main>
  </div>;
}
