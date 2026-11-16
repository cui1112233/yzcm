import Taro from "@tarojs/taro";
import { apiRequest } from "./api";
import { getBusinessToken, getSessionCookie } from "./session";
import { apiUrl } from "../config/runtime";
import { onSessionInvalidated, sessionRevision } from "./sessionBoundary";

type CachedMedia = { path: string; fromCache: boolean };

const cache = new Map<string, string>();
onSessionInvalidated(resetMediaCache);

function safeExtension(extension: string) {
  const value = String(extension || "bin").toLowerCase().replace(/[^a-z0-9]/g, "");
  return value || "bin";
}

function hash(value: string) {
  let result = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    result ^= value.charCodeAt(index);
    result = Math.imul(result, 16777619);
  }
  return (result >>> 0).toString(16);
}

function filePathFor(url: string, extension: string) {
  const root = String(Taro.env?.USER_DATA_PATH || "").trim();
  if (!root) throw new Error("小程序临时目录不可用，无法读取媒体");
  return `${root}/yzcm-media-${hash(url)}.${safeExtension(extension)}`;
}

function writeFile(filePath: string, data: ArrayBuffer) {
  return new Promise<void>((resolve, reject) => {
    Taro.getFileSystemManager().writeFile({
      filePath,
      data,
      success: () => resolve(),
      fail: (error: unknown) => reject(error),
    });
  });
}

function isDownloadDomainRestriction(cause: unknown) {
  const source = cause && typeof cause === "object" ? cause as { errMsg?: unknown; message?: unknown } : {};
  return /url not in domain list/i.test(String(source.errMsg || source.message || cause || ""));
}

function downloadFailureMessage(cause: unknown) {
  const source = cause && typeof cause === "object" ? cause as { errMsg?: unknown; message?: unknown } : {};
  return typeof source.errMsg === "string" ? source.errMsg : typeof source.message === "string" ? source.message : "媒体下载失败";
}

// 只有视频走 downloadFile（它的域名白名单另说）；图片与合集压缩包直接走 request 拉二进制，
// 避免 downloadFile 白名单没配主域时，每张图都刷一条 “url not in domain list”。
const DOWNLOAD_EXTENSIONS = new Set(["mp4"]);

export async function loadAuthenticatedMedia(url: string, extension = "bin"): Promise<CachedMedia> {
  const normalizedUrl = String(url || "").trim();
  if (!normalizedUrl) throw new Error("服务端未返回媒体地址");
  const token = getBusinessToken();
  const cookie = getSessionCookie();
  const revision = sessionRevision();
  const scopedUrl = `${apiUrl(normalizedUrl)}:${token}:${cookie}:${revision}`;
  const cached = cache.get(scopedUrl);
  if (cached) return { path: cached, fromCache: true };
  if (DOWNLOAD_EXTENSIONS.has(safeExtension(extension)) && typeof Taro.downloadFile === "function") {
    const sameOrigin = new URL(apiUrl(normalizedUrl)).origin === new URL(apiUrl("/")).origin;
    try {
      const downloaded = await new Promise<string>((resolve, reject) => {
        let task: Taro.DownloadTask | undefined;
        let settled = false;
        const deadline = setTimeout(() => { settled = true; task?.abort(); reject(new Error("视频下载超时，请检查网络后重试")); }, 120000);
        task = Taro.downloadFile({
          url: apiUrl(normalizedUrl), timeout: 120000,
          header: sameOrigin ? { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(cookie ? { Cookie: cookie } : {}) } : {},
          success: (result) => {
            if (settled) { if (result.tempFilePath) removeFile(result.tempFilePath); return; }
            settled = true;
            clearTimeout(deadline);
            if (result.statusCode < 200 || result.statusCode >= 300 || !result.tempFilePath) { if (result.tempFilePath) removeFile(result.tempFilePath); reject(new Error(`视频下载失败：${result.statusCode}`)); return; }
            resolve(result.tempFilePath);
          },
          fail: (cause) => { if (settled) return; settled = true; clearTimeout(deadline); reject(cause); },
        });
      });
      if (token !== getBusinessToken() || cookie !== getSessionCookie() || revision !== sessionRevision()) { removeFile(downloaded); throw new Error("账号已切换，已清理旧账号媒体"); }
      cache.set(scopedUrl, downloaded);
      return { path: downloaded, fromCache: false };
    } catch (cause) {
      if (!isDownloadDomainRestriction(cause)) throw new Error(downloadFailureMessage(cause));
    }
  }
  const path = filePathFor(scopedUrl, extension);
  // 视频与合集压缩包改走 request 时需放宽超时，避免几 MB 的文件卡在默认 15 秒
  const largeBinary = ["mp4", "zip"].includes(safeExtension(extension));
  const data = await apiRequest<ArrayBuffer>(normalizedUrl, { responseType: "arraybuffer", recoverSession: false, ...(largeBinary ? { timeout: 120000 } : {}) });
  if (token !== getBusinessToken() || cookie !== getSessionCookie() || revision !== sessionRevision()) throw new Error("账号已切换，已取消旧账号媒体读取");
  if (!(data instanceof ArrayBuffer)) throw new Error("服务端媒体回执不是有效二进制内容");
  await writeFile(path, data);
  if (token !== getBusinessToken() || cookie !== getSessionCookie() || revision !== sessionRevision()) {
    removeFile(path);
    throw new Error("登录已变化，已清理旧账号媒体");
  }
  cache.set(scopedUrl, path);
  return { path, fromCache: false };
}

export function forgetAuthenticatedMedia(url: string) {
  const key = `${apiUrl(url)}:${getBusinessToken()}:${getSessionCookie()}:${sessionRevision()}`;
  const path = cache.get(key);
  cache.delete(key);
  if (path) removeFile(path);
}

export function resetMediaCache() {
  cache.forEach(removeFile);
  cache.clear();
}

function removeFile(filePath: string) {
  try { Taro.getFileSystemManager().unlink({ filePath, fail: () => undefined }); } catch { /* A cleared sandbox can already have removed the file. */ }
}
