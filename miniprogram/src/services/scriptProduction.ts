import { apiRequest } from "./api";
import type { ScriptVideoTask } from "./scriptWorkspace";
import { sessionRevision } from "./sessionBoundary";
const uncertainSubmissions = new Set<string>();
const unsyncedReceipts = new Map<string, ScriptVideoTask>();
const historyWrites = new Map<string, Promise<unknown>>();
export function retainedScriptVideoTasks(historyId: string) {
  const prefix = `${sessionRevision()}:${historyId}:`;
  return Object.fromEntries([...unsyncedReceipts].filter(([key]) => key.startsWith(prefix)).map(([key, task]) => [key.slice(prefix.length), task]));
}
export function splitScriptShots(output: string) {
  const text = output.trim();
  if (!text) return [];
  const headings = [...text.matchAll(/^###\s+分镜(?:\d+|[一二三四五六七八九十百千万两]+)(?:[ \t]*(?:（[^\n]*）|\([^\n]*\)))?[ \t]*$/gim)];
  return headings.length ? headings.map((match, index) => text.slice(match.index, headings[index + 1]?.index ?? text.length).trim()) : [text];
}
export function scriptVideoPayload(prompt: string, modelKey: string, fallback: string, imageUrls: string[]) {
  if (!modelKey || !prompt.trim()) throw new Error("请先选择视频模型并填写分镜");
  const images = imageUrls.filter(Boolean).slice(0, 9);
  if (modelKey !== "minimax-h3-video") return { prompt, modelKey, ...(images.length ? { imageUrls: images } : {}) };
  const marker = prompt.match(/总时长\s*[：:]\s*([+-]?\d+(?:\.\d+)?)\s*s/i);
  const duration = /总时长\s*[：:]/.test(prompt) ? Number(marker?.[1] || NaN) : /15/.test(fallback) ? 15 : 10;
  if (!Number.isInteger(duration) || duration < 1 || duration > 15) throw new Error("H3 视频总时长须为 1–15 秒整数");
  return { prompt, modelKey, duration, resolution: "480p竖", imageUrls: images };
}
export async function createShotVideo(historyId: string, index: string, prompt: string, model: string, duration: string, tasks: Record<string, ScriptVideoTask>, images: string[] = []) {
  if (!historyId) throw new Error("请先保存剧本历史，再生成视频");
  const submission = `${sessionRevision()}:${historyId}:${index}`;
  const retained = unsyncedReceipts.get(submission);
  if (retained && !["failed", "error", "cancelled", "canceled"].includes(String(retained.status))) {
    let next = { ...tasks, [index]: retained };
    let warning = "";
    try { next = await persistScriptVideoTasks(historyId, { [index]: retained }); }
    catch { warning = `任务 ${retained.taskId} 已提交，历史同步仍未确认，请勿重复生成。`; }
    return { tasks: next, warning };
  }
  if (uncertainSubmissions.has(submission)) throw new Error("此分镜提交结果未知，请先核对服务端任务，已阻止重复生成");
  if (tasks[index]?.taskId && !["failed", "error", "cancelled", "canceled"].includes(String(tasks[index].status))) throw new Error("此分镜已有任务，请刷新状态或查看结果");
  const data = scriptVideoPayload(prompt, model, duration, images);
  uncertainSubmissions.add(submission);
  let serverTasks: Record<string, ScriptVideoTask>;
  try {
    const payload = await apiRequest<any>(`/api/history/${encodeURIComponent(historyId)}`);
    const entry = payload?.entry || payload;
    if (!entry || typeof entry !== "object") throw new Error("历史读取失败，未提交视频");
    serverTasks = entry.videoTasks || {};
  } catch (cause) { uncertainSubmissions.delete(submission); throw cause; }
  if (serverTasks[index]?.taskId && !["failed", "error", "cancelled", "canceled"].includes(String(serverTasks[index].status))) {
    uncertainSubmissions.delete(submission);
    unsyncedReceipts.set(submission, serverTasks[index]);
    return { tasks: { ...tasks, ...serverTasks }, warning: "已恢复此分镜的已有任务，未重复生成" };
  }
  const response = await apiRequest<ScriptVideoTask>("/api/script-video", { method: "POST", timeout: 180000, data });
  if (!response?.taskId) throw new Error("未收到视频任务编号，请核对服务端记录，勿重复提交");
  uncertainSubmissions.delete(submission);
  let next = { ...tasks, ...serverTasks, [index]: { taskId: response.taskId, status: "processing", prompt } };
  unsyncedReceipts.set(submission, next[index]);
  let warning = "";
  try { next = await persistScriptVideoTasks(historyId, { [index]: next[index] }); }
  catch (cause) { warning = `任务 ${response.taskId} 已提交，历史同步未确认：${cause instanceof Error ? cause.message : "读取失败"}。请勿重复生成。`; }
  return { tasks: next, warning };
}
export async function persistScriptVideoTasks(historyId: string, tasks: Record<string, ScriptVideoTask>) {
  const session = sessionRevision(), key = `${session}:${historyId}`;
  const operation = (historyWrites.get(key) || Promise.resolve()).catch(() => {}).then(async () => {
    if (session !== sessionRevision()) throw new Error("账号已切换，取消旧账号历史保存");
    const before = await apiRequest<any>(`/api/history/${encodeURIComponent(historyId)}`);
    const previous = (before?.entry || before)?.videoTasks || {};
    for (const [index, task] of Object.entries(tasks)) {
      if (previous[index]?.taskId && previous[index].taskId !== task.taskId && !["failed", "error", "cancelled", "canceled"].includes(String(previous[index].status))) throw new Error("服务端已有其他视频任务，已阻止旧记录覆盖，请重新读取历史");
    }
    const merged = { ...previous, ...tasks };
    await apiRequest(`/api/history/${encodeURIComponent(historyId)}`, { method: "PATCH", data: { videoTasks: merged } });
    const payload = await apiRequest<any>(`/api/history/${encodeURIComponent(historyId)}`);
    const entry = payload?.entry || payload;
    if (Object.entries(tasks).some(([index, value]) => entry?.videoTasks?.[index]?.taskId !== value.taskId)) throw new Error("视频任务历史读回不一致");
    for (const [index, value] of Object.entries(merged) as [string, ScriptVideoTask][]) {
      const receiptKey = `${key}:${index}`;
      unsyncedReceipts.set(receiptKey, value);
    }
    return merged as Record<string, ScriptVideoTask>;
  });
  historyWrites.set(key, operation);
  try { return await operation; } finally { if (historyWrites.get(key) === operation) historyWrites.delete(key); }
}
export type PersonalPrompt = { id?: string; category: string; name: string; body: string };
export async function listPersonalPrompts(category: string): Promise<PersonalPrompt[]> {
  const payload = await apiRequest<any>(`/api/script-constraint-prompts?category=${encodeURIComponent(category)}`);
  if (!Array.isArray(payload?.prompts)) throw new Error("服务端未返回提示词列表");
  return payload.prompts;
}
export async function savePersonalPrompt(prompt: PersonalPrompt) {
  const data = { category: prompt.category, name: prompt.name.trim(), body: prompt.body.trim() };
  if (!data.name || !data.body) throw new Error("请填写名称和提示词正文");
  await apiRequest(`/api/script-constraint-prompts${prompt.id ? `/${encodeURIComponent(prompt.id)}` : ""}`, { method: prompt.id ? "PUT" : "POST", data });
  const list = await listPersonalPrompts(data.category);
  if (!list.some(item => (!prompt.id || item.id === prompt.id) && item.name === data.name && item.body === data.body)) throw new Error("提示词保存后读回不一致，请刷新核对");
  return list;
}
export async function deletePersonalPrompt(prompt: PersonalPrompt) {
  if (!prompt.id) throw new Error("缺少提示词编号");
  await apiRequest(`/api/script-constraint-prompts/${encodeURIComponent(prompt.id)}`, { method: "DELETE" });
  const list = await listPersonalPrompts(prompt.category);
  if (list.some(item => item.id === prompt.id)) throw new Error("删除未确认，请刷新核对");
  return list;
}
