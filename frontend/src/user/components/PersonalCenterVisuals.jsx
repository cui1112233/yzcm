import brandEnergyMark from '../assets/personal-center/brand-energy-mark.webp';
import energySpace from '../assets/personal-center/energy-space.webp';
import PersonalCenterIcon from './PersonalCenterIcon.jsx';

export function EnergyScene({ compact = false }) {
  return <div className={`pc-energy-scene ${compact ? 'is-compact' : ''}`} aria-hidden="true">
    <div className="pc-energy-far"><img className="pc-energy-scene-space" src={energySpace} alt="" draggable="false" /></div>
    <div className="pc-energy-flow"><img className="pc-energy-scene-mark" src={brandEnergyMark} alt="" draggable="false" /></div>
    <div className="pc-energy-near">
      {Array.from({ length: 8 }, (_, index) => <i key={index} className={`pc-energy-particle pc-energy-particle-${index + 1}`} />)}
    </div>
  </div>;
}

export function GlassPanel({ as: Tag = 'section', className = '', title, icon, actions, children, ...props }) {
  return <Tag className={`pc-glass ${className}`.trim()} {...props}>
    {(title || actions) ? <header className="pc-glass-head">
      <div className="pc-glass-title">{icon ? <PersonalCenterIcon name={icon} /> : null}{title ? <h2>{title}</h2> : null}</div>
      {actions ? <div className="pc-glass-actions">{actions}</div> : null}
    </header> : null}
    <div className="pc-glass-body">{children}</div>
  </Tag>;
}

export function MetricTile({ icon = 'metrics', label, value, suffix, tone = 'cyan', className = '' }) {
  return <div className={`pc-metric pc-metric-${tone} ${className}`.trim()}>
    <span className="pc-metric-icon"><PersonalCenterIcon name={icon} size={24} /></span>
    <span className="pc-metric-copy"><small>{label}</small><strong>{value ?? '暂不可用'}</strong>{suffix ? <em>{suffix}</em> : null}</span>
  </div>;
}

export function DesignedEmptyState({ icon = 'spark', title = '暂无数据', description = '数据准备好后会显示在这里。', action }) {
  return <div className="pc-empty" role="status">
    <span className="pc-empty-icon"><PersonalCenterIcon name={icon} size={30} /></span>
    <strong>{title}</strong>
    <p>{description}</p>
    {action ? <div className="pc-empty-action">{action}</div> : null}
  </div>;
}
