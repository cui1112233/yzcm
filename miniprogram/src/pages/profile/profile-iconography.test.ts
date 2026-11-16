import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { expect, test } from "vitest";

const pageRoot = resolve(__dirname, "..");

test("个人中心按当前服务端角色生成菜单并使用本地图标", () => {
  const source = readFileSync(resolve(pageRoot, "profile/index.tsx"), "utf8");

  expect(source).toContain("buildProfileSections(facts)");
  expect(source).toContain('<AppIcon name="camera"');
  expect(source).not.toMatch(/[♙♕⌕☼⚙⌾›]/);
});

test("账号绑定页为账号凭证输入显示本地语义图标", () => {
  const source = readFileSync(resolve(pageRoot, "../components/LoginScene.tsx"), "utf8");

  expect(source).toContain('<AppIcon name="user-round"');
  expect(source).toContain('<AppIcon name="key-round"');
});
