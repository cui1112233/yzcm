import { Checkbox, Form, Input, message, Modal, Select, Spin } from 'antd';
import { Upload } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { commitScriptEditorImport, copyScriptEditorWork, createScriptEditorWork, deleteScriptEditorWork, exportScriptEditorWork, getScriptEditorWork, listScriptEditorWorks, previewScriptEditorImport, updateScriptEditorWork } from '../../shared/api/scriptEditor.js';
import ScriptEditorLibrary from './script-editor/ScriptEditorLibrary.jsx';
import ScriptEditorWorkspace from './script-editor/ScriptEditorWorkspace.jsx';
import './script-editor.css';

const initialFilters = { query: '', type: '', mark: '', sort: 'updated-desc' };

export default function ScriptEditorPage() {
  const [works, setWorks] = useState([]), [loading, setLoading] = useState(true), [view, setView] = useState(() => localStorage.getItem('script-editor:view') || 'grid');
  const [filters, setFilters] = useState(initialFilters), [createOpen, setCreateOpen] = useState(false), [importOpen, setImportOpen] = useState(false);
  const [preview, setPreview] = useState(null), [selected, setSelected] = useState([]), [importFile, setImportFile] = useState(null), [busy, setBusy] = useState(false);
  const [detail, setDetail] = useState(null), [createForm] = Form.useForm();
  const selectedSections = useMemo(() => preview?.sections?.filter(section => selected.includes(section.id)) || [], [preview, selected]);

  async function load() { setLoading(true); try { const result = await listScriptEditorWorks(); setWorks(result.works || []); } catch (error) { message.error(error.message || '作品库加载失败'); } finally { setLoading(false); } }
  useEffect(() => {
    load();
    const workId = new URLSearchParams(window.location.search).get('workId');
    if (workId) openWork(workId);
  }, []);
  async function openWork(id) { setLoading(true); try { const result = await getScriptEditorWork(id); setDetail(result.work); const query = new URLSearchParams(window.location.search); query.set('workId', id); if (!query.get('tab')) query.set('tab', result.work.type === 'script' ? 'scripts' : 'source'); window.history.replaceState({}, '', `/script-editor?${query}`); } catch (error) { message.error(error.message || '作品打开失败'); } finally { setLoading(false); } }
  async function reloadWork() { if (detail?.id) await openWork(detail.id); }
  async function create(values) { setBusy(true); try { const result = await createScriptEditorWork(values); setCreateOpen(false); createForm.resetFields(); await load(); await openWork(result.work.id); } catch (error) { message.error(error.message || '新建失败'); } finally { setBusy(false); } }
  async function chooseImport(file) { setBusy(true); try { const result = await previewScriptEditorImport(file); setImportFile(file); setPreview(result.preview); setSelected(result.preview.sections.map(section => section.id)); } catch (error) { message.error(error.message || '文件解析失败'); } finally { setBusy(false); } return false; }
  async function commitImport() { if (!preview || !selected.length) return message.warning('至少选择一段内容'); setBusy(true); try { const result = await commitScriptEditorImport({ title: importFile.name.replace(/\.(txt|md|markdown|docx)$/i, ''), type: preview.inferredType, filename: preview.filename, sections: preview.sections, selectedSectionIds: selected }); setImportOpen(false); setPreview(null); setImportFile(null); await load(); await openWork(result.work.id); } catch (error) { message.error(error.message || '导入失败'); } finally { setBusy(false); } }
  function changeView(next) { localStorage.setItem('script-editor:view', next); setView(next); }
  async function workAction(action, work) {
    try {
    if (action === 'open') return openWork(work.id);
    if (action === 'workspace') { await openWork(work.id); window.history.replaceState({}, '', `/script-editor?workId=${encodeURIComponent(work.id)}&tab=canvas`); window.dispatchEvent(new PopStateEvent('popstate')); return; }
    if (action === 'rename') {
      let title = work.title;
      Modal.confirm({ title: '重命名作品', content: <Input defaultValue={work.title} maxLength={120} onChange={event => { title = event.target.value; }} />, okText: '保存', onOk: async () => { await updateScriptEditorWork(work.id, { title, expectedRevision: work.revision }); await load(); } });
      return;
    }
    if (action === 'copy') { await copyScriptEditorWork(work.id, crypto.randomUUID()); message.success('作品已复制'); await load(); return; }
    if (action === 'mark') { const order = ['', 'key', 'pending', 'done']; const markStatus = order[(order.indexOf(work.markStatus || '') + 1) % order.length]; await updateScriptEditorWork(work.id, { markStatus, expectedRevision: work.revision }); await load(); return; }
    if (action === 'export') { const blob = await exportScriptEditorWork(work.id, 'txt'); const url = URL.createObjectURL(blob); const anchor = document.createElement('a'); anchor.href = url; anchor.download = `${work.title}.txt`; anchor.click(); URL.revokeObjectURL(url); return; }
      if (action === 'delete') Modal.confirm({ title: `删除“${work.title}”？`, content: '作品会从作品库中软删除，不会连带删除已关联的画布和媒体。', okText: '删除', okButtonProps: { danger: true }, onOk: async () => { await deleteScriptEditorWork(work.id, work.revision); await load(); } });
    } catch (error) {
      message.error(error.message || '作品操作失败');
    }
  }

  if (detail) return <ScriptEditorWorkspace work={detail} onReload={reloadWork} onBack={() => { window.history.replaceState({}, '', '/script-editor'); setDetail(null); load(); }} />;
  return <main className="script-editor-page"><Spin spinning={loading}><ScriptEditorLibrary works={works} filters={filters} onFilters={setFilters} view={view} onView={changeView} onCreate={() => setCreateOpen(true)} onImport={() => setImportOpen(true)} onOpen={openWork} onAction={workAction} loading={loading} /></Spin>
    <Modal title="新建作品" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={() => createForm.submit()} confirmLoading={busy} okText="创建并打开">
      <Form form={createForm} layout="vertical" onFinish={create} initialValues={{ type: 'novel' }}><Form.Item name="title" label="作品名称" rules={[{ required: true, message: '请输入作品名称' }]}><Input maxLength={120} autoFocus /></Form.Item><Form.Item name="type" label="作品类型"><Select options={[{ value: 'novel', label: '小说原文' }, { value: 'script', label: '分集剧本' }]} /></Form.Item></Form>
    </Modal>
    <Modal title="导入文档" width={720} open={importOpen} onCancel={() => { setImportOpen(false); setPreview(null); }} onOk={commitImport} okText="确认导入" okButtonProps={{ disabled: !preview || !selected.length }} confirmLoading={busy}>
      {!preview ? <label className="script-editor-upload"><Upload size={28} /><strong>选择 TXT、Markdown 或 DOCX</strong><span>文件只会先解析预览，点击“确认导入”后才写入作品库。</span><input type="file" accept=".txt,.md,.markdown,.docx,.doc" onChange={e => e.target.files?.[0] && chooseImport(e.target.files[0])} /></label> : <div className="script-editor-import-preview"><div className="script-editor-preview-summary">识别出 {preview.sections.length} 段，已选择 {selectedSections.length} 段</div><Checkbox.Group value={selected} onChange={setSelected}>{preview.sections.map(section => <Checkbox key={section.id} value={section.id}><strong>{section.title}</strong><small>{section.wordCount} 字 · {section.preview || '空段落'}</small></Checkbox>)}</Checkbox.Group></div>}
    </Modal>
  </main>;
}
