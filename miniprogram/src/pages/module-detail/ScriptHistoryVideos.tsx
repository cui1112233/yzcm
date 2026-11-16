import type { ScriptVideoTask } from "../../services/scriptWorkspace";
import { Button, Text, Video, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import { apiRequest } from "../../services/api";
import { loadAuthenticatedMedia } from "../../services/mediaCache";

export default function ScriptHistoryVideos({ tasks, onTaskUpdate, acquire, release }: { tasks: Record<string, ScriptVideoTask>; onTaskUpdate?: (index: string, task: ScriptVideoTask) => Promise<void>; acquire?: () => boolean; release?: () => void }) {
  const [latest, setLatest] = useState<Record<string, ScriptVideoTask>>({});
  const [paths, setPaths] = useState<Record<string, string>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<Record<string, boolean>>({});
  const pending = useRef(new Set<string>());
  const generation = useRef(0);
  useEffect(() => () => { generation.current += 1; }, []);

  const run = async (index: string, operation: () => Promise<void>) => {
    if (pending.current.has(index)) return;
    if (acquire && !acquire()) return;
    pending.current.add(index);
    const current = generation.current;
    setBusy((value) => ({ ...value, [index]: true }));
    setErrors((value) => ({ ...value, [index]: "" }));
    try { await operation(); }
    catch (cause) { if (current === generation.current) setErrors((value) => ({ ...value, [index]: cause instanceof Error ? cause.message : "视频读取失败" })); }
    finally { pending.current.delete(index); release?.(); if (current === generation.current) setBusy((value) => ({ ...value, [index]: false })); }
  };
  const refresh = (index: string, task: ScriptVideoTask) => run(index, async () => {
    const current = generation.current;
    const result = await apiRequest<ScriptVideoTask>(`/api/script-video/${encodeURIComponent(task.taskId!)}`);
    if (current !== generation.current) return;
    if (!result || typeof result !== "object" || (result.taskId && result.taskId !== task.taskId) || (!result.status && !result.videoUrl)) throw new Error("视频任务响应无效，请重试");
    setLatest((value) => ({ ...value, [index]: { ...task, ...result, taskId: task.taskId } }));
    if (result.videoUrl && result.videoUrl !== task.videoUrl) setPaths((value) => ({ ...value, [index]: "" }));
    if (onTaskUpdate) await onTaskUpdate(index, { ...task, ...result, taskId: task.taskId });
  });
  const play = (index: string, task: ScriptVideoTask) => run(index, async () => {
    const current = generation.current;
    const media = await loadAuthenticatedMedia(task.videoUrl!, "mp4");
    if (current !== generation.current) return;
    if (!media.path) throw new Error("服务端未返回可播放的视频文件");
    setPaths((value) => ({ ...value, [index]: media.path }));
  });
  const entries = Object.entries(tasks).filter(([, task]) => task && typeof task === "object");
  if (!entries.length) return <View className="mp-card"><Text className="mp-row-description">这条历史记录尚无视频任务。</Text></View>;
  return <View>{entries.map(([index, original]) => {
    const task = latest[index]?.taskId === original.taskId ? latest[index] : original;
    return <View className="mp-card" key={index}>
      <Text className="mp-row-title">分镜 {Number.isFinite(Number(index)) ? Number(index) + 1 : index}</Text>
      <Text className="mp-row-description">{task.status || "服务端未返回状态"}</Text>
      {task.error ? <Text className="mp-error">{task.error}</Text> : null}
      {errors[index] ? <Text className="mp-error">{errors[index]}</Text> : null}
      {paths[index] ? <Video src={paths[index]} controls onError={() => setErrors((value) => ({ ...value, [index]: "微信播放器无法播放此文件，请核对视频格式或重新读取" }))} /> : null}
      {task.videoUrl ? <Button className="mp-secondary" loading={busy[index]} disabled={busy[index]} onClick={() => play(index, task)}>查看视频</Button> : <Text className="mp-row-description">服务端尚未返回视频地址，可刷新已有任务状态。</Text>}
      {task.taskId ? <Button className="mp-secondary" loading={busy[index]} disabled={busy[index]} onClick={() => refresh(index, task)}>刷新状态</Button> : null}
    </View>;
  })}</View>;
}
