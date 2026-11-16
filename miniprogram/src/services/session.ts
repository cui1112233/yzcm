import Taro from "@tarojs/taro";
import { apiUrl, requireApiOrigin } from "../config/runtime";
import { authStorageKey, isLocalAccountLogin } from "../config/localAuth";
import { invalidateSessionResources, sessionRevision } from "./sessionBoundary";
import { withRequestDeadline } from "./requestDeadline";

export const BUSINESS_TOKEN_KEY = "yzcm.miniprogram.authToken";
export const BINDING_TICKET_KEY = "yzcm.miniprogram.bindingTicket";
const LEGACY_PUBLIC_API_ORIGIN = "https://yzcm.site";

type Account = {
  username?: string;
  displayName?: string;
  role?: string;
};

let loginAttempt = 0;
// Native requests have no browser cookie jar. Keep verified login cookies only
// in memory, scoped to the exact origin and the currently published token.
const sessionCookies = new Map<string, { cookie: string; token: string; expires: number; logoutPath?: string }>();
const volatileTokens = new Map<string, string>();
const REMEMBERED_SESSION_KEY = "yzcm.miniprogram.rememberedSession";
const maxRememberedAge = 7 * 24 * 60 * 60 * 1000;
function cookieFromLogin(response: any) {
  const raw = Array.isArray(response.cookies) ? response.cookies : Object.entries(response.header || {}).flatMap(([key, value]) => key.toLowerCase() === "set-cookie" ? (Array.isArray(value) ? value : [String(value)]).flatMap(v => String(v).split(/,(?=\s*[\w-]+=)/)) : []);
  const origin = new URL(apiUrl("/"));
  let expires = Number.POSITIVE_INFINITY;
  const pairs = raw.flatMap((line: string) => {
    const parts = line.split(";").map(v => v.trim());
    const pair = parts.shift() || "";
    if (!/^[\w-]+=[^;\s,]+$/.test(pair)) return [];
    const attributes = Object.fromEntries(parts.map(v => { const i = v.indexOf("="); return [v.slice(0, i < 0 ? undefined : i).toLowerCase(), i < 0 ? true : v.slice(i + 1)]; }));
    // Some native runtimes expose only name=value in response.cookies.
    // Known session cookie names still require the subsequent identity read.
    if ((!attributes.httponly && !/^(ycm_session|ycm_access)=/.test(pair)) || (attributes.path && attributes.path !== "/")) return [];
    if (attributes.secure && origin.protocol !== "https:") return [];
    if (attributes.domain && String(attributes.domain).replace(/^\./, "").toLowerCase() !== origin.hostname.toLowerCase()) return [];
    const expiry = attributes["max-age"] !== undefined ? Date.now() + Number(attributes["max-age"]) * 1000 : attributes.expires ? Date.parse(String(attributes.expires)) : Number.POSITIVE_INFINITY;
    if (Number.isNaN(expiry) || expiry <= Date.now()) return [];
    expires = Math.min(expires, expiry);
    return [pair];
  });
  return { cookie: pairs.join("; "), expires };
}
export function getSessionCookie() {
  const origin = new URL(apiUrl("/")).origin;
  const token = getBusinessToken();
  if (!sessionCookies.has(origin)) {
    const saved = Taro.getStorageSync(authStorageKey(REMEMBERED_SESSION_KEY));
    if (saved && saved.origin === origin && saved.token === token && typeof saved.cookie === "string" && saved.expires > Date.now() && saved.expires <= Date.now() + maxRememberedAge && ["/api/logout", "/api/login/logout"].includes(saved.logoutPath)) {
      sessionCookies.set(origin, { cookie: saved.cookie, token, expires: saved.expires, logoutPath: saved.logoutPath });
    }
  }
  const entry = sessionCookies.get(origin);
  return entry && entry.token === getBusinessToken() && entry.expires > Date.now() ? entry.cookie : "";
}
export function hasAccountSession() { return Boolean(getBusinessToken() || getSessionCookie()); }

type AuthenticatedSession = {
  state: "authenticated";
  account: Account;
};

type BindingRequiredSession = {
  state: "binding-required";
  bindingTicket: string;
};

type SessionResponse = {
  token?: string;
  account?: Account;
  bindingRequired?: boolean;
  bindingTicket?: string;
  error?: string;
  message?: string;
};

export type WechatSession = AuthenticatedSession | BindingRequiredSession;

function boundedAuthOperation<T>(operation: () => PromiseLike<T>): Promise<T> {
  let timer: ReturnType<typeof setTimeout>;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error("微信登录请求超时，请检查网络后重试")), 15000);
  });
  return Promise.race([Promise.resolve().then(operation), timeout]).catch((cause: unknown) => {
    if (cause instanceof Error) throw cause;
    throw new Error("微信登录网络连接失败，请检查网络后重试");
  }).finally(() => clearTimeout(timer));
}

export function getBusinessToken() {
  const origin = new URL(apiUrl("/")).origin;
  if (volatileTokens.has(origin)) return volatileTokens.get(origin) || "";
  const saved = Taro.getStorageSync(authStorageKey(REMEMBERED_SESSION_KEY));
  if (saved && saved.origin === origin && saved.expires <= Date.now()) {
    Taro.removeStorageSync(authStorageKey(REMEMBERED_SESSION_KEY));
    Taro.removeStorageSync(authStorageKey(BUSINESS_TOKEN_KEY));
    return "";
  }
  const scopedKey = authStorageKey(BUSINESS_TOKEN_KEY);
  const scopedToken = String(Taro.getStorageSync(scopedKey) || "");
  if (scopedToken || origin !== LEGACY_PUBLIC_API_ORIGIN) return scopedToken;

  // Releases before origin-scoped sessions stored the public token under the
  // unscoped key. Move only that known production session; never import it
  // into local or third-party service origins.
  const legacyToken = String(Taro.getStorageSync(BUSINESS_TOKEN_KEY) || "");
  if (!legacyToken) return "";
  Taro.setStorageSync(scopedKey, legacyToken);
  Taro.removeStorageSync(BUSINESS_TOKEN_KEY);
  return legacyToken;
}

export function clearBusinessToken() {
  loginAttempt += 1;
  invalidateSessionResources();
  recentVerification = null;
  pendingVerification = null;
  sessionCookies.clear();
  volatileTokens.clear();
  Taro.removeStorageSync(authStorageKey(REMEMBERED_SESSION_KEY));
  Taro.removeStorageSync(authStorageKey(BUSINESS_TOKEN_KEY));
  Taro.removeStorageSync(BUSINESS_TOKEN_KEY);
  Taro.removeStorageSync(authStorageKey(BINDING_TICKET_KEY));
}

export function saveBindingTicket(ticket: string) {
  Taro.setStorageSync(authStorageKey(BINDING_TICKET_KEY), ticket);
}

export function getBindingTicket() {
  return String(Taro.getStorageSync(authStorageKey(BINDING_TICKET_KEY)) || "");
}

function saveAuthenticatedSession(payload: SessionResponse, cookie?: { cookie: string; expires: number; logoutPath?: string }, remember?: boolean): AuthenticatedSession {
  const token = String(payload.token || "").trim();
  if (!token && !cookie?.cookie) throw new Error("登录成功，但服务器没有返回业务令牌或 Cookie 会话");
  invalidateSessionResources();
  const origin = new URL(apiUrl("/")).origin;
  volatileTokens.delete(origin);
  Taro.removeStorageSync(authStorageKey(REMEMBERED_SESSION_KEY));
  if (token && remember !== false) Taro.setStorageSync(authStorageKey(BUSINESS_TOKEN_KEY), token);
  else Taro.removeStorageSync(authStorageKey(BUSINESS_TOKEN_KEY));
  if (remember === false) volatileTokens.set(origin, token);
  sessionCookies.clear();
  if (cookie?.cookie) sessionCookies.set(new URL(apiUrl("/")).origin, { ...cookie, token });
  if (remember === true) {
    Taro.setStorageSync(authStorageKey(REMEMBERED_SESSION_KEY), { origin, token, cookie: cookie?.cookie || "", expires: Math.min(cookie?.expires || Infinity, Date.now() + maxRememberedAge), logoutPath: cookie?.logoutPath || "/api/login/logout" });
  }
  return { state: "authenticated", account: payload.account || {} };
}

async function postSession(path: string, data: Record<string, string | boolean>) {
  requireApiOrigin();
  const response = await boundedAuthOperation(() => Taro.request<SessionResponse>({
    url: apiUrl(path),
    method: "POST",
    data,
    timeout: 12000,
    header: { "content-type": "application/json" },
  }));

  if (response.statusCode < 200 || response.statusCode >= 300) {
    if (response.statusCode === 404) {
      throw new Error("当前服务端微信登录接口不存在或尚未上线，请联系管理员");
    }
    const message = response.data?.message || response.data?.error || "微信登录服务不可用";
    throw new Error(message);
  }
  return response.data || {};
}

export async function bootstrapWechatSession(): Promise<WechatSession> {
  requireApiOrigin();
  const login = await boundedAuthOperation(() => Taro.login({ timeout: 12000 }));
  const code = String(login.code || "").trim();
  if (!code) throw new Error("未获取到微信登录凭证，请重试");

  const payload = await postSession("/api/wechat-miniprogram/session", { code });
  if (payload.bindingRequired) {
    const bindingTicket = String(payload.bindingTicket || "").trim();
    if (!bindingTicket) throw new Error("服务器未返回账号绑定凭证");
    return { state: "binding-required", bindingTicket };
  }
  return saveAuthenticatedSession(payload);
}

export async function bindExistingAccount(
  bindingTicket: string,
  username: string,
  password: string,
): Promise<AuthenticatedSession> {
  const account = String(username || "").trim();
  if (!account || !password) throw new Error("请填写账号和密码后完成绑定");

  let ticket = String(bindingTicket || "").trim();
  if (!ticket) {
    const session = await bootstrapWechatSession();
    if (session.state === "authenticated") return session;
    ticket = session.bindingTicket;
    saveBindingTicket(ticket);
  }

  const payload = await postSession("/api/wechat-miniprogram/bind", {
    bindingTicket: ticket,
    username: account,
    password,
  });
  const session = saveAuthenticatedSession(payload);
  Taro.removeStorageSync(authStorageKey(BINDING_TICKET_KEY));
  return session;
}

export async function loginLocalAccount(username: string, password: string): Promise<AuthenticatedSession> {
  if (!isLocalAccountLogin()) throw new Error("账号密码调试登录仅允许连接本地服务");
  const account = username.trim();
  if (!account || !password) throw new Error("请填写本地账号和密码");
  const payload = await postSession("/api/login", { username: account, password, remember: true });
  return saveAuthenticatedSession({ ...payload, account: payload.account || payload as Account });
}

function accountFromPayload(payload: any): Account {
  const account = payload?.account || payload?.member || payload?.user || payload;
  if (!account || typeof account.username !== "string" || !account.username.trim()) {
    throw new Error("服务端未返回可验证的账号身份，请重新登录");
  }
  return account;
}

async function verifyAccountToken(token: string, cookie = ""): Promise<{ account: Account; logoutPath: string }> {
  let response = await withRequestDeadline(() => Taro.request<any>({
    url: apiUrl("/api/login/session"), method: "GET", timeout: 12000,
    header: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(cookie ? { Cookie: cookie } : {}) },
  }));
  let logoutPath = "/api/login/logout";
  if (response.statusCode === 404 && cookie) {
    response = await withRequestDeadline(() => Taro.request<any>({ url: apiUrl("/api/me"), method: "GET", timeout: 12000, header: { Cookie: cookie } }));
    logoutPath = "/api/logout";
  }
  if (response.statusCode < 200 || response.statusCode >= 300) {
    throw Object.assign(new Error(response.data?.message || response.data?.error || "登录会话已失效，请重新登录"), { statusCode: response.statusCode });
  }
  return { account: accountFromPayload(response.data), logoutPath };
}

// 登录页验证通过后会立刻跳转到工作区，工作区 onShow 会再次验证同一份令牌；
// 3 秒窗口内复用上一次的成功结果即可去掉这次重复请求，窗口又足够短，不会把失效状态粘住。
const SESSION_VERIFY_TTL_MS = 3000;
let recentVerification: { key: string; at: number; session: AuthenticatedSession } | null = null;
let pendingVerification: { key: string; promise: Promise<AuthenticatedSession> } | null = null;

function verificationKey(token: string, cookie: string) {
  // 键里带会话版本：换号、登出或清理会话后版本自增，旧结果自动失配
  return `${sessionRevision()}|${token}|${cookie}`;
}

export async function readAccountSession(): Promise<AuthenticatedSession> {
  requireApiOrigin();
  const token = getBusinessToken();
  const cookie = getSessionCookie();
  if (!token && !cookie) throw new Error("请先登录账号");
  const key = verificationKey(token, cookie);
  if (recentVerification && recentVerification.key === key && Date.now() - recentVerification.at < SESSION_VERIFY_TTL_MS) {
    return recentVerification.session;
  }
  if (pendingVerification && pendingVerification.key === key) return pendingVerification.promise;
  const promise = (async () => {
    const { account } = await verifyAccountToken(token, cookie);
    if (getBusinessToken() !== token || getSessionCookie() !== cookie) throw new Error("账号已切换，请重新读取");
    const session: AuthenticatedSession = { state: "authenticated", account };
    // 只有成功结果进入短时缓存，失败不缓存，避免把“会话已失效”粘住
    recentVerification = { key, at: Date.now(), session };
    return session;
  })();
  pendingVerification = { key, promise };
  try {
    return await promise;
  } finally {
    if (pendingVerification?.key === key) pendingVerification = null;
  }
}

export async function loginAccount(username: string, password: string, mfaCode = "", options?: { remember: boolean }): Promise<AuthenticatedSession> {
  requireApiOrigin();
  const target = new URL(apiUrl("/"));
  if ((!isLocalAccountLogin() && target.protocol !== "https:") || target.username || target.password) {
    throw new Error("账号登录仅允许 HTTPS 服务或本机调试服务");
  }
  const usernameValue = username.trim();
  if (!usernameValue || !password) throw new Error("请填写账号和密码");
  const attempt = ++loginAttempt;
  const response = await withRequestDeadline(() => Taro.request<any>({
    url: apiUrl("/api/login"), method: "POST", timeout: 12000,
    header: { "content-type": "application/json" },
    data: { username: usernameValue, password, remember: options?.remember ?? true, ...(mfaCode.trim() ? { mfaCode: mfaCode.trim() } : {}) },
  }));
  if (response.statusCode < 200 || response.statusCode >= 300) {
    throw Object.assign(new Error(response.data?.message || response.data?.error || `账号登录失败：${response.statusCode}`), {
      statusCode: response.statusCode, code: response.data?.error,
    });
  }
  const token = String(response.data?.token || "").trim();
  const cookie = cookieFromLogin(response);
  if (!token && !cookie.cookie) throw new Error("服务端没有返回登录令牌或合法 Cookie，请检查账号验证要求");
  const { account, logoutPath } = await verifyAccountToken(token, cookie.cookie);
  if (attempt !== loginAttempt) throw new Error("登录操作已取消或账号已切换");
  if (account.username !== usernameValue) throw new Error("登录返回的账号身份不匹配，未保存会话");
  Taro.removeStorageSync(authStorageKey(BINDING_TICKET_KEY));
  return saveAuthenticatedSession({ token, account }, { ...cookie, logoutPath }, options?.remember);
}

export async function logoutAccount(): Promise<{ revoked: boolean }> {
  const token = getBusinessToken();
  const cookie = getSessionCookie();
  const url = apiUrl(cookie ? sessionCookies.get(new URL(apiUrl("/")).origin)?.logoutPath || "/api/login/logout" : "/api/login/logout");
  clearBusinessToken();
  if (!token && !cookie) return { revoked: true };
  try {
    const response = await withRequestDeadline(() => Taro.request({
      url, method: "POST", timeout: 12000, header: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(cookie ? { Cookie: cookie } : {}) },
    }));
    return { revoked: response.statusCode >= 200 && response.statusCode < 300 };
  } catch { return { revoked: false }; }
}
