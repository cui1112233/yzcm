import { Button, Empty, Input, InputNumber, message, Modal, Popconfirm, Tag } from 'antd';
import { Copy, Plus, Star, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { copyScriptEditorScript, createScriptEditorScript, deleteScriptEditorScript, preferScriptEditorScript, updateScriptEditorScript } from '../../../shared/api/scriptEditor.js';

export default function ScriptEditorScripts({ work, onReload }) {
  const scripts = work.scripts || [];
  const [activeId, setActiveId] = useState(scripts[0]?.id || ''), [draft, setDraft] = useState(null), [status, setStatus] = useState(''), [createOpen, setCreateOpen] = useState(false), [episode, setEpisode] = useState(1);
  const active = useMemo(() => scripts.find(row => row.id === activeId) || scripts[0], [scripts, activeId]);
  useEffect(() => { setDraft(active ? { ...active } : null); setStatus(''); }, [active?.id, active?.revision]);
  useEffect(() => {
    if (!draft || !active || (draft.title === active.title && draft.content === active.content)) return;
    setStatus('正在保存…'); const timer = setTimeout(async () => { try { await updateScriptEditorScript(work.id, active.id, { title: draft.title, content: draft.content, expectedRevision: active.revision }); setStatus('已保存'); await onReload(); } catch (error) { setStatus(error.status === 409 ? '保存冲突：本地文字仍保留，请刷新后处理' : '保存失败'); } }, 800); return () => clearTimeout(timer);
  }, [draft?.title, draft?.content]);
  async function add() { const result = await createScriptEditorScript(work.id, { episode, title: `第${episode}集`, content: '', generationSource: 'manual' }); setCreateOpen(false); setActiveId(result.script.id); await onReload(); }
  async function copy() { const result = await copyScriptEditorScript(work.id, active.id); setActiveId(result.script.id); await onReload(); }
  async function prefer() { try { await preferScriptEditorScript(work.id, active.id, active.revision); await onReload(); } catch (error) { message.error(error.message); } }
  async function remove() { await deleteScriptEditorScript(work.id, active.id, active.revision); setActiveId(''); await onReload(); }
  return <div className="script-editor-edit-layout"><aside className="script-editor-outline"><div className="script-editor-pane-title"><strong>剧本版本</strong><Button type="text" icon={<Plus size={15} />} onClick={() => setCreateOpen(true)} /></div>{scripts.length ? scripts.map(script => <button className={script.id === active?.id ? 'is-active' : ''} key={script.id} onClick={() => setActiveId(script.id)}><span>第{script.episode}集 · V{script.version} {script.preferred && <Star size={12} fill="currentColor" />}</span><small>{script.content?.replace(/\s/g, '').length || 0} 字</small></button>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无剧本" />}</aside><section className="script-editor-document">{draft ? <><div className="script-editor-document-actions"><Tag color="purple">第{draft.episode}集 V{draft.version}</Tag><Button icon={<Copy size={15} />} onClick={copy}>复制为新版本</Button><Button type={draft.preferred ? 'primary' : 'default'} icon={<Star size={15} />} onClick={prefer}>{draft.preferred ? '正式版' : '设为正式版'}</Button><Popconfirm title="删除这个版本？" onConfirm={remove}><Button danger icon={<Trash2 size={15} />} /></Popconfirm><span>{status}</span></div><Input value={draft.title} onChange={event => setDraft({ ...draft, title: event.target.value })} className="script-editor-document-title" /><Input.TextArea value={draft.content} onChange={event => setDraft({ ...draft, content: event.target.value })} autoSize={{ minRows: 22 }} placeholder="在这里编写分集剧本。停止输入后自动保存。" /><footer>{draft.content?.replace(/\s/g, '').length || 0} 字</footer></> : <Empty description="新建一个剧本版本后开始编辑" />}</section><Modal title="新建剧本版本" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={add} okText="创建"><label>集数 <InputNumber min={1} value={episode} onChange={value => setEpisode(value || 1)} /></label></Modal></div>;
}
