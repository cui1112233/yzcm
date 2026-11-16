import type { PublicMemberCenter } from "../config/profileData";
import { apiRequest } from "./api";
import { isLocalAccountLogin } from "../config/localAuth";

export async function readMemberProfile(): Promise<PublicMemberCenter> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      apiRequest<PublicMemberCenter>("/api/member/me", { timeout: 12000 }),
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error("账号资料读取超时，请检查网络后重新读取")), 15000);
      }),
    ]);
  } catch (cause) {
    const failure = cause as { statusCode?: number; message?: string; errMsg?: string } | null;
    if (failure?.statusCode === 401) throw new Error(isLocalAccountLogin() ? "本地登录已失效，请退出登录后重新登录本地账号。" : "当前登录状态已失效，请退出后重新登录公网账号。");
    throw new Error(failure?.message || failure?.errMsg || "账号资料读取失败，请检查网络后重试");
  } finally {
    if (timer !== undefined) clearTimeout(timer);
  }
}
