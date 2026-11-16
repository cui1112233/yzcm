import Taro from "@tarojs/taro";
import { Button, Image, Text, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import { apiRequest } from "../../services/api";
import { entityImagePayload, entityImageUrls, generateEntityImage } from "../../services/scriptEntityMedia";
import { loadAuthenticatedMedia } from "../../services/mediaCache";
import { sessionRevision } from "../../services/sessionBoundary";
import type { ScriptEntity } from "../../services/scriptWorkspace";

export default function ScriptEntityMedia({ type, entity, model, novel, preset, onChange }: { type: string; entity: ScriptEntity; model: string; novel: string; preset: string; onChange: (entity: ScriptEntity) => void }) {
  const [error, setError] = useState(""), [busy, setBusy] = useState(false), [paths, setPaths] = useState<Record<string, string>>({});
  const id = useRef(entity.id || `entity-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`), lock = useRef(false), mounted = useRef(true);
  const urls = entityImageUrls(entity), main = (typeof entity.mainImageUrl === "string" ? entity.mainImageUrl : (entity.data as any)?.mainImageUrl) || urls[0];
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const append = (url: string) => onChange({ ...entity, id: id.current, imageUrls: [...new Set([...urls, url])], mainImageUrl: main || url });
  const create = async (upload: boolean) => {
    if (lock.current) return; lock.current = true; setBusy(true); setError(""); const session = sessionRevision();
    try {
      const payload = upload ? null : entityImagePayload(type, { ...entity, id: id.current }, novel, preset, model);
      const confirmed = await Taro.showModal({ title: upload ? "上传参考图" : "生成参考图", content: upload ? "选择图片上传到当前账号资产库？" : "调用所选图片模型生成参考图，可能产生费用。确认继续？" });
      if (!confirmed.confirm || !mounted.current || session !== sessionRevision()) return;
      let url: string;
      if (upload) {
        const picked = await Taro.chooseImage({ count: 1, sizeType: ["compressed"] });
        if (!mounted.current || session !== sessionRevision()) return;
        if (!picked.tempFilePaths?.[0]) return;
        if (picked.tempFiles?.[0]?.size > 10 * 1024 * 1024) throw new Error("参考图不能超过 10MB");
        const encoded = await new Promise<string>((resolve, reject) => Taro.getFileSystemManager().readFile({ filePath: picked.tempFilePaths[0], encoding: "base64", success: value => resolve(String(value.data)), fail: reject }));
        if (!mounted.current || session !== sessionRevision()) return;
        const mime = encoded.startsWith("iVBOR") ? "png" : encoded.startsWith("/9j/") ? "jpeg" : encoded.startsWith("UklGR") ? "webp" : "";
        if (!mime || encoded.length > Math.ceil(10 * 1024 * 1024 / 3) * 4) throw new Error("仅支持 10MB 以内 PNG、JPG、WebP 图片");
        const result = await apiRequest<any>("/api/novel-panel/reference-assets/upload", { method: "POST", timeout: 120000, data: { asset_type: type === "characters" ? "character" : "scene", asset_id: id.current, variant: "source", data_url: `data:image/${mime};base64,${encoded}` } });
        url = result?.url || result?.data?.url || result?.result?.url;
        if (typeof url !== "string" || !url.trim()) throw new Error("服务端未返回上传图片地址，请核对资产库");
      } else url = await generateEntityImage(payload!);
      if (mounted.current && session === sessionRevision()) append(url);
    } catch (cause: any) { if (mounted.current) setError(cause.message || cause.errMsg || "参考图操作失败"); }
    finally { lock.current = false; if (mounted.current) setBusy(false); }
  };
  const view = async (url: string) => {
    if (lock.current) return; lock.current = true; setBusy(true); setError("");
    try { const file = await loadAuthenticatedMedia(url, "png"); if (mounted.current) setPaths(current => ({ ...current, [url]: file.path })); }
    catch (cause: any) { if (mounted.current) setError(cause.message); }
    finally { lock.current = false; if (mounted.current) setBusy(false); }
  };
  return <View><Text className="mp-field-label">参考图片 · {urls.length} 张</Text>{error ? <Text className="mp-error">{error}</Text> : null}{urls.map(url => <View key={url}>{paths[url] ? <Image src={paths[url]} mode="widthFix" onError={() => setError("图片展示失败，请重新读取")} /> : <Button disabled={busy} onClick={() => void view(url)}>查看图片{url === main ? " · 主图" : ""}</Button>}<Button disabled={busy || url === main} onClick={() => onChange({ ...entity, mainImageUrl: url })}>{url === main ? "当前主图" : "设为主图"}</Button><Button disabled={busy} onClick={() => { const remaining = urls.filter(value => value !== url); onChange({ ...entity, imageUrls: remaining, mainImageUrl: main === url ? remaining[0] || "" : main }); }}>移出当前设定</Button></View>)}<Button disabled={busy} loading={busy} onClick={() => void create(true)}>上传参考图</Button><Button disabled={busy || !model} onClick={() => void create(false)}>用所选模型生成参考图</Button><Text className="mp-row-description">点击“应用修改”用于当前剧本；生成剧本时随历史一起保存。</Text></View>;
}
