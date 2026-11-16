import { onSessionInvalidated, sessionRevision } from "./sessionBoundary";
type UnknownRecord = Record<string, unknown>;
const validationError = (message: string) => Object.assign(new Error(message), { unsubmitted: true });
let pendingRun: { key: string; session: number } | null = null;
onSessionInvalidated(() => { pendingRun = null; });
export function pendingStudioRun() { return pendingRun?.session === sessionRevision() ? pendingRun.key : ""; }
export function settleStudioRuns(runs: Record<string, any>[]) {
  if (pendingRun && runs.some(run => run.id && run.request?.request_key === pendingRun?.key)) pendingRun = null;
}

export type StudioTextModel = {
  id: number;
  name: string;
};

function asRecord(value: unknown): UnknownRecord | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as UnknownRecord : null;
}

export function textModelsFromPayload(payload: unknown): StudioTextModel[] {
  const root = asRecord(payload);
  const models = Array.isArray(root?.models) ? root.models : [];

  return models.flatMap((candidate) => {
    const model = asRecord(candidate);
    const id = Number(model?.id);
    if (model?.kind !== "text" || model?.enabled === false || !Number.isSafeInteger(id) || id < 1) return [];
    const name = typeof model?.name === "string" && model.name.trim() ? model.name.trim() : `文本模型 ${id}`;
    return [{ id, name }];
  });
}

export function buildAgentProject(name: string) {
  const normalized = name.trim();
  if (!normalized) throw validationError("请先填写项目名称");
  return {
    name: normalized,
    document: { nodes: [], edges: [], viewport: { x: 0, y: 0, zoom: 1 } },
  };
}

export function buildAgentPlanRequest(textModelId: string | number, message: string, projectName = "") {
  const id = Number(textModelId);
  const normalizedMessage = message.trim();
  if (!Number.isSafeInteger(id) || id < 1) throw validationError("请先选择可用的文本模型");
  if (!normalizedMessage) throw validationError("请先输入任务内容");
  const normalizedProjectName = projectName.trim();
  return {
    text_model_id: id,
    message: normalizedMessage,
    ...(normalizedProjectName ? { context: `当前项目：${normalizedProjectName}` } : {}),
  };
}

export type StudioRecord = Record<string, any>;
export type StudioProject = { id: string; name: string; revision: number; document: StudioRecord };
export function studioList(payload: any, key: string): StudioRecord[] {
  if (!Array.isArray(payload?.[key])) throw new Error(`Studio ${key === "projects" ? "项目" : key} 响应不完整`);
  return payload[key];
}
function projectRecord(payload: any): StudioProject {
  if (!payload?.id || !Number.isSafeInteger(payload.revision) || payload.revision < 1 || !asRecord(payload.document)) throw new Error("Studio 项目文档或版本响应不完整");
  return payload;
}
export function createStudioClient(request: (path: string, options?: any) => Promise<any>) {
  const projectPath = (id: string) => `/api/studio/agent/projects/${encodeURIComponent(id)}`;
  const readProject = async (id: string) => projectRecord(await request(projectPath(id)));
  const readSavedProject = async (id: string) => {
    try { const result = await readProject(id); if (result.id !== id) throw new Error("项目 ID 不一致"); return result; }
    catch { throw new Error("项目保存结果未知：写入后的同项目读回失败，请重新读取核对"); }
  };
  return {
    projects: async () => studioList(await request("/api/studio/agent/projects"), "projects"),
    project: readProject,
    createProject: async (name: string, id?: string) => {
      const result = projectRecord(await request("/api/studio/agent/projects", { method: "POST", data: { ...buildAgentProject(name), ...(id ? { id } : {}) } }));
      return readSavedProject(result.id);
    },
    saveProject: async (project: StudioProject) => {
      if (!project.name.trim()) throw validationError("请输入项目名称");
      const { id, name, revision, document } = project;
      await request(projectPath(id), { method: "PUT", data: { id, name, revision, document } });
      return readSavedProject(project.id);
    },
    removeProject: async (id: string) => { await request(projectPath(id), { method: "DELETE" }); return studioList(await request("/api/studio/agent/projects"), "projects"); },
    catalog: async () => {
      const [models, assets, runs, kits, skills] = await Promise.allSettled([request("/api/studio/models"), request("/api/studio/assets"), request("/api/studio/runs"), request("/api/studio/agent/brand-kits"), request("/api/studio/agent/skills")]);
      const errors: string[] = [];
      const value = (result: PromiseSettledResult<any>, key: string) => { try { if (result.status === "rejected") throw result.reason; return studioList(result.value, key); } catch (e: any) { errors.push(`${key}: ${e.message}`); return []; } };
      return { models: value(models, "models"), assets: value(assets, "assets"), runs: value(runs, "runs"), brandKits: value(kits, "brand_kits"), skills: value(skills, "skills"), enabled: models.status === "fulfilled" && models.value?.generation_enabled === true, errors };
    },
    plan: (data: StudioRecord) => request("/api/studio/agent/plan", { method: "POST", data, timeout: 180000 }),
    submitRun: async (data: StudioRecord) => {
      if (data.confirmed !== true) throw validationError("请先确认媒体生成");
      if (pendingStudioRun()) throw new Error("上次生成结果未知，请先核对运行回执，禁止重复提交");
      if (!data.request_key) throw validationError("缺少请求标识");
      const ticket = { key: data.request_key, session: sessionRevision() }; pendingRun = ticket;
      try {
        const run = await request("/api/studio/runs", { method: "POST", data, timeout: 180000 });
        if (!run?.id) throw new Error("生成结果未知：服务器未返回运行 ID，请核对运行记录，禁止重复提交");
        if (pendingRun === ticket) pendingRun = null;
        return run;
      } catch (e: any) { if (e.statusCode >= 400 && e.statusCode < 500 && pendingRun === ticket) pendingRun = null; throw e; }
    },
  };
}
export function buildStudioRun({ mode, instruction, imageModelId = "", videoModelId = "", assetId = "", requestKey, confirmed }: { mode: "image" | "video" | "image_to_video" | "image_edit"; instruction: string; imageModelId?: string; videoModelId?: string; assetId?: string; requestKey: string; confirmed: boolean }) {
  if (!confirmed) throw new Error("请先确认媒体生成");
  if (!instruction.trim()) throw new Error("请输入生成要求");
  const image = Number(imageModelId), video = Number(videoModelId);
  if ((mode === "image" || mode === "image_edit" || mode === "image_to_video") && (!Number.isSafeInteger(image) || image < 1)) throw new Error("请选择可用图片模型");
  if ((mode === "video" || mode === "image_to_video") && (!Number.isSafeInteger(video) || video < 1)) throw new Error("请选择可用视频模型");
  if ((mode === "video" || mode === "image_edit") && !assetId) throw new Error("请先选择服务端图片素材");
  if (!requestKey) throw new Error("缺少请求标识");
  return { request_key: requestKey, mode, instruction: instruction.trim(), image_model_id: mode === "video" ? 0 : image, video_model_id: mode === "image" || mode === "image_edit" ? 0 : video, input_asset_id: assetId, size: "", duration: 0, confirmed: true };
}
