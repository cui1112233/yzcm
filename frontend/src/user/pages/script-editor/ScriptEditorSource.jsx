import { Button, Checkbox, Empty, Input, message, Modal, Popconfirm, Select, Tag } from 'antd';
import { ChevronDown, ChevronUp, FileInput, Plus, Search, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { commitScriptEditorImport, createScriptEditorChapter, deleteScriptEditorChapter, importIntoScriptEditorWork, previewScriptEditorImport, reorderScriptEditorChapters, searchScriptEditorWork, updateScriptEditorChapter } from '../../../shared/api/scriptEditor.js';
import { referencedChapterCounts } from './sourceSearch.js';

export default function ScriptEditorSource({ work, onReload }) {
  const chapters = work.chapters || [];
  const references = useMemo(() => referencedChapterCounts(work.scripts || []), [work.scripts]);
  const [activeId, setActiveId] = useState(chapters[0]?.id || ''), [draft, setDraft] = useState(null), [status, setStatus] = useState('');
  const [query, setQuery] = useState(''), [hits, setHits] = useState([]), [searching, setSearching] = useState(false);
  const [importOpen, setImportOpen] = useState(false), [preview, setPreview] = useState(null), [selected, setSelected] = useState([]), [mode, setMode] = useState('append'), [importFile, setImportFile] = useState(null), [importing, setImporting] = useState(false);
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
  useEffect(() => {
    const normalized = query.trim();
    if (!normalized) { setHits([]); return; }
    const controller = new AbortController(); setSearching(true);
    const timer = setTimeout(() => searchScriptEditorWork(work.id, normalized, controller.signal).then(result => setHits(result.hits || [])).catch(error => { if (error.name !== 'AbortError') message.error(error.message || '搜索失败'); }).finally(() => setSearching(false)), 250);
    return () => { clearTimeout(timer); controller.abort(); };
  }, [query, work.id]);
  async function add() { const result = await createScriptEditorChapter(work.id, { title: `第${chapters.length + 1}章`, content: '' }); setActiveId(result.chapter.id); await onReload(); }
  async function remove() { if (!active) return; try { await deleteScriptEditorChapter(work.id, active.id, active.revision); setActiveId(''); await onReload(); } catch (error) { message.error(error.message); } }
  async function move(delta) { const index = chapters.findIndex(row => row.id === active.id), next = index + delta; if (next < 0 || next >= chapters.length) return; const ids = chapters.map(row => row.id); [ids[index], ids[next]] = [ids[next], ids[index]]; await reorderScriptEditorChapters(work.id, ids); await onReload(); }
  async function chooseImport(file) { setImporting(true); try { const result = await previewScriptEditorImport(file); setImportFile(file); setPreview(result.preview); setSelected(result.preview.sections.map(section => section.id)); } catch (error) { message.error(error.message || '文件解析失败'); } finally { setImporting(false); } return false; }
  async function commitImportMode() {
    setImporting(true);
    try {
      if (mode === 'new_work') { const result = await commitScriptEditorImport({ title: importFile.name.replace(/\.(txt|md|markdown|docx)$/i, ''), type: preview.inferredType, filename: preview.filename, sections: preview.sections, selectedSectionIds: selected }); window.location.assign(`/script-editor?workId=${encodeURIComponent(result.work.id)}`); return; }
      await importIntoScriptEditorWork(work.id, { mode, chapterId: mode === 'replace_chapter' ? active.id : '', expectedRevision: mode === 'replace_chapter' ? active.revision : 0, sections: preview.sections, selectedSectionIds: selected });
      setImportOpen(false); setPreview(null); await onReload(); message.success(mode === 'append' ? '已追加导入' : '当前章节已替换');
    } catch (error) { message.error(error.status === 409 ? '章节已在其他窗口更新，请刷新后重试' : error.message || '导入失败'); }
    finally { setImporting(false); }
  }
  function applyImport() {
    if (!preview || !selected.length) return message.warning('至少选择一段内容');
    if (mode === 'replace_chapter' && !active) return message.warning('请先选择要替换的章节');
    if (mode === 'replace_chapter') return Modal.confirm({ title: `确认替换“${active.title}”？`, content: '原章节会被替换；如版本已变化，系统会阻止覆盖。', okText: '确认替换', okButtonProps:{ danger:true }, onOk:commitImportMode });
    return commitImportMode();
  }
  return <>
    <div className="script-editor-edit-layout"><aside className="script-editor-outline"><div className="script-editor-pane-title"><strong>原文章节</strong><span><Button type="text" icon={<FileInput size={15} />} onClick={() => setImportOpen(true)} /><Button type="text" icon={<Plus size={15} />} onClick={add} /></span></div><div className="script-editor-outline-search"><Input allowClear prefix={<Search size={14} />} placeholder="搜索标题和正文" value={query} onChange={event => setQuery(event.target.value)} /></div>
      {query.trim() ? <div className="script-editor-search-results">{searching ? <small>正在搜索…</small> : hits.length ? hits.map((hit, index) => <button type="button" key={`${hit.chapterId}-${hit.field}-${hit.start}-${index}`} onClick={() => { setActiveId(hit.chapterId); setQuery(''); }}><strong>{hit.title}</strong><span>{hit.excerpt}</span></button>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配内容" />}</div> : chapters.length ? chapters.map(chapter => <button className={chapter.id === active?.id ? 'is-active' : ''} key={chapter.id} onClick={() => setActiveId(chapter.id)}><span>{chapter.title}{references[chapter.id] ? <Tag color="purple">已引用 {references[chapter.id]}</Tag> : null}</span><small>{chapter.wordCount} 字</small></button>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无章节" />}</aside>
      <section className="script-editor-document">{draft ? <><div className="script-editor-document-actions"><Button icon={<ChevronUp size={15} />} onClick={() => move(-1)} /><Button icon={<ChevronDown size={15} />} onClick={() => move(1)} /><Popconfirm title="删除这个章节？" onConfirm={remove}><Button danger icon={<Trash2 size={15} />} /></Popconfirm><span>{status}</span></div><Input value={draft.title} onChange={event => setDraft({ ...draft, title: event.target.value })} className="script-editor-document-title" /><Input.TextArea value={draft.content} onChange={event => setDraft({ ...draft, content: event.target.value })} autoSize={{ minRows: 22 }} placeholder="在这里整理原文。停止输入后自动保存。" /><footer>{draft.content?.replace(/\s/g, '').length || 0} 字</footer></> : <Empty description="新建一个章节后开始编辑" />}</section></div>
    <Modal title="导入原文" width={720} open={importOpen} onCancel={() => { setImportOpen(false); setPreview(null); }} onOk={applyImport} okText="确认导入" confirmLoading={importing} okButtonProps={{ disabled: !preview || !selected.length }}>
      {!preview ? <label className="script-editor-upload"><FileInput size={28} /><strong>选择 TXT、Markdown 或 DOCX</strong><input type="file" accept=".txt,.md,.markdown,.docx" onChange={event => event.target.files?.[0] && chooseImport(event.target.files[0])} /></label> : <><Select value={mode} onChange={setMode} style={{width:'100%',marginBottom:12}} options={[{value:'append',label:'追加到当前作品'},{value:'replace_chapter',label:'替换当前章节'},{value:'new_work',label:'导入为新作品'}]} /><Checkbox.Group value={selected} onChange={setSelected} style={{display:'flex',flexDirection:'column',maxHeight:400,overflow:'auto'}}>{preview.sections.map(section => <Checkbox key={section.id} value={section.id}><strong>{section.title}</strong> · {section.wordCount} 字</Checkbox>)}</Checkbox.Group></>}
    </Modal>
  </>;
}
