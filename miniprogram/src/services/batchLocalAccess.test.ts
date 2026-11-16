import { beforeEach, expect, test, vi } from "vitest";
const request = vi.hoisted(() => vi.fn());
const local = vi.hoisted(() => ({ enabled: false }));
vi.mock("./api", () => ({ apiRequest: request }));
vi.mock("../config/localAuth", () => ({ isLocalAccountLogin: () => local.enabled }));
import { batchLocalRequest, BATCH_LOCAL_NOTICE } from "./batchLocalAccess";

beforeEach(() => { request.mockReset(); local.enabled = false; });
test("explicit loopback environment can submit the existing business contract", async () => {
  local.enabled = true;
  request.mockResolvedValue({ batch: { id: "local" } });
  await expect(batchLocalRequest("/api/batch-factory/v12/batches", { method: "POST", data: { title: "local" } })).resolves.toEqual({ batch: { id: "local" } });
});
test.each(["POST", "PUT", "PATCH", "DELETE", "post"])("authorized public client forwards %s to the existing batch endpoint", async (method) => {
  request.mockResolvedValue({ ok: true });
  await expect(batchLocalRequest("/api/batch-factory/v12/batches", { method, data: { title: "作品" } })).resolves.toEqual({ ok: true });
  expect(request).toHaveBeenCalledWith("/api/batch-factory/v12/batches", { method, data: { title: "作品" }, recoverSession: false });
});
test.each(["https://other.test/api/batch-factory/v12/batches", "//other.test/api/batch-factory/v12/batches", "/api/member/profile", "/api/batch-factory/v12/../accounts"])("does not broaden batch write authority to %s", async (path) => {
  await expect(batchLocalRequest(path, { method: "POST" })).rejects.toThrow(BATCH_LOCAL_NOTICE);
  expect(request).not.toHaveBeenCalled();
});
test.each(["GET", "HEAD"])("allows %s without automatic session writes", async (method) => {
  request.mockResolvedValue({ batches: [] });
  await expect(batchLocalRequest("/api/batch-factory/v12/batches", { method, recoverSession: true })).resolves.toEqual({ batches: [] });
  expect(request).toHaveBeenCalledWith("/api/batch-factory/v12/batches", { method, recoverSession: false });
});
