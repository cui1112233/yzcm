const ROLE_VARIANTS = {
  founder: { label: '开发创始', tone: 'founder', nodes: 4 },
  '开发创始': { label: '开发创始', tone: 'founder', nodes: 4 },
  developer: { label: '开发者', tone: 'developer', nodes: 3 },
  '开发者': { label: '开发者', tone: 'developer', nodes: 3 },
  manager: { label: '管理者', tone: 'manager', nodes: 2 },
  '管理者': { label: '管理者', tone: 'manager', nodes: 2 },
  member: { label: '组员', tone: 'member', nodes: 1 },
  '组员': { label: '组员', tone: 'member', nodes: 1 }
};

export default function RoleEnergyIcon({ role, size = 26, decorative = false, className = '' }) {
  const meta = ROLE_VARIANTS[role] || ROLE_VARIANTS.member;
  return <span
    className={`role-energy-icon role-energy-icon-${meta.tone} ${className}`.trim()}
    style={{ '--role-icon-size': `${size}px` }}
    aria-hidden={decorative || undefined}
    aria-label={decorative ? undefined : `${meta.label}身份图标`}
    title={decorative ? undefined : meta.label}
  >
    <svg viewBox="0 0 48 48" focusable="false" aria-hidden="true">
      <path className="role-energy-orbit" d="M6 25c5-9 13-14 23-13 6 1 10 4 13 9-5 9-13 14-23 13-6-1-10-4-13-9Z" />
      <path className="role-energy-mark" d="m8 24 10-8 6 8 6-8 10 8-10 8-6-8-6 8-10-8Z" />
      <path className="role-energy-core" d="m24 14 6 10-6 10-6-10 6-10Z" />
      {Array.from({ length: meta.nodes }, (_, index) => {
        const positions = [[24, 6], [42, 24], [24, 42], [6, 24]];
        const [cx, cy] = positions[index];
        return <circle className="role-energy-node" cx={cx} cy={cy} r="2" key={`${cx}-${cy}`} />;
      })}
    </svg>
  </span>;
}
