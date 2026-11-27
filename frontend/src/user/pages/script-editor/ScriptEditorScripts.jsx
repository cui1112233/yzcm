import { Button, Checkbox, Empty, Input, InputNumber, message, Modal, Popconfirm, Select, Tag } from 'antd';
import { Copy, FileInput, GitCompare, Plus, Replace, Sparkles, Star, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { copyScriptEditorScript, createScriptEditorGenerationJob, createScriptEditorReviewJob, createScriptEditorScript, deleteScriptEditorScript, importScriptEditorScripts, preferScriptEditorScript, replaceScriptEditorScripts, updateScriptEditorScript } from '../../../shared/api/scriptEditor.js';
import { listAvailableModels, modelSelectOptions } from '../../../shared/api/modelCatalog.js';
import ScriptEditorJobs from './ScriptEditorJobs.jsx';
import { scriptEditorJobTitle } from './scriptEditorJobs.js';
import ScriptEditorCompare from './ScriptEditorCompare.jsx';

const requestKey = () => globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random()}`;

export default function ScriptEditorScripts({ work, onReload }) {
  const scripts = work.scripts || [];
  const [activeId, setActiveId] = useState(scripts[0]?.id || '');
  const [draft, setDraft] = useState(null);
  const [status, setStatus] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [episode, setEpisode] = useState(1);
  const [jobOpen, setJobOpen] = useState(false);
  const [jobKind, setJobKind] = useState('script-generation');
  const [models, setModels] = useState([]);
  const [modelId, setModelId] = useState('');
  const [chapterIds, setChapterIds] = useState([]);
  const [extra, setExtra] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [jobsRefresh, setJobsRefresh] = useState(0);
  const [compareOpen, setCompareOpen] = useState(false), [importOpen, setImportOpen] = useState(false), [importText, setImportText] = useState(''), [replaceOpen, setReplaceOpen] = useState(false);
  const [findText, setFindText] = useState(''), [replaceText, setReplaceText] = useState(''), [replaceScope, setReplaceScope] = useState('current_version');
  const active = useMemo(() => scripts.find(row => row.id === activeId) || scripts[0], [scripts, activeId]);

  useEffect(() => { setDraft(active ? { ...active } : null); setStatus(''); }, [active?.id, active?.revision]);
  useEffect(() => {
    if (!jobOpen) return;
    listAvailableModels('text').then(rows => { setModels(rows); if (!modelId) setModelId(rows[0]?.id || ''); }).catch(error => message.error(error.message));
    if (!chapterIds.length) setChapterIds((work.chapters || []).map(row => row.id));
  }, [jobOpen]);
  useEffect(() => {
    if (!draft || !active || (draft.title === active.title && draft.content === active.content)) return;
    setStatus('正在保存…');
    const timer = setTimeout(async () => {
      try {
        await updateScriptEditorScript(work.id, active.id, { title: draft.title, content: draft.content, expectedRevision: active.revision });
        setStatus('已保存'); await onReload();
      } catch (error) { setStatus(error.status === 409 ? '保存冲突：本地文字仍保留，请刷新后处理' : '保存失败'); }
    }, 800);
    return () => clearTimeout(timer);
  }, [draft?.title, draft?.content]);

  function openJob(kind) { if (kind !== 'script-generation' && !active) return message.warning('请先选择一个剧本版本'); setJobKind(kind); setJobOpen(true); }
  async function add() { const result = await createScriptEditorScript(work.id, { episode, title: `第${episode}集`, content: '', generationSource: 'manual' }); setCreateOpen(false); setActiveId(result.script.id); await onReload(); }
  async function copy() { const result = await copyScriptEditorScript(work.id, active.id); setActiveId(result.script.id); await onReload(); }
  async function prefer() { try { await preferScriptEditorScript(work.id, active.id, active.revision); await onReload(); } catch (error) { message.error(error.message); } }
  async function remove() { await deleteScriptEditorScript(work.id, active.id, active.revision); setActiveId(''); await onReload(); }
  async function importScripts() {
    let rows;
    try { const decoded = JSON.parse(importText); rows = Array.isArray(decoded) ? decoded : [decoded]; }
    catch { rows = [{ episode, title:`第${episode}集`, content:importText }]; }
    if (!rows.length || rows.length > 10 || rows.some(row => !Number(row.episode) || typeof row.content !== 'string')) return message.warning('请输入剧本文本，或包含 episode、content 的 1—10 条 JSON 数组');
    setSubmitting(true); try { const result = await importScriptEditorScripts(work.id, { requestKey:requestKey(), scripts:rows.map(row => ({ ...row, generationSource:'import' })) }); setImportOpen(false); setImportText(''); setActiveId(result.scripts?.[0]?.id || ''); await onReload(); message.success(`已导入 ${result.scripts?.length || 0} 个新版本`); } catch (error) { message.error(error.message || '剧本导入失败'); } finally { setSubmitting(false); }
  }
  async function replaceAll() {
    if (!active || !findText) return message.warning('请输入查找内容');
    const selected = scripts.filter(row => replaceScope === 'all_scripts' || replaceScope === 'current_episode' && row.episode === active.episode || replaceScope === 'current_version' && row.id === active.id);
    const expectedRevisions = Object.fromEntries(selected.map(row => [row.id, row.revision]));
    setSubmitting(true); try { await replaceScriptEditorScripts(work.id, { scope:replaceScope, scriptId:active.id, find:findText, replace:replaceText, expectedRevisions }); setReplaceOpen(false); await onReload(); message.success('查找替换已完成'); } catch (error) { message.error(error.status === 409 ? '有版本已变化，所有内容均未替换' : error.message || '替换失败'); } finally { setSubmitting(false); }
  }
  async function mergeAsVersion(source) { const result = await createScriptEditorScript(work.id, { episode:source.episode, title:`${source.title} 合并版`, content:source.content, sourceChapterIds:source.sourceChapterIds, generationSource:'copy' }); setActiveId(result.script.id); setCompareOpen(false); await onReload(); }
  async function submitJob() {
    if (!modelId) return message.warning('请选择文字模型');
    if (jobKind === 'script-generation' && !chapterIds.length) return message.warning('请选择原文章节');
    setSubmitting(true);
    try {
      if (jobKind === 'script-generation') await createScriptEditorGenerationJob(work.id, { requestKey: requestKey(), episodes: [episode], chapterIds, modelId, extra });
      else await createScriptEditorReviewJob(work.id, { requestKey: requestKey(), scriptId: active.id, kind: jobKind, modelId, extra });
      setJobOpen(false); setJobsRefresh(value => value + 1); message.success('任务已提交，可以离开页面，完成后结果会保留');
    } catch (error) { message.error(error.message || '任务提交失败'); }
    finally { setSubmitting(false); }
  }

  return <div className="script-editor-edit-layout">
    <aside className="script-editor-outline">
      <div className="script-editor-pane-title"><strong>剧本版本</strong><span><Button type="text" icon={<FileInput size={15} />} onClick={() => setImportOpen(true)} /><Button type="text" icon={<Sparkles size={15} />} onClick={() => openJob('script-generation')} /><Button type="text" icon={<Plus size={15} />} onClick={() => setCreateOpen(true)} /></span></div>
      {scripts.length ? scripts.map(script => <button className={script.id === active?.id ? 'is-active' : ''} key={script.id} onClick={() => setActiveId(script.id)}><span>第{script.episode}集 · V{script.version} {script.preferred && <Star size={12} fill="currentColor" />}</span><small>{script.content?.replace(/\s/g, '').length || 0} 字</small></button>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无剧本" />}
    </aside>
    <div className="script-editor-main-column">
      <section className="script-editor-document">
        {draft ? <><div className="script-editor-document-actions"><Tag color="purple">第{draft.episode}集 V{draft.version}</Tag><Button icon={<Sparkles size={15} />} onClick={() => openJob('script-generation')}>AI 出剧本</Button><Button onClick={() => openJob('review')}>AI 审稿</Button><Button onClick={() => openJob('polish')}>AI 优化</Button><Button onClick={() => openJob('viral-opening')}>爆款开头</Button><Button icon={<GitCompare size={15} />} onClick={() => setCompareOpen(value => !value)}>版本对比</Button><Button icon={<Replace size={15} />} onClick={() => setReplaceOpen(true)}>查找替换</Button><Button icon={<Copy size={15} />} onClick={copy}>复制为新版本</Button><Button type={draft.preferred ? 'primary' : 'default'} icon={<Star size={15} />} onClick={prefer}>{draft.preferred ? '正式版' : '设为正式版'}</Button><Popconfirm title="删除这个版本？" onConfirm={remove}><Button danger icon={<Trash2 size={15} />} /></Popconfirm><span>{status}</span></div>{draft.sourceChapterIds?.length ? <div className="script-editor-source-context"><strong>原文对照</strong>{(work.chapters || []).filter(row => draft.sourceChapterIds.includes(row.id)).map(row => <article key={row.id}><Tag>{row.title}</Tag><p>{row.content}</p></article>)}</div> : null}<Input value={draft.title} onChange={event => setDraft({ ...draft, title: event.target.value })} className="script-editor-document-title" /><Input.TextArea value={draft.content} onChange={event => setDraft({ ...draft, content: event.target.value })} autoSize={{ minRows: 22 }} placeholder="在这里编写分集剧本。停止输入后自动保存。" /><footer>{draft.content?.replace(/\s/g, '').length || 0} 字</footer></> : <Empty description="新建或 AI 生成一个剧本版本后开始编辑"><Button type="primary" icon={<Sparkles size={15} />} onClick={() => openJob('script-generation')}>AI 出剧本</Button></Empty>}
      </section>
      {compareOpen ? <section className="script-editor-document"><ScriptEditorCompare scripts={scripts} activeId={active?.id} onMerge={mergeAsVersion} /></section> : null}
      <ScriptEditorJobs workId={work.id} refreshToken={jobsRefresh} onReload={onReload} />
    </div>
    <Modal title={scriptEditorJobTitle(jobKind)} width={620} open={jobOpen} onCancel={() => setJobOpen(false)} onOk={submitJob} okText="提交任务（会调用模型）" confirmLoading={submitting}>
      <div className="script-editor-generate-form">
        {jobKind === 'script-generation' && <><label>目标集数<InputNumber min={1} value={episode} onChange={value => setEpisode(value || 1)} /></label><label>采用原文章节<Checkbox.Group value={chapterIds} onChange={setChapterIds} options={(work.chapters || []).map(row => ({ value: row.id, label: row.title }))} /></label></>}
        <label>文字模型<Select value={modelId || undefined} placeholder="请选择已配置的文字模型" options={modelSelectOptions(models, 'text')} onChange={setModelId} /></label>
        <label>额外要求<Input.TextArea value={extra} onChange={event => setExtra(event.target.value)} rows={3} placeholder={jobKind === 'review' ? '例如：重点检查人物动机和结尾悬念' : '例如：前三秒出现冲突，结尾留强悬念'} /></label>
        <small>提交后会使用所选模型，可能产生模型费用。结果会先保存；AI 优化和爆款开头只有点“采用为新版本”后才写入，不会覆盖原稿。</small>
      </div>
    </Modal>
    <Modal title="新建剧本版本" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={add} okText="创建"><label>集数 <InputNumber min={1} value={episode} onChange={value => setEpisode(value || 1)} /></label></Modal>
    <Modal title="导入剧本版本" width={700} open={importOpen} onCancel={() => setImportOpen(false)} onOk={importScripts} okText="导入为新版本" confirmLoading={submitting}><p>粘贴一个剧本文本，或粘贴最多 10 条 JSON 数组。导入永远新建版本，不覆盖旧稿。</p><InputNumber min={1} value={episode} onChange={value => setEpisode(value || 1)} /><Input.TextArea rows={14} value={importText} onChange={event => setImportText(event.target.value)} placeholder={'剧本文本，或 [{"episode":1,"title":"第一集","content":"..."}]'} /></Modal>
    <Modal title="查找替换" open={replaceOpen} onCancel={() => setReplaceOpen(false)} onOk={replaceAll} okText="确认替换" confirmLoading={submitting}><Select value={replaceScope} onChange={setReplaceScope} style={{width:'100%',marginBottom:10}} options={[{value:'current_version',label:'当前版本'},{value:'current_episode',label:'当前集全部版本'},{value:'all_scripts',label:'全部剧本'}]} /><Input value={findText} onChange={event => setFindText(event.target.value)} placeholder="查找内容" /><Input value={replaceText} onChange={event => setReplaceText(event.target.value)} placeholder="替换为" style={{marginTop:10}} /><p>提交前会核对所有目标版本；任何一个版本发生变化时，本次替换不会写入。</p></Modal>
  </div>;
}
