import { apiRequest } from "./api";
import { sessionRevision } from "./sessionBoundary";
const pending = new Set<string>();
const receipts = new Map<string, string>();
export function entityImageUrls(entity: any): string[] {
  const raw = entity.imageUrls ?? entity.data?.imageUrls ?? entity.data?.images ?? entity.data?.imageList ?? [];
  const urls = Array.isArray(raw) ? raw.map(value => typeof value === "string" ? value : value?.url || value?.imageUrl || value?.src).filter(Boolean) : [];
  const main = typeof entity.mainImageUrl === "string" ? entity.mainImageUrl : entity.data?.mainImageUrl || entity.imageUrl;
  return [...new Set<string>([...urls, ...(main ? [main] : [])])];
}
export function entityImagePayload(type: string, entity: any, novelText: string, extractionPreset: string, imageModelId: string) {
  if (!imageModelId) throw new Error("请先选择图片模型");
  const fields = entity.data || entity;
  const name = fields.角色名称 || fields.场景名称 || fields.姓名 || fields.名称 || fields.name;
  if (!entity.id || !name) throw new Error("请先填写人物或场景名称");
  const assetType = type === "characters" ? "character" : "scene";
  const description = Object.entries(fields).filter(([key, value]) => typeof value === "string" && value.trim() && !/^(id|imageUrls|images|imageList|mainImageUrl)$/.test(key)).map(([key, value]) => `${key}：${String(value).trim()}`).join("；");
  return { asset_type: assetType, asset_id: entity.id, description, context: novelText.trim(), generation_guidance: assetType === "character" ? `生成${name}的人物参考图：完整人物、清晰服装与外形特征、干净背景，保持角色身份和设定一致。${extractionPreset ? `提取预设：${extractionPreset}。` : ""}` : `生成${name}的场景参考图：电影级空间构图，明确时段、环境、光线与氛围，保持场景设定一致。${extractionPreset ? `提取预设：${extractionPreset}。` : ""}`, ...(assetType === "character" ? { character: { name } } : {}), imageModelId };
}
export async function generateEntityImage(payload: ReturnType<typeof entityImagePayload>) {
  const { asset_id: _assetId, ...content } = payload;
  const key = `${sessionRevision()}:${JSON.stringify(content)}`;
  if (receipts.has(key)) return receipts.get(key)!;
  if (pending.has(key)) throw new Error("此参考图生成结果尚未确认，请核对服务端资产，已阻止重复生成");
  pending.add(key);
  const result = await apiRequest<any>("/api/novel-panel/reference-assets/generate", { method: "POST", data: payload, timeout: 180000 });
  const url = result?.url || result?.data?.url || result?.result?.url;
  if (typeof url !== "string" || !url.trim()) throw new Error("未收到参考图地址，请核对服务端资产，勿重复生成");
  receipts.set(key, url); pending.delete(key);
  return url;
}
