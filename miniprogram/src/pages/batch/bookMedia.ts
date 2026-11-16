import type { BatchFactoryBookWorkspace } from "../../services/batchFactoryV11";

export function mediaFailureMessage(cause: unknown, phase: "download" | "playback") {
  const error = cause && typeof cause === "object" ? cause as { message?: unknown; errMsg?: unknown; errCode?: unknown } : {};
  const detail = error.errMsg || error.message || (typeof cause === "string" ? cause : "");
  const reason = typeof detail === "string" ? detail.replace(/(?:https?|wxfile):\/\/[^\s"'<>]+/gi, "[媒体地址]").slice(0, 300) : "";
  const code = typeof error.errCode === "number" && Number.isFinite(error.errCode) ? `（错误码 ${error.errCode}）` : "";
  return `${phase === "download" ? "视频读取失败" : "视频播放失败"}：${reason || "播放器或下载接口未返回具体原因"}${code}`;
}

export async function readAssetImage(url: string, download: (url: string, extension: string) => Promise<{ path: string }>) {
  if (!url) return { path: "", error: "" };
  try {
    const extension = url.match(/\.([a-z0-9]{2,5})(?:\?|$)/i)?.[1] || "png";
    const result = await download(url, extension);
    if (!result.path) throw new Error("未返回图片文件");
    return { path: result.path, error: "" };
  } catch (cause) {
    return { path: "", error: `图片读取失败：${cause instanceof Error ? cause.message : "下载失败"}` };
  }
}

export function stagePrompt(value: unknown): string {
  if (!value || typeof value !== "object") return "";
  if (Array.isArray(value)) return value.map(stagePrompt).filter(Boolean).join("\n\n");
  const source = value as Record<string, unknown>;
  const prompt = source.videoPrompt || source.prompt || source.visualPrompt;
  if (typeof prompt === "string" && prompt.trim()) return prompt.trim();
  return stagePrompt(source.data) || stagePrompt(source.stages) || stagePrompt(source.shots);
}

export function bookMediaOptions(workspace: BatchFactoryBookWorkspace) {
  const videos = workspace.book?.videos;
  const tasks = workspace.production?.tasks || [];
  // Saved storyboard definitions own count/order; production may include retired jobs.
  // Keep task indices in keys so the workbench still resolves task-specific errors.
  const shots = Array.isArray(videos) ? videos.map((video, index) => {
    const taskIndex = video.id ? tasks.findIndex(task => task.videoId === video.id) : -1;
    const task = tasks[taskIndex];
    return {
      key: taskIndex >= 0 ? `shot-${taskIndex}` : `video-${video.id || index}`,
      label: `分镜 ${index + 1}`, url: String(task?.mediaUrl || "").trim(),
      prompt: String(video.videoPrompt || video.visualPrompt || task?.prompt || ""),
      status: task?.status || video.status,
      videoId: String(video.id || ""),
      revision: video.revision,
    };
  }) : tasks.map((task, index) => ({
    key: `shot-${index}`, label: `分镜 ${index + 1}`, url: String(task.mediaUrl || "").trim(),
    prompt: String(task.prompt || ""), status: task.status, videoId: String(task.videoId || ""), revision: task.revision,
  }));
  const merged = (workspace.mergeStatus?.jobs || [])
    .filter((job) => String(job.bookId) === String(workspace.bookId))
    .map((job, index) => ({ key: `merge-${index}`, label: `合成成片 ${index + 1}`, url: String(job.outputUrl || job.mediaUrl || "").trim(), prompt: "", status: job.status, videoId: "", revision: undefined }));
  return [...shots, ...merged];
}
