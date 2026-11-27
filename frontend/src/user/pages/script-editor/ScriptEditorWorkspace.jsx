import { Button, Empty, Tag, Tooltip } from 'antd';
import {
  ArrowLeft, BookOpen, Bot, Boxes, Clapperboard, FileText, Film,
  LayoutDashboard, PackageCheck, Settings, Sparkles,
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import ScriptEditorSource from './ScriptEditorSource.jsx';
import ScriptEditorScripts from './ScriptEditorScripts.jsx';
import ScriptEditorDelivery from './ScriptEditorDelivery.jsx';
import ScriptEditorAgent from './ScriptEditorAgent.jsx';
import { buildStudioSearch, resolveStudioLocation, SCRIPT_EDITOR_RAIL_ITEMS } from './studioContract.js';

const icons = {
  source: BookOpen,
  scripts: FileText,
  agent: Bot,
  assets: Boxes,
  storyboard: Clapperboard,
  canvas: LayoutDashboard,
  production: Film,
  delivery: PackageCheck,
  settings: Settings,
};

const panelCopy = {
  agent: ['作品 Agent', '这里会接入当前作品的会话、文件、Skill 和可审计写入。'],
  assets: ['资产工作台', '人物、场景、道具和导演台会共享当前作品身份。'],
  storyboard: ['分镜工作台', '按集管理镜头、覆盖率、素材绑定和生成提示词。'],
  canvas: ['CM 画布', 'CM 画布承接洛水 momo 画布，并与当前作品稳定绑定。'],
  production: ['生产工作台', '统一查看图片、视频、配音、字幕和剪辑任务。'],
  settings: ['作品设置', '保存当前作品的模型、风格、比例、并发和分镜密度。'],
};

function PendingPanel({ tab }) {
  const [title, description] = panelCopy[tab];
  return <section className="script-editor-stage-panel"><Empty image={<Sparkles size={40} />} description={<div><strong>{title}</strong><p>{description}</p></div>} /></section>;
}

export default function ScriptEditorWorkspace({ work, onBack, onReload }) {
  const [location, setLocation] = useState(() => resolveStudioLocation(window.location.search, work));

  useEffect(() => {
    const onPopState = () => setLocation(resolveStudioLocation(window.location.search, work));
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, [work]);

  useEffect(() => {
    const search = buildStudioSearch(location);
    if (window.location.search !== search) window.history.replaceState({}, '', `/script-editor${search}`);
  }, [location]);

  const counts = useMemo(() => ({
    chapters: work.chapters?.length || 0,
    scripts: work.scripts?.length || 0,
    words: work.chapters?.reduce((sum, row) => sum + Number(row.wordCount || 0), 0) || 0,
  }), [work]);

  function selectTab(tab) {
    setLocation(current => ({ ...current, workId: work.id, tab, itemId: '' }));
  }

  let content = <PendingPanel tab={location.tab} />;
  if (location.tab === 'source') content = <ScriptEditorSource work={work} onReload={onReload} />;
  if (location.tab === 'scripts') content = <ScriptEditorScripts work={work} onReload={onReload} />;
  if (location.tab === 'agent') content = <ScriptEditorAgent work={work} onReload={onReload} />;
  if (location.tab === 'delivery') content = <ScriptEditorDelivery work={work} />;

  return <main className="script-editor-page is-workspace">
    <div className="script-editor-studio">
      <nav className="script-editor-rail" aria-label="剧本编辑工作区">
        <Tooltip title="返回作品库" placement="right"><Button type="text" icon={<ArrowLeft size={18} />} onClick={onBack} /></Tooltip>
        <span className="script-editor-rail-divider" />
        {SCRIPT_EDITOR_RAIL_ITEMS.map(item => {
          const Icon = icons[item.key];
          return <Tooltip key={item.key} title={item.label} placement="right"><button type="button" aria-label={item.label} className={location.tab === item.key ? 'is-active' : ''} onClick={() => selectTab(item.key)}><Icon size={18} /><small>{item.label === 'CM 画布' ? '画布' : item.label}</small></button></Tooltip>;
        })}
      </nav>
      <section className="script-editor-studio-body">
        <header className="script-editor-studio-header">
          <div><span className="script-editor-kicker">SCRIPT STUDIO</span><h1>{work.title}</h1></div>
          <div className="script-editor-studio-stats"><Tag>{work.type === 'script' ? '剧本作品' : '小说作品'}</Tag><span>{counts.chapters} 章</span><span>{counts.scripts} 个版本</span><span>{counts.words} 字</span><span className="is-saved">已保存</span></div>
        </header>
        <div className="script-editor-studio-content">{content}</div>
      </section>
    </div>
  </main>;
}
