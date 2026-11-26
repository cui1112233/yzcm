import { Button, Empty, Input, message, Popconfirm } from 'antd';
import { ChevronDown, ChevronUp, Plus, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { createScriptEditorChapter, deleteScriptEditorChapter, reorderScriptEditorChapters, updateScriptEditorChapter } from '../../../shared/api/scriptEditor.js';

export default function ScriptEditorSource({ work, onReload }) {
  const chapters = work.chapters || [];
  const [activeId, setActiveId] = useState(chapters[0]?.id || ''), [draft, setDraft] = useState(null), [status, setStatus] = useState('');
  const active = useMemo(() => chapters.find(row => row.id === activeId) || chapters[0], [chapters, activeId]);
  useEffect(() => { setDraft(active ? { ...active } : null); setStatus(''); }, [active?.id, active?.revision]);
  useEffect(() => {
    if (!draft || !active || (draft.title === active.title && draft.content === active.content)) return;
    setStatus('正在保存…');
    const timer = setTimeout(async () => {
      try { await updateScriptEditorChapter(work.id, active.id, { title: draft.title, content: draft.content, expectedRevision: active.revision }); setStatus('已保存'); await onReload(); }
      catch (error) { setStatus(error.status === 409 ? '保存冲突：请刷新后再编辑' : '保存失败'); }
    }, 800);
    return () => clearTimeout(timer);
  }, [draft?.title, draft?.content]);
  async function add() { const result = await createScriptEditorChapter(work.id, { title: `第${chapters.length + 1}章`, content: '' }); setActiveId(result.chapter.id); await onReload(); }
  async function remove() { if (!active) return; try { await deleteScriptEditorChapter(work.id, active.id, active.revision); setActiveId(''); await onReload(); } catch (error) { message.error(error.message); } }
  async function move(delta) { const index = chapters.findIndex(row => row.id === active.id), next = index + delta; if (next < 0 || next >= chapters.length) return; const ids = chapters.map(row => row.id); [ids[index], ids[next]] = [ids[next], ids[index]]; await reorderScriptEditorChapters(work.id, ids); await onReload(); }
  return <div className="script-editor-edit-layout"><aside className="script-editor-outline"><div className="script-editor-pane-title"><strong>原文章节</strong><Button type="text" icon={<Plus size={15} />} onClick={add} /></div>{chapters.length ? chapters.map(chapter => <button className={chapter.id === active?.id ? 'is-active' : ''} key={chapter.id} onClick={() => setActiveId(chapter.id)}><span>{chapter.title}</span><small>{chapter.wordCount} 字</small></button>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无章节" />}</aside><section className="script-editor-document">{draft ? <><div className="script-editor-document-actions"><Button icon={<ChevronUp size={15} />} onClick={() => move(-1)} /><Button icon={<ChevronDown size={15} />} onClick={() => move(1)} /><Popconfirm title="删除这个章节？" onConfirm={remove}><Button danger icon={<Trash2 size={15} />} /></Popconfirm><span>{status}</span></div><Input value={draft.title} onChange={event => setDraft({ ...draft, title: event.target.value })} className="script-editor-document-title" /><Input.TextArea value={draft.content} onChange={event => setDraft({ ...draft, content: event.target.value })} autoSize={{ minRows: 22 }} placeholder="在这里整理原文。停止输入后自动保存。" /><footer>{draft.content?.replace(/\s/g, '').length || 0} 字</footer></> : <Empty description="新建一个章节后开始编辑" />}</section></div>;
}
