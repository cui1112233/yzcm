import developerBadge from '../assets/personal-center/role-developer.png';
import developerBadge24 from '../assets/personal-center/role-developer-24.png';
import developerBadge32 from '../assets/personal-center/role-developer-32.png';
import developerBadge48 from '../assets/personal-center/role-developer-48.png';
import developerBadge64 from '../assets/personal-center/role-developer-64.png';
import founderBadge from '../assets/personal-center/role-founder.png';
import founderBadge24 from '../assets/personal-center/role-founder-24.png';
import founderBadge32 from '../assets/personal-center/role-founder-32.png';
import founderBadge48 from '../assets/personal-center/role-founder-48.png';
import founderBadge64 from '../assets/personal-center/role-founder-64.png';
import managerBadge from '../assets/personal-center/role-manager.png';
import managerBadge24 from '../assets/personal-center/role-manager-24.png';
import managerBadge32 from '../assets/personal-center/role-manager-32.png';
import managerBadge48 from '../assets/personal-center/role-manager-48.png';
import managerBadge64 from '../assets/personal-center/role-manager-64.png';
import memberBadge from '../assets/personal-center/role-member.png';
import memberBadge24 from '../assets/personal-center/role-member-24.png';
import memberBadge32 from '../assets/personal-center/role-member-32.png';
import memberBadge48 from '../assets/personal-center/role-member-48.png';
import memberBadge64 from '../assets/personal-center/role-member-64.png';

const ROLE_VARIANTS = {
  founder: { label: '开发创始', tone: 'founder' },
  '开发创始': { label: '开发创始', tone: 'founder' },
  developer: { label: '开发者', tone: 'developer' },
  '开发者': { label: '开发者', tone: 'developer' },
  manager: { label: '管理者', tone: 'manager' },
  '管理者': { label: '管理者', tone: 'manager' },
  member: { label: '组员', tone: 'member' },
  '组员': { label: '组员', tone: 'member' }
};

const ROLE_ASSETS = {
  founder: { src: founderBadge, srcSet: `${founderBadge24} 24w, ${founderBadge32} 32w, ${founderBadge48} 48w, ${founderBadge64} 64w` },
  developer: { src: developerBadge, srcSet: `${developerBadge24} 24w, ${developerBadge32} 32w, ${developerBadge48} 48w, ${developerBadge64} 64w` },
  manager: { src: managerBadge, srcSet: `${managerBadge24} 24w, ${managerBadge32} 32w, ${managerBadge48} 48w, ${managerBadge64} 64w` },
  member: { src: memberBadge, srcSet: `${memberBadge24} 24w, ${memberBadge32} 32w, ${memberBadge48} 48w, ${memberBadge64} 64w` }
};

export default function RoleEnergyIcon({ role, size = 26, decorative = false, animated = true, className = '' }) {
  const meta = ROLE_VARIANTS[role] || ROLE_VARIANTS.member;
  const asset = ROLE_ASSETS[meta.tone];
  return <span
    className={`role-energy-icon role-energy-icon-${meta.tone} ${animated ? 'is-animated' : ''} ${className}`.trim()}
    style={{ '--role-icon-size': `${size}px` }}
    data-role={meta.label}
    title={decorative ? undefined : meta.label}
  >
    <img
      src={asset.src}
      srcSet={asset.srcSet}
      sizes={`${size}px`}
      width={size}
      height={size}
      alt=""
      aria-hidden={decorative || undefined}
      aria-label={decorative ? undefined : `${meta.label}身份图标`}
      draggable="false"
    />
  </span>;
}
