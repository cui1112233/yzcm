import { expect, test, vi } from "vitest";
import { cleanFetchWebSettings, writeFetchMaintenance } from "./fetchMaintenance";

test("saved credentials never enter editable settings or replacement writes", () => {
  expect(cleanFetchWebSettings({ username: "u", password: "secret", password_masked: "***", selected_profile: "p" })).toEqual({ username: "u", password_masked: "***", selected_profile: "p" });
});
test("maintenance only reports confirmed after readback and never replays uncertain writes", async () => {
  const request = vi.fn().mockResolvedValueOnce({ ok: true }).mockRejectedValueOnce(new Error("offline"));
  await expect(writeFetchMaintenance(request, "/api/batch-rewrite/config", "POST", { app_config: {} }, "/api/batch-rewrite/config")).rejects.toThrow("结果未知");
  expect(request.mock.calls).toEqual([["/api/batch-rewrite/config", { method: "POST", data: { app_config: {} } }], ["/api/batch-rewrite/config"]]);
});
test("explicit server rejection is not reported as successful persistence", async () => {
  const request = vi.fn().mockResolvedValue({ ok: false, error: "denied" });
  await expect(writeFetchMaintenance(request, "/x", "PATCH", { enabled: false }, "/list")).rejects.toThrow("denied");
  expect(request).toHaveBeenCalledTimes(1);
});
