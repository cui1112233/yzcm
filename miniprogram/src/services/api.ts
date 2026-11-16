import Taro from "@tarojs/taro";
import { apiUrl, requireApiOrigin } from "../config/runtime";
import { BUSINESS_TOKEN_KEY, clearBusinessToken, getBusinessToken, getSessionCookie } from "./session";
import { withRequestDeadline } from "./requestDeadline";
import { sessionRevision } from "./sessionBoundary";

type ApiMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE" | "HEAD";

export async function apiRequest<T>(path: string, options: Record<string, unknown> = {}): Promise<T> {
  requireApiOrigin();
  const target = apiUrl(path);
  const sameOrigin = new URL(target).origin === new URL(apiUrl("/")).origin;
  const studio = sameOrigin && new URL(target).pathname.startsWith("/api/studio/");
  const token = sameOrigin ? getBusinessToken() : "";
  const cookie = sameOrigin ? getSessionCookie() : "";
  const revision = sessionRevision();
  if (studio && !cookie) throw new Error("Studio 不可用：当前登录未返回有效 Cookie 会话，请重新登录；不会使用业务令牌伪造会话");
  const method = String(options.method || "GET").toUpperCase() as ApiMethod;
  const timeout = typeof options.timeout === "number" && options.timeout > 0 ? options.timeout : 15000;
  const customHeaders = Object.fromEntries(Object.entries(options.header as Record<string, string> || {}).filter(([key]) => !["cookie", "authorization"].includes(key.toLowerCase())));
  const response = await withRequestDeadline(() => Taro.request<T>({
    url: target, method, data: options.data, timeout,
    header: {
      "content-type": "application/json",
      ...customHeaders,
      ...(cookie ? { Cookie: cookie } : {}),
      ...(studio ? { "X-Studio-Request": "1" } : token ? { Authorization: `Bearer ${token}` } : {}),
    },
    ...(options.responseType ? { responseType: options.responseType as "text" | "arraybuffer" } : {}),
  }), timeout);

  // Account changes invalidate both successful and failing late responses.
  if (sameOrigin && (sessionRevision() !== revision || getBusinessToken() !== token || getSessionCookie() !== cookie)) throw new Error("账号已切换，请重新读取当前账号数据");
  if (response.statusCode >= 200 && response.statusCode < 300) return response.data;
  const payload = response.data as { error?: string | { message?: string; code?: string }; message?: string; code?: string } | undefined;
  const headers = response.header || {};
  const sessionFailure = Object.entries(headers).some(([name, value]) => name.toLowerCase() === "x-qiantie-auth-failure" && value === "session")
    || path === "/api/login/session";
  if (sameOrigin && response.statusCode === 401 && sessionFailure && options.recoverSession !== false) {
    clearBusinessToken();
    void Taro.reLaunch({ url: "/pages/bind/index" });
    throw new Error("登录已失效，请重新输入账号密码");
  }
  const nested = payload?.error && typeof payload.error === "object" ? payload.error : null;
  const reason = payload?.message || nested?.message || (typeof payload?.error === "string" ? payload.error : "") || `请求失败：${response.statusCode}`;
  // message 只保留状态码，不拼响应体：响应体挂在 error.body 上，
  // 由需要排障的调用方（漫画链路）自己决定是否展示，避免污染其他页面的 toast。
  throw Object.assign(new Error(`${reason}（HTTP ${response.statusCode}）`), {
    statusCode: response.statusCode,
    code: nested?.code || payload?.code,
    body: response.data,
    requestData: options.data,
    requestUrl: target,
    method,
  });
}

export { BUSINESS_TOKEN_KEY };
