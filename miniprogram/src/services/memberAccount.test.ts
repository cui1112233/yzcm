import { beforeEach, expect, test, vi } from "vitest";
const request = vi.hoisted(() => vi.fn());
vi.mock("./api", () => ({ apiRequest: request }));
import { readAccountSection, updateManagedMember, changeAccountPassword, createManagedAccount, setManagedMemberAccess } from "./memberAccount";
beforeEach(() => request.mockReset());
test("安全与团队使用网页现有接口，不把无权限或未知结构变成空成员", async () => {
  request.mockResolvedValueOnce({ mfa: { enabled: true }, sessions: [] }).mockResolvedValueOnce({ members: [{ username: "alice" }] });
  expect(await readAccountSection("security")).toMatchObject({ mfa: { enabled: true } });
  expect(await readAccountSection("team")).toMatchObject({ members: [{ username: "alice" }] });
  expect(request.mock.calls.map(([url]) => url)).toEqual(["/api/member/security", "/api/member/team"]);
  request.mockResolvedValueOnce({ error: "forbidden" });
  await expect(readAccountSection("team")).rejects.toThrow();
});
test("组员更新核对真实读回，不接受错账号或额度不匹配", async () => {
  request.mockResolvedValueOnce({ ok: true }).mockResolvedValueOnce({ members: [{ username: "alice", displayName: "旧名称" }] });
  await expect(updateManagedMember("team", "alice", { displayName: "新名称" })).rejects.toThrow(/读回/);
  expect(request.mock.calls[0]).toEqual(["/api/member/team/members/alice", { method: "PATCH", data: { displayName: "新名称" } }]);
});
test("密码只传现有修改接口，失败回执绝不报成功", async () => {
  request.mockResolvedValue({ ok: false, error: "wrong password" });
  await expect(changeAccountPassword("old", "new-password")).rejects.toThrow("wrong password");
  expect(request.mock.calls[0]).toEqual(["/api/member/security/password", { method: "POST", data: { currentPassword: "old", newPassword: "new-password" } }]);
});
test("new member uses existing create contract and requires account readback", async () => {
  request.mockResolvedValueOnce({ ok: true }).mockResolvedValueOnce({ members: [{ username: "a", displayName: "A" }] });
  await createManagedAccount("team", { username: "a", displayName: "A", password: "12345678", role: "manager", apiScopes: ["video"] });
  expect(request.mock.calls[0]).toEqual(["/api/member/team/members", { method: "POST", data: { username: "a", displayName: "A", password: "12345678", role: "member", apiEnabled: true, apiScopes: ["video"] } }]);
});
test("member status and scopes use dedicated endpoints and reject mismatched readback", async () => {
  request.mockResolvedValueOnce({ ok: true }).mockResolvedValueOnce({ ok: true }).mockResolvedValueOnce({ members: [{ username: "a", active: false, apiScopes: [] }] });
  await expect(setManagedMemberAccess("a", true, ["video"])).rejects.toThrow(/读回/);
  expect(request.mock.calls[1]).toEqual(["/api/member/team/members/a/api-scopes", { method: "PUT", data: { scopes: ["video"] } }]);
});
