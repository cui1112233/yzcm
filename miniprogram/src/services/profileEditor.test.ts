import { expect, test, vi } from "vitest";
vi.mock("./api", () => ({ apiRequest: vi.fn() }));
import { apiRequest } from "./api";
import { saveMemberProfile, uploadProfileAvatar } from "./profileEditor";
const fields = { displayName: "新名称", phone: "123", email: "a@b.cn", teamTitle: "编导", bio: "简介" };
test("all editable fields use the existing public profile API and are verified by readback", async () => {
  vi.mocked(apiRequest).mockReset().mockResolvedValueOnce({ member: fields }).mockResolvedValueOnce({ member: { username: "account", ...fields } });
  await expect(saveMemberProfile(fields, "account")).resolves.toMatchObject({ member: fields });
  expect(apiRequest).toHaveBeenNthCalledWith(1, "/api/member/profile", { method: "PATCH", data: fields });
  expect(apiRequest).toHaveBeenNthCalledWith(2, "/api/member/me");
});
test("missing persisted fields cannot be called saved", async () => {
  vi.mocked(apiRequest).mockReset().mockResolvedValueOnce({}).mockResolvedValueOnce({ member: { username: "account", displayName: fields.displayName } });
  await expect(saveMemberProfile(fields, "account")).rejects.toThrow(/读回/);
});
test("blank names are rejected before sending and avatar uses original JSON contract", async () => {
  vi.mocked(apiRequest).mockReset();
  await expect(saveMemberProfile({ ...fields, displayName: " " }, "account")).rejects.toThrow(/显示名称/);
  expect(apiRequest).not.toHaveBeenCalled();
  vi.mocked(apiRequest).mockResolvedValueOnce({ member: { avatarUrl: "/a.png" } }).mockResolvedValueOnce({ member: { username: "account", avatarUrl: "/a.png" } });
  await expect(uploadProfileAvatar("data:image/png;base64,iVBORw==", "account")).resolves.toMatchObject({ member: { avatarUrl: "/a.png" } });
  expect(apiRequest).toHaveBeenNthCalledWith(1, "/api/member/avatar", { method: "POST", data: { avatarDataUrl: "data:image/png;base64,iVBORw==" } });
});
