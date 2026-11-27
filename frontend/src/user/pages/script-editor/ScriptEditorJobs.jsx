import { Button, Empty, Progress, Tag, message } from 'antd';
import { Check, RefreshCw, Square } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { applyScriptEditorJob, cancelScriptEditorJob, listScriptEditorJobs, retryScriptEditorJob } from '../../../shared/api/scriptEditor.js';
import { jobProgressView, scriptEditorJobTitle, shouldPollScriptEditorJobs } from './scriptEditorJobs.js';

export default function ScriptEditorJobs({ workId, refreshToken, onReload }) {
  const [jobs, setJobs] = useState([]);
  const [busyId, setBusyId] = useState('');
  const load = useCallback(async signal => {
    try {
      const result = await listScriptEditorJobs(workId, signal);
      setJobs(result.jobs || []);
      return result.jobs || [];
    } catch (error) {
      if (error?.name !== 'AbortError') message.error(error.message || '任务列表加载失败');
      return [];
    }
  }, [workId]);

  useEffect(() => {
    const controller = new AbortController();
    let timer;
    const tick = async () => {
      const rows = await load(controller.signal);
      if (!controller.signal.aborted && shouldPollScriptEditorJobs(rows)) timer = setTimeout(tick, 2000);
    };
    tick();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [load, refreshToken]);

  async function act(job, action) {
    setBusyId(job.id);
    try {
      await action(workId, job.id);
      await load();
      if (action === applyScriptEditorJob) {
        await onReload();
        message.success('已保存为新的剧本版本，原版本没有被覆盖');
      }
    } catch (error) {
      message.error(error.message || '操作失败');
    } finally { setBusyId(''); }
  }

  return <section className="script-editor-jobs">
    <div className="script-editor-jobs-title"><div><strong>AI 任务</strong><small>离开页面也会继续，回来可查看结果</small></div><Button type="text" icon={<RefreshCw size={15} />} onClick={() => load()} /></div>
    {!jobs.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无 AI 任务" /> : jobs.map(job => {
      const view = jobProgressView(job);
      const canApply = job.status === 'succeeded' && ['polish', 'viral-opening'].includes(job.type);
      return <article key={job.id} className="script-editor-job-card">
        <div className="script-editor-job-head"><strong>{scriptEditorJobTitle(job.type)}</strong><Tag color={view.tone}>{view.label}</Tag></div>
        {['queued', 'running'].includes(job.status) && <Progress size="small" percent={job.progressTotal ? Math.round((job.progressCurrent || 0) * 100 / job.progressTotal) : 0} status="active" />}
        {job.result?.output && <pre>{job.result.output}</pre>}
        <div className="script-editor-job-actions">
          {['queued', 'running'].includes(job.status) && <Button size="small" icon={<Square size={13} />} loading={busyId === job.id} onClick={() => act(job, cancelScriptEditorJob)}>取消</Button>}
          {view.retryable && <Button size="small" icon={<RefreshCw size={13} />} loading={busyId === job.id} onClick={() => act(job, retryScriptEditorJob)}>重试失败部分</Button>}
          {canApply && <Button size="small" type="primary" icon={<Check size={13} />} loading={busyId === job.id} onClick={() => act(job, applyScriptEditorJob)}>采用为新版本</Button>}
        </div>
      </article>;
    })}
  </section>;
}
