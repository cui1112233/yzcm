import { Button, Checkbox, Form, Input, message, Modal, Select, Spin, Tabs } from 'antd';
import { ArrowLeft, Upload } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { commitScriptEditorImport, createScriptEditorWork, getScriptEditorWork, listScriptEditorWorks, previewScriptEditorImport } from '../../shared/api/scriptEditor.js';
import ScriptEditorLibrary from './script-editor/ScriptEditorLibrary.jsx';
import ScriptEditorSource from './script-editor/ScriptEditorSource.jsx';
import ScriptEditorScripts from './script-editor/ScriptEditorScripts.jsx';
import ScriptEditorDelivery from './script-editor/ScriptEditorDelivery.jsx';
import './script-editor.css';

const initialFilters = { query: '', type: '', mark: '', sort: 'updated-desc' };

export default function ScriptEditorPage() {
  const [works, setWorks] = useState([]), [loading, setLoading] = useState(true), [view, setView] = useState(() => localStorage.getItem('script-editor:view') || 'grid');
  const [filters, setFilters] = useState(initialFilters), [createOpen, setCreateOpen] = useState(false), [importOpen, setImportOpen] = useState(false);
  const [preview, setPreview] = useState(null), [selected, setSelected] = useState([]), [importFile, setImportFile] = useState(null), [busy, setBusy] = useState(false);
  const [detail, setDetail] = useState(null), [createForm] = Form.useForm();
  const selectedSections = useMemo(() => preview?.sections?.filter(section => selected.includes(section.id)) || [], [preview, selected]);

  async function load() { setLoading(true); try { const result = await listScriptEditorWorks(); setWorks(result.works || []); } catch (error) { message.error(error.message || '作品库加载失败'); } finally { setLoading(false); } }
  useEffect(() => { load(); }, []);
  async function openWork(id) { setLoading(true); try { const result = await getScriptEditorWork(id); setDetail(result.work); } catch (error) { message.error(error.message || '作品打开失败'); } finally { setLoading(false); } }
  async function reloadWork() { if (detail?.id) await openWork(detail.id); }
  async function create(values) { setBusy(true); try { const result = await createScriptEditorWork(values); setCreateOpen(false); createForm.resetFields(); await load(); await openWork(result.work.id); } catch (error) { message.error(error.message || '新建失败'); } finally { setBusy(false); } }
  async function chooseImport(file) { setBusy(true); try { const result = await previewScriptEditorImport(file); setImportFile(file); setPreview(result.preview); setSelected(result.preview.sections.map(section => section.id)); } catch (error) { message.error(error.message || '文件解析失败'); } finally { setBusy(false); } return false; }
  async function commitImport() { if (!preview || !selected.length) return message.warning('至少选择一段内容'); setBusy(true); try { const result = await commitScriptEditorImport({ title: importFile.name.replace(/\.(txt|md|markdown|docx)$/i, ''), type: preview.inferredType, filename: preview.filename, sections: preview.sections, selectedSectionIds: selected }); setImportOpen(false); setPreview(null); setImportFile(null); await load(); await openWork(result.work.id); } catch (error) { message.error(error.message || '导入失败'); } finally { setBusy(false); } }
  function changeView(next) { localStorage.setItem('script-editor:view', next); setView(next); }

  if (detail) return <main className="script-editor-page"><div className="script-editor-workspace"><header><Button icon={<ArrowLeft size={16} />} onClick={() => { setDetail(null); load(); }}>作品库</Button><div><span className="script-editor-kicker">{detail.type === 'script' ? 'SCRIPT' : 'NOVEL'}</span><h1>{detail.title}</h1></div></header><Tabs defaultActiveKey={detail.type === 'script' ? 'scripts' : 'source'} items={[{ key: 'source', label: '原文', children: <ScriptEditorSource work={detail} onReload={reloadWork} /> }, { key: 'scripts', label: '分集剧本', children: <ScriptEditorScripts work={detail} onReload={reloadWork} /> }, { key: 'delivery', label: '导出交付', children: <ScriptEditorDelivery work={detail} /> }]} /></div></main>;
  return <main className="script-editor-page"><Spin spinning={loading}><ScriptEditorLibrary works={works} filters={filters} onFilters={setFilters} view={view} onView={changeView} onCreate={() => setCreateOpen(true)} onImport={() => setImportOpen(true)} onOpen={openWork} loading={loading} /></Spin>
    <Modal title="新建作品" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={() => createForm.submit()} confirmLoading={busy} okText="创建并打开">
      <Form form={createForm} layout="vertical" onFinish={create} initialValues={{ type: 'novel' }}><Form.Item name="title" label="作品名称" rules={[{ required: true, message: '请输入作品名称' }]}><Input maxLength={120} autoFocus /></Form.Item><Form.Item name="type" label="作品类型"><Select options={[{ value: 'novel', label: '小说原文' }, { value: 'script', label: '分集剧本' }]} /></Form.Item></Form>
    </Modal>
    <Modal title="导入文档" width={720} open={importOpen} onCancel={() => { setImportOpen(false); setPreview(null); }} onOk={commitImport} okText="确认导入" okButtonProps={{ disabled: !preview || !selected.length }} confirmLoading={busy}>
      {!preview ? <label className="script-editor-upload"><Upload size={28} /><strong>选择 TXT、Markdown 或 DOCX</strong><span>文件只会先解析预览，点击“确认导入”后才写入作品库。</span><input type="file" accept=".txt,.md,.markdown,.docx,.doc" onChange={e => e.target.files?.[0] && chooseImport(e.target.files[0])} /></label> : <div className="script-editor-import-preview"><div className="script-editor-preview-summary">识别出 {preview.sections.length} 段，已选择 {selectedSections.length} 段</div><Checkbox.Group value={selected} onChange={setSelected}>{preview.sections.map(section => <Checkbox key={section.id} value={section.id}><strong>{section.title}</strong><small>{section.wordCount} 字 · {section.preview || '空段落'}</small></Checkbox>)}</Checkbox.Group></div>}
    </Modal>
  </main>;
}
