import { describe, expect, test } from "vitest";
import { createSessionGate, getSessionErrorMessage, resolveSessionGate } from "./sessionGate";

describe("小程序启动会话状态", () => {
  test("没有令牌时进入连接中状态", () => {
    expect(createSessionGate(false)).toEqual({ status: "checking", error: "" });
  });

  test("绑定凭证返回时进入绑定状态", () => {
    expect(resolveSessionGate({ state: "binding-required", bindingTicket: "ticket" })).toEqual({
      status: "binding-required",
      error: "",
    });
  });

  test("异常会变成可展示的错误状态", () => {
    expect(resolveSessionGate(new Error("公网服务不可用"))).toEqual({
      status: "error",
      error: "公网服务不可用",
    });
    expect(getSessionErrorMessage({})).toBe("微信登录失败，请点击重试");
  });
});
