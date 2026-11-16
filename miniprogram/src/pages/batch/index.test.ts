import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { expect, test } from "vitest";

const source = readFileSync(resolve(__dirname, "index.tsx"), "utf8");

test("批量导入只能从服务端返回的公网书城目录选择", () => {
  expect(source).toContain('apiRequest<unknown>("/api/batch-rewrite/config")');
  expect(source).toContain("range={importPlatforms.map((platform) => platform.name)}");
  expect(source).toContain("setImportPlatformId(platform.id)");
  expect(source).toContain("setImportPlatformName(platform.name)");
  expect(source).not.toContain('placeholder="书城 ID');
  expect(source).not.toContain('placeholder="书城名称');
});

test("单书模式不会继续渲染批次工具栏和批次列表", () => {
  const source = readFileSync(resolve(__dirname, "index.tsx"), "utf8");
  expect(source).toContain('pageState.mode === "project"');
  expect(source).toContain('pageState.mode === "book"');
  expect(source).toContain("BookWorkbenchView");
});

test("新建批量在统一配置中提供原文获取与 AI 内容范围", () => {
  expect(source).toContain('aria-label="原文获取上限"');
  expect(source).toContain('aria-label="AI 内容范围"');
  expect(source).toContain("contentCaptureCharacters");
  expect(source).toContain("contentRangeLines");
  expect(source).toContain("每本原文最多");
});

test("每次新建批量默认立即执行全流程并自动上传", () => {
  expect(source).toContain('useState<"none" | "immediate" | "scheduled">("immediate")');
  expect(source).toContain('useState("full_submit")');
  expect(source).toContain('setImportExecution("immediate")');
  expect(source).toContain('setRunMode("full_submit")');
});

test("单书提交直连服务端发布回执，不再请求未部署的确认单接口", () => {
  expect(source).toContain("submitBatchFactoryBookTo121(pageState.selectedBatchId, bookId)");
  expect(source).toContain("已接收，等待后台列表回读");
  expect(source).toContain("视频管理系统已确认上传");
  expect(source).not.toContain("createPublishIntent(provider");
});
