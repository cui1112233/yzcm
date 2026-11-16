import { beforeEach, describe, expect, test, vi } from "vitest";

const taro = vi.hoisted(() => ({
  request: vi.fn(),
  login: vi.fn(),
  getStorageSync: vi.fn(),
  setStorageSync: vi.fn(),
  removeStorageSync: vi.fn(),
}));

const runtime = vi.hoisted(() => ({
  apiUrl: (path: string) => `https://api.test${path}`,
  requireApiOrigin: vi.fn(),
}));

vi.mock("@tarojs/taro", () => ({
  default: taro,
}));
vi.mock("../config/runtime", () => runtime);

import {
  bindExistingAccount,
  bootstrapWechatSession,
  clearBusinessToken,
  getBusinessToken,
  readAccountSession,
} from "./session";

describe("微信小程序会话", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    runtime.requireApiOrigin.mockImplementation(() => undefined);
  });

  test("登录接口不存在时说明服务端接口缺失且不继续提交密码", async () => {
    taro.login.mockResolvedValue({ code: "fresh-code" });
    taro.request.mockResolvedValue({ statusCode: 404, data: "404 page not found" });

    await expect(bindExistingAccount("", "ming", "password")).rejects.toThrow(/接口.*未.*上线|接口.*不存在/);
    expect(taro.request).toHaveBeenCalledTimes(1);
    expect(taro.request.mock.calls[0][0].data).toEqual({ code: "fresh-code" });
    expect(taro.setStorageSync).not.toHaveBeenCalled();
  });

  test("后端错误码和中文提示并存时展示中文提示", async () => {
    taro.request.mockResolvedValue({ statusCode: 401, data: {
      error: "invalid_credentials", message: "用户名或密码错误",
    } });
    await expect(bindExistingAccount("ticket", "ming", "password")).rejects.toThrow("用户名或密码错误");
    expect(taro.removeStorageSync).not.toHaveBeenCalled();
  });

  test("原生网络异常转成可展示的错误", async () => {
    taro.request.mockRejectedValue({ errMsg: "request:fail ERR_CONNECTION_RESET" });
    await expect(bindExistingAccount("ticket", "ming", "password")).rejects.toThrow(/网络|连接/);
    expect(taro.removeStorageSync).not.toHaveBeenCalled();
  });

  test("绑定响应缺少令牌时保留绑定凭证", async () => {
    taro.request.mockResolvedValue({ statusCode: 200, data: {} });
    await expect(bindExistingAccount("ticket", "ming", "password")).rejects.toThrow(/令牌/);
    expect(taro.removeStorageSync).not.toHaveBeenCalled();
  });

  test.each(["微信凭证", "登录请求"])("%s挂起后能超时退出且迟到响应不保存会话", async (phase) => {
    vi.useFakeTimers();
    try {
      let resolve!: (value: any) => void;
      const pending = new Promise((done) => { resolve = done; });
      taro.login.mockImplementation(() => phase === "微信凭证" ? pending : Promise.resolve({ code: "code" }));
      taro.request.mockImplementation(() => pending);
      const result = bootstrapWechatSession();
      const rejected = expect(result).rejects.toThrow(/超时/);
      await vi.advanceTimersByTimeAsync(16000);
      await rejected;
      resolve(phase === "微信凭证" ? { code: "late-code" } : {
        statusCode: 200, data: { token: "late-token" },
      });
      await Promise.resolve();
      expect(taro.setStorageSync).not.toHaveBeenCalled();
      if (phase === "微信凭证") expect(taro.request).not.toHaveBeenCalled();
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });

  test("已绑定微信账号会保存服务端业务令牌", async () => {
    taro.login.mockResolvedValue({ code: "once-only-code" });
    taro.request.mockResolvedValue({
      statusCode: 200,
      data: { token: "business-token", account: { username: "ming" } },
    });

    await expect(bootstrapWechatSession()).resolves.toMatchObject({
      state: "authenticated",
      account: { username: "ming" },
    });
    expect(taro.setStorageSync).toHaveBeenCalledWith(expect.stringContaining("yzcm.miniprogram.authToken:"), "business-token");
    expect(taro.request).toHaveBeenCalledWith(expect.objectContaining({
      url: expect.stringContaining("/api/wechat-miniprogram/session"),
      data: { code: "once-only-code" },
    }));
  });

  test("未绑定账号会返回短时绑定凭证而不保存业务令牌", async () => {
    taro.login.mockResolvedValue({ code: "first-code" });
    taro.request.mockResolvedValue({
      statusCode: 200,
      data: { bindingRequired: true, bindingTicket: "short-lived" },
    });

    await expect(bootstrapWechatSession()).resolves.toEqual({
      state: "binding-required",
      bindingTicket: "short-lived",
    });
    expect(taro.setStorageSync).not.toHaveBeenCalled();
  });

  test("绑定既有账号后保存服务端令牌", async () => {
    taro.request.mockResolvedValue({
      statusCode: 200,
      data: { token: "bound-token", account: { username: "ming" } },
    });

    await expect(bindExistingAccount("short-lived", "ming", "password")).resolves.toMatchObject({
      state: "authenticated",
    });
    expect(taro.setStorageSync).toHaveBeenCalledWith(expect.stringContaining("yzcm.miniprogram.authToken:"), "bound-token");
  });

  test("绑定页丢失短期凭证时会重新换取凭证再提交账号绑定", async () => {
    taro.login.mockResolvedValue({ code: "fresh-wechat-code" });
    taro.request
      .mockResolvedValueOnce({
        statusCode: 200,
        data: { bindingRequired: true, bindingTicket: "fresh-ticket" },
      })
      .mockResolvedValueOnce({
        statusCode: 200,
        data: { token: "bound-token", account: { username: "ming" } },
      });

    await expect(bindExistingAccount("", "ming", "password")).resolves.toMatchObject({
      state: "authenticated",
      account: { username: "ming" },
    });

    expect(taro.setStorageSync).toHaveBeenCalledWith(expect.stringContaining("yzcm.miniprogram.bindingTicket:"), "fresh-ticket");
    expect(taro.request).toHaveBeenNthCalledWith(1, expect.objectContaining({
      url: expect.stringContaining("/api/wechat-miniprogram/session"),
      data: { code: "fresh-wechat-code" },
    }));
    expect(taro.request).toHaveBeenNthCalledWith(2, expect.objectContaining({
      url: expect.stringContaining("/api/wechat-miniprogram/bind"),
      data: { bindingTicket: "fresh-ticket", username: "ming", password: "password" },
    }));
  });

  test("令牌从小程序存储读取", () => {
    taro.getStorageSync.mockReturnValue("stored-token");
    expect(getBusinessToken()).toBe("stored-token");
  });

  test("服务端地址未配置时不会把微信登录凭证发往占位地址", async () => {
    taro.login.mockResolvedValue({ code: "once-only-code" });
    runtime.requireApiOrigin.mockImplementation(() => {
      throw new Error("服务端地址未配置，请使用已配置微信合法域名的 HTTPS 构建");
    });

    await expect(bootstrapWechatSession()).rejects.toThrow(
      "服务端地址未配置，请使用已配置微信合法域名的 HTTPS 构建",
    );
    expect(taro.request).not.toHaveBeenCalled();
  });
});

describe("会话校验去重", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    runtime.requireApiOrigin.mockImplementation(() => undefined);
    taro.getStorageSync.mockReturnValue("stored-token");
    clearBusinessToken();
  });

  test("同一令牌短时间内连续读取只发一次校验请求", async () => {
    taro.request.mockResolvedValue({ statusCode: 200, data: { username: "ming" } });

    const first = await readAccountSession();
    const second = await readAccountSession();

    expect(taro.request).toHaveBeenCalledTimes(1);
    expect(taro.request.mock.calls[0][0].url).toContain("/api/login/session");
    expect(second).toEqual(first);
  });

  test("并发读取共用同一条在途请求", async () => {
    let resolve!: (value: any) => void;
    taro.request.mockImplementation(() => new Promise((done) => { resolve = done; }));

    const first = readAccountSession();
    const second = readAccountSession();
    await Promise.resolve();
    expect(taro.request).toHaveBeenCalledTimes(1);

    resolve({ statusCode: 200, data: { username: "ming" } });
    await Promise.all([first, second]);
    expect(taro.request).toHaveBeenCalledTimes(1);
  });

  test("超过有效期后重新发起校验", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    try {
      vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
      taro.request.mockResolvedValue({ statusCode: 200, data: { username: "ming" } });

      await readAccountSession();
      vi.setSystemTime(new Date("2026-01-01T00:00:04Z"));
      await readAccountSession();

      expect(taro.request).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  test("登出清理会话后不再复用旧结果", async () => {
    taro.request.mockResolvedValue({ statusCode: 200, data: { username: "ming" } });

    await readAccountSession();
    clearBusinessToken();
    taro.getStorageSync.mockReturnValue("stored-token");
    await readAccountSession();

    expect(taro.request).toHaveBeenCalledTimes(2);
  });

  test("失败的校验结果不会被缓存", async () => {
    taro.request.mockResolvedValueOnce({ statusCode: 401, data: { message: "会话已失效" } });
    await expect(readAccountSession()).rejects.toThrow();

    taro.request.mockResolvedValueOnce({ statusCode: 200, data: { username: "ming" } });
    await expect(readAccountSession()).resolves.toMatchObject({ state: "authenticated" });

    expect(taro.request).toHaveBeenCalledTimes(2);
  });
});
