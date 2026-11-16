type Request = (path: string, options: { method: "POST"; data: unknown; timeout?: number }) => Promise<any>;
function selection(bookId: string) {
  if (!bookId.trim()) throw new Error("请先选择要提交的小说");
  return { mode: "selected", ids: [bookId], force: false, grouped: true };
}
export async function previewFetchSubmission(request: Request, bookId: string) {
  const result = await request("/api/batch-rewrite/web-submit/preview", { method: "POST", data: selection(bookId) });
  if (!result || result.ok === false || result.error || !Array.isArray(result.groups) || !Array.isArray(result.skipped)) throw new Error(result?.message || result?.error || "服务端预览回执格式无效");
  return result;
}
export async function submitFetchSubmission(request: Request, bookId: string) {
  const result = await request("/api/batch-rewrite/web-submit/submit", { method: "POST", data: selection(bookId), timeout: 180000 });
  if (!result || result.ok === false || result.error || !Array.isArray(result.groups) || ![result.success_groups, result.accepted_groups, result.failed_groups].every((value) => typeof value === "number" && Number.isFinite(value) && value >= 0)) throw new Error(result?.message || result?.error || "服务端提交回执无效，请读取任务状态后再操作");
  return result;
}
