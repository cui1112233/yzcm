import Taro from "@tarojs/taro";
import { Button, Picker, Text, Textarea, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import { apiRequest } from "../../services/api";
import { sessionRevision } from "../../services/sessionBoundary";
import { entityImageUrls } from "../../services/scriptEntityMedia";
import { createShotVideo, persistScriptVideoTasks, retainedScriptVideoTasks, splitScriptShots } from "../../services/scriptProduction";
import type { ScriptVideoTask, ScriptEntity } from "../../services/scriptWorkspace";
import ScriptHistoryVideos from "./ScriptHistoryVideos";

export default function ScriptVideoProduction({ output, historyId, tasks, duration, entities, onTasksChange }: { output: string; historyId: string; tasks: Record<string, ScriptVideoTask>; duration: string; entities: ScriptEntity[]; onTasksChange: (tasks: Record<string, ScriptVideoTask>) => void }) {
  const [models, setModels] = useState<any[]>([]), [modelKey, setModelKey] = useState(""), [latest, setLatest] = useState(tasks), [error, setError] = useState(""), [busy, setBusy] = useState("");
  const [canVideo, setCanVideo] = useState(false), [shots, setShots] = useState(() => splitScriptShots(output));
  const current = useRef(true), locked = useRef(false), latestRef = useRef(latest); latestRef.current = latest;
  const revision = useRef(0);
  const load = async () => {
    setError(""); setCanVideo(false);
    try {
      const [result, member] = await Promise.all([apiRequest<any>("/api/shuihuo-production/models"), apiRequest<any>("/api/member/me")]);
      if (!current.current) return;
      if (!Array.isArray(result?.models)) throw new Error("视频模型目录无效");
      const enabled = result.models.filter((item: any) => item.enabled !== false);
      setModels(enabled); setModelKey(value => enabled.some((item: any) => String(item.key || item.modelKey || item.id) === value) ? value : "");
      setCanVideo(Boolean(["dev", "manager"].includes(member?.member?.role) || member?.member?.apiScopes?.some((scope: string) => ["*", "video"].includes(scope))));
    } catch (cause: any) { if (current.current) setError(cause.message); }
  };
  useEffect(() => { current.current = true; void load(); return () => { current.current = false; }; }, []);
  useEffect(() => {
    const ticket = revision.current;
    const retained = retainedScriptVideoTasks(historyId);
    if (Object.keys(retained).length) { const next = { ...latestRef.current, ...retained }; latestRef.current = next; setLatest(next); onTasksChange(next); }
    if (historyId) void apiRequest<any>(`/api/history/${encodeURIComponent(historyId)}`).then(payload => {
      if (!current.current || ticket !== revision.current) return;
      const entry = payload?.entry || payload;
      if (!entry || typeof entry !== "object") throw new Error("历史响应无效");
      const next = { ...latestRef.current, ...(entry.videoTasks || {}) }; latestRef.current = next; setLatest(next); onTasksChange(next);
    }).catch(cause => { if (current.current && ticket === revision.current) setError(cause.message); });
  }, [historyId]);
  const create = async (index: number) => {
    if (locked.current || !canVideo) return;
    locked.current = true; setBusy(String(index)); setError("");
    revision.current++;
    const session = sessionRevision();
    try {
      const confirmation = await Taro.showModal({ title: "生成分镜视频", content: `将为第 ${index + 1} 条分镜提交真实视频任务，可能产生费用。是否继续？` });
      if (!confirmation.confirm || !current.current || session !== sessionRevision()) return;
      const images = entities.flatMap(entity => {
        const record = (entity.data || entity) as any;
        const name = record.角色名称 || record.场景名称 || record.姓名 || record.name || record.名称 || entity.name;
        const image = entity.mainImageUrl || record.mainImageUrl || entityImageUrls(entity)[0];
        return name && shots[index].includes(name) && image ? [image] : [];
      });
      const result = await createShotVideo(historyId, String(index), shots[index], modelKey, duration, latestRef.current, images);
      if (current.current) { latestRef.current = result.tasks; setLatest(result.tasks); onTasksChange(result.tasks); setError(result.warning); }
    } catch (cause: any) { if (current.current) setError(cause.message); }
    finally { locked.current = false; if (current.current) setBusy(""); }
  };
  return <View>
    <View className="mp-card"><Text className="mp-row-title">分镜与视频制作 · {shots.length} 条</Text><Picker range={models.map(item => item.name || item.label || item.key || item.id)} disabled={Boolean(busy)} onChange={event => { const item = models[Number(event.detail.value)]; setModelKey(String(item?.key || item?.modelKey || item?.id || "")); }}><View className="mp-picker-field"><Text>视频模型：{models.find(item => String(item.key || item.modelKey || item.id) === modelKey)?.name || modelKey || "请选择"}</Text></View></Picker><Button onClick={() => void load()} disabled={Boolean(busy)}>重新读取模型与权限</Button>
      {!canVideo ? <Text className="mp-row-description">当前账号未获得视频生成权限；已有视频仍可查看。</Text> : null}
      {!historyId ? <Text className="mp-error">历史尚未保存，暂不能提交视频。</Text> : null}
      <Button disabled={Boolean(busy) || !historyId || !Object.keys(latest).length} onClick={async () => { if (locked.current) return; locked.current = true; setBusy("sync"); try { await persistScriptVideoTasks(historyId, latestRef.current); setError(""); } catch (cause: any) { setError(cause.message); } finally { locked.current = false; setBusy(""); } }}>重试同步已有视频到历史</Button>
      {error ? <Text className="mp-error">{error}</Text> : null}
    </View>
    {shots.map((shot, index) => <View className="mp-card" key={index}><Text className="mp-row-title">分镜 {index + 1}</Text><Textarea className="mp-result" maxlength={-1} value={shot} disabled={Boolean(busy) || Boolean(latest[index]?.taskId)} onInput={event => setShots(value => value.map((text, i) => i === index ? event.detail.value : text))} />
      <Button className="mp-secondary" onClick={() => Taro.setClipboardData({ data: shot })}>复制提示词</Button>
      <Button className="mp-primary" disabled={Boolean(busy) || !canVideo || !modelKey || !historyId || Boolean(latest[index]?.taskId && !["failed", "error", "cancelled", "canceled"].includes(String(latest[index].status)))} loading={busy === String(index)} onClick={() => void create(index)}>{latest[index]?.taskId ? "重试失败分镜" : "生成此分镜视频"}</Button>
    </View>)}
    <ScriptHistoryVideos key={Object.values(latest).map(task => task.taskId).join(":")} tasks={latest} acquire={() => { if (locked.current) return false; locked.current = true; setBusy("refresh"); return true; }} release={() => { locked.current = false; if (current.current) setBusy(""); }} onTaskUpdate={async (index, task) => { const next = historyId ? await persistScriptVideoTasks(historyId, { [index]: task }) : { ...latestRef.current, [index]: task }; if (current.current) { latestRef.current = next; setLatest(next); onTasksChange(next); } }} />
  </View>;
}
