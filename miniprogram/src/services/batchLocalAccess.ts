import { apiRequest } from "./api";

export const BATCH_LOCAL_NOTICE = "批量工厂操作必须使用当前服务的批量接口，未发送操作请求。";

export async function batchLocalRequest<T>(path: string, options: Record<string, unknown> = {}): Promise<T> {
  const method = String(options.method || "GET").toUpperCase();
  if (method !== "GET" && method !== "HEAD") {
    const prefix = "/api/batch-factory/v12/";
    if (!path.startsWith(prefix) || !new URL(path, "https://batch.invalid").pathname.startsWith(prefix)) throw new Error(BATCH_LOCAL_NOTICE);
  }
  return apiRequest<T>(path, { ...options, recoverSession: false });
}
