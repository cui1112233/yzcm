import { Alert, Button, Card, Input, Modal, Space, Tag, message } from 'antd';
import { Download, ExternalLink, FileArchive, FileJson, FileText } from 'lucide-react';
import { useState } from 'react';
import { exportScriptEditorWork, handoffScriptEditorWork } from '../../../shared/api/scriptEditor.js';
import { validateDelivery } from './scriptEditorDelivery.js';

export default function ScriptEditorDelivery({ work }) {
  const [busy, setBusy] = useState('');
  const [handoffOpen, setHandoffOpen] = useState(false);
  const [projectTitle, setProjectTitle] = useState(work.title);
  const validation = validateDelivery(work.scripts);
  async function download(format) {
    setBusy(format);
    try {
      const blob = await exportScriptEditorWork(work.id, format);
      const href = URL.createObjectURL(blob), link = document.createElement('a');
      link.href = href; link.download = `${work.title}-正式剧本.${format}`; link.click(); URL.revokeObjectURL(href);
    } finally { setBusy(''); }
  }
  async function handoff() {
    setBusy('handoff');
    try {
      const episodes = [...new Set(work.scripts.filter(row => row.preferred).map(row => row.episode))].sort((a, b) => a - b);
      const requestKey = `script-editor-${work.id}-${episodes.join('-')}-${crypto.randomUUID()}`;
      const result = await handoffScriptEditorWork(work.id, { requestKey, episodes, title: projectTitle.trim() });
      message.success('已创建漫剧项目，正在打开');
      window.location.href = `/comic-screenplay?projectId=${encodeURIComponent(result.handoff.screenplayProjectId)}`;
    } catch (error) {
      message.error(error?.message || '导入漫剧创作失败');
    } finally { setBusy(''); }
  }
  return <section className="script-editor-delivery"><div><span className="script-editor-kicker">DELIVERY</span><h2>导出与继续制作</h2><p>只导出每一集标记为“正式版”的剧本，草稿和历史版本不会混进去。</p></div>
    {!validation.ok && <Alert type="warning" showIcon message={validation.missing.length ? `第 ${validation.missing.join('、')} 集还没有正式版` : '还没有可交付的剧本'} description="请回到“分集剧本”，为每一集选择一个正式版。" />}
    <div className="script-editor-delivery-grid">
      <Card title="合并文本" extra={<FileText size={18} />}><p>按集数合并成一个 TXT，适合审阅和复制。</p><Button disabled={!validation.ok} loading={busy === 'txt'} onClick={() => download('txt')} icon={<Download size={15} />}>导出 TXT</Button></Card>
      <Card title="分集压缩包" extra={<FileArchive size={18} />}><p>每集一个 TXT，并附带版本元数据。</p><Button disabled={!validation.ok} loading={busy === 'zip'} onClick={() => download('zip')} icon={<Download size={15} />}>导出 ZIP</Button></Card>
      <Card title="结构化数据" extra={<FileJson size={18} />}><p>保留集数、版本、模型和来源信息。</p><Button disabled={!validation.ok} loading={busy === 'json'} onClick={() => download('json')} icon={<Download size={15} />}>导出 JSON</Button></Card>
    </div>
    <Card className="script-editor-comic-handoff" title="下一步：漫剧创作"><Space direction="vertical"><span>剧本确定后，可以把正式版直接创建成漫剧项目，继续做人物、场景、分镜、图片、配音和视频。</span><Tag color="blue">剧本编辑负责写；漫剧创作负责拍</Tag><Button disabled={!validation.ok} loading={busy === 'handoff'} icon={<ExternalLink size={15} />} onClick={() => setHandoffOpen(true)}>导入漫剧创作</Button></Space></Card>
    <Modal title="创建漫剧项目" open={handoffOpen} onCancel={() => setHandoffOpen(false)} onOk={handoff} okText="创建并打开" confirmLoading={busy === 'handoff'} okButtonProps={{ disabled: !projectTitle.trim() }}><p>每一集的正式版会按顺序写入新项目，不会覆盖已有漫剧。</p><Input value={projectTitle} maxLength={120} onChange={event => setProjectTitle(event.target.value)} placeholder="漫剧项目名称" /></Modal>
  </section>;
}
