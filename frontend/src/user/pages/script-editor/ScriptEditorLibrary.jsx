import { Badge, Button, Dropdown, Empty, Input, Segmented, Select, Tag, Tooltip } from 'antd';
import { BookOpen, Ellipsis, FileInput, FileText, Grid2X2, List, Plus, Search } from 'lucide-react';
import { useState } from 'react';
import { filterAndSortWorks, markLabel, workTypeLabel } from './scriptEditorState.js';
import { buildWorkMenu } from './libraryModel.js';

export default function ScriptEditorLibrary({ works, filters, onFilters, view, onView, onCreate, onImport, onOpen, onAction, loading }) {
  const [scope, setScope] = useState('personal');
  const rows = filterAndSortWorks(works, filters);
  return <section className="script-editor-library">
    <header className="script-editor-titlebar">
      <div><span className="script-editor-kicker">SCRIPT WORKSPACE</span><h1>剧本编辑</h1><p>从原文整理、分集改编到正式版本交付，都在这里完成。</p></div>
      <div className="script-editor-primary-actions"><Segmented value={scope} onChange={setScope} options={[{ value: 'personal', label: '个人作品' }, { value: 'team', label: '团队作品' }]} /><Button icon={<FileInput size={16} />} onClick={onImport}>导入文档</Button><Button type="primary" icon={<Plus size={16} />} onClick={onCreate}>新建作品</Button></div>
    </header>
    <div className="script-editor-toolbar">
      <Input allowClear prefix={<Search size={15} />} placeholder="搜索作品名称" value={filters.query} onChange={e => onFilters({ ...filters, query: e.target.value })} />
      <Select value={filters.type || ''} onChange={type => onFilters({ ...filters, type })} options={[{ value: '', label: '全部类型' }, { value: 'novel', label: '小说' }, { value: 'script', label: '剧本' }]} />
      <Select value={filters.mark || ''} onChange={mark => onFilters({ ...filters, mark })} options={[{ value: '', label: '全部标记' }, { value: 'key', label: '重点' }, { value: 'pending', label: '待处理' }, { value: 'done', label: '已完成' }]} />
      <Select value={filters.sort} onChange={sort => onFilters({ ...filters, sort })} options={[{ value: 'updated-desc', label: '最近编辑' }, { value: 'updated-asc', label: '最早编辑' }, { value: 'name-asc', label: '名称 A-Z' }]} />
      <div className="script-editor-view-toggle"><Tooltip title="卡片"><Button type={view === 'grid' ? 'primary' : 'text'} icon={<Grid2X2 size={16} />} onClick={() => onView('grid')} /></Tooltip><Tooltip title="列表"><Button type={view === 'list' ? 'primary' : 'text'} icon={<List size={16} />} onClick={() => onView('list')} /></Tooltip></div>
    </div>
    {scope === 'team' ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂未接入团队作品；这里不会显示虚构数据" /> : !loading && rows.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有作品，先新建或导入一份文档" /> : <div className={`script-editor-work-list is-${view}`}>
      {rows.map(work => <article className="script-editor-work-card" key={work.id} onClick={() => onOpen(work.id)}>
        <span className={`script-editor-cover is-${work.type}`}>{work.type === 'script' ? <FileText /> : <BookOpen />}</span>
        <span className="script-editor-work-main"><strong>{work.title}</strong><span className="script-editor-work-tags"><Tag>{workTypeLabel(work.type)}</Tag>{work.markStatus && <Tag color="blue">{markLabel(work.markStatus)}</Tag>}{work.jobSummary?.running ? <Badge status="processing" text={`${work.jobSummary.running} 个执行中`} /> : null}{work.jobSummary?.failed ? <Badge status="error" text={`${work.jobSummary.failed} 个失败`} /> : null}</span><small>{work.chapterCount || 0} 章 · {work.scriptCount || 0} 个剧本版本 · {work.wordCount || 0} 字</small></span>
        <time>{work.updatedAt ? new Date(work.updatedAt).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : ''}</time>
        <Dropdown menu={{ items: buildWorkMenu(work), onClick: ({ key, domEvent }) => { domEvent.stopPropagation(); onAction(key, work); } }} trigger={['click']}><Button aria-label={`${work.title}更多操作`} type="text" icon={<Ellipsis size={17} />} onClick={event => event.stopPropagation()} /></Dropdown>
      </article>)}
    </div>}
  </section>;
}
