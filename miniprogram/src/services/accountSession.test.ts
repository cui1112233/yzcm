import { beforeEach, expect, test, vi } from "vitest";

const state = vi.hoisted(() => ({ storage: new Map<string, string>(), origin: "https://yzcm.site" }));
const taro = vi.hoisted(() => ({ request: vi.fn(), login: vi.fn(), reLaunch: vi.fn(), getStorageSync: vi.fn(), setStorageSync: vi.fn(), removeStorageSync: vi.fn() }));
vi.mock("@tarojs/taro", () => ({ default: taro }));
vi.mock("../config/runtime", () => ({ requireApiOrigin: () => undefined, apiUrl: (p: string) => new URL(p, state.origin).toString() }));
import { loginAccount, getBusinessToken, getSessionCookie, hasAccountSession, logoutAccount, clearBusinessToken } from "./session";
import { apiRequest } from "./api";
import { authStorageKey } from "../config/localAuth";

beforeEach(() => {
  clearBusinessToken();
  vi.resetAllMocks(); state.storage.clear(); state.origin = "https://yzcm.site";
  taro.getStorageSync.mockImplementation((key) => state.storage.get(key));
  taro.setStorageSync.mockImplementation((key, value) => state.storage.set(key, value));
  taro.removeStorageSync.mockImplementation((key) => state.storage.delete(key));
});

test("Studio uses only cookies returned by verified login, never synthesizes one from bearer token", async () => {
  taro.request.mockResolvedValueOnce({ statusCode: 200, cookies: ["ycm_session=legitimate; Path=/; HttpOnly; Secure"], data: { token: "verified" } })
    .mockResolvedValueOnce({ statusCode: 200, data: { username: "alice" } });
  await loginAccount("alice", "password");
  expect(getSessionCookie()).toBe("ycm_session=legitimate");
  taro.request.mockResolvedValueOnce({ statusCode: 200, data: { projects: [] } });
  await apiRequest("/api/studio/agent/projects");
  expect(taro.request.mock.calls[2][0].header).toMatchObject({ Cookie: "ycm_session=legitimate", "X-Studio-Request": "1" });
  expect(taro.request.mock.calls[2][0].header).not.toHaveProperty("Authorization");
  state.origin = "https://other.example";
  expect(getSessionCookie()).toBe("");
});
test("token-only login leaves Studio explicitly unavailable without sending a request", async () => {
  taro.request.mockResolvedValueOnce({ statusCode: 200, data: { token: "verified" } }).mockResolvedValueOnce({ statusCode: 200, data: { username: "alice" } });
  await loginAccount("alice", "password");
  await expect(apiRequest("/api/studio/runs")).rejects.toThrow(/Cookie|会话/);
  expect(taro.request).toHaveBeenCalledTimes(2);
});
test("login rejects foreign-domain and expired cookies without publishing Studio access", async () => {
  taro.request.mockResolvedValueOnce({ statusCode: 200, cookies: ["ycm_session=foreign; Domain=evil.example; Path=/; HttpOnly; Secure", "old=expired; Max-Age=0; Path=/; HttpOnly; Secure"], data: { token: "verified" } })
    .mockResolvedValueOnce({ statusCode: 200, data: { username: "alice" } });
  await loginAccount("alice", "password"); expect(getSessionCookie()).toBe("");
});
test("cookie-only legitimate login is verified by cookie without inventing a business token", async () => {
  taro.request.mockResolvedValueOnce({ statusCode: 200, cookies: ["ycm_session=cookie-only; Path=/; HttpOnly; Secure"], data: { user: { username: "alice" } } })
    .mockResolvedValueOnce({ statusCode: 200, data: { user: { username: "alice" } } });
  await loginAccount("alice", "password");
  expect(getBusinessToken()).toBe(""); expect(getSessionCookie()).toBe("ycm_session=cookie-only");
  expect(hasAccountSession()).toBe(true);
  expect(taro.request.mock.calls[1][0].header).toEqual({ Cookie: "ycm_session=cookie-only" });
  await logoutAccount(); expect(getSessionCookie()).toBe(""); expect(hasAccountSession()).toBe(false);
});
test("Go Studio cookie login uses documented /api/me only after legacy verifier returns 404 and logs out via /api/logout", async () => {
  taro.request.mockResolvedValueOnce({ statusCode: 200, cookies: ["ycm_session=go-cookie; Path=/; HttpOnly; Secure"], data: { user: { username: "alice" } } })
    .mockResolvedValueOnce({ statusCode: 404, data: {} })
    .mockResolvedValueOnce({ statusCode: 200, data: { user: { id: 12, username: "alice" } } });
  await loginAccount("alice", "password");
  expect(taro.request.mock.calls[2][0]).toMatchObject({ url: "https://yzcm.site/api/me", header: { Cookie: "ycm_session=go-cookie" } });
  taro.request.mockResolvedValueOnce({ statusCode: 204 }); await logoutAccount();
  expect(taro.request.mock.calls[3][0].url).toBe("https://yzcm.site/api/logout");
});
test("Cookie verification 401 never falls back to another authentication API", async () => {
  taro.request.mockResolvedValueOnce({ statusCode: 200, cookies: ["ycm_session=go-cookie; Path=/; HttpOnly; Secure"], data: {} }).mockResolvedValueOnce({ statusCode: 401, data: {} });
  await expect(loginAccount("alice", "password")).rejects.toThrow();
  expect(taro.request).toHaveBeenCalledTimes(2); expect(getSessionCookie()).toBe("");
});

test("public credentials and MFA use existing login then verify the returned identity", async () => {
  taro.request.mockResolvedValueOnce({ statusCode: 200, data: { token: "verified-token", username: "alice" } })
    .mockResolvedValueOnce({ statusCode: 200, data: { username: "alice", role: "member" } });
  const session = await loginAccount(" alice ", "password", "123456");
  expect(session.account.username).toBe("alice");
  expect(getBusinessToken()).toBe("verified-token");
  expect(taro.request.mock.calls.map(([r]) => [r.url, r.method])).toEqual([
    ["https://yzcm.site/api/login", "POST"], ["https://yzcm.site/api/login/session", "GET"],
  ]);
  expect(taro.request.mock.calls[0][0].data).toEqual({ username: "alice", password: "password", remember: true, mfaCode: "123456" });
  expect([...state.storage.values()]).not.toContain("password");
  expect(taro.login).not.toHaveBeenCalled();
});

test("login never publishes a token if session verification fails or returns another account", async () => {
  taro.request.mockResolvedValueOnce({ statusCode: 200, data: { token: "unverified" } })
    .mockResolvedValueOnce({ statusCode: 200, data: { username: "someone-else" } });
  await expect(loginAccount("alice", "password")).rejects.toThrow(/账号|身份/);
  expect(getBusinessToken()).toBe("");
});

test("a delayed 401 from an older login cannot clear the new account", async () => {
  state.storage.set(authStorageKey("yzcm.miniprogram.authToken"), "old");
  let resolve!: (value: unknown) => void;
  taro.request.mockReturnValueOnce(new Promise((done) => { resolve = done; }));
  const result = apiRequest("/api/history").catch((cause) => cause);
  state.storage.set(authStorageKey("yzcm.miniprogram.authToken"), "new");
  resolve({ statusCode: 401, data: { error: "Unauthorized" }, header: { "X-Qiantie-Auth-Failure": "session" } });
  await result;
  expect(getBusinessToken()).toBe("new");
  expect(taro.login).not.toHaveBeenCalled();
  expect(taro.reLaunch).not.toHaveBeenCalled();
});

test("a business/provider 401 does not log the user out or initiate WeChat recovery", async () => {
  state.storage.set(authStorageKey("yzcm.miniprogram.authToken"), "current");
  taro.request.mockResolvedValue({ statusCode: 401, data: { message: "提供方凭证失效" } });
  await expect(apiRequest("/api/chat", { method: "POST" })).rejects.toThrow("提供方凭证失效");
  expect(getBusinessToken()).toBe("current");
  expect(taro.login).not.toHaveBeenCalled();
});

test("logout revokes the old session but preserves a newer login arriving while revocation is pending", async () => {
  state.storage.set(authStorageKey("yzcm.miniprogram.authToken"), "old");
  let resolve!: (value: unknown) => void;
  taro.request.mockReturnValueOnce(new Promise((done) => { resolve = done; }));
  const pending = logoutAccount();
  expect(getBusinessToken()).toBe("");
  state.storage.set(authStorageKey("yzcm.miniprogram.authToken"), "new");
  resolve({ statusCode: 200, data: {} });
  await pending;
  expect(getBusinessToken()).toBe("new");
  expect(taro.request.mock.calls[0][0].url).toBe("https://yzcm.site/api/login/logout");
});

test("sessions are isolated between public service origins", async () => {
  state.storage.set(authStorageKey("yzcm.miniprogram.authToken"), "origin-a");
  state.origin = "https://other.example";
  expect(getBusinessToken()).toBe("");
  state.origin = "https://yzcm.site";
  expect(getBusinessToken()).toBe("origin-a");
});

test("the public build migrates the valid token key used by earlier mini-program versions", () => {
  state.storage.set("yzcm.miniprogram.authToken", "legacy-public-token");

  expect(getBusinessToken()).toBe("legacy-public-token");
  expect(state.storage.get(authStorageKey("yzcm.miniprogram.authToken"))).toBe("legacy-public-token");
  expect(state.storage.has("yzcm.miniprogram.authToken")).toBe(false);
});

test("a legacy public token is never migrated into another service origin", () => {
  state.storage.set("yzcm.miniprogram.authToken", "legacy-public-token");
  state.origin = "https://other.example";

  expect(getBusinessToken()).toBe("");
  expect(state.storage.get("yzcm.miniprogram.authToken")).toBe("legacy-public-token");
});
