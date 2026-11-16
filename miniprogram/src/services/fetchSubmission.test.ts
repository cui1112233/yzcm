import { expect, test, vi } from "vitest";
import { previewFetchSubmission, submitFetchSubmission } from "./fetchSubmission";
test("提交预览与确认复用书号选择及分组契约，未知回执不冒充已提交", async () => {
  const request = vi.fn().mockResolvedValueOnce({ groups: [{ items: [{ id: "b1" }] }], skipped: [] }).mockResolvedValueOnce({ accepted_groups: 1, success_groups: 0, failed_groups: 0, groups: [{ status: "accepted_pending" }] });
  await previewFetchSubmission(request, "b1");
  expect(request.mock.calls[0]).toEqual(["/api/batch-rewrite/web-submit/preview", { method: "POST", data: { mode: "selected", ids: ["b1"], force: false, grouped: true } }]);
  expect(await submitFetchSubmission(request, "b1")).toMatchObject({ accepted_groups: 1, success_groups: 0 });
  request.mockResolvedValue({ ok: true });
  await expect(submitFetchSubmission(request, "b1")).rejects.toThrow(/回执/);
});
