import { Avatar, Progress } from 'antd';
import { canonicalRoleLabel } from './accountCenterMemberState.js';
import RoleEnergyIcon from '../components/RoleEnergyIcon.jsx';
import PersonalCenterIcon from '../components/PersonalCenterIcon.jsx';

const ROLE_META = {
  开发创始: { style: 'dev' },
  开发者: { style: 'dev' },
  管理者: { style: 'manager' },
  组员: { style: 'member' }
};

export const FEATURE_LABELS = {
  model: '模型调用',
  audio: '配音与音频',
  canvas: 'CM 画布',
  comic: '漫剧制作',
  'novel-creation': '小说创作',
  'novel-fetch': '批量测书',
  'shuihuo-production': '批量制作',
  aicut: 'AI-CUT',
  agent: 'CM 画布',
  chat: '对话生成',
  script: '私人定制',
  image: '生图工具',
  tts: '配音',
  unknown: '其他'
};

export function formatTokens(value) {
  const number = Number(value) || 0;
  if (number >= 1_000_000_000) return `${(number / 1_000_000_000).toFixed(number >= 10_000_000_000 ? 0 : 2)}B`;
  if (number >= 1_000_000) return `${(number / 1_000_000).toFixed(number >= 10_000_000 ? 1 : 2)}M`;
  if (number >= 1_000) return `${(number / 1_000).toFixed(number >= 100_000 ? 0 : 1)}K`;
  return String(number);
}

export function formatDate(value, fallback = '—') {
  if (!value) return fallback;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return fallback;
  return date.toLocaleString('zh-CN', { hour12: false, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

export function avatarFallback(member) {
  return String(member?.displayName || member?.username || '?').trim().slice(0, 1).toUpperCase();
}

export function RoleBadge({ canonicalRole, compact = false }) {
  const label = canonicalRoleLabel(canonicalRole);
  const meta = ROLE_META[label];
  return <span className={`member-role-badge${meta ? ` role-${meta.style}` : ''}${compact ? ' compact' : ''}`}>
    <RoleEnergyIcon role={label} size={compact ? 18 : 22} decorative />
    {label}
  </span>;
}

export function PageHeader({ title, subtitle, actions }) {
  const icon = ({ '账户总览': 'home', '个人资料': 'profile', '能量与账单': 'layers', '全平台团队消耗': 'trend', '账号安全': 'security', '团队管理': 'team', '账号与角色': 'accounts', 'API 配置': 'api' })[title];
  return <div className="ac-page-header">
    <div>{icon ? <PersonalCenterIcon name={icon} /> : null}<h1>{title}</h1>{subtitle ? <p>{subtitle}</p> : null}</div>
    {actions ? <div className="ac-page-actions">{actions}</div> : null}
  </div>;
}

export function MetricCard({ label, value, suffix, icon: Icon, accent = 'violet', hint }) {
  return <section className={`ac-metric-card accent-${accent}`}>
    <div className="ac-metric-top"><span>{label}</span>{Icon ? <i><Icon size={18} /></i> : null}</div>
    <div className="ac-metric-value"><strong>{value}</strong>{suffix ? <span>{suffix}</span> : null}</div>
    {hint ? <small>{hint}</small> : null}
    <div className="ac-sparkline" aria-hidden="true"><span /><span /><span /><span /><span /><span /></div>
  </section>;
}

export function Panel({ title, eyebrow, action, className = '', children }) {
  return <section className={`ac-panel ${className}`.trim()}>
    {(title || eyebrow || action) ? <header className="ac-panel-header">
      <div>{eyebrow ? <small>{eyebrow}</small> : null}{title ? <h2>{title}</h2> : null}</div>
      {action ? <div>{action}</div> : null}
    </header> : null}
    {children}
  </section>;
}

export function MemberIdentity({ member, size = 38, showUsername = true }) {
  return <div className="ac-member-identity">
    <Avatar size={size} src={member?.avatarUrl}>{avatarFallback(member)}</Avatar>
    <div><strong>{member?.displayName || member?.username}</strong>{showUsername ? <small>@{member?.username}</small> : null}</div>
  </div>;
}

export function FeatureBars({ byFeature = {}, total = 0, limit = 6 }) {
  const entries = Object.entries(byFeature)
    .filter(([, value]) => Number(value) > 0)
    .sort((a, b) => b[1] - a[1])
    .slice(0, limit);
  if (!entries.length) return <div className="ac-empty">本月还没有可统计的用量</div>;
  return <div className="ac-feature-bars">
    {entries.map(([key, value], index) => {
      const percent = total ? Math.max(2, Math.round((Number(value) / total) * 100)) : 0;
      return <div className="ac-feature-row" key={key}>
        <span className={`ac-feature-dot tone-${index % 5}`} />
        <div><strong>{FEATURE_LABELS[key] || key}</strong><small>{formatTokens(value)} Tokens</small></div>
        <Progress percent={percent} showInfo={false} strokeLinecap="round" />
        <b>{percent}%</b>
      </div>;
    })}
  </div>;
}
