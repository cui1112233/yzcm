import { beforeEach, expect, test, vi } from "vitest";
const state = vi.hoisted(() => ({ origin: "http://127.0.0.1:13192", storage: new Map<string, string>() }));
const taro = vi.hoisted(() => ({ request: vi.fn(), login: vi.fn(), reLaunch: vi.fn(),
  getStorageSync: vi.fn(), setStorageSync: vi.fn(), removeStorageSync: vi.fn() }));
vi.mock("@tarojs/taro", () => ({ default: taro }));
vi.mock("../config/runtime", () => ({ requireApiOrigin: () => undefined,
  apiUrl: (path: string) => new URL(path, state.origin).toString() }));
import { getBusinessToken, loginLocalAccount } from "./session";
import { apiRequest } from "./api";

beforeEach(() => {
  vi.resetAllMocks(); state.origin = "http://127.0.0.1:13192"; state.storage.clear();
  taro.getStorageSync.mockImplementation((key) => state.storage.get(key));
  taro.setStorageSync.mockImplementation((key, value) => state.storage.set(key, value));
  taro.removeStorageSync.mockImplementation((key) => state.storage.delete(key));
});

test("local credentials obtain a real server token and public tokens are isolated", async () => {
  state.storage.set("yzcm.miniprogram.authToken:https://yzcm.site", "public-token");
  expect(getBusinessToken()).toBe("");
  taro.request.mockResolvedValue({ statusCode: 200, data: { token: "local-token", username: "local-user" } });
  await expect(loginLocalAccount(" local-user ", "password")).resolves.toMatchObject({ state: "authenticated", account: { username: "local-user" } });
  expect(taro.request).toHaveBeenCalledWith(expect.objectContaining({ url: "http://127.0.0.1:13192/api/login", data: { username: "local-user", password: "password", remember: true } }));
  expect(getBusinessToken()).toBe("local-token");
  expect([...state.storage.values()]).not.toContain("password");
  state.origin = "http://127.0.0.1:18140"; expect(getBusinessToken()).toBe("");
  state.origin = "https://yzcm.site"; expect(getBusinessToken()).toBe("public-token");
});

test("local login cannot send credentials to the public site", async () => {
  state.origin = "https://yzcm.site";
  await expect(loginLocalAccount("local-user", "password")).rejects.toThrow(/本地/);
  expect(taro.request).not.toHaveBeenCalled();
});

test("rejected credentials never create a session", async () => {
  taro.request.mockResolvedValue({ statusCode: 401, data: { error: "用户名或密码错误" } });
  await expect(loginLocalAccount("local-user", "wrong")).rejects.toThrow("用户名或密码错误");
  expect(getBusinessToken()).toBe("");
});

test("local token is not attached to media requests on another origin", async () => {
  taro.request.mockResolvedValue({ statusCode: 200, data: { token: "local-token" } });
  await loginLocalAccount("local-user", "password");
  taro.request.mockClear();
  taro.request.mockResolvedValue({ statusCode: 200, data: "media" });
  await apiRequest("https://media.example.test/video.mp4");
  expect(taro.request.mock.calls[0][0].header.Authorization).toBeUndefined();
});

test("expired local session returns to login without WeChat or replaying a write", async () => {
  taro.request.mockResolvedValueOnce({ statusCode: 200, data: { token: "local-token" } });
  await loginLocalAccount("local-user", "password");
  taro.request.mockClear();
  taro.request.mockResolvedValue({ statusCode: 401, data: {}, header: { "X-Qiantie-Auth-Failure": "session" } });
  await expect(apiRequest("/api/example", { method: "POST", data: { item: "one" } })).rejects.toThrow(/登录/);
  expect(getBusinessToken()).toBe("");
  expect(taro.request).toHaveBeenCalledTimes(1);
  expect(taro.login).not.toHaveBeenCalled();
  expect(taro.reLaunch).toHaveBeenCalledWith({ url: "/pages/bind/index" });
});
