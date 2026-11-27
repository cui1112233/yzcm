import { Activity, BarChart3, Boxes, ChevronDown, CircleUserRound, Code2, CreditCard, ExternalLink, FileKey2, Gauge, Home, KeyRound, Layers3, LockKeyhole, Mail, MonitorSmartphone, ReceiptText, Search, Settings, ShieldCheck, Sparkles, Trophy, UserRound, UsersRound, Zap } from 'lucide-react';

const ICONS = {
  activity: Activity, accounts: CircleUserRound, api: Code2, billing: ReceiptText,
  cards: CreditCard, collapse: ChevronDown, devices: MonitorSmartphone, energy: Zap,
  external: ExternalLink, home: Home, key: KeyRound, layers: Layers3, lock: LockKeyhole,
  mail: Mail, metrics: Gauge, models: Boxes, profile: UserRound, ranking: Trophy,
  search: Search, security: ShieldCheck, settings: Settings, spark: Sparkles,
  team: UsersRound, trend: BarChart3, verify: FileKey2
};

export default function PersonalCenterIcon({ name, size = 20, strokeWidth = 1.8, className = '', ...props }) {
  const Icon = ICONS[name] || Sparkles;
  return <Icon className={`pc-icon ${className}`.trim()} size={size} strokeWidth={strokeWidth} aria-hidden="true" {...props} />;
}
