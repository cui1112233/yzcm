export function cleanFetchWebSettings(settings: Record<string, any>) {
  const { password: _password, ...safe } = settings;
  return safe;
}

export async function writeFetchMaintenance(request: (path: string, options?: any) => Promise<any>, path: string, method: "POST" | "PATCH" | "DELETE", data: unknown, readPath: string) {
  let result: any;
  try { result = await request(path, { method, ...(data === undefined ? {} : { data }) }); }
  catch { throw new Error("操作结果未知，请刷新读回核对后再操作，勿重复提交"); }
  if (result?.ok === false || result?.error) throw new Error(String(result.error || result.message || "服务端拒绝操作"));
  try { return await request(readPath); }
  catch { throw new Error("操作结果未知：保存后的读回失败，请刷新核对，勿重复提交"); }
}
