import { apiRequest } from "./api";
import { apiUrl } from "../config/runtime";
import { onSessionInvalidated } from "./sessionBoundary";

// Only a navigation identifier lives in memory; project contents always come from the server.
const selectedProjects = new Map<string, string>();
onSessionInvalidated(() => selectedProjects.clear());
export function rememberPanelProject(id: string) { selectedProjects.set(new URL(apiUrl("/")).origin, id); }
export function selectedPanelProject() { return selectedProjects.get(new URL(apiUrl("/")).origin) || ""; }

export type PanelProject = { id: string; name: string; data: Record<string, any>; updated_at?: string; [key: string]: any };
export function panelProject(payload: any, expectedId?: string): PanelProject {
  const value = payload?.project;
  if (!value?.id || typeof value.name !== "string" || !value.data || typeof value.data !== "object" || Array.isArray(value.data)) throw new Error("小说面板项目回执缺少有效项目数据");
  if (expectedId && value.id !== expectedId) throw new Error("服务端返回了不匹配的项目");
  return value;
}
export async function readPanelProject(id: string) {
  return panelProject(await apiRequest(`/api/novel-panel/projects/${encodeURIComponent(id)}`), id);
}
function canonical(value: any): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  if (value && typeof value === "object") return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonical(value[key])}`).join(",")}}`;
  return JSON.stringify(value);
}
export async function savePanelProject(project: PanelProject) {
  const result = panelProject(await apiRequest("/api/novel-panel/projects", { method: "POST", data: { project_id: project.id || undefined, name: project.name, data: project.data } }), project.id || undefined);
  const readback = await readPanelProject(result.id);
  if (readback.name !== project.name || canonical(readback.data) !== canonical(project.data)) throw new Error("项目已提交，但读回内容不一致，请刷新核对后再操作");
  return readback;
}
export function panelEntities(data: Record<string, any>, key: string) {
  const source = Array.isArray(data[key]) ? data[key] : [];
  return source.map((item: any, index: number) => ({
    id: String(item.id || item.asset_id || item.slot_id || index),
    name: String(item.name || item.scene_name || item.title || `条目 ${index + 1}`),
    description: String((key === "final_segments" ? item.current_text : "") || item.appearance || item.description || item.prompt || item.visual_description || item.source_text || item.text || ""),
    raw: item,
  }));
}
export function updatePanelEntity(data: Record<string, any>, key: string, index: number, patch: Record<string, any>) {
  if (!Array.isArray(data[key]) || !data[key][index]) throw new Error("所选条目已不存在，请刷新");
  return { ...data, [key]: data[key].map((item: any, position: number) => position === index ? { ...item, ...patch } : item) };
}
export function panelOutlineRequest(data: Record<string, any>) {
  return {
    novel_text: String(data.novel_text || ""),
    genre: data.style?.genre || "", trailer_style: data.style?.trailer_style || "", camera: data.style?.camera || "",
    characters: Array.isArray(data.characters) ? data.characters : [],
    audio_total_seconds: data.audio_total_seconds || 0,
    must_cover_details: data.must_cover_details || "", shot_rhythm_requirements: data.shot_rhythm_requirements || "",
  };
}
export function panelImageUrl(asset: any): string {
  const direct = String(asset?.main_image_url || asset?.mainImageUrl || asset?.image_url || asset?.url || "");
  if (direct) return /^https:\/\/[^/]*tos[^/]*\.volces\.com\//i.test(direct) ? `/api/novel-panel/reference-assets/legacy?url=${encodeURIComponent(direct)}` : direct.replace(/^\/api\/reference-assets\//, "/api/novel-panel/reference-assets/");
  const variant = asset?.has_main_image ? "main" : asset?.has_source_image ? "source" : "";
  if (!variant || !asset?.asset_id || !asset?.type) return "";
  const revision = asset[`${variant}_revision`] || asset[`${variant}_updated_at`] || asset.updated_at || asset.created_at || "1";
  return `/api/novel-panel/reference-assets/file/${encodeURIComponent(asset.type)}/${encodeURIComponent(asset.asset_id)}/${variant}?v=${encodeURIComponent(revision)}`;
}
