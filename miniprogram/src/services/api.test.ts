import { beforeEach, describe, expect, test, vi } from "vitest";

const taro = vi.hoisted(() => ({
  request: vi.fn(),
  login: vi.fn(),
  getStorageSync: vi.fn(),
  setStorageSync: vi.fn(),
  removeStorageSync: vi.fn(),
  reLaunch: vi.fn(),
}));

const runtime = vi.hoisted(() => ({
  apiUrl: (path: string) => `https://api.test${path}`,
  requireApiOrigin: vi.fn(),
}));

vi.mock("@tarojs/taro", () => ({ default: taro }));
vi.mock("../config/runtime", () => runtime);

import { apiRequest } from "./api";
import { invalidateSessionResources } from "./sessionBoundary";

test("same-token re-login still invalidates an old in-flight response", async () => {
  taro.getStorageSync.mockReturnValue("same-token");
  let finish!: (value: any) => void;
  taro.request.mockReturnValueOnce(new Promise(resolve => { finish = resolve; }));
  const pending = apiRequest("/api/member/me");
  await Promise.resolve();
  invalidateSessionResources();
  finish({ statusCode: 200, data: { member: { username: "old" } } });
  await expect(pending).rejects.toThrow("账号已切换");
});

describe("小程序请求封装", () => {
  test("只读请求遇到 401 不换票、不清理令牌、不跳转绑定", async () => {
    vi.resetAllMocks();
    taro.request.mockResolvedValue({ statusCode: 401, data: { error: "Unauthorized" } });
    await expect(apiRequest("/api/batch-factory/v11/batches", { recoverSession: false })).rejects.toThrow("Unauthorized");
    expect(taro.request).toHaveBeenCalledTimes(1);
    expect(taro.login).not.toHaveBeenCalled();
    expect(taro.removeStorageSync).not.toHaveBeenCalled();
    expect(taro.reLaunch).not.toHaveBeenCalled();
  });
  beforeEach(() => {
    vi.resetAllMocks();
    runtime.requireApiOrigin.mockImplementation(() => undefined);
  });

  test("携带业务令牌请求 HTTPS API", async () => {
    taro.getStorageSync.mockReturnValue("business-token");
    taro.request.mockResolvedValue({ statusCode: 200, data: { ok: true } });

    await expect(apiRequest("/api/member/me")).resolves.toEqual({ ok: true });
    expect(taro.request).toHaveBeenCalledWith(expect.objectContaining({
      url: expect.stringContaining("/api/member/me"),
      header: expect.objectContaining({ Authorization: "Bearer business-token" }),
    }));
  });

  test("会话过期返回账号登录，不换微信票据或重放读取", async () => {
    taro.getStorageSync.mockReturnValue("expired-token");
    taro.request.mockResolvedValue({ statusCode: 401, data: { error: "Unauthorized" }, header: { "X-Qiantie-Auth-Failure": "session" } });
    await expect(apiRequest("/api/history")).rejects.toThrow("登录已失效");
    expect(taro.request).toHaveBeenCalledTimes(1);
    expect(taro.login).not.toHaveBeenCalled();
    expect(taro.reLaunch).toHaveBeenCalledWith({ url: "/pages/bind/index" });
  });

  test("写请求遇到过期会话不会自动重放", async () => {
    taro.getStorageSync.mockReturnValue("expired-token");
    taro.request.mockResolvedValue({ statusCode: 401, data: {}, header: { "X-Qiantie-Auth-Failure": "session" } });
    await expect(apiRequest("/api/tasks/42/retry", { method: "POST" })).rejects.toThrow("登录已失效");
    expect(taro.request).toHaveBeenCalledTimes(1);
    expect(taro.login).not.toHaveBeenCalled();
  });

  test("服务端地址未配置时不会发送用户数据请求", async () => {
    runtime.requireApiOrigin.mockImplementation(() => {
      throw new Error("服务端地址未配置，请使用已配置微信合法域名的 HTTPS 构建");
    });

    await expect(apiRequest("/api/member/me")).rejects.toThrow(
      "服务端地址未配置，请使用已配置微信合法域名的 HTTPS 构建",
    );
    expect(taro.request).not.toHaveBeenCalled();
  });

  test("配音请求保留二进制响应类型", async () => {
    taro.request.mockResolvedValue({ statusCode: 200, data: new ArrayBuffer(4) });

    await expect(apiRequest("/api/tts", {
      method: "POST",
      data: { input: "你好" },
      responseType: "arraybuffer",
    })).resolves.toBeInstanceOf(ArrayBuffer);

    expect(taro.request).toHaveBeenCalledWith(expect.objectContaining({ responseType: "arraybuffer" }));
  });

  test("JSON 业务写入保留方法和载荷", async () => {
    taro.request.mockResolvedValue({ statusCode: 200, data: { projects: [] } });

    await apiRequest("/api/agent/tasks", { method: "POST", data: {} });

    expect(taro.request).toHaveBeenCalledWith(expect.objectContaining({
      method: "POST", data: {},
    }));
  });
});
