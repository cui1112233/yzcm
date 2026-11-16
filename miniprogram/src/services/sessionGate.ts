import type { WechatSession } from "./session";

export type SessionGateState =
  | { status: "checking"; error: "" }
  | { status: "ready"; error: "" }
  | { status: "binding-required"; error: "" }
  | { status: "error"; error: string };

export function createSessionGate(hasToken: boolean): SessionGateState {
  return hasToken ? { status: "ready", error: "" } : { status: "checking", error: "" };
}

export function getSessionErrorMessage(cause: unknown) {
  if (cause && typeof cause === "object" && "message" in cause) {
    const message = String((cause as { message?: unknown }).message || "").trim();
    if (message) return message;
  }
  return "微信登录失败，请点击重试";
}

export function resolveSessionGate(result: WechatSession | unknown): SessionGateState {
  if (result && typeof result === "object" && "state" in result) {
    return (result as WechatSession).state === "binding-required"
      ? { status: "binding-required", error: "" }
      : { status: "ready", error: "" };
  }
  return { status: "error", error: getSessionErrorMessage(result) };
}
