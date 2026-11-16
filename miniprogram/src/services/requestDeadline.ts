export async function withRequestDeadline<T>(request: () => PromiseLike<T>, milliseconds = 15000): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      Promise.resolve().then(request),
      new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error("请求超时，请检查网络后重试")), milliseconds); }),
    ]);
  } catch (cause) {
    if (cause instanceof Error) throw cause;
    throw new Error(String((cause as { errMsg?: string })?.errMsg || "网络连接失败，请重试"));
  } finally { if (timer) clearTimeout(timer); }
}
