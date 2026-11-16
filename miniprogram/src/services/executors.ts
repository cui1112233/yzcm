import { apiRequest } from "./api";

export type ExecutorSummary = { paired: number; onlineCount: number };

/** 只统计服务端明确标记 online === true 的执行器，不猜测缺失字段。 */
export function summarizeExecutors(payload: unknown): ExecutorSummary {
  const source = payload && typeof payload === "object" ? payload as { executors?: unknown; items?: unknown } : {};
  const list = Array.isArray(source.executors) ? source.executors : Array.isArray(source.items) ? source.items : null;
  if (!list) throw new Error("服务端未返回执行器列表");
  const onlineCount = list.filter((item) => item && typeof item === "object" && (item as { online?: unknown }).online === true).length;
  return { paired: list.length, onlineCount };
}

export async function readExecutorSummary(): Promise<ExecutorSummary> {
  return summarizeExecutors(await apiRequest<unknown>("/api/shuihuo-production/local-executors", { timeout: 12000 }));
}
